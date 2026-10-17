package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/232252/dyj/pkg/ipp"
)

// fakeSpool records admin-API actions.
type fakeSpool struct {
	mu        sync.Mutex
	accepting bool
	queued    int32
	canceled  []int32
	jobs      []ipp.JobView
}

func newFakeSpool() *fakeSpool {
	return &fakeSpool{
		accepting: true,
		jobs: []ipp.JobView{
			{ID: 1, State: ipp.JobStateProcessing, Reasons: []string{"job-printing"}, Name: "report", User: "alice", Format: "image/pwg-raster", Copies: 1, SizeKOctets: 12},
			{ID: 2, State: ipp.JobStateCompleted, Reasons: []string{"job-completed-successfully"}, Name: "scan", User: "bob", Format: "application/pdf", Copies: 2, SizeKOctets: 300},
		},
	}
}

func (f *fakeSpool) Status() (bool, int32, []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.accepting {
		return false, f.queued, []string{"paused"}
	}
	return true, f.queued, nil
}

func (f *fakeSpool) SetAccepting(a bool) { f.mu.Lock(); f.accepting = a; f.mu.Unlock() }
func (f *fakeSpool) Snapshot(which string) []ipp.JobView {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]ipp.JobView(nil), f.jobs...)
}
func (f *fakeSpool) Cancel(id int32) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.canceled {
		if c == id {
			return nil
		}
	}
	f.canceled = append(f.canceled, id)
	return nil
}

func newTestHandler(t *testing.T, token string) (*Handler, *fakeSpool, *LogRing) {
	t.Helper()
	spool := newFakeSpool()
	ring := NewLogRing(100)
	ring.Write([]byte("dyj: line one\n"))
	ring.Write([]byte("dyj: line two\n"))
	h := New(Options{
		StatusFn: func() Status {
			return Status{
				Version: "test", PrinterName: "Test Printer", Backend: "file(null)",
				Port: 6631, URIs: []string{"ipp://127.0.0.1:6631/ipp/print"},
				AdminURL: "http://127.0.0.1:6631/admin", MDNS: true,
			}
		},
		ConfigGet: func() any { return map[string]any{"port": 6631, "name": "Test Printer"} },
		ConfigPut: func(raw []byte) error {
			var cfg map[string]any
			if err := json.Unmarshal(raw, &cfg); err != nil {
				return err
			}
			if p, ok := cfg["port"].(float64); !ok || p < 1 || p > 65535 {
				return fmt.Errorf("端口 %v 超出范围", cfg["port"])
			}
			return nil
		},
		Spool: spool, Logs: ring, Token: token,
		Diagnostics: func(any) any { return []map[string]string{{"name": "port", "level": "ok"}} },
		Web:         DefaultWeb(),
	})
	return h, spool, ring
}

// remoteRequest fakes a LAN client (httptest.NewRequest defaults to
// 192.0.2.1, which is NOT loopback).
func remoteRequest(method, target string, body io.Reader) *http.Request {
	return httptest.NewRequest(method, target, body)
}

func loopbackRequest(method, target string, body io.Reader) *http.Request {
	r := httptest.NewRequest(method, target, body)
	r.RemoteAddr = "127.0.0.1:54321"
	return r
}

func doJSON(t *testing.T, h *Handler, r *http.Request) (int, map[string]any) {
	t.Helper()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	var out map[string]any
	if w.Body.Len() > 0 {
		_ = json.Unmarshal(w.Body.Bytes(), &out)
	}
	return w.Code, out
}

func TestStatusMergesStaticAndDynamic(t *testing.T) {
	h, spool, _ := newTestHandler(t, "")
	code, out := doJSON(t, h, remoteRequest(http.MethodGet, "/admin/api/status", nil))
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if out["printer_name"] != "Test Printer" || out["version"] != "test" {
		t.Errorf("static fields missing: %v", out)
	}
	if accepting, _ := out["accepting"].(bool); !accepting {
		t.Errorf("accepting = %v", out["accepting"])
	}
	if _, ok := out["reasons"].([]any); !ok {
		t.Errorf("reasons should be a list: %v", out["reasons"])
	}
	_ = spool
}

