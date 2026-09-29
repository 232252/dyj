// image.go rasterizes JPEG and PNG documents onto pages: decode with
// the standard library, scale to fit the page with a small margin, and
// lay out one image per page (the sane behavior for photo printing).
package render

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"

	_ "image/jpeg" // register decoders
	_ "image/png"

	xdraw "golang.org/x/image/draw"
)

// ImageOptions configures image rasterization.
type ImageOptions struct {
	// DPI of the target page (default 300).
	DPI int
	// Media is the page size (default A4).
	Media Media
	// MarginMM is the margin around the fitted image (default 5 mm).
	MarginMM float64
	// Grayscale forces sgray_8 output (e.g. for mono printers).
	Grayscale bool
}

// ImagePages renders one image document into pages.
func ImagePages(data []byte, opt ImageOptions) ([]*Page, error) {
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("render: decoding image: %w", err)
	}
	if opt.DPI <= 0 {
		opt.DPI = 300
	}
	if opt.Media.WidthMM == 0 {
		opt.Media = A4()
	}
	if opt.MarginMM == 0 {
		opt.MarginMM = 5
	}

	pw, ph := opt.Media.PixelSize(opt.DPI)
	m := int(opt.MarginMM * float64(opt.DPI) / 25.4)
	boxW, boxH := pw-2*m, ph-2*m
	if boxW < 32 || boxH < 32 {
		return nil, fmt.Errorf("render: page too small for a %.0f mm margin", opt.MarginMM)
	}

	// Scale to FIT (never distort; upscale at most 2×).
	sb := src.Bounds()
	dw, dh := sb.Dx(), sb.Dy()
	if dw <= 0 || dh <= 0 {
		return nil, fmt.Errorf("render: image has empty bounds")
	}
	if dw > boxW || dh > boxH {
		// Shrink to fit.
		ratio := float64(boxW) / float64(dw)
		if r2 := float64(boxH) / float64(dh); r2 < ratio {
			ratio = r2
		}
		dw, dh = int(float64(dw)*ratio), int(float64(dh)*ratio)
	} else if dw*2 <= boxW && dh*2 <= boxH {
		// Small image on a big page: double it once, then stop — bitmap
		// upscaling beyond 2× looks worse than white margins.
		dw, dh = dw*2, dh*2
	}
	if dw > boxW {
		dw = boxW
	}
	if dh > boxH {
		dh = boxH
	}

	gray := opt.Grayscale || isGrayscale(src)
	cs := SRGB
	if gray {
		cs = SGray
	}
	page := newBlankPage(pw, ph, cs)
	page.Header.MediaName = opt.Media.Name
	page.Header.TotalPageCount = 1

	// Draw into an intermediate RGBA at target size, then convert, so
	// scaling is uniform regardless of source color model.
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	draw.Draw(dst, dst.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	xdraw.ApproxBiLinear.Scale(dst, dst.Bounds(), src, sb, xdraw.Over, nil)

	ox, oy := (pw-dw)/2, (ph-dh)/2
	stride := page.Header.Stride()
	for y := 0; y < dh; y++ {
		row := (oy + y) * stride
		for x := 0; x < dw; x++ {
			c := dst.RGBAAt(x, y)
			if gray {
				page.Pix[row+ox+x] = toGray(c)
			} else {
				base := row + (ox+x)*3
				page.Pix[base+0] = c.R
				page.Pix[base+1] = c.G
				page.Pix[base+2] = c.B
			}
		}
	}
	return []*Page{page}, nil
}

// isGrayscale reports whether every sampled pixel has R==G==B.
func isGrayscale(img image.Image) bool {
	switch img.ColorModel() {
	case color.GrayModel, color.Gray16Model:
		return false // caller already forces gray; keep the cheap path
	}
	b := img.Bounds()
	// Sample a grid rather than every pixel; a full scan of a 12 MP
	// photo costs tens of milliseconds for no practical gain.
	const n = 32
	for gy := 0; gy < n; gy++ {
		for gx := 0; gx < n; gx++ {
			x := b.Min.X + gx*(b.Dx()-1)/max1(n-1)
			y := b.Min.Y + gy*(b.Dy()-1)/max1(n-1)
			r, g, bl, _ := img.At(x, y).RGBA()
			if uint8(r>>8) != uint8(g>>8) || uint8(g>>8) != uint8(bl>>8) {
				return false
			}
		}
	}
	return true
}

// toGray converts an sRGB pixel to 8-bit luminance (Rec. 601 luma, the
// convention CUPS uses for sGray).
func toGray(c color.RGBA) byte {
	return byte((299*int(c.R) + 587*int(c.G) + 114*int(c.B)) / 1000)
}

// ImageToPage converts an already page-shaped image — every pixel is
// layout, e.g. a pdftoppm raster page — into a Page at the given DPI.
// Unlike ImagePages there is no fitting or margin: the image IS the
// page. The color space follows the content (sgray when every sampled
// pixel is neutral, sRGB otherwise), MediaName stays empty because the
// page size comes from the source document, not from dyj.
func ImageToPage(img image.Image, dpi int) *Page {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	gray := isGrayscale(img)
	cs := SRGB
	if gray {
		cs = SGray
	}
	page := newBlankPage(w, h, cs)
	stride := page.Header.Stride()
	for y := 0; y < h; y++ {
		row := y * stride
		for x := 0; x < w; x++ {
			c := img.At(b.Min.X+x, b.Min.Y+y)
			if gray {
				r, g, bl, _ := c.RGBA()
				page.Pix[row+x] = toGray(color.RGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(bl >> 8)})
			} else {
				r, g, bl, _ := c.RGBA()
				base := row + x*3
				page.Pix[base+0] = uint8(r >> 8)
				page.Pix[base+1] = uint8(g >> 8)
				page.Pix[base+2] = uint8(bl >> 8)
			}
		}
	}
	page.Header.XDPI, page.Header.YDPI = dpi, dpi
	return page
}
