package discovery

import (
	"context"
	"net"
	"strconv"
	"testing"
	"time"
)

func TestCIDRHosts(t *testing.T) {
	hs, err := cidrHosts("192.168.1.0/24")
	if err != nil {
		t.Fatal(err)
	}
	// /24 = 256 addresses minus network + broadcast = 254.
	if len(hs) != 254 {
		t.Fatalf("got %d hosts, want 254", len(hs))
	}
	if hs[0] != "192.168.1.1" || hs[len(hs)-1] != "192.168.1.254" {
		t.Fatalf("range endpoints: %s .. %s", hs[0], hs[len(hs)-1])
	}
	// Too-large range is rejected, not silently truncated into a storm.
	if _, err := cidrHosts("10.0.0.0/8"); err == nil {
		t.Fatal("/8 must be rejected")
	}
	if _, err := cidrHosts("not-a-cidr"); err == nil {
		t.Fatal("bad CIDR must be rejected")
	}
	// /32 is a single host (no network/broadcast exclusion).
	if hs, err := cidrHosts("10.0.0.5/32"); err != nil || len(hs) != 1 || hs[0] != "10.0.0.5" {
		t.Fatalf("/32: %v %v", hs, err)
	}
}

// TestSweepSubnetsConfirmsViaIPP pins the core safety rule: a TCP-open
// port that does NOT speak IPP must not surface, and a real IPP endpoint
// must. The fake probe stands in for the IPP client.
func TestSweepSubnetsConfirmsViaIPP(t *testing.T) {
	// One live listener on loopback; the probe only "confirms" that port.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()
	_, portStr, _ := net.SplitHostPort(ln.Addr().String())
	livePort, _ := strconv.Atoi(portStr)

	probe := func(ctx context.Context, host string, port int) (string, bool) {
		if host == "127.0.0.1" && port == livePort {
			return "test-printer", true
		}
		return "", false // TCP open but not IPP → must be dropped
	}
	// Sweep a /32 so only loopback is dialed; override the port via a
	// custom target by using the explicit-CIDR path is not enough (it
	// hardcodes 6631/631), so test the probe gate through sweepTargets +
	// a direct SweepSubnets on a CIDR that includes loopback is impractical.
	// Instead verify the building blocks: tcpOpen + probe gate.
	if !tcpOpen(context.Background(), "127.0.0.1", livePort, time.Second) {
		t.Fatal("live port must be tcp-open")
	}
	if tcpOpen(context.Background(), "127.0.0.1", 1, 200*time.Millisecond) {
		t.Fatal("port 1 must not be tcp-open")
	}
	if name, ok := probe(context.Background(), "127.0.0.1", livePort); !ok || name != "test-printer" {
		t.Fatalf("probe gate: %q %v", name, ok)
	}
	if _, ok := probe(context.Background(), "127.0.0.1", livePort+1); ok {
		t.Fatal("non-IPP port must not confirm")
	}
}

// TestSweepSubnetsEndToEnd runs a real sweep against a loopback /32 with
// the live port injected: the full fan-out → TCP screen → IPP-confirm
// path must surface exactly the live endpoint, and a probe that refuses
// (TCP open but not IPP) must surface nothing.
func TestSweepSubnetsEndToEnd(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()
	_, portStr, _ := net.SplitHostPort(ln.Addr().String())
	livePort, _ := strconv.Atoi(portStr)

	found, err := SweepSubnets(context.Background(), SweepOptions{
		CIDRs:  []string{"127.0.0.1/32"},
		Ports:  []int{livePort},
		Budget: 2 * time.Second,
		Probe: func(ctx context.Context, host string, port int) (string, bool) {
			return "test-printer", true
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 || found[0].Host != "127.0.0.1" || found[0].Port != livePort || found[0].Name != "test-printer" {
		t.Fatalf("live endpoint must surface: %+v", found)
	}

	// Same open port, but the probe refuses → nothing (mesh fake-handshake).
	none, err := SweepSubnets(context.Background(), SweepOptions{
		CIDRs:  []string{"127.0.0.1/32"},
		Ports:  []int{livePort},
		Budget: 2 * time.Second,
		Probe: func(ctx context.Context, host string, port int) (string, bool) {
			return "", false
		},
	})
	if err != nil || len(none) != 0 {
		t.Fatalf("refused probe must yield nothing: %+v %v", none, err)
	}
}