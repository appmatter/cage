package network

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// TrafficEvent is one SOCKS CONNECT outcome (JSONL for CLI follow + future UI).
type TrafficEvent struct {
	TS     time.Time `json:"ts"`
	Action string    `json:"action"` // ALLOW | DENY | FAIL | RELOAD | SOFTNET
	Host   string    `json:"host,omitempty"`
	Port   int       `json:"port,omitempty"`
	Method string    `json:"method,omitempty"`
	Path   string    `json:"path,omitempty"`
	Reason string    `json:"reason,omitempty"`
	Error  string    `json:"error,omitempty"`
}

// TrafficLogger records CONNECT events.
type TrafficLogger interface {
	Log(e TrafficEvent)
}

// ProxyLogPath is .cage/run/<vmID>/proxy.log under projectRoot.
func ProxyLogPath(projectRoot, vmID string) string {
	return filepath.Join(RunDir(projectRoot, vmID), "proxy.log")
}

// JSONLTrafficLogger appends one JSON object per line.
type JSONLTrafficLogger struct {
	mu sync.Mutex
	w  io.Writer
}

// NewJSONLTrafficLogger writes JSONL to w (caller owns close).
func NewJSONLTrafficLogger(w io.Writer) *JSONLTrafficLogger {
	return &JSONLTrafficLogger{w: w}
}

// Log writes one event. Sets TS if zero.
func (l *JSONLTrafficLogger) Log(e TrafficEvent) {
	if l == nil || l.w == nil {
		return
	}
	if e.TS.IsZero() {
		e.TS = time.Now().UTC()
	}
	b, err := json.Marshal(e)
	if err != nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	_, _ = l.w.Write(append(b, '\n'))
	if s, ok := l.w.(interface{ Sync() error }); ok {
		_ = s.Sync()
	}
}

// MultiTrafficLogger fans out to all loggers.
type MultiTrafficLogger []TrafficLogger

// Log calls each non-nil logger.
func (m MultiTrafficLogger) Log(e TrafficEvent) {
	for _, l := range m {
		if l != nil {
			l.Log(e)
		}
	}
}

// HumanTrafficLogger prints CLI-style lines (cage: proxy ALLOW …).
type HumanTrafficLogger struct {
	Print func(format string, args ...any)
}

// Log formats a human line.
func (h HumanTrafficLogger) Log(e TrafficEvent) {
	if h.Print == nil {
		return
	}
	h.Print("%s", FormatTrafficHuman(e))
}

// FormatTrafficHuman is the stderr / vm logs display line (without "cage: " prefix).
func FormatTrafficHuman(e TrafficEvent) string {
	switch e.Action {
	case "RELOAD":
		if e.Reason != "" {
			return fmt.Sprintf("proxy RELOAD egress (%s)", e.Reason)
		}
		return "proxy RELOAD egress"
	case "SOFTNET":
		if e.Host != "" && e.Port > 0 {
			dest := fmt.Sprintf("%s:%d", e.Host, e.Port)
			if e.Reason != "" {
				return fmt.Sprintf("proxy SOFTNET %s (%s)", dest, e.Reason)
			}
			return fmt.Sprintf("proxy SOFTNET %s", dest)
		}
		if e.Reason != "" {
			return fmt.Sprintf("proxy SOFTNET %s", e.Reason)
		}
		return "proxy SOFTNET"
	}
	dest := fmt.Sprintf("%s:%d", e.Host, e.Port)
	if e.Method != "" {
		dest += " " + e.Method
		if e.Path != "" {
			dest += " " + e.Path
		}
	}
	switch e.Action {
	case "ALLOW":
		return fmt.Sprintf("proxy ALLOW %s", dest)
	case "DENY":
		if e.Reason != "" {
			return fmt.Sprintf("proxy DENY %s (%s)", dest, e.Reason)
		}
		return fmt.Sprintf("proxy DENY %s", dest)
	case "FAIL":
		if e.Error != "" {
			return fmt.Sprintf("proxy FAIL %s: %s", dest, e.Error)
		}
		return fmt.Sprintf("proxy FAIL %s", dest)
	default:
		return fmt.Sprintf("proxy %s %s", e.Action, dest)
	}
}

// OpenProxyLog truncates and opens proxy.log for a new proxy-serve session.
func OpenProxyLog(projectRoot, vmID string) (*os.File, error) {
	dir := RunDir(projectRoot, vmID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return os.OpenFile(ProxyLogPath(projectRoot, vmID), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
}

// AppendProxyTraffic appends one event to an existing proxy.log (O_APPEND).
func AppendProxyTraffic(projectRoot, vmID string, e TrafficEvent) error {
	dir := RunDir(projectRoot, vmID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(ProxyLogPath(projectRoot, vmID), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	NewJSONLTrafficLogger(f).Log(e)
	return nil
}

// WriteTrafficFollow prints existing JSONL as human lines, then optionally follows new lines.
func WriteTrafficFollow(path string, follow bool, printHuman func(string)) error {
	if printHuman == nil {
		printHuman = func(line string) { fmt.Println(line) }
	}
	emitLine := func(line string) {
		line = strings.TrimSpace(line)
		if line == "" {
			return
		}
		var e TrafficEvent
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			printHuman(line)
			return
		}
		printHuman(FormatTrafficHuman(e))
	}

	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("no proxy log at %s (set network.proxy.logging: true and restart the VM)", path)
		}
		return err
	}

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		emitLine(sc.Text())
	}
	if err := sc.Err(); err != nil {
		f.Close()
		return err
	}
	if !follow {
		f.Close()
		return nil
	}

	offset, err := f.Seek(0, io.SeekCurrent)
	f.Close()
	if err != nil {
		return err
	}

	var partial []byte
	for {
		f, err := os.Open(path)
		if err != nil {
			if os.IsNotExist(err) {
				time.Sleep(200 * time.Millisecond)
				continue
			}
			return err
		}
		st, err := f.Stat()
		if err != nil {
			f.Close()
			return err
		}
		if st.Size() < offset {
			offset = 0
			partial = nil
		}
		if _, err := f.Seek(offset, io.SeekStart); err != nil {
			f.Close()
			return err
		}
		chunk, err := io.ReadAll(f)
		f.Close()
		if err != nil {
			return err
		}
		if len(chunk) == 0 {
			time.Sleep(200 * time.Millisecond)
			continue
		}
		offset += int64(len(chunk))
		partial = append(partial, chunk...)
		for {
			i := indexByte(partial, '\n')
			if i < 0 {
				break
			}
			emitLine(string(partial[:i]))
			partial = partial[i+1:]
		}
	}
}

func indexByte(b []byte, c byte) int {
	for i, x := range b {
		if x == c {
			return i
		}
	}
	return -1
}
