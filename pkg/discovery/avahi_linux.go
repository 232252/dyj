//go:build linux

package discovery

import (
	"fmt"
	"strings"
	"sync"

	"github.com/godbus/dbus/v5"
)

// avahi D-Bus constants (org.freedesktop.Avahi, verified against avahi 0.8
// via introspection — research/16 §3).
const (
	avahiDest      = "org.freedesktop.Avahi"
	avahiServerIf  = "org.freedesktop.Avahi.Server"
	avahiGroupIf   = "org.freedesktop.Avahi.EntryGroup"
	avahiIfaceUnsp = -1 // AVAHI_IF_UNSPEC: all interfaces
	avahiProtoUnsp = -1 // AVAHI_PROTO_UNSPEC: IPv4 + IPv6
	avahiServerRun = 2  // AVAHI_SERVER_RUNNING
)

// NewPublisher prefers the system avahi daemon when it is running: avahi
// owns UDP 5353 and Linux SO_REUSEPORT multicast delivers queries to only
// one socket, so dyj's own responder would be starved (research/16 §3).
// Publishing through avahi's D-Bus API is the standard coexistence path.
// When avahi is absent or the D-Bus setup fails, it falls back to dyj's
// own advertiser so headless servers keep working. The bool reports
// whether avahi is in use.
func NewPublisher(cfg Config) (Publisher, bool, error) {
	if p, err := newAvahiPublisher(cfg); err == nil {
		return p, true, nil
	} else if cfg.Logger != nil {
		cfg.Logger.Printf("discovery: avahi unavailable, falling back to own mDNS: %v", err)
	}
	a, err := New(cfg)
	if err != nil {
		return nil, false, err
	}
	return a, false, nil
}

// avahiPublisher publishes one service through an avahi EntryGroup.
type avahiPublisher struct {
	mu       sync.Mutex
	conn     *dbus.Conn
	group    dbus.BusObject
	name     string // instance name as committed (avahi may rename on conflict)
	svcType  string // "_ipp._tcp"
	domain   string // "local."
	host     string // FQDN with trailing dot
	port     uint16
	txt      []string
	subtype  string // AirPrint sub-service, "" to skip
	started  bool
	closed   bool
}

// newAvahiPublisher connects to the system bus, checks avahi is running,
// creates an EntryGroup and registers the service (not yet committed).
func newAvahiPublisher(cfg Config) (*avahiPublisher, error) {
	if cfg.Instance == "" {
		return nil, fmt.Errorf("discovery: empty instance name")
	}
	if cfg.Port <= 0 || cfg.Port > 65535 {
		return nil, fmt.Errorf("discovery: invalid port %d", cfg.Port)
	}
	conn, err := dbus.SystemBus()
	if err != nil {
		return nil, fmt.Errorf("discovery: system bus: %w", err)
	}
	server := conn.Object(avahiDest, "/")
	var state int32
	if err := server.Call(avahiServerIf+".GetState", 0).Store(&state); err != nil {
		return nil, fmt.Errorf("discovery: avahi GetState: %w", err)
	}
	if state != avahiServerRun {
		return nil, fmt.Errorf("discovery: avahi not running (state %d)", state)
	}
	var path dbus.ObjectPath
	if err := server.Call(avahiServerIf+".EntryGroupNew", 0).Store(&path); err != nil {
		return nil, fmt.Errorf("discovery: avahi EntryGroupNew: %w", err)
	}
	group := conn.Object(avahiDest, path)

	host := cfg.HostName
	if host == "" {
		host = DefaultHostName(cfg.Instance)
	}
	if !strings.HasSuffix(host, ".") {
		host += "."
	}
	txt := cfg.TXT
	if len(txt) == 0 {
		txt = BuildTXT(TXTParams{Port: cfg.Port, Path: cfg.Path})
	}
	p := &avahiPublisher{
		conn:    conn,
		group:   group,
		name:    cfg.Instance,
		svcType: IPPService,
		domain:  "local.",
		host:    host,
		port:    uint16(cfg.Port),
		txt:     append([]string(nil), txt...),
		subtype: IPPSubService,
	}
	if err := p.addService(); err != nil {
		_ = group.Call(avahiGroupIf+".Free", 0).Err
		return nil, err
	}
	return p, nil
}

// addService registers the service (+ AirPrint subtype) in the group.
func (p *avahiPublisher) addService() error {
	txtBytes := txtToBytes(p.txt)
	if err := p.group.Call(avahiGroupIf+".AddService", 0,
		int32(avahiIfaceUnsp), int32(avahiProtoUnsp), uint32(0),
		p.name, p.svcType, p.domain, p.host, p.port, txtBytes).Err; err != nil {
		return fmt.Errorf("discovery: avahi AddService: %w", err)
	}
	if p.subtype != "" {
		// Subtype registration is best-effort: some avahi builds reject
		// it, and the primary _ipp._tcp service is what dyj needs.
		_ = p.group.Call(avahiGroupIf+".AddServiceSubtype", 0,
			int32(avahiIfaceUnsp), int32(avahiProtoUnsp), uint32(0),
			p.name, p.svcType, p.domain, p.subtype).Err
	}
	return nil
}

// Start commits the entry group so avahi announces the service.
func (p *avahiPublisher) Start() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return fmt.Errorf("discovery: publisher closed")
	}
	if p.started {
		return nil
	}
	if err := p.group.Call(avahiGroupIf+".Commit", 0).Err; err != nil {
		return fmt.Errorf("discovery: avahi Commit: %w", err)
	}
	p.started = true
	return nil
}

// Close frees the entry group (avahi sends the goodbye) and detaches.
func (p *avahiPublisher) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil
	}
	p.closed = true
	if p.group != nil {
		_ = p.group.Call(avahiGroupIf+".Free", 0).Err
	}
	return nil
}

// Instance reports the committed instance name.
func (p *avahiPublisher) Instance() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.name
}

// TXT returns a copy of the current TXT content.
func (p *avahiPublisher) TXT() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.txt...)
}

// SetTXT updates the TXT record in place via avahi UpdateServiceTxt, so
// the open-printing pair= mark flips without rebuilding the service.
func (p *avahiPublisher) SetTXT(txt []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || !p.started || sameTXT(p.txt, txt) {
		return
	}
	p.txt = append([]string(nil), txt...)
	_ = p.group.Call(avahiGroupIf+".UpdateServiceTxt", 0,
		int32(avahiIfaceUnsp), int32(avahiProtoUnsp), uint32(0),
		p.name, p.svcType, p.domain, txtToBytes(p.txt)).Err
}

// txtToBytes converts DNS-SD "key=value" strings to avahi's aay payload.
func txtToBytes(txt []string) [][]byte {
	out := make([][]byte, 0, len(txt))
	for _, s := range txt {
		out = append(out, []byte(s))
	}
	return out
}