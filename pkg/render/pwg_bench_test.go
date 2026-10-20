package render

import (
	"bytes"
	"testing"
)

// benchPage builds a synthetic page that exercises both PackBits regimes:
// long horizontal runs (identical rows compress to repeat prefixes) and a
// noisy band that falls back to literal packets.
func benchPage(width, height int, cs ColorSpace) *Page {
	p := &Page{Header: PageHeader{Width: width, Height: height, ColorSpace: cs}}
	p.Pix = make([]byte, p.Header.Stride()*height)
	bpp := cs.BytesPerPixel()
	for y := 0; y < height; y++ {
		row := p.Row(y)
		if y%64 == 0 { // fresh stripe every 64 rows: run reset + literals
			for x := 0; x < len(row); x += bpp {
				v := byte((x*7 + y*13) % 251) // pseudo-noise: no runs
				for k := 0; k < bpp; k++ {
					row[x+k] = v
				}
			}
		} else { // run rows: one value per stripe
			v := byte(y)
			for x := 0; x < len(row); x += bpp {
				for k := 0; k < bpp; k++ {
					row[x+k] = v
				}
			}
		}
	}
	return p
}

// A4 @ 300 dpi grayscale is the text path's real workload.
func BenchmarkPWGEncodeA4Gray(b *testing.B) {
	p := benchPage(2480, 3508, SGray)
	b.SetBytes(int64(p.Header.Stride() * p.Header.Height))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var buf bytes.Buffer
		w, err := NewWriter(&buf)
		if err != nil {
			b.Fatal(err)
		}
		if err := w.WritePage(p); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPWGDecodeA4Gray(b *testing.B) {
	p := benchPage(2480, 3508, SGray)
	var buf bytes.Buffer
	w, _ := NewWriter(&buf)
	if err := w.WritePage(p); err != nil {
		b.Fatal(err)
	}
	stream := buf.Bytes()
	b.SetBytes(int64(p.Header.Stride() * p.Header.Height))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r, err := NewReader(bytes.NewReader(stream))
		if err != nil {
			b.Fatal(err)
		}
		for {
			if _, err := r.Next(); err != nil {
				break
			}
		}
	}
}

// Letter @ 300 dpi sRGB is the photo path.
func BenchmarkPWGEncodeLetterColor(b *testing.B) {
	p := benchPage(2550, 3300, SRGB)
	b.SetBytes(int64(p.Header.Stride() * p.Header.Height))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var buf bytes.Buffer
		w, err := NewWriter(&buf)
		if err != nil {
			b.Fatal(err)
		}
		if err := w.WritePage(p); err != nil {
			b.Fatal(err)
		}
	}
}
