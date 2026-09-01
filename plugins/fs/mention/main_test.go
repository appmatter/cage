package main

import (
	"os"
	"path/filepath"
	"testing"

	fsplugin "github.com/appmatter/cage/pkg/plugin/v1/fs"
)

func TestSuggestAndResolveExplicitFile(t *testing.T) {
	root := t.TempDir()
	writeMentionFile(t, root, "src/main.go", "package main\n")
	writeMentionFile(t, root, "README.md", "read me")
	writeMentionFile(t, root, ".env", "secret")

	m := &Mention{}
	if err := m.Configure(fsplugin.Context{
		ProjectRoot: root,
		SeatYAML:    []byte("include: ['**/*']\nexclude: ['**/.env', '**/.env.*']\n"),
	}); err != nil {
		t.Fatal(err)
	}
	hits, err := m.Suggest("main", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Path != "src/main.go" || hits[0].Kind != "file" || hits[0].VisibleToGuest {
		t.Fatalf("hits %#v", hits)
	}
	rootHits, err := m.Suggest("readme", 0)
	if err != nil || len(rootHits) != 1 || rootHits[0].Path != "README.md" {
		t.Fatalf("root hits %#v, err %v", rootHits, err)
	}
	resolved, err := m.Resolve(hits)
	if err != nil {
		t.Fatal(err)
	}
	if resolved[0].Content != "package main\n" {
		t.Fatalf("resolved %#v", resolved)
	}
	if _, err := m.Resolve([]mention{{Path: ".env", Kind: "file"}}); err == nil {
		t.Fatal("resolved excluded file")
	}
}

func TestGuestTranslationAndDirectoryListing(t *testing.T) {
	root := t.TempDir()
	writeMentionFile(t, root, "src/main.go", "package main")
	writeMentionFile(t, root, "src/internal/app.go", "package internal")

	m := &Mention{}
	if err := m.Configure(fsplugin.Context{
		ProjectRoot: root,
		Mounts:      []fsplugin.Path{{Host: filepath.Join(root, "src"), Path: "src", Guest: "/workspace/src"}},
	}); err != nil {
		t.Fatal(err)
	}
	hits, err := m.Suggest("src", 0)
	if err != nil {
		t.Fatal(err)
	}
	var dir mention
	for _, hit := range hits {
		if hit.Kind == "directory" && hit.Path == "src/internal/" {
			dir = hit
		}
		if hit.Path == "src/main.go" && (!hit.VisibleToGuest || hit.GuestPath != "/workspace/src/main.go") {
			t.Fatalf("guest mapping %#v", hit)
		}
	}
	if dir.Path == "" {
		t.Fatalf("directory missing from %#v", hits)
	}
	resolved, err := m.Resolve([]mention{dir})
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved[0].DirectoryEntries) != 0 {
		t.Fatalf("guest directory should not snapshot: %#v", resolved)
	}
}

func writeMentionFile(t *testing.T, root, rel, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
