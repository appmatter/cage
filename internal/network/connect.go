package network

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	netplugin "github.com/appmatter/cage/pkg/plugin/v1/network"
)

// HTTPProxyServer is a guest-facing HTTP CONNECT (+ plain HTTP) proxy with optional MITM.
type HTTPProxyServer struct {
	Pipeline     *Pipeline
	Dial         DialFunc
	OnTraffic    TrafficLogger
	MITM         *MITM // nil → tunnel CONNECT without break
	Terminate    netplugin.Terminate
	HostToEP     map[string]string // lowercase hostname → http-proxy endpoint name
	UpstreamTLS  *tls.Config       // nil → system roots; tests may set InsecureSkipVerify
	DenyHTTP     bool
	DenyMessage  string

	denyMu sync.RWMutex

	mu       sync.Mutex
	listener net.Listener
	done     chan struct{}
}

// ListenAndServe binds addr and serves until Close.
func (s *HTTPProxyServer) ListenAndServe(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.listener = ln
	s.done = make(chan struct{})
	s.mu.Unlock()
	for {
		c, err := ln.Accept()
		if err != nil {
			select {
			case <-s.done:
				return nil
			default:
				return err
			}
		}
		go s.handle(c)
	}
}

// Port returns the bound TCP port.
func (s *HTTPProxyServer) Port() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener == nil {
		return 0
	}
	tcp, ok := s.listener.Addr().(*net.TCPAddr)
	if !ok {
		return 0
	}
	return tcp.Port
}

// Close stops the listener.
func (s *HTTPProxyServer) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done != nil {
		select {
		case <-s.done:
		default:
			close(s.done)
		}
	}
	if s.listener != nil {
		return s.listener.Close()
	}
	return nil
}

// SetDenyHTTP updates inject settings (hot reload).
func (s *HTTPProxyServer) SetDenyHTTP(enabled bool, message string) {
	if s == nil {
		return
	}
	s.denyMu.Lock()
	s.DenyHTTP = enabled
	s.DenyMessage = message
	s.denyMu.Unlock()
}

func (s *HTTPProxyServer) denyHTTPSettings() (bool, string) {
	s.denyMu.RLock()
	defer s.denyMu.RUnlock()
	return s.DenyHTTP, s.DenyMessage
}

func (s *HTTPProxyServer) handle(c net.Conn) {
	defer c.Close()
	_ = c.SetReadDeadline(time.Now().Add(30 * time.Second))
	br := bufio.NewReader(c)
	req, err := http.ReadRequest(br)
	if err != nil {
		return
	}
	_ = c.SetReadDeadline(time.Time{})

	if req.Method == http.MethodConnect {
		s.handleCONNECT(c, br, req)
		return
	}
	s.handlePlainHTTP(c, br, req)
}

func (s *HTTPProxyServer) handleCONNECT(c net.Conn, br *bufio.Reader, req *http.Request) {
	host, port, err := splitHostPortDefault(req.Host, 443)
	if err != nil {
		_, _ = io.WriteString(c, "HTTP/1.1 400 Bad Request\r\n\r\n")
		return
	}
	nreq := netplugin.Request{Host: host, Port: port, Method: "CONNECT", Partial: true}
	if s.Pipeline != nil {
		d, err := s.Pipeline.Check(nreq)
		if err != nil {
			s.log(nreq, "FAIL", "", err.Error())
			_, _ = io.WriteString(c, "HTTP/1.1 502 Bad Gateway\r\n\r\n")
			return
		}
		if !d.Allow {
			s.log(nreq, "DENY", d.Reason, "")
			_, _ = io.WriteString(c, "HTTP/1.1 403 Forbidden\r\n\r\n")
			return
		}
	}
	if _, err := io.WriteString(c, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		return
	}

	client := bufConn{Reader: br, Conn: c}
	if s.MITM == nil {
		s.tunnel(client, nreq)
		return
	}
	s.mitmCONNECT(client, nreq)
}

func (s *HTTPProxyServer) tunnel(client net.Conn, req netplugin.Request) {
	req.Partial = false
	var up net.Conn
	var err error
	if s.Dial != nil {
		up, err = s.Dial(context.Background(), req)
	} else if s.Pipeline != nil {
		up, err = s.Pipeline.Open(context.Background(), req)
	} else {
		up, err = net.DialTimeout("tcp", net.JoinHostPort(req.Host, strconv.Itoa(req.Port)), 30*time.Second)
	}
	if err != nil {
		s.log(req, "FAIL", "", err.Error())
		return
	}
	defer up.Close()
	s.log(req, "ALLOW", "tunnel", "")
	cage(client, up)
}

