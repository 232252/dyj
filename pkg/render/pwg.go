// Package render owns everything about PAGE PIXELS: the PWG Raster
// (PWG 5102.4) reader/writer, text layout, and image rasterization.
//
// The PWG implementation mirrors CUPS' cups/raster-stream.c byte for byte
// (verified against the OpenPrinting source, 2026-09): the stream starts
// with the 4-byte magic "RaS2", every page carries a 1796-octet
// network-byte-order version-2 page header with most fields zeroed, and
// raster rows are stored as a one-octet repeat count (N-1 identical
// consecutive rows share one encoding) followed by a modified PackBits
// encoding of the row.
//
// Sequence encoding inside a row (bpp = bytes per pixel):
//
//	0x00 bpp           a single pixel (the repeat form with count 1)
//	0x00..0x7E         repeat the next bpp bytes (count+1) times
//	0x80               clear the REST of the line (white for sRGB/sGray)
//	0x81..0xFF         (257-byte) literal pixels follow, 2..128 of them
//
// Note the asymmetry with classic PackBits: literals never encode a
// single pixel (that is the 0x00 form) and 0x80 means end-of-line fill.
package render

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
)

// pwgMagic is the CUPS_RASTER_SYNC_PWG word, big-endian: 'RaS2'.
var pwgMagic = []byte{'R', 'a', 'S', '2'}

// pwgHeaderSize is sizeof(cups_page_header2_t): every field is a 64-byte
// string or a 4-byte word, no padding.
const pwgHeaderSize = 1796

// pwgColorSpace values are the CUPS_CSPACE numbers the header carries;
// PWG 5102.4 names them sgray_8/srgb_8.
const (
	cupsColorSpaceSW   = 18 // sGray, gamma 2.2
	cupsColorSpaceSRGB = 19 // sRGB
)

// ColorSpace is a raster pixel format.
type ColorSpace byte

// The two pixel formats dyj accepts and emits. PWG 5102.4 defines more
// (device-N, Adobe RGB, 16-bit...); accepting exactly what we can render
// and declare is the whole point of the format table (ADR-0007).
const (
	SGray ColorSpace = ColorSpace(cupsColorSpaceSW) // sgray_8, 1 byte/pixel
	SRGB  ColorSpace = ColorSpace(cupsColorSpaceSRGB)
)

// BytesPerPixel returns the pixel stride of the format.
func (c ColorSpace) BytesPerPixel() int {
	switch c {
	case SGray:
		return 1
	case SRGB:
		return 3
	default:
		return 0
	}
}

// PWGName returns the pwg-raster-document-type-supported keyword.
func (c ColorSpace) PWGName() string {
	switch c {
	case SGray:
		return "sgray_8"
	case SRGB:
		return "srgb_8"
	default:
		return fmt.Sprintf("cups-%d", byte(c))
	}
}

// PageHeader is the subset of the PWG page header dyj consumes. The
// remaining ~1700 bytes are structural (strings, vendor fields) and are
// preserved as zeros on write, exactly like CUPS does.
type PageHeader struct {
	Width, Height  int // pixels
	XDPI, YDPI     int
	ColorSpace     ColorSpace
	TotalPageCount uint32 // cupsInteger[0]; 0 = unknown
	MediaName      string // cupsPageSizeName, e.g. "iso_a4_210x297mm"
}

// Stride returns the byte width of one raster row.
func (h PageHeader) Stride() int { return h.Width * h.ColorSpace.BytesPerPixel() }

// Page is one fully materialized raster page. A 300 dpi A4 page in sRGB
// is ~26 MB and the GDI print path needs the whole bitmap anyway, so
// materializing one page at a time is the right memory trade-off
// (sequential streaming is preserved at the document level).
type Page struct {
	Header PageHeader
	Pix    []byte // Header.Height rows of Header.Stride() bytes
}

// Row returns the pixel bytes of row y.
func (p *Page) Row(y int) []byte {
	s := p.Header.Stride()
	return p.Pix[y*s : (y+1)*s]
}

// ---------------------------------------------------------------------------
// reading
// ---------------------------------------------------------------------------

// Reader decodes a PWG raster stream page by page.
type Reader struct {
	br      *bufio.Reader
	started bool
	pageNum int
}

// NewReader wraps r. The stream must begin with the "RaS2" magic.
func NewReader(r io.Reader) (*Reader, error) {
	br := bufio.NewReaderSize(r, 64<<10)
	magic := make([]byte, 4)
	if _, err := io.ReadFull(br, magic); err != nil {
		return nil, fmt.Errorf("render: reading PWG magic: %w", err)
	}
	if string(magic) != "RaS2" {
		return nil, fmt.Errorf("render: not a PWG raster stream (magic %q)", magic)
	}
	return &Reader{br: br, started: true}, nil
}

