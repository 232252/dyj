package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/232252/dyj/pkg/admin"
	"github.com/232252/dyj/pkg/ipp"
)

// callOpen drives n.apiOpen with the given method/body and decodes the
// JSON answer into a map.
func callOpen(t *testing.T, n *node, method, body string) (int, map[string]any) {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, "/admin/api/open", nil)
	} else {
		r = httptest.NewRequest(method, "/admin/api/open", strings.NewReader(body))
	}
	w := httptest.NewRecorder()
	n.apiOpen(w, r)
	out := map[string]any{}
	if w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode %s response: %v (%s)", method, err, w.Body.String())
		}
	}
	return w.Code, out
}

// openField reads a bool out of the decoded answer.
func openField(t *testing.T, st map[string]any, key string) bool {
	t.Helper()
	v, ok := st[key].(bool)
	if !ok {
		t.Fatalf("%s missing/ungtyped in %v", key, st)
	}
	return v
}

func openRemain(t *testing.T, st map[string]any) int {
	t.Helper()
	v, ok := st["remain_sec"].(float64)
	if !ok {
		t.Fatalf("remain_sec missing/ungtyped in %v", st)
	}
	return int(v)
}

// TestOpenWindowLifecycle walks the runtime window: default 10 minutes,
// fake-clock expiry, clamping, and the strict rule that a window POST
// never touches service.json.
func TestOpenWindowLifecycle(t *testing.T) {
	n, _, advance := newGateNode(t)
	writes := stubPersist(t)

	code, st := callOpen(t, n, http.MethodGet, "")
	if code != http.StatusOK || openField(t, st, "enabled") {
		t.Fatalf("initial: %d %v", code, st)
	}

	code, st = callOpen(t, n, http.MethodPost, `{"duration_sec":600}`)
	if code != http.StatusOK || !openField(t, st, "enabled") || openField(t, st, "always") {
		t.Fatalf("POST 600: %d %v", code, st)
	}
	if r := openRemain(t, st); r != 600 {
		t.Fatalf("remain after POST 600 = %d, want 600", r)
	}

	advance(601 * time.Second)
	_, st = callOpen(t, n, http.MethodGet, "")
	if openField(t, st, "enabled") {
		t.Fatalf("expired window still enabled: %v", st)
	}

	_, st = callOpen(t, n, http.MethodPost, `{"duration_sec":99999}`)
	if r := openRemain(t, st); r > 3600 {
		t.Fatalf("clamp broken: remain %d", r)
	}

	_, st = callOpen(t, n, http.MethodPost, `{}`)
	if r := openRemain(t, st); r != 600 {
		t.Fatalf("default duration = %d, want 600", r)
	}

	if code, _ := callOpen(t, n, http.MethodPost, `not-json`); code != http.StatusBadRequest {
		t.Fatalf("bad body: %d, want 400", code)
	}

	if len(writes()) != 0 {
		t.Fatalf("window POST must not persist, wrote %d times", len(writes()))
	}
}

// TestOpenAlwaysPersistsAndDisableClears covers the persisted always-on
// flag: POST persists open_print=true, DELETE clears the flag AND the
// window and persists the removal (omitempty drops the key).
func TestOpenAlwaysPersistsAndDisableClears(t *testing.T) {
	n, _, _ := newGateNode(t)
	writes := stubPersist(t)

	code, st := callOpen(t, n, http.MethodPost, `{"always":true}`)
	if code != http.StatusOK || !openField(t, st, "enabled") || !openField(t, st, "always") {
		t.Fatalf("POST always: %d %v", code, st)
	}
	if len(writes()) != 1 || !strings.Contains(writes()[0], `"open_print": true`) {
		t.Fatalf("always POST must persist open_print=true, got %q", writes())
	}

	// 常开时 remain 恒为 0(无倒计时)。
	if r := openRemain(t, st); r != 0 {
		t.Fatalf("always-on remain = %d, want 0", r)
	}

	// DELETE 双清:窗口与标志一起消失,并持久化回写。
	if code, st := callOpen(t, n, http.MethodDelete, ""); code != http.StatusOK ||
		openField(t, st, "enabled") || openField(t, st, "always") {
		t.Fatalf("DELETE: %d %v", code, st)
	}
	if len(writes()) != 2 || strings.Contains(writes()[1], "open_print") {
		t.Fatalf("DELETE must persist the flag away, got %q", writes())
	}

	// 再删一次无事可做,也不该再写盘。
	callOpen(t, n, http.MethodDelete, "")
	if len(writes()) != 2 {
		t.Fatalf("idempotent DELETE must not persist, wrote %d times", len(writes()))
	}
}

