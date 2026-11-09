// repair.go implements the admin self-repair endpoint (自修复): the
// doctor checks that set CanFix get one-click remediation. Everything
// here sits behind the loopback/token gate that admin.serveAPI enforces;
// the firewall fix may pop one UAC prompt (same contract as
// queue/install). `dyj doctor --fix` drives the same functions
// out-of-process via repairCtx.
package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/232252/dyj/pkg/doctor"
)

// repairOutcome is one executed repair's result. OK=false means the fix
// ran and failed (or could not run) — Detail says what to do next.
type repairOutcome struct {
	Check  string `json:"check"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
	// Skipped marks "nothing needed fixing"; the fix-all run filters
	// these so the UI only hears about real work.
	Skipped bool `json:"-"`
}

// repairCtx is what a fix needs, decoupled from the live node so the CLI
// can drive the same functions.
type repairCtx struct {
	cfg      serveOptions
	stateDir string
}

type repairFunc func(rc repairCtx) repairOutcome

// repairOrder keeps fix-all deterministic and the endpoint's unknown-name
// error message honest; registry keys must match doctor check names.
var (
	repairOrder = []string{"firewall", "tls", "state-dir", "pdf"}
	repairFuncs = map[string]repairFunc{
		"firewall":  repairFirewall,
		"tls":       repairTLS,
		"state-dir": repairStateDir,
		"pdf":       repairPDF,
	}
)

// apiRepair handles POST /admin/api/repair {"check":"firewall"|"tls"|
// "state-dir"|"pdf"|"all"}. Response: {"results":[repairOutcome...]}.
func (n *node) apiRepair(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var body struct {
		Check string `json:"check"`
	}
	if !readJSONBody(w, r, &body) {
		return
	}
	name := strings.TrimSpace(body.Check)
	rc := repairCtx{cfg: n.cfg, stateDir: n.stateDir}
	var results []repairOutcome
	switch name {
	case "all":
		for _, cn := range repairOrder {
			if res := repairFuncs[cn](rc); !res.Skipped {
				results = append(results, res)
			}
		}
	default:
		fn, ok := repairFuncs[name]
		if !ok {
			writeErr(w, http.StatusBadRequest, "未知或不可自动修复的检查项: "+name)
			return
		}
		results = append(results, fn(rc))
	}
	if len(results) == 0 {
		results = append(results, repairOutcome{Check: name, OK: true, Detail: "没有需要修复的项目"})
	}
	for _, res := range results {
		if res.OK {
			n.log.Info("repair.done", "check", res.Check, "detail", res.Detail)
		} else {
			n.log.Warn("repair.fail", "check", res.Check, "detail", res.Detail)
		}
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"results": results})
}

// repairTLS regenerates a missing or corrupt self-signed certificate.
// The TLS listener loads the cert at startup only, so a fresh cert takes
// effect after restart — the detail says so.
func repairTLS(rc repairCtx) repairOutcome {
	if rc.cfg.TLSPort <= 0 {
		return repairOutcome{Check: "tls", OK: true, Detail: "加密服务未开启，无需修复", Skipped: true}
	}
	if _, err := os.Stat(certFile(rc.stateDir)); err == nil {
		return repairOutcome{Check: "tls", OK: true, Detail: "证书已就绪，无需修复", Skipped: true}
	}
	hostName := hostNameOr(rc.cfg.Hostname, rc.cfg.Name)
	if _, err := loadOrCreateTLSCert(rc.stateDir, rc.cfg.Name, hostName); err != nil {
		return repairOutcome{Check: "tls", OK: false, Detail: "证书生成失败: " + err.Error()}
	}
	return repairOutcome{Check: "tls", OK: true, Detail: "已生成新证书；重启服务后生效"}
}

// repairStateDir recreates the state directory and re-probes the write.
// An ACL problem this cannot fix is reported honestly.
func repairStateDir(rc repairCtx) repairOutcome {
	dir := rc.stateDir
	if dir == "" {
		return repairOutcome{Check: "state-dir", OK: false, Detail: "未配置状态目录"}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return repairOutcome{Check: "state-dir", OK: false, Detail: "创建目录失败: " + err.Error()}
	}
	probe := filepath.Join(dir, ".doctor-probe")
	if err := os.WriteFile(probe, []byte("ok"), 0o600); err != nil {
		return repairOutcome{Check: "state-dir", OK: false,
			Detail: "目录已就绪但仍不可写（需手动检查权限/所有者）: " + err.Error()}
	}
	_ = os.Remove(probe)
	return repairOutcome{Check: "state-dir", OK: true, Detail: "状态目录已可写: " + dir}
}

// firewallCheckNow re-grades just the firewall check against the live
// rule table (a full doctorParams run would spend 3s on the mDNS browse
// this decision does not need).
func firewallCheckNow(rc repairCtx) doctor.Check {
	for _, c := range doctor.Run(doctor.Params{Port: rc.cfg.Port, TLSPort: rc.cfg.TLSPort, StateDir: rc.stateDir}) {
		if c.Name == "firewall" {
			return c
		}
	}
	return doctor.Check{Name: "firewall", Level: doctor.LevelInfo}
}

func firstLineOf(s string) string {
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}
