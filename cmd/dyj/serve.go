package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/sq199/dyj/pkg/admin"
	"github.com/sq199/dyj/pkg/attributes"
	"github.com/sq199/dyj/pkg/driver"
	"github.com/sq199/dyj/pkg/driver/cups"
	"github.com/sq199/dyj/pkg/discovery"
	"github.com/sq199/dyj/pkg/ipp"
	"github.com/sq199/dyj/pkg/service"
	"github.com/sq199/dyj/pkg/spooler"
)

// serveOptions carries everything `dyj serve` needs. It is a plain
// struct so the service runner can load it from disk without parsing
// flags twice.
type serveOptions struct {
	Name     string `json:"name"`
	Port     int    `json:"port"`
	TLSPort  int    `json:"tls_port,omitempty"`
	Printer  string `json:"printer,omitempty"`
	Backend  string `json:"backend,omitempty"`
	Out      string `json:"out,omitempty"`
	Spool    string `json:"spool,omitempty"`
	Ifaces   string `json:"ifaces,omitempty"`
	NoMDNS   bool   `json:"no_mdns,omitempty"`
	Verbose  bool   `json:"verbose,omitempty"`
	Hostname string `json:"hostname,omitempty"`
}

func cmdServe(args []string) error {
	var o serveOptions
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.IntVar(&o.Port, "port", 6631, "IPP port (631 needs admin rights)")
	fs.IntVar(&o.TLSPort, "tls-port", 0, "also serve ipps:// on this port")
	fs.StringVar(&o.Name, "name", "", "printer name (default \"dyj on <host>\")")
	fs.StringVar(&o.Printer, "printer", "", "Windows print queue to print through")
	fs.StringVar(&o.Backend, "backend", "", "wingdi | cups | file | null (default: platform pick)")
	fs.StringVar(&o.Out, "out", "", "output dir for the file backend")
	fs.StringVar(&o.Spool, "spool", "", "spool directory")
	fs.StringVar(&o.Ifaces, "iface", "", "interfaces to advertise on")
	fs.BoolVar(&o.NoMDNS, "no-mdns", false, "skip mDNS advertisement")
	fs.BoolVar(&o.Verbose, "v", false, "verbose logging")
	fs.StringVar(&o.Hostname, "hostname", "", "mDNS host name override")
	if err := fs.Parse(args); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return serve(ctx, o)
}

// serveFromJSON is the service entry: options arrive as the JSON
// persisted by `dyj service install`.
func serveFromJSON(ctx context.Context, raw string) error {
	var o serveOptions
	if err := json.Unmarshal([]byte(raw), &o); err != nil {
		return fmt.Errorf("persisted service options are invalid: %w", err)
	}
	return serve(ctx, o)
}

