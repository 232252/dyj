package driver

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"

	"github.com/sq199/dyj/pkg/render"
)

// File is the debugging/verification backend: every page lands in the
// output directory as a PNG, named job-<id>-doc<k>-page<n>.png. It is
// how the E2E tests prove the whole pipeline (IPP → spool → decode →
// page) without hardware, and it doubles as "print to PNG" for users.
type File struct{ Dir string }

// NewFile creates the output directory.
func NewFile(dir string) (*File, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("driver: creating output dir: %w", err)
	}
	return &File{Dir: dir}, nil
}

// Name implements Interface.
func (f *File) Name() string { return "file" }

// Print decodes the document and writes one PNG per page.
func (f *File) Print(ctx context.Context, job PrintJob, doc Document) error {
	pages, errc := Pages(ctx, doc, DecoderOptions{
		Media:     job.Media,
		Grayscale: !job.Color,
	})
	n := 0
	for p := range pages {
		n++
		name := fmt.Sprintf("job-%06d-doc%d-page%03d.png", job.ID, 0, n)
		out := filepath.Join(f.Dir, name)
		if err := writePNG(out, p); err != nil {
			return err
		}
	}
	return <-errc
}

func writePNG(path string, p *render.Page) error {
	var img image.Image
	if p.Header.ColorSpace == render.SGray {
		g := image.NewGray(image.Rect(0, 0, p.Header.Width, p.Header.Height))
		copy(g.Pix, p.Pix)
		img = g
	} else {
		rgb := image.NewRGBA(image.Rect(0, 0, p.Header.Width, p.Header.Height))
		stride := p.Header.Stride()
		for y := 0; y < p.Header.Height; y++ {
			row := y * stride
			for x := 0; x < p.Header.Width; x++ {
				off := row + x*3
				rgb.SetRGBA(x, y, color.RGBA{p.Pix[off], p.Pix[off+1], p.Pix[off+2], 0xff})
			}
		}
		img = rgb
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return fmt.Errorf("driver: encoding PNG: %w", err)
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}
