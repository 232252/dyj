// node.go is the single assembly point of a running dyj (research/13
// §4.1/§4.5 of the ADR-0010 design): one HTTP listener that serves
//
//	/admin/*  the web UI + admin API (loopback full / LAN token-gated)
//	/pair     the PIN handshake
//	everything else — the IPP Router (per-printer servers, /p/<token> gate)
//
// plus mDNS advertisement and the share stack (backend + spooler per
// printer). The node ALWAYS starts — even with zero printers or a broken
// queue (bootstrap mode): the wizard must be reachable to fix things.
// Config saves rebuild the share stack in-process; the listener never
// closes (hot reload, ADR-0010 D5).
package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/232252/dyj/pkg/admin"
	"github.com/232252/dyj/pkg/attributes"
	"github.com/232252/dyj/pkg/discovery"
	"github.com/232252/dyj/pkg/doctor"
	"github.com/232252/dyj/pkg/driver"
	"github.com/232252/dyj/pkg/ipp"
	"github.com/232252/dyj/pkg/logx"
	"github.com/232252/dyj/pkg/pairing"
	"github.com/232252/dyj/pkg/service"
	"github.com/232252/dyj/pkg/spooler"
)

// runMode tunes the console/UX behavior of one node run.
type runMode string

const (
	modeConsole runMode = "console" // `dyj serve`: banner, no browser
	modeService runMode = "service" // SCM/systemd/launchd: quiet
	modeLaunch  runMode = "launch"  // `dyj` no-arg: banner + open browser
)

// sharedPrinter is one live printer inside the share stack.
type sharedPrinter struct {
	spec    printerSpec
	backend driver.Interface
	spool   *spooler.Spooler
	profile ipp.Printer
	uuid    string
}

// node owns the whole runtime.
type node struct {
	mu         sync.Mutex
	cfg        serveOptions
	stateDir   string
	actualPort int

	log     *logx.Logger
	legacy  *log.Logger
	logRing *admin.LogRing

	pair  *pairing.Store
	added *addedStore
	share []*sharedPrinter
	adv   []*discovery.Advertiser

	swap    *swapHandler
	httpSrv *http.Server

	ifaces []*net.Interface
	ips    []net.IP
}

// swapHandler lets the IPP router be rebuilt under a live listener.
type swapHandler struct {
	cur atomic.Value // http.Handler
}

func (s *swapHandler) Store(h http.Handler) { s.cur.Store(h) }

func (s *swapHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h, ok := s.cur.Load().(http.Handler); ok && h != nil {
		h.ServeHTTP(w, r)
		return
	}
	http.Error(w, "dyj: not ready", http.StatusServiceUnavailable)
}

