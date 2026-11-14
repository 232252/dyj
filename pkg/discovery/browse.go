package discovery

import (
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/232252/dyj/internal/cmp"
	"github.com/232252/dyj/internal/slices"

	"github.com/hashicorp/mdns"
	"github.com/miekg/dns"
)

// Found describes one discovered IPP service.
type Found struct {
	Instance   string
	Host       string
	Port       int
	Addrs      []string
	TXT        []string
	TXTMap     map[string]string
	Source     string // "lib" = hashicorp/mdns reference client, "raw" = own resolver
	ResolvedOK bool
	ResolveErr string
	Records    []string
}

// zoneState accumulates records seen while browsing.
type zoneState struct {
	ptr map[string]bool     // instance FQDN (lowercase, trailing dot)
	srv map[string]*dns.SRV // by instance FQDN
	txt map[string]*dns.TXT // by instance FQDN
	a   map[string][]string // host FQDN -> addresses
	raw []string            // textual RRs for evidence
}

func newZoneState() *zoneState {
	return &zoneState{
		ptr: map[string]bool{},
		srv: map[string]*dns.SRV{},
		txt: map[string]*dns.TXT{},
		a:   map[string][]string{},
	}
}

func (s *zoneState) addMsg(msg *dns.Msg) {
	rrSets := [][]dns.RR{msg.Answer, msg.Extra, msg.Ns}
	for _, set := range rrSets {
		for _, rr := range set {
			name := strings.ToLower(rr.Header().Name)
			switch v := rr.(type) {
			case *dns.PTR:
				if strings.Contains(strings.ToLower(v.Ptr), "_ipp._tcp") {
					s.ptr[strings.ToLower(v.Ptr)] = true
					s.raw = append(s.raw, "PTR "+rr.Header().Name+" -> "+v.Ptr)
				}
			case *dns.SRV:
				cp := *v
				s.srv[name] = &cp
				s.raw = append(s.raw, "SRV "+cp.Hdr.Name+" -> "+cp.Target+fmt.Sprintf(":%d", cp.Port))
			case *dns.TXT:
				cp := *v
				s.txt[name] = &cp
			case *dns.A:
				key := strings.ToLower(v.Hdr.Name)
				s.a[key] = appendUnique(s.a[key], v.A.String())
			case *dns.AAAA:
				key := strings.ToLower(v.Hdr.Name)
				s.a[key] = appendUnique(s.a[key], v.AAAA.String())
			}
		}
	}
}

func (s *zoneState) found() []Found {
	names := make([]string, 0, len(s.ptr))
	for n := range s.ptr {
		names = append(names, n)
	}
	slices.Sort(names)
	out := make([]Found, 0, len(names))
	for _, inst := range names {
		f := Found{Instance: strings.TrimSuffix(inst, "."), Source: "raw"}
		if srv, ok := s.srv[inst]; ok {
			f.Host = srv.Target
			f.Port = int(srv.Port)
			f.Records = append(f.Records, srv.String())
		}
		if txt, ok := s.txt[inst]; ok {
			f.TXT = txt.Txt
			f.TXTMap = txtToMap(txt.Txt)
			f.Records = append(f.Records, txt.String())
		}
		if f.Host != "" {
			key := strings.ToLower(f.Host)
			for _, a := range s.a[key] {
				f.Addrs = append(f.Addrs, a)
				f.Records = append(f.Records, fmt.Sprintf("%s IN A %s", f.Host, a))
			}
		}
		out = append(out, f)
	}
	return out
}

// Browse looks for _ipp._tcp services on the given interfaces.
//
// Two independent paths are used on purpose:
//
//  1. a raw DNS-SD query assembled from the answer packets, so the result does
//     not depend on anything being resolvable, and
//  2. hashicorp/mdns's own Query, i.e. a third-party reference client. That is
//     the honest test of "can a real mDNS client see this?".
//
// When both paths see the service, the advertisement is genuinely browsable.
func Browse(ifaces []*net.Interface, timeout time.Duration) ([]Found, error) {
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	if len(ifaces) == 0 {
		var err error
		ifaces, err = BrowseInterfaces()
		if err != nil {
			return nil, err
		}
	}
	// Each path gets the full budget: the raw path spends it on query
	// rounds + collection, the reference client on its own window. The
	// old timeout/2 split left ~1.5s for everything and dropped peers
	// that answered a hair late.
	per := timeout
	if per < 700*time.Millisecond {
		per = 700 * time.Millisecond
	}

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		raw     []Found
		rawErr  error
		refSeen []Found
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		f, err := browseRaw(ifaces, per)
		mu.Lock()
		raw, rawErr = f, err
		mu.Unlock()
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		f := browseReference(ifaces, per)
		mu.Lock()
		refSeen = f
		mu.Unlock()
	}()
	wg.Wait()

	merged := mergeFound(append(raw, refSeen...))
	// Verify in parallel: each resolve can burn the full window on a
	// silent host, and a serial loop multiplied that by the printer count.
	var vwg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for i := range merged {
		vwg.Add(1)
		go func(f *Found) {
			defer vwg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			f.verify(ifaces, per)
		}(&merged[i])
	}
	vwg.Wait()
	if len(merged) == 0 {
		return nil, rawErr
	}
	return merged, nil
}

