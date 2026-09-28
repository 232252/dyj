//go:build windows

// wingdi_windows.go is the production Windows backend: it draws decoded
// pages onto a printer device context, so the vendor driver already
// installed in the Windows spooler does the printer-language encoding
// (ADR-0002 D3 "转发给已有打印队列" — dyj never talks printer languages
// itself and never needs its own driver stack).
//
// Flow per document:
//
//	OpenPrinterW → DocumentPropertiesW(DM_OUT_BUFFER) → patch paper
//	size / copies / orientation → CreateDCW("WINSPOOL", ..., devmode)
//	→ StartDoc → { StartPage → StretchDIBits → EndPage }* → EndDoc.
//
// The DEVMODE is patched by byte offset (dmFields/dmOrientation/
// dmPaperSize/dmCopies) rather than by redeclaring the whole struct:
// DocumentPropertiesW hands us a correctly sized, driver-initialized
// buffer, and patching four fields cannot drift from the real layout.
package driver

import (
	"context"
	"fmt"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/sq199/dyj/pkg/render"
)

// DEVMODE field offsets (x64 and x86 agree here: everything below
// dmFields is 2-byte-aligned until dmCopies).
const (
	devModeOffsetFields     = 72 // DWORD dmFields
	devModeOffsetOrientation = 76 // SHORT dmOrientation
	devModeOffsetPaperSize   = 78 // SHORT dmPaperSize
	devModeOffsetCopies      = 86 // SHORT dmCopies

	dmOrientation  = 0x00000001
	dmPaperSize    = 0x00000002
	dmCopies       = 0x00000100

	dmOrientPortrait  = 1
	dmOrientLandscape = 2
)

// DMPAPER codes for the media set we advertise.
var paperCodes = map[string]uint16{
	"iso_a4":          9,  // DMPAPER_A4
	"na_letter":       1,  // DMPAPER_LETTER
	"na_legal":        5,  // DMPAPER_LEGAL
	"iso_a5":          11, // DMPAPER_A5
	"na_index-4x6":    70, // DMPAPER_4X6
}

// WingDI prints onto a Windows spooler queue by name.
type WingDI struct {
	// Printer is the queue name as shown by `Get-Printer` in
	// PowerShell, e.g. "EPSONC42E48 (K200)".
	Printer string
}

// Name implements Interface.
func (w *WingDI) Name() string { return "wingdi:" + w.Printer }

var (
	spool32 = windows.NewLazySystemDLL("winspool.drv")
	procOpenPrinterW           = spool32.NewProc("OpenPrinterW")
	procClosePrinter           = spool32.NewProc("ClosePrinter")
	procDocumentPropertiesW    = spool32.NewProc("DocumentPropertiesW")
	gdi32                      = windows.NewLazySystemDLL("gdi32.dll")
	procCreateDCW              = gdi32.NewProc("CreateDCW")
	procDeleteDC               = gdi32.NewProc("DeleteDC")
	procGetDeviceCaps          = gdi32.NewProc("GetDeviceCaps")
	procStartDocW              = gdi32.NewProc("StartDocW")
	procStartPage              = gdi32.NewProc("StartPage")
	procEndPage                = gdi32.NewProc("EndPage")
	procEndDoc                 = gdi32.NewProc("EndDoc")
	procStretchDIBits          = gdi32.NewProc("StretchDIBits")
)

const (
	logPixelsX = 88
	logPixelsY = 90

	dmOutBuffer = 2
	dmInBuffer  = 1

	dibRGBColors = 0
	ropSRCCOPY   = 0x00CC0020

	biRGB = 0
)

