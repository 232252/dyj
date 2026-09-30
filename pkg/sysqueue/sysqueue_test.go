package sysqueue

import (
	"strings"
	"testing"
)

func TestSpecDescribeMasksToken(t *testing.T) {
	s := Spec{Name: "办公室打印机", IPPURL: "ipp://10.0.0.5:6631/p/SECRETTOKEN/ipp/print/epson"}
	d := s.Describe()
	if strings.Contains(d, "SECRETTOKEN") {
		t.Fatalf("token leaked: %s", d)
	}
	if d != "ipp://10.0.0.5:6631/p/****/ipp/print/epson" {
		t.Fatalf("describe: %s", d)
	}
}

func TestPSQuote(t *testing.T) {
	cases := []struct{ in, want string }{
		{"plain", `'plain'`},
		{"it's", `'it''s'`},
		{`a'b'c`, `'a''b''c'`},
		{"", `''`},
	}
	for _, c := range cases {
		if got := psQuote(c.in); got != c.want {
			t.Errorf("psQuote(%q) = %s, want %s", c.in, got, c.want)
		}
	}
}

func TestUniqueName(t *testing.T) {
	taken := map[string]bool{"epson": true, "epson (2)": true}
	if got := uniqueName("epson", func(n string) bool { return taken[n] }); got != "epson (3)" {
		t.Fatalf("got %q", got)
	}
	if got := uniqueName("free", func(string) bool { return false }); got != "free" {
		t.Fatalf("free name must stay, got %q", got)
	}
}

func TestFirstLines(t *testing.T) {
	in := "l1\nl2\nl3\nl4"
	if got := firstLines(in, 2); got != "l1\nl2 …" {
		t.Fatalf("got %q", got)
	}
	if got := firstLines("only", 5); got != "only" {
		t.Fatalf("got %q", got)
	}
}
