package admin

import (
	"bufio"
	"io"
	"regexp"
	"strings"
	"testing"
)

// 文案规范是硬约束（research/14 §3.6，ADR-0011 D4）：用户可见界面零工程
// 术语。本测试扫描嵌入的 i18n 词条值，禁用词出现即失败——例外只有三类：
// help_/about_ 前缀（帮助页只读技术区）、token_ 前缀与 err_auth（局域网
// 管理令牌条）。加新文案时若确需术语，走这三个例外前缀并想清楚。
func TestI18nNoJargonInUserCopy(t *testing.T) {
	// ASCII 词按不区分大小写匹配；CJK 词按原文匹配。
	banned := []string{
		"uri", "url", "ipp", "mdns", "tls", "token", "port", "backend",
		"driver", "queue", "hostname", "iface",
		"端口", "后端", "队列", "驱动", "mDNS", "主机名", "监听接口",
	}
	allowedPrefix := []string{"help_", "about_", "token_"}
	allowedKey := map[string]bool{"err_auth": true}

	lineRe := regexp.MustCompile(`^\s*([a-zA-Z0-9_]+):\s*'(.*)',?\s*$`)
	fsys := DefaultWeb()
	for _, name := range []string{"i18n/zh-CN.js", "i18n/en.js"} {
		f, err := fsys.Open(name)
		if err != nil {
			t.Fatalf("open %s: %v", name, err)
		}
		sc := bufio.NewScanner(f)
		for ln := 1; sc.Scan(); ln++ {
			m := lineRe.FindStringSubmatch(sc.Text())
			if m == nil {
				continue
			}
			key, val := m[1], strings.ReplaceAll(m[2], `\'`, "'")
			if allowedKey[key] {
				continue
			}
			skip := false
			for _, p := range allowedPrefix {
				if strings.HasPrefix(key, p) {
					skip = true
					break
				}
			}
			if skip {
				continue
			}
			lower := strings.ToLower(val)
			for _, b := range banned {
				hit := strings.Contains(val, b) // CJK 原文
				if isASCII(b) {
					hit = strings.Contains(lower, b)
				}
				if hit {
					t.Errorf("%s:%d %s: 禁用词 %q 出现在用户文案 %q", name, ln, key, b, val)
				}
			}
		}
		_ = f.(io.Closer)
		scErr := sc.Err()
		if scErr != nil {
			t.Fatalf("scan %s: %v", name, scErr)
		}
		f.Close()
	}
}

func isASCII(s string) bool {
	for _, r := range s {
		if r > 127 {
			return false
		}
	}
	return true
}
