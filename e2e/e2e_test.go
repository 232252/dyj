// Package e2e exercises the FULL pipeline in-process: a real IPP
// server wired to a real spooler with the file backend, driven by the
// real client package. This is the regression net for everything the
// unit tests assemble only piecewise.
//
// The binary-level equivalent (actual `dyj serve` + `dyj print`
// processes) is scripts/e2e.sh, run before every release.
package e2e

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"image/png"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/232252/dyj/pkg/attributes"
	"github.com/232252/dyj/pkg/client"
	"github.com/232252/dyj/pkg/driver"
	"github.com/232252/dyj/pkg/ipp"
	"github.com/232252/dyj/pkg/spooler"
)

// newSpool builds the printing stack's tail end: a real spooler over the
// file backend, its output directory alongside.
func newSpool(t *testing.T) (spool *spooler.Spooler, outDir string) {
	t.Helper()
	outDir = t.TempDir()
	backend, err := driver.NewFile(outDir)
	if err != nil {
		t.Fatal(err)
	}
	spool, err = spooler.New(spooler.Options{
		Dir:     t.TempDir(),
		Backend: backend,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = spool.Close(ctx)
	})
	return spool, outDir
}

// servePlain runs an IPP server over plain HTTP on an ephemeral port.
func servePlain(t *testing.T, spool *spooler.Spooler, name string) *client.Client {
	t.Helper()
	srv := ipp.NewServer(ipp.Options{Printer: testPrinter(name), Sink: spool})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve(ln) }()
	c, err := client.Parse(fmt.Sprintf("ipp://127.0.0.1:%d/ipp/print", portOf(ln)), "e2e-runner")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})
	return c
}

func testPrinter(name string) ipp.Printer {
	printer := ipp.DefaultPrinter()
	printer.Name = name
	printer.ColorSupported = true
	printer.AcceptingJobs = true
	printer.Port = 0
	return printer
}

func portOf(ln net.Listener) int { return ln.Addr().(*net.TCPAddr).Port }

// textJobBody renders text to a PWG raster stream, as `dyj print` does.
func textJobBody(t *testing.T, text string) []byte {
	t.Helper()
	pages, err := renderTextPages(text)
	if err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	w, err := newPWGWriter(&body)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range pages {
		if err := w.WritePage(p); err != nil {
			t.Fatal(err)
		}
	}
	return body.Bytes()
}

func TestFullPipelineTextDocument(t *testing.T) {
	spool, outDir := newSpool(t)
	c := servePlain(t, spool, "e2e printer")

	// 1. The printer answers Get-Printer-Attributes with a complete,
	//    decodable table (the same query Windows issues when adding).
	msg, err := c.GetPrinterAttributes()
	if err != nil {
		t.Fatal(err)
	}
	if msg.Code != 0 {
		t.Fatalf("status 0x%04X", msg.Code)
	}
	if name := attributes.GroupFirstString(msg.Group(0x04), "printer-name"); name != "e2e printer" {
		t.Fatalf("printer-name = %q", name)
	}

	// 2. Submit a text document like `dyj print` does.
	body := textJobBody(t, "hello from the e2e test\nsecond line\n")
	jobID, err := c.PrintJob(bytes.NewReader(body), int64(len(body)),
		"image/pwg-raster", client.PrintOptions{JobName: "e2e.txt"})
	if err != nil {
		t.Fatal(err)
	}

	// 3. Poll until the job completes.
	waitJobState(t, c, spool, jobID, 9 /*completed*/, 10*time.Second)

	// 4. The file backend must have produced a valid PNG page.
	assertOnePNG(t, outDir)
}

