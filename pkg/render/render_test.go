package render

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

// TestPWGRoundTrip writes two pages through the encoder and reads them
// back byte-perfect. The pages exercise both color spaces and the row
// repeat path (identical rows) plus varied rows (packbit literals and
// repeats).
func TestPWGRoundTrip(t *testing.T) {
	p1 := &Page{
		Header: PageHeader{Width: 33, Height: 9, XDPI: 300, YDPI: 300,
			ColorSpace: SGray, TotalPageCount: 2, MediaName: "iso_a4_210x297mm"},
	}
	p1.Pix = make([]byte, p1.Header.Stride()*p1.Header.Height)
	for i := range p1.Pix {
		p1.Pix[i] = byte(i % 251)
	}

	p2 := &Page{
		Header: PageHeader{Width: 8, Height: 6, XDPI: 300, YDPI: 300, ColorSpace: SRGB},
	}
	p2.Pix = make([]byte, p2.Header.Stride()*p2.Header.Height)
	for y := 0; y < p2.Header.Height; y++ {
		for x := 0; x < p2.Header.Width; x++ {
			off := y * p2.Header.Stride()
			p2.Pix[off+x*3] = byte(x)
			p2.Pix[off+x*3+1] = byte(y)
			p2.Pix[off+x*3+2] = 0x80
		}
	}
	// A run of identical rows exercises the row repeat prefix.
	for y := 2; y < 5; y++ {
		copy(p2.Row(y), p2.Row(1))
	}

	var buf bytes.Buffer
	w, err := NewWriter(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.WritePage(p1); err != nil {
		t.Fatal(err)
	}
	if err := w.WritePage(p2); err != nil {
		t.Fatal(err)
	}

	got, err := DecodeAll(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("decoded %d pages, want 2", len(got))
	}
	if got[0].Header != p1.Header {
		t.Errorf("page1 header = %+v, want %+v", got[0].Header, p1.Header)
	}
	if !bytes.Equal(got[0].Pix, p1.Pix) {
		t.Errorf("page1 pixels differ (%d vs %d bytes)", len(got[0].Pix), len(p1.Pix))
	}
	if got[1].Header != p2.Header {
		t.Errorf("page2 header = %+v, want %+v", got[1].Header, p2.Header)
	}
	if !bytes.Equal(got[1].Pix, p2.Pix) {
		t.Errorf("page2 pixels differ")
		for y := 0; y < p2.Header.Height; y++ {
			if !bytes.Equal(got[1].Row(y), p2.Row(y)) {
				t.Errorf("row %d: got %x want %x", y, got[1].Row(y)[:min(8, len(p2.Row(y)))], p2.Row(y)[:min(8, len(p2.Row(y)))])
			}
		}
	}
}

// TestPWGMagicAndShortStream checks the reader's error paths.
func TestPWGMagicAndShortStream(t *testing.T) {
	if _, err := DecodeAll(bytes.NewReader([]byte("nope"))); err == nil {
		t.Fatal("expected an error for a non-PWG stream")
	}
	// Truncated page header: magic ok, header cut short.
	var buf bytes.Buffer
	buf.WriteString("RaS2")
	buf.Write(make([]byte, 100))
	if _, err := DecodeAll(bytes.NewReader(buf.Bytes())); err == nil {
		t.Fatal("expected an error for a truncated page header")
	}
}

func TestMediaFromPWG(t *testing.T) {
	cases := []struct {
		name    string
		w, h    float64
		wantErr bool
	}{
		{"iso_a4_210x297mm", 210, 297, false},
		{"na_letter_8.5x11in", 215.9, 279.4, false},
		{"na_index-4x6_4x6in", 101.6, 152.4, false},
		{"om_fancy", 0, 0, true},
		{"na_bad_1x2cm", 0, 0, true},
	}
	for _, tc := range cases {
		m, err := MediaFromPWG(tc.name)
		if tc.wantErr {
			if err == nil {
				t.Errorf("MediaFromPWG(%q) = %+v, want error", tc.name, m)
			}
			continue
		}
		if err != nil {
			t.Errorf("MediaFromPWG(%q): %v", tc.name, err)
			continue
		}
		const eps = 0.01
		if abs(m.WidthMM-tc.w) > eps || abs(m.HeightMM-tc.h) > eps {
			t.Errorf("MediaFromPWG(%q) = %vx%v, want %vx%v", tc.name, m.WidthMM, m.HeightMM, tc.w, tc.h)
		}
	}
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func TestTextPagesProducesInk(t *testing.T) {
	pages, err := TextPages("hello, printer!\nsecond line\n", TextOptions{DPI: 150})
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != 1 {
		t.Fatalf("pages = %d, want 1", len(pages))
	}
	p := pages[0]
	if p.Header.ColorSpace != SGray || p.Header.Width == 0 || p.Header.Height == 0 {
		t.Fatalf("header = %+v", p.Header)
	}
	ink := 0
	for _, b := range p.Pix {
		if b == 0 {
			ink++
		}
	}
	if ink == 0 {
		t.Fatal("no ink on the page — text did not render")
	}
	// Margins must stay blank.
	m := 10 * 150 / 2540
	for y := 0; y < m; y++ {
		row := p.Row(y)
		for _, b := range row {
			if b != 0xff {
				t.Fatalf("margin row %d has ink", y)
			}
		}
	}
}

func TestTextPagesWrapsAndPaginates(t *testing.T) {
	long := ""
	for i := 0; i < 5000; i++ {
		long += "word "
	}
	pages, err := TextPages(long, TextOptions{DPI: 150})
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) < 2 {
		t.Fatalf("a 25 KB text produced %d pages; wrap/paginate broken", len(pages))
	}
	for i, p := range pages {
		if p.Header.TotalPageCount != uint32(len(pages)) {
			t.Errorf("page %d TotalPageCount = %d, want %d", i, p.Header.TotalPageCount, len(pages))
		}
	}
}

func TestImagePagesGrayscaleAndColor(t *testing.T) {
	// A 64x64 gray PNG.
	gray := image.NewGray(image.Rect(0, 0, 64, 64))
	for i := range gray.Pix {
		gray.Pix[i] = byte(i)
	}
	var gb bytes.Buffer
	if err := png.Encode(&gb, gray); err != nil {
		t.Fatal(err)
	}
	pages, err := ImagePages(gb.Bytes(), ImageOptions{DPI: 150, Grayscale: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != 1 || pages[0].Header.ColorSpace != SGray {
		t.Fatalf("gray image → %+v, want one sgray page", pages[0].Header)
	}

	// A color PNG.
	col := image.NewRGBA(image.Rect(0, 0, 32, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			col.Set(x, y, color.RGBA{byte(x * 8), byte(y * 8), 0x40, 255})
		}
	}
	var cb bytes.Buffer
	if err := png.Encode(&cb, col); err != nil {
		t.Fatal(err)
	}
	pages, err = ImagePages(cb.Bytes(), ImageOptions{DPI: 150})
	if err != nil {
		t.Fatal(err)
	}
	if pages[0].Header.ColorSpace != SRGB {
		t.Fatalf("color image → %v, want srgb", pages[0].Header.ColorSpace)
	}
}
