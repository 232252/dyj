//go:build windows

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/sq199/dyj/pkg/service"
	"github.com/sq199/dyj/pkg/tray"
)

// cmdTray runs the tray inside the console binary — the developer and
// power-user path. The shipping autostart path is the separate
// windowsgui binary dyj-tray.exe (no console flash at login); the Run
// key therefore points at whichever executable is running when the
// user flips the toggle.
func cmdTray(args []string) error {
	fs := flag.NewFlagSet("tray", flag.ContinueOnError)
	port := fs.Int("port", 0, "admin API port (default: persisted service options, else 6631)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *port == 0 {
		if raw, err := service.LoadOptions(); err == nil {
			var o serveOptions
			if json.Unmarshal([]byte(raw), &o) == nil && o.Port > 0 {
				*port = o.Port
			}
		}
	}
	if *port == 0 {
		*port = 6631
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return tray.Run(tray.Options{
		AdminURL: fmt.Sprintf("http://127.0.0.1:%d", *port),
		ExePath:  exe,
	})
}
