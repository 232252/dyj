package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/232252/dyj/pkg/doctor"
	"github.com/232252/dyj/pkg/sysqueue"
)

// cmdDoctor runs the diagnostics and prints them human-readable
// (default) or as JSON (--json). Exit code stays 0 for warn/info —
// only "fail" (something is broken on this machine) fails the run.
func cmdDoctor(args []string) error {
	var o serveOptions
	asJSON := false
	asClient := false
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.IntVar(&o.Port, "port", 6631, "IPP port to check")
	fs.IntVar(&o.TLSPort, "tls-port", 0, "ipps port")
	fs.BoolVar(&o.NoMDNS, "no-mdns", false, "skip mDNS checks")
	fs.StringVar(&o.Backend, "backend", "", "backend to probe (wingdi|cups|file|null)")
	fs.StringVar(&o.Printer, "printer", "", "queue name")
	fs.StringVar(&o.Spool, "spool", "", "state dir")
	fs.BoolVar(&asClient, "client", false, "check the added network printers instead of the share side")
	fs.BoolVar(&asJSON, "json", false, "machine-readable output")
	if err := fs.Parse(args); err != nil {
		return err
	}

	var checks []doctor.Check
	if asClient {
		stateDir := o.Spool
		if stateDir == "" {
			stateDir = defaultStateDir()
		}
		checks = runClientChecks(stateDir)
	} else {
		checks = doctor.Run(doctorParams(o, o.Spool, false))
	}
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
	for _, spec := range o.Printers {
		p.MDNSInstances = append(p.MDNSInstances, spec.Name)
	}
	return p
}

// runClientChecks diagnoses the RECEIVER side (research/13 §5.4): every
// added printer is checked for queue presence, server reachability and
// pairing-token validity (a 401 means the share side revoked us).
func runClientChecks(stateDir string) []doctor.Check {
	var out []doctor.Check
	data, err := os.ReadFile(filepath.Join(stateDir, "added-printers.json"))
	if err != nil {
		return []doctor.Check{{Name: "client", Level: doctor.LevelInfo,
			Detail: "本机没有添加过网络打印机（added-printers.json 不存在）"}}
	}
	var added []addedPrinter
	if err := json.Unmarshal(data, &added); err != nil || len(added) == 0 {
		return []doctor.Check{{Name: "client", Level: doctor.LevelInfo,
			Detail: "本机没有添加过网络打印机"}}
	}
	for _, a := range added {
		// queue exists?
		if ok, err := sysqueue.Exists(a.QueueName); err != nil {
			out = append(out, doctor.Check{Name: "client-queue-exists", Level: doctor.LevelWarn,
				Detail: fmt.Sprintf("无法查询系统队列 %s: %v", a.QueueName, err)})
		} else if !ok {
			out = append(out, doctor.Check{Name: "client-queue-exists", Level: doctor.LevelFail,
				Detail: fmt.Sprintf("系统队列 %s 不存在（可能被手动删除）", a.QueueName),
				Fix:    "在 dyj 管理页 → 添加打印机 重新安装"})
			continue
		} else {
			out = append(out, doctor.Check{Name: "client-queue-exists", Level: doctor.LevelOK,
				Detail: fmt.Sprintf("系统队列 %s 存在", a.QueueName)})
		}
		// Application-layer reachability. A TCP dial is not evidence: mesh
		// overlays complete the handshake on ports with no listener
		// (research/12 §4) and would mark a dead share side "可达".
		if a.ServerHost == "" || a.ServerPort <= 0 {
			continue
		}
		pr := probeIPP(a.ServerHost, a.ServerPort, a.URIPath, 2*time.Second)
		if !pr.Alive {
			out = append(out, doctor.Check{Name: "client-uri-reachable", Level: doctor.LevelFail,
				Detail: fmt.Sprintf("共享端 %s:%d 没有响应打印请求（对方关机、地址变了，或中间网络只完成了空连接）", a.ServerHost, a.ServerPort),
				Fix:    "打开添加页会自动重试以前添加过的地址；仍没有的话，用「按地址添加」填现在的地址"})
			continue
		}
		out = append(out, doctor.Check{Name: "client-uri-reachable", Level: doctor.LevelOK,
			Detail: fmt.Sprintf("共享端 %s:%d 能响应打印请求", a.ServerHost, a.ServerPort)})
		if a.URIPath == "" {
			continue
		}
		if pr.Revoked {
			out = append(out, doctor.Check{Name: "client-token-valid", Level: doctor.LevelFail,
				Detail: "配对已被共享端撤销（或服务端重置），打印会失败",
				Fix:    "在共享端管理页查看新配对码，本机重新配对并重装队列"})
		} else {
			out = append(out, doctor.Check{Name: "client-token-valid", Level: doctor.LevelOK,
				Detail: "配对仍然有效"})
		}
	}
	return out
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
