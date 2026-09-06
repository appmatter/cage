package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/appmatter/cage/internal/contextapi"
)

const serveTestConfig = `
version: 1
runtime:
  plugins:
    tart:
      priority: 1
      image: ubuntu
    incus:
      priority: 2
      image: ubuntu/24.04
    hyperv:
      priority: 3
      image: Ubuntu
  workdir: /workspace
fs:
  layout:
    mode: flat
`

func writeServeProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".cage"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".cage", "cage.yaml"), []byte(serveTestConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// Serve writes addr, token, pid, and allowed hosts to serve.json and answers
// GET /v1/configs on the bound port with that token.
func TestStartContextServeWritesStateAndURL(t *testing.T) {
	root := writeServeProject(t)
	var out bytes.Buffer
	srv, ln, err := startContextServe(&out, root, nil, "", "127.0.0.1:0", []string{"127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	defer ln.Close()

	var state contextapi.ServeState
	b, err := os.ReadFile(contextapi.ServeStatePath(root))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &state); err != nil {
		t.Fatal(err)
	}
	if state.Addr != ln.Addr().String() || state.Token == "" || state.PID != os.Getpid() || len(state.AllowedHosts) != 1 || state.AllowedHosts[0] != "127.0.0.1" {
		t.Fatalf("%+v bound=%s", state, ln.Addr())
	}

	u, err := url.Parse(strings.TrimSpace(out.String()))
	if err != nil {
		t.Fatal(err)
	}
	if u.Host != state.Addr || u.Query().Get("token") != state.Token {
		t.Fatalf("url=%q state=%+v", out.String(), state)
	}

	go srv.Serve(ln)
	client := &http.Client{Timeout: 2 * time.Second}
	base := "http://" + state.Addr
	req, err := http.NewRequest(http.MethodGet, base+"/v1/configs", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+state.Token)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(body), `"default"`) {
		t.Fatal(resp.StatusCode, string(body))
	}
	resp, err = client.Get(base + "/v1/configs")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatal(resp.StatusCode)
	}
}

// --token is stored as-is instead of generating one.
func TestStartContextServeHonorsToken(t *testing.T) {
	root := writeServeProject(t)
	var out bytes.Buffer
	srv, ln, err := startContextServe(&out, root, nil, "fixed-token", "127.0.0.1:0", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	defer ln.Close()

	var state contextapi.ServeState
	b, err := os.ReadFile(contextapi.ServeStatePath(root))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &state); err != nil {
		t.Fatal(err)
	}
	if state.Token != "fixed-token" {
		t.Fatalf("token=%q", state.Token)
	}
}

// Listen still rejects wildcard / LAN binds.
func TestStartContextServeRejectsNonLoopback(t *testing.T) {
	root := writeServeProject(t)
	_, _, err := startContextServe(&bytes.Buffer{}, root, nil, "t", "0.0.0.0:0", nil)
	if err == nil {
		t.Fatal("accepted non-loopback")
	}
}
