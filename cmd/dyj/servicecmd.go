package main

import (
	"errors"
	"fmt"

	"github.com/232252/dyj/pkg/service"
)

// cmdService manages the OS service integration.
//
//	install  — persist the serve flags (none = bootstrap role: the node
//	           comes up unconfigured and the wizard completes it), then
//	           register the service
//	uninstall/start/stop — operate the registered service
//	run      — the entry the SCM/systemd/launchd invokes (loads persisted
//	           options); exposed for debugging
func cmdService(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: dyj service install|uninstall|start|stop|run [-- <serve flags>]")
	}
	switch args[0] {
	case "install":
		rest := args[1:]
		// Accept both `service install -port N` and
		// `service install -- -port N`.
		if len(rest) > 0 && rest[0] == "--" {
			rest = rest[1:]
		}
		o, err := parseServeOptions(rest)
		if err != nil {
			return err
		}
		raw, err := marshalOptions(o)
		if err != nil {
			return err
		}
		// No explicit flags = installer/upgrade path: keep the user's
		// existing configuration (printers, always-on open printing).
		// Only a first install (or a corrupt file) gets the defaults.
		if len(rest) == 0 {
			kept, kerr := service.SaveOptionsIfAbsent(raw)
			if kerr != nil {
				return kerr
			}
			if kept {
				raw, _ = service.LoadOptions()
			}
		} else if err := service.SaveOptions(raw); err != nil {
			return err
		}
		if err := service.Install(); err != nil {
			return err
		}
		fmt.Printf("dyj: service installed (options: %s)\n", raw)
		return nil
	case "uninstall":
		return service.Uninstall()
	case "start":
		return service.Start()
	case "stop":
		return service.Stop()
	case "run":
		return service.RunForeground(serveFromJSON)
	default:
		return fmt.Errorf("unknown service action %q", args[0])
	}
}