// Next returns the next page, or io.EOF after the last one. Row data is
// decompressed into Page.Pix; corrupt streams return an error and leave
// the reader unusable.
func (r *Reader) Next() (*Page, error) {
	head := make([]byte, pwgHeaderSize)
	if _, err := io.ReadFull(r.br, head); err != nil {
		if err == io.EOF {
			return nil, io.EOF
		}
		return nil, fmt.Errorf("render: reading page header: %w", err)
	}

	h := PageHeader{}
	h.Width = int(be32(head[372:]))
	h.Height = int(be32(head[376:]))
	h.XDPI = int(be32(head[276:]))
	h.YDPI = int(be32(head[280:]))
	cs := be32(head[400:])
	switch cs {
	case cupsColorSpaceSW:
		h.ColorSpace = SGray
	case cupsColorSpaceSRGB:
		h.ColorSpace = SRGB
	default:
		return nil, fmt.Errorf("render: page %d has unsupported cupsColorSpace %d (want sgray_8/srgb_8)", r.pageNum+1, cs)
	}
	bitsPerColor := be32(head[384:])
	bitsPerPixel := be32(head[388:])
	if bitsPerPixel != uint32(h.ColorSpace.BytesPerPixel())*8 || bitsPerColor != 8 {
		return nil, fmt.Errorf("render: page %d has unsupported depth (bits/color %d, bits/pixel %d)", r.pageNum+1, bitsPerColor, bitsPerPixel)
	}
	if h.Width <= 0 || h.Height <= 0 || h.Width > 1<<20 || h.Height > 1<<20 {
		return nil, fmt.Errorf("render: page %d has absurd dimensions %dx%d", r.pageNum+1, h.Width, h.Height)
	}
	if bpl := be32(head[392:]); int(bpl) != h.Stride() {
		return nil, fmt.Errorf("render: page %d cupsBytesPerLine %d != width*bpp %d", r.pageNum+1, bpl, h.Stride())
	}
	h.TotalPageCount = be32(head[452:])
	if name := zeroStr(head[1732:1796]); name != "" {
		h.MediaName = name
	}

	page := &Page{Header: h, Pix: make([]byte, h.Stride()*h.Height)}
	if err := r.readRows(page); err != nil {
		return nil, err
	}
	r.pageNum++
	return page, nil
}

// readRows decodes every row of the page, honoring the row repeat count.
func (r *Reader) readRows(p *Page) error {
	stride := p.Header.Stride()
	bpp := p.Header.ColorSpace.BytesPerPixel()
	row := make([]byte, stride)
	for y := 0; y < p.Header.Height; {
		repeat, err := r.br.ReadByte()
		if err != nil {
			return fmt.Errorf("render: page %d row %d: %w", r.pageNum+1, y, err)
		}
		if err := readRowData(r.br, row, bpp); err != nil {
			return fmt.Errorf("render: page %d row %d: %w", r.pageNum+1, y, err)
		}
		n := int(repeat) + 1
		for i := 0; i < n && y < p.Header.Height; i, y = i+1, y+1 {
			copy(p.Row(y), row)
		}
	}
	return nil
}

