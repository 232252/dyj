// nodeapi.go implements the admin Extra endpoints behind the wizard and
// the receiver flows (research/13 §4.8): local printer enumeration, LAN
// scan, the /pair proxy (so the browser never fights CORS), system queue
// install/remove/test, the added-printer registry, and the pairing/devices
// management surface. Everything here sits behind the loopback/token gate
// that admin.serveAPI enforces.
package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/232252/dyj/pkg/attributes"
	"github.com/232252/dyj/pkg/client"
	"github.com/232252/dyj/pkg/discovery"
	"github.com/232252/dyj/pkg/doctor"
	"github.com/232252/dyj/pkg/localprint"
	"github.com/232252/dyj/pkg/sysqueue"
)

func (n *node) extraAPIs() map[string]http.Handler {
	return map[string]http.Handler{
		"local-printers": http.HandlerFunc(n.apiLocalPrinters),
		"scan":           http.HandlerFunc(n.apiScan),
		"deep-scan":      http.HandlerFunc(n.apiDeepScan),
		"probe":          http.HandlerFunc(n.apiProbe),
		"pair":           http.HandlerFunc(n.apiPairProxy),
		"queue/install":  http.HandlerFunc(n.apiQueueInstall),
		"queue/remove":   http.HandlerFunc(n.apiQueueRemove),
		"queue/test":     http.HandlerFunc(n.apiQueueTest),
		"added-printers": http.HandlerFunc(n.apiAddedPrinters),
		"pairing":        http.HandlerFunc(n.apiPairing),
		"pairing/rotate": http.HandlerFunc(n.apiPairingRotate),
		"devices":        http.HandlerFunc(n.apiDevices),
		"open":           http.HandlerFunc(n.apiOpen),
		"repair":         http.HandlerFunc(n.apiRepair),
		"diag/export":    http.HandlerFunc(n.apiDiagExport),
	}
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// requireMethod enforces a handler's HTTP methods, answering 405 with the
// Allow header on mismatch. Returns false once the error is written.
func requireMethod(w http.ResponseWriter, r *http.Request, methods ...string) bool {
	for _, m := range methods {
		if r.Method == m {
			return true
		}
	}
	allow := strings.Join(methods, ", ")
	w.Header().Set("Allow", allow)
	writeErr(w, http.StatusMethodNotAllowed, allow+" only")
	return false
}

func readJSONBody(w http.ResponseWriter, r *http.Request, v any) bool {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<16))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "unreadable body")
		return false
	}
	if err := json.Unmarshal(body, v); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return false
	}
	return true
}

// ---------------------------------------------------------------------------
// share side: local printers + pairing management
// ---------------------------------------------------------------------------

// apiLocalPrinters backs the share wizard's checkbox list (SHARE-1/2).
func (n *node) apiLocalPrinters(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	printers, err := localprint.List()
	if err != nil {
		n.log.Warn("share.enumerate", "err", err)
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	printers = localprint.ProbeAll(printers)
	virtual := 0
	for _, p := range printers {
		if p.Virtual {
			virtual++
		}
		if p.ProbeErr != "" {
			n.log.Warn("share.probe", "qname", p.Name, "err", p.ProbeErr)
		}
	}
	n.log.Info("share.enumerate", "total", len(printers), "virtual", virtual)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"printers": printers})
}

// apiPairing serves the current PIN to the dashboard/tray (loopback/token
// gated by the Extra mount).
func (n *node) apiPairing(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	if n.pair == nil {
		writeErr(w, http.StatusServiceUnavailable, "pairing disabled")
		return
	}
	pin, remain := n.pair.Current()
	_ = json.NewEncoder(w).Encode(map[string]any{"pin": pin, "remain_sec": remain})
}

func (n *node) apiPairingRotate(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	if n.pair == nil {
		writeErr(w, http.StatusServiceUnavailable, "pairing disabled")
		return
	}
	pin, remain := n.pair.Rotate()
	_ = json.NewEncoder(w).Encode(map[string]any{"pin": pin, "remain_sec": remain})
}

// apiDevices lists (GET) and revokes (DELETE /admin/api/devices/<id>) the
// paired receiver set.
func (n *node) apiDevices(w http.ResponseWriter, r *http.Request) {
	if n.pair == nil {
		writeErr(w, http.StatusServiceUnavailable, "pairing disabled")
		return
	}
	switch r.Method {
	case http.MethodGet:
		_ = json.NewEncoder(w).Encode(map[string]any{"devices": n.pair.List()})
	case http.MethodDelete:
		id := strings.TrimPrefix(r.URL.Path, "/admin/api/devices/")
		if id == "" || strings.Contains(id, "/") {
			writeErr(w, http.StatusBadRequest, "missing device id")
			return
		}
		if !n.pair.Revoke(id) {
			writeErr(w, http.StatusNotFound, "no such device")
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"revoked": id})
	default:
		requireMethod(w, r, http.MethodGet, http.MethodDelete)
	}
}

