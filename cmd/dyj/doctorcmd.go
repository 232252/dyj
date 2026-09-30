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

	checks := doctorParams(o, o.Spool, false)
	if asJSON {
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"checks": doctor.Run(checks)})
	}
	icon := map[string]string{
		doctor.LevelOK: "[ ok ]", doctor.LevelWarn: "[警告]", doctor.LevelFail: "[失败]", doctor.LevelInfo: "[说明]",
	}
	failed := false
	for _, c := range doctor.Run(checks) {
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

// doctorParams derives the doctor input from the v2 options (first
// printer carries the backend probe; state dir resolution shared).
func doctorParams(o serveOptions, stateDir string, inProc bool) doctor.Params {
	if stateDir == "" {
		stateDir = defaultStateDir()
	}
	p := doctor.Params{
		Port:     o.Port,
		TLSPort:  o.TLSPort,
		MDNS:     !o.NoMDNS,
		StateDir: stateDir,
		InProc:   inProc,
	}
	if len(o.Printers) > 0 {
		spec := o.Printers[0]
		p.Backend = spec.Backend
		p.Printer = spec.Queue
	}
	if p.Backend == "" {
		p.Backend = o.Backend
	}
	return p
}

// parseServeOptionsJSON validates an options JSON coming from the admin
// API (or the persisted service file) before it may replace the live
// configuration.
func parseServeOptionsJSON(raw []byte) (serveOptions, error) {
	var o serveOptions
	if err := json.Unmarshal(raw, &o); err != nil {
		return o, fmt.Errorf("配置 JSON 无效: %w", err)
	}
	if o.Port < 0 || o.Port > 65535 {
		return o, fmt.Errorf("端口 %d 超出范围（0-65535，0=默认 6631）", o.Port)
	}
	if o.TLSPort < 0 || o.TLSPort > 65535 || (o.TLSPort != 0 && o.TLSPort == o.Port) {
		return o, fmt.Errorf("TLS 端口 %d 无效（需 0-65535 且不与 IPP 端口相同）", o.TLSPort)
	}
	if !validRole(o.Role) {
		return o, fmt.Errorf("未知角色 %q（share|use|both 或空）", o.Role)
	}
	for i, p := range o.Printers {
		switch p.Backend {
		case "", "wingdi", "cups", "file", "null":
		default:
			return o, fmt.Errorf("打印机 %d 后端未知 %q（wingdi|cups|file|null）", i+1, p.Backend)
		}
		if p.Backend == "" && o.Backend != "" {
			switch o.Backend {
			case "wingdi", "cups", "file", "null":
			default:
				return o, fmt.Errorf("默认后端未知 %q（wingdi|cups|file|null）", o.Backend)
			}
		}
	}
	return normalizeServeOptions(o), nil
}