func TestRemoteReadsAllowedWritesNeedToken(t *testing.T) {
	h, spool, _ := newTestHandler(t, "abc123")

	// Read-only pair: allowed without token.
	for _, ep := range []string{"/admin/api/status", "/admin/api/jobs"} {
		code, _ := doJSON(t, h, remoteRequest(http.MethodGet, ep, nil))
		if code != http.StatusOK {
			t.Errorf("GET %s from LAN = %d, want 200", ep, code)
		}
	}
	// Writes without token: 401.
	code, _ := doJSON(t, h, remoteRequest(http.MethodPost, "/admin/api/pause", nil))
	if code != http.StatusUnauthorized {
		t.Fatalf("remote pause without token = %d, want 401", code)
	}
	// Writes with the wrong token: 401.
	req := remoteRequest(http.MethodPost, "/admin/api/pause", nil)
	req.Header.Set("X-Dyj-Token", "wrong")
	code, _ = doJSON(t, h, req)
	if code != http.StatusUnauthorized {
		t.Fatalf("remote pause with wrong token = %d, want 401", code)
	}
	// Writes with the right token: 200, and it took effect.
	req = remoteRequest(http.MethodPost, "/admin/api/pause", nil)
	req.Header.Set("X-Dyj-Token", "abc123")
	code, _ = doJSON(t, h, req)
	if code != http.StatusOK {
		t.Fatalf("remote pause with token = %d", code)
	}
	if accepting, _, _ := spool.Status(); accepting {
		t.Error("pause did not take effect")
	}
	// Log stream also token-gated from LAN.
	code, _ = doJSON(t, h, remoteRequest(http.MethodGet, "/admin/api/logs", nil))
	if code != http.StatusUnauthorized {
		t.Errorf("remote logs without token = %d, want 401", code)
	}
}

func TestLoopbackHasFullAccess(t *testing.T) {
	h, spool, _ := newTestHandler(t, "abc123")
	code, _ := doJSON(t, h, loopbackRequest(http.MethodPost, "/admin/api/pause", nil))
	if code != http.StatusOK {
		t.Fatalf("loopback pause = %d, want 200", code)
	}
	if accepting, _, _ := spool.Status(); accepting {
		t.Error("loopback pause did not take effect")
	}
	code, _ = doJSON(t, h, loopbackRequest(http.MethodPost, "/admin/api/resume", nil))
	if code != http.StatusOK {
		t.Fatalf("loopback resume = %d", code)
	}
	if accepting, _, _ := spool.Status(); !accepting {
		t.Error("loopback resume did not take effect")
	}
}

func TestJobsAndCancel(t *testing.T) {
	h, spool, _ := newTestHandler(t, "")
	code, out := doJSON(t, h, remoteRequest(http.MethodGet, "/admin/api/jobs", nil))
	if code != http.StatusOK {
		t.Fatalf("jobs = %d", code)
	}
	jobs := out["jobs"].([]any)
	if len(jobs) != 2 {
		t.Fatalf("got %d jobs", len(jobs))
	}
	first := jobs[0].(map[string]any)
	if first["state_name"] != "processing" || first["user"] != "alice" {
		t.Errorf("job json wrong: %v", first)
	}

	code, out = doJSON(t, h, loopbackRequest(http.MethodPost, "/admin/api/jobs/1/cancel", nil))
	if code != http.StatusOK {
		t.Fatalf("cancel = %d (%v)", code, out)
	}
	if len(spool.canceled) != 1 || spool.canceled[0] != 1 {
		t.Errorf("canceled = %v", spool.canceled)
	}
	code, _ = doJSON(t, h, loopbackRequest(http.MethodPost, "/admin/api/jobs/0/cancel", nil))
	if code != http.StatusBadRequest {
		t.Errorf("cancel 0 = %d, want 400", code)
	}
}

func TestConfigRoundTrip(t *testing.T) {
	h, _, _ := newTestHandler(t, "")
	code, out := doJSON(t, h, loopbackRequest(http.MethodGet, "/admin/api/config", nil))
	if code != http.StatusOK || out["config"].(map[string]any)["port"].(float64) != 6631 {
		t.Fatalf("config get = (%d, %v)", code, out)
	}

	bad := strings.NewReader(`{"port": 99999}`)
	code, out = doJSON(t, h, loopbackRequest(http.MethodPut, "/admin/api/config", bad))
	if code != http.StatusBadRequest {
		t.Fatalf("invalid config put = %d (%v)", code, out)
	}
	good := strings.NewReader(`{"port": 7000, "name": "x"}`)
	code, out = doJSON(t, h, loopbackRequest(http.MethodPut, "/admin/api/config", good))
	if code != http.StatusOK || out["restart_required"] != true {
		t.Fatalf("config put = (%d, %v)", code, out)
	}
}

