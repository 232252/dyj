package driver

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/sq199/dyj/pkg/render"
)

func TestPDFRasterizerArgs(t *testing.T) {
	got := pdfRasterizerArgs(300, "/tmp/x/in.pdf", "/tmp/x/pg")
	want := []string{"-r", "300", "-png", "/tmp/x/in.pdf", "/tmp/x/pg"}
	if len(got) != len(want) {
		t.Fatalf("args = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("args[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// writeTestPNG writes a small deterministic PNG page into dir.
func writeTestPNG(t *testing.T, dir, name string, w, h int, tint uint8) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: tint, G: uint8(y), B: uint8(x), A: 0xff})
		}
	}
	f, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestCollectPDFPagesNaturalOrder(t *testing.T) {
	dir := t.TempDir()
	// Write in scrambled lexical order: 10, 2, 1 — collection must
	// return document order 1, 2, 10 (pdftoppm pads numbers only for
	// 10+ page documents, so natural sort is the contract).
	writeTestPNG(t, dir, "pg-10.png", 8, 8, 30)
	writeTestPNG(t, dir, "pg-2.png", 8, 8, 20)
	writeTestPNG(t, dir, "pg-1.png", 8, 8, 10)
	// Stray files must be ignored.
	if err := os.WriteFile(filepath.Join(dir, "pg-junk.png"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	pages, err := collectPDFPages(dir, "pg", 150)
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != 3 {
		t.Fatalf("got %d pages, want 3", len(pages))
	}
	// The tint lives in R; page order proves natural sorting.
	for i, want := range []uint8{10, 20, 30} {
		if pages[i].Header.Width != 8 || pages[i].Header.Height != 8 {
			t.Errorf("page %d dims = %dx%d", i, pages[i].Header.Width, pages[i].Header.Height)
		}
		if pages[i].Header.ColorSpace != render.SRGB {
			t.Errorf("page %d colorspace = %v, want sRGB", i, pages[i].Header.ColorSpace)
		}
		if pages[i].Pix[0] != want {
			t.Errorf("page %d out of order (R=%d, want %d)", i, pages[i].Pix[0], want)
		}
	}
}

func TestCollectPDFPagesEmpty(t *testing.T) {
	dir := t.TempDir()
	if _, err := collectPDFPages(dir, "pg", 150); err == nil {
		t.Fatal("empty dir should error")
	}
}

func TestPDFAvailableRespectsOverride(t *testing.T) {
	old := pdfRasterizerOverride
	defer func() { pdfRasterizerOverride = old }()
	pdfRasterizerOverride = "/nonexistent/fake-pdftoppm"
	if !PDFAvailable() {
		t.Fatal("override should make PDFAvailable true")
	}
	pdfRasterizerOverride = ""
	_ = PDFAvailable() // resets nothing; just exercises the default path
}

// TestPDFDecodeEndToEnd runs the real pdftoppm pipeline when poppler is
// installed (Linux CI / dev machines); skipped elsewhere. It round-trips
// render.PagesToPDF output back through the decoder.
func TestPDFDecodeEndToEnd(t *testing.T) {
	if !PDFAvailable() {
		t.Skip("pdftoppm not installed on this machine")
	}
	pages := make([]*render.Page, 2)
	for i := range pages {
		pages[i] = &render.Page{
			Header: render.PageHeader{
				Width: 16, Height: 16, XDPI: 72, YDPI: 72,
				ColorSpace: render.SRGB, TotalPageCount: 2,
			},
			Pix: make([]byte, 16*16*3),
		}
		for p := range pages[i].Pix {
			pages[i].Pix[p] = byte(i*50 + p%50)
		}
	}
	pdf, err := render.PagesToPDF(pages)
	if err != nil {
		t.Fatal(err)
	}
	doc := Document{
		Format: "application/pdf",
		Size:   int64(len(pdf)),
		Open: func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(pdf)), nil
		},
	}
	got, errc := Pages(context.Background(), doc, DecoderOptions{DPI: 72})
	var round []*render.Page
	for p := range got {
		round = append(round, p)
	}
	if err := <-errc; err != nil {
		t.Fatalf("pdf decode pipeline: %v", err)
	}
	if len(round) != 2 {
		t.Fatalf("decoded %d pages, want 2", len(round))
	}
	for i, p := range round {
		if p.Header.Width != 16 || p.Header.Height != 16 {
			t.Errorf("page %d dims = %dx%d, want 16x16", i, p.Header.Width, p.Header.Height)
		}
		if p.Header.ColorSpace != render.SRGB {
			t.Errorf("page %d colorspace = %v, want sRGB", i, p.Header.ColorSpace)
		}
	}
}
