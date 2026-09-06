package contextapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	plugin "github.com/appmatter/cage/pkg/plugin/v1/client"
	runtimeplugin "github.com/appmatter/cage/pkg/plugin/v1/runtime"
)

type fakePlugin struct {
	caps            []string
	configured      plugin.Context
	request         plugin.Request
	configureCalls  int
	capabilityCalls int
	response        json.RawMessage
	configErr       error
	err             error
	started         chan struct{}
	release         <-chan struct{}
	startOnce       sync.Once
	mu              sync.Mutex
}

func (p *fakePlugin) Name() string { return "fake" }
func (p *fakePlugin) Capabilities() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.capabilityCalls++
	return append([]string(nil), p.caps...)
}
func (p *fakePlugin) Configure(c plugin.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.configured = c
	p.configureCalls++
	return p.configErr
}
func (p *fakePlugin) Call(r plugin.Request) (plugin.Response, error) {
	p.mu.Lock()
	p.request = r
	p.mu.Unlock()
	if p.started != nil {
		p.startOnce.Do(func() { close(p.started) })
	}
	if p.release != nil {
		<-p.release
	}
	if p.err != nil {
		return plugin.Response{}, p.err
	}
	if p.response != nil {
		return plugin.Response{Payload: p.response}, nil
	}
	return plugin.Response{Payload: json.RawMessage(`{"ok":true}`)}, nil
}

type fakeBackend struct {
	mu     sync.Mutex
	states map[string]string
	spec   runtimeplugin.Spec
}

func (b *fakeBackend) Name() string { return "fake" }
func (b *fakeBackend) Create(spec runtimeplugin.Spec) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.states == nil {
		b.states = map[string]string{}
	}
	b.spec = spec
	b.states[spec.ID] = "created"
	return nil
}
func (b *fakeBackend) Start(spec runtimeplugin.Spec) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.states[spec.ID] = "running"
	return nil
}
func (b *fakeBackend) Stop(id string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.states[id] = "stopped"
	return nil
}
func (b *fakeBackend) Status(id string) (runtimeplugin.Status, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	st, ok := b.states[id]
	if !ok {
		return runtimeplugin.Status{}, errors.New("absent")
	}
	return runtimeplugin.Status{ID: id, State: st}, nil
}
func (b *fakeBackend) Delete(spec runtimeplugin.Spec) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.states, spec.ID)
	return nil
}
func (b *fakeBackend) Exec(string, runtimeplugin.ExecOpts) error { return nil }
func (b *fakeBackend) Bake(runtimeplugin.BakeSpec) error         { return nil }

func testProject(t *testing.T, aliases ...string) *Project {
	t.Helper()
	if len(aliases) == 0 {
		aliases = []string{"default"}
	}
	root := t.TempDir()
	paths := make([]string, 0, len(aliases))
	for _, alias := range aliases {
		name := "cage.yaml"
		if alias != "default" {
			name = "cage." + alias + ".yaml"
		}
		writeProjectConfig(t, root, name, projectRuntime)
		paths = append(paths, filepath.Join(".cage", name))
	}
	project, err := LoadProject(root, paths, runtime.GOOS)
	if err != nil {
		t.Fatal(err)
	}
	return project
}

func testServer(t *testing.T, p *fakePlugin) *Server {
	t.Helper()
	return testServerOpts(t, Options{
		Authorize: func(*http.Request, string, string) bool { return true },
		LoadSeats: func(Entry, VMMeta) *LoadedSeats {
			return &LoadedSeats{Seats: map[string]map[string]Seat{
				"any": {"ordinary": {Context: plugin.Context{Kind: "any", Data: json.RawMessage(`{"safe":true}`)}, Plugin: p}},
			}}
		},
		WithRuntime: func(string, string, func(runtimeplugin.Backend) error) error {
			return errors.New("runtime unused")
		},
	}, "default")
}

func testServerOpts(t *testing.T, opts Options, aliases ...string) *Server {
	t.Helper()
	if opts.Project == nil {
		opts.Project = testProject(t, aliases...)
	}
	s := New(opts)
	t.Cleanup(s.Close)
	return s
}

