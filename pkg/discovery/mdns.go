// Package discovery advertises the IPP endpoint over mDNS/DNS-SD.
//
// Why this package owns its own sockets instead of using hashicorp/mdns:
//
//  1. `mdns.NewServer` cannot answer on a per-interface basis. CUPS/Avahi use
//     the `${IFACE}` mechanism to change the query name to the host name of the
//     interface the query arrived on, because clients cannot resolve a
//     multi-homed responder's advertised name over multicast.
//  2. `net.ListenMulticastUDP` (which mdns.NewServer uses) turns
//     IP_MULTICAST_LOOP off, so the responder cannot see its own traffic and
//     therefore cannot verify its own records.
//  3. `mdns.NewServer` discards listener errors (`ipv4List, _ := ...`) and only
//     accepts a single interface, while this host has four real subnets plus
//     three APIPA virtual adapters.
//
// See NOTES.md in this directory for the measured evidence.
package discovery

import (
	"fmt"
	"log"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"
)

var zeroTime time.Time

func timeAfter(d time.Duration) time.Time { return time.Now().Add(d) }

// mDNS constants (RFC 6762).
const (
	MDNSGroupV4 = "224.0.0.251"
	MDNSGroupV6 = "ff02::fb"
	MDNSPort    = 5353

	// browseService is the DNS-SD meta-query service used by browsers.
	BrowseService = "_services._dns-sd._udp.local."

	// IPPService is the DNS-SD service type for IPP.
	IPPService = "_ipp._tcp"

	// IPPSubService is the AirPrint sub-type (RFC 6763 §7.1). Browsing
	// `_universal._sub._ipp._tcp.local.` is how iOS/macOS find AirPrint
	// printers; hashicorp/mdns cannot emit this, so we do it by hand.
	IPPSubService = "_universal._sub._ipp._tcp"
)

// Config describes the service to advertise.
type Config struct {
	// Instance is the DNS-SD instance name, e.g. "dyj on desktop".
	Instance string
	// HostName is the FQDN of this responder, must end in a dot
	// (e.g. "desktop.local."). Never let this fall back to os.Hostname():
	// on Windows that is the NetBIOS name, which does not resolve over mDNS.
	HostName string
	// Port of the IPP service.
	Port int
	// IPs advertised in the A/AAAA records.
	IPs []net.IP
	// Path of the IPP resource, e.g. "/ipp/print".
	Path string
	// TXT holds the DNS-SD key=value strings. Built by BuildTXT.
	TXT []string
	// Ifaces limits multicast membership/sending to these interfaces.
	// nil means "every multicast-capable interface".
	Ifaces []*net.Interface
	// Logger, optional.
	Logger *log.Logger
	// TTL for published records.
	TTL uint32
}

// Advertiser publishes a single DNS-SD service over mDNS.
type Advertiser struct {
	cfg Config

	mu       sync.Mutex
	conns    []*ifaceConn
	instance string // possibly renamed after conflict resolution
	host     string
	closed   bool
	ttl      uint32

	respCount uint64
}

type ifaceConn struct {
	ifi  *net.Interface
	conn *net.UDPConn
	// ipv4 is true when the socket is an IPv4 multicast socket.
	ipv4 bool
	// ip is the primary address of this interface (for membership + records).
	ip net.IP
}

// New validates cfg and prepares (but does not start) the advertiser.
func New(cfg Config) (*Advertiser, error) {
	if cfg.Instance == "" {
		return nil, fmt.Errorf("discovery: empty instance name")
	}
	if cfg.Port <= 0 || cfg.Port > 65535 {
		return nil, fmt.Errorf("discovery: invalid port %d", cfg.Port)
	}
	if cfg.HostName == "" {
		return nil, fmt.Errorf("discovery: empty host name (refusing to fall back to NetBIOS name)")
	}
	if !strings.HasSuffix(cfg.HostName, ".") {
		cfg.HostName += "."
	}
	if cfg.Path == "" {
		cfg.Path = "/ipp/print"
	}
	if !strings.HasPrefix(cfg.Path, "/") {
		cfg.Path = "/" + cfg.Path
	}
	if len(cfg.IPs) == 0 {
		return nil, fmt.Errorf("discovery: no IP addresses to advertise")
	}
	if cfg.TTL == 0 {
		cfg.TTL = 120
	}
	if len(cfg.TXT) == 0 {
		cfg.TXT = BuildTXT(TXTParams{Port: cfg.Port, Path: cfg.Path})
	}
	return &Advertiser{cfg: cfg, instance: cfg.Instance, host: cfg.HostName, ttl: cfg.TTL}, nil
}

