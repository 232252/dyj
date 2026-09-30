//go:build windows

package localprint

import (
	"fmt"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	winspoolDLL           = windows.NewLazySystemDLL("winspool.drv")
	procEnumPrintersW     = winspoolDLL.NewProc("EnumPrintersW")
	procGetDefaultPrinter = winspoolDLL.NewProc("GetDefaultPrinterW")
	procOpenPrinterW      = winspoolDLL.NewProc("OpenPrinterW")
	procClosePrinter      = winspoolDLL.NewProc("ClosePrinter")
)

const (
	printerEnumLocal       = 0x00000002
	printerEnumConnections = 0x00000004
)

// printerInfo2W mirrors PRINTER_INFO_2W: 13 string pointers followed by 8
// DWORDs (136 bytes on 64-bit, no implicit padding). Pointer fields are
// unsafe.Pointer (not uintptr) so reading them stays vet-clean; the struct
// lives inside our own byte buffer for the duration of the call.
type printerInfo2W struct {
	pServerName, pPrinterName, pShareName, pPortName, pDriverName,
	pComment, pLocation, pDevMode, pSepFile, pPrintProcessor,
	pDatatype, pParameters, pSecurityDescriptor unsafe.Pointer

	Attributes, Priority, DefaultTime, StartTime, UntilTime, Status, Jobs, AveragePPM uint32
}

// utf16At reads the UTF-16 string a PRINTER_INFO_2W pointer field points to.
func utf16At(p unsafe.Pointer) string {
	if p == nil {
		return ""
	}
	return windows.UTF16PtrToString((*uint16)(p))
}

// List enumerates local queues and per-machine connections via
// EnumPrintersW level 2 (queue name, driver, port) plus the default marker.
func List() ([]Printer, error) {
	var needed, returned uint32
	flags := uintptr(printerEnumLocal | printerEnumConnections)
	var buf []byte
	for attempt := 0; attempt < 3; attempt++ {
		_, _, _ = procEnumPrintersW.Call(
			flags, 0, 2,
			0, 0,
			uintptr(unsafe.Pointer(&needed)), uintptr(unsafe.Pointer(&returned)))
		if needed == 0 {
			return nil, nil
		}
		buf = make([]byte, needed)
		r1, _, callErr := procEnumPrintersW.Call(
			flags, 0, 2,
			uintptr(unsafe.Pointer(&buf[0])), uintptr(needed),
			uintptr(unsafe.Pointer(&needed)), uintptr(unsafe.Pointer(&returned)))
		if r1 != 0 {
			break
		}
		if attempt == 2 {
			return nil, fmt.Errorf("EnumPrintersW: %w", callErr)
		}
		// Another queue appeared mid-call; retry with the new size.
	}
	def := defaultPrinterName()
	infos := (*[256]printerInfo2W)(unsafe.Pointer(&buf[0]))[:returned:returned]
	out := make([]Printer, 0, returned)
	for i := range infos {
		p := Printer{
			Name:    utf16At(infos[i].pPrinterName),
			Driver:  utf16At(infos[i].pDriverName),
			Port:    utf16At(infos[i].pPortName),
			Default: utf16At(infos[i].pPrinterName) == def,
		}
		p.Virtual = IsVirtual(p.Name, p.Driver, p.Port)
		out = append(out, p)
	}
	return out, nil
}

// Probe opens (and immediately closes) a printer handle: the same gate
// wingdi passes through at print time.
func Probe(name string) error {
	pn, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return fmt.Errorf("queue name: %w", err)
	}
	var handle syscall.Handle
	// OpenPrinterW(pPrinterName, phPrinter, pDefault) — the name comes
	// FIRST; swapping the args hands the API a null name and crashes the
	// process (caught by the live wizard test).
	r1, _, callErr := procOpenPrinterW.Call(uintptr(unsafe.Pointer(pn)), uintptr(unsafe.Pointer(&handle)), 0)
	if r1 == 0 {
		return fmt.Errorf("OpenPrinter(%s): %w", name, callErr)
	}
	_, _, _ = procClosePrinter.Call(uintptr(handle))
	return nil
}

func defaultPrinterName() string {
	var n uint32
	_, _, _ = procGetDefaultPrinter.Call(0, uintptr(unsafe.Pointer(&n)))
	if n == 0 {
		return ""
	}
	b := make([]uint16, n)
	r1, _, _ := procGetDefaultPrinter.Call(uintptr(unsafe.Pointer(&b[0])), uintptr(unsafe.Pointer(&n)))
	if r1 == 0 {
		return ""
	}
	return windows.UTF16ToString(b[:n])
}
