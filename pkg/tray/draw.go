package tray

import (
	"bytes"
	"image"
	"image/png"
	"math"
)

// Vector rasterization for the tray icon: the 32×32 canvas carries the
// brand mark (assets/brand/dyj-mark.svg, printer + broadcast waves) as
// glyph strokes plus a bottom-right status badge. Shapes are evaluated
// as signed distance functions with 4×4 supersampling — deterministic,
// zero asset files, no cgo (docs/research/11 §4.3).

// nrgbaImage: straight-alpha canvas — must be NRGBA, not RGBA: png.Encode
// un-premultiplies *image.RGBA on write, which oversaturates every
// semi-transparent AA pixel.
type nrgbaImage struct{ *image.NRGBA }

func imageNewRGBA(w, h int) nrgbaImage {
	return nrgbaImage{image.NewNRGBA(image.Rect(0, 0, w, h))}
}

func (m nrgbaImage) set(x, y int, r, g, b, a uint8) {
	off := y*m.Stride + x*4
	m.Pix[off] = r
	m.Pix[off+1] = g
	m.Pix[off+2] = b
	m.Pix[off+3] = a
}

func pngEncode(buf *bytes.Buffer, img image.Image) error {
	return png.Encode(buf, img)
}

type vec struct{ x, y float64 }

// prim is a signed distance field: negative inside the shape.
type prim func(p vec) float64

func union(fns ...prim) prim {
	return func(p vec) float64 {
		d := math.Inf(1)
		for _, f := range fns {
			if v := f(p); v < d {
				d = v
			}
		}
		return d
	}
}

// stroke turns a filled-field SDF into a band of the given half width.
func stroke(f prim, half float64) prim {
	return func(p vec) float64 { return math.Abs(f(p)) - half }
}

// roundRectSD: signed distance to a filled rounded rectangle.
func roundRectSD(c, half, radius vec) prim {
	return func(p vec) float64 {
		qx := math.Abs(p.x-c.x) - (half.x - radius.x)
		qy := math.Abs(p.y-c.y) - (half.y - radius.y)
		ox := math.Max(qx, 0)
		oy := math.Max(qy, 0)
		return math.Hypot(ox, oy) + math.Min(math.Max(qx, qy), 0) - radius.x
	}
}

// arcSD removed: the small-size tray mark carries no waves (they fused
// into mush at 16px); the full mark lives in the SVG assets and the web
// icon set instead.

func diskSD(c vec, radius float64) prim {
	return func(p vec) float64 { return math.Hypot(p.x-c.x, p.y-c.y) - radius }
}

// Geometry: the tray icon is the brand tile — a blue rounded square
// carrying the white printer glyph (assets/brand/dyj-logo.svg, waves
// dropped for small-size legibility) — plus a bottom-right status badge
// whose rim fakes a taskbar-colored cutout. All shapes in 32px canvas
// units; the glyph is the 24-grid mark placed with place().

// place maps canvas p into the mark's 24-grid frame: uniform scale s
// around mark center mc, landing at canvas center c. Returned distance
// is scaled back so SDF math stays in canvas units.
func place(f prim, c, mc vec, s float64) prim {
	return func(p vec) float64 {
		q := vec{x: (p.x-c.x)/s + mc.x, y: (p.y-c.y)/s + mc.y}
		return f(q) * s
	}
}

var (
	// printer glyph: paper slot + body + output tray (Lucide printer
	// shapes, fused), stroke 2.4 on the 24 grid for small-size weight.
	printerSD = union(
		stroke(roundRectSD(vec{12, 6}, vec{6, 3}, vec{1, 1}), 1.2),
		stroke(roundRectSD(vec{12, 13.5}, vec{10, 4.5}, vec{2, 2}), 1.2),
		stroke(roundRectSD(vec{12, 18}, vec{6, 4}, vec{1, 1}), 1.2),
	)
	glyphSD = place(printerSD, vec{x: 14.4, y: 14.4}, vec{x: 12, y: 12.5}, 0.85)

	tileSD  = roundRectSD(vec{x: 16, y: 16}, vec{x: 15, y: 15}, vec{x: 6.75, y: 6.75})
	tileRGB = [3]byte{0x25, 0x63, 0xEB} // --dyj-primary blue-600

	badgeCenter = vec{x: 24.5, y: 24.5}
	badgeR      = 5.2
	badgeRimR   = 6.6
)

// render draws tile + glyph(ink) + badge rim(rim) + badge(status) with
// straight alpha compositing, 16 coverage levels per pixel.
func render(ink, rim, status [3]byte) []byte {
	const size = 32
	const ss = 4
	img := imageNewRGBA(size, size)
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			aTile, aInk, aRim, aStat := 0.0, 0.0, 0.0, 0.0
			for sy := 0; sy < ss; sy++ {
				for sx := 0; sx < ss; sx++ {
					p := vec{x: float64(x) + (float64(sx)+0.5)/ss, y: float64(y) + (float64(sy)+0.5)/ss}
					u := 1.0 / (ss * ss)
					if tileSD(p) <= 0 {
						aTile += u
					}
					if glyphSD(p) <= 0 {
						aInk += u
					}
					if diskSD(badgeCenter, badgeRimR)(p) <= 0 {
						aRim += u
					}
					if diskSD(badgeCenter, badgeR)(p) <= 0 {
						aStat += u
					}
				}
			}
			rC, gC, bC, aC := over(tileRGB, aTile, ink, 0) // tile base
			rC, gC, bC, aC = over(ink, aInk, [3]byte{rC, gC, bC}, aC)
			rC, gC, bC, aC = over(rim, aRim, [3]byte{rC, gC, bC}, aC)
			rC, gC, bC, aC = over(status, aStat, [3]byte{rC, gC, bC}, aC)
			if aC > 0 {
				img.set(x, y, rC, gC, bC, uint8(math.Round(aC*255)))
			}
		}
	}
	var buf bytes.Buffer
	_ = pngEncode(&buf, img)
	return buf.Bytes()
}

// over composites a straight-alpha top layer over a bottom one.
func over(top [3]byte, aT float64, bot [3]byte, aB float64) (r, g, b uint8, a float64) {
	a = aT + aB*(1-aT)
	if a <= 0 {
		return 0, 0, 0, 0
	}
	mix := func(t, u float64) float64 { return (t*aT + u*aB*(1-aT)) / a }
	return uint8(math.Round(mix(float64(top[0]), float64(bot[0])))),
		uint8(math.Round(mix(float64(top[1]), float64(bot[1])))),
		uint8(math.Round(mix(float64(top[2]), float64(bot[2])))), a
}
