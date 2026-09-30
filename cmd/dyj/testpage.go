// testpage.go renders the one-click test page (research/13 §4.6): a
// self-drawn PNG pushed through the LOCAL system queue — Windows via the
// same wingdi GDI path production jobs take, Linux/macOS via lp.
package main

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"time"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

var testPageCache []byte

// testPagePNG draws an A4-ish page with the brand line, host and
// timestamp. ASCII only: the stdlib bitmap font has no CJK glyphs.
func testPagePNG() ([]byte, error) {
	if testPageCache != nil {
		return testPageCache, nil
	}
	const w, h = 1240, 1754 // A4 at 150 dpi
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	// frame
	for x := 40; x < w-40; x++ {
		img.Set(x, 40, color.Black)
		img.Set(x, h-40, color.Black)
	}
	for y := 40; y < h-40; y++ {
		img.Set(40, y, color.Black)
		img.Set(w-40, y, color.Black)
	}
	lines := []string{
		"dyj test page",
		"",
		"host:   " + hostnameOr("unknown"),
		"time:   " + time.Now().Format("2006-01-02 15:04:05"),
		"",
		"If you can read this, the shared printer works.",
	}
	for i, line := range lines {
		drawText(img, 80, 120+i*40, line)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	testPageCache = buf.Bytes()
	return testPageCache, nil
}

func drawText(img *image.RGBA, x, y int, s string) {
	d := &font.Drawer{
		Dst:  img,
		Src:  image.NewUniform(color.Black),
		Face: basicfont.Face7x13,
	}
	d.Dot = fixed.P(x, y)
	d.DrawString(s)
}

// printTestPage pushes the PNG through the named system queue.
func printTestPage(ctx context.Context, queue string) error {
	pngBytes, err := testPagePNG()
	if err != nil {
		return err
	}
	return printPNGToQueue(ctx, queue, pngBytes)
}
