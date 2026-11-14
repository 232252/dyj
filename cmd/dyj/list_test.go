package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/232252/dyj/pkg/discovery"
	"github.com/232252/dyj/pkg/sysqueue"
)

// TestScanEntriesSameNameDifferentHosts pins the row-swallowing bug: two
// machines sharing same-named printers (same label, same port) must show
// as two rows. With UUIDs the identity is exact; without them the host
// base keeps them apart.
func TestScanEntriesSameNameDifferentHosts(t *testing.T) {
	mk := func(host, uuid string) discovery.Found {
		txt := map[string]string{"ty": "HP 1020", "dyj": "1", "pair": "1", "rp": "ipp/print"}
		if uuid != "" {
			txt["UUID"] = uuid
		}
		return discovery.Found{
			Instance: "HP 1020._ipp._tcp.local.", Host: host, Port: 6631,
			Addrs: []string{"10.0.0.1"}, TXTMap: txt,
		}
	}
	withUUID := []discovery.Found{mk("a-eth0.local.", "u-a"), mk("b-eth0.local.", "u-b")}
	if got := scanEntries(withUUID, map[string]bool{}); len(got) != 2 {
		t.Fatalf("UUID rows: got %d, want 2: %+v", len(got), got)
	}
	noUUID := []discovery.Found{mk("a-eth0.local.", ""), mk("b-eth0.local.", "")}
	if got := scanEntries(noUUID, map[string]bool{}); len(got) != 2 {
		t.Fatalf("host-base rows: got %d, want 2: %+v", len(got), got)
	}
	// The ${IFACE} views of ONE machine still merge into a single row.
	one := []discovery.Found{mk("a-eth0.local.", "u-a"), mk("a-wlan0.local.", "u-a")}
	if got := scanEntries(one, map[string]bool{}); len(got) != 1 {
		t.Fatalf("iface views: got %d, want 1: %+v", len(got), got)
	}
}

// TestApplyConfigInheritsPrinterKeys guards the paired-URI contract: the
// wizard PUTs {name, queue} without keys, and an unchanged printer must
// keep its persisted key so /p/<token>/ipp/print/<key> keeps working.
func TestApplyConfigInheritsPrinterKeys(t *testing.T) {
	n, _, _ := newGateNode(t)
	n.swap = &swapHandler{}
	stubPersist(t)
	n.mu.Lock()
	n.cfg = serveOptions{
		Role: roleShare,
		Printers: []printerSpec{
			{Key: "office-key", Name: "办公室", Queue: "办公室"},
		},
	}
	n.mu.Unlock()

	raw := `{"role":"share","printers":[{"name":"办公室","queue":"办公室"},{"name":"新打印机","queue":"新打印机"}]}`
	if err := n.applyConfig([]byte(raw)); err != nil {
		t.Fatalf("applyConfig: %v", err)
	}
	n.mu.Lock()
	ps := append([]printerSpec(nil), n.cfg.Printers...)
	n.mu.Unlock()
	if len(ps) != 2 {
		t.Fatalf("printers = %+v", ps)
	}
	if ps[0].Key != "office-key" {
		t.Errorf("existing printer lost its key: %+v", ps[0])
	}
	if ps[1].Key == "" || ps[1].Key == "office-key" {
		t.Errorf("new printer needs its own fresh key: %+v", ps[1])
	}
}

// TestAddedStatusesClassifies drives the batched added-list path against
// a live test IPP server: alive → ok, 401 → revoked, silent → offline.
// The system-queue query may fail in the test environment; classifyAdded
// keeps that inconclusive, so the IPP verdict still shows.
func TestAddedStatusesClassifies(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/ipp/print", func(w http.ResponseWriter, r *http.Request) {
		writeTestIPP(w, "list-target")
	})
	mux.HandleFunc("/p/dead/ipp/print", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	host, port := splitTestHost(t, srv.URL)

	list := []addedPrinter{
		{QueueName: "dyj-list-ok", ServerHost: host, ServerPort: port, URIPath: "/ipp/print"},
		{QueueName: "dyj-list-revoked", ServerHost: host, ServerPort: port, URIPath: "/p/dead/ipp/print"},
		{QueueName: "dyj-list-offline", ServerHost: host, ServerPort: 1, URIPath: "/ipp/print"},
	}
	out := addedStatuses(list)
	if len(out) != 3 {
		t.Fatalf("got %d statuses, want 3", len(out))
	}
	// When the batched system-queue query succeeds (a real Windows host),
	// these fabricated queue names are genuinely absent → "missing" wins
	// over the IPP verdict, exactly as classifyAdded specifies. When the
	// query is inconclusive (CI without a spooler), the IPP verdict shows.
	_, qErr := sysqueue.Names([]string{"dyj-list-ok"})
	want := map[string]string{"dyj-list-ok": "ok", "dyj-list-revoked": "revoked", "dyj-list-offline": "offline"}
	if qErr == nil {
		want = map[string]string{"dyj-list-ok": "missing", "dyj-list-revoked": "missing", "dyj-list-offline": "missing"}
	}
	for _, s := range out {
		if w := want[s.QueueName]; s.Status != w {
			t.Errorf("%s: status %q, want %q", s.QueueName, s.Status, w)
		}
	}
}

// TestAddedStoreUpsertKeepsDistinctPorts pins the registry key: the same
// host+path on two ports is two printers, not one overwritten entry.
func TestAddedStoreUpsertKeepsDistinctPorts(t *testing.T) {
	s := openAddedStore(filepath.Join(t.TempDir(), "added.json"))
	s.upsert(addedPrinter{QueueName: "q1", ServerHost: "h", ServerPort: 6631, URIPath: "/ipp/print"})
	s.upsert(addedPrinter{QueueName: "q2", ServerHost: "h", ServerPort: 6632, URIPath: "/ipp/print"})
	if got := s.list(); len(got) != 2 {
		t.Fatalf("distinct ports collapsed: %+v", got)
	}
	// Same host+port+path replaces (reinstall of one printer).
	s.upsert(addedPrinter{QueueName: "q1b", ServerHost: "h", ServerPort: 6631, URIPath: "/ipp/print"})
	got := s.list()
	if len(got) != 2 {
		t.Fatalf("reinstall must replace, got %+v", got)
	}
	for _, p := range got {
		if p.ServerPort == 6631 && p.QueueName != "q1b" {
			t.Errorf("reinstall did not replace the 6631 entry: %+v", got)
		}
	}
}

// TestAPIAddedPrintersShape is the endpoint smoke test: JSON shape and
// one status per registry entry.
func TestAPIAddedPrintersShape(t *testing.T) {
	n, _, _ := newGateNode(t)
	n.added = openAddedStore(filepath.Join(t.TempDir(), "added.json"))
	n.added.upsert(addedPrinter{QueueName: "q1", ServerHost: "127.0.0.1", ServerPort: 1, URIPath: "/ipp/print"})
	w := httptest.NewRecorder()
	n.apiAddedPrinters(w, httptest.NewRequest(http.MethodGet, "/admin/api/added-printers", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var out struct {
		Printers []addedPrinterStatus `json:"printers"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Printers) != 1 || out.Printers[0].QueueName != "q1" || out.Printers[0].Status == "" {
		t.Fatalf("bad shape: %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"status"`) {
		t.Fatalf("status field missing: %s", w.Body.String())
	}
}