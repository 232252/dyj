package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/232252/dyj/pkg/logx"
)

// repairNode builds the minimal node the repair handler needs. Firewall
// and pdf repairs are stubbed per-test — they shell out to netsh /
// package managers and must never run for real in unit tests.
func repairNode(t *testing.T, cfg serveOptions, stateDir string) *node {
	t.Helper()
	return &node{cfg: cfg, stateDir: stateDir, log: logx.New(io.Discard, logx.LevelDebug)}
}

func callRepair(t *testing.T, n *node, body string) (int, map[string]any) {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/admin/api/repair", strings.NewReader(body))
	w := httptest.NewRecorder()
	n.apiRepair(w, r)
	out := map[string]any{}
	if w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode repair response: %v (%s)", err, w.Body.String())
		}
	}
	return w.Code, out
}

func resultsOf(t *testing.T, out map[string]any) []repairOutcome {
	t.Helper()
	raw, err := json.Marshal(out["results"])
	if err != nil {
		t.Fatal(err)
	}
	var rs []repairOutcome
	if err := json.Unmarshal(raw, &rs); err != nil {
		t.Fatalf("results shape: %v (%s)", err, raw)
	}
	return rs
}

func stubRepair(t *testing.T, name string, fn repairFunc) {
	t.Helper()
	old := repairFuncs[name]
	repairFuncs[name] = fn
	t.Cleanup(func() { repairFuncs[name] = old })
}

func TestRepairMethodAndUnknownCheck(t *testing.T) {
	n := repairNode(t, serveOptions{}, t.TempDir())
	w := httptest.NewRecorder()
	n.apiRepair(w, httptest.NewRequest(http.MethodGet, "/admin/api/repair", nil))
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET = %d, want 405", w.Code)
	}
	code, _ := callRepair(t, n, `{"check":"nope"}`)
	if code != http.StatusBadRequest {
		t.Errorf("unknown check = %d, want 400", code)
	}
}

func TestRepairStateDirCreatesAndVerifies(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "lv1", "state")
	n := repairNode(t, serveOptions{}, dir)
	code, out := callRepair(t, n, `{"check":"state-dir"}`)
	if code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	rs := resultsOf(t, out)
	if len(rs) != 1 || rs[0].Check != "state-dir" || !rs[0].OK {
		t.Fatalf("unexpected results: %+v", rs)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("state dir not created: %v", err)
	}
}

func TestRepairTLS(t *testing.T) {
	// 未开启加密 → 无需修复。
	n := repairNode(t, serveOptions{}, t.TempDir())
	_, out := callRepair(t, n, `{"check":"tls"}`)
	rs := resultsOf(t, out)
	if len(rs) != 1 || !rs[0].OK || !strings.Contains(rs[0].Detail, "无需修复") {
		t.Fatalf("tls disabled: %+v", rs)
	}
	// 开启且证书缺失 → 真实生成一对文件。
	dir := t.TempDir()
	n = repairNode(t, serveOptions{TLSPort: 16632, Name: "dyj-test"}, dir)
	_, out = callRepair(t, n, `{"check":"tls"}`)
	rs = resultsOf(t, out)
	if len(rs) != 1 || !rs[0].OK || strings.Contains(rs[0].Detail, "无需修复") {
		t.Fatalf("tls fix: %+v", rs)
	}
	for _, f := range []string{"tls.crt", "tls.key"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("%s not generated: %v", f, err)
		}
	}
	// 证书已在 → 无需修复。
	_, out = callRepair(t, n, `{"check":"tls"}`)
	rs = resultsOf(t, out)
	if len(rs) != 1 || !rs[0].OK || !strings.Contains(rs[0].Detail, "无需修复") {
		t.Fatalf("tls already present: %+v", rs)
	}
}

func TestRepairAllFiltersSkipped(t *testing.T) {
	stubRepair(t, "firewall", func(rc repairCtx) repairOutcome {
		return repairOutcome{Check: "firewall", OK: true, Detail: "fake-fw", Skipped: true}
	})
	stubRepair(t, "pdf", func(rc repairCtx) repairOutcome {
		return repairOutcome{Check: "pdf", OK: true, Detail: "fake-pdf"}
	})
	n := repairNode(t, serveOptions{}, t.TempDir()) // tls 未开启 → skip
	_, out := callRepair(t, n, `{"check":"all"}`)
	rs := resultsOf(t, out)
	seen := map[string]bool{}
	for _, r := range rs {
		seen[r.Check] = true
	}
	if seen["firewall"] || seen["tls"] {
		t.Errorf("skipped repairs must be filtered from fix-all: %+v", rs)
	}
	if !seen["pdf"] || !seen["state-dir"] {
		t.Errorf("expected pdf + state-dir in results: %+v", rs)
	}
}