// apiOpen drives the open-printing mode (ADR-0014): GET reports the state,
// POST starts a bounded window (runtime-only) or flips the persisted
// always-on flag, DELETE clears both. The endpoint sits behind the admin
// loopback/token gate — opening the data plane never opens the management
// plane. Always-on wins when both fields arrive in one POST.
func (n *node) apiOpen(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
	case http.MethodPost:
		var req struct {
			DurationSec int  `json:"duration_sec"`
			Always      bool `json:"always"`
		}
		if !readJSONBody(w, r, &req) {
			return
		}
		if req.Always {
			n.openSetAlways(true)
			n.log.Info("open.print_always", "src", hostOf(r.RemoteAddr))
		} else {
			// Missing or non-positive means the default 10 minutes; the
			// cap keeps a stray value from turning "temporary" into a day.
			d := 10 * time.Minute
			if req.DurationSec > 0 {
				d = time.Duration(req.DurationSec) * time.Second
				if d > time.Hour {
					d = time.Hour
				}
			}
			n.openSetWindow(d)
			n.log.Info("open.print_window", "src", hostOf(r.RemoteAddr), "sec", int(d/time.Second))
		}
	case http.MethodDelete:
		n.openDisable()
		n.log.Info("open.print_close", "src", hostOf(r.RemoteAddr))
	default:
		requireMethod(w, r, http.MethodGet, http.MethodPost, http.MethodDelete)
		return
	}
	n.mu.Lock()
	st := n.openStateLocked()
	n.mu.Unlock()
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"enabled":    st.enabled,
		"always":     st.always,
		"remain_sec": st.remainSec,
	})
}

// ---------------------------------------------------------------------------
// receive side: scan / pair / queue install
// ---------------------------------------------------------------------------

// scanEntry is one discovered IPP service for the add-printer list.
type scanEntry struct {
	Instance string   `json:"instance"`
	Host     string   `json:"host"`
	Port     int      `json:"port"`
	Addrs    []string `json:"addrs"`
	Name     string   `json:"name"`
	IsDyj    bool     `json:"dyj"`
	NeedPIN  bool     `json:"need_pin"`
	// Path is the IPP resource path non-dyj clients should install
	// against (TXT rp=; "/ipp/print" when absent).
	Path      string `json:"path"`
	Reachable bool   `json:"reachable"`
	RTTms     int    `json:"rtt_ms"`
	// Known is set when this row was rediscovered from a printer this
	// machine already added (research/15 P2). The UI pins those rows.
	Known bool `json:"known,omitempty"`
}

// apiScan browses _ipp._tcp on the LAN (USE-1/2) and, in parallel, probes
// printers this machine already added (research/15 P2). dyj shares carry
// the dyj=1/pair=1 TXT marks; own instances are filtered out.
//
// A browse failure with nothing found is still a 200 and an empty list.
// The client page's empty state is the manual-address path, and the
// previous 502 ("mDNS scan failed") hid that path behind a jargon error
// exactly when the two machines are not on the same broadcast domain.
func (n *node) apiScan(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	refresh := r.URL.Query().Get("refresh") == "1"
	out := n.scanCached(refresh)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"services": out})
}

// scanResultCache is the node-wide scan cache: the UI polls every 20s and
// several tabs may poll at once, but the wire work (multicast rounds +
// known-node probes) runs at most once per 10s, single-flight.
var scanResultCache struct {
	mu      sync.Mutex
	at      time.Time
	entries []scanEntry
	running bool
	cond    *sync.Cond
}

// scanCached serves the cached scan when fresh; refresh=true (the manual
// "rescan" button) always bypasses it. Concurrent callers wait for the
// in-flight scan instead of starting a second one.
func (n *node) scanCached(refresh bool) []scanEntry {
	c := &scanResultCache
	c.mu.Lock()
	if c.cond == nil {
		c.cond = sync.NewCond(&c.mu)
	}
	for {
		if !refresh && c.entries != nil && time.Since(c.at) < 10*time.Second {
			out := append([]scanEntry(nil), c.entries...)
			c.mu.Unlock()
			return out
		}
		if !c.running {
			c.running = true
			break
		}
		c.cond.Wait()
	}
	c.mu.Unlock()

	out := n.scanNow()

	c.mu.Lock()
	c.at = time.Now()
	c.entries = out
	c.running = false
	c.cond.Broadcast()
	c.mu.Unlock()
	return append([]scanEntry(nil), out...)
}

// lastSeen tracks when each service identity was last observed so a
// printer that went away lingers briefly (flap tolerance) and then
// disappears instead of haunting the list forever (ADR-0015).
var lastSeen = struct {
	mu sync.Mutex
	m  map[string]time.Time
}{m: map[string]time.Time{}}

const offlineTTL = 90 * time.Second

// scanNow performs one full scan: mDNS browse + known-node probes in
// parallel, reachability with real RTT, then the offline prune.
func (n *node) scanNow() []scanEntry {
	var (
		found     []discovery.Found
		browseErr error
		known     []scanEntry
		probed    int
		alive     int
	)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		ifaces, _ := discovery.SelectBrowseInterfaces(nil)
		found, browseErr = discovery.Browse(ifaces, 3*time.Second)
	}()
	go func() {
		defer wg.Done()
		known, probed, alive = n.probeKnownNodes(2 * time.Second)
	}()
	wg.Wait()
	n.log.Info("scan.result", "found", len(found), "err", browseErr)
	n.log.Info("scan.known", "probed", probed, "alive", alive)

	out := scanEntries(found, n.ownUUIDs())
	for i := range out {
		addr, rtt := firstReachable(out[i].Addrs, out[i].Port)
		if addr != "" {
			out[i].Reachable = true
			out[i].RTTms = rtt
		}
	}
	out = pinKnown(out, known)
	out = mergeWithLastSeen(out)
	return out
}