// verify resolves the SRV target. This is exactly where a naive mDNS
// advertisement breaks: the SRV exposes a host name, and the client cannot
// resolve it.
func (f *Found) verify(ifaces []*net.Interface, timeout time.Duration) {
	if f.Host == "" {
		return
	}
	host := strings.TrimSuffix(f.Host, ".")
	if ip := net.ParseIP(host); ip != nil {
		f.ResolvedOK = true
		f.Addrs = appendUnique(f.Addrs, ip.String())
		return
	}
	// Direct mDNS resolution: Windows' stub resolver does not answer mDNS
	// queries for arbitrary names, so net.Resolver is not authoritative here.
	if ip, err := ResolveName(ifaces, host, timeout); err == nil && ip != "" {
		f.ResolvedOK = true
		f.Addrs = appendUnique(f.Addrs, ip)
		f.Records = append(f.Records, fmt.Sprintf("resolved %s -> %s via mDNS", host, ip))
		return
	} else if err != nil {
		f.ResolveErr = err.Error()
	}
	// As a second opinion, ask the OS resolver too.
	addrs, err := net.LookupHost(host)
	if err == nil && len(addrs) > 0 {
		f.ResolvedOK = true
		f.ResolveErr = ""
		for _, a := range addrs {
			f.Addrs = appendUnique(f.Addrs, a)
		}
		f.Records = append(f.Records, fmt.Sprintf("resolved %s -> %v via OS resolver", host, addrs))
	}
}

func mergeFound(in []Found) []Found {
	byKey := map[string]*Found{}
	var order []string
	for _, f := range in {
		// An entry without SRV has no port and no target: RFC 6763
		// requires SRV+TXT for a browsable service, so PTR targets that
		// never resolved (e.g. a service-type name seen in a
		// _services._dns-sd meta-query answer) are dropped.
		if f.Port == 0 || f.Host == "" {
			continue
		}
		key := mergeKey(f)
		if cur, ok := byKey[key]; ok {
			cur.Addrs = appendUnique(cur.Addrs, f.Addrs...)
			cur.Records = appendUnique(cur.Records, f.Records...)
			if len(f.TXT) > len(cur.TXT) {
				cur.TXT, cur.TXTMap = f.TXT, f.TXTMap
			}
			if cur.Source == "raw" {
				cur.Source = f.Source
			}
			continue
		}
		cp := f
		byKey[key] = &cp
		order = append(order, key)
	}
	out := make([]Found, 0, len(order))
	for _, k := range order {
		f := byKey[k]
		slices.Sort(f.Addrs)
		out = append(out, *f)
	}
	slices.SortStableFunc(out, func(a, b Found) int { return cmp.Compare(a.Instance, b.Instance) })
	return out
}

// mergeKey is the dedup identity of one discovered service (ADR-0015):
// the TXT UUID when present (stable across interfaces and renames; two
// machines sharing same-named printers carry different UUIDs), else the
// instance label + host-base + port. The host base strips the "-<iface>"
// suffix the ${IFACE} naming adds per interface, so one physical printer
// merges into one entry while same-named printers on different hosts stay
// distinct.
func mergeKey(f Found) string {
	label := strings.ToLower(strings.TrimSuffix(f.Instance, "."))
	if uuid := strings.ToLower(f.TXTMap["UUID"]); uuid != "" {
		return "uuid:" + uuid + "|" + fmt.Sprint(f.Port)
	}
	return label + "|" + hostBase(f.Host) + "|" + fmt.Sprint(f.Port)
}

// hostBase normalizes an mDNS host name for comparison: trailing dot and
// ".local" removed, per-interface "-<iface>" suffix stripped. IP literals
// pass through unchanged.
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

