package network

import (
	"context"
	"fmt"
	"net"
	"sort"
	"time"

	netplugin "github.com/appmatter/cage/pkg/plugin/v1/network"
)

// FilterSeat is one configured filter plugin with operator priority.
type FilterSeat struct {
	Priority int
	Filter   netplugin.Filter
}

// Pipeline runs network.traffic: filter → (terminate later) → dial.
type Pipeline struct {
	Filters []FilterSeat
	Dialer  DialContextFunc
}

// DialContextFunc opens a TCP connection (testable).
type DialContextFunc func(ctx context.Context, network, address string) (net.Conn, error)

// NewPipeline builds a pipeline. Filters are sorted by priority ascending.
func NewPipeline(filters []FilterSeat) *Pipeline {
	out := append([]FilterSeat{}, filters...)
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Priority < out[j].Priority
	})
	return &Pipeline{
		Filters: out,
		Dialer:  (&net.Dialer{Timeout: 30 * time.Second}).DialContext,
	}
}

// Check runs all filters; any deny wins. No filters → allow.
func (p *Pipeline) Check(req netplugin.Request) (netplugin.Decision, error) {
	if p == nil || len(p.Filters) == 0 {
		return netplugin.Decision{Allow: true, Reason: "no filters"}, nil
	}
	for _, seat := range p.Filters {
		d, err := seat.Filter.Check(req)
		if err != nil {
			return netplugin.Decision{}, err
		}
		if !d.Allow {
			return d, nil
		}
	}
	return netplugin.Decision{Allow: true, Reason: "all filters allowed"}, nil
}

// Dial checks filters then opens a TCP connection to host:port.
func (p *Pipeline) Dial(ctx context.Context, req netplugin.Request) (net.Conn, error) {
	d, err := p.Check(req)
	if err != nil {
		return nil, err
	}
	if !d.Allow {
		return nil, fmt.Errorf("network.traffic denied: %s", d.Reason)
	}
	return p.Open(ctx, req)
}

// Open dials host:port without running filters (caller already checked).
func (p *Pipeline) Open(ctx context.Context, req netplugin.Request) (net.Conn, error) {
	if req.Host == "" {
		return nil, fmt.Errorf("host is required")
	}
	if req.Port <= 0 {
		return nil, fmt.Errorf("port is required")
	}
	dial := DialContextFunc(nil)
	if p != nil {
		dial = p.Dialer
	}
	if dial == nil {
		dial = (&net.Dialer{Timeout: 30 * time.Second}).DialContext
	}
	addr := net.JoinHostPort(req.Host, fmt.Sprintf("%d", req.Port))
	return dial(ctx, "tcp", addr)
}