// mergeWithLastSeen keeps a row that vanished from THIS scan visible until
// it has been absent for offlineTTL (a single lost multicast round must
// not flicker the list), then drops it. Fresh rows update the stamp;
// carried-over rows are marked unreachable. The previous result is read
// from the scan cache under its own lock (scanNow runs outside that lock).
func mergeWithLastSeen(current []scanEntry) []scanEntry {
	now := time.Now()
	scanResultCache.mu.Lock()
	prev := append([]scanEntry(nil), scanResultCache.entries...)
	scanResultCache.mu.Unlock()

	byID := map[string]int{}
	for i, e := range current {
		byID[scanIdentity(e)] = i
	}
	lastSeen.mu.Lock()
	for _, e := range current {
		lastSeen.m[scanIdentity(e)] = now
	}
	for _, e := range prev {
		id := scanIdentity(e)
		if _, ok := byID[id]; ok {
			continue // fresh this round
		}
		at, tracked := lastSeen.m[id]
		if !tracked {
			// First absence: start the clock, keep the row one more round.
			lastSeen.m[id] = now
			at = now
		}
		if now.Sub(at) > offlineTTL {
			delete(lastSeen.m, id)
			continue // gone for good
		}
		e.Reachable = false
		e.RTTms = 0
		current = append(current, e)
	}
	lastSeen.mu.Unlock()
	return current
}

// scanIdentity is the stable row identity for the lastSeen cache: the
// instance label plus port (the UUID lives in TXT, not in the row).
func scanIdentity(e scanEntry) string {
	return strings.ToLower(e.Instance) + "|" + fmt.Sprint(e.Port)
}

// apiProbe answers "does this IPP endpoint accept a tokenless request
// right now?" for the client page's pairing-free install path (ADR-0015).
// The TXT pair= mark can lag a just-closed open window; a live
// Get-Printer-Attributes probe catches the 401 before a dead queue gets
// installed. Loopback/admin-gated like every Extra endpoint; the probe
// itself only reads printer-name.
func (n *node) apiProbe(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		Host string `json:"host"`
		Port int    `json:"port"`
		Path string `json:"path"`
	}
	if !readJSONBody(w, r, &req) {
		return
	}
	if req.Host == "" || req.Port <= 0 || req.Port > 65535 {
		writeErr(w, http.StatusBadRequest, "host and port required")
		return
	}
	path := req.Path
	if path == "" {
		path = "/ipp/print"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	pr := probeIPP(req.Host, req.Port, path, 2*time.Second)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"alive": pr.Alive, "revoked": pr.Revoked, "status": pr.Status, "name": pr.Name,
	})
}

// deepScanState is the single-flight background sweep (research/15 P3):
// POST starts it, GET polls it. Only one sweep runs at a time; a second
// POST while running returns the current state instead of stacking work.
var deepScanState = struct {
	mu      sync.Mutex
	running bool
	done    bool
	entries []scanEntry
	err     string
	cidr    string
}{}

// apiDeepScan drives the manual cross-subnet sweep. POST {cidr?} starts a
// background task (empty cidr = every selected interface's /24); GET
// reports {running, done, entries, error}. The sweep is IPP-confirmed, so
// mesh fake-handshakes never surface as printers.
func (n *node) apiDeepScan(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var req struct {
			CIDR string `json:"cidr"`
		}
		if !readJSONBody(w, r, &req) {
			return
		}
		deepScanState.mu.Lock()
		if deepScanState.running {
			st := deepScanSnapshotLocked()
			deepScanState.mu.Unlock()
			writeJSON(w, st)
			return
		}
		deepScanState.running = true
		deepScanState.done = false
		deepScanState.entries = nil
		deepScanState.err = ""
		deepScanState.cidr = req.CIDR
		deepScanState.mu.Unlock()
		go n.runDeepScan(req.CIDR)
		writeJSON(w, map[string]any{"running": true, "done": false})
	case http.MethodGet:
		deepScanState.mu.Lock()
		st := deepScanSnapshotLocked()
		deepScanState.mu.Unlock()
		writeJSON(w, st)
	default:
		requireMethod(w, r, http.MethodGet, http.MethodPost)
	}
}