func appendUnique(dst []string, vals ...string) []string {
	for _, v := range vals {
		if v != "" && !slices.Contains(dst, v) {
			dst = append(dst, v)
		}
	}
	return dst
}

// browseRaw performs the DNS-SD browsing queries itself.
func browseRaw(ifaces []*net.Interface, timeout time.Duration) ([]Found, error) {
	conns, err := dialQuerySockets(ifaces)
	if err != nil {
		return nil, err
	}
	defer func() {
		for _, c := range conns {
			_ = c.conn.Close()
		}
	}()

	send := func(name string, qtype uint16) {
		m := new(dns.Msg)
		m.SetQuestion(name, qtype)
		m.RecursionDesired = false
		// Top bit of qclass = "unicast response preferred" (RFC 6762 §5.4).
		m.Question[0].Qclass |= 1 << 15
		buf, err := m.Pack()
		if err != nil {
			return
		}
		for _, c := range conns {
			_, _ = c.conn.WriteToUDP(buf, multicastAddr(c))
		}
	}

	queryAll := func() {
		send(IPPSubService+".local.", dns.TypePTR)
		send(IPPService+".local.", dns.TypePTR)
		send(IPPService+".local.", dns.TypeANY)
		send(BrowseService, dns.TypePTR)
	}

	st := newZoneState()
	var mu sync.Mutex
	deadline := time.Now().Add(timeout)
	var wg sync.WaitGroup
	for _, c := range conns {
		wg.Add(1)
		go func(c *ifaceConn) {
			defer wg.Done()
			buf := make([]byte, 9000)
			for {
				remain := time.Until(deadline)
				if remain <= 0 {
					return
				}
				_ = c.conn.SetReadDeadline(deadline)
				n, _, err := c.conn.ReadFromUDP(buf)
				if err != nil {
					return
				}
				var msg dns.Msg
				if err := msg.Unpack(buf[:n]); err != nil || !msg.Response {
					continue
				}
				mu.Lock()
				st.addMsg(&msg)
				mu.Unlock()
			}
		}(c)
	}

	// Query rounds with backoff (RFC 6762 §5.2 spirit): one multicast
	// packet lost used to mean the printer was invisible for the whole
	// scan. 0 / 400ms / 1.2s covers both the cold-cache and the
	// late-responder cases without flooding the link.
	queryAll()
	for _, delay := range []time.Duration{400 * time.Millisecond, 800 * time.Millisecond} {
		if !sleepUntil(deadline, delay) {
			break
		}
		queryAll()
	}
	wg.Wait()

	// Instances whose SRV or TXT never arrived: ask point-blank for the
	// instance records. The old trigger (len(srv)==0) missed the common
	// partial-answer case where SOME instances resolved and one did not.
	mu.Lock()
	var incomplete []string
	for inst := range st.ptr {
		if st.srv[inst] == nil || st.txt[inst] == nil {
			incomplete = append(incomplete, inst)
		}
	}
	mu.Unlock()
	if len(incomplete) > 0 {
		for _, inst := range incomplete {
			send(inst, dns.TypeANY)
		}
		sub := time.Now().Add(timeout / 2)
		var swg sync.WaitGroup
		for _, c := range conns {
			swg.Add(1)
			go func(c *ifaceConn) {
				defer swg.Done()
				buf := make([]byte, 9000)
				for time.Now().Before(sub) {
					_ = c.conn.SetReadDeadline(sub)
					n, _, err := c.conn.ReadFromUDP(buf)
					if err != nil {
						return
					}
					var msg dns.Msg
					if err := msg.Unpack(buf[:n]); err != nil || !msg.Response {
						continue
					}
					mu.Lock()
					st.addMsg(&msg)
					mu.Unlock()
				}
			}(c)
		}
		swg.Wait()
	}
	return st.found(), nil
}

// sleepUntil waits d, reporting false when the deadline would be crossed.
func sleepUntil(deadline time.Time, d time.Duration) bool {
	if time.Now().Add(d).After(deadline) {
		return false
	}
	time.Sleep(d)
	return true
}

func txtToMap(txt []string) map[string]string {
	m := map[string]string{}
	for _, kv := range txt {
		if i := strings.IndexByte(kv, '='); i > 0 {
			m[kv[:i]] = kv[i+1:]
		} else {
			m[kv] = ""
		}
	}
	return m
}

