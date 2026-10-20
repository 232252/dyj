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
	"fmt"
	"image/png"
	"io"
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

type fixture struct {
	base   string // http://127.0.0.1:<port>
	client *client.Client
	outDir string
	spool  *spooler.Spooler
	srv    *ipp.Server
}

func startFixture(t *testing.T) *fixture {
	t.Helper()
	outDir := t.TempDir()
	backend, err := driver.NewFile(outDir)
	if err != nil {
		t.Fatal(err)
	}
	spool, err := spooler.New(spooler.Options{
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

	printer := ipp.DefaultPrinter()
	printer.Name = "e2e printer"
	printer.ColorSupported = true
	printer.AcceptingJobs = true
	printer.Port = 0
	srv := ipp.NewServer(ipp.Options{Printer: printer, Sink: spool})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	go func() { _ = srv.Serve(ln) }()

	uri := fmt.Sprintf("ipp://127.0.0.1:%d/ipp/print", port)
	c, err := client.Parse(uri, "e2e-runner")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})
	return &fixture{base: fmt.Sprintf("http://127.0.0.1:%d", port),
		client: c, outDir: outDir, spool: spool, srv: srv}
}

func TestFullPipelineTextDocument(t *testing.T) {
	f := startFixture(t)

	// 1. The printer answers Get-Printer-Attributes with a complete,
	//    decodable table (the same query Windows issues when adding).
	msg, err := f.client.GetPrinterAttributes()
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
	pages, err := makeTextPages("hello from the e2e test\nsecond line\n")
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
	jobID, err := f.client.PrintJob(bytes.NewReader(body.Bytes()), int64(body.Len()),
		"image/pwg-raster", client.PrintOptions{JobName: "e2e.txt"})
	if err != nil {
		t.Fatal(err)
	}

	// 3. Poll until the job completes.
	waitJobState(t, f, jobID, 9 /*completed*/, 10*time.Second)

	// 4. The file backend must have produced a valid PNG page.
	pngs, err := filepath.Glob(filepath.Join(f.outDir, "job-*.png"))
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

func TestFullPipelineCancel(t *testing.T) {
	f := startFixture(t)
	pages, err := makeTextPages("cancel me\n")
	if err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	w, _ := newPWGWriter(&body)
	for _, p := range pages {
		_ = w.WritePage(p)
	}
	jobID, err := f.client.PrintJob(bytes.NewReader(body.Bytes()), int64(body.Len()),
		"image/pwg-raster", client.PrintOptions{JobName: "cancel.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.client.Cancel(jobID); err != nil {
		t.Fatal(err)
	}
	waitJobState(t, f, jobID, 7 /*canceled*/, 10*time.Second)
}

func TestFullPipelineGzipSubmission(t *testing.T) {
	f := startFixture(t)
	pages, err := makeTextPages("gzipped!\n")
	if err != nil {
		t.Fatal(err)
	}
	var raw bytes.Buffer
	w, _ := newPWGWriter(&raw)
	for _, p := range pages {
		_ = w.WritePage(p)
	}
	// The CLIENT compresses (like a real CUPS/Windows client would):
	// one gzip layer end to end, unwrapped exactly once at the server.
	jobID, err := f.client.PrintJob(bytes.NewReader(raw.Bytes()), int64(raw.Len()),
		"image/pwg-raster", client.PrintOptions{JobName: "gz.txt", Gzip: true})
	if err != nil {
		t.Fatal(err)
	}
	waitJobState(t, f, jobID, 9, 10*time.Second)
}

func waitJobState(t *testing.T, f *fixture, id int32, want int32, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		rows, err := f.client.Jobs("all")
		if err == nil {
			for _, r := range rows {
				if r.ID == id && r.State == want {
					return
				}
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	rows, _ := f.client.Jobs("all")
	t.Fatalf("job %d never reached state %d; jobs: %+v", id, want, rows)
}

// --- tiny helpers duplicating cmd-level encoding (main packages cannot
// be imported) ---

func makeTextPages(text string) ([]*page, error) {
	return renderTextPages(text)
}

var _ = io.EOF