// runNode assembles and runs the whole node; ctx cancellation is the one
// stop signal for every entry point.
func runNode(ctx context.Context, raw serveOptions, mode runMode) error {
	o := normalizeServeOptions(raw)

	stateDir := o.Spool
	if stateDir == "" {
		stateDir = defaultStateDir()
	}
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return fmt.Errorf("creating state dir: %w", err)
	}

	// ---- logging: stderr + admin ring + rotating file (research/13 §5.3)
	logRing := admin.NewLogRing(500)
	rot, rotErr := logx.OpenRotating(filepath.Join(stateDir, "logs", "dyj.log"), 10<<20, 3)
	sinks := []io.Writer{os.Stderr, logRing}
	if rotErr == nil {
		sinks = append(sinks, rot)
	}
	lg := logx.New(io.MultiWriter(sinks...), logx.ParseLevel(o.Verbose))
	legacy := lg.Legacy()
	defer func() {
		if rot != nil {
			_ = rot.Close()
		}
	}()

	ifaces, err := discovery.SelectInterfaces(splitList(o.Ifaces))
	if err != nil || len(ifaces) == 0 {
		// Bootstrap principle: no multicast interface costs us mDNS, not
		// the node. IPP still works by IP.
		lg.Warn("boot.mdns_skip", "err", err, "hint", "dyj list-interfaces")
		ifaces = nil
	}
	ips := discovery.InterfaceAddrs(ifaces)
	if len(ips) == 0 {
		ips = localIPs()
	}
	if len(ips) == 0 {
		ips = []net.IP{net.IPv4(127, 0, 0, 1)}
	}

	n := &node{
		cfg:      o,
		stateDir: stateDir,
		log:      lg,
		legacy:   legacy,
		logRing:  logRing,
		ifaces:   ifaces,
		ips:      ips,
		swap:     &swapHandler{},
	}

	pairStore, err := pairing.Open(filepath.Join(stateDir, "paired-devices.json"), pairing.Limits{}, lg)
	if err != nil {
		lg.Warn("boot.pairing", "err", err, "note", "pairing disabled, data plane stays loopback-only")
	}
	n.pair = pairStore
	defer func() {
		if n.pair != nil {
			n.pair.Close()
		}
	}()
	n.added = openAddedStore(filepath.Join(stateDir, "added-printers.json"))

	// ---- share stack (may end up empty — bootstrap) ----
	if sharesRole(o.Role) {
		if err := n.buildShare(); err != nil {
			// A broken share config must never take the admin UI down.
			lg.Error("cfg.rebuild", "err", err)
		}
	}

	// ---- gate: loopback full access, LAN needs a paired token ----
	gate := func(r *http.Request, token string) bool {
		if isLoopbackRequest(r) {
			return true
		}
		if token == "" || n.pair == nil {
			return false
		}
		if id, ok := n.pair.Verify(token); ok {
			_ = id
			return true
		}
		lg.Info("deny.ipp", "src", hostOf(r.RemoteAddr))
		return false
	}
	n.swap.Store(ipp.NewRouter(ipp.RouterOptions{
		Entries: n.routerEntries(),
		Gate:    gate,
		Logger:  legacy,
	}))

	// ---- admin UI + APIs ----
	token, err := admin.LoadOrCreateToken(filepath.Join(stateDir, "admin-token"))
	if err != nil {
		return fmt.Errorf("admin token: %w", err)
	}
	adminHandler := admin.New(admin.Options{
		StatusFn:    n.adminStatus,
		ConfigGet:   n.configGet,
		ConfigPut:   n.applyConfig,
		Spool:       &nodeSpool{n: n},
		Logs:        logRing,
		Token:       token,
		Diagnostics: n.diagnostics,
		Web:         admin.DefaultWeb(),
		Extra:       n.extraAPIs(),
		LiveReload:  true,
	})

	root := http.NewServeMux()
	root.Handle("/admin", adminHandler)
	root.Handle("/admin/", adminHandler)
	root.Handle("/pair", n.pairHandler())
	root.Handle("/", n.swap)

	n.httpSrv = &http.Server{
		Handler:           root,
		ReadHeaderTimeout: 10 * time.Second,
		MaxHeaderBytes:    1 << 16,
		IdleTimeout:       120 * time.Second,
	}

	// ---- listeners (probe upward so the node ALWAYS comes up) ----
	port := o.Port
	ln, lerr := net.Listen("tcp", fmt.Sprintf(":%d", port))
	for i := 1; lerr != nil && i < 10; i++ {
		port = o.Port + i
		ln, lerr = net.Listen("tcp", fmt.Sprintf(":%d", port))
	}
	if lerr != nil {
		return fmt.Errorf("listen :%d..: no free port: %w", o.Port, lerr)
	}
	if port != o.Port {
		lg.Warn("boot.listen", "cfg_port", o.Port, "using", port, "note", "配置端口被占用")
	}
	n.actualPort = port
	lg.Info("boot.listen", "port", port, "role", o.Role)

	errCh := make(chan error, 4)
	go func() { errCh <- n.httpSrv.Serve(ln) }()

	// TLS listener (same handler set).
	if o.TLSPort > 0 {
		hostName := hostNameOr(o.Hostname, o.Name)
		if _, err := loadOrCreateTLSCert(stateDir, o.Name, hostName); err != nil {
			return err
		}
		tport := o.TLSPort
		tln, terr := net.Listen("tcp", fmt.Sprintf(":%d", tport))
		for i := 1; terr != nil && i < 10; i++ {
			tport = o.TLSPort + i
			tln, terr = net.Listen("tcp", fmt.Sprintf(":%d", tport))
		}
		if terr == nil {
			cert, cerr := tls.LoadX509KeyPair(certFile(stateDir), keyFile(stateDir))
			if cerr != nil {
				lg.Warn("boot.listen_tls", "err", cerr)
			} else {
				go func() {
					errCh <- n.httpSrv.Serve(tls.NewListener(tln, &tls.Config{
						Certificates: []tls.Certificate{cert},
						MinVersion:   tls.VersionTLS12,
					}))
				}()
			}
		} else {
			lg.Warn("boot.listen_tls", "err", terr)
		}
	}

	// ---- mDNS advertisement (per printer) ----
	n.startMDNS(hostNameOr(o.Hostname, o.Name))

	lg.Info("boot.ready", "role", o.Role, "port", port, "printers", len(n.shareSlice()), "mode", string(mode))

	// ---- banner + browser ----
	if mode != modeService {
		n.banner(token)
	}
	if mode == modeLaunch {
		go func() {
			time.Sleep(400 * time.Millisecond) // let the listener settle
			openBrowser(fmt.Sprintf("http://127.0.0.1:%d/admin/%s", port, launchHash(o.Role)))
		}()
	}

	// ---- run until stopped ----
	<-ctx.Done()
	lg.Info("boot.stop", "printers", len(n.shareSlice()))
	n.stopMDNS()
	shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	n.closeSpools(shutCtx)
	return n.httpSrv.Shutdown(shutCtx)
}