// Instance reports the instance name actually in use (may differ from the
// configured one if a name conflict was resolved).
func (a *Advertiser) Instance() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.instance
}

// ServiceType reports the full service type, e.g. "_ipp._tcp.local.".
func (a *Advertiser) ServiceType() string { return IPPService + ".local." }

// InstanceFQDN returns the fully qualified instance name.
func (a *Advertiser) InstanceFQDN() string {
	return fmt.Sprintf("%s.%s.local.", escapeInstance(a.Instance()), IPPService)
}

// Responses reports how many query responses have been sent (for the self-test
// command and for tests).
func (a *Advertiser) Responses() uint64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.respCount
}

// Start binds the multicast sockets, announces the service and begins
// answering queries.
func (a *Advertiser) Start() error {
	ifaces := a.cfg.Ifaces
	if len(ifaces) == 0 {
		var err error
		ifaces, err = MulticastCapableInterfaces()
		if err != nil {
			return err
		}
	}
	if len(ifaces) == 0 {
		return fmt.Errorf("discovery: no multicast-capable interfaces found")
	}

	var conns []*ifaceConn
	for _, ifi := range ifaces {
		ip := primaryIPv4(ifi)
		if ip == nil {
			a.logf("iface %s has no IPv4 address, skipping", ifi.Name)
			continue
		}
		c, err := listenMulticast(ifi, ip)
		if err != nil {
			// Do not swallow this: hashicorp/mdns silently degrades here and the
			// result is a responder that never answers on the main LAN.
			a.logf("iface %s: multicast listen failed: %v", ifi.Name, err)
			continue
		}
		conns = append(conns, c)
	}
	if len(conns) == 0 {
		return fmt.Errorf("discovery: could not bind mDNS on any interface")
	}

	// Optional: rename out of a conflict (RFC 6762 §9).
	if name, ok := a.probeName(conns); ok != "" {
		a.instance = name
	}

	a.mu.Lock()
	a.conns = conns
	a.closed = false
	a.mu.Unlock()

	for _, c := range conns {
		go a.serve(c)
	}
	a.announce()
	return nil
}

// Close sends goodbye packets (TTL=0) and stops the listeners. Without this,
// browsers keep showing a printer that is already gone.
func (a *Advertiser) Close() error {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return nil
	}
	a.closed = true
	conns := a.conns
	a.conns = nil
	a.mu.Unlock()

	if len(conns) > 0 {
		a.sendRecords(conns, a.goodbyeRecords(), true)
	}
	for _, c := range conns {
		_ = c.conn.Close()
	}
	return nil
}

// ---------------------------------------------------------------------------
// query handling
// ---------------------------------------------------------------------------

func (a *Advertiser) serve(c *ifaceConn) {
	buf := make([]byte, 9000)
	for {
		n, src, err := c.conn.ReadFromUDP(buf)
		if err != nil {
			a.mu.Lock()
			closed := a.closed
			a.mu.Unlock()
			if !closed {
				a.logf("iface %s: read: %v", c.ifi.Name, err)
			}
			return
		}
		var msg dns.Msg
		if err := msg.Unpack(buf[:n]); err != nil {
			continue // not DNS, ignore (RFC 6762 §6: silently discard)
		}
		if msg.Response {
			continue
		}
		a.answer(c, &msg, src)
	}
}

