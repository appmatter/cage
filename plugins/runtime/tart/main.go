package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/hashicorp/go-plugin"

	"github.com/appmatter/cage/internal/termlog"
	runtimeplugin "github.com/appmatter/cage/pkg/plugin/v1/runtime"
)

const (
	virtiofsTag      = "com.apple.virtio-fs.automount"
	readyTimeout     = 45 * time.Second
	readyPollEvery   = 2 * time.Second
	defaultShareRoot = "/tmp/cage"
)

func main() {
	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: runtimeplugin.Handshake,
		Plugins:         runtimeplugin.PluginMap(&Tart{}),
	})
}

// Tart implements runtime.Backend via the tart CLI.
type Tart struct{}

func (t *Tart) Name() string { return "tart" }

func progress(format string, args ...any) {
	termlog.Plugin("tart", format, args...)
}

func (t *Tart) Create(spec runtimeplugin.Spec) error {
	if spec.ID == "" {
		return fmt.Errorf("id is required")
	}
	if spec.Image == "" {
		return fmt.Errorf("image is required")
	}
	if err := ensureTart(); err != nil {
		return err
	}
	exists, err := t.vmExists(spec.ID)
	if err != nil {
		return err
	}
	if exists {
		if from, err := readImageStamp(spec.ID); err == nil && from != "" && from != spec.Image {
			return fmt.Errorf("VM instance exists from image %q but config wants %q — run cage vm delete --id <instance> first",
				from, spec.Image)
		}
		progress("vm %q already exists", spec.ID)
		return nil
	}
	progress("ensuring image %s", spec.Image)
	if err := ensureImage(spec.Image); err != nil {
		return err
	}
	progress("cloning %s → %s", spec.Image, spec.ID)
	if err := runTart("clone", spec.Image, spec.ID); err != nil {
		return err
	}
	return writeImageStamp(spec.ID, spec.Image)
}

func (t *Tart) Start(spec runtimeplugin.Spec) error {
	if spec.ID == "" {
		return fmt.Errorf("id is required")
	}
	if err := ensureTart(); err != nil {
		return err
	}
	if err := checkMountHosts(spec.Mounts); err != nil {
		return err
	}

	wantUI := spec.Graphics
	// Always bring the VM up headless for mounts/lifecycle so the Tart window
	// does not open before the guest is actually ready.
	if err := t.ensureRunning(spec, false); err != nil {
		return err
	}
	if err := applyGuestFS(spec); err != nil {
		return err
	}
	if err := runOnCreate(spec); err != nil {
		return err
	}
	if err := runHostScripts(spec.ID, spec.Workdir, "on-start", spec.OnStart); err != nil {
		return err
	}
	if !wantUI {
		return nil
	}
	progress("setup done; reopening with graphics UI")
	if err := t.Stop(spec.ID); err != nil {
		return err
	}
	if err := t.ensureRunning(spec, true); err != nil {
		return err
	}
	return applyGuestFS(spec)
}

// ensureRunning starts tart run if needed. graphics=false → --no-graphics.
// If the VM is already running without the requested ExtraRunArgs (e.g. softnet),
// it is stopped and relaunched so network lock args actually apply.
func (t *Tart) ensureRunning(spec runtimeplugin.Spec, graphics bool) error {
	st, err := t.Status(spec.ID)
	if err != nil {
		return err
	}
	if st.State == "running" && len(spec.ExtraRunArgs) > 0 && !tartRunHasArgs(spec.ID, spec.ExtraRunArgs) {
		progress("restarting %s to apply ExtraRunArgs", spec.ID)
		if err := t.Stop(spec.ID); err != nil {
			return err
		}
		st.State = "stopped"
	}
	var runDied <-chan error
	if st.State != "running" {
		progress("running %s (%d mounts, graphics=%v)", spec.ID, len(spec.Mounts), graphics)
		args := []string{"run"}
		if !graphics {
			args = append(args, "--no-graphics")
		}
		args = append(args, spec.ExtraRunArgs...)
		top, nestedRO, nestedRW, err := partitionMounts(spec.Mounts)
		if err != nil {
			return err
		}
		dirs := append([]runtimeplugin.PathSpec{}, top...)
		dirs = append(dirs, nestedRO...)
		dirs = append(dirs, nestedRW...)
		args = append(args, dirArgs(dirs)...)
		args = append(args, spec.ID)
		cmd := exec.Command("tart", args...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("tart run: %w", err)
		}
		died := make(chan error, 1)
		go func() { died <- cmd.Wait() }()
		runDied = died
	} else {
		progress("vm %q already running", spec.ID)
	}
	progress("waiting for guest agent (up to %s)", readyTimeout)
	return waitReady(spec.ID, runDied)
}

