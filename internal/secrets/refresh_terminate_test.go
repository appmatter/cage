package secrets

import (
	"sync/atomic"
	"testing"
	"time"

	netplugin "github.com/appmatter/cage/pkg/plugin/v1/network"
)

type stubTerminate struct {
	configureN atomic.Int32
	prepareN   atomic.Int32
}

func (s *stubTerminate) Name() string { return "stub" }
func (s *stubTerminate) Configure(raw []byte) error {
	s.configureN.Add(1)
	return nil
}
func (s *stubTerminate) Prepare(in netplugin.PrepareIn) (netplugin.PrepareOut, error) {
	s.prepareN.Add(1)
	return netplugin.PrepareOut{UpstreamHost: "example.com", UpstreamPort: 443}, nil
}

func TestRefreshingTerminateDebounce(t *testing.T) {
	inner := &stubTerminate{}
	rt := &RefreshingTerminate{
		Inner:        inner,
		ProjectRoot:  t.TempDir(),
		ConfigPath:   "",
		TemplateYAML: []byte("x: y\n"),
		Interval:     time.Hour,
	}
	rt.mu.Lock()
	rt.nextAt = time.Now().Add(time.Hour)
	rt.mu.Unlock()

	if _, err := rt.Prepare(netplugin.PrepareIn{Endpoint: "openai"}); err != nil {
		t.Fatal(err)
	}
	if inner.prepareN.Load() != 1 {
		t.Fatalf("prepare calls %d", inner.prepareN.Load())
	}
	if inner.configureN.Load() != 0 {
		t.Fatal("debounced path must not Configure")
	}
}