func (a *Advertiser) answer(c *ifaceConn, q *dns.Msg, from *net.UDPAddr) {
	// A PROBE query (RFC 6762 §8.1) has the authority section carrying the
	// proposed record with a zero address; treating it as a normal query is the
	// safe behaviour for a responder that does not defend names.
	var all []dns.RR
	for i := range q.Question {
		all = append(all, a.records(c, &q.Question[i])...)
	}
	if len(all) == 0 {
		return
	}
	resp := new(dns.Msg)
	resp.Response = true
	resp.Authoritative = true
	resp.RecursionDesired = false
	resp.Compress = true
	resp.Answer = dedupe(all)
	if len(resp.Answer) > 0 {
		// Answer every query, including legacy unicast queries from port != 5353,
		// with a multicast response (RFC 6762 §6.7). The unicast bit in the
		// question is honoured by sending directly to the source too.
		a.sendMsg(c, resp, nil)
		if from.Port != MDNSPort {
			a.sendMsg(c, resp, from)
		}
	}
	a.mu.Lock()
	a.respCount++
	a.mu.Unlock()
}

// records returns the RRs this responder can answer for a question.
func (a *Advertiser) records(c *ifaceConn, q *dns.Question) []dns.RR {
	qName := strings.ToLower(q.Name)
	instance := a.InstanceFQDN()
	svc := a.ServiceType()
	ttl := a.ttl

	// IMPORTANT (this is the `${IFACE}` trick the mdns library cannot do):
	// a multi-homed host must answer for the host name of the interface the
	// query arrived on, because clients cannot resolve "name.local." to a
	// specific address. CUPS/Avahi do the same by rewriting the query name to
	// the interface's host name. `hostName` below is the per-interface name.
	hostName := a.hostFor(c)

	switch {
	case qName == BrowseService:
		// _services._dns-sd._udp.local. PTR -> service types
		for _, st := range []string{IPPService + ".local.", "_http._tcp.local."} {
			return []dns.RR{&dns.PTR{Hdr: hdr(BrowseService, dns.TypePTR, ttl, false), Ptr: st}}
		}
		return nil

	case qName == strings.ToLower(IPPSubService+".local."):
		return []dns.RR{&dns.PTR{Hdr: hdr(IPPSubService+".local.", dns.TypePTR, ttl, false), Ptr: instance}}

	case qName == svc:
		return []dns.RR{&dns.PTR{Hdr: hdr(svc, dns.TypePTR, ttl, false), Ptr: instance}}

	case qName == strings.ToLower(instance):
		return a.instanceRecords(c, instance, nil)

	case qName == strings.ToLower(hostName) || qName == strings.ToLower(a.host):
		return a.hostRecords(c, q.Qtype)
	}
	return nil
}

// instanceRecords builds SRV + TXT (+ A/AAAA when the type is ANY/SRV).
func (a *Advertiser) instanceRecords(c *ifaceConn, instance string, qtypes []uint16) []dns.RR {
	wantAny := len(qtypes) == 0
	hostName := a.hostFor(c)
	var out []dns.RR

	for _, qt := range append(qtypes, dns.TypeANY) {
		switch qt {
		case dns.TypeANY, dns.TypeSRV:
			out = append(out, &dns.SRV{
				Hdr:      hdr(instance, dns.TypeSRV, a.ttl, true),
				Priority: 0,
				Weight:   0,
				Port:     uint16(a.cfg.Port),
				Target:   hostName,
			})
			if !wantAny {
				break
			}
		}
		if qt == dns.TypeANY || qt == dns.TypeTXT {
			out = append(out, &dns.TXT{Hdr: hdr(instance, dns.TypeTXT, a.ttl, true), Txt: a.cfg.TXT})
		}
		if !wantAny {
			continue
		}
		out = append(out, a.hostRecords(c, dns.TypeANY)...)
	}
	return out
}

