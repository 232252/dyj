package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/sq199/dyj/pkg/doctor"
)

// cmdDoctor runs the diagnostics and prints them human-readable
// (default) or as JSON (--json). Exit code stays 0 for warn/info —
// only "fail" (something is broken on this machine) fails the run.
func cmdDoctor(args []string) error {
	var o serveOptions
	asJSON := false
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.IntVar(&o.Port, "port", 6631, "IPP port to check")
	fs.IntVar(&o.TLSPort, "tls-port", 0, "ipps port")
	fs.BoolVar(&o.NoMDNS, "no-mdns", false, "skip mDNS checks")
	fs.StringVar(&o.Backend, "backend", "", "backend to probe (wingdi|cups|file|null)")
	fs.StringVar(&o.Printer, "printer", "", "queue name")
	fs.StringVar(&o.Spool, "spool", "", "state dir")
	fs.BoolVar(&asJSON, "json", false, "machine-readable output")
	if err := fs.Parse(args); err != nil {
		return err
	}

	checks := doctor.Run(doctor.Params{
		Port:     o.Port,
		TLSPort:  o.TLSPort,
		MDNS:     !o.NoMDNS,
		StateDir: o.Spool,
		Backend:  o.Backend,
		Printer:  o.Printer,
	})
	if asJSON {
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"checks": checks})
	}
	icon := map[string]string{
		doctor.LevelOK: "[ ok ]", doctor.LevelWarn: "[警告]", doctor.LevelFail: "[失败]", doctor.LevelInfo: "[说明]",
	}
	failed := false
	for _, c := range checks {
		fmt.Printf("%s %-10s %s\n", icon[c.Level], c.Name, c.Detail)
		if c.Fix != "" {
			fmt.Printf("             └─ %s\n", c.Fix)
		}
		if c.Level == doctor.LevelFail {
			failed = true
		}
	}
	if failed {
		return errors.New("doctor: 存在失败项")
	}
	return nil
}

// runDiagnostics adapts doctor.Run to the admin API: in-process, so
// bind checks are reported from the server's point of view.
func runDiagnostics(o serveOptions) func(any) any {
	return func(any) any {
		stateDir := o.Spool
		if stateDir == "" {
			stateDir = defaultStateDir()
		}
		return doctor.Run(doctor.Params{
			Port:     o.Port,
			TLSPort:  o.TLSPort,
			MDNS:     !o.NoMDNS,
			StateDir: stateDir,
			Backend:  o.Backend,
			Printer:  o.Printer,
			InProc:   true,
		})
	}
}

// parseServeOptionsJSON validates an options JSON coming from the admin
// API before it may replace the persisted service configuration.
func parseServeOptionsJSON(raw []byte) (serveOptions, error) {
	var o serveOptions
	if err := json.Unmarshal(raw, &o); err != nil {
		return o, fmt.Errorf("配置 JSON 无效: %w", err)
	}
	if o.Port < 1 || o.Port > 65535 {
		return o, fmt.Errorf("端口 %d 超出范围（1-65535）", o.Port)
	}
	if o.TLSPort != 0 && (o.TLSPort < 1 || o.TLSPort > 65535 || o.TLSPort == o.Port) {
		return o, fmt.Errorf("TLS 端口 %d 无效（需 1-65535 且不与 IPP 端口相同）", o.TLSPort)
	}
	switch o.Backend {
	case "", "wingdi", "cups", "file", "null":
	default:
		return o, fmt.Errorf("未知后端 %q（wingdi|cups|file|null）", o.Backend)
	}
	if o.Name == "" {
		o.Name = "dyj on " + hostnameOr("localhost")
	}
	return o, nil
}
