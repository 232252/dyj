// Package winver guards against being started on Windows versions below
// the supported floor. The modern build (go 1.27) requires Windows 10+
// at the RUNTIME level — it crashes before main() on Win7 — so for it
// this guard is unreachable-but-harmless. Its real audience is the
// Win7 legacy build (scripts/build-legacy-windows.sh, go1.20.14, floor
// Win7 SP1): an XP/Vista box running that build would otherwise die
// with a cryptic runtime crash instead of a readable message.
package winver

import (
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

var modntdll = windows.NewLazySystemDLL("ntdll.dll")
var procRtlGetVersion = modntdll.NewProc("RtlGetVersion")

// osVersionInfoExW mirrors RTL_OSVERSIONINFOW; RtlGetVersion (unlike
// GetVersionEx) ignores the app-manifest compatibility lie.
type osVersionInfoExW struct {
	dwOSVersionInfoSize uint32
	dwMajorVersion      uint32
	dwMinorVersion      uint32
	dwBuildNumber       uint32
	dwPlatformId        uint32
	szCSDVersion        [128]uint16
	wServicePackMajor   uint16
	wServicePackMinor   uint16
	wSuiteMask          uint16
	wProductType        uint8
	wReserved           uint8
}

// Guard exits with a plain-language message when the OS is below
// Windows 7 SP1 (6.1 SP1). A failed RtlGetVersion call is treated as
// pass-through: refusing to start over a broken version probe would be
// worse than starting on an unknown old build.
func Guard() {
	var vi osVersionInfoExW
	vi.dwOSVersionInfoSize = uint32(unsafe.Sizeof(vi))
	r1, _, _ := procRtlGetVersion.Call(uintptr(unsafe.Pointer(&vi)))
	if r1 != 0 { // nonzero NTSTATUS = probe failed; don't block startup
		return
	}
	if vi.dwMajorVersion < 6 ||
		(vi.dwMajorVersion == 6 && vi.dwMinorVersion < 1) ||
		(vi.dwMajorVersion == 6 && vi.dwMinorVersion == 1 && vi.wServicePackMajor < 1) {
		fmt.Fprintf(os.Stderr,
			"dyj: 需要 Windows 7 SP1 或更高版本 (当前 %d.%d SP%d); XP/Vista 不受支持\n",
			vi.dwMajorVersion, vi.dwMinorVersion, vi.wServicePackMajor)
		os.Exit(1)
	}
}
