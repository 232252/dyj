package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestBootServesIPPAliasOn631 pins the field fix (research/17 §4, ADR-0017):
// OS "add printer by IP" dialogs assume port 631 and offer no port field.
// A share-role node must therefore also serve the IPP data plane on 631
// when the port is free — the primary port stays the contract.
func TestBootServesIPPAliasOn631(t *testing.T) {
	// Skip when a real CUPS/other service owns 631 on this machine.
	probe, err := net.Listen("tcp", fmt.Sprintf(":%d", ippAliasPort))
	if err != nil {
		t.Skipf("port %d busy on this host: %v", ippAliasPort, err)
	}
	probe.Close()

	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- runNode(ctx, serveOptions{
			Role:    roleShare,
			Name:    "alias-boot-test",
			Port:    16974,
			NoMDNS:  true,
			Spool:   dir,
			Out:     dir,
			Backend: "file",
		}, modeService)
	}()

	base := fmt.Sprintf("http://127.0.0.1:%d", ippAliasPort)
	var code int
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(base + "/ipp/print")
		if err == nil {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			code = resp.StatusCode
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if code == 0 {
		t.Fatal("share node did not serve the IPP alias on 631")
	}
	// Loopback is fully trusted: the data plane answers (not 401/404 from
	// some other service). Any IPP-level HTTP answer proves OUR router.
	if code == http.StatusNotFound {
		t.Fatalf("alias answered 404 — not the IPP router (code=%d)", code)
	}

	// The alias must NOT expose the admin surface.
	resp, err := http.Get(base + "/admin/api/status")
	if err == nil {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			t.Fatal("alias must be data-plane only: /admin answered 200")
		}
	}

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("node did not shut down")
	}
	// After shutdown the alias listener must be gone.
	if ln, err := net.Listen("tcp", fmt.Sprintf(":%d", ippAliasPort)); err == nil {
		ln.Close()
	} else if !strings.Contains(err.Error(), "address already in use") {
		t.Logf("post-shutdown bind: %v", err)
	} else {
		t.Fatal("alias listener still bound after shutdown")
	}
}