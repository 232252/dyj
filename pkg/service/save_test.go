package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// 升级防呆（2026-11-14 现场）：安装器每次运行 `dyj service install`（无
// 参数）。旧实现无条件 SaveOptions，把用户已配置好的共享清单、常开免认证
// 等整体覆盖成引导默认——升级一次，配置全丢。规则：无显式参数且已有有效
// 配置时，保留现有配置，只修复服务注册。
func TestSaveOptionsIfAbsentKeepsExisting(t *testing.T) {
	dir := withScratchStateDir(t)
	existing := `{"role":"share","name":"keep-me","port":6631,"open_print":true}`
	if err := SaveOptions(existing); err != nil {
		t.Fatal(err)
	}
	kept, err := SaveOptionsIfAbsent(`{"role":"","name":"bootstrap","port":6631}`)
	if err != nil {
		t.Fatal(err)
	}
	if !kept {
		t.Fatal("existing valid config must be kept on a no-arg install")
	}
	data, err := os.ReadFile(filepath.Join(dir, optionsFile))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	if m["name"] != "keep-me" || m["open_print"] != true {
		t.Fatalf("existing config was overwritten: %s", data)
	}
}

func TestSaveOptionsIfAbsentWritesWhenMissing(t *testing.T) {
	dir := withScratchStateDir(t)
	kept, err := SaveOptionsIfAbsent(`{"role":"share","name":"fresh","port":6631}`)
	if err != nil {
		t.Fatal(err)
	}
	if kept {
		t.Fatal("missing config must be written, not kept")
	}
	if _, err := os.Stat(filepath.Join(dir, optionsFile)); err != nil {
		t.Fatalf("config not written: %v", err)
	}
}

func TestSaveOptionsIfAbsentReplacesCorrupt(t *testing.T) {
	dir := withScratchStateDir(t)
	if err := os.WriteFile(filepath.Join(dir, optionsFile), []byte(`{corrupt`), 0o644); err != nil {
		t.Fatal(err)
	}
	kept, err := SaveOptionsIfAbsent(`{"role":"share","name":"repaired","port":6631}`)
	if err != nil {
		t.Fatal(err)
	}
	if kept {
		t.Fatal("corrupt config must be replaced, not kept")
	}
	data, _ := os.ReadFile(filepath.Join(dir, optionsFile))
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil || m["name"] != "repaired" {
		t.Fatalf("corrupt config not replaced: %s", data)
	}
}