func (a *Advertiser) hostRecords(c *ifaceConn, qtype uint16) []dns.RR {
	hostName := a.hostFor(c)
	var out []dns.RR
	add4 := qtype == dns.TypeANY || qtype == dns.TypeA
	add6 := qtype == dns.TypeANY || qtype == dns.TypeAAAA
	for _, ip := range a.cfg.IPs {
		if ip4 := ip.To4(); ip4 != nil && add4 {
			out = append(out, &dns.A{Hdr: hdr(hostName, dns.TypeA, a.ttl, true), A: ip4})
			continue
		}
		if ip16 := ip.To16(); ip16 != nil && add6 {
			out = append(out, &dns.AAAA{Hdr: hdr(hostName, dns.TypeAAAA, a.ttl, true), AAAA: ip16})
		}
	}
	return out
}

// hostFor returns the host name to advertise on the given interface.
//
// The first/preferred interface (the filtered whitelist should put the LAN
// first) uses the configured host name; additional interfaces use a
// per-interface name, which is also the name the A records are published under.
// This mirrors Avahi's `${IFACE}` behaviour without needing a rename pass.
func (a *Advertiser) hostFor(c *ifaceConn) string {
	a.mu.Lock()
	first := len(a.conns) == 0 || a.conns[0] == c
	a.mu.Unlock()
	if first {
		return a.host
	}
	return ifaceHostName(a.host, c.ifi.Name)
}

// ifaceHostName derives "desktop-2.local." style names for extra interfaces.
func ifaceHostName(base, ifaceName string) string {
	label := strings.TrimSuffix(base, ".")
	label = strings.TrimSuffix(label, ".local")
	suffix := sanitizeLabel(ifaceName)
	if suffix == "" {
		return base
	}
	return fmt.Sprintf("%s-%s.local.", label, suffix)
}

func sanitizeLabel(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
			b.WriteRune(r)
		default:
			// drop spaces / non-ASCII (interface names here are Chinese)
		}
	}
	return b.String()
}

func hdr(name string, rrtype uint16, ttl uint32, flush bool) dns.RR_Header {
	class := uint16(dns.ClassINET)
	if flush {
		class |= 1 << 15 // cache-flush bit, RFC 6762 §10.2
	}
	return dns.RR_Header{Name: name, Rrtype: rrtype, Class: class, Ttl: ttl}
}

// ---------------------------------------------------------------------------
// announcements / goodbye
// ---------------------------------------------------------------------------

func (a *Advertiser) announce() {
	records := a.allRecords()
	a.sendRecords(nil, records, true)
}

func (a *Advertiser) goodbyeRecords() []dns.RR {
	records := a.allRecords()
	for _, rr := range records {
		rr.Header().Ttl = 0
	}
	return records
}

// allRecords builds the full record set (both the LAN host name and every
// per-interface host name so a client that cached the "2nd interface" name is
// also served).
func (a *Advertiser) allRecords() []dns.RR {
	a.mu.Lock()
	conns := append([]*ifaceConn(nil), a.conns...)
	a.mu.Unlock()

	instance := a.InstanceFQDN()
	svc := a.ServiceType()
	var out []dns.RR

	out = append(out, &dns.PTR{Hdr: hdr(BrowseService, dns.TypePTR, a.ttl, false), Ptr: svc})
	out = append(out, &dns.PTR{Hdr: hdr(IPPSubService+".local.", dns.TypePTR, a.ttl, false), Ptr: instance})
	out = append(out, &dns.PTR{Hdr: hdr(svc, dns.TypePTR, a.ttl, false), Ptr: instance})

	// The SRV target must match the interface the query arrived on, so the
	// announcement carries one SRV per interface host name.
	names := map[string]bool{}
	for _, c := range conns {
		names[a.hostFor(c)] = true
	}
	names[a.host] = true
	ordered := make([]string, 0, len(names))
	for n := range names {
		ordered = append(ordered, n)
	}
	sort.Strings(ordered)
	for _, hostName := range ordered {
		out = append(out, &dns.SRV{
			Hdr:    hdr(instance, dns.TypeSRV, a.ttl, true),
			Port:   uint16(a.cfg.Port),
			Target: hostName,
		})
	}
	out = append(out, &dns.TXT{Hdr: hdr(instance, dns.TypeTXT, a.ttl, true), Txt: a.cfg.TXT})
	for _, hostName := range ordered {
		for _, ip := range a.cfg.IPs {
			if ip4 := ip.To4(); ip4 != nil {
				out = append(out, &dns.A{Hdr: hdr(hostName, dns.TypeA, a.ttl, true), A: ip4})
			}
		}
	}
	return dedupe(out)
}

