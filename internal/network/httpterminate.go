package network

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	netplugin "github.com/appmatter/cage/pkg/plugin/v1/network"
)

// HTTPEndpointListen is one named reverse-proxy bind.
type HTTPEndpointListen struct {
	Name   string
	Listen int // 0 = ephemeral
}

// HTTPTerminate runs per-endpoint reverse HTTP proxies through Prepare + egress Check.
type HTTPTerminate struct {
	Terminate   netplugin.Terminate
	Pipeline    *Pipeline
	OnTraffic   TrafficLogger
	DenyHTTP    bool
	DenyMessage string
	Client      *http.Client

	mu        sync.Mutex
	listeners map[string]net.Listener
	ports     map[string]int
}

// Ports returns name → bound port after Start.
func (h *HTTPTerminate) Ports() map[string]int {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make(map[string]int, len(h.ports))
	for k, v := range h.ports {
		out[k] = v
	}
	return out
}

// Start binds listeners for each endpoint and serves until Close.
func (h *HTTPTerminate) Start(endpoints []HTTPEndpointListen) error {
	if h == nil || h.Terminate == nil {
		return nil
	}
	h.mu.Lock()
	if h.listeners == nil {
		h.listeners = map[string]net.Listener{}
		h.ports = map[string]int{}
	}
	h.mu.Unlock()
	client := h.Client
	if client == nil {
		client = &http.Client{Timeout: 120 * time.Second}
	}
	for _, ep := range endpoints {
		addr := "0.0.0.0:" + strconv.Itoa(ep.Listen)
		if ep.Listen <= 0 {
			addr = "0.0.0.0:0"
		}
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			_ = h.Close()
			return fmt.Errorf("http-proxy %s listen: %w", ep.Name, err)
		}
		port := ln.Addr().(*net.TCPAddr).Port
		h.mu.Lock()
		h.listeners[ep.Name] = ln
		h.ports[ep.Name] = port
		h.mu.Unlock()
		name := ep.Name
		srv := &http.Server{
			Handler: h.handler(name, client),
		}
		go func() {
			_ = srv.Serve(ln)
		}()
	}
	return nil
}

// Close stops all listeners.
func (h *HTTPTerminate) Close() error {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	var first error
	for name, ln := range h.listeners {
		if err := ln.Close(); err != nil && first == nil {
			first = err
		}
		delete(h.listeners, name)
	}
	return first
}

func (h *HTTPTerminate) handler(endpoint string, client *http.Client) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.RequestURI() // path + query
		in := netplugin.PrepareIn{
			Endpoint: endpoint,
			Method:   r.Method,
			Path:     path,
			Header:   r.Header.Clone(),
		}
		out, err := h.Terminate.Prepare(in)
		if err != nil {
			http.Error(w, "http-proxy prepare: "+err.Error(), http.StatusBadGateway)
			return
		}
		checkPath := r.URL.Path
		if u, err := url.Parse(out.UpstreamURL); err == nil && u.Path != "" {
			checkPath = u.Path
		}
		req := netplugin.Request{
			Host:   out.UpstreamHost,
			Port:   out.UpstreamPort,
			Method: r.Method,
			Path:   checkPath,
		}
		if h.Pipeline != nil {
			d, err := h.Pipeline.Check(req)
			if err != nil {
				h.log(req, "FAIL", "", err.Error())
				http.Error(w, err.Error(), http.StatusBadGateway)
				return
			}
			if !d.Allow {
				h.log(req, "DENY", d.Reason, "")
				h.writeDeny(w, req, d.Reason)
				return
			}
		}
		upReq, err := http.NewRequestWithContext(r.Context(), r.Method, out.UpstreamURL, r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		upReq.Header = out.Header
		resp, err := client.Do(upReq)
		if err != nil {
			h.log(req, "FAIL", "", err.Error())
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		h.log(req, "ALLOW", "http-proxy:"+endpoint, "")
		for k, vals := range resp.Header {
			for _, v := range vals {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
	})
}

func (h *HTTPTerminate) log(req netplugin.Request, action, reason, errMsg string) {
	if h == nil || h.OnTraffic == nil {
		return
	}
	h.OnTraffic.Log(TrafficEvent{
		Action: action,
		Host:   req.Host,
		Port:   req.Port,
		Method: req.Method,
		Path:   req.Path,
		Reason: reason,
		Error:  errMsg,
	})
}

func (h *HTTPTerminate) writeDeny(w http.ResponseWriter, req netplugin.Request, reason string) {
	enabled := h.DenyHTTP
	message := h.DenyMessage
	if !enabled {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	if message == "" {
		message = DefaultDenyHTTPMessage
	}
	detail := fmt.Sprintf("%s:%d", req.Host, req.Port)
	if req.Method != "" {
		detail += " " + req.Method
		if req.Path != "" {
			detail += " " + req.Path
		}
	}
	if reason != "" {
		detail += " (" + reason + ")"
	}
	body := message + "\n\n" + detail + "\n"
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Connection", "close")
	w.WriteHeader(http.StatusForbidden)
	_, _ = io.WriteString(w, body)
}
