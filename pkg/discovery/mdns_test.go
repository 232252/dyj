package discovery

import (
	"net"
	"testing"

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