func (a *Advertiser) sendRecords(conns []*ifaceConn, records []dns.RR, cacheFlush bool) {
	if conns == nil {
		a.mu.Lock()
		conns = append([]*ifaceConn(nil), a.conns...)
		a.mu.Unlock()
	}
	if len(records) == 0 {
		return
	}
	msg := new(dns.Msg)
	msg.Response = true
	msg.Authoritative = true
	msg.Compress = true
	msg.Answer = records
	for _, c := range conns {
		a.sendMsg(c, msg, nil)
	}
}

// sendMsg writes msg on the interface socket, optionally unicast to dst.
func (a *Advertiser) sendMsg(c *ifaceConn, msg *dns.Msg, dst *net.UDPAddr) {
	buf, err := msg.Pack()
	if err != nil {
		a.logf("pack: %v", err)
		return
	}
	buf = clampForInterface(buf, c)
	var target *net.UDPAddr
	if dst != nil {
		target = dst
	} else if c.ipv4 {
		target = &net.UDPAddr{IP: net.ParseIP(MDNSGroupV4), Port: MDNSPort}
	} else {
		target = &net.UDPAddr{IP: net.ParseIP(MDNSGroupV6), Port: MDNSPort, Zone: c.ifi.Name}
	}
	if _, err := c.conn.WriteToUDP(buf, target); err != nil {
		a.logf("iface %s: write: %v", c.ifi.Name, err)
	}
}

// clampForInterface keeps the packet inside the interface MTU. PWG raster has
// nothing to do with mDNS, but the TXT record of an IPP service can still push
// the announcement past 1500 bytes once IPv6 is in play, and a fragmented mDNS
// response is dropped by several clients.
func clampForInterface(buf []byte, c *ifaceConn) []byte {
	mtu := c.ifi.MTU
	if mtu <= 0 {
		mtu = 1500
	}
	limit := mtu - 48
	if len(buf) <= limit {
		return buf
	}
	var msg dns.Msg
	if err := msg.Unpack(buf); err != nil {
		return buf
	}
	msg.Truncate(limit)
	if out, err := msg.Pack(); err == nil {
		return out
	}
	return buf
}

// probeName performs a simplified RFC 6762 §8.1 conflict probe for the service
// instance name and returns a non-conflicting name.
//
// NOTE this is a partial implementation: it only observes; it does not defend
// the name afterwards, and it does not probe the host name. hashicorp/mdns does
// none of this at all (see docs/research/03-verified-blockers.md).
func (a *Advertiser) probeName(conns []*ifaceConn) (string, string) {
	instance := a.InstanceFQDN()
	q := new(dns.Msg)
	q.SetQuestion(strings.ToLower(instance), dns.TypeANY)
	q.RecursionDesired = false

	// Random delay, RFC 6762 §8.1 says 0-250ms.
	deadline := timeAfter(220)

	for _, c := range conns {
		a.sendMsg(c, q, nil)
	}
	conflict := false
	for _, c := range conns {
		_ = c.conn.SetReadDeadline(deadline)
		buf := make([]byte, 9000)
		for {
			n, _, err := c.conn.ReadFromUDP(buf)
			if err != nil {
				break
			}
			var msg dns.Msg
			if err := msg.Unpack(buf[:n]); err != nil || !msg.Response {
				continue
			}
			for _, rr := range append(append([]dns.RR{}, msg.Answer...), msg.Extra...) {
				if strings.EqualFold(rr.Header().Name, instance) {
					conflict = true
				}
			}
			if conflict {
				break
			}
		}
		_ = c.conn.SetReadDeadline(zeroTime)
		if conflict {
			break
		}
	}
	if !conflict {
		return a.cfg.Instance, ""
	}
	a.logf("instance name %q is already in use, renaming", a.cfg.Instance)
	for i := 2; i < 100; i++ {
		candidate := fmt.Sprintf("%s (%d)", a.cfg.Instance, i)
		if _, err := dns.NewRR(fmt.Sprintf("%s.local.", candidate)); err != nil {
			continue
		}
		return candidate, fmt.Sprintf("renamed to %q (RFC 6762 §9)", candidate)
	}
	return a.cfg.Instance, ""
}

