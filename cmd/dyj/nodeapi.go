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
		"pair":           http.HandlerFunc(n.apiPairProxy),
		"queue/install":  http.HandlerFunc(n.apiQueueInstall),
		"queue/remove":   http.HandlerFunc(n.apiQueueRemove),
		"queue/test":     http.HandlerFunc(n.apiQueueTest),
		"added-printers": http.HandlerFunc(n.apiAddedPrinters),
		"pairing":        http.HandlerFunc(n.apiPairing),
		"pairing/rotate": http.HandlerFunc(n.apiPairingRotate),
		"devices":        http.HandlerFunc(n.apiDevices),
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
		ifaces, _ := discovery.SelectInterfaces(nil)
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
		if addr := firstReachable(out[i].Addrs, out[i].Port); addr != "" {
			out[i].Reachable = true
		}
	}
	out = pinKnown(out, known)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"services": out})
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
	for _, a := range n.added.list() {
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
	if len(order) > 16 {
		order = order[:16]
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

// scanEntries turns discovered services into add-printer rows: it drops
// non-IPP wire noise and this machine's own instances, then merges the raw
// and reference-client views of the same printer.
//
// The merge key is the instance label + port ONLY. The SRV target differs
// per interface (the ${IFACE} naming in discovery), so including it in the
// key split one physical printer into one row per interface — research/15
// D-1. Same label+port on one link means the same printer: instance-name
// conflicts are renamed away at announce time (RFC 6762 §9).
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
		key := strings.ToLower(strings.TrimSuffix(label, ".")) + "|" + fmt.Sprint(f.Port)
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

func firstReachable(addrs []string, port int) string {
	for _, a := range addrs {
		conn, err := net.DialTimeout("tcp", net.JoinHostPort(a, fmt.Sprint(port)), 800*time.Millisecond)
		if err == nil {
			conn.Close()
			return a
		}
	}
	return ""
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
		writeErr(w, http.StatusInternalServerError, err.Error())
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
	out := make([]addedPrinterStatus, 0, len(list))
	for _, a := range list {
		out = append(out, addedPrinterStatus{addedPrinter: a, Status: addedStatus(a)})
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"printers": out})
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
		if s.entries[i].QueueName == p.QueueName || (p.ServerHost != "" && s.entries[i].ServerHost == p.ServerHost && s.entries[i].URIPath == p.URIPath) {
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
