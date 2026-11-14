package discovery

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/232252/dyj/internal/slices"
)

// Subnet scanning is the cross-subnet / mesh fallback (research/15 P3,
// ADR-0015): mDNS multicast does not cross routers and many mesh adapters
// lack the multicast flag, so a directed unicast sweep of the local /24s
// (plus an optional user CIDR) finds shares the browse can never see.
//
// Hard rules from the field (research/12 §4): a TCP handshake alone is NOT
// proof of a printer — mesh overlays complete handshakes on ports with no
// listener. Every candidate must answer a real IPP Get-Printer-Attributes.

// IPPProbe is the minimal "does this endpoint speak IPP?" check the scan
// needs. The caller injects its own (cmd/dyj has the full IPP client);
// keeping it an interface avoids a discovery→client import cycle.
type IPPProbe func(ctx context.Context, host string, port int) (name string, ok bool)

// SubnetEntry is one confirmed IPP endpoint found by the sweep.
type SubnetEntry struct {
	Host string
	Port int
	Name string
}

// DefaultIPPHost is the port dyj shares listen on.
const DefaultIPPHost = 6631

// maxSweepAddresses caps a single sweep so a fat-fingered /8 cannot turn
// into a 16-million-dial storm.
const maxSweepAddresses = 1024

// SweepOptions tunes one subnet sweep.
type SweepOptions struct {
	// Ifaces supplies the local /24s when CIDRs is empty.
	Ifaces []*net.Interface
	// CIDRs are explicit user ranges (each ≤ maxSweepAddresses hosts).
	CIDRs []string
	// Probe confirms a candidate; required.
	Probe IPPProbe
	// Budget bounds the whole sweep (default 8s).
	Budget time.Duration
	// Concurrency caps parallel dials (default 64).
	Concurrency int
	// Ports overrides the dial ports (default 6631, plus 631 for explicit
	// CIDRs). Tests inject the ephemeral port of a live listener.
	Ports []int
}

// SweepSubnets runs the directed sweep and returns confirmed IPP endpoints.
// It is safe to call concurrently; the caller (admin API) single-flights it.
func SweepSubnets(ctx context.Context, opt SweepOptions) ([]SubnetEntry, error) {
	if opt.Probe == nil {
		return nil, fmt.Errorf("discovery: sweep needs an IPP probe")
	}
	budget := opt.Budget
	if budget <= 0 {
		budget = 8 * time.Second
	}
	conc := opt.Concurrency
	if conc <= 0 {
		conc = 64
	}
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()

	targets, err := sweepTargets(opt)
	if err != nil {
		return nil, err
	}
	if len(targets) == 0 {
		return nil, nil
	}

	type result struct {
		e  SubnetEntry
		ok bool
	}
	ch := make(chan result, len(targets))
	sem := make(chan struct{}, conc)
	var wg sync.WaitGroup
	for _, t := range targets {
		wg.Add(1)
		go func(t sweepTarget) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			// Fast TCP screen first: a closed port must not cost an IPP
			// round-trip. 600ms is generous for a LAN, tight for a sweep.
			if !tcpOpen(ctx, t.host, t.port, 600*time.Millisecond) {
				ch <- result{}
				return
			}
			name, ok := opt.Probe(ctx, t.host, t.port)
			if !ok {
				ch <- result{}
				return
			}
			ch <- result{e: SubnetEntry{Host: t.host, Port: t.port, Name: name}, ok: true}
		}(t)
	}
	wg.Wait()
	close(ch)

	var out []SubnetEntry
	seen := map[string]bool{}
	for r := range ch {
		if !r.ok {
			continue
		}
		k := r.e.Host + "|" + strconv.Itoa(r.e.Port)
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, r.e)
	}
	slices.SortStableFunc(out, func(a, b SubnetEntry) int {
		if a.Host != b.Host {
			return strings.Compare(a.Host, b.Host)
		}
		return a.Port - b.Port
	})
	return out, nil
}

type sweepTarget struct {
	host string
	port int
}

// sweepTargets builds the deduped dial list: every selected interface's
// /24 on 6631, plus any explicit CIDR (6631, and 631 only as a fallback
// for hosts that did not answer on 6631 — third-party IPP printers).
func sweepTargets(opt SweepOptions) ([]sweepTarget, error) {
	var cidrs []string
	if len(opt.CIDRs) > 0 {
		cidrs = opt.CIDRs
	} else {
		for _, ifi := range opt.Ifaces {
			ip := primaryIPv4(ifi)
			if ip == nil {
				continue
			}
			cidrs = append(cidrs, ip.String()+"/24")
		}
	}
	hosts := map[string]bool{}
	var order []string
	for _, c := range cidrs {
		hs, err := cidrHosts(c)
		if err != nil {
			return nil, err
		}
		for _, h := range hs {
			if !hosts[h] {
				hosts[h] = true
				order = append(order, h)
			}
		}
	}
	if len(order) > maxSweepAddresses {
		order = order[:maxSweepAddresses]
	}
	ports := opt.Ports
	if len(ports) == 0 {
		ports = []int{DefaultIPPHost}
		// 631 fallback only for explicit CIDRs, to keep the auto /24 sweep
		// cheap. Third-party printers on the LAN are already found by
		// mDNS; the sweep's job is the dyj shares mDNS missed.
		if len(opt.CIDRs) > 0 {
			ports = append(ports, 631)
		}
	}
	var targets []sweepTarget
	for _, h := range order {
		for _, p := range ports {
			targets = append(targets, sweepTarget{host: h, port: p})
		}
	}
	return targets, nil
}

// cidrHosts expands a CIDR into usable host addresses (network and
// broadcast excluded for ≤/30), capped at maxSweepAddresses.
func cidrHosts(cidr string) ([]string, error) {
	_, ipnet, err := net.ParseCIDR(strings.TrimSpace(cidr))
	if err != nil {
		return nil, fmt.Errorf("discovery: bad CIDR %q: %w", cidr, err)
	}
	ones, bits := ipnet.Mask.Size()
	if bits != 32 {
		return nil, fmt.Errorf("discovery: only IPv4 CIDRs are supported: %q", cidr)
	}
	hostBits := bits - ones
	if hostBits > 20 {
		return nil, fmt.Errorf("discovery: CIDR %q too large (max /12)", cidr)
	}
	base := ipnet.IP.To4()
	start := uint32(base[0])<<24 | uint32(base[1])<<16 | uint32(base[2])<<8 | uint32(base[3])
	count := uint32(1) << hostBits
	var out []string
	for i := uint32(0); i < count && len(out) < maxSweepAddresses; i++ {
		// Skip network/broadcast addresses on normal subnets.
		if count > 2 && (i == 0 || i == count-1) {
			continue
		}
		v := start + i
		out = append(out, fmt.Sprintf("%d.%d.%d.%d", byte(v>>24), byte(v>>16), byte(v>>8), byte(v)))
	}
	return out, nil
}

// tcpOpen reports whether a TCP connect completes within the budget.
func tcpOpen(ctx context.Context, host string, port int, timeout time.Duration) bool {
	d := net.Dialer{Timeout: timeout}
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// LocalSweepCIDRs derives the /24 of every selected interface, for the UI
// to show as the default sweep range.
func LocalSweepCIDRs(ifaces []*net.Interface) []string {
	var out []string
	seen := map[string]bool{}
	for _, ifi := range ifaces {
		ip := primaryIPv4(ifi)
		if ip == nil {
			continue
		}
		c := ip.String() + "/24"
		if !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	return out
}