// tartRunHasArgs reports whether a live tart process for id includes all want args.
func tartRunHasArgs(id string, want []string) bool {
	if len(want) == 0 {
		return true
	}
	out, err := exec.Command("ps", "-ax", "-o", "args=").Output()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.Contains(line, "tart") || !strings.Contains(line, " run") {
			continue
		}
		if !strings.Contains(line, id) {
			continue
		}
		ok := true
		for _, w := range want {
			if !strings.Contains(line, w) {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

func applyGuestFS(spec runtimeplugin.Spec) error {
	shareRoot := defaultShareRoot
	if len(spec.Mounts) > 0 {
		top, nestedRO, nestedRW, err := partitionMounts(spec.Mounts)
		if err != nil {
			return err
		}
		progress("mounting virtiofs → %s", shareRoot)
		if err := mountVirtiofs(spec.ID, shareRoot); err != nil {
			return err
		}
		if len(top) > 0 {
			progress("linking %d mounts under guest paths", len(top))
			if err := linkMounts(spec.ID, shareRoot, top); err != nil {
				return err
			}
		}
		if len(nestedRO) > 0 {
			progress("bind-mounting %d nested ro shares", len(nestedRO))
			if err := bindNested(spec.ID, shareRoot, nestedRO, true); err != nil {
				return err
			}
		}
		if len(nestedRW) > 0 {
			progress("bind-mounting %d nested rw shares", len(nestedRW))
			if err := bindNested(spec.ID, shareRoot, nestedRW, false); err != nil {
				return err
			}
		}
	}
	if len(spec.DenyMasks) > 0 {
		progress("masking %d denied paths under mounts", len(spec.DenyMasks))
		if err := applyDenyMasks(spec.ID, shareRoot, spec.DenyMasks); err != nil {
			return err
		}
	}
	if len(spec.Copies) > 0 {
		progress("applying %d copies", len(spec.Copies))
	}
	return applyCopies(spec.ID, spec.Copies)
}

func (t *Tart) Stop(id string) error {
	if id == "" {
		return fmt.Errorf("id is required")
	}
	if err := ensureTart(); err != nil {
		return err
	}
	return runTart("stop", id)
}

func (t *Tart) Status(id string) (runtimeplugin.Status, error) {
	if id == "" {
		return runtimeplugin.Status{}, fmt.Errorf("id is required")
	}
	if err := ensureTart(); err != nil {
		return runtimeplugin.Status{}, err
	}
	out, err := exec.Command("tart", "list", "--format", "json").Output()
	if err != nil {
		return runtimeplugin.Status{}, fmt.Errorf("tart list: %w", err)
	}
	var rows []struct {
		Name  string `json:"Name"`
		State string `json:"State"`
	}
	if err := json.Unmarshal(out, &rows); err != nil {
		return runtimeplugin.Status{}, fmt.Errorf("tart list json: %w", err)
	}
	for _, row := range rows {
		if row.Name == id {
			return runtimeplugin.Status{ID: id, State: normalizeState(row.State)}, nil
		}
	}
	return runtimeplugin.Status{ID: id, State: "unknown"}, fmt.Errorf("VM instance not found; run cage vm create --id <instance>")
}

func (t *Tart) Delete(spec runtimeplugin.Spec) error {
	id := spec.ID
	if id == "" {
		return fmt.Errorf("id is required")
	}
	if err := ensureTart(); err != nil {
		return err
	}
	st, err := t.Status(id)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return nil
		}
		return err
	}
	if len(spec.OnDestroy) > 0 {
		if st.State != "running" {
			return fmt.Errorf("on-destroy requires a running VM; run cage vm start --id <instance> first")
		}
		if err := runHostScripts(id, spec.Workdir, "on-destroy", spec.OnDestroy); err != nil {
			return err
		}
	}
	if st.State == "running" {
		if err := t.Stop(id); err != nil {
			return err
		}
	}
	return runTart("delete", id)
}

func (t *Tart) Exec(id string, opts runtimeplugin.ExecOpts) error {
	if id == "" {
		return fmt.Errorf("id is required")
	}
	if len(opts.Argv) == 0 {
		return fmt.Errorf("exec argv is required")
	}
	if err := ensureTart(); err != nil {
		return err
	}
	if opts.TTY {
		return tartExec(id, os.Stdin, true, true, opts.Argv...)
	}
	var stdin io.Reader
	if len(opts.Stdin) > 0 {
		stdin = bytes.NewReader(opts.Stdin)
	}
	return tartExec(id, stdin, true, false, opts.Argv...)
}

// Bake clones BaseImage → DerivedID, runs Scripts once, stops. No-op if DerivedID exists
// and Force is false. Uses open guest networking (no ExtraRunArgs) so package installs work.
func (t *Tart) Bake(spec runtimeplugin.BakeSpec) error {
	if spec.DerivedID == "" {
		return fmt.Errorf("bake: derived id is required")
	}
	if spec.BaseImage == "" {
		return fmt.Errorf("bake: base image is required")
	}
	if err := ensureTart(); err != nil {
		return err
	}
	exists, err := t.vmExists(spec.DerivedID)
	if err != nil {
		return err
	}
	if exists && spec.Force {
		progress("bake force: removing incomplete %q", spec.DerivedID)
		_ = t.Stop(spec.DerivedID)
		if err := runTart("delete", spec.DerivedID); err != nil {
			return fmt.Errorf("bake force delete: %w", err)
		}
		exists = false
	}
	if exists {
		progress("bake cache hit %q", spec.DerivedID)
		return nil
	}
	progress("bake miss %q from %s (%d scripts) — this can take a while",
		spec.DerivedID, spec.BaseImage, len(spec.Scripts))
	if err := ensureImage(spec.BaseImage); err != nil {
		return err
	}
	progress("bake clone %s → %s", spec.BaseImage, spec.DerivedID)
	if err := runTart("clone", spec.BaseImage, spec.DerivedID); err != nil {
		return err
	}
	bakeSpec := runtimeplugin.Spec{
		ID:       spec.DerivedID,
		Image:    spec.BaseImage,
		Workdir:  spec.Workdir,
		Graphics: false,
	}
	cleanup := func() {
		_ = t.Stop(spec.DerivedID)
		_ = runTart("delete", spec.DerivedID)
	}
	if err := t.ensureRunning(bakeSpec, false); err != nil {
		cleanup()
		return fmt.Errorf("bake start: %w", err)
	}
	if err := runHostScripts(spec.DerivedID, spec.Workdir, "bake", spec.Scripts); err != nil {
		cleanup()
		return err
	}
	// Persist guest writes before stop (otherwise tart stop can drop unflushed npm installs).
	if err := tartExec(spec.DerivedID, nil, false, false, "sudo", "sh", "-c",
		`sync; mkdir -p /var/lib/cage && touch /var/lib/cage/bake.done && sync`); err != nil {
		cleanup()
		return fmt.Errorf("bake sync: %w", err)
	}
	if err := tartExec(spec.DerivedID, nil, false, false, "test", "-f", "/var/lib/cage/bake.done"); err != nil {
		cleanup()
		return fmt.Errorf("bake: completion marker missing after sync")
	}
	progress("bake stopping %s", spec.DerivedID)
	if err := t.Stop(spec.DerivedID); err != nil {
		cleanup()
		return fmt.Errorf("bake stop: %w", err)
	}
	progress("bake ready %s", spec.DerivedID)
	return nil
}

func (t *Tart) vmExists(id string) (bool, error) {
	st, err := t.Status(id)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return false, nil
		}
		return false, err
	}
	return st.State != "unknown", nil
}

