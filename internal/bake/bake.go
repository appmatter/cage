package bake

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"

	runtimeplugin "github.com/appmatter/cage/pkg/plugin/v1/runtime"
)

// SchemaVersion is included in the content hash; bump when bake semantics change.
const SchemaVersion = "1"

// DerivedName returns the backend-local image id for a full hash.
func DerivedName(hash string) string {
	short := hash
	if len(short) > 16 {
		short = short[:16]
	}
	return "cage-bake-" + short
}

// HashInputs are the content-addressed bake recipe.
type HashInputs struct {
	BaseImage string
	Backend   string // host runtime plugin id (tart, incus, …)
	Scripts   []string
	GuestGOOS string
	GuestArch string // empty = host GOARCH
}

// Hash returns a hex sha256 of bake inputs.
func Hash(in HashInputs) (string, error) {
	if in.BaseImage == "" {
		return "", fmt.Errorf("bake: base image is required")
	}
	arch := in.GuestArch
	if arch == "" {
		arch = goruntime.GOARCH
	}
	goos := in.GuestGOOS
	if goos == "" {
		goos = "linux"
	}

	h := sha256.New()
	write := func(s string) {
		_, _ = h.Write([]byte(s))
		_, _ = h.Write([]byte{0})
	}
	write(SchemaVersion)
	write(in.BaseImage)
	write(in.Backend)
	write(goos)
	write(arch)
	for _, p := range in.Scripts {
		write("script:" + filepath.ToSlash(p))
		b, err := os.ReadFile(p)
		if err != nil {
			return "", fmt.Errorf("bake script %s: %w", p, err)
		}
		_, _ = h.Write(b)
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// EnsureDerived resolves a derived image for Create. Empty scripts → return baseImage.
// Incomplete caches (VM exists, no host .ok stamp) are force-rebaked.
func EnsureDerived(
	projectRoot string,
	baseImage string,
	scripts []string,
	guestGOOS string,
	backendName string,
	b runtimeplugin.Backend,
	workdir string,
	logf func(string, ...any),
) (image string, hash string, err error) {
	if len(scripts) == 0 {
		return baseImage, "", nil
	}
	hash, err = Hash(HashInputs{
		BaseImage: baseImage,
		Backend:   backendName,
		Scripts:   scripts,
		GuestGOOS: guestGOOS,
	})
	if err != nil {
		return "", "", err
	}
	derived := DerivedName(hash)
	force := !stampOK(projectRoot, hash)
	if logf != nil {
		if force {
			logf("bake ensure %s (base=%s backend=%s scripts=%d) — no completion stamp, will rebuild if needed",
				derived, baseImage, backendName, len(scripts))
		} else {
			logf("bake ensure %s (base=%s scripts=%d)", derived, baseImage, len(scripts))
		}
	}
	if err := b.Bake(runtimeplugin.BakeSpec{
		BaseImage: baseImage,
		DerivedID: derived,
		Scripts:   scripts,
		Workdir:   workdir,
		Force:     force,
	}); err != nil {
		_ = os.Remove(stampPath(projectRoot, hash))
		return "", hash, err
	}
	if err := writeManifest(projectRoot, hash, derived, baseImage, backendName, scripts); err != nil {
		return "", hash, err
	}
	if err := writeStamp(projectRoot, hash); err != nil {
		return "", hash, err
	}
	return derived, hash, nil
}

// ImagesDir is .cage/.cache/images (bake stamps + attachments).
func ImagesDir(projectRoot string) string {
	if projectRoot == "" {
		projectRoot = "."
	}
	return filepath.Join(projectRoot, ".cage", ".cache", "images")
}

func stampPath(projectRoot, hash string) string {
	return filepath.Join(ImagesDir(projectRoot), hash[:16]+".ok")
}

func stampOK(projectRoot, hash string) bool {
	_, err := os.Stat(stampPath(projectRoot, hash))
	return err == nil
}

func writeStamp(projectRoot, hash string) error {
	dir := ImagesDir(projectRoot)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(stampPath(projectRoot, hash), []byte("ok\n"), 0o644)
}

func writeManifest(projectRoot, hash, derived, base, backend string, scripts []string) error {
	dir := ImagesDir(projectRoot)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	var body strings.Builder
	body.WriteString("schema: " + SchemaVersion + "\n")
	body.WriteString("hash: " + hash + "\n")
	body.WriteString("image: " + derived + "\n")
	body.WriteString("base: " + base + "\n")
	body.WriteString("backend: " + backend + "\n")
	body.WriteString("scripts:\n")
	for _, s := range scripts {
		body.WriteString("  - " + s + "\n")
	}
	path := filepath.Join(dir, hash[:16]+".txt")
	return os.WriteFile(path, []byte(body.String()), 0o644)
}

// Entry is one derived bake recorded under .cage/.cache/images/.
type Entry struct {
	Short   string // 16-hex prefix
	Hash    string
	Image   string // cage-bake-…
	Base    string
	Backend string
	OK      bool // host .ok stamp present
}

// List returns bake manifests under projectRoot/.cage/.cache/images/.
func List(projectRoot string) ([]Entry, error) {
	dir := ImagesDir(projectRoot)
	ents, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []Entry
	for _, e := range ents {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".txt") {
			continue
		}
		short := strings.TrimSuffix(name, ".txt")
		if len(short) < 8 {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		ent := parseManifest(short, string(b))
		_, err = os.Stat(filepath.Join(dir, short+".ok"))
		ent.OK = err == nil
		out = append(out, ent)
	}
	return out, nil
}

func parseManifest(short, body string) Entry {
	ent := Entry{Short: short, Image: DerivedName(short)}
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "hash: "):
			ent.Hash = strings.TrimPrefix(line, "hash: ")
		case strings.HasPrefix(line, "image: "):
			ent.Image = strings.TrimPrefix(line, "image: ")
		case strings.HasPrefix(line, "base: "):
			ent.Base = strings.TrimPrefix(line, "base: ")
		case strings.HasPrefix(line, "backend: "):
			ent.Backend = strings.TrimPrefix(line, "backend: ")
		}
	}
	return ent
}