func launchHash(role string) string {
	if role == "" {
		return "#/setup"
	}
	if role == roleUse {
		return "#/add"
	}
	return ""
}

// ---------------------------------------------------------------------------
// share stack build / rebuild
// ---------------------------------------------------------------------------

// buildShare (re)creates backends, spoolers and profiles for the configured
// printers. Errors disable the offending printer, never the node.
func (n *node) buildShare() error {
	n.stopMDNS()
	n.closeSpools(context.Background())

	cfg := n.cfg
	if !sharesRole(cfg.Role) || len(cfg.Printers) == 0 {
		n.mu.Lock()
		n.share = nil
		n.mu.Unlock()
		n.log.Info("share.enumerate", "printers", 0, "note", "bootstrap (no printers configured)")
		return nil
	}

	var out []*sharedPrinter
	for i, spec := range cfg.Printers {
		backend, err := buildBackendFor(spec, cfg)
		if err != nil {
			n.log.Warn("share.backend", "printer", spec.Key, "queue", spec.Queue, "err", err)
			continue // partial availability beats total outage
		}
		spoolDir := filepath.Join(n.stateDir, "spool")
		if spec.Key != "" {
			spoolDir = filepath.Join(spoolDir, spec.Key)
		}
		spool, err := spooler.New(spooler.Options{
			Dir:     spoolDir,
			Backend: backend,
			IDBase:  int32(i) * 1_000_000,
			Logger:  n.legacy,
		})
		if err != nil {
			n.log.Warn("share.backend", "printer", spec.Key, "err", err)
			continue
		}
		uuid, err := loadOrCreateUUID(filepath.Join(n.stateDir, "uuid-"+spec.Key+".txt"))
		if err != nil {
			uuid = "00000000-0000-4000-8000-000000000001"
		}
		profile := printerProfileFor(spec, cfg, uuid, n.ips, n.actualPort)
		n.log.Info("share.backend", "printer", spec.Key, "backend", backend.Name(), "queue", spec.Queue)
		out = append(out, &sharedPrinter{spec: spec, backend: backend, spool: spool, profile: profile, uuid: uuid})
	}

	n.mu.Lock()
	n.share = out
	n.mu.Unlock()
	n.startMDNS(hostNameOr(cfg.Hostname, cfg.Name))
	return nil
}