func deepScanSnapshotLocked() map[string]any {
	return map[string]any{
		"running": deepScanState.running,
		"done":    deepScanState.done,
		"entries": deepScanState.entries,
		"error":   deepScanState.err,
		"cidr":    deepScanState.cidr,
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

// runDeepScan executes the sweep off the request path and publishes the
// result into deepScanState.
func (n *node) runDeepScan(cidr string) {
	ifaces, _ := discovery.SelectBrowseInterfaces(nil)
	opt := discovery.SweepOptions{
		Ifaces: ifaces,
		Probe: func(ctx context.Context, host string, port int) (string, bool) {
			pr := probeIPPContext(ctx, host, port, "/ipp/print", 1500*time.Millisecond)
			if !pr.Alive {
				return "", false
			}
			return pr.Name, true
		},
	}
	if cidr != "" {
		opt.CIDRs = []string{cidr}
	}
	found, err := discovery.SweepSubnets(context.Background(), opt)
	entries := make([]scanEntry, 0, len(found))
	for _, f := range found {
		entries = append(entries, scanEntry{
			Instance:  f.Name,
			Host:      f.Host,
			Port:      f.Port,
			Addrs:     []string{f.Host},
			Name:      f.Name,
			Path:      "/ipp/print",
			Reachable: true,
		})
	}
	n.log.Info("scan.deep", "cidr", cidr, "found", len(entries), "err", err)
	deepScanState.mu.Lock()
	deepScanState.running = false
	deepScanState.done = true
	deepScanState.entries = entries
	if err != nil {
		deepScanState.err = err.Error()
	}
	deepScanState.mu.Unlock()
}

// probeKnownNodes asks every previously added share side whether it still
// speaks IPP. The probe is Get-Printer-Attributes, never a bare TCP dial:
// mesh/VPN overlays complete the handshake on ports that have no listener
// and then send nothing (research/12 §4), which would pin dead rows.
func (n *node) probeKnownNodes(timeout time.Duration) (alive []scanEntry, probed, nAlive int) {
	if n == nil || n.added == nil {
		return nil, 0, 0
	}
	type key struct {
		host string
		port int
		path string
	}
	seen := map[key]addedPrinter{}
	var order []key
	list := n.added.list()
	// Newest first: when the cap trims, the most recently added printers
	// (the ones the user is actively working with) keep their probes.
	sort.SliceStable(list, func(i, j int) bool { return list[i].AddedAt.After(list[j].AddedAt) })
	for _, a := range list {
		if a.ServerHost == "" || a.ServerPort <= 0 {
			continue
		}
		k := key{strings.ToLower(strings.Trim(a.ServerHost, "[]")), a.ServerPort, a.URIPath}
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = a
		order = append(order, k)
	}
	if len(order) > 32 {
		order = order[:32]
	}
	probed = len(order)
	if probed == 0 {
		return nil, 0, 0
	}
	type result struct {
		i  int
		e  scanEntry
		ok bool
	}
	ch := make(chan result, probed)
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for i, k := range order {
		wg.Add(1)
		go func(i int, a addedPrinter) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			pr := probeIPP(a.ServerHost, a.ServerPort, a.URIPath, timeout)
			if !pr.Alive {
				ch <- result{i: i}
				return
			}
			name := a.PrinterName
			if name == "" {
				name = a.ServerName
			}
			if name == "" {
				name = pr.Name
			}
			if name == "" {
				name = a.ServerHost
			}
			path := a.URIPath
			if path == "" {
				path = "/ipp/print"
			}
			if !strings.HasPrefix(path, "/") {
				path = "/" + path
			}
			// A live token path can be reinstalled as-is. HTTP 401 means
			// the share side is up and the old pairing is dead, so the
			// row must ask for a new code.
			paired := strings.Contains(path, "/p/")
			ch <- result{i: i, ok: true, e: scanEntry{
				Instance:  name,
				Host:      strings.Trim(a.ServerHost, "[]"),
				Port:      a.ServerPort,
				Addrs:     []string{strings.Trim(a.ServerHost, "[]")},
				Name:      name,
				IsDyj:     paired || pr.Revoked,
				NeedPIN:   pr.Revoked,
				Path:      path,
				Reachable: true,
				Known:     true,
			}}
		}(i, seen[k])
	}
	wg.Wait()
	close(ch)
	slots := make([]scanEntry, probed)
	okn := make([]bool, probed)
	for r := range ch {
		if r.ok {
			slots[r.i] = r.e
			okn[r.i] = true
		}
	}
	for i, ok := range okn {
		if ok {
			alive = append(alive, slots[i])
			nAlive++
		}
	}
	return alive, probed, nAlive
}

// pinKnown moves rediscovered printers to the front of the scan list.
// A known address that the browse already returned is marked and not
// duplicated; a known address the browse missed (mesh/VPN, no multicast)
// is inserted as its own row.
func pinKnown(scanned, known []scanEntry) []scanEntry {
	if len(known) == 0 {
		return scanned
	}
	used := map[int]bool{}
	front := make([]scanEntry, 0, len(known))
	for _, k := range known {
		matched := -1
		for i, s := range scanned {
			if used[i] {
				continue
			}
			if sameEndpoint(s, k) {
				matched = i
				break
			}
		}
		if matched >= 0 {
			s := scanned[matched]
			s.Known = true
			used[matched] = true
			front = append(front, s)
			continue
		}
		k.Known = true
		front = append(front, k)
	}
	rest := make([]scanEntry, 0, len(scanned))
	for i, s := range scanned {
		if !used[i] {
			rest = append(rest, s)
		}
	}
	return append(front, rest...)
}

// sameEndpoint reports whether two scan rows are the same printer reached
// by a shared address. Ports must match; names alone do not, because two
// offices can both call a printer "HP".
func sameEndpoint(a, b scanEntry) bool {
	if a.Port == 0 || a.Port != b.Port {
		return false
	}
	if hostMatch(a.Host, b.Host) {
		return true
	}
	for _, addr := range a.Addrs {
		if hostMatch(addr, b.Host) {
			return true
		}
		for _, addr2 := range b.Addrs {
			if hostMatch(addr, addr2) {
				return true
			}
		}
	}
	for _, addr := range b.Addrs {
		if hostMatch(addr, a.Host) {
			return true
		}
	}
	return false
}

func hostMatch(a, b string) bool {
	a = strings.ToLower(strings.Trim(strings.TrimSuffix(a, "."), "[]"))
	b = strings.ToLower(strings.Trim(strings.TrimSuffix(b, "."), "[]"))
	return a != "" && a == b
}

// ippProbe is one application-layer Get-Printer-Attributes outcome.
// Alive is true only when the peer spoke IPP, or rejected the paired
// path with HTTP 401 (the share side is up; the token is not).
type ippProbe struct {
	Alive   bool
	Revoked bool
	Name    string
	Status  int
}

// probeIPP performs Get-Printer-Attributes. timeout bounds the whole HTTP
// exchange so a mesh fake-handshake cannot pin the scan.
func probeIPP(host string, port int, path string, timeout time.Duration) ippProbe {
	return probeIPPContext(context.Background(), host, port, path, timeout)
}

// probeIPPContext is probeIPP under an extra context budget: the added
// list bounds ALL its probes together, not just each one. The context
// rides on the transport (the IPP client has no ctx API and the wire
// layer stays untouched).
func probeIPPContext(ctx context.Context, host string, port int, path string, timeout time.Duration) ippProbe {
	host = strings.Trim(host, "[]")
	if host == "" || port <= 0 {
		return ippProbe{}
	}
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	if path == "" {
		path = "/ipp/print"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	uri := fmt.Sprintf("ipp://%s%s", net.JoinHostPort(host, strconv.Itoa(port)), path)
	c, err := client.Parse(uri, "dyj")
	if err != nil {
		return ippProbe{}
	}
	c.HTTP.Timeout = timeout
	c.HTTP.Transport = &ctxTransport{base: c.HTTP.Transport, ctx: ctx}
	msg, err := c.GetPrinterAttributes("printer-name")
	if err != nil {
		var he *client.HTTPStatusError
		if errors.As(err, &he) && he.Status == http.StatusUnauthorized {
			return ippProbe{Alive: true, Revoked: true, Status: he.Status}
		}
		return ippProbe{}
	}
	name := ""
	if g := msg.Group(attributes.TagPrinterAttributes); g != nil {
		name = attributes.GroupFirstString(g, "printer-name")
	}
	return ippProbe{Alive: true, Name: name, Status: http.StatusOK}
}

// ctxTransport injects a context into every request without touching the
// IPP client's wire code.
type ctxTransport struct {
	base http.RoundTripper
	ctx  context.Context
}

func (t *ctxTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return t.base.RoundTrip(r.WithContext(t.ctx))
}

// scanEntries turns discovered services into add-printer rows: it drops
// non-IPP wire noise and this machine's own instances, then merges the raw
// and reference-client views of the same printer.
//
// Merge key (ADR-0015): the TXT UUID when present — it is stable across
// interfaces and renames, and two machines sharing same-named printers
// carry different UUIDs, so they must NOT collapse into one row. Without
// a UUID the fallback is label + host-base + port: the SRV target differs
// per interface only by the "-<iface>" suffix (the ${IFACE} naming in
// discovery), and stripping it keeps one physical printer as one row
// (research/15 D-1) while same-named printers on different hosts stay
// distinct.
func scanEntries(found []discovery.Found, own map[string]bool) []scanEntry {
	byKey := map[string]*scanEntry{}
	ordered := []*scanEntry{}
	for _, f := range found {
		// The reference-client path surfaces non-IPP services that merely
		// coexist on the wire; only actual _ipp._tcp instances (or dyj
		// TXT marks) belong in the add-printer list.
		if !strings.Contains(f.Instance, "_ipp._tcp") && f.TXTMap["dyj"] != "1" {
			continue
		}
		if own[strings.ToLower(f.TXTMap["UUID"])] || own[strings.ToLower(strings.TrimSuffix(f.Host, "."))] {
			continue
		}
		// The instance label without the service suffix: both resolver
		// paths report the same printer, sometimes with/without the
		// trailing dot.
		label := f.Instance
		if i := strings.Index(label, "._ipp"); i > 0 {
			label = label[:i]
		}
		e := scanEntry{
			Instance: f.Instance,
			Host:     strings.TrimSuffix(f.Host, "."),
			Port:     f.Port,
			Addrs:    f.Addrs,
			Name:     f.TXTMap["ty"],
			IsDyj:    f.TXTMap["dyj"] == "1",
			NeedPIN:  f.TXTMap["pair"] == "1",
			Path:     "/" + strings.TrimPrefix(f.TXTMap["rp"], "/"),
		}
		if e.Name == "" {
			e.Name = f.Instance
		}
		key := scanMergeKey(f, label)
		if prev, ok := byKey[key]; ok {
			prev.Addrs = unionAddrs(prev.Addrs, e.Addrs)
			continue
		}
		byKey[key] = &e
		ordered = append(ordered, &e)
	}
	out := make([]scanEntry, 0, len(ordered))
	for _, e := range ordered {
		out = append(out, *e)
	}
	return out
}

// scanMergeKey is the dedup identity of a discovered service (see
// scanEntries): UUID first, else label + host-base + port.
func scanMergeKey(f discovery.Found, label string) string {
	label = strings.ToLower(strings.TrimSuffix(label, "."))
	if uuid := strings.ToLower(f.TXTMap["UUID"]); uuid != "" {
		return "uuid:" + uuid + "|" + fmt.Sprint(f.Port)
	}
	return label + "|" + hostBase(f.Host) + "|" + fmt.Sprint(f.Port)
}

// hostBase strips the trailing dot and the per-interface "-<iface>" suffix
// from an mDNS host name ("dyj-el-br-e57f74e0bde4.local." → "dyj-el"), so
// the ${IFACE} views of one machine compare equal. A host that is already
// an IP literal is returned as-is.
func hostBase(host string) string {
	h := strings.ToLower(strings.TrimSuffix(host, "."))
	h = strings.TrimSuffix(h, ".local")
	if net.ParseIP(h) != nil {
		return h
	}
	if i := strings.LastIndex(h, "-"); i > 0 {
		h = h[:i]
	}
	return h
}

// unionAddrs appends b's addresses to a without duplicates.
func unionAddrs(a, b []string) []string {
	seen := make(map[string]bool, len(a))
	for _, v := range a {
		seen[v] = true
	}
	for _, v := range b {
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		a = append(a, v)
	}
	return a
}

// ownUUIDs collects the local per-printer UUIDs + mDNS host so the scan
// never offers this machine to itself.
func (n *node) ownUUIDs() map[string]bool {
	own := map[string]bool{}
	matches, _ := filepath.Glob(filepath.Join(n.stateDir, "uuid*.txt"))
	for _, m := range matches {
		if data, err := os.ReadFile(m); err == nil {
			own[strings.ToLower(strings.TrimSpace(string(data)))] = true
		}
	}
	cfg := n.cfg
	if fqdn := discovery.DefaultHostName(hostNameOr(cfg.Hostname, cfg.Name)); fqdn != "" {
		own[strings.ToLower(strings.TrimSuffix(fqdn, "."))] = true
	}
	return own
}

// firstReachable dials every address in parallel and returns the fastest
// responder plus its TCP handshake RTT in milliseconds. The serial loop
// used to burn 800ms per dead address before reaching the live one, and
// RTTms was never populated at all.
func firstReachable(addrs []string, port int) (string, int) {
	if len(addrs) == 0 {
		return "", 0
	}
	type res struct {
		addr string
		rtt  time.Duration
	}
	ch := make(chan res, len(addrs))
	for _, a := range addrs {
		go func(a string) {
			start := time.Now()
			conn, err := net.DialTimeout("tcp", net.JoinHostPort(a, fmt.Sprint(port)), 800*time.Millisecond)
			if err != nil {
				ch <- res{}
				return
			}
			_ = conn.Close()
			ch <- res{addr: a, rtt: time.Since(start)}
		}(a)
	}
	best := res{}
	for range addrs {
		r := <-ch
		if r.addr != "" && (best.addr == "" || r.rtt < best.rtt) {
			best = r
		}
	}
	if best.addr == "" {
		return "", 0
	}
	ms := int(best.rtt / time.Millisecond)
	if ms < 1 {
		ms = 1
	}
	return best.addr, ms
}

// apiPairProxy relays the browser's pairing call to the SHARE machine's
// /pair endpoint (avoids CORS and keeps the device id stable on this
// machine). The response is passed through with full IPP URLs added.
func (n *node) apiPairProxy(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		Host       string `json:"host"`
		Port       int    `json:"port"`
		PIN        string `json:"pin"`
		DeviceName string `json:"device_name"`
	}
	if !readJSONBody(w, r, &req) {
		return
	}
	if req.Host == "" || req.Port <= 0 {
		writeErr(w, http.StatusBadRequest, "host and port required")
		return
	}
	deviceID, err := loadOrCreateDeviceID(n.stateDir)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "device id: "+err.Error())
		return
	}
	payload, _ := json.Marshal(map[string]string{
		"pin":         req.PIN,
		"device_id":   deviceID,
		"device_name": hostnameOr(req.DeviceName),
	})
	n.log.Info("pair.req", "dst", net.JoinHostPort(req.Host, fmt.Sprint(req.Port)))
	client := &http.Client{Timeout: 6 * time.Second}
	resp, err := client.Post(
		fmt.Sprintf("http://%s/pair", net.JoinHostPort(req.Host, fmt.Sprint(req.Port))),
		"application/json", bytes.NewReader(payload))
	if err != nil {
		n.log.Warn("pair.req", "dst", req.Host, "err", err)
		writeErr(w, http.StatusBadGateway, "无法连接共享端："+err.Error())
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode != http.StatusOK {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(body)
		return
	}
	var pr struct {
		Token      string `json:"token"`
		ServerName string `json:"server_name"`
		Printers   []struct {
			Key     string `json:"key"`
			Name    string `json:"name"`
			URIPath string `json:"uri_path"`
		} `json:"printers"`
	}
	if err := json.Unmarshal(body, &pr); err != nil {
		writeErr(w, http.StatusBadGateway, "share side answered garbage: "+err.Error())
		return
	}
	host := req.Host
	if ip := net.ParseIP(host); ip == nil && strings.HasSuffix(host, ".local") {
		// Prefer the resolved address for the queue URL; .local names are
		// the fallback when resolution is unavailable.
		if addrs, err := net.LookupHost(host); err == nil && len(addrs) > 0 {
			host = addrs[0]
		}
	}
	type printerOut struct {
		Key     string `json:"key"`
		Name    string `json:"name"`
		URIPath string `json:"uri_path"`
		FullURL string `json:"ipp_url"`
	}
	printers := make([]printerOut, 0, len(pr.Printers))
	for _, p := range pr.Printers {
		printers = append(printers, printerOut{
			Key: p.Key, Name: p.Name, URIPath: p.URIPath,
			FullURL: fmt.Sprintf("ipp://%s:%d%s", host, req.Port, p.URIPath),
		})
	}
	n.log.Info("pair.ok", "dst", req.Host, "server", pr.ServerName, "printers", len(printers))
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"token":       pr.Token,
		"server_name": pr.ServerName,
		"printers":    printers,
	})
}

