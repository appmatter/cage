package contextapi

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/appmatter/cage/internal/config"
)

var validConfigID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// Entry is one allowlisted config in the served project.
type Entry struct {
	ID          string
	ProjectRoot string
	ConfigPath  string
	Resolved    config.Resolved
}

// Project is the server-owned allowlist of configs for one repo.
//
// Clients pick a config id from this list; they never supply host paths.
// Each entry's resolved YAML is the shared VM template for named VMs under it.
type Project struct {
	Root    string
	Configs map[string]Entry
}

// Get returns an allowlisted config by id.
func (p *Project) Get(id string) (Entry, bool) {
	if p == nil {
		return Entry{}, false
	}
	e, ok := p.Configs[id]
	return e, ok
}

// LoadProject resolves one project root and the configs it may serve.
// Empty configInputs uses the usual config.Resolve path (one file, or --config).
func LoadProject(projectRoot string, configInputs []string, goos string) (*Project, error) {
	root, err := filepath.Abs(projectRoot)
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("project: %w", err)
	}
	if !st.IsDir() {
		return nil, fmt.Errorf("project %q is not a directory", root)
	}

	if len(configInputs) == 0 {
		resolved, err := config.Resolve(root, "")
		if err != nil {
			return nil, err
		}
		configInputs = []string{resolved}
	}

	out := &Project{Root: root, Configs: make(map[string]Entry, len(configInputs))}
	for i, raw := range configInputs {
		entry, err := loadConfig(root, raw, goos)
		if err != nil {
			return nil, fmt.Errorf("config[%d]: %w", i, err)
		}
		if _, exists := out.Configs[entry.ID]; exists {
			return nil, fmt.Errorf("duplicate config id %q", entry.ID)
		}
		out.Configs[entry.ID] = entry
	}
	return out, nil
}

func loadConfig(projectRoot, configInput, goos string) (Entry, error) {
	configInput = strings.TrimSpace(configInput)
	if configInput == "" {
		return Entry{}, fmt.Errorf("config is required")
	}
	configPath := configInput
	if !filepath.IsAbs(configPath) {
		configPath = filepath.Join(projectRoot, configPath)
	}
	configPath, err := filepath.Abs(configPath)
	if err != nil {
		return Entry{}, err
	}
	if err := pathWithinRoot(projectRoot, configPath); err != nil {
		return Entry{}, err
	}
	if _, err := os.Stat(configPath); err != nil {
		return Entry{}, fmt.Errorf("config: %w", err)
	}
	id := configAlias(configPath)
	if !validConfigID.MatchString(id) {
		return Entry{}, fmt.Errorf("invalid config id %q", id)
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

func configAlias(path string) string {
	base := filepath.Base(path)
	name := strings.TrimSuffix(base, filepath.Ext(base))
	if name == "cage" {
		return "default"
	}
	if strings.HasPrefix(name, "cage.") {
		return strings.TrimPrefix(name, "cage.")
	}
	return name
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

// ServeState is written to .cage/serve.json so clients can attach.
type ServeState struct {
	Addr         string   `json:"addr"`
	Token        string   `json:"token"`
	PID          int      `json:"pid"`
	AllowedHosts []string `json:"allowedHosts,omitempty"`
}

// ServeStatePath is the discovery file for this project's serve.
func ServeStatePath(projectRoot string) string {
	return filepath.Join(projectRoot, ".cage", "serve.json")
}

// WriteServeState writes pid, addr, and token for this project's serve.
func WriteServeState(projectRoot string, state ServeState) error {
	dir := filepath.Join(projectRoot, ".cage")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return os.WriteFile(ServeStatePath(projectRoot), append(b, '\n'), 0o600)
}