// Print implements Interface.
func (w *WingDI) Print(ctx context.Context, job PrintJob, doc Document) error {
	ph, err := w.openPrinter()
	if err != nil {
		return err
	}
	defer procClosePrinter.Call(uintptr(ph))

	dm, err := w.devMode(ph)
	if err != nil {
		return err
	}
	patchDevMode(dm, job)

	hdc, _, _ := procCreateDCW.Call(
		uintptr(unsafe.Pointer(utf16Ptr("WINSPOOL"))),
		uintptr(unsafe.Pointer(utf16Ptr(w.Printer))),
		0,
		uintptr(unsafe.Pointer(&dm[0])),
	)
	if hdc == 0 {
		return fmt.Errorf("driver: CreateDC for printer %q failed", w.Printer)
	}
	defer procDeleteDC.Call(hdc)

	px, _, _ := procGetDeviceCaps.Call(hdc, logPixelsX)
	py, _, _ := procGetDeviceCaps.Call(hdc, logPixelsY)
	px, py = normalizeDPI(px), normalizeDPI(py)

	docName := utf16Ptr(job.Name)
	di := struct {
		cbSize       uint32
		lpszDocName  *uint16
		lpszOutput   *uint16
		lpszDatatype *uint16
		fwType       uint32
	}{cbSize: 20, lpszDocName: docName}
	if r, _, _ := procStartDocW.Call(hdc, uintptr(unsafe.Pointer(&di))); int32(r) <= 0 {
		return fmt.Errorf("driver: StartDoc failed for %q", job.Name)
	}
	abort := func(err error) error {
		procEndDoc.Call(hdc)
		return err
	}

	pages, errc := Pages(ctx, doc, DecoderOptions{Media: job.Media, Grayscale: !job.Color})
	n := 0
	for p := range pages {
		n++
		if ctx.Err() != nil {
			return abort(fmt.Errorf("driver: job canceled after %d pages", n))
		}
		if err := w.printPage(hdc, px, py, p); err != nil {
			return abort(err)
		}
	}
	if err := <-errc; err != nil {
		return abort(err)
	}
	if r, _, _ := procEndDoc.Call(hdc); int32(r) <= 0 {
		return fmt.Errorf("driver: EndDoc failed")
	}
	return nil
}

// printPage blits one raster page. Destination size preserves physical
// inches: page px × printer dpi ÷ page dpi.
func (w *WingDI) printPage(hdc, pdpiX, pdpiY uintptr, p *render.Page) error {
	width := p.Header.Width
	height := p.Header.Height
	bpp := p.Header.ColorSpace.BytesPerPixel() * 8
	srcStride := p.Header.Stride()

	// DIB rows are DWORD-aligned.
	dibStride := (width*bpp/8 + 3) &^ 3
	bits := make([]byte, dibStride*height)
	for y := 0; y < height; y++ {
		copy(bits[y*dibStride:y*dibStride+srcStride], p.Row(y))
	}

	// BITMAPINFOHEADER (40 bytes) + 256-entry palette for 8 bpp.
	biSize := 40
	if bpp == 8 {
		biSize += 256 * 4
	}
	info := make([]byte, biSize)
	putLE32(info[0:], 40)                                // biSize
	putLE32(info[4:], uint32(width))                     // biWidth
	putLE32(info[8:], uint32(-height))                   // biHeight (negative = top-down)
	putLE16(info[12:], 1)                                // biPlanes
	putLE16(info[14:], uint16(bpp))                      // biBitCount
	putLE32(info[16:], biRGB)                            // biCompression
	putLE32(info[20:], uint32(dibStride*height))         // biSizeImage
	if bpp == 8 {
		for i := 0; i < 256; i++ {
			off := 40 + i*4
			info[off], info[off+1], info[off+2] = byte(i), byte(i), byte(i)
		}
	}

	destW := int64(width) * int64(pdpiX) / int64(maxInt64(int64(p.Header.XDPI), 1))
	destH := int64(height) * int64(pdpiY) / int64(maxInt64(int64(p.Header.YDPI), 1))

	if r, _, err := procStartPage.Call(hdc); int32(r) <= 0 {
		return fmt.Errorf("driver: StartPage failed: %v", err)
	}
	ok, _, err := procStretchDIBits.Call(
		hdc,
		0, 0, uintptr(destW), uintptr(destH),
		0, 0, uintptr(width), uintptr(height),
		uintptr(unsafe.Pointer(&bits[0])),
		uintptr(unsafe.Pointer(&info[0])),
		dibRGBColors, ropSRCCOPY,
	)
	if int32(ok) != int32(height) {
		procEndPage.Call(hdc)
		return fmt.Errorf("driver: StretchDIBits failed: %v", err)
	}
	if r, _, err := procEndPage.Call(hdc); int32(r) <= 0 {
		return fmt.Errorf("driver: EndPage failed: %v", err)
	}
	return nil
}

