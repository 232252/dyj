package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/232252/dyj/pkg/attributes"
	"github.com/232252/dyj/pkg/discovery"
	"github.com/232252/dyj/pkg/driver"
	"github.com/232252/dyj/pkg/ipp"
	"github.com/232252/dyj/pkg/spooler"
)

// txtTestAdvertiser builds an advertiser that never binds sockets: the
// record builders work on an unstarted advertiser (hostFor falls back to
// the primary host), which is exactly what the TXT-sync tests need.
func txtTestAdvertiser(t *testing.T, pair string) discovery.Publisher {
	t.Helper()
	a, err := discovery.New(discovery.Config{
		Instance: "dyj on txt-test",
		HostName: "txt-test.local.",
		Port:     6631,
		IPs:      []net.IP{net.ParseIP("192.168.1.5")},
		Path:     "/ipp/print",
		TXT:      discovery.BuildTXT(discovery.TXTParams{Port: 6631, Path: "/ipp/print"}),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	txt := a.TXT()
	found := false
	for i, s := range txt {
		if strings.HasPrefix(s, "pair=") {
			txt[i] = "pair=" + pair
			found = true
		}
	}
	if !found {
		txt = append(txt, "pair="+pair)
	}
	a.SetTXT(txt)
	return a
}

func advPairValue(t *testing.T, a discovery.Publisher) string {
	t.Helper()
	for _, s := range a.TXT() {
		if strings.HasPrefix(s, "pair=") {
			return strings.TrimPrefix(s, "pair=")
		}
	}
	t.Fatalf("no pair= mark in TXT %v", a.TXT())
	return ""
}

// TestPairTXTValueFollowsOpenState pins the discovery-side contract: the
// pair mark is "0" exactly while the data plane accepts tokenless IPP.
func TestPairTXTValueFollowsOpenState(t *testing.T) {
	n, _, advance := newGateNode(t)
	if got := n.pairTXTValue(); got != "1" {
		t.Fatalf("closed: pair mark = %q, want 1", got)
	}
	n.openSetWindow(time.Minute)
	if got := n.pairTXTValue(); got != "0" {
		t.Fatalf("window open: pair mark = %q, want 0", got)
	}
	advance(2 * time.Minute)
	if got := n.pairTXTValue(); got != "1" {
		t.Fatalf("expired: pair mark = %q, want 1", got)
	}
	n.openSetAlways(true)
	if got := n.pairTXTValue(); got != "0" {
		t.Fatalf("always-on: pair mark = %q, want 0", got)
	}
	n.openDisable()
	if got := n.pairTXTValue(); got != "1" {
		t.Fatalf("disabled: pair mark = %q, want 1", got)
	}
}

// TestSyncOpenTXTRefreshesAdvertisers drives the real sync path over live
// (unstarted) advertisers: window open flips every TXT to pair=0, close
// flips it back, and the window expiry timer is armed/stopped correctly.
func TestSyncOpenTXTRefreshesAdvertisers(t *testing.T) {
	n, _, _ := newGateNode(t)
	writes := stubPersist(t)
	a1 := txtTestAdvertiser(t, "1")
	a2 := txtTestAdvertiser(t, "1")
	n.mu.Lock()
	n.adv = []discovery.Publisher{a1, a2}
	n.mu.Unlock()

	n.openSetWindow(time.Minute)
	for i, a := range []discovery.Publisher{a1, a2} {
		if got := advPairValue(t, a); got != "0" {
			t.Fatalf("advertiser %d after open: pair=%q, want 0", i, got)
		}
	}
	n.mu.Lock()
	timer := n.openTimer
	n.mu.Unlock()
	if timer == nil {
		t.Fatal("openSetWindow must arm the expiry re-announce timer")
	}

	n.openDisable()
	for i, a := range []discovery.Publisher{a1, a2} {
		if got := advPairValue(t, a); got != "1" {
			t.Fatalf("advertiser %d after close: pair=%q, want 1", i, got)
		}
	}
	n.mu.Lock()
	timer = n.openTimer
	n.mu.Unlock()
	if timer != nil {
		t.Fatal("openDisable must stop and clear the expiry timer")
	}
	_ = writes // window open/close never persists; open_test.go pins that
}

// TestApplyConfigPreservesOpenPrint covers the silent-clear bug: a wizard
// PUT built from a stale snapshot (no open_print key) must not turn the
// persisted always-on flag off; an explicit false still clears it.
func TestApplyConfigPreservesOpenPrint(t *testing.T) {
	n, _, _ := newGateNode(t)
	n.swap = &swapHandler{} // applyConfig hot-swaps the router under it
	writes := stubPersist(t)
	n.openSetAlways(true)

	if err := n.applyConfig([]byte(`{"role":"share","name":"dyj"}`)); err != nil {
		t.Fatalf("applyConfig without open_print: %v", err)
	}
	n.mu.Lock()
	kept := n.cfg.OpenPrint
	n.mu.Unlock()
	if !kept {
		t.Fatal("PUT without open_print must preserve the always-on flag")
	}
	if len(writes()) == 0 || !strings.Contains(writes()[len(writes())-1], `"open_print": true`) {
		t.Fatalf("preserved flag must survive persistence, writes=%q", writes())
	}

	if err := n.applyConfig([]byte(`{"role":"share","name":"dyj","open_print":false}`)); err != nil {
		t.Fatalf("applyConfig with explicit false: %v", err)
	}
	n.mu.Lock()
	cleared := !n.cfg.OpenPrint
	n.mu.Unlock()
	if !cleared {
		t.Fatal("explicit open_print=false must clear the flag")
	}
}

// TestOpenPrintingDiscoveryToGate is the full in-process loop the user
// reported broken: open the window → the scan row says need_pin=false →
// a tokenless LAN IPP request is answered; close → need_pin=true → 401.
func TestOpenPrintingDiscoveryToGate(t *testing.T) {
	n, _, _ := newGateNode(t)
	backend, err := driver.NewFile(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	spool, err := spooler.New(spooler.Options{Dir: t.TempDir(), Backend: backend})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = spool.Close(ctx)
	}()
	p := ipp.DefaultPrinter()
	p.Name = "open e2e"
	p.Key = "open"
	p.AcceptingJobs = true
	rt := ipp.NewRouter(ipp.RouterOptions{
		Entries: []ipp.Entry{{Printer: p, Sink: spool}},
		Gate:    n.gate(),
	})
	a := txtTestAdvertiser(t, "1")
	n.mu.Lock()
	n.adv = []discovery.Publisher{a}
	n.mu.Unlock()

	found := []discovery.Found{{
		Instance: "open e2e._ipp._tcp.local.", Host: "txt-test.local.", Port: 6631,
		Addrs:  []string{"192.168.1.5"},
		TXTMap: txtMap(a.TXT()),
	}}
	post := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "http://dyj.test/ipp/print", bytes.NewReader(ippGetPrinterFrame(t)))
		r.RemoteAddr = "10.0.0.10:4400"
		r.Header.Set("Content-Type", "application/ipp")
		w := httptest.NewRecorder()
		rt.ServeHTTP(w, r)
		return w
	}

	// Closed: the row demands a PIN and the gate refuses tokenless IPP.
	if rows := scanEntries(found, map[string]bool{}); len(rows) != 1 || !rows[0].NeedPIN {
		t.Fatalf("closed: want one need_pin row, got %+v", rows)
	}
	if w := post(); w.Code != http.StatusUnauthorized {
		t.Fatalf("closed: want 401, got %d", w.Code)
	}

	// Open via the real API handler: TXT flips, scan row frees, IPP passes.
	if w := openPost(t, n, `{"duration_sec":600}`); w.Code != http.StatusOK {
		t.Fatalf("open POST: %d %s", w.Code, w.Body.String())
	}
	found[0].TXTMap = txtMap(a.TXT())
	if rows := scanEntries(found, map[string]bool{}); len(rows) != 1 || rows[0].NeedPIN {
		t.Fatalf("open: want need_pin=false, got %+v", rows)
	}
	w := post()
	if w.Code != http.StatusOK {
		t.Fatalf("open: want 200, got %d", w.Code)
	}
	if msg, err := attributes.DecodeMessageStream(bytes.NewReader(w.Body.Bytes())); err != nil || msg.Code != 0 {
		t.Fatalf("open: bad IPP answer: %v code 0x%04X", err, msgCode(msg, err))
	}

	// Close via the real API handler: everything returns to the PIN path.
	dw := httptest.NewRecorder()
	n.apiOpen(dw, httptest.NewRequest(http.MethodDelete, "/admin/api/open", nil))
	if dw.Code != http.StatusOK {
		t.Fatalf("open DELETE: %d", dw.Code)
	}
	found[0].TXTMap = txtMap(a.TXT())
	if rows := scanEntries(found, map[string]bool{}); len(rows) != 1 || !rows[0].NeedPIN {
		t.Fatalf("closed again: want need_pin=true, got %+v", rows)
	}
	if w := post(); w.Code != http.StatusUnauthorized {
		t.Fatalf("closed again: want 401, got %d", w.Code)
	}
}