func printerProfileFor(spec printerSpec, cfg serveOptions, uuid string, ips []net.IP, port int) ipp.Printer {
	p := ipp.DefaultPrinter()
	p.Name = spec.Name
	p.Key = spec.Key
	p.Port = port
	p.UUID = "urn:uuid:" + uuid
	p.Info = "dyj shared printer"
	path := "/ipp/print"
	if spec.Key != "" {
		path += "/" + spec.Key
	}
	p.DocumentFormats = attributes.DefaultFormats()
	if driver.PDFAvailable() {
		p.DocumentFormats = append(p.DocumentFormats, "application/pdf")
	}
	p.URIs = nil
	for _, ip := range ips {
		p.URIs = append(p.URIs, fmt.Sprintf("ipp://%s:%d%s", ip, port, path))
	}
	p.URIs = append(p.URIs, fmt.Sprintf("ipp://127.0.0.1:%d%s", port, path))
	return p
}

// startMDNS advertises every live printer as its own _ipp._tcp instance.
func (n *node) startMDNS(hostName string) {
	cfg := n.cfg
	if cfg.NoMDNS || len(n.ifaces) == 0 {
		return
	}
	fqdn := discovery.DefaultHostName(hostName)
	for _, sp := range n.shareSlice() {
		path := "/ipp/print"
		if sp.spec.Key != "" {
			path += "/" + sp.spec.Key
		}
		adv, err := discovery.New(discovery.Config{
			Instance: sp.spec.Name,
			HostName: fqdn,
			Port:     n.actualPort,
			IPs:      n.ips,
			Path:     path,
			Ifaces:   n.ifaces,
			Logger:   n.legacy,
			TXT: discovery.BuildTXT(discovery.TXTParams{
				Port:     n.actualPort,
				Path:     path,
				Type:     sp.spec.Name,
				UUID:     sp.uuid,
				PDL:      pdlPromise(),
				AdminURL: fmt.Sprintf("http://%s:%d/admin", n.ips[0], n.actualPort),
				Color:    sp.profile.ColorSupported,
				Extra: map[string]string{
					"dyj":    "1",
					"pair":   "1",
					"dyjver": "3",
				},
			}),
		})
		if err != nil {
			n.log.Warn("mdns.announce", "printer", sp.spec.Key, "err", err)
			continue
		}
		if err := adv.Start(); err != nil {
			n.log.Warn("mdns.announce", "printer", sp.spec.Key, "err", err)
			continue
		}
		n.mu.Lock()
		n.adv = append(n.adv, adv)
		n.mu.Unlock()
		n.log.Info("mdns.announce", "printer", sp.spec.Key, "instance", adv.Instance(), "ifaces", len(n.ifaces))
	}
}

func (n *node) stopMDNS() {
	n.mu.Lock()
	advs := n.adv
	n.adv = nil
	n.mu.Unlock()
	for _, adv := range advs {
		adv.Close() // goodbye packets included
	}
}

func (n *node) closeSpools(ctx context.Context) {
	n.mu.Lock()
	share := n.share
	n.share = nil
	n.mu.Unlock()
	for _, sp := range share {
		if err := sp.spool.Close(ctx); err != nil {
			n.log.Warn("cfg.rebuild", "err", err, "printer", sp.spec.Key)
		}
	}
}

func (n *node) shareSlice() []*sharedPrinter {
	n.mu.Lock()
	defer n.mu.Unlock()
	out := make([]*sharedPrinter, len(n.share))
	copy(out, n.share)
	return out
}

func (n *node) routerEntries() []ipp.Entry {
	entries := make([]ipp.Entry, 0)
	for _, sp := range n.shareSlice() {
		entries = append(entries, ipp.Entry{Printer: sp.profile, Sink: sp.spool})
	}
	return entries
}