// serve assembles and runs the whole server. ctx cancellation is the
// one stop signal for every entry point (console, SCM, systemd, launchd).
func serve(ctx context.Context, o serveOptions) error {
	// logRing feeds the admin UI's live log view; stderr stays the
	// primary sink for console runs and journald/launchd capture.
	logRing := admin.NewLogRing(500)
	logger := log.New(io.MultiWriter(os.Stderr, logRing), "", log.LstdFlags)

	if o.Name == "" {
		o.Name = "dyj on " + hostnameOr("localhost")
	}

	ifaces, err := discovery.SelectInterfaces(splitList(o.Ifaces))
	if err != nil {
		return err
	}
	if len(ifaces) == 0 {
		return errors.New("no candidate multicast interface; run `dyj list-interfaces`")
	}
	ips := discovery.InterfaceAddrs(ifaces)

	// ---- state dir: spool + persisted UUID + TLS key ----
	stateDir := o.Spool
	if stateDir == "" {
		stateDir = defaultStateDir()
	}
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return fmt.Errorf("creating state dir: %w", err)
	}
	uuid, err := loadOrCreateUUID(filepath.Join(stateDir, "uuid.txt"))
	if err != nil {
		return err
	}

	// ---- backend ----
	// Default pick: wingdi on Windows; elsewhere a running CUPS with at
	// least one queue is the USEFUL default (sharing a real printer is
	// the product), falling back to the file backend on headless hosts.
	backendName := o.Backend
	if backendName == "" {
		if runtime.GOOS == "windows" {
			backendName = "wingdi"
		} else if qs, err := cups.Queues(); err == nil && len(qs) > 0 {
			backendName = "cups"
			logger.Printf("dyj: no -backend given, using cups queue %q (lpstat -p)", qs[0].Name)
		} else {
			backendName = "file"
		}
	}
	backend, err := buildBackend(backendName, o)
	if err != nil {
		return err
	}

	// ---- spooler ----
	spool, err := spooler.New(spooler.Options{
		Dir:     filepath.Join(stateDir, "spool"),
		Backend: backend,
		Logger:  logger,
	})
	if err != nil {
		return err
	}

	// ---- printer profile ----
	path := "/ipp/print"
	printer := ipp.DefaultPrinter()
	printer.Name = o.Name
	printer.Port = o.Port
	printer.UUID = "urn:uuid:" + uuid
	printer.Info = "dyj shared printer (" + backendName + ")"
	// Honest format declaration (ADR-0007): application/pdf joins the
	// table ONLY when a rasterizer (poppler pdftoppm) exists on this
	// machine; the mDNS pdl= promise stays in lockstep.
	if driver.PDFAvailable() {
		printer.DocumentFormats = append(attributes.DefaultFormats(), "application/pdf")
	}
	printer.URIs = nil
	for _, ip := range ips {
		printer.URIs = append(printer.URIs, fmt.Sprintf("ipp://%s:%d%s", ip, o.Port, path))
	}
	printer.URIs = append(printer.URIs, fmt.Sprintf("ipp://127.0.0.1:%d%s", o.Port, path))
	if err := ipp.ValidatePrinter(printer); err != nil {
		return err
	}

	// ---- admin UI (ADR-0008) ----
	// Loopback requests have full access; LAN requests may read status
	// and the job list, and must present the token for anything else.
	token, err := admin.LoadOrCreateToken(filepath.Join(stateDir, "admin-token"))
	if err != nil {
		return err
	}
	adminHandler := admin.New(admin.Options{
		StatusFn: func() admin.Status {
			return admin.Status{
				Version:     dyjVersion,
				PrinterName: printer.Name,
				PrinterInfo: printer.Info,
				URIs:        printer.URIs,
				Backend:     backend.Name(),
				Port:        o.Port,
				TLSPort:     o.TLSPort,
				MDNS:        !o.NoMDNS,
				AdminURL:    fmt.Sprintf("http://127.0.0.1:%d/admin", o.Port),
				Formats:     printer.DocumentFormats,
			}
		},
		ConfigGet: func() any { return o },
		ConfigPut: func(raw []byte) error {
			next, err := parseServeOptionsJSON(raw)
			if err != nil {
				return err
			}
			persisted, err := json.Marshal(next)
			if err != nil {
				return err
			}
			return service.SaveOptions(string(persisted))
		},
		Spool:       spool,
		Logs:        logRing,
		Token:       token,
		Diagnostics: runDiagnostics(o),
		Web:         admin.DefaultWeb(),
	})
	srv := ipp.NewServer(ipp.Options{Printer: printer, Sink: spool, Logger: logger, Admin: adminHandler})

	// ---- listeners ----
	hostName := o.Hostname
	if hostName == "" {
		hostName = discovery.DefaultHostName(o.Name)
	}
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", o.Port))
	if err != nil {
		return fmt.Errorf("listen :%d: %w", o.Port, err)
	}
	errCh := make(chan error, 4)
	go func() { errCh <- srv.Serve(ln) }()

	if o.TLSPort > 0 {
		if _, err := loadOrCreateTLSCert(stateDir, o.Name, hostName); err != nil {
			return err
		}
		tln, err := net.Listen("tcp", fmt.Sprintf(":%d", o.TLSPort))
		if err != nil {
			return fmt.Errorf("listen tls :%d: %w", o.TLSPort, err)
		}
		go func() { errCh <- srv.ServeTLS(tln, certFile(stateDir), keyFile(stateDir)) }()
		printer.URIs = append(printer.URIs, fmt.Sprintf("ipps://%s:%d%s", ips[0], o.TLSPort, path))
	}

	// ---- mDNS ----
	var adv *discovery.Advertiser
	if !o.NoMDNS {
		adv, err = discovery.New(discovery.Config{
			Instance: o.Name,
			HostName: hostName,
			Port:     o.Port,
			IPs:      ips,
			Path:     path,
			Ifaces:   ifaces,
			Logger:   logger,
			TXT: discovery.BuildTXT(discovery.TXTParams{
				Port:     o.Port,
				Path:     path,
				Type:     o.Name,
				UUID:     uuid,
				PDL:      pdlPromise(),
				AdminURL: fmt.Sprintf("http://%s:%d/admin", ips[0], o.Port),
				Color:    printer.ColorSupported,
			}),
		})
		if err != nil {
			return fmt.Errorf("mdns setup: %w", err)
		}
		if err := adv.Start(); err != nil {
			return fmt.Errorf("mdns start: %w", err)
		}
		defer adv.Close()
	}

	// ---- report ----
	fmt.Printf("dyj: backend        %s\n", backend.Name())
	fmt.Printf("dyj: IPP endpoint   http://%s:%d%s\n", ips[0], o.Port, path)
	if o.TLSPort > 0 {
		fmt.Printf("dyj: IPPS endpoint  https://%s:%d%s (self-signed TLS)\n", ips[0], o.TLSPort, path)
	}
	fmt.Printf("dyj: admin UI       http://127.0.0.1:%d/admin\n", o.Port)
	fmt.Printf("dyj: admin token    %s（本机免填；局域网管理需在管理页输入一次）\n", token)
	for _, u := range printer.URIs {
		fmt.Printf("dyj: printer URI    %s\n", u)
	}
	if adv != nil {
		fmt.Printf("dyj: mDNS           %s / %s -> %s:%d\n",
			adv.Instance(), adv.InstanceFQDN(), hostNameOr(o.Hostname, o.Name), o.Port)
	}
	fmt.Printf("dyj: spool          %s\n", stateDir)
	fmt.Println("dyj: Windows 11 -> Settings -> Bluetooth & devices -> Printers & scanners ->")
	fmt.Printf("     Add device -> Add manually -> Add a printer using an IP address or hostname -> IPP -> %s\n",
		printer.URIs[0])

	// ---- wait for ctx / failure ----
	select {
	case <-ctx.Done():
		fmt.Println("dyj: shutting down")
	case err := <-errCh:
		if err != nil {
			return err
		}
	}

	shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := spool.Close(shutCtx); err != nil {
		logger.Printf("dyj: spool close: %v", err)
	}
	return srv.Shutdown(shutCtx)
}

