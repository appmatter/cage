package bake

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHashStableAndChangesWithContent(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.sh")
	b := filepath.Join(dir, "b.sh")
	if err := os.WriteFile(a, []byte("echo a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("echo b\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	h1, err := Hash(HashInputs{BaseImage: "ubuntu", Backend: "tart", Scripts: []string{a}, GuestGOOS: "linux", GuestArch: "arm64"})
	if err != nil {
		t.Fatal(err)
	}
	h2, err := Hash(HashInputs{BaseImage: "ubuntu", Backend: "tart", Scripts: []string{a}, GuestGOOS: "linux", GuestArch: "arm64"})
	if err != nil {
		t.Fatal(err)
	}
	if h1 != h2 {
		t.Fatalf("unstable hash")
	}
	h3, err := Hash(HashInputs{BaseImage: "ubuntu", Backend: "tart", Scripts: []string{b}, GuestGOOS: "linux", GuestArch: "arm64"})
	if err != nil {
		t.Fatal(err)
	}
	if h1 == h3 {
		t.Fatalf("content change should change hash")
	}
	h4, err := Hash(HashInputs{BaseImage: "other", Backend: "tart", Scripts: []string{a}, GuestGOOS: "linux", GuestArch: "arm64"})
	if err != nil {
		t.Fatal(err)
	}
	if h1 == h4 {
		t.Fatalf("base change should change hash")
	}
	h5, err := Hash(HashInputs{BaseImage: "ubuntu", Backend: "incus", Scripts: []string{a}, GuestGOOS: "linux", GuestArch: "arm64"})
	if err != nil {
		t.Fatal(err)
	}
	if h1 == h5 {
		t.Fatalf("backend change should change hash")
	}
	name := DerivedName(h1)
	if len(name) < len("cage-bake-")+16 {
		t.Fatalf("derived name %q", name)
	}
}

func TestListResolveRemoveHostFiles(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".cage", ".cache", "images")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	hash := "abcdef0123456789deadbeefcafebabe0123456789abcdef0123456789abcd"
	short := hash[:16]
	body := "schema: 1\nhash: " + hash + "\nimage: cage-bake-" + short + "\nbase: ubuntu\nbackend: tart\nscripts:\n"
	if err := os.WriteFile(filepath.Join(dir, short+".txt"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, short+".ok"), []byte("ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ents, err := List(root)
	if err != nil || len(ents) != 1 {
		t.Fatalf("list=%v err=%v", ents, err)
	}
	if !ents[0].OK || ents[0].Image != "cage-bake-"+short {
		t.Fatalf("%+v", ents[0])
	}
	e, err := ResolveEntry(root, short)
	if err != nil || e.Short != short {
		t.Fatalf("resolve short: %v %+v", err, e)
	}
	e2, err := ResolveEntry(root, "cage-bake-"+short)
	if err != nil || e2.Image != e.Image {
		t.Fatalf("resolve image: %v", err)
	}
	if err := RemoveHostFiles(root, e); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, short+".txt")); !os.IsNotExist(err) {
		t.Fatalf("txt still there")
	}
	if _, err := os.Stat(filepath.Join(dir, short+".ok")); !os.IsNotExist(err) {
		t.Fatalf("ok still there")
	}
}