func (s *HTTPProxyServer) mitmCONNECT(client net.Conn, dest netplugin.Request) {
	leaf, err := s.MITM.LeafForHost(dest.Host)
	if err != nil {
		s.log(dest, "FAIL", "", err.Error())
		return
	}
	tlsCfg := &tls.Config{
		Certificates: []tls.Certificate{leaf},
		NextProtos:   []string{"http/1.1"},
	}
	srv := tls.Server(client, tlsCfg)
	if err := srv.Handshake(); err != nil {
		s.log(dest, "FAIL", "", "mitm handshake: "+err.Error())
		return
	}
	defer srv.Close()

	br := bufio.NewReader(srv)
	for {
		httpReq, err := http.ReadRequest(br)
		if err != nil {
			return
		}
		if err := s.proxyMITMRequest(srv, httpReq, dest); err != nil {
			return
		}
		if httpReq.Close || !httpReq.ProtoAtLeast(1, 1) {
			return
		}
	}
}

func (s *HTTPProxyServer) proxyMITMRequest(client net.Conn, httpReq *http.Request, dest netplugin.Request) error {
	path := httpReq.URL.Path
	if path == "" {
		path = "/"
	}
	checkPath := path
	nreq := netplugin.Request{
		Host:   dest.Host,
		Port:   dest.Port,
		Method: httpReq.Method,
		Path:   checkPath,
	}
	if s.Pipeline != nil {
		d, err := s.Pipeline.Check(nreq)
		if err != nil {
			s.log(nreq, "FAIL", "", err.Error())
			writeProxyHTTPDeny(client, s, nreq, "")
			return err
		}
		if !d.Allow {
			s.log(nreq, "DENY", d.Reason, "")
			writeProxyHTTPDeny(client, s, nreq, d.Reason)
			return fmt.Errorf("denied")
		}
	}

	hdr := httpReq.Header.Clone()
	reason := "mitm"
	if ep := s.endpointForHost(dest.Host); ep != "" && s.Terminate != nil {
		out, err := s.Terminate.Prepare(netplugin.PrepareIn{
			Endpoint: ep,
			Method:   httpReq.Method,
			Path:     httpReq.URL.RequestURI(),
			Header:   hdr,
		})
		if err != nil {
			s.log(nreq, "FAIL", "", err.Error())
			_, _ = io.WriteString(client, "HTTP/1.1 502 Bad Gateway\r\n\r\n")
			return err
		}
		hdr = out.Header
		reason = "mitm:" + ep
	}

	upURL := &url.URL{
		Scheme:   "https",
		Host:     net.JoinHostPort(dest.Host, strconv.Itoa(dest.Port)),
		Path:     httpReq.URL.Path,
		RawQuery: httpReq.URL.RawQuery,
	}
	if dest.Port == 443 {
		upURL.Host = dest.Host
	}
	upReq, err := http.NewRequestWithContext(context.Background(), httpReq.Method, upURL.String(), httpReq.Body)
	if err != nil {
		return err
	}
	upReq.Header = hdr
	upReq.Host = dest.Host

	tlsCfg := &tls.Config{ServerName: dest.Host}
	if s.UpstreamTLS != nil {
		tlsCfg = s.UpstreamTLS.Clone()
		if tlsCfg.ServerName == "" {
			tlsCfg.ServerName = dest.Host
		}
	}
	transport := &http.Transport{
		TLSClientConfig: tlsCfg,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			if s.Dial != nil {
				return s.Dial(ctx, dest)
			}
			if s.Pipeline != nil {
				return s.Pipeline.Open(ctx, dest)
			}
			var d net.Dialer
			return d.DialContext(ctx, network, address)
		},
	}
	clientHTTP := &http.Client{Transport: transport, Timeout: 120 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := clientHTTP.Do(upReq)
	if err != nil {
		s.log(nreq, "FAIL", "", err.Error())
		_, _ = io.WriteString(client, "HTTP/1.1 502 Bad Gateway\r\n\r\n")
		return err
	}
	defer resp.Body.Close()
	s.log(nreq, "ALLOW", reason, "")
	if err := resp.Write(client); err != nil {
		return err
	}
	return nil
}