func hostNameOr(override, name string) string {
	if override != "" {
		return override
	}
	return discovery.DefaultHostName(name)
}

func buildBackend(name string, o serveOptions) (driver.Interface, error) {
	switch name {
	case "wingdi":
		if runtime.GOOS != "windows" {
			return nil, errors.New("backend wingdi requires Windows")
		}
		queue := o.Printer
		if queue == "" {
			return nil, errors.New("backend wingdi requires -printer <queue name> (see Get-Printer)")
		}
		return &driver.WingDI{Printer: queue}, nil
	case "cups":
		queue := o.Printer
		if queue == "" {
			qs, err := cups.Queues()
			if err != nil {
				return nil, fmt.Errorf("backend cups needs -printer <queue>: %w", err)
			}
			if len(qs) == 0 {
				return nil, errors.New("backend cups needs -printer <queue> (lpstat -p shows none)")
			}
			queue = qs[0].Name
		}
		return cups.New(queue)
	case "file":
		out := o.Out
		if out == "" {
			out = "dyj-out"
		}
		return driver.NewFile(out)
	case "null":
		return &driver.Null{}, nil
	default:
		return nil, fmt.Errorf("unknown backend %q (want wingdi|cups|file|null)", name)
	}
}

// pdlPromise is the mDNS pdl= value: exactly what the decoder accepts
// on this machine (attributes.DefaultFormats + optional PDF).
func pdlPromise() string {
	formats := attributes.DefaultFormats()
	if driver.PDFAvailable() {
		formats = append(formats, "application/pdf")
	}
	return strings.Join(formats, ",")
}

