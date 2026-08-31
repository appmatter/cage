package termlog

import (
	"bytes"
	"fmt"
	"io"
	"os"

	"github.com/fatih/color"
)

var (
	cli    = color.New(color.FgCyan, color.Bold)
	plugin = color.New(color.FgHiMagenta)
	guest  = color.New(color.FgHiGreen)
)

// CLI writes a cage host-side status line.
func CLI(format string, args ...any) {
	_, _ = cli.Fprintf(os.Stderr, "cage: "+format+"\n", args...)
}

// Plugin writes a runtime-plugin status line (e.g. tart: …).
func Plugin(name, format string, args ...any) {
	_, _ = plugin.Fprintf(os.Stderr, name+": "+format+"\n", args...)
}

// GuestWriter prefixes each line for guest/script output (apt, on-create, …).
// Raw (uncolored, no prefix) bytes are also written to raw if non-nil (host log file).
func GuestWriter(raw io.Writer) io.Writer {
	return &linePrefixWriter{
		out:    os.Stderr,
		raw:    raw,
		prefix: guest.Sprint("guest | "),
	}
}

type linePrefixWriter struct {
	out    io.Writer
	raw    io.Writer
	prefix string
	buf    []byte
}

func (w *linePrefixWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			break
		}
		line := w.buf[:i+1]
		w.buf = w.buf[i+1:]
		if w.raw != nil {
			if _, err := w.raw.Write(line); err != nil {
				return len(p), err
			}
		}
		if _, err := fmt.Fprintf(w.out, "%s%s", w.prefix, line); err != nil {
			return len(p), err
		}
		if s, ok := w.out.(interface{ Sync() error }); ok {
			_ = s.Sync()
		}
	}
	return len(p), nil
}
