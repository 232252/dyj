package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"

	"github.com/sq199/dyj/pkg/service"
)

// cmdService manages the OS service integration.
//
//	install  — parse serve flags, persist them, register the service
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
		raw, err := json.Marshal(o)
		if err != nil {
			return err
		}
		if err := service.SaveOptions(string(raw)); err != nil {
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

// parseServeOptions reuses the serve flag set without running.
func parseServeOptions(args []string) (serveOptions, error) {
	var o serveOptions
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.IntVar(&o.Port, "port", 6631, "IPP port")
	fs.IntVar(&o.TLSPort, "tls-port", 0, "ipps port")
	fs.StringVar(&o.Name, "name", "", "printer name")
	fs.StringVar(&o.Printer, "printer", "", "Windows print queue")
	fs.StringVar(&o.Backend, "backend", "", "backend")
	fs.StringVar(&o.Out, "out", "", "file backend output dir")
	fs.StringVar(&o.Spool, "spool", "", "spool dir")
	fs.StringVar(&o.Ifaces, "iface", "", "interfaces")
	fs.BoolVar(&o.NoMDNS, "no-mdns", false, "no mDNS")
	fs.BoolVar(&o.Verbose, "v", false, "verbose")
	fs.StringVar(&o.Hostname, "hostname", "", "mDNS host name")
	if err := fs.Parse(args); err != nil {
		return o, err
	}
	if o.Name == "" {
		o.Name = "dyj on " + hostnameOr("localhost")
	}
	return o, nil
}