func splitList(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func hostnameOr(def string) string {
	h, err := os.Hostname()
	if err != nil || h == "" {
		return def
	}
	return h
}

// asciiHostName reduces a host name to what a DNS SAN can hold: IA5String
// allows only ASCII, so a CJK machine name ("生命") must fold to dashes —
// and if nothing printable survives, fall back to "dyj".
func asciiHostName(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z':
			c += 'a' - 'A'
		case (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-':
		default:
			c = '-'
		}
		out = append(out, c)
	}
	for len(out) > 0 && (out[0] == '-' || out[0] == '.') {
		out = out[1:]
	}
	for len(out) > 0 && (out[len(out)-1] == '-' || out[len(out)-1] == '.') {
		out = out[:len(out)-1]
	}
	if len(out) == 0 {
		return "dyj"
	}
	return string(out)
}

// uuid.txt: a random UUIDv4 persisted next to the spool, so the
// printer-uuid clients see stays stable across restarts.
func loadOrCreateUUID(path string) (string, error) {
	if data, err := os.ReadFile(path); err == nil {
		if s := strings.TrimSpace(string(data)); len(s) == 36 {
			return s, nil
		}
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10xx
	s := fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
	if err := os.WriteFile(path, []byte(s+"\n"), 0o644); err != nil {
		return "", err
	}
	return s, nil
}

func certFile(dir string) string { return filepath.Join(dir, "tls.crt") }
func keyFile(dir string) string  { return filepath.Join(dir, "tls.key") }

// loadOrCreateTLSCert returns the persisted self-signed certificate,
// generating one on first run (10 years, the printer hostname + IPs in
// SANs). Clients that verify strictly must trust it; AirPrint-class
// clients accept it.
func loadOrCreateTLSCert(dir, name, hostName string) (tls.Certificate, error) {
	certPath, keyPath := certFile(dir), keyFile(dir)
	if cert, err := tls.LoadX509KeyPair(certPath, keyPath); err == nil {
		return cert, nil
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, err
	}
	tmpl := x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: name, Organization: []string{"dyj"}},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(10 * 365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost", asciiHostName(hostName) + ".local"},
	}
	tmpl.IPAddresses = append(tmpl.IPAddresses, net.IPv4(127, 0, 0, 1), net.IPv6loopback)
	if ips := localIPs(); len(ips) > 0 {
		tmpl.IPAddresses = append(tmpl.IPAddresses, ips...)
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return tls.Certificate{}, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	if err := os.WriteFile(certPath, certPEM, 0o644); err != nil {
		return tls.Certificate{}, err
	}
	// The private key is written user-read-only; it never leaves this
	// machine and never enters the repository or logs.
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		return tls.Certificate{}, err
	}
	return tls.X509KeyPair(certPEM, keyPEM)
}

func localIPs() []net.IP {
	var out []net.IP
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	for i := range ifaces {
		addrs, err := ifaces[i].Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			if ipnet, ok := a.(*net.IPNet); ok && ipnet.IP.To4() != nil && !ipnet.IP.IsLoopback() {
				out = append(out, ipnet.IP)
			}
		}
	}
	return out
}

func defaultStateDir() string {
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(os.Getenv("ProgramData"), "dyj")
	case "darwin":
		return "/Library/Application Support/dyj"
	default:
		return "/var/lib/dyj"
	}
}
