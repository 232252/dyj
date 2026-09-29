package render

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"testing"
)

// makeTestPage builds a small deterministic raster page.
func makeTestPage(t *testing.T, cs ColorSpace, w, h, dpi int) *Page {
	t.Helper()
	p := &Page{
		Header: PageHeader{
			Width: w, Height: h, XDPI: dpi, YDPI: dpi,
			ColorSpace:     cs,
			TotalPageCount: 2,
			MediaName:      "iso_a4_210x297mm",
		},
		Pix: make([]byte, h*cs.BytesPerPixel()*w),
	}
	for y := 0; y < h; y++ {
		row := p.Row(y)
		for x := 0; x < w; x++ {
			if cs == SGray {
				row[x] = byte((x + y) % 251)
			} else {
				row[x*3+0] = byte(x % 251)
				row[x*3+1] = byte(y % 241)
				row[x*3+2] = byte((x + y) % 233)
			}
		}
	}
	return p
}

// findObj returns the object body between "N 0 obj" and the matching
// "endobj".
func findObj(t *testing.T, pdf []byte, num int) string {
	t.Helper()
	start := strings.Index(string(pdf), fmt.Sprintf("\n%d 0 obj\n", num))
	if start < 0 {
		t.Fatalf("object %d not found", num)
	}
	end := strings.Index(string(pdf[start:]), "\nendobj\n")
	return string(pdf[start : start+end])
}

func TestPagesToPDFStructure(t *testing.T) {
	pages := []*Page{
		makeTestPage(t, SGray, 64, 32, 72),
		makeTestPage(t, SRGB, 48, 48, 144),
	}
	pdf, err := PagesToPDF(pages)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(pdf, []byte("%PDF-1.4\n")) {
		t.Fatalf("missing PDF header: %q", pdf[:20])
	}
	if !bytes.Contains(pdf, []byte("%%EOF")) {
		t.Fatal("missing EOF marker")
	}

	// Catalog and page tree.
	if cat := findObj(t, pdf, 1); !strings.Contains(cat, "/Type /Catalog") || !strings.Contains(cat, "/Pages 2 0 R") {
		t.Errorf("bad catalog: %s", cat)
	}
	if tree := findObj(t, pdf, 2); !strings.Contains(tree, "/Count 2") || !strings.Contains(tree, "4 0 R 7 0 R") {
		t.Errorf("bad pages tree: %s", tree)
	}

	// Page 1: gray image, 64x32 pt MediaBox at 72 dpi.
	if img := findObj(t, pdf, 3); !strings.Contains(img, "/Subtype /Image") || !strings.Contains(img, "/DeviceGray") ||
		!strings.Contains(img, "/Filter /DCTDecode") || !strings.Contains(img, "/Width 64") || !strings.Contains(img, "/Height 32") {
		t.Errorf("bad image object 3: %s", img)
	}
	if pg := findObj(t, pdf, 4); !strings.Contains(pg, "/MediaBox [0 0 64.00 32.00]") || !strings.Contains(pg, "/Contents 5 0 R") {
		t.Errorf("bad page object 4: %s", pg)
	}
	// Content stream: image fills the page; /Length matches the body.
	content := findObj(t, pdf, 5)
	if !strings.Contains(content, "q 64 0 0 32 0 0 cm /Im0 Do Q") {
		t.Errorf("bad content stream 5: %s", content)
	}
	verifyStreamLength(t, content)

	// Page 2: RGB, 48x48 px at 144 dpi = 24x24 pt.
	if img := findObj(t, pdf, 6); !strings.Contains(img, "/DeviceRGB") {
		t.Errorf("page 2 image should be DeviceRGB: %s", img)
	}
	if pg := findObj(t, pdf, 7); !strings.Contains(pg, "/MediaBox [0 0 24.00 24.00]") {
		t.Errorf("page 2 MediaBox wrong: %s", pg)
	}
}

func verifyStreamLength(t *testing.T, obj string) {
	t.Helper()
	i := strings.Index(obj, "/Length ")
	if i < 0 {
		t.Fatal("object has no /Length")
	}
	rest := obj[i+len("/Length "):]
	n, err := strconv.Atoi(rest[:strings.IndexByte(rest, ' ')])
	if err != nil {
		t.Fatalf("bad /Length: %v", err)
	}
	s := strings.Index(obj, "stream\n") + len("stream\n")
	if got := len(obj[s : s+n]); got != n {
		t.Errorf("/Length %d but stream body is %d bytes", n, got)
	}
}

func TestPagesToPDFXrefOffsets(t *testing.T) {
	pdf, err := PagesToPDF([]*Page{
		makeTestPage(t, SRGB, 32, 32, 72),
		makeTestPage(t, SGray, 16, 16, 72),
	})
	if err != nil {
		t.Fatal(err)
	}

	// startxref must point exactly at the xref keyword.
	i := strings.LastIndex(string(pdf), "startxref\n")
	tail := string(pdf[i+len("startxref\n"):])
	tail = tail[:strings.Index(tail, "\n%%EOF")]
	num, err := strconv.Atoi(strings.TrimSpace(tail))
	if err != nil {
		t.Fatalf("bad startxref: %v", err)
	}
	if string(pdf[num:num+5]) != "xref\n" {
		t.Fatalf("startxref %d does not point at xref table (got %q)", num, pdf[num:num+12])
	}

	// Every in-use xref row must point at "<objnum> 0 obj", with rows in
	// object-number order (1..N).
	rows := strings.Split(strings.TrimSpace(string(pdf[num:])), "\n")
	objNum := 0
	for _, row := range rows[2:] { // skip "xref" and "0 N"
		fields := strings.Fields(row)
		if len(fields) != 3 {
			break // trailer reached
		}
		if fields[1] == "65535" {
			continue // the object-0 free entry
		}
		objNum++
		off, err := strconv.Atoi(fields[0])
		if err != nil {
			t.Fatalf("bad xref offset %q", row)
		}
		want := fmt.Sprintf("%d 0 obj", objNum)
		if !strings.HasPrefix(string(pdf[off:]), want) {
			t.Errorf("xref row %q → offset %d points at %q, want %q", row, off, pdf[off:off+12], want)
		}
	}
	if objNum != 8 { // 2 + 3 per page
		t.Errorf("validated %d xref rows, want 8", objNum)
	}
}

func TestPagesToPDFEmpty(t *testing.T) {
	if _, err := PagesToPDF(nil); err == nil {
		t.Fatal("empty page list should error")
	}
}