// apiQueueInstall installs a paired printer as a system queue (USE-4/5).
func (n *node) apiQueueInstall(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		Name       string `json:"name"`
		IPPURL     string `json:"ipp_url"`
		ServerName string `json:"server_name"`
		ServerHost string `json:"server_host"`
		ServerPort int    `json:"server_port"`
		URIPath    string `json:"uri_path"`
	}
	if !readJSONBody(w, r, &req) {
		return
	}
	if req.Name == "" || req.IPPURL == "" {
		writeErr(w, http.StatusBadRequest, "name and ipp_url required")
		return
	}
	name, res, err := sysqueue.InstallAuto(sysqueue.Spec{Name: req.Name, IPPURL: req.IPPURL})
	n.log.Info("queue.install", "qname", name, "rc", boolToInt(err == nil), "elevated", res.Elevated, "err", err, "cmd", res.Cmd)
	if err != nil {
		// One JSON body: the browser's res.json() cannot parse two
		// concatenated objects, so the error text and the helper result
		// (elevated/cmd context) must share a single write.
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": err.Error(), "result": res})
		return
	}
	n.added.upsert(addedPrinter{
		QueueName:   name,
		ServerName:  req.ServerName,
		PrinterName: req.Name,
		ServerHost:  req.ServerHost,
		ServerPort:  req.ServerPort,
		URIPath:     req.URIPath,
		AddedAt:     time.Now(),
	})
	_ = json.NewEncoder(w).Encode(map[string]any{"queue_name": name, "result": res})
}