// dirArgs builds tart run --dir flags (top-level and nested overlay shares).
func dirArgs(mounts []runtimeplugin.PathSpec) []string {
	var out []string
	for _, m := range mounts {
		if m.Host == "" {
			continue
		}
		name := shareName(m.Guest)
		arg := name + ":" + m.Host
		if m.Permission == "ro" {
			arg += ":ro"
		}
		out = append(out, "--dir="+arg)
	}
	return out
}

// shareName is a stable virtiofs share id from the guest path.
// Readable prefix + short path hash so a.b and a/b cannot collide.
func shareName(guest string) string {
	cleaned := guestClean(guest)
	c := strings.Trim(cleaned, "/")
	prefix := "share"
	if c != "" && c != "." {
		var b strings.Builder
		for _, r := range c {
			switch {
			case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
				b.WriteRune(r)
			default:
				b.WriteByte('_')
			}
		}
		if s := b.String(); s != "" {
			prefix = s
		}
	}
	sum := sha256.Sum256([]byte(cleaned))
	return fmt.Sprintf("%s_%x", prefix, sum[:4])
}

// partitionMounts splits mounts into top-level shares vs nested paths.
// Nested ro/rw → own --dir + bind over the guest path (host honored; no rm).
// Nested rw punches through an ro parent; nested ro is remount,bind,ro after bind.
func partitionMounts(mounts []runtimeplugin.PathSpec) (top, nestedRO, nestedRW []runtimeplugin.PathSpec, err error) {
	for i, m := range mounts {
		if m.Guest == "" {
			continue
		}
		parent := nestParent(mounts, i)
		if parent == "" {
			top = append(top, m)
			continue
		}
		if m.Permission == "ro" {
			nestedRO = append(nestedRO, m)
		} else {
			nestedRW = append(nestedRW, m)
		}
	}
	return top, nestedRO, nestedRW, nil
}

