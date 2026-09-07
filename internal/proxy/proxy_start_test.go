package proxy

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func init() {
	if len(os.Args) > 1 && os.Args[1] == "proxy-serve" {
		fakeProxyServe()
		os.Exit(0)
	}
}

func fakeProxyServe() {
	project, id, ready := "", "", ""
	for i, a := range os.Args {
		if i+1 >= len(os.Args) {
			break
		}
		switch a {
		case "--project":
			project = os.Args[i+1]
		case "--id":
			id = os.Args[i+1]
		case "--ready":
			ready = os.Args[i+1]
		}
	}
	if report := os.Getenv("CAGE_TEST_PROXY_REPORT"); report != "" {
		_ = os.WriteFile(filepath.Join(report, "argv"), []byte(strings.Join(os.Args[1:], "\n")+"\n"), 0o644)
	}
	if project != "" && id != "" {
		resolved := ResolvedYAMLPath(project, id)
		if fi, err := os.Stat(resolved); err == nil {
			if report := os.Getenv("CAGE_TEST_PROXY_REPORT"); report != "" {
				_ = os.WriteFile(filepath.Join(report, "mode"), []byte(fmt.Sprintf("%04o", fi.Mode().Perm())), 0o644)
				body, _ := os.ReadFile(resolved)
				_ = os.WriteFile(filepath.Join(report, "resolved"), body, 0o644)
			}
			_ = os.Remove(resolved)
		}
	}
	if project != "" && id != "" {
		_ = WriteState(project, id, State{PID: os.Getpid(), Port: 1, HTTPPort: 2})
	}
	if ready != "" {
		_ = os.WriteFile(ready, []byte("ok\n"), 0o644)
	}
}

func TestStartKeepsResolvedSecretsOffArgvAndTmp(t *testing.T) {
	root := t.TempDir()
	tmp := t.TempDir()
	report := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	t.Setenv("CAGE_TEST_PROXY_REPORT", report)

	const secret = "super-secret-token-xyz"
	template := []byte("endpoints:\n  api:\n    url: https://example.com\n")
	resolved := []byte("endpoints:\n  api:\n    url: https://example.com\n    token: " + secret + "\n")
	id := "vm-secrets"

	beforeTMP := tmpNames(t, tmp)
	st, err := Start(root, id, os.Args[0], StartOptions{
		HTTPProxyYAML:         template,
		HTTPProxyResolvedYAML: resolved,
	})
	if err != nil {
		t.Fatal(err)
	}
	if st.PID == 0 {
		t.Fatal("expected child pid")
	}

	resolvedPath := ResolvedYAMLPath(root, id)
	if _, err := os.Stat(resolvedPath); !os.IsNotExist(err) {
		t.Fatalf("resolved file should be gone after child read, err=%v", err)
	}

	mode, err := os.ReadFile(filepath.Join(report, "mode"))
	if err != nil {
		t.Fatal(err)
	}
	if string(mode) != "0600" {
		t.Fatalf("resolved mode %q, want 0600", mode)
	}
	gotResolved, err := os.ReadFile(filepath.Join(report, "resolved"))
	if err != nil {
		t.Fatal(err)
	}
	if string(gotResolved) != string(resolved) {
		t.Fatalf("child read %q, want %q", gotResolved, resolved)
	}

	argv, err := os.ReadFile(filepath.Join(report, "argv"))
	if err != nil {
		t.Fatal(err)
	}
	argvText := string(argv)
	for _, forbid := range []string{
		"--http-proxy-resolved",
		resolvedPath,
		secret,
		string(resolved),
	} {
		if strings.Contains(argvText, forbid) {
			t.Fatalf("argv must not contain %q:\n%s", forbid, argvText)
		}
	}
	if !strings.Contains(argvText, "--http-proxy") {
		t.Fatalf("argv should keep --http-proxy:\n%s", argvText)
	}
	if !strings.Contains(argvText, httpProxyConfigPath(root, id)) {
		t.Fatalf("argv should pass template path:\n%s", argvText)
	}

	afterTMP := tmpNames(t, tmp)
	if extra := difference(afterTMP, beforeTMP); len(extra) > 0 {
		t.Fatalf("process temp dir gained files %v", extra)
	}

	tmpl, err := os.ReadFile(httpProxyConfigPath(root, id))
	if err != nil {
		t.Fatal(err)
	}
	if string(tmpl) != string(template) {
		t.Fatalf("template file %q, want %q", tmpl, template)
	}

	_ = Stop(root, id)
}

func TestStartRemovesResolvedFileOnFailure(t *testing.T) {
	root := t.TempDir()
	id := "vm-fail"
	_, err := Start(root, id, filepath.Join(root, "missing-cage"), StartOptions{
		HTTPProxyYAML:         []byte("endpoints: {}\n"),
		HTTPProxyResolvedYAML: []byte("token: super-secret-token-xyz\n"),
	})
	if err == nil {
		t.Fatal("expected start failure")
	}
	if _, err := os.Stat(ResolvedYAMLPath(root, id)); !os.IsNotExist(err) {
		t.Fatalf("resolved file should be removed on Start failure, err=%v", err)
	}
}

func tmpNames(t *testing.T, dir string) map[string]struct{} {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]struct{}{}
	for _, e := range ents {
		out[e.Name()] = struct{}{}
	}
	return out
}

func difference(after, before map[string]struct{}) []string {
	var extra []string
	for name := range after {
		if _, ok := before[name]; !ok {
			extra = append(extra, name)
		}
	}
	return extra
}

func TestStartWritesOwnerOnlyResolvedFile(t *testing.T) {
	// Direct write contract: 0600 from O_CREATE|O_TRUNC, under the run dir.
	root := t.TempDir()
	id := "vm-mode"
	dir := runDir(root, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := ResolvedYAMLPath(root, id)
	if err := writeOwnerOnlyFile(path, []byte("secret: 1\n")); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %04o, want 0600", fi.Mode().Perm())
	}
	if !strings.HasPrefix(path, filepath.Join(root, ".cage", "run", id)+string(os.PathSeparator)) {
		t.Fatalf("resolved path %q is not under run dir", path)
	}
}