func (n *node) sharedPrinterList() []pairing.SharedPrinter {
	out := make([]pairing.SharedPrinter, 0)
	for _, sp := range n.shareSlice() {
		out = append(out, pairing.SharedPrinter{Key: sp.spec.Key, Name: sp.spec.Name})
	}
	return out
}

func (n *node) currentName() string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.cfg.Name
}

// ---------------------------------------------------------------------------
// admin wiring: status / config / aggregate spool control
// ---------------------------------------------------------------------------

func (n *node) adminStatus() admin.Status {
	n.mu.Lock()
	cfg := n.cfg
	n.mu.Unlock()
	st := admin.Status{
		Version:        dyjVersion,
		PrinterName:    cfg.Name,
		PrinterInfo:    "dyj shared printer",
		Backend:        "multi",
		Port:           n.actualPort,
		TLSPort:        cfg.TLSPort,
		MDNS:           !cfg.NoMDNS,
		AdminURL:       fmt.Sprintf("http://127.0.0.1:%d/admin", n.actualPort),
		Role:           cfg.Role,
		PairingEnabled: n.pair != nil,
	}
	first := true
	for _, sp := range n.shareSlice() {
		accepting, queued, _ := sp.spool.Status()
		st.Printers = append(st.Printers, admin.PrinterStatus{
			Key: sp.spec.Key, Name: sp.spec.Name,
			Backend: sp.backend.Name(), Accepting: accepting, Queued: queued,
		})
		st.URIs = append(st.URIs, sp.profile.URIs...)
		if first {
			st.PrinterName = sp.spec.Name
			st.Formats = sp.profile.DocumentFormats
			st.Backend = sp.backend.Name()
			first = false
		}
	}
	if st.Printers == nil {
		st.Printers = []admin.PrinterStatus{}
	}
	if st.URIs == nil {
		st.URIs = []string{}
	}
	return st
}

func (n *node) configGet() any {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.cfg
}

// applyConfig validates, persists and LIVE-APPLIES a new configuration
// (admin PUT /api/config): rebuild the share stack and the mDNS set under
// the same listener. The returned error surfaces as HTTP 400.
func (n *node) applyConfig(raw []byte) error {
	next, err := parseServeOptionsJSON(raw)
	if err != nil {
		return err
	}
	persisted, err := marshalOptions(next)
	if err != nil {
		return err
	}
	if serr := service.SaveOptions(persisted); serr != nil {
		// Live apply still holds for this process; the service context
		// (SYSTEM/root) persists on its next save. Surface a warning.
		n.log.Warn("cfg.save", "err", serr, "note", "本进程已生效；持久化由服务上下文完成")
	}
	n.mu.Lock()
	n.cfg = next
	n.mu.Unlock()
	if err := n.buildShare(); err != nil {
		n.log.Error("cfg.rebuild", "err", err)
	}
	n.swap.Store(ipp.NewRouter(ipp.RouterOptions{
		Entries: n.routerEntries(),
		Gate:    n.gate(),
		Logger:  n.legacy,
	}))
	n.log.Info("cfg.save", "role", next.Role, "printers", len(next.Printers))
	return nil
}

// gate rebuilds the data-plane gate (loopback or valid pairing token).
func (n *node) gate() func(*http.Request, string) bool {
	return func(r *http.Request, token string) bool {
		if isLoopbackRequest(r) {
			return true
		}
		if token == "" || n.pair == nil {
			return false
		}
		_, ok := n.pair.Verify(token)
		return ok
	}
}

// nodeSpool aggregates the per-printer spoolers into the admin control
// surface; with zero printers it reports the honest not-configured state.
type nodeSpool struct{ n *node }