func nestParent(mounts []runtimeplugin.PathSpec, idx int) string {
	child := guestClean(mounts[idx].Guest)
	var best string
	for i, m := range mounts {
		if i == idx || m.Guest == "" {
			continue
		}
		p := guestClean(m.Guest)
		if !guestUnder(child, p) {
			continue
		}
		if best == "" || len(p) > len(best) {
			best = p
		}
	}
	return best
}

func guestClean(p string) string {
	p = filepath.ToSlash(p)
	if p == "" {
		return "/"
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return filepath.Clean(p)
}

// guestUnder reports whether child is strictly under parent (not equal).
func guestUnder(child, parent string) bool {
	if parent == "/" {
		return child != "/"
	}
	return strings.HasPrefix(child, parent+"/")
}

func ensureImage(image string) error {
	// Local clone names exist as VMs; remote OCI refs need pull.
	if !strings.Contains(image, "/") && !strings.Contains(image, ":") {
		return nil
	}
	out, err := exec.Command("tart", "list", "--format", "json").Output()
	if err == nil {
		var rows []struct {
			Name string `json:"Name"`
		}
		if json.Unmarshal(out, &rows) == nil {
			for _, row := range rows {
				if row.Name == image {
					return nil
				}
			}
		}
	}
	return runTart("pull", image)
}

func checkMountHosts(mounts []runtimeplugin.PathSpec) error {
	var missing []string
	for _, m := range mounts {
		if m.Host == "" {
			continue
		}
		if _, err := os.Stat(m.Host); err != nil {
			missing = append(missing, m.Host)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("mount host path(s) missing:\n  %s", strings.Join(missing, "\n  "))
}

func waitReady(id string, runDied <-chan error) error {
	deadline := time.Now().Add(readyTimeout)
	var last error
	started := time.Now()
	for time.Now().Before(deadline) {
		if runDied != nil {
			select {
			case err := <-runDied:
				if err != nil {
					return fmt.Errorf("tart run failed: %w", err)
				}
				return fmt.Errorf("tart run exited before guest was ready")
			default:
			}
		}
		err := tartExec(id, nil, false, false, "true")
		if err == nil {
			progress("guest ready (%s)", time.Since(started).Round(time.Second))
			return nil
		}
		last = err
		time.Sleep(readyPollEvery)
	}
	if last == nil {
		last = fmt.Errorf("timeout")
	}
	return fmt.Errorf("VM instance not ready after %s: %w", readyTimeout, last)
}

func mountVirtiofs(id, shareRoot string) error {
	script := fmt.Sprintf(
		`mkdir -p %q && (mountpoint -q %q || sudo mount -t virtiofs %s %q)`,
		shareRoot, shareRoot, virtiofsTag, shareRoot,
	)
	return tartExec(id, nil, false, false, "sh", "-c", script)
}

// linkMounts symlinks top-level virtiofs shares into guest paths.
// Must not be used for nested paths (see bindNested).
func linkMounts(id, shareRoot string, mounts []runtimeplugin.PathSpec) error {
	for _, m := range mounts {
		if m.Guest == "" {
			continue
		}
		name := shareName(m.Guest)
		src := filepath.ToSlash(filepath.Join(shareRoot, name))
		script := fmt.Sprintf(
			`sudo mkdir -p %q && sudo rm -rf %q && sudo ln -s %q %q`,
			filepath.Dir(m.Guest), m.Guest, src, m.Guest,
		)
		if err := tartExec(id, nil, false, false, "sh", "-c", script); err != nil {
			return fmt.Errorf("link mount %s -> %s: %w", src, m.Guest, err)
		}
	}
	return nil
}

// bindNested overlays a nested guest path with its own virtiofs share (no rm).
// Host is honored via a separate --dir. remountRO applies remount,bind,ro after bind
// (plain remount,ro would EROFS the parent share).
func bindNested(id, shareRoot string, mounts []runtimeplugin.PathSpec, remountRO bool) error {
	for _, m := range mounts {
		guest := guestClean(m.Guest)
		src := filepath.ToSlash(filepath.Join(shareRoot, shareName(m.Guest)))
		script := fmt.Sprintf(
			`set -e
test -d %q
if [ ! -e %q ]; then
  sudo mkdir -p %q
fi
if ! mountpoint -q %q; then
  sudo mount --bind %q %q
fi`,
			src, guest, guest, guest, src, guest,
		)
		if remountRO {
			script += fmt.Sprintf("\nsudo mount -o remount,bind,ro %q", guest)
		}
		if err := tartExec(id, nil, false, false, "sh", "-c", script); err != nil {
			return fmt.Errorf("bind nested %s <- %s: %w", guest, src, err)
		}
	}
	return nil
}

// applyDenyMasks obscures guest paths that match fs.deny under an allowed parent mount.
// Directories get a mode-0 tmpfs; files get an empty ro bind (source outside virtiofs).
func applyDenyMasks(id, shareRoot string, guests []string) error {
	_ = shareRoot
	const emptyDir = "/var/tmp/cage-deny"
	const emptyFile = "/var/tmp/cage-deny/empty"
	prep := fmt.Sprintf(
		`sudo mkdir -p %q && sudo tee %q </dev/null >/dev/null && sudo chmod 644 %q`,
		emptyDir, emptyFile, emptyFile,
	)
	if err := tartExec(id, nil, false, false, "sh", "-c", prep); err != nil {
		return fmt.Errorf("deny mask prep: %w", err)
	}
	for _, g := range guests {
		guest := guestClean(g)
		if guest == "/" || guest == "" {
			continue
		}
		script := fmt.Sprintf(
			`set -e
g=%q
if [ ! -e "$g" ]; then
  exit 0
fi
if mountpoint -q "$g"; then
  exit 0
fi
if [ -d "$g" ]; then
  sudo mount -t tmpfs -o size=1m,mode=000,uid=0,gid=0 cage-deny "$g"
  exit 0
fi
if [ -f "$g" ] || [ -L "$g" ]; then
  sudo mount --bind %q "$g"
  sudo mount -o remount,bind,ro "$g"
fi`,
			guest, emptyFile,
		)
		if err := tartExec(id, nil, false, false, "sh", "-c", script); err != nil {
			return fmt.Errorf("deny mask %s: %w", guest, err)
		}
	}
	return nil
}

func applyCopies(id string, copies []runtimeplugin.PathSpec) error {
	for _, c := range copies {
		if c.Host == "" || c.Guest == "" {
			return fmt.Errorf("copy requires host and guest paths")
		}
		f, err := os.Open(c.Host)
		if err != nil {
			return fmt.Errorf("copy open %s: %w", c.Host, err)
		}
		parent := filepath.Dir(c.Guest)
		if err := tartExec(id, nil, false, false, "sudo", "mkdir", "-p", parent); err != nil {
			f.Close()
			return fmt.Errorf("copy mkdir %s: %w", parent, err)
		}
		// Write via sudo tee so guest paths under /workspace work.
		err = tartExec(id, f, false, false, "sudo", "tee", c.Guest)
		f.Close()
		if err != nil {
			return fmt.Errorf("copy %s -> %s: %w", c.Host, c.Guest, err)
		}
		mode := copyMode(c.Permission)
		if err := tartExec(id, nil, false, false, "sudo", "chmod", mode, c.Guest); err != nil {
			return fmt.Errorf("copy chmod %s: %w", c.Guest, err)
		}
		// Default tart Linux images use admin; fall back to root:root if missing.
		if err := tartExec(id, nil, false, false, "sudo", "sh", "-c",
			fmt.Sprintf(`if id -u admin >/dev/null 2>&1; then chown admin:admin %q; else chown root:root %q; fi`, c.Guest, c.Guest)); err != nil {
			return fmt.Errorf("copy chown %s: %w", c.Guest, err)
		}
	}
	return nil
}

func copyMode(permission string) string {
	if permission == "ro" {
		return "0444"
	}
	return "0644"
}

const onCreateMarker = "/var/lib/cage/on-create.done"

func runOnCreate(spec runtimeplugin.Spec) error {
	if len(spec.OnCreate) == 0 {
		return nil
	}
	err := tartExec(spec.ID, nil, false, false, "test", "-f", onCreateMarker)
	if err == nil {
		progress("on-create already done")
		return nil
	}
	if err := runHostScripts(spec.ID, spec.Workdir, "on-create", spec.OnCreate); err != nil {
		return err
	}
	mark := fmt.Sprintf(`sudo mkdir -p /var/lib/cage && sudo touch %q`, onCreateMarker)
	return tartExec(spec.ID, nil, false, false, "sh", "-c", mark)
}

func runHostScripts(id, workdir, label string, scripts []string) error {
	if len(scripts) == 0 {
		return nil
	}
	if workdir == "" {
		workdir = "/workspace"
	}
	guestDir := "/tmp/cage-lifecycle"
	if err := tartExec(id, nil, false, false, "sudo", "sh", "-c",
		fmt.Sprintf(`mkdir -p %q && chmod 1777 %q`, guestDir, guestDir)); err != nil {
		return fmt.Errorf("%s: mkdir %s: %w", label, guestDir, err)
	}
	for i, path := range scripts {
		base := filepath.Base(path)
		guest := fmt.Sprintf("%s/%s-%d-%s", guestDir, label, i, base)
		hostLog, err := os.CreateTemp("", fmt.Sprintf("cage-%s-%s-*.log", label, base))
		if err != nil {
			return fmt.Errorf("%s host log: %w", label, err)
		}
		hostLogPath := hostLog.Name()
		progress("%s %s", label, base)
		progress("%s streaming output (also %s)", label, hostLogPath)

		f, err := os.Open(path)
		if err != nil {
			hostLog.Close()
			return fmt.Errorf("%s %s: %w", label, path, err)
		}
		err = tartExec(id, f, false, false, "sudo", "tee", guest)
		f.Close()
		if err != nil {
			hostLog.Close()
			return fmt.Errorf("%s copy %s: %w", label, path, err)
		}
		if err := tartExec(id, nil, false, false, "sudo", "chmod", "0755", guest); err != nil {
			hostLog.Close()
			return fmt.Errorf("%s chmod %s: %w", label, guest, err)
		}

		// Prefer line-buffered stdio so apt progress shows in IDE terminals.
		run := fmt.Sprintf(`cd %q 2>/dev/null || cd /; if command -v stdbuf >/dev/null 2>&1; then exec stdbuf -oL -eL sh %q; else exec sh %q; fi`, workdir, guest, guest)
		progress("%s ── begin (live; also %s) ──", label, hostLogPath)
		err = tartExecLogged(id, hostLog, "sh", "-c", run)
		hostLog.Close()
		if err != nil {
			progress("%s failed — log %s", label, hostLogPath)
			return fmt.Errorf("%s %s: %w", label, path, err)
		}
		progress("%s ── done ── (log %s)", label, hostLogPath)
	}
	return nil
}

func tartExecLogged(id string, hostLog *os.File, args ...string) error {
	cmdArgs := append([]string{"exec", id}, args...)
	cmd := exec.Command("tart", cmdArgs...)
	// go-plugin only SyncStderr to the host CLI — guest stream goes there with a green prefix.
	w := termlog.GuestWriter(hostLog)
	cmd.Stdout = w
	cmd.Stderr = w
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("tart exec: %w", err)
	}
	return nil
}

func tartExec(id string, stdin io.Reader, stream, tty bool, args ...string) error {
	cmdArgs := []string{"exec"}
	if stdin != nil {
		cmdArgs = append(cmdArgs, "-i")
	}
	if tty {
		cmdArgs = append(cmdArgs, "-t")
	}
	cmdArgs = append(cmdArgs, id)
	cmdArgs = append(cmdArgs, args...)
	cmd := exec.Command("tart", cmdArgs...)
	cmd.Stdin = stdin
	var stderr bytes.Buffer
	if stream {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	} else {
		cmd.Stdout = io.Discard
		cmd.Stderr = &stderr
	}
	if err := cmd.Run(); err != nil {
		if stream {
			return fmt.Errorf("tart exec: %w", err)
		}
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return fmt.Errorf("tart exec: %s", msg)
		}
		return fmt.Errorf("tart exec: %w", err)
	}
	return nil
}

func normalizeState(s string) string {
	switch strings.ToLower(s) {
	case "running":
		return "running"
	case "stopped":
		return "stopped"
	default:
		return strings.ToLower(s)
	}
}

func ensureTart() error {
	if _, err := exec.LookPath("tart"); err != nil {
		return fmt.Errorf("tart not found on PATH; install from https://tart.run")
	}
	return nil
}

func imageStampPath(id string) string {
	return filepath.Join(".cage", "run", id, "image")
}

func writeImageStamp(id, image string) error {
	dir := filepath.Join(".cage", "run", id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(imageStampPath(id), []byte(image+"\n"), 0o644)
}

func readImageStamp(id string) (string, error) {
	b, err := os.ReadFile(imageStampPath(id))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

func runTart(args ...string) error {
	cmd := exec.Command("tart", args...)
	var stderr bytes.Buffer
	cmd.Stdout = os.Stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return fmt.Errorf("tart %s: %s", strings.Join(args, " "), msg)
		}
		return fmt.Errorf("tart %s: %w", strings.Join(args, " "), err)
	}
	return nil
}
