package pluginhost

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// LockFile is the committed project plugin pin list.
type LockFile struct {
	Plugins []Manifest `json:"plugins"`
}

// LockPath is .cage/plugins.lock.json under the project.
func LockPath(projectRoot string) string {
	if projectRoot == "" {
		projectRoot = "."
	}
	return filepath.Join(projectRoot, ".cage", "plugins.lock.json")
}

// LoadLock reads the project lock file. Missing file → empty lock.
func LoadLock(projectRoot string) (LockFile, error) {
	path := LockPath(projectRoot)
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return LockFile{}, nil
		}
		return LockFile{}, err
	}
	var lock LockFile
	if err := json.Unmarshal(b, &lock); err != nil {
		return LockFile{}, err
	}
	return lock, nil
}

// SaveLock writes the project lock file.
func SaveLock(projectRoot string, lock LockFile) error {
	dir := filepath.Join(projectRoot, ".cage")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if lock.Plugins == nil {
		lock.Plugins = []Manifest{}
	}
	b, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(LockPath(projectRoot), append(b, '\n'), 0o644)
}

// UpsertLockEntry adds or updates a plugin in the lock.
// Same kind+name + same source → update pin. Same kind+name + different source → error
// (install under a different --name, then point config package: at the source).
func UpsertLockEntry(projectRoot string, m Manifest) error {
	lock, err := LoadLock(projectRoot)
	if err != nil {
		return err
	}
	for i := range lock.Plugins {
		p := lock.Plugins[i]
		if p.Kind != m.Kind || p.Name != m.Name {
			continue
		}
		if normalizePluginSource(p.Source) != normalizePluginSource(m.Source) {
			return fmt.Errorf(
				"plugin %s/%s already locked from %q; refusing %q — install with --name <alias> and set package: in config",
				m.Kind, m.Name, p.Source, m.Source,
			)
		}
		lock.Plugins[i] = m
		return SaveLock(projectRoot, lock)
	}
	lock.Plugins = append(lock.Plugins, m)
	return SaveLock(projectRoot, lock)
}

func normalizePluginSource(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, ".git")
	return filepath.ToSlash(s)
}

// RemoveLockEntry drops a plugin from the lock.
func RemoveLockEntry(projectRoot, kind, name string) error {
	lock, err := LoadLock(projectRoot)
	if err != nil {
		return err
	}
	out := make([]Manifest, 0, len(lock.Plugins))
	for _, p := range lock.Plugins {
		if p.Kind == kind && p.Name == name {
			continue
		}
		out = append(out, p)
	}
	return SaveLock(projectRoot, LockFile{Plugins: out})
}

// InstallFromLock installs every plugin listed in plugins.lock.json into the project plugins dir.
func InstallFromLock(projectRoot string) error {
	lock, err := LoadLock(projectRoot)
	if err != nil {
		return err
	}
	if len(lock.Plugins) == 0 {
		return fmt.Errorf("no plugins in %s", LockPath(projectRoot))
	}
	for _, p := range lock.Plugins {
		src := p.Source
		if p.Pin != "" && p.Pin != "local" && strings.HasPrefix(src, "git:") && !strings.Contains(src, "@") {
			src = src + "@" + p.Pin
		}
		_, err := Install(InstallOptions{
			Source:      src,
			Project:     true,
			ProjectRoot: projectRoot,
			Kind:        p.Kind,
			Name:        p.Name,
		})
		if err != nil {
			return fmt.Errorf("%s/%s: %w", p.Kind, p.Name, err)
		}
	}
	return nil
}
