package render

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
)

// ---------------------------------------------------------------------------
// Image-only PDF writer.
//
// CUPS accepts PDF on stdin/file and runs its own filter chain, which
// makes PDF the one container every CUPS queue renders correctly. dyj
// uses it as the delivery envelope for the cups backend: spooled raster
// pages are wrapped into a single "one image per page" PDF — no fonts,
// no vectors, no text — so the writer below stays a few hundred lines
// and fully deterministic (golden-tested).
//
// This is NOT a document renderer: PDF *decoding* is the separate,
// optional pdftoppm path (driver/pdf.go). Writing an envelope is a
// different problem from rendering arbitrary PDF content.
// ---------------------------------------------------------------------------

// PagesToPDF wraps raster pages into a one-image-per-page PDF. Page
// dimensions come from each page's header (pixels / DPI → points).
func PagesToPDF(pages []*Page) ([]byte, error) {
	if len(pages) == 0 {
		return nil, fmt.Errorf("render: PagesToPDF needs at least one page")
	}

	// PDF object numbering: 1 = catalog, 2 = pages tree, then per page
	// i (0-based): 3+3i = image XObject, 4+3i = page, 5+3i = contents.
	var buf bytes.Buffer
	offsets := make([]int, 3+3*len(pages))
	writeObj := func(num int, body []byte) {
		offsets[num] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n", num)
		buf.Write(body)
		buf.WriteString("\nendobj\n")
	}

	buf.WriteString("%PDF-1.4\n%\xe2\xe3\xcf\xd3\n")

	kids := make([]byte, 0, 16*len(pages))
	for i := range pages {
		if i > 0 {
			kids = append(kids, ' ')
		}
		kids = fmt.Appendf(kids, "%d 0 R", 4+3*i)
	}
	writeObj(1, []byte("<< /Type /Catalog /Pages 2 0 R >>"))
	writeObj(2, []byte(fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", kids, len(pages))))

	for i, p := range pages {
		jpegBytes, err := pageJPEG(p)
		if err != nil {
			return nil, err
		}
		wPt := float64(p.Header.Width) * 72.0 / float64(p.Header.XDPI)
		hPt := float64(p.Header.Height) * 72.0 / float64(p.Header.YDPI)

		csName := "/DeviceRGB"
		if p.Header.ColorSpace == SGray {
			csName = "/DeviceGray"
		}
		imgNum, pageNum, contentNum := 3+3*i, 4+3*i, 5+3*i

		var img bytes.Buffer
		fmt.Fprintf(&img, "<< /Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace %s /BitsPerComponent 8 /Filter /DCTDecode /Length %d >>\nstream\n",
			p.Header.Width, p.Header.Height, csName, len(jpegBytes))
		img.Write(jpegBytes)
		img.WriteString("\nendstream")
		writeObj(imgNum, img.Bytes())

		// The image fills the page exactly: the raster WAS produced at
		// this page size, so no scaling math beyond MediaBox is needed.
		var content bytes.Buffer
		fmt.Fprintf(&content, "q %d 0 0 %d 0 0 cm /Im0 Do Q", int(wPt), int(hPt))
		var contentObj bytes.Buffer
		fmt.Fprintf(&contentObj, "<< /Length %d >>\nstream\n", content.Len())
		content.WriteTo(&contentObj)
		contentObj.WriteString("\nendstream")
		writeObj(contentNum, contentObj.Bytes())

		var page bytes.Buffer
		fmt.Fprintf(&page, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %.2f %.2f] /Resources << /XObject << /Im0 %d 0 R >> >> /Contents %d 0 R >>",
			wPt, hPt, imgNum, contentNum)
		writeObj(pageNum, page.Bytes())
	}

	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n", len(offsets))
	buf.WriteString("0000000000 65535 f \n")
	for _, off := range offsets[1:] {
		fmt.Fprintf(&buf, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets), xref)
	return buf.Bytes(), nil
}

// pageJPEG encodes a raster page as JPEG in its native color space.
func pageJPEG(p *Page) ([]byte, error) {
	switch p.Header.ColorSpace {
	case SGray:
		img := image.NewGray(image.Rect(0, 0, p.Header.Width, p.Header.Height))
		for y := 0; y < p.Header.Height; y++ {
			copy(img.Pix[y*img.Stride:y*img.Stride+p.Header.Width], p.Row(y))
		}
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 92}); err != nil {
			return nil, fmt.Errorf("render: encoding page JPEG: %w", err)
		}
		return buf.Bytes(), nil
	case SRGB:
		img := image.NewRGBA(image.Rect(0, 0, p.Header.Width, p.Header.Height))
		for y := 0; y < p.Header.Height; y++ {
			row := p.Row(y)
			off := y * img.Stride
			for x := 0; x < p.Header.Width; x++ {
				img.Pix[off+x*4+0] = row[x*3+0]
				img.Pix[off+x*4+1] = row[x*3+1]
				img.Pix[off+x*4+2] = row[x*3+2]
				img.Pix[off+x*4+3] = 0xff
			}
		}
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 92}); err != nil {
			return nil, fmt.Errorf("render: encoding page JPEG: %w", err)
		}
		return buf.Bytes(), nil
	default:
		return nil, fmt.Errorf("render: unsupported page color space %d", p.Header.ColorSpace)
	}
}
