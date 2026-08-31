package network

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	netplugin "github.com/appmatter/cage/pkg/plugin/v1/network"
)

type stubTerminate struct {
	out netplugin.PrepareOut
	err error
}

func (s *stubTerminate) Name() string           { return "stub" }
func (s *stubTerminate) Configure([]byte) error { return nil }
func (s *stubTerminate) Prepare(netplugin.PrepareIn) (netplugin.PrepareOut, error) {
	return s.out, s.err
}

func TestHTTPTerminateAllowAndDeny(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer x" {
			t.Errorf("auth %q", r.Header.Get("Authorization"))
		}
		_, _ = io.WriteString(w, "ok")
	}))
	defer upstream.Close()
	upPort := upstream.Listener.Addr().(*net.TCPAddr).Port

	term := &stubTerminate{out: netplugin.PrepareOut{
		UpstreamHost: "127.0.0.1",
		UpstreamPort: upPort,
		UpstreamURL:  upstream.URL + "/v1/chat",
		Header:       map[string][]string{"Authorization": {"Bearer x"}},
	}}

	var logs []TrafficEvent
	ht := &HTTPTerminate{
		Terminate: term,
		Pipeline: NewPipeline([]FilterSeat{{
			Priority: 1,
			Filter:   &stubFilter{allow: true, reason: "ok"},
		}}),
		OnTraffic: &captureTraffic{&logs},
		Client:    upstream.Client(),
	}
	if err := ht.Start([]HTTPEndpointListen{{Name: "openai", Listen: 0}}); err != nil {
		t.Fatal(err)
	}
	defer ht.Close()
	p := ht.Ports()["openai"]
	resp, err := http.Get("http://127.0.0.1:" + strconv.Itoa(p) + "/v1/chat")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || string(body) != "ok" {
		t.Fatalf("status=%d body=%q", resp.StatusCode, body)
	}

	ht2 := &HTTPTerminate{
		Terminate: term,
		Pipeline: NewPipeline([]FilterSeat{{
			Priority: 1,
			Filter:   &stubFilter{allow: false, reason: "nope"},
		}}),
		DenyHTTP:  true,
		OnTraffic: &captureTraffic{&logs},
	}
	if err := ht2.Start([]HTTPEndpointListen{{Name: "openai", Listen: 0}}); err != nil {
		t.Fatal(err)
	}
	defer ht2.Close()
	p2 := ht2.Ports()["openai"]
	resp, err = http.Post("http://127.0.0.1:"+strconv.Itoa(p2)+"/x", "text/plain", nil)
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 403 || !strings.Contains(string(body), "intentionally blocked") {
		t.Fatalf("deny status=%d body=%q", resp.StatusCode, body)
	}
}
