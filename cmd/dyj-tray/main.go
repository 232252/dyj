//go:build windows

// Command dyj-tray is the shipping Windows tray executable, built with
// -ldflags="-H=windowsgui" so launching it at login shows no console
// window. It is a thin shim: resolve the admin port from the persisted
// service options and hand over to pkg/tray. The print service itself
// is unaffected by this process.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/sq199/dyj/pkg/service"
	"github.com/sq199/dyj/pkg/tray"
)

func main() {
	port := 6631
	if raw, err := service.LoadOptions(); err == nil {
		var o struct {
			Port int `json:"port"`
		}
		if json.Unmarshal([]byte(raw), &o) == nil && o.Port > 0 {
			port = o.Port
		}
	}
	exe, err := os.Executable()
	if err != nil {
		os.Exit(1)
	}
	if err := tray.Run(tray.Options{
		AdminURL: fmt.Sprintf("http://127.0.0.1:%d", port),
		ExePath:  exe,
	}); err != nil {
		// windowsgui has no console; a nonzero exit is all we can do.
		os.Exit(1)
	}
}
