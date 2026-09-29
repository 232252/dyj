package driver

// Optional application/pdf DECODE support (ADR-0007 honesty rule in
// action: the format is declared to clients only when a rasterizer
// exists on this machine — see serve.go). The rasterizer is poppler's
// pdftoppm, the PDF raster everyone already ships; dyj runs it as an
// external process, keeping the Go binary free of Ghostscript (AGPL)
// and of any CGO PDF binding.
//
// Process spawning policy (same as pkg/driver/cups): one whitelisted
// absolute-path program, fixed argument arrays, no shell at any layer.
// The testable halves (argument construction, page collection) are
// separate pure-ish functions; the full invocation runs as a
// skip-guarded integration test wherever poppler is installed.

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/sq199/dyj/pkg/render"
)

// pdfRasterizerOverride lets environments inject a stub pdftoppm (the
// full-path integration test uses the real one when present).
var pdfRasterizerOverride string

var (
	rasterizerOnce sync.Once
	rasterizerPath string
)

// rasterizer resolves pdftoppm once per process; "" when absent.
func rasterizer() string {
	if pdfRasterizerOverride != "" {
		return pdfRasterizerOverride
	}
	rasterizerOnce.Do(func() {
		if p, err := exec.LookPath("pdftoppm"); err == nil {
			rasterizerPath = p
		}
	})
	return rasterizerPath
}

// PDFAvailable reports whether this machine can render application/pdf.
func PDFAvailable() bool { return rasterizer() != "" }

// pdfRasterizerArgs builds the fixed-shape pdftoppm command line for
// one invocation: `TOOL -r DPI -png IN PREFIX`.
func pdfRasterizerArgs(dpi int, in, prefix string) []string {
	return []string{"-r", strconv.Itoa(dpi), "-png", in, prefix}
}

// collectPDFPages gathers `PREFIX-<n>.png` files from dir in document
// order and converts each into a Page at dpi. Natural ordering matters:
// pdftoppm pads page numbers only when a document has 10+ pages, so
// "pg-10.png" sorts after "pg-9.png" by number, not lexically.
func collectPDFPages(dir, prefix string, dpi int) ([]*render.Page, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	type pageFile struct {
		n    int
		path string
	}
	var files []pageFile
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, prefix+"-") || !strings.HasSuffix(name, ".png") {
			continue
		}
		mid := strings.TrimSuffix(strings.TrimPrefix(name, prefix+"-"), ".png")
		n, err := strconv.Atoi(mid)
		if err != nil {
			continue
		}
		files = append(files, pageFile{n, filepath.Join(dir, name)})
	}
	sort.Slice(files, func(i, k int) bool { return files[i].n < files[k].n })
	if len(files) == 0 {
		return nil, fmt.Errorf("driver: rasterizer produced no pages")
	}
	pages := make([]*render.Page, 0, len(files))
	for _, f := range files {
		img, err := decodePNGFile(f.path)
		if err != nil {
			return nil, fmt.Errorf("driver: pdf page %d: %w", f.n, err)
		}
		pages = append(pages, render.ImageToPage(img, dpi))
	}
	return pages, nil
}

// rasterizePDF runs the rasterizer over data and returns decoded pages
// in document order.
func rasterizePDF(ctx context.Context, tool string, data []byte, dpi int) ([]*render.Page, error) {
	dir, err := os.MkdirTemp("", "dyj-pdf-*")
	if err != nil {
		return nil, fmt.Errorf("driver: pdf temp dir: %w", err)
	}
	defer os.RemoveAll(dir)

	in := filepath.Join(dir, "in.pdf")
	if err := os.WriteFile(in, data, 0o600); err != nil {
		return nil, fmt.Errorf("driver: pdf input: %w", err)
	}
	prefix := filepath.Join(dir, "pg")

	cmd := &exec.Cmd{
		Path: tool,
		Args: append([]string{tool}, pdfRasterizerArgs(dpi, in, prefix)...),
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("driver: starting %s: %w", filepath.Base(tool), err)
	}
	cmd.Cancel = func() error {
		if ctx.Err() != nil {
			return cmd.Process.Kill()
		}
		return nil
	}
	if err := cmd.Wait(); err != nil {
		line := stderr.String()
		if i := strings.IndexByte(line, '\n'); i >= 0 {
			line = line[:i]
		}
		return nil, fmt.Errorf("driver: pdftoppm: %w (%s)", err, strings.TrimSpace(line))
	}
	return collectPDFPages(dir, "pg", dpi)
}

// decodePNGFile decodes one rasterized page.
func decodePNGFile(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("decoding raster page: %w", err)
	}
	return img, nil
}