func putLE32(b []byte, v uint32) {
	b[0], b[1], b[2], b[3] = byte(v), byte(v>>8), byte(v>>16), byte(v>>24)
}

func putLE16(b []byte, v uint16) {
	b[0], b[1] = byte(v), byte(v>>8)
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func (w *WingDI) openPrinter() (syscall.Handle, error) {
	name := utf16Ptr(w.Printer)
	var h syscall.Handle
	r, _, err := procOpenPrinterW.Call(
		uintptr(unsafe.Pointer(name)),
		uintptr(unsafe.Pointer(&h)),
		0,
	)
	if r == 0 {
		return 0, fmt.Errorf("driver: OpenPrinter %q: %v", w.Printer, err)
	}
	return h, nil
}

// devMode fetches the driver-initialized DEVMODE for the printer.
func (w *WingDI) devMode(h syscall.Handle) ([]byte, error) {
	name := utf16Ptr(w.Printer)
	size, _, _ := procDocumentPropertiesW.Call(
		0, uintptr(h), uintptr(unsafe.Pointer(name)), 0, 0, 0,
	)
	if size <= 0 {
		return nil, fmt.Errorf("driver: DocumentProperties(size) failed for %q", w.Printer)
	}
	buf := make([]byte, size)
	r, _, _ := procDocumentPropertiesW.Call(
		0, uintptr(h), uintptr(unsafe.Pointer(name)),
		uintptr(unsafe.Pointer(&buf[0])), 0, dmOutBuffer,
	)
	if int32(r) < 0 {
		return nil, fmt.Errorf("driver: DocumentProperties(fetch) failed for %q", w.Printer)
	}
	return buf, nil
}

// patchDevMode applies the job's paper size, copies and orientation.
func patchDevMode(dm []byte, job PrintJob) {
	fields := le32(dm[devModeOffsetFields:])
	if job.Media != "" {
		if code, ok := matchPaper(job.Media); ok {
			putLE(dm[devModeOffsetPaperSize:], uint16(code))
			fields |= dmPaperSize
		}
	}
	if job.Copies > 1 && job.Copies <= 999 {
		putLE(dm[devModeOffsetCopies:], uint16(job.Copies))
		fields |= dmCopies
	}
	_ = fields // orientation stays portrait v1
	dm[devModeOffsetFields] = byte(fields)
	dm[devModeOffsetFields+1] = byte(fields >> 8)
	dm[devModeOffsetFields+2] = byte(fields >> 16)
	dm[devModeOffsetFields+3] = byte(fields >> 24)
}

func matchPaper(media string) (uint16, bool) {
	m := media
	for i := 0; i < len(m); i++ {
		if m[i] == '_' && i+1 < len(m) && isDigit(m[i+1]) {
			m = m[:i] // cut the size segment: iso_a4_210x297mm → iso_a4
			break
		}
	}
	if code, ok := paperCodes[m]; ok {
		return code, true
	}
	return 0, false
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func le32(b []byte) uint32 {
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
}

func putLE(b []byte, v uint16) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
}

func normalizeDPI(v uintptr) uintptr {
	if v == 0 {
		return 300
	}
	return v
}

func utf16Ptr(s string) *uint16 {
	p, _ := syscall.UTF16PtrFromString(s)
	return p
}
