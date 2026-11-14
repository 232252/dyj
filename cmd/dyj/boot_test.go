package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestBootPrinterURIsHaveRealPort pins the startup-order fix: the IPP port
// must be bound BEFORE buildShare bakes it into printer URIs and the mDNS
// advertisement. The old order left actualPort=0 at first boot, producing
// "ipp://host:0/..." URIs and "discovery: invalid port 0" — printers were
// undiscoverable on the LAN.
func TestBootPrinterURIsHaveRealPort(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- runNode(ctx, serveOptions{
			Role:    roleShare,
			Name:    "boot-test",
			Port:    16971,
			NoMDNS:  true,
			Spool:   dir,
			Out:     dir,
			Backend: "file",
		}, modeService)
	}()

	base := "http://127.0.0.1:16971"
	var st struct {
		Version  string   `json:"version"`
		Port     int      `json:"port"`
		URIs     []string `json:"uris"`
		Printers []struct {
			Name string `json:"name"`
		} `json:"printers"`
	}
	up := false
	for i := 0; i < 60; i++ {
		resp, err := http.Get(base + "/admin/api/status")
		if err == nil {
			err = json.NewDecoder(resp.Body).Decode(&st)
			resp.Body.Close()
			if err == nil && st.Port != 0 {
				up = true
				break
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !up {
		t.Fatal("node did not come up on :16971")
	}
	if st.Port != 16971 {
		t.Fatalf("status port = %d, want 16971", st.Port)
	}
	if len(st.Printers) == 0 {
		t.Fatal("file backend must synthesize one printer")
	}
	if len(st.URIs) == 0 {
		t.Fatal("printer must advertise URIs")
	}
	for _, u := range st.URIs {
		if strings.Contains(u, ":0/") {
			t.Fatalf("printer URI has port 0 (startup-order bug): %s", u)
		}
		if !strings.Contains(u, fmt.Sprintf(":%d/", st.Port)) {
			t.Fatalf("printer URI port mismatch: %s (want :%d)", u, st.Port)
		}
	}

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("node did not shut down")
	}
}