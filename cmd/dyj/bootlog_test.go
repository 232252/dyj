package main

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestBootLogFileHasEvents pins service-mode observability: after the node
// is up, the rotating log file must exist and carry the boot events. The
// field report (2026-11-14) had a running service with an empty log ring
// and a stale file — one broken sink must never silence the whole logger.
func TestBootLogFileHasEvents(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- runNode(ctx, serveOptions{
			Role:    roleShare,
			Name:    "bootlog-test",
			Port:    16973,
			NoMDNS:  true,
			Spool:   dir,
			Out:     dir,
			Backend: "file",
		}, modeService)
	}()

	up := false
	for i := 0; i < 60; i++ {
		resp, err := http.Get("http://127.0.0.1:16973/admin/api/status")
		if err == nil {
			resp.Body.Close()
			up = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !up {
		t.Fatal("node did not come up on :16973")
	}

	data, err := os.ReadFile(filepath.Join(dir, "logs", "dyj.log"))
	if err != nil {
		t.Fatalf("service-mode log file missing: %v", err)
	}
	if !strings.Contains(string(data), "evt=boot.listen") {
		t.Fatalf("log file has no boot events:\n%s", data)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("node did not shut down")
	}
}