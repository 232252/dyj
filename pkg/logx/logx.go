// Package logx is the structured event logger behind dyj's observability
// contract (research/13 §5.1): every automation step emits a leveled,
// logfmt-style event line that stays human-readable in a plain editor.
//
//	2026-09-30T10:14:14+08:00 lvl=info evt=pair.ok src=192.168.1.23 device=9f3a msg="paired"
//
// Field dictionary (fixed semantics across the codebase): evt event name;
// src/dst addresses; printer key; qname system queue name; job job number;
// device paired-device id prefix; ifname interface name; rc subprocess exit
// code; dur duration; err error text. Redaction rule: pairing tokens and
// PINs never appear in logs — use RedactPath on any URI that may carry one.
package logx

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Level is the severity of an event line.
type Level int

// Levels, ordered.
const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

func (l Level) String() string {
	switch l {
	case LevelDebug:
		return "debug"
	case LevelInfo:
		return "info"
	case LevelWarn:
		return "warn"
	case LevelError:
		return "error"
	}
	return "info"
}

// ParseLevel maps a verbosity flag to a level (false → info, true → debug).
func ParseLevel(verbose bool) Level {
	if verbose {
		return LevelDebug
	}
	return LevelInfo
}

// Logger writes leveled event lines. It is safe for concurrent use.
type Logger struct {
	mu    sync.Mutex
	out   io.Writer
	level Level
	now   func() time.Time
}

// New builds a logger writing to out (multi-writer setups are the caller's
// job: pass io.MultiWriter(stderr, logRing, file)).
func New(out io.Writer, level Level) *Logger {
	return &Logger{out: out, level: level, now: time.Now}
}

// SetLevel raises or lowers the filter at runtime.
func (l *Logger) SetLevel(level Level) {
	l.mu.Lock()
	l.level = level
	l.mu.Unlock()
}

// SetOutput swaps the sink (used by tests).
func (l *Logger) SetOutput(w io.Writer) {
	l.mu.Lock()
	l.out = w
	l.mu.Unlock()
}

// Event writes one line. kv is key/value pairs (keys must be strings);
// odd trailing items are ignored. The special key msg goes last.
func (l *Logger) Event(level Level, evt string, kv ...any) {
	if level < l.levelSafe() {
		return
	}
	var b strings.Builder
	b.WriteString(l.now().Format(time.RFC3339))
	fmt.Fprintf(&b, " lvl=%s evt=%s", level, sanitizeKey(evt))
	var msg string
	hasMsg := false
	for i := 0; i+1 < len(kv); i += 2 {
		k, ok := kv[i].(string)
		if !ok {
			k = fmt.Sprint(kv[i])
		}
		if k == "msg" {
			if s, ok := kv[i+1].(string); ok {
				msg, hasMsg = s, true
			}
			continue
		}
		fmt.Fprintf(&b, " %s=%s", sanitizeKey(k), quoteValue(kv[i+1]))
	}
	if hasMsg {
		fmt.Fprintf(&b, " msg=%s", quoteValue(msg))
	}
	b.WriteByte('\n')

	l.mu.Lock()
	_, _ = l.out.Write([]byte(b.String()))
	l.mu.Unlock()
}

func (l *Logger) levelSafe() Level {
	l.mu.Lock()
	lv := l.level
	l.mu.Unlock()
	return lv
}

// Shorthands for the fixed levels.
func (l *Logger) Debug(evt string, kv ...any) { l.Event(LevelDebug, evt, kv...) }
func (l *Logger) Info(evt string, kv ...any)  { l.Event(LevelInfo, evt, kv...) }
func (l *Logger) Warn(evt string, kv ...any)  { l.Event(LevelWarn, evt, kv...) }
func (l *Logger) Error(evt string, kv ...any) { l.Event(LevelError, evt, kv...) }

// Legacy returns a *log.Logger that forwards each written line as one info
// event tagged evt=legacy. Existing components (spooler, discovery, drivers)
// keep their *log.Logger signature while their output lands in the same
// sinks and file as structured events.
func (l *Logger) Legacy() *log.Logger {
	return log.New(&legacyWriter{l: l}, "", 0)
}

type legacyWriter struct{ l *Logger }