// ResolveEntry finds a bake by short hash, full hash, or image name (cage-bake-…).
func ResolveEntry(projectRoot, ref string) (Entry, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return Entry{}, fmt.Errorf("bake: empty id")
	}
	ents, err := List(projectRoot)
	if err != nil {
		return Entry{}, err
	}
	for _, e := range ents {
		if e.Short == ref || e.Hash == ref || e.Image == ref ||
			strings.HasPrefix(e.Hash, ref) || strings.TrimPrefix(e.Image, "cage-bake-") == ref {
			return e, nil
		}
	}
	// Allow deleting by image name even if manifest is gone.
	if strings.HasPrefix(ref, "cage-bake-") {
		short := strings.TrimPrefix(ref, "cage-bake-")
		return Entry{Short: short, Image: ref}, nil
	}
	if len(ref) >= 8 && len(ref) <= 64 && isHex(ref) {
		return Entry{Short: trimShort(ref), Hash: ref, Image: DerivedName(ref)}, nil
	}
	return Entry{}, fmt.Errorf("bake %q not found (cage bake list)", ref)
}

func isHex(s string) bool {
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}

func trimShort(hash string) string {
	if len(hash) > 16 {
		return hash[:16]
	}
	return hash
}

// RemoveHostFiles deletes .cage/.cache/images/<short>.txt and .ok for the entry.
func RemoveHostFiles(projectRoot string, e Entry) error {
	dir := ImagesDir(projectRoot)
	short := e.Short
	if short == "" && e.Hash != "" {
		short = trimShort(e.Hash)
	}
	if short == "" {
		return fmt.Errorf("bake: cannot remove host files without hash")
	}
	_ = os.Remove(filepath.Join(dir, short+".txt"))
	_ = os.Remove(filepath.Join(dir, short+".ok"))
	return nil
}

// RemoveDerived deletes the backend image (via Backend.Delete) and host hash stamps.
func RemoveDerived(projectRoot string, e Entry, b runtimeplugin.Backend) error {
	if e.Image != "" && b != nil {
		_ = b.Stop(e.Image) // ignore not-running
		if err := b.Delete(runtimeplugin.Spec{ID: e.Image}); err != nil {
			// still scrub host files
			_ = RemoveHostFiles(projectRoot, e)
			return fmt.Errorf("delete image %s: %w", e.Image, err)
		}
	}
	return RemoveHostFiles(projectRoot, e)
}

