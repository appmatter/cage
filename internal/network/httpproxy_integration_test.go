//go:build integration

package network

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/appmatter/cage/internal/pluginhost"
)

// TestHTTPProxyHostAuthIntegration runs an authenticated API on the host, terminates
// through http-proxy (Bearer inject), and gates method/path with the egress plugin.
// No VM / softnet — pure host stack.
func TestHTTPProxyHostAuthIntegration(t *testing.T) {
	const secret = "test-host-secret"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+secret {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.URL.Path != "/v1/secure" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer upstream.Close()
	upPort := upstream.Listener.Addr().(*net.TCPAddr).Port

	root := findRepoRoot(t)
	dir := t.TempDir()
	httpBin := filepath.Join(dir, "cage-network-http-proxy.cageplugin")
	egressBin := filepath.Join(dir, "cage-network-egress.cageplugin")
	buildPlugin(t, root, "./plugins/network/http-proxy", httpBin)
	buildPlugin(t, root, "./plugins/network/egress", egressBin)

	t.Setenv("CAGE_IT_API_TOKEN", secret)

	httpClient, err := pluginhost.DispenseNetworkTerminate(httpBin)
	if err != nil {
		t.Fatal(err)
	}
	defer httpClient.Close()
	httpYAML := fmt.Sprintf(`
api:
  url: %s
  headers:
    Authorization: "Bearer {{ env.CAGE_IT_API_TOKEN }}"
`, upstream.URL)
	if err := httpClient.Terminate.Configure([]byte(httpYAML)); err != nil {
		t.Fatal(err)
	}

	egressClient, err := pluginhost.DispenseNetworkFilter(egressBin)
	if err != nil {
		t.Fatal(err)
	}
	defer egressClient.Close()
	egressYAML := fmt.Sprintf(`
deny_response:
  http: true
allow:
  - host: 127.0.0.1
    port: %d
    method: POST
    path: /v1/secure
`, upPort)
	if err := egressClient.Filter.Configure([]byte(egressYAML)); err != nil {
		t.Fatal(err)
	}

	var logs []TrafficEvent
	allow := &SourceAllowlist{}
	allow.SetStrings([]string{"127.0.0.1"})
	ht := &HTTPTerminate{
		Terminate: httpClient.Terminate,
		BindHost:  "127.0.0.1",
		Allow:     allow,
		Pipeline: NewPipeline([]FilterSeat{{
			Priority: 1,
			Filter:   egressClient.Filter,
		}}),
		OnTraffic: &captureTraffic{&logs},
		DenyHTTP:  true,
		Client:    upstream.Client(),
	}
	if err := ht.Start([]HTTPEndpointListen{{Name: "api", Listen: 0}}); err != nil {
		t.Fatal(err)
	}
	defer ht.Close()
	proxyURL := "http://127.0.0.1:" + strconv.Itoa(ht.Ports()["api"])

	// Direct upstream without auth → 401 (proves mock requires Bearer).
	resp, err := http.Post(upstream.URL+"/v1/secure", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("direct want 401 got %d", resp.StatusCode)
	}

	// GET via terminate → egress DENY (method).
	resp, err = http.Get(proxyURL + "/v1/secure")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("GET want 403 got %d body=%q", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), "intentionally blocked") {
		t.Fatalf("deny body=%q", body)
	}

	// POST via terminate → inject auth → upstream 200.
	resp, err = http.Post(proxyURL+"/v1/secure", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || string(body) != `{"ok":true}` {
		t.Fatalf("POST want 200 ok got %d %q", resp.StatusCode, body)
	}
}

func buildPlugin(t *testing.T, root, pkg, out string) {
	t.Helper()
	cmd := exec.Command("go", "build", "-o", out, pkg)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build %s: %v\n%s", pkg, err, b)
	}
}

func findRepoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Clean(filepath.Join(wd, "../.."))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("repo root from %s: %v", wd, err)
	}
	return root
}
