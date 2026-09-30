package main

import (
	"reflect"
	"testing"

	"github.com/sq199/dyj/pkg/discovery"
)

func TestScanEntriesMergesPerInterfaceViews(t *testing.T) {
	// research/15 D-1: the raw and reference-client browsing paths report the
	// same printer under different ${IFACE} SRV targets; the merge key must
	// not include the host or one printer shows up as N rows.
	found := []discovery.Found{
		{
			Instance: "dyj-el._ipp._tcp.local",
			Host:     "dyj-el-br-e57f74e0bde4.local",
			Port:     6631,
			Addrs:    []string{"10.2.3.29", "172.17.0.1", "192.168.110.214"},
			TXTMap:   map[string]string{"ty": "dyj-el", "dyj": "1", "pair": "1", "rp": "ipp/print", "UUID": "u1"},
		},
		{
			Instance: "dyj-el._ipp._tcp.local.",
			Host:     "dyj-el-optun.local",
			Port:     6631,
			Addrs:    []string{"192.168.110.214"},
			TXTMap:   map[string]string{"ty": "dyj-el", "rp": "ipp/print", "UUID": "u1"},
		},
	}
	got := scanEntries(found, map[string]bool{})
	if len(got) != 1 {
		t.Fatalf("got %d entries, want 1 (merged): %+v", len(got), got)
	}
	e := got[0]
	if e.Name != "dyj-el" || !e.IsDyj || !e.NeedPIN || e.Path != "/ipp/print" {
		t.Errorf("unexpected fields: %+v", e)
	}
	if want := []string{"10.2.3.29", "172.17.0.1", "192.168.110.214"}; !reflect.DeepEqual(e.Addrs, want) {
		t.Errorf("addrs not unioned: got %v want %v", e.Addrs, want)
	}
}

func TestScanEntriesKeepsDistinctPrinters(t *testing.T) {
	found := []discovery.Found{
		{
			Instance: "dyj-a._ipp._tcp.local.", Host: "dyj-a.local.", Port: 6631,
			Addrs: []string{"192.168.110.10"}, TXTMap: map[string]string{"ty": "A"},
		},
		{
			Instance: "dyj-b._ipp._tcp.local.", Host: "dyj-b.local.", Port: 6631,
			Addrs: []string{"192.168.110.11"}, TXTMap: map[string]string{"ty": "B"},
		},
		// same label, different port: a different service, must not merge
		{
			Instance: "dyj-a._ipp._tcp.local.", Host: "dyj-a.local.", Port: 6642,
			Addrs: []string{"192.168.110.10"}, TXTMap: map[string]string{"ty": "A-tls"},
		},
	}
	got := scanEntries(found, map[string]bool{})
	if len(got) != 3 {
		t.Fatalf("got %d entries, want 3: %+v", len(got), got)
	}
}

func TestScanEntriesDropsOwnAndNonIPP(t *testing.T) {
	found := []discovery.Found{
		{Instance: "mine._ipp._tcp.local.", Host: "mine.local.", Port: 6631,
			TXTMap: map[string]string{"UUID": "own-uuid"}},
		{Instance: "_services._dns-sd._udp.local.", Host: "", Port: 0,
			TXTMap: map[string]string{}},
	}
	own := map[string]bool{"own-uuid": true, "mine.local": true}
	if got := scanEntries(found, own); len(got) != 0 {
		t.Fatalf("got %d entries, want 0: %+v", len(got), got)
	}
}

func TestUnionAddrs(t *testing.T) {
	got := unionAddrs([]string{"a", "b"}, []string{"b", "", "c"})
	if want := []string{"a", "b", "c"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %v want %v", got, want)
	}
}
