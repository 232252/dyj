package logx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestLogger(buf *strings.Builder, level Level) *Logger {
	l := New(buf, level)
	l.now = func() time.Time { return time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC) }
	return l
}

func TestEventFormat(t *testing.T) {
	var b strings.Builder
	l := newTestLogger(&b, LevelInfo)
	l.Info("pair.ok", "src", "192.168.1.23", "dur", 12*time.Millisecond, "msg", "paired")
	want := "2026-09-30T10:00:00Z lvl=info evt=pair.ok src=192.168.1.23 dur=12ms msg=paired\n"
	if got := b.String(); got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
}

func TestEventQuoting(t *testing.T) {
	cases := []struct {
		val  any
		want string
	}{
		{"plain", "plain"},
		{"", `""`},
		{"two words", `"two words"`},
		{`say "hi"`, `"say \"hi\""`},
		{"a=b", `"a=b"`},
		{"line1\nline2", `"line1\nline2"`},
		{42, "42"},
	}
	for _, c := range cases {
		var b strings.Builder
		l := newTestLogger(&b, LevelInfo)
		l.Info("t", "k", c.val)
		if !strings.HasSuffix(b.String(), " k="+c.want+"\n") {
			t.Errorf("k=%v: got %q, want suffix k=%s", c.val, b.String(), c.want)
		}
	}
}

func TestLevelFilter(t *testing.T) {
	var b strings.Builder
	l := newTestLogger(&b, LevelWarn)
	l.Debug("d", "k", 1)
	l.Info("i", "k", 1)
	l.Warn("w", "k", 1)
	l.Error("e", "k", 1)
	lines := strings.Split(strings.TrimSpace(b.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 lines (warn+error), got %d: %q", len(lines), b.String())
	}
	if !strings.Contains(lines[0], "evt=w") || !strings.Contains(lines[1], "evt=e") {
		t.Fatalf("unexpected lines: %q", lines)
	}
}

func TestRedactPath(t *testing.T) {
	cases := []struct{ in, want string }{
		{"/p/AB12CD34/ipp/print/epson", "/p/****/ipp/print/epson"},
		{"/ipp/print", "/ipp/print"},
		{"/p/****/ipp/print", "/p/****/ipp/print"}, // already masked
		{"no path at all", "no path at all"},
		{"/p/tok/ipp/print/key/p/tok2", "/p/****/ipp/print/key/p/****"},
	}
	for _, c := range cases {
		if got := RedactPath(c.in); got != c.want {
			t.Errorf("RedactPath(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestQuoteValueRedactsTokens(t *testing.T) {
	var b strings.Builder
	l := newTestLogger(&b, LevelInfo)
	l.Info("deny.ipp", "path", "/p/SECRET123/ipp/print")
	if strings.Contains(b.String(), "SECRET123") {
		t.Fatalf("token leaked into log: %q", b.String())
	}
	if !strings.Contains(b.String(), "/p/****/ipp/print") {
		t.Fatalf("path not masked: %q", b.String())
	}
}

func TestLegacyBridge(t *testing.T) {
	var b strings.Builder
	l := newTestLogger(&b, LevelInfo)
	lg := l.Legacy()
	lg.Printf("mdns: announced on %s", "eth0")
	if !strings.Contains(b.String(), `evt=legacy msg="mdns: announced on eth0"`) {
		t.Fatalf("legacy line not wrapped: %q", b.String())
	}
}

func TestRotatingWriter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dyj.log")
	w, err := OpenRotating(path, 64, 3)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		if _, err := w.Write([]byte(strings.Repeat("x", 20))); err != nil {
			t.Fatal(err)
		}
	}
	_ = w.Close()
	// Active file plus up to 3 generations must exist; the active file is
	// small (just rotated or partially filled), old content is in .1 …
	st, err := os.Stat(path)
	if err != nil {
		t.Fatalf("active log missing: %v", err)
	}
	if st.Size() > 64+20 {
		t.Fatalf("active file larger than max+one write: %d", st.Size())
	}
	for _, gen := range []string{path + ".1", path + ".2", path + ".3"} {
		if _, err := os.Stat(gen); err != nil {
			t.Fatalf("generation %s missing: %v", gen, err)
		}
	}
	if _, err := os.Stat(path + ".4"); err == nil {
		t.Fatal("generation .4 should have been rotated away")
	}
}

func TestRotatingWriterClosedDrops(t *testing.T) {
	dir := t.TempDir()
	w, err := OpenRotating(filepath.Join(dir, "a.log"), 1<<20, 2)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if n, err := w.Write([]byte("x")); err != nil || n != 1 {
		t.Fatalf("write after close must drop silently, got n=%d err=%v", n, err)
	}
}
