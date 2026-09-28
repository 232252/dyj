// Package driver is the printing-backend seam (ADR-0002 D4): the one
// extension point where "what to do with rendered pages" is decided.
//
// A backend consumes decoded render.Pages. Document decoding (PWG
// raster, JPEG, PNG, text) happens HERE, once, so every backend sees the
// same pixel stream and adding a backend never touches protocol code.
//
// Shipped backends:
//
//	wingdi (windows) — draw pages on a Windows printer DC through the
//	                   vendor driver already installed in the spooler.
//	                   This is the production path on Windows: the OS
//	                   driver does the printer-language encoding.
//	file  (all)      — save each page as PNG into a directory; the
//	                   debugging/E2E backend and the "PDF-printer"
//	                   experience.
//	null  (all)      — count pages and drop them; for tests.
//
// There is deliberately no raw//dev/usb/lp0 backend: dyj's declared
// document formats are raster formats, and streaming raster into a
// printer that expects PCL/ESC-P produces garbage. A raw backend only
// makes sense together with raw passthrough formats, which we do not
// declare (honesty rule, ADR-0007).
package driver

import (
	"context"
	"fmt"
	"io"

	"github.com/sq199/dyj/pkg/render"
)

// Document is one spooled document.
type Document struct {
	// Format is the document-format mime type as submitted.
	Format string
	// Size is the spooled size in bytes (-1 unknown).
	Size int64
	// Open opens the spooled bytes. Called once per attempt so retries
	// re-read from the start.
	Open func() (io.ReadCloser, error)
}

// PrintJob carries the job metadata a backend may honor.
type PrintJob struct {
	ID     int32
	User   string
	Name   string
	Media  string // PWG media name, "" = driver default
	Copies int
	Color  bool   // print-color-mode requested color
	Sides  string // "" | one-sided | two-sided-long-edge | two-sided-short-edge
}

// Interface is a printing backend. Implementations must be safe for
// concurrent use (the spooler serializes jobs, but status probes may
// run alongside).
type Interface interface {
	// Name identifies the backend in logs and diagnostics.
	Name() string
	// Print renders one document. ctx is canceled when the job is
	// canceled mid-flight; implementations must check it between pages.
	Print(ctx context.Context, job PrintJob, doc Document) error
}

// DecoderOptions tune document decoding.
type DecoderOptions struct {
	// DPI is the rasterization target for text and images (default 300).
	DPI int
	// Media is the requested page size (empty = A4).
	Media string
	// Grayscale forces sgray output.
	Grayscale bool
}

// Pages decodes a document into a stream of pages. It is the shared
// front half every backend uses; backends implement only "what happens
// to a page".
func Pages(ctx context.Context, doc Document, opt DecoderOptions) (<-chan *render.Page, <-chan error) {
	pages := make(chan *render.Page, 1)
	errc := make(chan error, 1)
	go func() {
		defer close(pages)
		err := decodeInto(ctx, doc, opt, pages)
		errc <- err
		close(errc)
	}()
	return pages, errc
}

// decodeInto dispatches on the declared document format. Anything we do
// not explicitly decode is an error: the server never accepted such a
// format, so reaching here means a client lied about the format — and
// "application/octet-stream" is not a renderable promise.
func decodeInto(ctx context.Context, doc Document, opt DecoderOptions, out chan<- *render.Page) error {
	dpi := opt.DPI
	if dpi <= 0 {
		dpi = 300
	}
	media := render.A4()
	if opt.Media != "" {
		if m, err := render.MediaFromPWG(opt.Media); err == nil {
			media = m
		}
	}

	rc, err := doc.Open()
	if err != nil {
		return fmt.Errorf("driver: opening document: %w", err)
	}
	defer rc.Close()

	switch doc.Format {
	case "image/pwg-raster":
		rd, err := render.NewReader(rc)
		if err != nil {
			return err
		}
		for {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			p, err := rd.Next()
			if err == io.EOF {
				return nil
			}
			if err != nil {
				return err
			}
			select {
			case out <- p:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	case "image/jpeg", "image/png":
		data, err := io.ReadAll(rc)
		if err != nil {
			return err
		}
		pgs, err := render.ImagePages(data, render.ImageOptions{
			DPI: dpi, Media: media, Grayscale: opt.Grayscale,
		})
		if err != nil {
			return err
		}
		return emit(ctx, pgs, out)
	case "text/plain":
		data, err := io.ReadAll(rc)
		if err != nil {
			return err
		}
		pgs, err := render.TextPages(string(data), render.TextOptions{
			DPI: dpi, Media: media,
		})
		if err != nil {
			return err
		}
		return emit(ctx, pgs, out)
	default:
		return fmt.Errorf("driver: cannot render document format %q", doc.Format)
	}
}

func emit(ctx context.Context, pgs []*render.Page, out chan<- *render.Page) error {
	for _, p := range pgs {
		select {
		case out <- p:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// EachCopy expands a page stream by the requested copy count.
func EachCopy(pages <-chan *render.Page, copies int) <-chan *render.Page {
	if copies <= 1 {
		return pages
	}
	out := make(chan *render.Page, 1)
	go func() {
		defer close(out)
		var all []*render.Page
		for p := range pages {
			all = append(all, p)
		}
		for c := 0; c < copies; c++ {
			for _, p := range all {
				out <- p
			}
		}
	}()
	return out
}
