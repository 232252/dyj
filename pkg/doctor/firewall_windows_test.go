//go:build windows

package doctor

import "testing"

// 探测层负责把 PowerShell 的 Inbound/Outbound（及 netsh 的 In/Out）归一
// 成 grader 比较的小写 in/out——真实机器上 PowerShell 返回的是 Inbound。
func TestNormalizeDirection(t *testing.T) {
	cases := map[string]string{
		"Inbound": "in", "Outbound": "out", "In": "in", "Out": "out",
		"inbound": "in", " inbound ": "in", "weird": "weird",
	}
	for in, want := range cases {
		if got := normalizeDirection(in); got != want {
			t.Errorf("normalizeDirection(%q) = %q, want %q", in, got, want)
		}
	}
}