func callPath(config, vm, context, seat string) string {
	return fmt.Sprintf("/v1/configs/%s/vms/%s/context/%s/plugins/%s/call", config, vm, context, seat)
}

func call(s *Server, method, path, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
	}
	s.Handler().ServeHTTP(w, r)
	return w
}

// Token grants hide configs the bearer is not allowed to see.
func TestListConfigs(t *testing.T) {
	s := testServerOpts(t, Options{
		Authorize: BearerTokens(map[string]TokenGrant{"tok": {Configs: []string{"default"}}}),
	}, "default", "dogfood")
	req := httptest.NewRequest(http.MethodGet, "/v1/configs", nil)
	req.Header.Set("Authorization", "Bearer tok")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"default"`) || strings.Contains(w.Body.String(), `"dogfood"`) {
		t.Fatalf("body=%s", w.Body.String())
	}
}

// Empty AllowedHosts means any Host is accepted (including httptest's example.com).
func TestAllowedHostUnset(t *testing.T) {
	s := testServer(t, &fakePlugin{caps: []string{"do"}})
	w := call(s, http.MethodGet, "/v1/configs", "")
	if w.Code == http.StatusForbidden {
		t.Fatal(w.Code)
	}
}

// When AllowedHosts is set, a mismatched or empty Host is 403; a listed name is allowed.
func TestAllowedHost(t *testing.T) {
	s := testServerOpts(t, Options{
		Authorize:    func(*http.Request, string, string) bool { return true },
		AllowedHosts: []string{"127.0.0.1", "cage.internal"},
	}, "default")
	for _, host := range []string{"evil.com", ""} {
		req := httptest.NewRequest(http.MethodGet, "/v1/configs", nil)
		req.Host = host
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("host %q: %d", host, w.Code)
		}
	}
	req := httptest.NewRequest(http.MethodGet, "/v1/configs", nil)
	req.Host = "127.0.0.1:7411"
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, "/v1/configs", nil)
	req.Host = "cage.internal"
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
}

// Missing or wrong bearer is 401, even when the seat exists.
func TestAuthDeny(t *testing.T) {
	p := &fakePlugin{caps: []string{"do"}}
	s := testServerOpts(t, Options{
		Authorize: BearerTokens(map[string]TokenGrant{"ok": {}}),
		LoadSeats: func(Entry, VMMeta) *LoadedSeats {
			return &LoadedSeats{Seats: map[string]map[string]Seat{"any": {"ordinary": {Plugin: p}}}}
		},
	}, "default")
	w := call(s, http.MethodPost, callPath("default", "default", "any", "ordinary"), `{"operation":"do","payload":null}`)
	if w.Code != 401 {
		t.Fatal(w.Code)
	}
	req := httptest.NewRequest(http.MethodPost, callPath("default", "default", "any", "ordinary"), strings.NewReader(`{"operation":"do","payload":null}`))
	req.Header.Set("Authorization", "Bearer bad")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Fatal(rec.Code)
	}
}

// call forwards operation + payload and configures the plugin with server context.
func TestCallPath(t *testing.T) {
	p := &fakePlugin{caps: []string{"do"}}
	s := testServer(t, p)
	w := call(s, http.MethodPost, callPath("default", "default", "any", "ordinary"), `{"operation":"do","payload":{"x":[1,true]}}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if p.request.Operation != "do" || string(p.request.Payload) != `{"x":[1,true]}` {
		t.Fatalf("%+v", p.request)
	}
	if p.configured.Kind != "any" || string(p.configured.Data) != `{"safe":true}` {
		t.Fatalf("%+v", p.configured)
	}
}

// Bad VM ids are 400; unknown config is 404.
func TestInvalidVM(t *testing.T) {
	p := &fakePlugin{caps: []string{"do"}}
	s := testServer(t, p)
	w := call(s, http.MethodPost, callPath("default", "-bad", "any", "ordinary"), `{"operation":"do","payload":null}`)
	if w.Code != 400 {
		t.Fatal(w.Code)
	}
	w = call(s, http.MethodPost, "/v1/configs/default/vms/-bad/start", "")
	if w.Code != 400 {
		t.Fatal(w.Code)
	}
	w = call(s, http.MethodPost, "/v1/configs/missing/vms/default/start", "")
	if w.Code != 404 {
		t.Fatal(w.Code)
	}
}

