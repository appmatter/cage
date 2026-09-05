package pluginhost

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// InstallOptions controls plugin install.
type InstallOptions struct {
	Source      string // git:host/path@ref | local path
	Project     bool   // install under .cage/.cache/plugins
	ProjectRoot string
	Kind        string // override; otherwise inferred from local module or required for git later
	Name        string
}

// Install builds and registers a plugin from a local path or git source.
func Install(opts InstallOptions) (Manifest, error) {
	src := opts.Source
	if src == "" {
		return Manifest{}, fmt.Errorf("source is required")
	}

	root, err := installRoot(opts.Project, opts.ProjectRoot)
	if err != nil {
		return Manifest{}, err
	}

	var (
		buildDir string
		source   string
		pin      string
		cleanup  func()
	)

	if strings.HasPrefix(src, "git:") {
		buildDir, source, pin, cleanup, err = fetchGit(src)
		if err != nil {
			return Manifest{}, err
		}
		if cleanup != nil {
			defer cleanup()
		}
	} else {
		buildDir, err = filepath.Abs(src)
		if err != nil {
			return Manifest{}, err
		}
		source = buildDir
		pin = "local"
	}

	kind, name, err := inferKindName(buildDir, opts.Kind, opts.Name)
	if err != nil {
		return Manifest{}, err
	}

	binName := fmt.Sprintf("cage-%s-%s%s", kind, name, BinaryExt)
	outDir := filepath.Join(root, kind, name)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return Manifest{}, err
	}
	binPath := filepath.Join(outDir, binName)
	if !filepath.IsAbs(binPath) {
		absOut, err := filepath.Abs(binPath)
		if err != nil {
			return Manifest{}, err
		}
		binPath = absOut
	}

	cmd := exec.Command("go", "build", "-o", binPath, ".")
	cmd.Dir = buildDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if err := cmd.Run(); err != nil {
		return Manifest{}, fmt.Errorf("go build: %w", err)
	}
	if _, err := os.Stat(binPath); err != nil {
		return Manifest{}, fmt.Errorf("go build produced no binary at %s", binPath)
	}

	m := Manifest{
		Kind:    kind,
		Name:    name,
		Command: binName,
		Source:  source,
		Pin:     pin,
	}
	if meta, err := readPluginMeta(buildDir); err == nil {
		m.Stage = meta.Stage
		m.Hooks = append([]string{}, meta.Hooks...)
		m.Commands = append([]string{}, meta.Commands...)
		m.EgressHints = append([]EgressHint{}, meta.EgressHints...)
		m.Client = meta.Client
	}
	if opts.Project {
		base := opts.ProjectRoot
		if base == "" {
			base = "."
		}
		if absBase, err := filepath.Abs(base); err == nil {
			if rel, err := filepath.Rel(absBase, source); err == nil && !strings.HasPrefix(rel, "..") && pin == "local" {
				m.Source = filepath.ToSlash(rel)
			}
		}
	}
	if err := WriteManifest(root, m); err != nil {
		return Manifest{}, err
	}
	if opts.Project {
		if err := UpsertLockEntry(opts.ProjectRoot, m); err != nil {
			return Manifest{}, err
		}
	}
	return m, nil
}

// Remove deletes an installed plugin directory.
func Remove(project bool, projectRoot, kind, name string) error {
	if kind == "" || name == "" {
		return fmt.Errorf("kind and name are required (e.g. runtime/tart)")
	}
	root, err := installRoot(project, projectRoot)
	if err != nil {
		return err
	}
	dir := filepath.Join(root, kind, name)
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	if project {
		return RemoveLockEntry(projectRoot, kind, name)
	}
	return nil
}

func installRoot(project bool, projectRoot string) (string, error) {
	if project {
		root := ProjectDir(projectRoot)
		return root, os.MkdirAll(root, 0o755)
	}
	root, err := GlobalDir()
	if err != nil {
		return "", err
	}
	return root, os.MkdirAll(root, 0o755)
}

func inferKindName(buildDir, kind, name string) (string, string, error) {
	if kind != "" && name != "" {
		return kind, name, nil
	}
	// plugins/runtime/tart → runtime, tart
	parts := strings.Split(filepath.ToSlash(buildDir), "/")
	for i := 0; i < len(parts)-1; i++ {
		if parts[i] == "plugins" && i+2 < len(parts) {
			return parts[i+1], parts[i+2], nil
		}
	}
	if kind == "" || name == "" {
		return "", "", fmt.Errorf("cannot infer kind/name; pass --kind and --name")
	}
	return kind, name, nil
}

func fetchGit(src string) (dir, source, pin string, cleanup func(), err error) {
	ref := strings.TrimPrefix(src, "git:")
	pin = "HEAD"
	if i := strings.LastIndex(ref, "@"); i >= 0 {
		pin = ref[i+1:]
		ref = ref[:i]
	}
	url := gitURL(ref)
	tmp, err := os.MkdirTemp("", "cage-plugin-*")
	if err != nil {
		return "", "", "", nil, err
	}
	cleanup = func() { _ = os.RemoveAll(tmp) }

	clone := exec.Command("git", "clone", "--depth", "1", url, tmp)
	if pin != "HEAD" && !looksLikeCommit(pin) {
		clone = exec.Command("git", "clone", "--depth", "1", "--branch", pin, url, tmp)
	}
	clone.Stdout = os.Stdout
	clone.Stderr = os.Stderr
	if err := clone.Run(); err != nil {
		cleanup()
		return "", "", "", nil, fmt.Errorf("git clone: %w", err)
	}
	if looksLikeCommit(pin) {
		co := exec.Command("git", "checkout", pin)
		co.Dir = tmp
		co.Stdout = os.Stdout
		co.Stderr = os.Stderr
		if err := co.Run(); err != nil {
			cleanup()
			return "", "", "", nil, fmt.Errorf("git checkout %s: %w", pin, err)
		}
	}
	// Resolve actual commit for the lock.
	rev := exec.Command("git", "rev-parse", "HEAD")
	rev.Dir = tmp
	out, err := rev.Output()
	if err == nil {
		pin = strings.TrimSpace(string(out))
	}
	return tmp, "git:" + ref, pin, cleanup, nil
}

func gitURL(ref string) string {
	if strings.HasPrefix(ref, "github.com/") {
		return "https://" + ref + ".git"
	}
	if strings.Contains(ref, "://") {
		return ref
	}
	return "https://" + ref + ".git"
}

func looksLikeCommit(s string) bool {
	if len(s) < 7 {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}

type pluginMeta struct {
	Stage       string       `json:"stage"`
	Hooks       []string     `json:"hooks"`
	Commands    []string     `json:"commands"`
	EgressHints []EgressHint `json:"egress_hints"`
	Client      bool         `json:"client"`
}

func readPluginMeta(buildDir string) (pluginMeta, error) {
	b, err := os.ReadFile(filepath.Join(buildDir, "plugin.json"))
	if err != nil {
		return pluginMeta{}, err
	}
	var m pluginMeta
	if err := json.Unmarshal(b, &m); err != nil {
		return pluginMeta{}, err
	}
	return m, nil
}
