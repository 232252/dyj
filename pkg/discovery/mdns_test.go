package discovery

import (
	"net"
	"testing"
	"time"

	"github.com/miekg/dns"
)

func testAdvertiser(t *testing.T) *Advertiser {
	t.Helper()
	a, err := New(Config{
		Instance: "dyj on desktop",
		HostName: "desktop.local.",
		Port:     6631,
		IPs:      []net.IP{net.ParseIP("192.168.1.5")},
		Path:     "/ipp/print",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return a
}

// testConn fabricates the per-interface context records() needs; with no
// started conns, hostFor resolves to the primary host name.
func testConn() *ifaceConn {
	return &ifaceConn{ifi: &net.Interface{Name: "eth0"}, ip: net.ParseIP("192.168.1.5")}
}

// txtFromRecords pulls the TXT strings out of a record set (nil when absent).
func txtFromRecords(rrs []dns.RR) []string {
	for _, rr := range rrs {
		if txt, ok := rr.(*dns.TXT); ok {
			return txt.Txt
		}
	}
	return nil
}

// TestSetTXTUpdatesLiveRecords pins the ADR-0015 contract: SetTXT swaps the
// content both record builders read, an unchanged set is a no-op, and a
// closed advertiser never changes again.
func TestSetTXTUpdatesLiveRecords(t *testing.T) {
	a := testAdvertiser(t)
	want := []string{"txtvers=1", "pair=0"}
	a.SetTXT(want)
	if got := a.TXT(); len(got) != 2 || got[1] != "pair=0" {
		t.Fatalf("TXT() = %v, want %v", got, want)
	}
	if got := txtFromRecords(a.instanceRecords(testConn(), a.InstanceFQDN())); len(got) != 2 || got[1] != "pair=0" {
		t.Fatalf("instanceRecords TXT = %v, want %v", got, want)
	}
	if got := txtFromRecords(a.allRecords()); len(got) != 2 || got[1] != "pair=0" {
		t.Fatalf("allRecords TXT = %v, want %v", got, want)
	}
	// TXT() hands out a copy: mutating it must not leak into the advertiser.
	cp := a.TXT()
	cp[1] = "pair=9"
	if got := a.TXT(); got[1] != "pair=0" {
		t.Fatalf("TXT() leaked internal slice: %v", got)
	}
	a.Close()
	a.SetTXT([]string{"pair=1"})
	if got := a.TXT(); got[1] != "pair=0" {
		t.Fatalf("SetTXT after Close must be a no-op, got %v", got)
	}
}

// TestReannounceLoopStopsOnClose pins the RFC 6762 §8.3 refresh lifecycle:
// the loop ticks while open and returns promptly once Close fires, so no
// announcement can race past the goodbye.
func TestReannounceLoopStopsOnClose(t *testing.T) {
	a := testAdvertiser(t)
	a.reannounceEvery = 10 * time.Millisecond
	if err := a.Start(); err != nil {
		t.Skipf("multicast unavailable in this environment: %v", err)
	}
	// Let a few ticks happen, then close and give the loop time to exit.
	time.Sleep(50 * time.Millisecond)
	if err := a.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	a.mu.Lock()
	stopped := a.stopCh == nil
	a.mu.Unlock()
	if !stopped {
		t.Fatal("Close must clear the stop channel")
	}
	// A second Close is a no-op (idempotent goodbye).
	if err := a.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestRecordsMetaQueryListsOnlyAnnouncedTypes(t *testing.T) {
	a := testAdvertiser(t)
	rrs := a.records(testConn(), &dns.Question{Name: BrowseService, Qtype: dns.TypePTR})
	if len(rrs) != 1 {
		t.Fatalf("meta query returned %d RRs, want 1: %v", len(rrs), rrs)
	}
	ptr, ok := rrs[0].(*dns.PTR)
	if !ok || ptr.Ptr != a.ServiceType() {
		t.Errorf("meta query PTR = %v, want %s", rrs[0], a.ServiceType())
	}
}

func TestRecordsSubTypeAndServiceTypePTR(t *testing.T) {
	a := testAdvertiser(t)
	c := testConn()
	for _, name := range []string{IPPSubService + ".local.", a.ServiceType()} {
		rrs := a.records(c, &dns.Question{Name: name, Qtype: dns.TypePTR})
		if len(rrs) != 1 {
			t.Fatalf("query %q returned %d RRs, want 1", name, len(rrs))
		}
		if ptr := rrs[0].(*dns.PTR); ptr.Ptr != a.InstanceFQDN() {
			t.Errorf("query %q PTR -> %s, want instance %s", name, ptr.Ptr, a.InstanceFQDN())
		}
	}
}

func TestInstanceRecordsCompleteWithoutDuplicates(t *testing.T) {
	a := testAdvertiser(t)
	rrs := a.instanceRecords(testConn(), a.InstanceFQDN())

	// One SRV (targeting the primary host) + one TXT + one A record, no
	// duplicated record names: the pre-simplification code re-emitted the
	// pair once per qtype plus the ANY tail.
	want := map[string]int{}
	for _, rr := range rrs {
		key := dns.Type(rr.Header().Rrtype).String() + " " + rr.Header().Name
		want[key]++
		if want[key] > 1 {
			t.Errorf("duplicate record %s in %v", key, rrs)
		}
	}
	for _, typ := range []uint16{dns.TypeSRV, dns.TypeTXT, dns.TypeA} {
		if !hasType(rrs, typ) {
			t.Errorf("instance records missing %s: %v", dns.Type(typ), rrs)
		}
	}
	if rrs[0].(*dns.SRV).Target != "desktop.local." {
		t.Errorf("SRV target = %s, want desktop.local.", rrs[0].(*dns.SRV).Target)
	}
}

func TestHostRecordsHonorsQType(t *testing.T) {
	a := testAdvertiser(t)
	c := testConn()
	if rrs := a.hostRecords(c, dns.TypeA); len(rrs) != 1 || !hasType(rrs, dns.TypeA) {
		t.Errorf("A query = %v", rrs)
	}
	// A v4 address must never surface as a v4-mapped (::ffff:/96) AAAA.
	if rrs := a.hostRecords(c, dns.TypeAAAA); len(rrs) != 0 {
		t.Errorf("AAAA query on v4-only config returned %v", rrs)
	}
	a6, err := New(Config{
		Instance: "dyj on desktop",
		HostName: "desktop.local.",
		Port:     6631,
		IPs:      []net.IP{net.ParseIP("fd00::5")},
		Path:     "/ipp/print",
	})
	if err != nil {
		t.Fatal(err)
	}
	if rrs := a6.hostRecords(c, dns.TypeA); len(rrs) != 0 {
		t.Errorf("A query on v6-only config returned %v", rrs)
	}
	if rrs := a6.hostRecords(c, dns.TypeAAAA); len(rrs) != 1 || !hasType(rrs, dns.TypeAAAA) {
		t.Errorf("AAAA query on v6-only config = %v", rrs)
	}
}

func TestAllRecordsShape(t *testing.T) {
	a := testAdvertiser(t)
	rrs := a.allRecords()
	if !hasName(rrs, BrowseService) || !hasName(rrs, IPPSubService+".local.") || !hasName(rrs, a.ServiceType()) {
		t.Errorf("allRecords missing a browse/subtype/service PTR: %v", rrs)
	}
	for _, typ := range []uint16{dns.TypeSRV, dns.TypeTXT, dns.TypeA} {
		if !hasType(rrs, typ) {
			t.Errorf("allRecords missing %s", dns.Type(typ))
		}
	}
}

func hasType(rrs []dns.RR, typ uint16) bool {
	for _, rr := range rrs {
		if rr.Header().Rrtype == typ {
			return true
		}
	}
	return false
}

func hasName(rrs []dns.RR, name string) bool {
	for _, rr := range rrs {
		if rr.Header().Name == name {
			return true
		}
	}
	return false
}
