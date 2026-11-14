package main

import (
	"net"
	"strconv"
	"testing"
	"time"
)

// resetScanGlobals isolates the package-level scan caches between tests.
func resetScanGlobals(t *testing.T) {
	t.Helper()
	scanResultCache.mu.Lock()
	scanResultCache.entries = nil
	scanResultCache.at = time.Time{}
	scanResultCache.running = false
	scanResultCache.mu.Unlock()
	lastSeen.mu.Lock()
	lastSeen.m = map[string]time.Time{}
	lastSeen.mu.Unlock()
	t.Cleanup(func() { resetScanGlobalsNoT() })
}

func resetScanGlobalsNoT() {
	scanResultCache.mu.Lock()
	scanResultCache.entries = nil
	scanResultCache.at = time.Time{}
	scanResultCache.running = false
	scanResultCache.mu.Unlock()
	lastSeen.mu.Lock()
	lastSeen.m = map[string]time.Time{}
	lastSeen.mu.Unlock()
}

// TestMergeWithLastSeenKeepsThenDrops pins the offline TTL contract: a row
// missing from one scan survives (marked unreachable), and after 90s of
// absence it is gone.
func TestMergeWithLastSeenKeepsThenDrops(t *testing.T) {
	resetScanGlobals(t)
	row := scanEntry{Instance: "p._ipp._tcp.local.", Host: "h.local", Port: 6631, Reachable: true, RTTms: 3}

	// Round 1: row observed → cached.
	scanResultCache.mu.Lock()
	scanResultCache.entries = []scanEntry{row}
	scanResultCache.mu.Unlock()
	out := mergeWithLastSeen([]scanEntry{row})
	if len(out) != 1 || !out[0].Reachable {
		t.Fatalf("fresh row must stay reachable: %+v", out)
	}

	// Round 2: row absent → carried over, unreachable.
	scanResultCache.mu.Lock()
	scanResultCache.entries = out
	scanResultCache.mu.Unlock()
	out = mergeWithLastSeen(nil)
	if len(out) != 1 || out[0].Reachable || out[0].RTTms != 0 {
		t.Fatalf("absent row must linger unreachable: %+v", out)
	}

	// Age the stamp past the TTL → row dropped.
	lastSeen.mu.Lock()
	id := scanIdentity(row)
	lastSeen.m[id] = time.Now().Add(-offlineTTL - time.Second)
	lastSeen.mu.Unlock()
	scanResultCache.mu.Lock()
	scanResultCache.entries = out
	scanResultCache.mu.Unlock()
	if out = mergeWithLastSeen(nil); len(out) != 0 {
		t.Fatalf("stale row must be dropped: %+v", out)
	}
}

// TestFirstReachableParallelRTT checks the parallel dial returns the live
// address with a positive RTT, and an all-dead set returns empty.
func TestFirstReachableParallelRTT(t *testing.T) {
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
	port, _ := strconv.Atoi(portStr)

	addr, rtt := firstReachable([]string{"127.0.0.1", "127.0.0.2"}, port)
	if addr != "127.0.0.1" || rtt < 1 {
		t.Fatalf("live dial: addr=%q rtt=%d", addr, rtt)
	}
	// Port 1 on loopback: nothing listens there.
	if addr, _ := firstReachable([]string{"127.0.0.1"}, 1); addr != "" {
		t.Fatalf("dead dial must return empty, got %q", addr)
	}
	if addr, _ := firstReachable(nil, port); addr != "" {
		t.Fatal("empty address set must return empty")
	}
}