func TestDiagnosticsEndpoint(t *testing.T) {
	h, _, _ := newTestHandler(t, "t")
	req := remoteRequest(http.MethodGet, "/admin/api/diagnostics", nil)
	req.Header.Set("X-Dyj-Token", "t")
	code, out := doJSON(t, h, req)
	if code != http.StatusOK {
		t.Fatalf("diagnostics = %d", code)
	}
	checks := out["checks"].([]any)
	if len(checks) != 1 {
		t.Fatalf("checks = %v", checks)
	}
}

func TestLogRingAndSSESnapshot(t *testing.T) {
	h, _, ring := newTestHandler(t, "")
	// Snapshot phase of the SSE stream is written synchronously before
	// the handler blocks on live updates; use a request context that we
	// cancel shortly after.
	ctx, cancel := context.WithCancel(context.Background())
	req := loopbackRequest(http.MethodGet, "/admin/api/logs", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		h.ServeHTTP(w, req)
		close(done)
	}()
	deadline := time.Now().Add(2 * time.Second)
	for w.Body.Len() == 0 || !strings.Contains(w.Body.String(), "line two") {
		if time.Now().After(deadline) {
			t.Fatalf("SSE snapshot never arrived: %q", w.Body.String())
		}
		time.Sleep(5 * time.Millisecond)
	}
	ring.Write([]byte("dyj: line three\n"))
	if !strings.Contains(w.Body.String(), "line three") {
		// live line may take a moment to flush
		time.Sleep(50 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not return after context cancel")
	}
	body := w.Body.String()
	if !strings.Contains(body, "data: dyj: line one") || !strings.Contains(body, "line three") {
		t.Errorf("SSE body missing lines: %q", body)
	}
}

func TestLogRingCapacityAndSubscribe(t *testing.T) {
	r := NewLogRing(3)
	for i := 0; i < 5; i++ {
		r.Write([]byte(fmt.Sprintf("line-%d\n", i)))
	}
	snap := r.Snapshot()
	if len(snap) != 3 || snap[0] != "line-2" || snap[2] != "line-4" {
		t.Fatalf("ring = %v", snap)
	}
	live, stop := r.Subscribe()
	r.Write([]byte("live-1\n"))
	select {
	case line := <-live:
		if line != "live-1" {
			t.Fatalf("live line = %q", line)
		}
	case <-time.After(time.Second):
		t.Fatal("no live line")
	}
	r.Unsubscribe(stop)
	r.Write([]byte("live-2\n")) // must not panic after unsubscribe
}

func TestTokenLoadOrCreate(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/admin-token"
	tok1, err := LoadOrCreateToken(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(tok1) != 64 {
		t.Fatalf("token length = %d, want 64 hex chars", len(tok1))
	}
	tok2, err := LoadOrCreateToken(path)
	if err != nil || tok1 != tok2 {
		t.Fatalf("token not stable: %q vs %q (%v)", tok1, tok2, err)
	}
}

func TestStaticServesIndex(t *testing.T) {
	h, _, _ := newTestHandler(t, "")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, loopbackRequest(http.MethodGet, "/admin/", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("index = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "<!doctype html>") {
		t.Errorf("index content wrong: %q", w.Body.String()[:min(40, w.Body.Len())])
	}
}

// TestSlashlessAdminRedirects guards the trailing-slash redirect: without
// it the browser resolves relative module URLs against the server root
// and the UI white-screens (live smoke catch).
func TestSlashlessAdminRedirects(t *testing.T) {
	h, _, _ := newTestHandler(t, "")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, loopbackRequest(http.MethodGet, "/admin", nil))
	if w.Code != http.StatusMovedPermanently {
		t.Fatalf("code = %d, want 301", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/admin/" {
		t.Fatalf("Location = %q, want /admin/", loc)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
