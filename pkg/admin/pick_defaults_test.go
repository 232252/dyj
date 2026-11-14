package admin

import (
	"strings"
	"testing"
)

// 防呆（2026-11-14 现场）：机器上只有虚拟打印机（导出为WPS PDF 等）时，
// 向导展开虚拟列表后默认全选，一键共享出去的全是"不出纸"的队列，平板端
// 看似连上却永远打不出东西。规则：虚拟打印机必须显式勾选，绝不默认选中。
func TestPickStateDoesNotPreTickVirtualPrinters(t *testing.T) {
	fsys := DefaultWeb()
	data, err := fsys.Open("pages/page-server.js")
	if err != nil {
		t.Fatal(err)
	}
	defer data.Close()
	buf := make([]byte, 1<<20)
	n, _ := data.Read(buf)
	src := string(buf[:n])
	if !strings.Contains(src, "cb.checked = !p.probe_err && !p.virtual;") {
		t.Fatal("向导默认勾选必须排除虚拟打印机（!p.probe_err && !p.virtual）")
	}
}