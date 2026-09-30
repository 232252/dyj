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
	"flag"
	"fmt"
	"math/big"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/sq199/dyj/pkg/attributes"
	"github.com/sq199/dyj/pkg/discovery"
	"github.com/sq199/dyj/pkg/driver"
	"github.com/sq199/dyj/pkg/driver/cups"
	"github.com/sq199/dyj/pkg/localprint"
)

// serveOptions is the persisted node configuration (service.json). v0.3
// adds the role model and the printer list (research/13 §4.1); the legacy
// single `printer`/`backend` pair migrates into `printers` on load.
type serveOptions struct {
	// Role: "" (bootstrap / not configured yet), share, use, both.
	Role     string        `json:"role,omitempty"`
	Name     string        `json:"name"`
	Port     int           `json:"port"`
	TLSPort  int           `json:"tls_port,omitempty"`
	Printers []printerSpec `json:"printers,omitempty"`

	// Legacy v0.2 single-printer fields; kept so old service.json files
	// keep loading (normalizeServeOptions migrates them).
	Printer string `json:"printer,omitempty"`
	Backend string `json:"backend,omitempty"`

	Out      string `json:"out,omitempty"`
	Spool    string `json:"spool,omitempty"`
	Ifaces   string `json:"ifaces,omitempty"`
	NoMDNS   bool   `json:"no_mdns,omitempty"`
	Verbose  bool   `json:"verbose,omitempty"`
	Hostname string `json:"hostname,omitempty"`
}

// printerSpec is one shared printer.
type printerSpec struct {
	// Key is the URI slug (/ipp/print/<key>); "" on the single legacy
	// printer keeps the classic un-keyed path.
	Key     string `json:"key,omitempty"`
	Name    string `json:"name"`
	Queue   string `json:"queue,omitempty"`
	Backend string `json:"backend,omitempty"`
}

// Roles.
const (
	roleShare = "share"
	roleUse   = "use"
	roleBoth  = "both"
)

func validRole(r string) bool {
	return r == "" || r == roleShare || r == roleUse || r == roleBoth
}

func sharesRole(r string) bool { return r == roleShare || r == roleBoth }

// normalizeServeOptions fills defaults and migrates the legacy single
// printer into the printers list. It is pure (no I/O) so both the flag
// path, the service path and the admin PUT path share one truth.
func normalizeServeOptions(o serveOptions) serveOptions {
	if o.Port < 1 || o.Port > 65535 {
		o.Port = 6631
	}
	if o.TLSPort < 0 || o.TLSPort > 65535 || o.TLSPort == o.Port {
		o.TLSPort = 0
	}
	if o.Name == "" {
		o.Name = "dyj on " + hostnameOr("localhost")
	}
	if !validRole(o.Role) {
		o.Role = ""
	}
	// v0.2 migration: printer+backend → printers[0]. A queue-less file or
	// null backend (e2e, headless demo) also synthesizes one printer named
	// after the service.
	if len(o.Printers) == 0 && (o.Printer != "" || o.Backend == "file" || o.Backend == "null") {
		display := o.Printer
		if display == "" {
			display = o.Name
		}
		o.Printers = []printerSpec{{
			Key:     localprint.Slug(display),
			Name:    display,
			Queue:   o.Printer,
			Backend: o.Backend,
		}}
		o.Printer, o.Backend = "", ""
	}
	seen := map[string]int{}
	for i := range o.Printers {
		p := &o.Printers[i]
		if p.Name == "" {
			p.Name = p.Queue
		}
		if p.Name == "" {
			p.Name = "printer-" + fmt.Sprint(i+1)
		}
		if p.Key == "" {
			p.Key = localprint.Slug(p.Name)
		}
		// De-dup keys: second "epson" becomes "epson-2".
		if n, dup := seen[p.Key]; dup {
			p.Key = fmt.Sprintf("%s-%d", p.Key, n)
			seen[p.Key] = n + 1
		} else {
			seen[p.Key] = 2
		}
	}
	return o
}

func cmdServe(args []string) error {
	o, err := parseServeOptions(args)
	if err != nil {
		return err
	}
	// Explicit serve = share intent (the expert path).
	if o.Role == "" {
		o.Role = roleShare
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runNode(ctx, o, modeConsole)
}

// parseServeOptions is the serve flag set, shared with `service install`.
func parseServeOptions(args []string) (serveOptions, error) {
	var o serveOptions
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.IntVar(&o.Port, "port", 6631, "IPP port (631 needs admin rights)")
	fs.IntVar(&o.TLSPort, "tls-port", 0, "also serve ipps:// on this port")
	fs.StringVar(&o.Name, "name", "", "printer name (default \"dyj on <host>\")")
	fs.StringVar(&o.Printer, "printer", "", "Windows/CUPS queue to print through")
	fs.StringVar(&o.Backend, "backend", "", "wingdi | cups | file | null (default: platform pick)")
	fs.StringVar(&o.Out, "out", "", "output dir for the file backend")
	fs.StringVar(&o.Spool, "spool", "", "state/spool directory")
	fs.StringVar(&o.Ifaces, "iface", "", "interfaces to advertise on")
	fs.BoolVar(&o.NoMDNS, "no-mdns", false, "skip mDNS advertisement")
	fs.BoolVar(&o.Verbose, "v", false, "verbose logging")
	fs.StringVar(&o.Hostname, "hostname", "", "mDNS host name override")
	if err := fs.Parse(args); err != nil {
		return o, err
	}
	return normalizeServeOptions(o), nil
}

// backendName resolves the backend for one printer spec.
func backendNameFor(spec printerSpec, o serveOptions) string {
	if spec.Backend != "" {
		return spec.Backend
	}
	if o.Backend != "" {
		return o.Backend
	}
	if runtime.GOOS == "windows" {
		return "wingdi"
	}
	if qs, err := cups.Queues(); err == nil && len(qs) > 0 {
		return "cups"
	}
	return "file"
}

// buildBackendFor instantiates the backend of one printer. A queue-less
// wingdi/cups selection is a configuration error the wizard must catch —
// but the NODE keeps running without that printer instead of dying.
func buildBackendFor(spec printerSpec, o serveOptions) (driver.Interface, error) {
	name := backendNameFor(spec, o)
	switch name {
	case "wingdi":
		if runtime.GOOS != "windows" {
			return nil, fmt.Errorf("backend wingdi requires Windows")
		}
		if spec.Queue == "" {
			return nil, fmt.Errorf("backend wingdi needs a queue name (share-side wizard or -printer)")
		}
		return &driver.WingDI{Printer: spec.Queue}, nil
	case "cups":
		queue := spec.Queue
		if queue == "" {
			qs, err := cups.Queues()
			if err != nil || len(qs) == 0 {
				return nil, fmt.Errorf("backend cups needs a queue name (lpstat -p shows none)")
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
		return nil, fmt.Errorf("unknown backend %q (wingdi|cups|file|null)", name)
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

// hostNameOr resolves the mDNS host label: explicit override, else the
// default derived from the service name (never the raw os.Hostname, which
// on Windows is the NetBIOS name and does not resolve over mDNS).
func hostNameOr(override, name string) string {
	if override != "" {
		return override
	}
	return discovery.DefaultHostName(name)
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

// loadOrCreateUUID reads/creates a stable UUID at path (one per printer).
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

// marshalOptions renders normalized options for persistence.
func marshalOptions(o serveOptions) (string, error) {
	b, err := json.MarshalIndent(o, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}