func (s *HTTPProxyServer) handlePlainHTTP(c net.Conn, br *bufio.Reader, req *http.Request) {
	host, port, err := hostPortFromRequest(req)
	if err != nil {
		_, _ = io.WriteString(c, "HTTP/1.1 400 Bad Request\r\n\r\n")
		return
	}
	path := req.URL.Path
	if path == "" {
		path = "/"
	}
	nreq := netplugin.Request{Host: host, Port: port, Method: req.Method, Path: path}
	if s.Pipeline != nil {
		d, err := s.Pipeline.Check(nreq)
		if err != nil {
			s.log(nreq, "FAIL", "", err.Error())
			_, _ = io.WriteString(c, "HTTP/1.1 502 Bad Gateway\r\n\r\n")
			return
		}
		if !d.Allow {
			s.log(nreq, "DENY", d.Reason, "")
			writeProxyHTTPDeny(c, s, nreq, d.Reason)
			return
		}
	}
	upURL := req.URL
	if !upURL.IsAbs() {
		upURL = &url.URL{Scheme: "http", Host: net.JoinHostPort(host, strconv.Itoa(port)), Path: req.URL.Path, RawQuery: req.URL.RawQuery}
	}
	upReq, err := http.NewRequestWithContext(context.Background(), req.Method, upURL.String(), req.Body)
	if err != nil {
		return
	}
	upReq.Header = req.Header.Clone()
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			if s.Dial != nil {
				return s.Dial(ctx, nreq)
			}
			if s.Pipeline != nil {
				return s.Pipeline.Open(ctx, nreq)
			}
			var d net.Dialer
			return d.DialContext(ctx, network, address)
		},
	}
	cli := &http.Client{Transport: transport, Timeout: 120 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := cli.Do(upReq)
	if err != nil {
		s.log(nreq, "FAIL", "", err.Error())
		_, _ = io.WriteString(c, "HTTP/1.1 502 Bad Gateway\r\n\r\n")
		return
	}
	defer resp.Body.Close()
	s.log(nreq, "ALLOW", "", "")
	_ = resp.Write(c)
	_ = br
}

func (s *HTTPProxyServer) endpointForHost(host string) string {
	if s == nil || s.HostToEP == nil {
		return ""
	}
	return s.HostToEP[strings.ToLower(stripHostPort(host))]
}

func (s *HTTPProxyServer) log(req netplugin.Request, action, reason, errMsg string) {
	if s == nil || s.OnTraffic == nil {
		return
	}
	s.OnTraffic.Log(TrafficEvent{
		Action: action,
		Host:   req.Host,
		Port:   req.Port,
		Method: req.Method,
		Path:   req.Path,
		Reason: reason,
		Error:  errMsg,
	})
}

func writeProxyHTTPDeny(w io.Writer, s *HTTPProxyServer, req netplugin.Request, reason string) {
	enabled, message := s.denyHTTPSettings()
	if !enabled {
		_, _ = io.WriteString(w, "HTTP/1.1 403 Forbidden\r\nContent-Length: 0\r\n\r\n")
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
	resp := fmt.Sprintf("HTTP/1.1 403 Forbidden\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s", len(body), body)
	_, _ = io.WriteString(w, resp)
}

// ParseHostEndpointMap builds hostname → endpoint name from http-proxy yaml.
func ParseHostEndpointMap(raw []byte) (map[string]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var root map[string]any
	if err := yaml.Unmarshal(raw, &root); err != nil {
		return nil, err
	}
	out := map[string]string{}
	for name, v := range root {
		if name == "priority" || name == "package" {
			continue
		}
		m, ok := v.(map[string]any)
		if !ok {
			continue
		}
		u, _ := m["url"].(string)
		if u == "" {
			continue
		}
		parsed, err := url.Parse(u)
		if err != nil || parsed.Hostname() == "" {
			continue
		}
		out[strings.ToLower(parsed.Hostname())] = name
	}
	return out, nil
}

type bufConn struct {
	*bufio.Reader
	net.Conn
}

func (c bufConn) Read(p []byte) (int, error) { return c.Reader.Read(p) }

func splitHostPortDefault(hostport string, defPort int) (string, int, error) {
	if hostport == "" {
		return "", 0, fmt.Errorf("empty host")
	}
	if !strings.Contains(hostport, ":") {
		return hostport, defPort, nil
	}
	h, p, err := net.SplitHostPort(hostport)
	if err != nil {
		// IPv6 without port
		if ip := net.ParseIP(strings.Trim(hostport, "[]")); ip != nil {
			return ip.String(), defPort, nil
		}
		return "", 0, err
	}
	port, err := strconv.Atoi(p)
	if err != nil {
		return "", 0, err
	}
	return h, port, nil
}

func hostPortFromRequest(req *http.Request) (string, int, error) {
	if req.URL.IsAbs() && req.URL.Host != "" {
		return splitHostPortDefault(req.URL.Host, 80)
	}
	if req.Host != "" {
		return splitHostPortDefault(req.Host, 80)
	}
	return "", 0, fmt.Errorf("no host")
}