func TestFullPipelineCancel(t *testing.T) {
	spool, _ := newSpool(t)
	c := servePlain(t, spool, "e2e cancel printer")
	body := textJobBody(t, "cancel me\n")
	jobID, err := c.PrintJob(bytes.NewReader(body), int64(len(body)),
		"image/pwg-raster", client.PrintOptions{JobName: "cancel.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Cancel(jobID); err != nil {
		t.Fatal(err)
	}
	waitJobState(t, c, spool, jobID, 7 /*canceled*/, 10*time.Second)
}

func TestFullPipelineGzipSubmission(t *testing.T) {
	spool, outDir := newSpool(t)
	c := servePlain(t, spool, "e2e gzip printer")
	// The CLIENT compresses (like a real CUPS/Windows client would):
	// one gzip layer end to end, unwrapped exactly once at the server.
	body := textJobBody(t, "gzipped!\n")
	jobID, err := c.PrintJob(bytes.NewReader(body), int64(len(body)),
		"image/pwg-raster", client.PrintOptions{JobName: "gz.txt", Gzip: true})
	if err != nil {
		t.Fatal(err)
	}
	waitJobState(t, c, spool, jobID, 9, 10*time.Second)
	assertOnePNG(t, outDir)
}

// TestFullPipelineTLS drives the same flow over ipps:// — the transport
// AirPrint-class clients use. The client must accept the printer's
// self-signed certificate (ParseInsecure) and everything behind TLS
// (IPP routing → spool → file backend) must behave exactly like plain.
func TestFullPipelineTLS(t *testing.T) {
	spool, outDir := newSpool(t)
	srv := ipp.NewServer(ipp.Options{Printer: testPrinter("e2e tls printer"), Sink: spool})
	certFile, keyFile := selfSignedCert(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.ServeTLS(ln, certFile, keyFile) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})

	c, err := client.ParseInsecure(fmt.Sprintf("ipps://127.0.0.1:%d/ipp/print", portOf(ln)), "e2e-runner")
	if err != nil {
		t.Fatal(err)
	}
	msg, err := c.GetPrinterAttributes()
	if err != nil {
		t.Fatalf("ipps Get-Printer-Attributes: %v", err)
	}
	if name := attributes.GroupFirstString(msg.Group(0x04), "printer-name"); name != "e2e tls printer" {
		t.Fatalf("printer-name = %q", name)
	}

	body := textJobBody(t, "over ipps\n")
	jobID, err := c.PrintJob(bytes.NewReader(body), int64(len(body)),
		"image/pwg-raster", client.PrintOptions{JobName: "tls.txt"})
	if err != nil {
		t.Fatal(err)
	}
	waitJobState(t, c, spool, jobID, 9 /*completed*/, 10*time.Second)
	assertOnePNG(t, outDir)
}

func waitJobState(t *testing.T, c *client.Client, spool *spooler.Spooler, id int32, want int32, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		rows, err := c.Jobs("all")
		if err == nil {
			for _, r := range rows {
				if r.ID == id && r.State == want {
					return
				}
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	rows, _ := c.Jobs("all")
	t.Fatalf("job %d never reached state %d; jobs: %+v", id, want, rows)
}

func assertOnePNG(t *testing.T, outDir string) {
	t.Helper()
	pngs, err := filepath.Glob(filepath.Join(outDir, "job-*.png"))
	if err != nil || len(pngs) != 1 {
		t.Fatalf("output PNGs = %v (%v)", pngs, err)
	}
	fh, err := os.Open(pngs[0])
	if err != nil {
		t.Fatal(err)
	}
	defer fh.Close()
	img, err := png.Decode(fh)
	if err != nil {
		t.Fatalf("backend output is not a PNG: %v", err)
	}
	if img.Bounds().Dx() < 1000 || img.Bounds().Dy() < 1000 {
		t.Fatalf("page raster %v is implausibly small for A4@300dpi", img.Bounds())
	}
}

// selfSignedCert writes a throwaway ECDSA pair covering 127.0.0.1, the
// same shape the node's ipps bootstrap generates at first run.
func selfSignedCert(t *testing.T) (certPath, keyPath string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatal(err)
	}
	tmpl := x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "e2e dyj"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1)},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	certPath = filepath.Join(dir, "tls.crt")
	keyPath = filepath.Join(dir, "tls.key")
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatal(err)
	}
	return certPath, keyPath
}