func (w *legacyWriter) Write(p []byte) (int, error) {
	line := strings.TrimRight(string(p), "\n")
	w.l.Event(LevelInfo, "legacy", "msg", line)
	return len(p), nil
}

func sanitizeKey(k string) string {
	k = strings.TrimSpace(k)
	if k == "" {
		return "_"
	}
	var b strings.Builder
	for i := 0; i < len(k); i++ {
		c := k[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '_', c == '-', c == '.':
			b.WriteByte(c)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

func quoteValue(v any) string {
	var s string
	switch t := v.(type) {
	case string:
		s = t
	case error:
		s = t.Error()
	case time.Duration:
		s = t.String()
	case fmt.Stringer:
		s = t.String()
	default:
		s = fmt.Sprint(v)
	}
	// Tokens and PINs ride inside URIs; the redaction contract says they
	// must never reach a log line. Callers normally strip them earlier —
	// this is the belt to that suspenders.
	s = RedactPath(s)
	if s == "" {
		return `""`
	}
	needsQuote := strings.ContainsAny(s, " \"=") || strings.Contains(s, "\n")
	if !needsQuote {
		return s
	}
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\\', '"':
			b.WriteByte('\\')
			b.WriteByte(s[i])
		case '\n':
			b.WriteString("\\n")
		case '\t':
			b.WriteString("\\t")
		default:
			if s[i] < 0x20 {
				fmt.Fprintf(&b, "\\x%02x", s[i])
			} else {
				b.WriteByte(s[i])
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

// RedactPath masks a pairing token inside a URL path: /p/<token>/... becomes
// /p/****/... Values that are not paths pass through unchanged.
func RedactPath(s string) string {
	const marker = "/p/"
	var b strings.Builder
	i := 0
	for i < len(s) {
		j := strings.Index(s[i:], marker)
		if j < 0 {
			b.WriteString(s[i:])
			return b.String()
		}
		j += i
		b.WriteString(s[i : j+len(marker)])
		rest := s[j+len(marker):]
		end := len(rest)
		if e := strings.IndexByte(rest, '/'); e >= 0 {
			end = e
		}
		tok := rest[:end]
		if tok != "" && tok != "****" {
			b.WriteString("****")
		} else {
			b.WriteString(tok)
		}
		i = j + len(marker) + end
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// Rotating file sink
// ---------------------------------------------------------------------------

// RotatingWriter appends to path, renaming to path.1 … path.<keep> when the
// file exceeds maxBytes. Rotation keeps the last `keep` generations; the
// logger never blocks on rotation failure — a failed rotate drops the write
// rather than killing the process (observability must not take down sharing).
type RotatingWriter struct {
	mu   sync.Mutex
	path string
	max  int64
	keep int
	f    *os.File
	size int64
}

// OpenRotating creates (or appends to) path and its generations.
func OpenRotating(path string, maxBytes int64, keep int) (*RotatingWriter, error) {
	if maxBytes <= 0 {
		maxBytes = 10 << 20
	}
	if keep <= 0 {
		keep = 3
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	return &RotatingWriter{path: path, max: maxBytes, keep: keep, f: f, size: st.Size()}, nil
}

// Path reports the active file path (diagnostics).
func (w *RotatingWriter) Path() string { return w.path }

// Close flushes and closes the active file.
func (w *RotatingWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f == nil {
		return nil
	}
	err := w.f.Close()
	w.f = nil
	return err
}

func (w *RotatingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f == nil {
		// Sink already closed or rotation machinery broken: drop silently.
		return len(p), nil
	}
	if w.size+int64(len(p)) > w.max {
		w.rotateLocked()
	}
	n, err := w.f.Write(p)
	w.size += int64(n)
	return n, err
}

func (w *RotatingWriter) rotateLocked() {
	w.f.Close()
	for i := w.keep - 1; i >= 1; i-- {
		_ = os.Rename(genPath(w.path, i), genPath(w.path, i+1))
	}
	_ = os.Rename(w.path, genPath(w.path, 1))
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND|os.O_TRUNC, 0o644)
	if err != nil {
		w.f = nil // writes drop until the file becomes creatable again
		return
	}
	w.f = f
	w.size = 0
}

func genPath(path string, gen int) string {
	return fmt.Sprintf("%s.%d", path, gen)
}
