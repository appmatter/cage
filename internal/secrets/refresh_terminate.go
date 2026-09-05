package secrets

import (
	"fmt"
	"runtime"
	"sync"
	"time"

	"github.com/appmatter/cage/internal/config"
	netplugin "github.com/appmatter/cage/pkg/plugin/v1/network"
)

// DefaultRefreshInterval is how often a refreshing terminate re-resolves secrets.
// openai-oauth no-ops when the access token is still fresh.
const DefaultRefreshInterval = 2 * time.Minute

// RefreshingTerminate re-resolves {{ secrets.* }} in templateYAML and re-Configures
// the inner terminate plugin on a timer so OAuth access tokens stay valid without
// restarting the VM or the proxy listeners.
type RefreshingTerminate struct {
	Inner        netplugin.Terminate
	ProjectRoot  string
	ConfigPath   string
	TemplateYAML []byte
	Interval     time.Duration

	mu       sync.Mutex
	nextAt   time.Time
	lastErr  error
}

func (t *RefreshingTerminate) Name() string { return t.Inner.Name() }

func (t *RefreshingTerminate) Configure(raw []byte) error {
	return t.Inner.Configure(raw)
}

func (t *RefreshingTerminate) Prepare(in netplugin.PrepareIn) (netplugin.PrepareOut, error) {
	if err := t.maybeRefresh(); err != nil {
		return netplugin.PrepareOut{}, err
	}
	return t.Inner.Prepare(in)
}

func (t *RefreshingTerminate) maybeRefresh() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now()
	if !t.nextAt.IsZero() && now.Before(t.nextAt) {
		return nil
	}
	// Best-effort: on failure keep the last successful Configure so brief
	// auth.openai.com blips do not drop in-flight proxy traffic.
	t.lastErr = t.refreshLocked()
	interval := t.Interval
	if interval <= 0 {
		interval = DefaultRefreshInterval
	}
	t.nextAt = now.Add(interval)
	return nil
}

func (t *RefreshingTerminate) refreshLocked() error {
	if t.ConfigPath == "" {
		return fmt.Errorf("secrets refresh: --config required")
	}
	r, err := config.LoadResolved(t.ProjectRoot, t.ConfigPath, runtime.GOOS)
	if err != nil {
		return fmt.Errorf("secrets refresh: load config: %w", err)
	}
	if d, err := r.Secrets.RefreshEvery(); err == nil && d > 0 {
		t.Interval = d
	}
	vals, err := Resolve(t.ProjectRoot, r.Secrets.Plugins)
	if err != nil {
		return fmt.Errorf("secrets refresh: %w", err)
	}
	cfgRaw, err := ApplyBytes(t.TemplateYAML, vals)
	if err != nil {
		return fmt.Errorf("secrets refresh apply: %w", err)
	}
	if err := t.Inner.Configure(cfgRaw); err != nil {
		return fmt.Errorf("secrets refresh configure: %w", err)
	}
	return nil
}

var _ netplugin.Terminate = (*RefreshingTerminate)(nil)
