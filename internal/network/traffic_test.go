package network

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestJSONLTrafficLoggerAndFollow(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "proxy.log")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	l := NewJSONLTrafficLogger(f)
	l.Log(TrafficEvent{TS: time.Unix(1, 0).UTC(), Action: "ALLOW", Host: "a.example", Port: 443})
	l.Log(TrafficEvent{TS: time.Unix(2, 0).UTC(), Action: "DENY", Host: "b.example", Port: 80, Reason: "nope"})
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 2 {
		t.Fatalf("lines=%v", lines)
	}
	var e TrafficEvent
	if err := json.Unmarshal([]byte(lines[0]), &e); err != nil || e.Action != "ALLOW" {
		t.Fatalf("line0=%s err=%v", lines[0], err)
	}

	var out []string
	if err := WriteTrafficFollow(path, false, func(s string) { out = append(out, s) }); err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 || !strings.Contains(out[0], "ALLOW a.example:443") || !strings.Contains(out[1], "DENY") {
		t.Fatalf("out=%v", out)
	}
}

func TestFormatTrafficHuman(t *testing.T) {
	if got := FormatTrafficHuman(TrafficEvent{Action: "ALLOW", Host: "h", Port: 1}); got != "proxy ALLOW h:1" {
		t.Fatal(got)
	}
	if got := FormatTrafficHuman(TrafficEvent{Action: "DENY", Host: "api.github.com", Port: 443, Method: "CONNECT", Reason: "no allow rule matched"}); got != "proxy DENY api.github.com:443 CONNECT (no allow rule matched)" {
		t.Fatal(got)
	}
	if got := FormatTrafficHuman(TrafficEvent{Action: "ALLOW", Host: "api.openai.com", Port: 443, Method: "POST", Path: "/v1/chat/completions"}); got != "proxy ALLOW api.openai.com:443 POST /v1/chat/completions" {
		t.Fatal(got)
	}
	if got := FormatTrafficHuman(TrafficEvent{Action: "SOFTNET", Reason: "host-only active"}); got != "proxy SOFTNET host-only active" {
		t.Fatal(got)
	}
	if got := FormatTrafficHuman(TrafficEvent{Action: "SOFTNET", Host: "1.1.1.1", Port: 53, Reason: "direct egress blocked (expected)"}); got != "proxy SOFTNET 1.1.1.1:53 (direct egress blocked (expected))" {
		t.Fatal(got)
	}
}

func TestAppendProxyTraffic(t *testing.T) {
	dir := t.TempDir()
	// ProxyLogPath joins RunDir; use a fake project with .cage/run layout via writing directly.
	vmID := "vm1"
	run := filepath.Join(dir, ".cage", "run", vmID)
	if err := os.MkdirAll(run, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := AppendProxyTraffic(dir, vmID, SoftnetActiveEvent()); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(ProxyLogPath(dir, vmID))
	if err != nil {
		t.Fatal(err)
	}
	var e TrafficEvent
	if err := json.Unmarshal(bytesTrimLine(raw), &e); err != nil || e.Action != "SOFTNET" {
		t.Fatalf("raw=%s err=%v", raw, err)
	}
}

func bytesTrimLine(b []byte) []byte {
	return []byte(strings.TrimSpace(string(b)))
}
