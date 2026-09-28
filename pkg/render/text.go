// text.go lays plain text onto raster pages with an embedded bitmap
// font (X11's MIT-licensed 7x13, via golang.org/x/image/font/basicfont).
// It is deliberately simple: monospace, word wrap, ASCII (non-ASCII
// runes render as '?'), form feeds start a new page. The point is that
// `dyj print notes.txt` produces an honest, readable page — not desktop
// publishing.
package render

import (
	"fmt"
	"strings"

	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

// TextOptions configures text layout.
type TextOptions struct {
	// DPI of the target page. The font cell scales with dpi so line
	// counts stay sane: 300 dpi → 3× (24×39 cells).
	DPI int
	// Media is the page size (default A4).
	Media Media
	// MarginMM is the page margin (default 15 mm).
	MarginMM float64
}

// TextPages lays text out onto sgray_8 pages.
func TextPages(text string, opt TextOptions) ([]*Page, error) {
	if opt.DPI <= 0 {
		opt.DPI = 300
	}
	if opt.Media.WidthMM == 0 {
		opt.Media = A4()
	}
	if opt.MarginMM == 0 {
		opt.MarginMM = 15
	}
	w, h := opt.Media.PixelSize(opt.DPI)
	if w < 64 || h < 64 {
		return nil, fmt.Errorf("render: page too small: %dx%d px at %d dpi", w, h, opt.DPI)
	}

	// Integer scale so glyphs stay crisp.
	scale := opt.DPI / 100
	if scale < 1 {
		scale = 1
	}
	face := basicfont.Face7x13
	cw := (face.Width + 1) * scale // +1: inter-character gap
	ch := face.Height * scale

	mx := int(opt.MarginMM * float64(opt.DPI) / 25.4)
	cols := (w - 2*mx) / cw
	rows := (h - 2*mx) / ch
	if cols < 8 || rows < 8 {
		return nil, fmt.Errorf("render: page %dx%d px too small for %dx%d px cells", w, h, cw, ch)
	}

	var pages []*Page
	cur := &layout{
		page:   newBlankPage(w, h, SGray),
		ox:     mx, oy: mx,
		cw: cw, ch: ch,
		cols:   cols, rows: rows,
		ascent: face.Ascent * scale,
	}

	for _, line := range strings.Split(normalizeText(text), "\n") {
		if line == "\x0c" { // form feed: force a new page
			if cur.cy == 0 && cur.emptyPage() {
				continue // a break at the very start is a no-op
			}
			pages = append(pages, cur.page)
			cur.page = newBlankPage(w, h, SGray)
			cur.cx, cur.cy = 0, 0
			continue
		}
		for {
			fitStr, rest := wrapLine(line, cols)
			cur.writeLine(fitStr)
			line = rest
			if line == "" {
				break
			}
			// The line was cut: the continuation starts on the next row.
			if cur.newline() { // page full
				pages = append(pages, cur.page)
				cur.page = newBlankPage(w, h, SGray)
				cur.cx, cur.cy = 0, 0
			}
		}
		if cur.newline() { // page full
			pages = append(pages, cur.page)
			cur.page = newBlankPage(w, h, SGray)
			cur.cx, cur.cy = 0, 0
		}
	}
	pages = append(pages, cur.page)
	for _, p := range pages {
		p.Header.TotalPageCount = uint32(len(pages))
		p.Header.MediaName = opt.Media.Name
	}
	return pages, nil
}

// layout is the cursor state of the page being written.
type layout struct {
	page       *Page
	ox, oy     int
	cw, ch     int
	cols, rows int
	ascent     int
	cx, cy     int
}

// emptyPage reports whether nothing has been drawn yet.
func (l *layout) emptyPage() bool { return l.cy == 0 && l.cx == 0 }

// newline advances the cursor; it reports true when the page is full.
func (l *layout) newline() bool {
	l.cx = 0
	l.cy++
	return l.cy >= l.rows
}

func (l *layout) writeLine(s string) {
	baseline := l.oy + l.cy*l.ch + l.ascent
	for i, r := range s {
		if i >= l.cols {
			break
		}
		drawGlyph(l.page, l.ox+i*l.cw, baseline, r, l.cw, l.ch)
	}
}

// normalizeText converts tabs to spaces, normalizes line endings and
// isolates form feeds on their own line.
func normalizeText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = strings.ReplaceAll(s, "\t", "    ")
	return strings.ReplaceAll(s, "\f", "\n\x0c\n")
}

// wrapLine word-wraps one logical line to cols cells. A line without
// break opportunities is hard-cut.
func wrapLine(line string, cols int) (fitStr, rest string) {
	if len(line) <= cols {
		return line, ""
	}
	cut := strings.LastIndex(line[:cols+1], " ")
	if cut <= 0 {
		cut = cols
	}
	return line[:cut], strings.TrimLeft(line[cut:], " ")
}

// newBlankPage allocates a white (0xff = no ink) page.
func newBlankPage(w, h int, cs ColorSpace) *Page {
	p := &Page{
		Header: PageHeader{Width: w, Height: h, ColorSpace: cs},
		Pix:    make([]byte, w*h*cs.BytesPerPixel()),
	}
	for i := range p.Pix {
		p.Pix[i] = 0xff
	}
	return p
}

// drawGlyph stamps one rune with its BASELINE at pixel (x, y), scaling
// the 7x13 bitmap into cw×ch. It paints 0x00 (full ink) blocks —
// anti-aliasing would need a gray ramp, and block-scaled bitmap text is
// perfectly readable.
func drawGlyph(p *Page, x, baseline int, r rune, cw, ch int) {
	if r > 0x7E || r < 0x20 {
		r = '?'
	}
	face := basicfont.Face7x13
	dr, mask, maskp, _, ok := face.Glyph(fixed.P(0, 0), r)
	if !ok {
		return
	}
	stride := p.Header.Stride()
	scaleX := cw / face.Width
	scaleY := ch / face.Height
	if scaleX < 1 {
		scaleX = 1
	}
	if scaleY < 1 {
		scaleY = 1
	}
	for my := 0; my < dr.Dy(); my++ {
		py := baseline + dr.Min.Y + my*scaleY
		if py < 0 || py+scaleY > p.Header.Height {
			continue
		}
		for mx := 0; mx < dr.Dx(); mx++ {
			a, _, _, _ := mask.At(maskp.X+mx, maskp.Y+my).RGBA()
			if a < 128<<8 {
				continue
			}
			px := x + dr.Min.X + mx*scaleX
			for dy := 0; dy < scaleY; dy++ {
				row := (py + dy) * stride
				for dx := 0; dx < scaleX; dx++ {
					if col := px + dx; col >= 0 && col < p.Header.Width {
						p.Pix[row+col] = 0x00
					}
				}
			}
		}
	}
}
