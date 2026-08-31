package hooks

import (
	"archive/tar"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/appmatter/cage/internal/config"
	runtimeplugin "github.com/appmatter/cage/pkg/plugin/v1/runtime"
)

// PiGuestAgentDir is where pi reads models.json / auth in the Ubuntu Tart guest.
const PiGuestAgentDir = "/home/admin/.pi/agent"

// ResolveAgentDir returns the abs host agent config dir for a harness seat.
// Override: seat agent_dir (abs or project-relative).
// Default: .cage/plugins/runtime/<pluginID> (committed plugin artefacts).
func ResolveAgentDir(projectRoot string, hs config.HarnessSeat) string {
	if projectRoot == "" {
		projectRoot = "."
	}
	if hs.AgentDir != "" {
		if filepath.IsAbs(hs.AgentDir) {
			return hs.AgentDir
		}
		return filepath.Join(projectRoot, hs.AgentDir)
	}
	name := hs.PluginID
	if name == "" {
		name = hs.Seat
	}
	if name == "" {
		name = "pi-agent"
	}
	return filepath.Join(projectRoot, ".cage", "plugins", "runtime", name)
}

// SyncAgentDirToGuest tars hostDir into the guest pi agent directory.
func SyncAgentDirToGuest(b runtimeplugin.Backend, vmID, hostDir string) error {
	if b == nil || hostDir == "" {
		return nil
	}
	st, err := os.Stat(hostDir)
	if err != nil || !st.IsDir() {
		return nil
	}
	payload, err := tarDir(hostDir)
	if err != nil {
		return err
	}
	if len(payload) == 0 {
		return nil
	}
	if err := b.Exec(vmID, runtimeplugin.ExecOpts{
		Argv: []string{"sudo", "mkdir", "-p", PiGuestAgentDir},
	}); err != nil {
		return err
	}
	if err := b.Exec(vmID, runtimeplugin.ExecOpts{
		Argv:  []string{"sudo", "tar", "-x", "-C", PiGuestAgentDir},
		Stdin: payload,
	}); err != nil {
		return err
	}
	_ = b.Exec(vmID, runtimeplugin.ExecOpts{
		Argv: []string{"sudo", "chown", "-R", "admin:admin", "/home/admin/.pi"},
	})
	return nil
}

func tarDir(root string) ([]byte, error) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		// Skip local-only / secrets patterns if present on host.
		base := filepath.Base(rel)
		if base == "auth.json" || strings.HasSuffix(base, ".key") {
			return nil
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = filepath.ToSlash(rel)
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(tw, f)
		return err
	})
	if err != nil {
		_ = tw.Close()
		return nil, err
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