// browseReference runs hashicorp/mdns's own client — the third-party check.
// Interfaces are queried in parallel: each mdns.Query blocks for the full
// timeout, so a serial loop doubled the scan latency per extra NIC.
func browseReference(ifaces []*net.Interface, timeout time.Duration) []Found {
	var (
		mu  sync.Mutex
		wg  sync.WaitGroup
		out []Found
	)
	for _, ifi := range ifaces {
		wg.Add(1)
		go func(ifi *net.Interface) {
			defer wg.Done()
			entries := make(chan *mdns.ServiceEntry, 16)
			params := mdns.DefaultParams("_ipp._tcp")
			params.Entries = entries
			params.Timeout = timeout
			params.DisableIPv6 = true
			params.Interface = ifi
			params.Logger = log.New(discardWriter{}, "", 0)
			done := make(chan struct{})
			go func() {
				defer close(done)
				for e := range entries {
					var addrs []string
					if e.AddrV4 != nil {
						addrs = append(addrs, e.AddrV4.String())
					}
					mu.Lock()
					out = append(out, Found{
						Instance: e.Name,
						Host:     e.Host,
						Port:     e.Port,
						Addrs:    addrs,
						TXT:      e.InfoFields,
						TXTMap:   txtToMap(e.InfoFields),
						Source:   "lib",
					})
					mu.Unlock()
				}
			}()
			_ = mdns.Query(params)
			close(entries)
			<-done
		}(ifi)
	}
	wg.Wait()
	return out
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

// ResolveName resolves a name over mDNS by querying directly. On Windows the
// stub resolver does not speak mDNS for arbitrary names, so this is what a
// correct client has to do.
func ResolveName(ifaces []*net.Interface, name string, timeout time.Duration) (string, error) {
	if timeout <= 0 {
		timeout = time.Second
	}
	if !strings.HasSuffix(name, ".") {
		name += "."
	}
	conns, err := dialQuerySockets(ifaces)
	if err != nil {
		return "", err
	}
	defer func() {
		for _, c := range conns {
			_ = c.conn.Close()
		}
	}()

	m := new(dns.Msg)
	m.SetQuestion(name, dns.TypeA)
	m.RecursionDesired = false
	m.Question[0].Qclass |= 1 << 15
	buf, err := m.Pack()
	if err != nil {
		return "", err
	}
	for _, c := range conns {
		_, _ = c.conn.WriteToUDP(buf, multicastAddr(c))
	}

	deadline := time.Now().Add(timeout)
	for _, c := range conns {
		rbuf := make([]byte, 9000)
		for time.Now().Before(deadline) {
			_ = c.conn.SetReadDeadline(deadline)
			n, _, err := c.conn.ReadFromUDP(rbuf)
			if err != nil {
				break
			}
			var msg dns.Msg
			if err := msg.Unpack(rbuf[:n]); err != nil {
				continue
			}
			for _, set := range [][]dns.RR{msg.Answer, msg.Extra} {
				for _, rr := range set {
					if a, ok := rr.(*dns.A); ok && strings.EqualFold(a.Hdr.Name, name) {
						return a.A.String(), nil
					}
				}
			}
		}
	}
	return "", fmt.Errorf("no A record for %s", name)
}

// dialQuerySockets opens ephemeral-port UDP sockets that can send mDNS queries
// out of specific interfaces. It deliberately does not bind 5353: a query-only
// socket must not compete with the responder or with Chrome.
//
// The sockets DO join 224.0.0.251 on their interface: some responders
// answer with multicast even when the query carried the QU bit, and
// without membership those answers never reach this socket. Ephemeral
// port + membership coexists with Chrome (research/06). A failed join is
// logged-and-ignored: unicast (QU) answers still arrive.
func dialQuerySockets(ifaces []*net.Interface) ([]*ifaceConn, error) {
	var out []*ifaceConn
	for _, ifi := range ifaces {
		ip := primaryIPv4(ifi)
		if ip == nil {
			continue
		}
		conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: ip, Port: 0})
		if err != nil {
			continue
		}
		_ = conn.SetReadBuffer(1 << 20)
		if err := joinQueryGroup(conn, ip); err != nil {
			_ = err // membership is best-effort; QU unicast still works
		}
		out = append(out, &ifaceConn{ifi: ifi, conn: conn, ipv4: true, ip: ip})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("discovery: could not open any query socket")
	}
	return out, nil
}

func multicastAddr(c *ifaceConn) *net.UDPAddr {
	if c.ipv4 {
		return &net.UDPAddr{IP: net.ParseIP(MDNSGroupV4), Port: MDNSPort}
	}
	return &net.UDPAddr{IP: net.ParseIP(MDNSGroupV6), Port: MDNSPort, Zone: c.ifi.Name}
}
