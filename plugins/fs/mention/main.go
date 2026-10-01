package main

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/hashicorp/go-plugin"
	"gopkg.in/yaml.v3"

	fsplugin "github.com/appmatter/cage/pkg/plugin/v1/fs"
)

const (
	defaultLimit        = 20
	maxIndexEntries     = 20000
	maxSnapshotBytes    = 64 << 10
	maxDirectoryEntries = 200
	maxDirectoryDepth   = 8
)

func main() {
	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: fsplugin.Handshake,
		Plugins:         fsplugin.PluginMap(&Mention{}),
	})
}

// Mention indexes configured host paths and returns safe mention snapshots.
type Mention struct {
	mu      sync.RWMutex
	include []string
	exclude []string
	roots   []root
}

type root struct {
	host  string
	path  string
	guest string
}

type mention struct {
	Path             string   `json:"path"`
	Kind             string   `json:"kind"`
	GuestPath        string   `json:"guestPath,omitempty"`
	VisibleToGuest   bool     `json:"visibleToGuest"`
	Content          string   `json:"content,omitempty"`
	DirectoryEntries []string `json:"directoryEntries,omitempty"`
}

type indexed struct {
	mention mention
	host    string
}

func (m *Mention) Name() string { return "mention" }

func (m *Mention) Configure(ctx fsplugin.Context) error {
	var cfg struct {
		Include []string `yaml:"include"`
		Exclude []string `yaml:"exclude"`
	}
	if err := yaml.Unmarshal(ctx.SeatYAML, &cfg); err != nil {
		return fmt.Errorf("mention configure: %w", err)
	}
	if ctx.ProjectRoot == "" {
		return fmt.Errorf("mention: project root is required")
	}
	roots := []root{{host: ctx.ProjectRoot}}
	for _, path := range append(append([]fsplugin.Path{}, ctx.Mounts...), ctx.Copies...) {
		if path.Host == "" || path.Guest == "" {
			continue
		}
		rel, err := filepath.Rel(ctx.ProjectRoot, path.Host)
		if err != nil || strings.HasPrefix(rel, "..") {
			continue
		}
		roots = append([]root{{host: path.Host, path: cleanPath(rel), guest: path.Guest}}, roots...)
	}
	for i := range roots {
		info, err := os.Stat(roots[i].host)
		if err != nil || !info.IsDir() {
			continue
		}
		roots[i].host, err = filepath.Abs(roots[i].host)
		if err != nil {
			return err
		}
	}
	include := cfg.Include
	if len(include) == 0 {
		include = []string{"**/*"}
	}
	m.mu.Lock()
	m.include, m.exclude, m.roots = include, cfg.Exclude, roots
	m.mu.Unlock()
	return nil
}

func (m *Mention) Suggest(query string, limit int) ([]mention, error) {
	entries, err := m.entries()
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > defaultLimit {
		limit = defaultLimit
	}
	query = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(query), "@"))
	out := make([]mention, 0, limit)
	for _, entry := range entries {
		if query != "" && !matchesQuery(entry.mention.Path, query) {
			continue
		}
		out = append(out, entry.mention)
		if len(out) == limit {
			break
		}
	}
	return out, nil
}

func (m *Mention) Resolve(in []mention) ([]mention, error) {
	entries, err := m.entries()
	if err != nil {
		return nil, err
	}
	byPath := make(map[string]indexed, len(entries))
	for _, entry := range entries {
		byPath[entry.mention.Path] = entry
	}
	out := make([]mention, 0, len(in))
	seen := map[string]bool{}
	for _, want := range in {
		path := cleanPath(want.Path)
		if want.Kind == "directory" && !strings.HasSuffix(path, "/") {
			path += "/"
		}
		entry, ok := byPath[path]
		if !ok || seen[path] {
			return nil, fmt.Errorf("mention: path %q is not indexed", want.Path)
		}
		seen[path] = true
		resolved := entry.mention
		if !resolved.VisibleToGuest {
			if resolved.Kind == "file" {
				data, err := os.ReadFile(entry.host)
				if err != nil {
					return nil, fmt.Errorf("mention: read %q: %w", resolved.Path, err)
				}
				if len(data) > maxSnapshotBytes {
					return nil, fmt.Errorf("mention: %q exceeds %d byte snapshot limit", resolved.Path, maxSnapshotBytes)
				}
				resolved.Content = string(data)
			} else {
				listing, err := m.directoryListing(entry.host)
				if err != nil {
					return nil, err
				}
				resolved.DirectoryEntries = listing
			}
		}
		out = append(out, resolved)
	}
	return out, nil
}

