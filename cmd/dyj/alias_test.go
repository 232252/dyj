package main

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/232252/dyj/pkg/logx"
)

func testLogger() *logx.Logger { return logx.New(io.Discard, logx.ParseLevel(false)) }

// freeTCPPort asks the kernel for a free port (bind :0, close, reuse).
// The gap before the caller re-binds is a classic TOCTOU, but here only
// our own test binds it right after.
func freeTCPPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func TestStartIPPAliasSkipsWhenPrimaryIsAliasPort(t *testing.T) {
	if srv := startIPPAlias(ippAliasPort, ippAliasPort, http.NotFoundHandler(), testLogger()); srv != nil {
		srv.Close()
		t.Fatal("expected no alias when the primary port already is 631")
	}
}

func TestStartIPPAliasSkipsWhenPortBusy(t *testing.T) {
	port := freeTCPPort(t)
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		t.Skipf("cannot bind :%d here: %v", port, err)
	}
	defer ln.Close()

	if srv := startIPPAlias(6631, port, http.NotFoundHandler(), testLogger()); srv != nil {
		srv.Close()
		t.Fatal("expected no alias when the port is busy")
	}
}

func TestStartIPPAliasServesDataPlaneOnly(t *testing.T) {
	port := freeTCPPort(t)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "path=%s", r.URL.Path)
	})
	srv := startIPPAlias(6631, port, handler, testLogger())
	if srv == nil {
		t.Skipf("cannot bind :%d here", port)
	}
	defer srv.Close()

	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	var body string
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(base + "/ipp/print/laser")
		if err == nil {
			b, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			body = string(b)
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if body != "path=/ipp/print/laser" {
		t.Fatalf("alias did not serve the handler: body=%q", body)
	}

	if err := srv.Shutdown(t.Context()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	// After shutdown the alias must be gone: connections are refused.
	if _, err := http.Get(base + "/"); err == nil {
		t.Fatal("alias still serving after Shutdown")
	}
}