// readRowData decodes one modified-PackBits row into buf (exactly
// len(buf) bytes are produced).
func readRowData(br *bufio.Reader, buf []byte, bpp int) error {
	i := 0
	for i < len(buf) {
		b, err := br.ReadByte()
		if err != nil {
			return err
		}
		switch {
		case b == 0x80:
			// Clear the rest of the line: white for the formats we read.
			for ; i < len(buf); i++ {
				buf[i] = 0xff
			}
		case b&0x80 != 0:
			// Literal pixels: (257-b) of them, 2..128.
			n := (257 - int(b)) * bpp
			if n > len(buf)-i {
				n = len(buf) - i
			}
			if _, err := io.ReadFull(br, buf[i:i+n]); err != nil {
				return err
			}
			i += n
		default:
			// Repeat the next bpp bytes (b+1) times.
			n := (int(b) + 1) * bpp
			if n > len(buf)-i {
				n = len(buf) - i
			}
			pix := buf[i : i+bpp]
			if _, err := io.ReadFull(br, pix); err != nil {
				return err
			}
			i += bpp
			for n -= bpp; n > 0; n -= bpp {
				copy(buf[i:i+bpp], pix)
				i += bpp
			}
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// writing
// ---------------------------------------------------------------------------

// Writer emits a PWG raster stream. It writes the magic once and one
// header + compressed rows per page.
type Writer struct {
	w     io.Writer
	count uint32
}

// NewWriter writes the stream magic.
func NewWriter(w io.Writer) (*Writer, error) {
	if _, err := w.Write(pwgMagic); err != nil {
		return nil, err
	}
	return &Writer{w: w}, nil
}

// WritePage serializes one page. TotalPageCount is stamped into the
// header as-is (callers that know the final page count set it; 0 means
// unknown and is what CUPS readers expect from incremental streams).
func (w *Writer) WritePage(p *Page) error {
	h := make([]byte, pwgHeaderSize)
	copy(h[0:64], "PwgRaster") // MediaClass — the marker CUPS looks for
	put32(h[276:], uint32(p.Header.XDPI))
	put32(h[280:], uint32(p.Header.YDPI))
	// PageSize in points: px * 72 / dpi.
	put32(h[352:], uint32(p.Header.Width*72/max1(p.Header.XDPI)))
	put32(h[356:], uint32(p.Header.Height*72/max1(p.Header.YDPI)))
	put32(h[372:], uint32(p.Header.Width))
	put32(h[376:], uint32(p.Header.Height))
	put32(h[384:], 8)                                             // cupsBitsPerColor
	put32(h[388:], uint32(p.Header.ColorSpace.BytesPerPixel())*8) // cupsBitsPerPixel
	put32(h[392:], uint32(p.Header.Stride()))                     // cupsBytesPerLine
	put32(h[396:], 0)                                             // cupsColorOrder = chunked
	put32(h[400:], uint32(p.Header.ColorSpace))
	put32(h[420:], uint32(p.Header.ColorSpace.BytesPerPixel())) // cupsNumColors
	put32(h[452:], p.Header.TotalPageCount)                     // cupsInteger[0]
	put32(h[456:], 1)                                           // CrossFeedTransform = normal
	put32(h[460:], 1)                                           // FeedTransform = normal
	put32(h[464:], 0)                                           // ImageBox left
	put32(h[468:], 0)                                           // ImageBox top
	put32(h[472:], uint32(p.Header.Width))                      // ImageBox right
	put32(h[476:], uint32(p.Header.Height))                     // ImageBox bottom
	put32(h[480:], 0xffffff)                                    // AlternatePrimary = white
	if name := p.Header.MediaName; len(name) < 64 {
		copy(h[1732:1796], name)
	}

	if _, err := w.w.Write(h); err != nil {
		return err
	}
	if err := w.writeRows(p); err != nil {
		return err
	}
	w.count++
	return nil
}

// writeRows emits rows with the repeat-count prefix and PackBits rows.
func (w *Writer) writeRows(p *Page) error {
	stride := p.Header.Stride()
	var out bytesWriter
	out.buf = make([]byte, 0, stride*2+64)

	enc := make([]byte, 0, stride*2)
	for y := 0; y < p.Header.Height; {
		row := p.Row(y)
		// The repeat prefix is one byte: run-1, so a run is at most
		// 256 rows. Letting run reach 257 would truncate byte(run-1)
		// to 0x00 — which a reader decodes as ONE row, desynchronising
		// the whole stream.
		run := 1
		for run < 256 && y+run < p.Header.Height && bytes.Equal(p.Row(y+run), row) {
			run++
		}
		out.buf = append(out.buf, byte(run-1))
		enc = encodeRow(enc[:0], row, p.Header.ColorSpace.BytesPerPixel())
		out.buf = append(out.buf, enc...)
		y += run
	}
	_, err := w.w.Write(out.buf)
	return err
}

// encodeRow applies modified PackBits to one row.
func encodeRow(out []byte, row []byte, bpp int) []byte {
	i := 0
	for i < len(row) {
		start := i
		i += bpp

		if i == len(row) {
			// Single pixel at the end: the count-1 repeat form.
			out = append(out, 0x00)
			out = append(out, row[start:start+bpp]...)
			continue
		}

		if bytes.Equal(row[start:start+bpp], row[i:i+bpp]) {
			// Repeating pixel run, 2..128 pixels.
			count := 2
			i += bpp
			for count < 128 && i < len(row)-bpp && bytes.Equal(row[i:i+bpp], row[i+bpp:i+2*bpp]) {
				count++
				i += bpp
			}
			out = append(out, byte(count-1))
			out = append(out, row[start:start+bpp]...)
			continue
		}

		// Literal run. The loop breaks on a repeat pair or the end, so
		// count lands in 2..128 exactly like CUPS (single pixels are the
		// 0x00 form above).
		count := 1
		for count < 128 && i < len(row)-bpp {
			if bytes.Equal(row[i:i+bpp], row[i+bpp:i+2*bpp]) {
				break
			}
			count++
			i += bpp
		}
		if i >= len(row)-bpp && count < 128 {
			count++
			i += bpp
		}
		out = append(out, byte(257-count))
		out = append(out, row[start:start+count*bpp]...)
	}
	return out
}

// DecodeAll reads every page of the stream into memory. Convenience for
// tests and small documents; drivers stream via Reader.Next.
func DecodeAll(r io.Reader) ([]*Page, error) {
	rd, err := NewReader(r)
	if err != nil {
		return nil, err
	}
	var pages []*Page
	for {
		p, err := rd.Next()
		if err == io.EOF {
			return pages, nil
		}
		if err != nil {
			return nil, err
		}
		pages = append(pages, p)
	}
}

// ---------------------------------------------------------------------------
// small helpers
// ---------------------------------------------------------------------------

func be32(b []byte) uint32     { return binary.BigEndian.Uint32(b) }
func put32(b []byte, v uint32) { binary.BigEndian.PutUint32(b, v) }

func max1(v int) int {
	if v < 1 {
		return 1
	}
	return v
}

func zeroStr(b []byte) string {
	for i, c := range b {
		if c == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}

type bytesWriter struct{ buf []byte }

func (b *bytesWriter) Write(p []byte) (int, error) {
	b.buf = append(b.buf, p...)
	return len(p), nil
}