func (m *Mention) Call(in fsplugin.Request) (fsplugin.Response, error) {
	switch in.Operation {
	case "suggest":
		var request struct {
			Query string `json:"query"`
			Limit int    `json:"limit"`
		}
		if err := json.Unmarshal(in.Payload, &request); err != nil {
			return fsplugin.Response{}, err
		}
		out, err := m.Suggest(request.Query, request.Limit)
		if err != nil {
			return fsplugin.Response{}, err
		}
		payload, err := json.Marshal(out)
		return fsplugin.Response{Payload: payload}, err
	case "resolve":
		var request []mention
		if err := json.Unmarshal(in.Payload, &request); err != nil {
			return fsplugin.Response{}, err
		}
		out, err := m.Resolve(request)
		if err != nil {
			return fsplugin.Response{}, err
		}
		payload, err := json.Marshal(out)
		return fsplugin.Response{Payload: payload}, err
	default:
		return fsplugin.Response{}, fmt.Errorf("mention: unsupported operation %q", in.Operation)
	}
}

func (m *Mention) entries() ([]indexed, error) {
	m.mu.RLock()
	include, exclude, roots := append([]string{}, m.include...), append([]string{}, m.exclude...), append([]root{}, m.roots...)
	m.mu.RUnlock()
	var out []indexed
	seen := map[string]bool{}
	for _, root := range roots {
		err := filepath.WalkDir(root.host, func(host string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if len(out) >= maxIndexEntries {
				return filepath.SkipAll
			}
			rel, err := filepath.Rel(root.host, host)
			if err != nil || rel == "." {
				return nil
			}
			rel = filepath.ToSlash(rel)
			path := joinPath(root.path, rel)
			if excluded(path, d.IsDir(), exclude) {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if !d.IsDir() && !included(path, include) {
				return nil
			}
			kind := "file"
			if d.IsDir() {
				kind, path = "directory", path+"/"
			}
			if seen[path] {
				return nil
			}
			seen[path] = true
			guest := ""
			if root.guest != "" {
				guest = filepath.ToSlash(filepath.Join(root.guest, rel))
			}
			out = append(out, indexed{mention: mention{Path: path, Kind: kind, GuestPath: guest, VisibleToGuest: guest != ""}, host: host})
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].mention.Path < out[j].mention.Path })
	return out, nil
}

func (m *Mention) directoryListing(root string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil || rel == "." {
			return nil
		}
		if strings.Count(filepath.ToSlash(rel), "/") >= maxDirectoryDepth {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		out = append(out, filepath.ToSlash(rel))
		if len(out) == maxDirectoryEntries {
			return filepath.SkipAll
		}
		return nil
	})
	return out, err
}

func cleanPath(path string) string {
	path = strings.TrimPrefix(filepath.ToSlash(strings.TrimSpace(path)), "./")
	return strings.TrimPrefix(path, "/")
}
func joinPath(prefix, path string) string {
	if prefix == "" {
		return path
	}
	return strings.TrimSuffix(prefix, "/") + "/" + path
}
func matchesQuery(path, query string) bool {
	path = strings.ToLower(path)
	return strings.Contains(path, query) || strings.Contains(strings.ToLower(filepath.Base(path)), query)
}
func included(path string, patterns []string) bool { return anyGlob(path, patterns) }
func excluded(path string, dir bool, patterns []string) bool {
	if anyGlob(path, patterns) {
		return true
	}
	return dir && anyGlob(strings.TrimSuffix(path, "/")+"/**", patterns)
}
func anyGlob(path string, patterns []string) bool {
	for _, pattern := range patterns {
		if globMatch(pattern, path) {
			return true
		}
	}
	return false
}
func globMatch(pattern, path string) bool {
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		switch pattern[i] {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				if i+2 < len(pattern) && pattern[i+2] == '/' {
					b.WriteString("(?:.*/)?")
					i += 2
				} else {
					b.WriteString(".*")
					i++
				}
			} else {
				b.WriteString("[^/]*")
			}
		case '?':
			b.WriteString("[^/]")
		default:
			b.WriteString(regexp.QuoteMeta(string(pattern[i])))
		}
	}
	b.WriteString("$")
	return regexp.MustCompile(b.String()).MatchString(path)
}
