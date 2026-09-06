// Package contextapi serves the host-only per-project client context API.
package contextapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/appmatter/cage/internal/pluginhost"
	"github.com/appmatter/cage/internal/vminstance"
	plugin "github.com/appmatter/cage/pkg/plugin/v1/client"
	runtimeplugin "github.com/appmatter/cage/pkg/plugin/v1/runtime"
)

const (
	DefaultMaxRequest     int64 = 1 << 20
	DefaultMaxResponse    int64 = 4 << 20
	DefaultMaxRateBuckets       = 1024
	rateBucketTTL               = 5 * time.Minute
)

// Seat is a server-selected, already resolved plugin scope.
type Seat struct {
	Context plugin.Context
	Plugin  plugin.Capability
	Err     error
}

// Authorize authenticates and authorizes a request for config and optional VM.
// config is empty for list-configs. vm is empty for list-configs and list-vms.
type Authorize func(r *http.Request, config, vm string) bool

// Options configures a per-project context API server.
type Options struct {
	Project   *Project
	Authorize Authorize
	// LoadSeats overrides default seat loading (tests).
	LoadSeats func(entry Entry, meta VMMeta) *LoadedSeats
	// WithRuntime overrides runtime backend dispense (tests).
	WithRuntime func(projectRoot, backend string, fn func(runtimeplugin.Backend) error) error

	MaxRequest     int64
	MaxResponse    int64
	Concurrent     int
	Rate           int
	Burst          int
	MaxRateBuckets int
	// AllowedHosts are accepted Host header names (port stripped). Empty = no Host check.
	AllowedHosts []string
}

type scopeKey struct {
	config string
	vm     string
}

type vmScope struct {
	seats *LoadedSeats
	caps  map[string]map[string][]string
}

// Server is the project-scoped client context HTTP API.
type Server struct {
	opts     Options
	sem      chan struct{}
	mu       sync.Mutex
	buckets  map[string]*bucket
	scopes   map[scopeKey]*vmScope
	knownVMs map[string]map[string]struct{}
}

type bucket struct {
	at     time.Time
	tokens float64
}

// New builds a server. Call Close when finished to shut down loaded seats.
func New(opts Options) *Server {
	if opts.MaxRequest <= 0 {
		opts.MaxRequest = DefaultMaxRequest
	}
	if opts.MaxResponse <= 0 {
		opts.MaxResponse = DefaultMaxResponse
	}
	if opts.Concurrent <= 0 {
		opts.Concurrent = 16
	}
	if opts.Rate <= 0 {
		opts.Rate = 30
	}
	if opts.Burst <= 0 {
		opts.Burst = opts.Rate
	}
	if opts.MaxRateBuckets <= 0 {
		opts.MaxRateBuckets = DefaultMaxRateBuckets
	}
	known := map[string]map[string]struct{}{}
	if opts.Project != nil {
		for id := range opts.Project.Configs {
			known[id] = map[string]struct{}{"default": {}}
		}
	}
	return &Server{
		opts:     opts,
		sem:      make(chan struct{}, opts.Concurrent),
		buckets:  map[string]*bucket{},
		scopes:   map[scopeKey]*vmScope{},
		knownVMs: known,
	}
}

// Close shuts down lazily loaded seat processes.
func (s *Server) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, scope := range s.scopes {
		scope.seats.Close()
	}
	s.scopes = map[scopeKey]*vmScope{}
}

// Handler returns an HTTP handler. It has no guest-facing listener.
func (s *Server) Handler() http.Handler { return http.HandlerFunc(s.serveHTTP) }

// Listen binds loopback only. It rejects wildcard and guest-facing binds.
func (s *Server) Listen(addr string) (net.Listener, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("context API address: %w", err)
	}
	if host != "127.0.0.1" && host != "::1" && host != "localhost" {
		return nil, errors.New("context API must bind to loopback")
	}
	return net.Listen("tcp", addr)
}

// Serve serves the API on ln.
func (s *Server) Serve(ln net.Listener) error {
	return http.Serve(ln, s.Handler())
}

// ListenHost serves only on loopback. It deliberately rejects wildcard and guest-facing binds.
func (s *Server) ListenHost(addr string) error {
	ln, err := s.Listen(addr)
	if err != nil {
		return err
	}
	defer ln.Close()
	return s.Serve(ln)
}