func boolToInt(b bool) int {
	if b {
		return 0
	}
	return 1
}

func (n *node) apiQueueRemove(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		QueueName string `json:"queue_name"`
	}
	if !readJSONBody(w, r, &req) || req.QueueName == "" {
		writeErr(w, http.StatusBadRequest, "queue_name required")
		return
	}
	err := sysqueue.Remove(req.QueueName)
	n.log.Info("queue.remove", "qname", req.QueueName, "err", err)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	n.added.remove(req.QueueName)
	_ = json.NewEncoder(w).Encode(map[string]any{"removed": req.QueueName})
}

// apiQueueTest prints the embedded test page through a system queue
// (USE-6): the honest end-to-end check a human understands.
func (n *node) apiQueueTest(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		QueueName string `json:"queue_name"`
	}
	if !readJSONBody(w, r, &req) || req.QueueName == "" {
		writeErr(w, http.StatusBadRequest, "queue_name required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	start := time.Now()
	err := printTestPage(ctx, req.QueueName)
	n.log.Info("job.testpage", "qname", req.QueueName, "dur", time.Since(start).Round(time.Millisecond), "err", err)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

func (n *node) apiAddedPrinters(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	list := n.added.list()
	out := addedStatuses(list)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"printers": out})
}

// addedStatuses classifies the whole registry in one pass: ONE batched
// system-queue query (N PowerShell launches made the 15s UI poll race
// itself) plus parallel IPP probes behind a 10s cache, all under a 2.5s
// budget. A probe that misses the budget stays inconclusive → "ok", the
// same rule as a silent probe; the next poll refreshes it.
func addedStatuses(list []addedPrinter) []addedPrinterStatus {
	names := make([]string, 0, len(list))
	for _, a := range list {
		names = append(names, a.QueueName)
	}
	queueSet, qErr := sysqueue.Names(names)
	type probeKey struct {
		host string
		port int
		path string
	}
	probeOf := func(a addedPrinter) probeKey {
		return probeKey{strings.ToLower(strings.Trim(a.ServerHost, "[]")), a.ServerPort, a.URIPath}
	}
	// Dedup endpoints: two queues pointing at one share probe once.
	uniq := map[probeKey]bool{}
	for _, a := range list {
		if a.ServerHost != "" && a.ServerPort > 0 {
			uniq[probeOf(a)] = true
		}
	}
	probes := map[probeKey]ippProbe{}
	var pmu sync.Mutex
	ctx, cancel := context.WithTimeout(context.Background(), 2500*time.Millisecond)
	defer cancel()
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for k := range uniq {
		wg.Add(1)
		go func(k probeKey) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			pr := cachedProbeIPP(ctx, k.host, k.port, k.path)
			pmu.Lock()
			probes[k] = pr
			pmu.Unlock()
		}(k)
	}
	wg.Wait()
	out := make([]addedPrinterStatus, 0, len(list))
	for _, a := range list {
		queueOK := queueSet[strings.ToLower(a.QueueName)]
		queueKnown := qErr == nil
		probed := a.ServerHost != "" && a.ServerPort > 0
		var pr ippProbe
		if probed {
			pr = probes[probeOf(a)]
		}
		out = append(out, addedPrinterStatus{addedPrinter: a, Status: classifyAdded(queueOK, queueKnown, probed, pr)})
	}
	return out
}

