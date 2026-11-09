//go:build !windows

package main

import "fmt"

// The UI only offers fixes doctor flagged CanFix, and doctor only sets
// can_fix for firewall/pdf on Windows — these stubs exist so the
// registry compiles everywhere and `dyj doctor --fix` degrades to an
// honest "do it by hand" instead of a crash.

func repairFirewall(rc repairCtx) repairOutcome {
	return repairOutcome{Check: "firewall", OK: true, Skipped: true,
		Detail: "此平台不支持自动修复防火墙，请按自检给出的命令手动执行"}
}

func repairPDF(rc repairCtx) repairOutcome {
	return repairOutcome{Check: "pdf", OK: true, Skipped: true,
		Detail: "此平台请手动安装 poppler（brew / 发行版包管理器）后重启服务"}
}

// runFirewallHelper mirrors the Windows elevated child entry; the
// setup-helper --fix-firewall path never fires on this platform.
func runFirewallHelper(specPath, resPath string) error {
	_ = specPath
	_ = resPath
	return fmt.Errorf("setup-helper --fix-firewall is Windows-only")
}