func (s *Server) serveHTTP(w http.ResponseWriter, r *http.Request) {
	if !s.hostOK(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 2 || parts[0] != "v1" || parts[1] != "configs" {
		http.NotFound(w, r)
		return
	}

	switch {
	case r.Method == http.MethodGet && len(parts) == 2:
		s.handleListConfigs(w, r)
	case r.Method == http.MethodGet && len(parts) == 4 && parts[3] == "vms":
		s.handleListVMs(w, r, parts[2])
	case r.Method == http.MethodPost && len(parts) == 6 && parts[3] == "vms":
		s.handleLifecycle(w, r, parts[2], parts[4], parts[5])
	case r.Method == http.MethodPost && len(parts) == 10 && parts[3] == "vms" && parts[5] == "context" && parts[7] == "plugins" && parts[9] == "call":
		s.handleCall(w, r, parts[2], parts[4], parts[6], parts[8])
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) hostOK(r *http.Request) bool {
	if len(s.opts.AllowedHosts) == 0 {
		return true
	}
	got := requestHost(r.Host)
	if got == "" {
		return false
	}
	for _, want := range s.opts.AllowedHosts {
		if requestHost(want) == got {
			return true
		}
	}
	return false
}

func requestHost(host string) string {
	h, _, err := net.SplitHostPort(host)
	if err != nil {
		h = host
	}
	return strings.Trim(h, "[]")
}

func (s *Server) auth(w http.ResponseWriter, r *http.Request, config, vm string) bool {
	if s.opts.Authorize == nil || !s.opts.Authorize(r, config, vm) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return false
	}
	return true
}

func (s *Server) handleListConfigs(w http.ResponseWriter, r *http.Request) {
	if !s.auth(w, r, "", "") {
		return
	}
	if !s.allow(r, "", "") {
		http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
		return
	}
	type item struct {
		ID string `json:"id"`
	}
	ids := make([]string, 0)
	if s.opts.Project != nil {
		for id := range s.opts.Project.Configs {
			if s.opts.Authorize == nil || s.opts.Authorize(r, id, "") {
				ids = append(ids, id)
			}
		}
	}
	sort.Strings(ids)
	out := make([]item, len(ids))
	for i, id := range ids {
		out[i] = item{ID: id}
	}
	writeJSON(w, struct {
		Configs []item `json:"configs"`
	}{out})
}

func (s *Server) handleListVMs(w http.ResponseWriter, r *http.Request, config string) {
	if !s.auth(w, r, config, "") {
		return
	}
	entry, ok := s.entry(config)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if !s.allow(r, config, "") {
		http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
		return
	}
	s.mu.Lock()
	known := make([]string, 0, len(s.knownVMs[config]))
	for id := range s.knownVMs[config] {
		known = append(known, id)
	}
	s.mu.Unlock()
	sort.Strings(known)

	type item struct {
		ID    string `json:"id"`
		State string `json:"state"`
	}
	out := make([]item, 0, len(known))
	for _, id := range known {
		_, backendID, err := vminstance.Resolve(entry.ProjectRoot, entry.ConfigPath, id)
		if err != nil {
			out = append(out, item{ID: id, State: "unknown"})
			continue
		}
		state := "absent"
		_ = s.withRuntime(entry, func(b runtimeplugin.Backend) error {
			st, err := b.Status(backendID)
			if err != nil {
				state = "absent"
				return nil
			}
			if st.State == "" {
				state = "unknown"
			} else {
				state = st.State
			}
			return nil
		})
		out = append(out, item{ID: id, State: state})
	}
	writeJSON(w, struct {
		VMs []item `json:"vms"`
	}{out})
}

func (s *Server) handleLifecycle(w http.ResponseWriter, r *http.Request, config, vm, action string) {
	switch action {
	case "start", "stop", "delete":
	default:
		http.NotFound(w, r)
		return
	}
	if !s.auth(w, r, config, vm) {
		return
	}
	entry, ok := s.entry(config)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if _, err := vminstance.Normalize(vm); err != nil {
		http.Error(w, "invalid VM id", http.StatusBadRequest)
		return
	}
	_, backendID, err := vminstance.Resolve(entry.ProjectRoot, entry.ConfigPath, vm)
	if err != nil {
		http.Error(w, "invalid VM id", http.StatusBadRequest)
		return
	}
	if !s.allow(r, config, vm) {
		http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
		return
	}
	if err := s.acquire(); err != nil {
		http.Error(w, "too many concurrent calls", http.StatusTooManyRequests)
		return
	}
	defer s.release()

	spec := entrySpec(entry, backendID)
	err = s.withRuntime(entry, func(b runtimeplugin.Backend) error {
		switch action {
		case "start":
			if err := b.Create(spec); err != nil {
				return err
			}
			return b.Start(spec)
		case "stop":
			return b.Stop(backendID)
		case "delete":
			return b.Delete(spec)
		default:
			return nil
		}
	})
	if err != nil {
		http.Error(w, "lifecycle operation failed", http.StatusBadGateway)
		return
	}
	if action == "start" {
		s.rememberVM(config, vm)
	}
	if action == "delete" && vm != "default" {
		s.forgetVM(config, vm)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleCall(w http.ResponseWriter, r *http.Request, config, vm, context, seat string) {
	if !s.auth(w, r, config, vm) {
		return
	}
	entry, ok := s.entry(config)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if _, err := vminstance.Normalize(vm); err != nil {
		http.Error(w, "invalid VM id", http.StatusBadRequest)
		return
	}
	_, backendID, err := vminstance.Resolve(entry.ProjectRoot, entry.ConfigPath, vm)
	if err != nil {
		http.Error(w, "invalid VM id", http.StatusBadRequest)
		return
	}
	if !s.allow(r, config, vm) {
		http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
		return
	}
	scope, err := s.ensureScope(entry, config, vm, VMMeta{InstanceID: vm, BackendVMID: backendID})
	if err != nil {
		http.Error(w, "configured plugin is unavailable", http.StatusServiceUnavailable)
		return
	}
	contexts, ok := scope.seats.Seats[context]
	if !ok {
		http.NotFound(w, r)
		return
	}
	entrySeat, ok := contexts[seat]
	if !ok {
		http.NotFound(w, r)
		return
	}
	if entrySeat.Err != nil {
		http.Error(w, "configured plugin is unavailable", http.StatusServiceUnavailable)
		return
	}
	if entrySeat.Plugin == nil || len(scope.caps[context][seat]) == 0 {
		http.NotFound(w, r)
		return
	}
	if err := s.acquire(); err != nil {
		http.Error(w, "too many concurrent calls", http.StatusTooManyRequests)
		return
	}
	defer s.release()

	r.Body = http.MaxBytesReader(w, r.Body, s.opts.MaxRequest)
	var in struct {
		Operation string          `json:"operation"`
		Payload   json.RawMessage `json:"payload"`
	}
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(&in); err != nil {
		if _, ok := err.(*http.MaxBytesError); ok {
			http.Error(w, "request too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "malformed JSON", http.StatusBadRequest)
		return
	}
	var extra interface{}
	if err := dec.Decode(&extra); err != io.EOF {
		http.Error(w, "malformed JSON", http.StatusBadRequest)
		return
	}
	if in.Operation == "" || !allowed([]string{in.Operation}, scope.caps[context][seat]) {
		http.Error(w, "unknown operation", http.StatusBadRequest)
		return
	}
	if len(in.Payload) == 0 {
		in.Payload = json.RawMessage("null")
	}
	out, err := entrySeat.Plugin.Call(plugin.Request{Operation: in.Operation, Payload: in.Payload})
	if err != nil {
		http.Error(w, "plugin operation failed", http.StatusBadGateway)
		return
	}
	if len(out.Payload) == 0 || !json.Valid(out.Payload) || int64(len(out.Payload)) > s.opts.MaxResponse {
		http.Error(w, "invalid plugin response", http.StatusBadGateway)
		return
	}
	response, _ := json.Marshal(struct {
		Payload json.RawMessage `json:"payload"`
	}{out.Payload})
	if int64(len(response)) > s.opts.MaxResponse {
		http.Error(w, "plugin response too large", http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(append(response, '\n'))
}

func (s *Server) entry(config string) (Entry, bool) {
	if s.opts.Project == nil {
		return Entry{}, false
	}
	return s.opts.Project.Get(config)
}

func (s *Server) ensureScope(entry Entry, config, vm string, meta VMMeta) (*vmScope, error) {
	key := scopeKey{config: config, vm: vm}
	s.mu.Lock()
	if scope, ok := s.scopes[key]; ok {
		s.mu.Unlock()
		return scope, nil
	}
	s.mu.Unlock()

	var loaded *LoadedSeats
	if s.opts.LoadSeats != nil {
		loaded = s.opts.LoadSeats(entry, meta)
	} else {
		loaded = LoadSeats(entry.ProjectRoot, entry.Resolved, meta)
	}
	caps := map[string]map[string][]string{}
	for context, seats := range loaded.Seats {
		caps[context] = map[string][]string{}
		for name, seat := range seats {
			if seat.Plugin == nil {
				continue
			}
			seat.Err = seat.Plugin.Configure(seat.Context)
			seats[name] = seat
			if seat.Err == nil {
				caps[context][name] = append([]string(nil), seat.Plugin.Capabilities()...)
			}
		}
	}
	scope := &vmScope{seats: loaded, caps: caps}

	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.scopes[key]; ok {
		loaded.Close()
		return existing, nil
	}
	s.scopes[key] = scope
	s.rememberVMLocked(config, vm)
	return scope, nil
}

func (s *Server) rememberVM(config, vm string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rememberVMLocked(config, vm)
}

func (s *Server) rememberVMLocked(config, vm string) {
	if s.knownVMs[config] == nil {
		s.knownVMs[config] = map[string]struct{}{"default": {}}
	}
	s.knownVMs[config][vm] = struct{}{}
}

func (s *Server) forgetVM(config, vm string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if vm == "default" {
		return
	}
	delete(s.knownVMs[config], vm)
	key := scopeKey{config: config, vm: vm}
	if scope, ok := s.scopes[key]; ok {
		scope.seats.Close()
		delete(s.scopes, key)
	}
}

func (s *Server) withRuntime(entry Entry, fn func(runtimeplugin.Backend) error) error {
	backend := entry.Resolved.Runtime.Backend
	if s.opts.WithRuntime != nil {
		return s.opts.WithRuntime(entry.ProjectRoot, backend, fn)
	}
	cmdPath, _, err := pluginhost.ResolveCommand(entry.ProjectRoot, "runtime", backend)
	if err != nil {
		return err
	}
	client, err := pluginhost.DispenseRuntime(cmdPath, entry.ProjectRoot)
	if err != nil {
		return err
	}
	defer client.Close()
	return fn(client.Backend)
}

func entrySpec(entry Entry, backendVMID string) runtimeplugin.Spec {
	r := entry.Resolved
	spec := runtimeplugin.Spec{
		ID:          backendVMID,
		ProjectRoot: entry.ProjectRoot,
		Image:       r.Runtime.Image,
		Workdir:   r.Runtime.Workdir,
		Graphics:  r.Runtime.Graphics,
		Env:       copyStringMap(r.Runtime.Env),
		OnCreate:  append([]string{}, r.Runtime.OnCreate...),
		OnStart:   append([]string{}, r.Runtime.OnStart...),
		OnDestroy: append([]string{}, r.Runtime.OnDestroy...),
	}
	for _, m := range r.Mounts {
		spec.Mounts = append(spec.Mounts, runtimeplugin.PathSpec{
			Host: m.Host, Guest: m.Guest, Permission: m.Permission,
		})
	}
	for _, c := range r.Copies {
		spec.Copies = append(spec.Copies, runtimeplugin.PathSpec{
			Host: c.Host, Guest: c.Guest, Permission: c.Permission,
		})
	}
	spec.DenyMasks = append([]string{}, r.DenyMasks...)
	return spec
}

func copyStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func (s *Server) acquire() error {
	select {
	case s.sem <- struct{}{}:
		return nil
	default:
		return errors.New("busy")
	}
}

func (s *Server) release() { <-s.sem }

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func allowed(want []string, have []string) bool {
	for _, w := range want {
		for _, h := range have {
			if w == h {
				return true
			}
		}
	}
	return false
}

func (s *Server) allow(r *http.Request, config, vm string) bool {
	identity := bearerToken(r)
	if identity == "" {
		host, _, _ := net.SplitHostPort(r.RemoteAddr)
		if host == "" {
			host = r.RemoteAddr
		}
		identity = host
	}
	key := identity + "|" + config + "|" + vm
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	for address, bucket := range s.buckets {
		if now.Sub(bucket.at) > rateBucketTTL {
			delete(s.buckets, address)
		}
	}
	b := s.buckets[key]
	if b == nil {
		if len(s.buckets) >= s.opts.MaxRateBuckets {
			var oldest string
			var oldestAt time.Time
			for address, bucket := range s.buckets {
				if oldest == "" || bucket.at.Before(oldestAt) {
					oldest, oldestAt = address, bucket.at
				}
			}
			delete(s.buckets, oldest)
		}
		b = &bucket{at: now, tokens: float64(s.opts.Burst)}
		s.buckets[key] = b
	}
	b.tokens = min(float64(s.opts.Burst), b.tokens+now.Sub(b.at).Seconds()*float64(s.opts.Rate))
	b.at = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