// probeCacheEntry is one cached IPP probe result.
type probeCacheEntry struct {
	at time.Time
	pr ippProbe
}

var (
	probeCacheMu sync.Mutex
	probeCache   = map[string]probeCacheEntry{}
)

// cachedProbeIPP wraps probeIPP with a 10s result cache keyed by endpoint:
// the added list (15s poll) and the scan (known-node probes) hit the same
// share sides, and a dead endpoint must not cost a fresh 2s timeout on
// every call. The context budget bounds the underlying HTTP request.
func cachedProbeIPP(ctx context.Context, host string, port int, path string) ippProbe {
	key := host + "|" + strconv.Itoa(port) + "|" + path
	probeCacheMu.Lock()
	if e, ok := probeCache[key]; ok && time.Since(e.at) < 10*time.Second {
		probeCacheMu.Unlock()
		return e.pr
	}
	probeCacheMu.Unlock()
	pr := probeIPPContext(ctx, host, port, path, 2*time.Second)
	probeCacheMu.Lock()
	if len(probeCache) > 512 {
		probeCache = map[string]probeCacheEntry{}
	}
	probeCache[key] = probeCacheEntry{at: time.Now(), pr: pr}
	probeCacheMu.Unlock()
	return pr
}

// addedPrinterStatus is one registry entry plus its live receiver state
// (research/14 §7.2-B2): a read-only snapshot for the client home list.
type addedPrinterStatus struct {
	addedPrinter
	Status string `json:"status"`
}

