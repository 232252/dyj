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

	"github.com/232252/dyj/pkg/render"
)

// pwgBytes builds a 1-page PWG raster document from a generated image.
func pwgBytes(t *testing.T, w, h int, cs render.ColorSpace, fill func(x, y int) color.Color) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, fill(x, y))
		}
	}
	page := pageFromImage(t, img, cs)
	var buf bytes.Buffer
	wr, err := render.NewWriter(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if err := wr.WritePage(page); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func pageFromImage(t *testing.T, img image.Image, cs render.ColorSpace) *render.Page {
	t.Helper()
	b := img.Bounds()
	p := &render.Page{Header: render.PageHeader{
		Width: b.Dx(), Height: b.Dy(), XDPI: 300, YDPI: 300, ColorSpace: cs,
	}}
	p.Pix = make([]byte, p.Header.Stride()*b.Dy())
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			c := img.At(x, y)
			r, g, bl, _ := c.RGBA()
			if cs == render.SRGB {
				off := y*p.Header.Stride() + x*3
				p.Pix[off], p.Pix[off+1], p.Pix[off+2] = byte(r >> 8), byte(g >> 8), byte(bl >> 8)
			} else {
				p.Pix[y*p.Header.Stride()+x] = byte((299*int(r>>8) + 587*int(g>>8) + 114*int(bl>>8)) / 1000)
			}
		}
	}
	return p
}

func TestFileBackendWritesPNGPerFormat(t *testing.T) {
	dir := t.TempDir()
	f, err := NewFile(dir)
	if err != nil {
		t.Fatal(err)
	}

	pwg := pwgBytes(t, 16, 16, render.SRGB, func(x, y int) color.Color {
		return color.RGBA{byte(x * 16), byte(y * 16), 0x80, 255}
	})

	err = f.Print(context.Background(), PrintJob{ID: 7, Name: "t", Color: true}, Document{
		Format: "image/pwg-raster",
		Open: func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(pwg)), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	out, err := filepath.Glob(filepath.Join(dir, "job-000007-*.png"))
	if err != nil || len(out) != 1 {
		t.Fatalf("glob = %v (%v), want 1 png", out, err)
	}
	fh, err := os.Open(out[0])
	if err != nil {
		t.Fatal(err)
	}
	defer fh.Close()
	img, err := png.Decode(fh)
	if err != nil {
		t.Fatalf("output is not a valid PNG: %v", err)
	}
	if img.Bounds().Dx() != 16 || img.Bounds().Dy() != 16 {
		t.Fatalf("png bounds = %v, want 16x16", img.Bounds())
	}
}

func TestNullBackendCountsAndRejectsUnknownFormat(t *testing.T) {
	n := &Null{}
	if err := n.Print(context.Background(), PrintJob{}, Document{
		Format: "image/pwg-raster",
		Open: func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(pwgBytes(t, 8, 8, render.SGray,
				func(int, int) color.Color { return color.Gray{0x40} }))), nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	if n.Pages() != 1 {
		t.Fatalf("pages = %d, want 1", n.Pages())
	}

	if err := n.Print(context.Background(), PrintJob{}, Document{
		Format: "application/vnd.stupidthing",
		Open: func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(nil)), nil
		},
	}); err == nil {
		t.Fatal("unknown format must be an error, not silence")
	}
}

func TestBackendHonorsCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	n := &Null{}
	err := n.Print(ctx, PrintJob{}, Document{
		Format: "image/pwg-raster",
		Open: func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(pwgBytes(t, 8, 8, render.SGray,
				func(int, int) color.Color { return color.Gray{0x40} }))), nil
		},
	})
	if err == nil {
		t.Fatal("canceled context must surface as an error")
	}
}
