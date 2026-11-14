package discovery

import "testing"

// TestMergeFoundUUIDWinsOverIfaceNames pins the ADR-0015 merge contract at
// the discovery layer: the ${IFACE} views of one printer merge by UUID,
// while two machines sharing same-named printers stay distinct.
func TestMergeFoundUUIDWinsOverIfaceNames(t *testing.T) {
	in := []Found{
		{Instance: "p._ipp._tcp.local.", Host: "box-eth0.local.", Port: 6631,
			Addrs: []string{"10.0.0.1"}, TXTMap: map[string]string{"UUID": "u1"}},
		{Instance: "p._ipp._tcp.local.", Host: "box-wlan0.local.", Port: 6631,
			Addrs: []string{"192.168.1.2"}, TXTMap: map[string]string{"UUID": "u1"}},
		{Instance: "p._ipp._tcp.local.", Host: "other-eth0.local.", Port: 6631,
			Addrs: []string{"10.0.0.9"}, TXTMap: map[string]string{"UUID": "u2"}},
	}
	out := mergeFound(in)
	if len(out) != 2 {
		t.Fatalf("got %d entries, want 2 (u1 merged, u2 distinct): %+v", len(out), out)
	}
	for _, f := range out {
		if f.TXTMap["UUID"] == "u1" && len(f.Addrs) != 2 {
			t.Errorf("u1 addresses not unioned: %v", f.Addrs)
		}
	}
}

// TestMergeFoundHostBaseFallback covers services without a UUID (plain
// network printers): per-interface suffixes merge, different hosts do not.
func TestMergeFoundHostBaseFallback(t *testing.T) {
	in := []Found{
		{Instance: "p._ipp._tcp.local.", Host: "box-eth0.local.", Port: 6631, Addrs: []string{"10.0.0.1"}},
		{Instance: "p._ipp._tcp.local.", Host: "box.local.", Port: 6631, Addrs: []string{"10.0.0.1"}},
		{Instance: "p._ipp._tcp.local.", Host: "other.local.", Port: 6631, Addrs: []string{"10.0.0.9"}},
	}
	if out := mergeFound(in); len(out) != 2 {
		t.Fatalf("got %d entries, want 2: %+v", len(out), out)
	}
}

func TestHostBase(t *testing.T) {
	cases := []struct{ in, want string }{
		// One trailing "-<iface>" segment is stripped; the machine's own
		// hyphens stay (both ${IFACE} views of one host reduce equally).
		{"dyj-el-br-e57f74e0bde4.local.", "dyj-el-br"},
		{"dyj-el-optun.local.", "dyj-el"},
		{"box.local.", "box"},
		{"box.local", "box"},
		{"192.168.1.5", "192.168.1.5"},
		{"10.0.0.1:6631", "10.0.0.1:6631"}, // not an IP literal: untouched shape
	}
	for _, c := range cases {
		if got := hostBase(c.in); got != c.want {
			t.Errorf("hostBase(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}