// addedStatus classifies one added printer: missing (system queue gone),
// offline (share side did not speak IPP), revoked (share side answered
// 401) or ok. Reachability is an IPP Get-Printer-Attributes, not a TCP
// dial — a mesh fake-handshake would otherwise report "ok". Anything
// inconclusive stays ok; the diagnostics page is for deeper answers.
func addedStatus(a addedPrinter) string {
	ok, err := sysqueue.Exists(a.QueueName)
	queueKnown := err == nil
	probed := a.ServerHost != "" && a.ServerPort > 0
	var pr ippProbe
	if probed {
		pr = probeIPP(a.ServerHost, a.ServerPort, a.URIPath, 2*time.Second)
	}
	return classifyAdded(ok, queueKnown, probed, pr)
}

// classifyAdded is the pure half of addedStatus so the mesh false-positive
// cases can be tested without a system print queue.
func classifyAdded(queueOK, queueKnown, probed bool, pr ippProbe) string {
	if queueKnown && !queueOK {
		return "missing"
	}
	if !probed {
		return "ok"
	}
	if !pr.Alive {
		return "offline"
	}
	if pr.Revoked {
		return "revoked"
	}
	return "ok"
}

// apiDiagExport bundles what a helper needs to diagnose remotely
// (research/14 §6.3): summary, live config (no secrets — the admin token
// lives in its own file), doctor checks (share side + receiver side) and
// the tail of the rotating log. Served as one zip download.
func (n *node) apiDiagExport(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	n.mu.Lock()
	cfg := n.cfg
	n.mu.Unlock()

	n.log.Info("diag.export", "src", r.RemoteAddr)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	host, _ := os.Hostname()
	if s, err := zw.Create("summary.txt"); err == nil {
		fmt.Fprintf(s, "dyj %s\nos=%s/%s\nhost=%s\ntime=%s\nrole=%s\n",
			dyjVersion, runtime.GOOS, runtime.GOARCH, host,
			time.Now().Format(time.RFC3339), cfg.Role)
	}
	if s, err := zw.Create("config.json"); err == nil {
		if data, err := json.MarshalIndent(cfg, "", "  "); err == nil {
			_, _ = s.Write(data)
		}
	}
	if s, err := zw.Create("checks.json"); err == nil {
		checks := doctor.Run(doctorParams(cfg, n.stateDir, true))
		checks = append(checks, runClientChecks(n.stateDir)...)
		_ = json.NewEncoder(s).Encode(map[string]any{"checks": checks})
	}
	if s, err := zw.Create("log.txt"); err == nil {
		f, err := os.Open(filepath.Join(n.stateDir, "logs", "dyj.log"))
		if err != nil {
			fmt.Fprintln(s, "(log file not available:", err, ")")
		} else {
			const tail = 256 << 10
			if pos, err := f.Seek(0, io.SeekEnd); err == nil && pos > tail {
				fmt.Fprintf(s, "(...truncated, showing last %d bytes...)\n", tail)
				_, _ = f.Seek(-tail, io.SeekEnd)
			} else {
				_, _ = f.Seek(0, io.SeekStart)
			}
			_, _ = io.Copy(s, f)
			f.Close()
		}
	}
	if err := zw.Close(); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf("attachment; filename=dyj-diag-%s.zip", time.Now().Format("20060102-150405")))
	_, _ = w.Write(buf.Bytes())
}

// loadOrCreateDeviceID returns this machine's stable receiver identity.
func loadOrCreateDeviceID(stateDir string) (string, error) {
	return loadOrCreateUUID(filepath.Join(stateDir, "device-id"))
}

// ---------------------------------------------------------------------------
// added-printers registry (added-printers.json)
// ---------------------------------------------------------------------------

type addedPrinter struct {
	QueueName   string    `json:"queue_name"`
	ServerName  string    `json:"server_name,omitempty"`
	PrinterName string    `json:"printer_name,omitempty"`
	ServerHost  string    `json:"server_host"`
	ServerPort  int       `json:"server_port"`
	URIPath     string    `json:"uri_path"`
	AddedAt     time.Time `json:"added_at"`
}

type addedStore struct {
	mu      sync.Mutex
	path    string
	entries []addedPrinter
}

func openAddedStore(path string) *addedStore {
	s := &addedStore{path: path}
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &s.entries)
	}
	return s
}

func (s *addedStore) list() []addedPrinter {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]addedPrinter, len(s.entries))
	copy(out, s.entries)
	return out
}

func (s *addedStore) upsert(p addedPrinter) {
	s.mu.Lock()
	defer s.mu.Unlock()
	replaced := false
	for i := range s.entries {
		if s.entries[i].QueueName == p.QueueName || (p.ServerHost != "" && s.entries[i].ServerHost == p.ServerHost && s.entries[i].ServerPort == p.ServerPort && s.entries[i].URIPath == p.URIPath) {
			s.entries[i] = p
			replaced = true
			break
		}
	}
	if !replaced {
		s.entries = append(s.entries, p)
	}
	s.persist()
}

func (s *addedStore) remove(queueName string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.entries {
		if s.entries[i].QueueName == queueName {
			s.entries = append(s.entries[:i], s.entries[i+1:]...)
			break
		}
	}
	s.persist()
}

func (s *addedStore) persist() {
	if s.path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return
	}
	data, err := json.MarshalIndent(s.entries, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(s.path, data, 0o644)
}