// List always includes default. Start/stop/delete update state; delete drops
// named VMs but keeps default.
func TestListVMsAndLifecycle(t *testing.T) {
	backend := &fakeBackend{}
	s := testServerOpts(t, Options{
		Authorize: func(*http.Request, string, string) bool { return true },
		WithRuntime: func(_ string, _ string, fn func(runtimeplugin.Backend) error) error {
			return fn(backend)
		},
	}, "default")
	w := call(s, http.MethodGet, "/v1/configs/default/vms", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"default"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	w = call(s, http.MethodPost, "/v1/configs/default/vms/feature-01/start", "")
	if w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	if backend.spec.ID == "" || !strings.HasPrefix(backend.spec.ID, "cage-") {
		t.Fatalf("spec id=%q", backend.spec.ID)
	}
	w = call(s, http.MethodGet, "/v1/configs/default/vms", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"feature-01"`) || !strings.Contains(w.Body.String(), `"running"`) {
		t.Fatal(w.Code, w.Body.String())
	}

	w = call(s, http.MethodPost, "/v1/configs/default/vms/feature-01/stop", "")
	if w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	w = call(s, http.MethodGet, "/v1/configs/default/vms", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"feature-01"`) || !strings.Contains(w.Body.String(), `"stopped"`) {
		t.Fatal(w.Code, w.Body.String())
	}

	w = call(s, http.MethodPost, "/v1/configs/default/vms/feature-01/delete", "")
	if w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	w = call(s, http.MethodGet, "/v1/configs/default/vms", "")
	if w.Code != 200 || strings.Contains(w.Body.String(), `"feature-01"`) || !strings.Contains(w.Body.String(), `"default"`) {
		t.Fatal(w.Code, w.Body.String())
	}

	w = call(s, http.MethodPost, "/v1/configs/default/vms/default/start", "")
	if w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	w = call(s, http.MethodPost, "/v1/configs/default/vms/default/delete", "")
	if w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	w = call(s, http.MethodGet, "/v1/configs/default/vms", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"default"`) {
		t.Fatal(w.Code, w.Body.String())
	}
}

// Unknown config/context/seat or a non-callable seat is 404; a down seat is 503.
func TestRouteFailures(t *testing.T) {
	p := &fakePlugin{caps: []string{"do"}}
	s := testServer(t, p)
	for _, tt := range []struct {
		path string
		code int
	}{
		{callPath("default", "default", "no", "ordinary"), 404},
		{callPath("default", "default", "any", "no"), 404},
		{callPath("missing", "default", "any", "ordinary"), 404},
	} {
		if w := call(s, http.MethodPost, tt.path, `{"operation":"do","payload":null}`); w.Code != tt.code {
			t.Fatalf("%s: %d", tt.path, w.Code)
		}
	}
	// Inject non-callable after load by using a custom loader.
	s2 := testServerOpts(t, Options{
		Authorize: func(*http.Request, string, string) bool { return true },
		LoadSeats: func(Entry, VMMeta) *LoadedSeats {
			return &LoadedSeats{Seats: map[string]map[string]Seat{
				"any": {
					"ordinary":     {Plugin: p},
					"not-callable": {},
					"down":         {Err: errors.New("down")},
				},
			}}
		},
	}, "default")
	if w := call(s2, http.MethodPost, callPath("default", "default", "any", "not-callable"), `{"operation":"do","payload":null}`); w.Code != 404 {
		t.Fatal(w.Code)
	}
	if w := call(s2, http.MethodPost, callPath("default", "default", "any", "down"), `{"operation":"do","payload":null}`); w.Code != 503 {
		t.Fatal(w.Code)
	}
}

// Configure and Capabilities run once per scope. Oversized request is 413.
func TestCapabilitiesCachedAndLimits(t *testing.T) {
	p := &fakePlugin{caps: []string{"do"}}
	s := testServer(t, p)
	for range 2 {
		if w := call(s, http.MethodPost, callPath("default", "default", "any", "ordinary"), `{"operation":"do","payload":null}`); w.Code != 200 {
			t.Fatal(w.Code)
		}
	}
	if p.configureCalls != 1 || p.capabilityCalls != 1 {
		t.Fatalf("configure=%d caps=%d", p.configureCalls, p.capabilityCalls)
	}
	s.opts.Authorize = func(*http.Request, string, string) bool { return false }
	if w := call(s, http.MethodPost, callPath("default", "default", "any", "ordinary"), `{}`); w.Code != 401 {
		t.Fatal(w.Code)
	}
	s.opts.Authorize = func(*http.Request, string, string) bool { return true }
	s.opts.MaxRequest = 4
	if w := call(s, http.MethodPost, callPath("default", "default", "any", "ordinary"), `{"operation":"do","payload":null}`); w.Code != 413 {
		t.Fatal(w.Code)
	}
}

// Bad JSON or unknown operation is 400; oversized plugin response is 502.
func TestMalformedUnknownOperationAndResponseLimit(t *testing.T) {
	p := &fakePlugin{caps: []string{"do"}}
	s := testServer(t, p)
	for _, body := range []string{`{`, `{"operation":"other","payload":null}`} {
		if w := call(s, http.MethodPost, callPath("default", "default", "any", "ordinary"), body); w.Code != 400 {
			t.Fatal(w.Code)
		}
	}
	p.response = json.RawMessage(`"too long"`)
	s.opts.MaxResponse = 4
	if w := call(s, http.MethodPost, callPath("default", "default", "any", "ordinary"), `{"operation":"do","payload":null}`); w.Code != 502 {
		t.Fatal(w.Code)
	}
}

// Rate-limit buckets are capped; oldest entries are evicted.
func TestRateBucketsAreBounded(t *testing.T) {
	p := &fakePlugin{caps: []string{"do"}}
	s := testServerOpts(t, Options{
		Authorize:      func(*http.Request, string, string) bool { return true },
		MaxRateBuckets: 2,
		LoadSeats: func(Entry, VMMeta) *LoadedSeats {
			return &LoadedSeats{Seats: map[string]map[string]Seat{"any": {"ordinary": {Plugin: p}}}}
		},
	}, "default")
	for i := 0; i < 10; i++ {
		r := httptest.NewRequest(http.MethodPost, "/", nil)
		r.RemoteAddr = fmt.Sprintf("192.0.2.%d:1", i+1)
		if !s.allow(r, "default", "default") {
			t.Fatal("unexpected rate limit")
		}
	}
	if got := len(s.buckets); got > 2 {
		t.Fatalf("rate bucket count=%d", got)
	}
}

// Burst of 1: the second call in the same window is 429.
func TestRateLimit(t *testing.T) {
	p := &fakePlugin{caps: []string{"do"}}
	s := testServerOpts(t, Options{
		Authorize: func(*http.Request, string, string) bool { return true },
		Rate:      1,
		Burst:     1,
		LoadSeats: func(Entry, VMMeta) *LoadedSeats {
			return &LoadedSeats{Seats: map[string]map[string]Seat{"any": {"ordinary": {Plugin: p}}}}
		},
	}, "default")
	if w := call(s, http.MethodPost, callPath("default", "default", "any", "ordinary"), `{"operation":"do","payload":null}`); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w := call(s, http.MethodPost, callPath("default", "default", "any", "ordinary"), `{"operation":"do","payload":null}`); w.Code != 429 {
		t.Fatal(w.Code)
	}
}

// Arbitrary YAML seat names are valid {seat} path segments.
func TestNoSeatNameBehavior(t *testing.T) {
	p := &fakePlugin{caps: []string{"do"}}
	s := testServerOpts(t, Options{
		Authorize: func(*http.Request, string, string) bool { return true },
		LoadSeats: func(Entry, VMMeta) *LoadedSeats {
			return &LoadedSeats{Seats: map[string]map[string]Seat{
				"any": {"unusual-seat": {Context: plugin.Context{Kind: "any"}, Plugin: p}},
			}}
		},
	}, "default")
	if w := call(s, http.MethodPost, callPath("default", "default", "any", "unusual-seat"), `{"operation":"do","payload":null}`); w.Code != 200 {
		t.Fatal(w.Code)
	}
}

// Concurrent=1: a second in-flight call is 429 until the first finishes.
func TestConcurrentLimit(t *testing.T) {
	release := make(chan struct{})
	p := &fakePlugin{caps: []string{"do"}, started: make(chan struct{}), release: release}
	s := testServerOpts(t, Options{
		Authorize:  func(*http.Request, string, string) bool { return true },
		Concurrent: 1,
		LoadSeats: func(Entry, VMMeta) *LoadedSeats {
			return &LoadedSeats{Seats: map[string]map[string]Seat{"any": {"ordinary": {Plugin: p}}}}
		},
	}, "default")
	first := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		first <- call(s, http.MethodPost, callPath("default", "default", "any", "ordinary"), `{"operation":"do","payload":null}`)
	}()
	<-p.started
	if w := call(s, http.MethodPost, callPath("default", "default", "any", "ordinary"), `{"operation":"do","payload":null}`); w.Code != 429 {
		t.Fatal(w.Code)
	}
	close(release)
	if w := <-first; w.Code != 200 {
		t.Fatal(w.Code)
	}
}

// Plugin errors must not leak host paths or secrets in the HTTP body.
func TestPluginErrorsAreSafe(t *testing.T) {
	p := &fakePlugin{caps: []string{"do"}, err: errors.New("/host/private secret")}
	w := call(testServer(t, p), http.MethodPost, callPath("default", "default", "any", "ordinary"), `{"operation":"do","payload":null}`)
	if w.Code != 502 || strings.Contains(w.Body.String(), "secret") || strings.Contains(w.Body.String(), "/host") {
		t.Fatalf("%d: %s", w.Code, w.Body.String())
	}
}

// Same VM id under two configs must get different backend VM ids.
func TestTwoConfigsIsolated(t *testing.T) {
	backend := &fakeBackend{}
	s := testServerOpts(t, Options{
		Authorize: func(*http.Request, string, string) bool { return true },
		WithRuntime: func(_ string, _ string, fn func(runtimeplugin.Backend) error) error {
			return fn(backend)
		},
	}, "default", "dogfood")
	if w := call(s, http.MethodPost, "/v1/configs/default/vms/feature-01/start", ""); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	idA := backend.spec.ID
	if w := call(s, http.MethodPost, "/v1/configs/dogfood/vms/feature-01/start", ""); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	if backend.spec.ID == "" || backend.spec.ID == idA {
		t.Fatalf("backend ids collided: %q %q", idA, backend.spec.ID)
	}
}

// 127.0.0.1:0 binds an ephemeral loopback port.
func TestListenEphemeral(t *testing.T) {
	s := testServer(t, &fakePlugin{})
	ln, err := s.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if ln.Addr().String() == "127.0.0.1:0" {
		t.Fatal("did not bind ephemeral port")
	}
}

// Wildcard or LAN binds are rejected.
func TestLoopbackOnly(t *testing.T) {
	s := testServer(t, &fakePlugin{})
	for _, address := range []string{"0.0.0.0:0", "10.0.0.1:0", "[::]:0"} {
		if err := s.ListenHost(address); err == nil {
			t.Fatalf("accepted non-loopback bind %q", address)
		}
	}
}

// A seat that fails Configure is 503 and does not take down the server.
func TestConfigFailureIsolated(t *testing.T) {
	failed := &fakePlugin{caps: []string{"do"}, configErr: errors.New("unavailable")}
	s := testServer(t, failed)
	if w := call(s, http.MethodPost, callPath("default", "default", "any", "ordinary"), `{"operation":"do","payload":null}`); w.Code != 503 {
		t.Fatal(w.Code)
	}
}
