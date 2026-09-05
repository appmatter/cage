package contextapi

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/appmatter/cage/internal/config"
)

var validRegistryID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

// RegistryFile is the on-disk / CLI input for server-owned instances.
// Paths here are server-selected only; HTTP must never supply them.
type RegistryFile struct {
	Instances []RegistryFileEntry `yaml:"instances"`
}

// RegistryFileEntry is one named registry row before validation.
type RegistryFileEntry struct {
	ID      string `yaml:"id"`
	Project string `yaml:"project"`
	Config  string `yaml:"config"`
}

// Entry is one validated registry instance: project + allowlisted config.
type Entry struct {
	ID          string
	ProjectRoot string
	ConfigPath  string
	Resolved    config.Resolved
}

// Registry is the loaded, validated set of server-owned instances.
//
// A registry is the server-owned allowlist of Cage setups a client may target.
// Each entry is a named alias → fixed project root + one active config. Clients
// select an instance id from this list; they never supply host paths. The
// entry's resolved config is the shared VM template for named VMs under it.
type Registry struct {
	Entries map[string]Entry
}

// LoadRegistryFile reads YAML from path and validates every instance.
func LoadRegistryFile(path, goos string) (*Registry, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var file RegistryFile
	if err := yaml.Unmarshal(b, &file); err != nil {
		return nil, fmt.Errorf("registry: %w", err)
	}
	return LoadRegistry(file, goos)
}

// LoadRegistry validates registry input and resolves each active config.
func LoadRegistry(file RegistryFile, goos string) (*Registry, error) {
	if len(file.Instances) == 0 {
		return nil, fmt.Errorf("registry: no instances")
	}
	out := &Registry{Entries: make(map[string]Entry, len(file.Instances))}
	for i, raw := range file.Instances {
		entry, err := validateRegistryEntry(raw, goos)
		if err != nil {
			return nil, fmt.Errorf("registry instances[%d]: %w", i, err)
		}
		if _, exists := out.Entries[entry.ID]; exists {
			return nil, fmt.Errorf("registry: duplicate instance id %q", entry.ID)
		}
		out.Entries[entry.ID] = entry
	}
	return out, nil
}

// Get returns a registry entry by id.
func (r *Registry) Get(id string) (Entry, bool) {
	if r == nil {
		return Entry{}, false
	}
	e, ok := r.Entries[id]
	return e, ok
}

func validateRegistryEntry(raw RegistryFileEntry, goos string) (Entry, error) {
	id := strings.TrimSpace(raw.ID)
	if id == "" {
		return Entry{}, fmt.Errorf("id is required")
	}
	if !validRegistryID.MatchString(id) {
		return Entry{}, fmt.Errorf("invalid id %q", id)
	}
	project := strings.TrimSpace(raw.Project)
	if project == "" {
		return Entry{}, fmt.Errorf("project is required")
	}
	projectRoot, err := filepath.Abs(project)
	if err != nil {
		return Entry{}, err
	}
	st, err := os.Stat(projectRoot)
	if err != nil {
		return Entry{}, fmt.Errorf("project: %w", err)
	}
	if !st.IsDir() {
		return Entry{}, fmt.Errorf("project %q is not a directory", projectRoot)
	}

	configInput := strings.TrimSpace(raw.Config)
	if configInput == "" {
		return Entry{}, fmt.Errorf("config is required")
	}
	configPath := configInput
	if !filepath.IsAbs(configPath) {
		configPath = filepath.Join(projectRoot, configPath)
	}
	configPath, err = filepath.Abs(configPath)
	if err != nil {
		return Entry{}, err
	}
	if err := pathWithinRoot(projectRoot, configPath); err != nil {
		return Entry{}, err
	}
	if _, err := os.Stat(configPath); err != nil {
		return Entry{}, fmt.Errorf("config: %w", err)
	}

	resolved, err := config.LoadResolved(projectRoot, configPath, goos)
	if err != nil {
		return Entry{}, fmt.Errorf("config %q: %w", configPath, err)
	}
	return Entry{
		ID:          id,
		ProjectRoot: projectRoot,
		ConfigPath:  configPath,
		Resolved:    resolved,
	}, nil
}

func pathWithinRoot(root, path string) error {
	root = filepath.Clean(root)
	path = filepath.Clean(path)
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return fmt.Errorf("config %q escapes project %q", path, root)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("config %q escapes project %q", path, root)
	}
	return nil
}