// txtMap parses DNS-SD TXT strings into the map shape discovery.Found uses.
func txtMap(txt []string) map[string]string {
	m := map[string]string{}
	for _, s := range txt {
		if i := strings.Index(s, "="); i > 0 {
			m[s[:i]] = s[i+1:]
		}
	}
	return m
}

// TestScanEntriesPairMarkVariants pins the receiver side of the contract:
// pair=1 demands a PIN, pair=0 or an absent mark does not.
func TestScanEntriesPairMarkVariants(t *testing.T) {
	mk := func(pair string) discovery.Found {
		txt := map[string]string{"dyj": "1", "ty": "p", "rp": "ipp/print"}
		if pair != "" {
			txt["pair"] = pair
		}
		return discovery.Found{Instance: "p._ipp._tcp.local.", Host: "h.local.", Port: 6631, TXTMap: txt}
	}
	for _, tc := range []struct {
		pair string
		want bool
	}{{"1", true}, {"0", false}, {"", false}} {
		rows := scanEntries([]discovery.Found{mk(tc.pair)}, map[string]bool{})
		if len(rows) != 1 || rows[0].NeedPIN != tc.want {
			t.Errorf("pair=%q: got %+v, want NeedPIN=%v", tc.pair, rows, tc.want)
		}
	}
}

// TestAPIProbeReportsRevoked drives the pre-install probe endpoint against
// a live test IPP server: open path → alive, token-gated path → 401.
func TestAPIProbeReportsRevoked(t *testing.T) {
	n, _, _ := newGateNode(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/ipp/print", func(w http.ResponseWriter, r *http.Request) {
		writeTestIPP(w, "probe-target")
	})
	mux.HandleFunc("/p/dead/ipp/print", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	host, portStr, _ := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatal(err)
	}

	call := func(path string) map[string]any {
		body, _ := json.Marshal(map[string]any{"host": host, "port": port, "path": path})
		r := httptest.NewRequest(http.MethodPost, "/admin/api/probe", bytes.NewReader(body))
		w := httptest.NewRecorder()
		n.apiProbe(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("probe %s: %d %s", path, w.Code, w.Body.String())
		}
		out := map[string]any{}
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	if out := call("/ipp/print"); out["alive"] != true {
		t.Fatalf("open path probe = %v, want alive", out)
	}
	if out := call("/p/dead/ipp/print"); out["revoked"] != true || out["status"] != float64(401) {
		t.Fatalf("gated path probe = %v, want revoked/401", out)
	}
}