func (s *nodeSpool) Status() (bool, int32, []string) {
	share := s.n.shareSlice()
	if len(share) == 0 {
		return false, 0, []string{"not-configured"}
	}
	accepting := true
	var queued int32
	reasons := map[string]bool{}
	for _, sp := range share {
		a, q, rs := sp.spool.Status()
		accepting = accepting && a
		queued += q
		for _, r := range rs {
			reasons[r] = true
		}
	}
	out := make([]string, 0, len(reasons))
	for r := range reasons {
		out = append(out, r)
	}
	sort.Strings(out)
	return accepting, queued, out
}

func (s *nodeSpool) SetAccepting(accepting bool) {
	for _, sp := range s.n.shareSlice() {
		sp.spool.SetAccepting(accepting)
	}
}

func (s *nodeSpool) Snapshot(which string) []ipp.JobView {
	var out []ipp.JobView
	for _, sp := range s.n.shareSlice() {
		out = append(out, sp.spool.Snapshot(which)...)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (s *nodeSpool) Cancel(id int32) error {
	for _, sp := range s.n.shareSlice() {
		if _, ok := sp.spool.View(id); ok {
			return sp.spool.Cancel(id)
		}
	}
	return ipp.ErrUnknownJob
}

// ---------------------------------------------------------------------------
// diagnostics + banner
// ---------------------------------------------------------------------------

func (n *node) diagnostics(any) any {
	n.mu.Lock()
	cfg := n.cfg
	n.mu.Unlock()
	return doctor.Run(doctorParams(cfg, n.stateDir, true))
}

func (n *node) banner(token string) {
	cfg := n.cfg
	fmt.Printf("dyj: role           %s\n", roleLabel(cfg.Role))
	if len(n.shareSlice()) == 0 {
		fmt.Printf("dyj: IPP endpoint   http://%s:%d/ipp/print (未配置打印机 — 打开向导)\n", n.ips[0], n.actualPort)
	}
	for _, sp := range n.shareSlice() {
		fmt.Printf("dyj: backend        %s\n", sp.backend.Name())
		fmt.Printf("dyj: IPP endpoint   %s\n", sp.profile.URIs[0])
	}
	fmt.Printf("dyj: admin UI       http://127.0.0.1:%d/admin\n", n.actualPort)
	fmt.Printf("dyj: admin token    %s（本机免填；局域网管理需在管理页输入一次）\n", token)
	if n.pair != nil && len(n.shareSlice()) > 0 {
		pin, remain := n.pair.Current()
		fmt.Printf("dyj: 配对码          %s（%d 秒内有效，接收端添加打印机时输入）\n", pin, remain)
	}
	fmt.Printf("dyj: spool          %s\n", n.stateDir)
	if len(n.shareSlice()) == 0 {
		fmt.Println("dyj: 打开管理页完成共享设置，或让接收端机器直接添加本机打印机")
	}
}

func roleLabel(role string) string {
	switch role {
	case roleShare:
		return "share（共享本机打印机）"
	case roleUse:
		return "use（添加网络打印机）"
	case roleBoth:
		return "both（共享 + 接收）"
	default:
		return "未配置（bootstrap）"
	}
}

// pairHandler mounts /pair, refusing politely when the device store could
// not open (data plane is loopback-only in that case).
func (n *node) pairHandler() http.Handler {
	inner := &pairing.Handler{
		Store:      n.pair,
		Printers:   n.sharedPrinterList,
		ServerName: func() string { return n.currentName() },
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if n.pair == nil {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":"pairing disabled"}` + "\n"))
			return
		}
		inner.ServeHTTP(w, r)
	})
}

// ---------------------------------------------------------------------------
// small helpers
// ---------------------------------------------------------------------------

func isLoopbackRequest(r *http.Request) bool {
	ip := net.ParseIP(hostOf(r.RemoteAddr))
	return ip != nil && ip.IsLoopback()
}

func hostOf(remoteAddr string) string {
	if h, _, err := net.SplitHostPort(remoteAddr); err == nil {
		return h
	}
	return remoteAddr
}