// stubPersist swaps the persistOptions indirection for a recording stub
// and restores it on cleanup; the returned getter reads the writes so far.
func stubPersist(t *testing.T) func() []string {
	t.Helper()
	var writes []string
	old := persistOptions
	persistOptions = func(raw string) error {
		writes = append(writes, raw)
		return nil
	}
	t.Cleanup(func() { persistOptions = old })
	return func() []string { return writes }
}

// stubSpool is the minimal SpoolControl so admin.New accepts the wiring
// (the open endpoint tests never drive jobs).
type stubSpool struct{}

func (stubSpool) Status() (bool, int32, []string) { return true, 0, nil }
func (stubSpool) SetAccepting(bool)               {}
func (stubSpool) Snapshot(string) []ipp.JobView   { return nil }
func (stubSpool) Cancel(int32) error              { return ipp.ErrUnknownJob }

// TestOpenEndpointGatedForRemote pins the management-plane rule: opening
// the data plane never opens the admin API — a remote client still needs
// X-Dyj-Token for POST /api/open, while status stays remotely readable.
func TestOpenEndpointGatedForRemote(t *testing.T) {
	n, _, _ := newGateNode(t)
	h := admin.New(admin.Options{
		StatusFn: n.adminStatus,
		Spool:    stubSpool{},
		Token:    "testtoken",
		Extra:    n.extraAPIs(),
	})

	remote := func(method, body string, hdr map[string]string) *http.Request {
		var r *http.Request
		if body == "" {
			r = httptest.NewRequest(method, "/admin/api/open", nil)
		} else {
			r = httptest.NewRequest(method, "/admin/api/open", strings.NewReader(body))
		}
		r.RemoteAddr = "10.0.0.5:5555"
		for k, v := range hdr {
			r.Header.Set(k, v)
		}
		return r
	}

	w := httptest.NewRecorder()
	h.ServeHTTP(w, remote(http.MethodPost, `{"duration_sec":600}`, nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("remote POST open without token: %d, want 401", w.Code)
	}
	if n.openActive() {
		t.Fatal("denied POST must not open the window")
	}

	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, remote(http.MethodPost, `{"duration_sec":600}`, map[string]string{"X-Dyj-Token": "testtoken"}))
	if w2.Code != http.StatusOK {
		t.Fatalf("remote POST open with token: %d", w2.Code)
	}
	if !n.openActive() {
		t.Fatal("allowed POST must open the window")
	}

	// status 是远程唯一只读面:无令牌可读,并透出开放状态(供 LAN 侧看见)。
	r4 := httptest.NewRequest(http.MethodGet, "/admin/api/status", nil)
	r4.RemoteAddr = "10.0.0.5:5555"
	w4 := httptest.NewRecorder()
	h.ServeHTTP(w4, r4)
	if w4.Code != http.StatusOK {
		t.Fatalf("remote GET status: %d", w4.Code)
	}
	if !strings.Contains(w4.Body.String(), `"open_printing":true`) {
		t.Fatalf("status must expose open_printing, got %s", w4.Body.String())
	}
	n.openDisable()

	// open 端点整体属于控制面:远程连 GET 也要令牌。
	w3 := httptest.NewRecorder()
	h.ServeHTTP(w3, remote(http.MethodGet, "", nil))
	if w3.Code != http.StatusUnauthorized {
		t.Fatalf("remote GET open: %d, want 401", w3.Code)
	}
}
