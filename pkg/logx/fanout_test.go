package logx

import (
	"errors"
	"strings"
	"testing"
)

// errWriter simulates a broken sink (the field report: a running service
// whose stderr/file sink failed, silencing every other sink because
// io.MultiWriter aborts on the first error).
type errWriter struct{ calls int }

func (w *errWriter) Write(p []byte) (int, error) {
	w.calls++
	return 0, errors.New("sink broken")
}

// TestFanoutSurvivesBrokenSink: one failing writer must not stop the
// others from receiving the line.
func TestFanoutSurvivesBrokenSink(t *testing.T) {
	bad := &errWriter{}
	var good strings.Builder
	w := Fanout(bad, &good)
	n, err := w.Write([]byte("hello\n"))
	if err != nil {
		t.Fatalf("Fanout must swallow sink errors, got %v", err)
	}
	if n != len("hello\n") {
		t.Fatalf("Fanout short write: n=%d", n)
	}
	if good.String() != "hello\n" {
		t.Fatalf("healthy sink lost the line: %q", good.String())
	}
	if bad.calls != 1 {
		t.Fatalf("broken sink should still be attempted once, got %d", bad.calls)
	}
}

// TestFanoutAllBrokenStillReportsWrite: even with every sink broken the
// logger must keep running (observability must not take down sharing).
func TestFanoutAllBrokenStillReportsWrite(t *testing.T) {
	w := Fanout(&errWriter{}, &errWriter{})
	n, err := w.Write([]byte("x"))
	if err != nil || n != 1 {
		t.Fatalf("all-broken Fanout: n=%d err=%v", n, err)
	}
}