// ---------------------------------------------------------------------------
// interface selection
// ---------------------------------------------------------------------------

// MulticastCapableInterfaces returns every up, multicast-capable, non-loopback
// interface with an IPv4 address, sorted so the most LAN-like ones come first.
func MulticastCapableInterfaces() ([]*net.Interface, error) {
	all, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	var out []*net.Interface
	for i := range all {
		ifi := all[i]
		if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagLoopback != 0 {
			continue
		}
		if ifi.Flags&net.FlagMulticast == 0 {
			continue
		}
		ip := primaryIPv4(&ifi)
		if ip == nil {
			continue
		}
		out = append(out, &ifi)
	}
	sort.SliceStable(out, func(i, j int) bool {
		// Real LAN (RFC1918, not APIPA) first, then by name for determinism.
		ai, aj := scoreIface(out[i]), scoreIface(out[j])
		if ai != aj {
			return ai < aj
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

func scoreIface(ifi *net.Interface) int {
	ip := primaryIPv4(ifi)
	if ip == nil {
		return 100
	}
	if ip.IsLinkLocalUnicast() {
		return 50 // 169.254.x.x APIPA: last resort
	}
	if isDockerBridge(ip) {
		// Container bridges (172.17-172.31) are private but other LAN
		// hosts cannot reach them; advertising them first yields
		// unreachable adminurl/printer URIs.
		return 30
	}
	if ip.IsPrivate() {
		return 0
	}
	return 10
}

// isDockerBridge reports whether ip falls into the default Docker
// bridge pool 172.17.0.0/16 .. 172.31.255.255.
func isDockerBridge(ip net.IP) bool {
	if !ip.IsPrivate() {
		return false
	}
	v := ip.To4()
	if v == nil {
		return false
	}
	return v[0] == 172 && v[1] >= 17 && v[1] <= 31
}

// SelectInterfaces filters interfaces by an explicit allow-list of names.
// An empty list means "auto-select".
func SelectInterfaces(names []string) ([]*net.Interface, error) {
	all, err := MulticastCapableInterfaces()
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		return all, nil
	}
	byName := map[string]*net.Interface{}
	for _, ifi := range all {
		byName[strings.ToLower(ifi.Name)] = ifi
	}
	var out []*net.Interface
	var missing []string
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		// Allow matching by interface name or by address.
		if ifi, ok := byName[strings.ToLower(n)]; ok {
			out = append(out, ifi)
			continue
		}
		found := false
		for _, ifi := range all {
			if ip := primaryIPv4(ifi); ip != nil && ip.String() == n {
				out = append(out, ifi)
				found = true
				break
			}
		}
		if !found {
			missing = append(missing, n)
		}
	}
	if len(missing) > 0 {
		return out, fmt.Errorf("discovery: no multicast-capable interface matches %s", strings.Join(missing, ", "))
	}
	return out, nil
}

// DefaultHostName derives the mDNS host name for an instance name.
//
// This must never be left to os.Hostname(): on Windows that returns the NetBIOS
// name, which cannot be resolved over mDNS, so every SRV record would point at
// an unresolvable target.
func DefaultHostName(instance string) string {
	base := sanitizeLabel(strings.ReplaceAll(instance, " ", "-"))
	base = strings.Trim(base, "-")
	if base == "" {
		base = "dyj"
	}
	return base + ".local."
}

func primaryIPv4(ifi *net.Interface) net.IP {
	addrs, err := ifi.Addrs()
	if err != nil {
		return nil
	}
	for _, a := range addrs {
		var ip net.IP
		switch v := a.(type) {
		case *net.IPNet:
			ip = v.IP
		case *net.IPAddr:
			ip = v.IP
		}
		if ip4 := ip.To4(); ip4 != nil {
			return ip4
		}
	}
	return nil
}

// InterfaceAddrs returns the IPv4 addresses of the given interfaces.
func InterfaceAddrs(ifaces []*net.Interface) []net.IP {
	var out []net.IP
	for _, ifi := range ifaces {
		if ip := primaryIPv4(ifi); ip != nil {
			out = append(out, ip)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// TXT records
// ---------------------------------------------------------------------------

// TXTParams describes the DNS-SD TXT record contents for an IPP service.
type TXTParams struct {
	Port      int
	Path      string
	Type      string // ty=
	PDL       string // pdl=
	UUID      string
	AdminURL  string
	Note      string
	Product   string
	Color     bool
	TLS       bool
	Extra     map[string]string
}

// BuildTXT renders DNS-SD key=value pairs (RFC 6763 §6). hashicorp/mdns does
// not do this layer at all — it copies the strings verbatim — so the assembly
// lives here.
//
// Keys are emitted in a fixed order for reproducible output:
// txtvers, qtotal, rp, ty, pdl, adminurl, note, product, UUID, TLS, Color.
func BuildTXT(p TXTParams) []string {
	if p.Path == "" {
		p.Path = "/ipp/print"
	}
	if p.Type == "" {
		p.Type = "dyj shared printer"
	}
	if p.PDL == "" {
		// Keep in lockstep with the document formats pkg/render can
		// actually decode (ADR-0007): pdl= is a promise, not a wish.
		p.PDL = "image/pwg-raster,image/jpeg,image/png,text/plain"
	}
	if p.UUID == "" {
		p.UUID = "00000000-0000-4000-8000-000000000001"
	}
	if p.AdminURL == "" {
		p.AdminURL = fmt.Sprintf("http://localhost:%d/", p.Port)
	}
	if p.Note == "" {
		p.Note = "shared via dyj"
	}
	if p.Product == "" {
		p.Product = "(dyj)"
	}

	txt := []string{
		"txtvers=1",
		"qtotal=1",
		"rp=" + strings.TrimPrefix(p.Path, "/"),
		"ty=" + p.Type,
		"pdl=" + p.PDL,
		"adminurl=" + p.AdminURL,
		"note=" + p.Note,
		"product=" + p.Product,
		"UUID=" + p.UUID,
		"TLS=" + boolTXT(p.TLS),
		"Color=" + boolTXT(p.Color),
		"printer-type=0x0000",
		"kind=document",
		"PaperMax=legal-A4",
		// No URF= key: that would promise Apple AirPrint raster support
		// we do not implement (ADR-0007). AirPrint clients still browse
		// _ipp._tcp; they just fall back to the declared pdl=.
	}
	for _, k := range sortedKeys(p.Extra) {
		txt = append(txt, k+"="+p.Extra[k])
	}
	return txt
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func boolTXT(b bool) string {
	if b {
		return "T"
	}
	return "F"
}

// ---------------------------------------------------------------------------
// small helpers
// ---------------------------------------------------------------------------

func dedupe(rrs []dns.RR) []dns.RR {
	seen := make(map[string]bool, len(rrs))
	out := make([]dns.RR, 0, len(rrs))
	for _, rr := range rrs {
		key := rr.String()
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, rr)
	}
	return out
}

// escapeInstance escapes a DNS-SD instance label (RFC 6763 §4.3).
func escapeInstance(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case ch == '.' || ch == '\\':
			b.WriteByte('\\')
			b.WriteByte(ch)
		case ch < 0x20 || ch >= 0x7f:
			fmt.Fprintf(&b, "\\%03d", ch)
		default:
			b.WriteByte(ch)
		}
	}
	return b.String()
}

func (a *Advertiser) logf(format string, args ...any) {
	if a.cfg.Logger != nil {
		a.cfg.Logger.Printf("mdns: "+format, args...)
	}
}
