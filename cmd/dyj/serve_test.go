package main

import "testing"

func TestASCIIHostName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"dyj", "dyj"},
		{"My PC-01", "my-pc-01"},
		{"生命", "dyj"},          // non-ASCII: every byte maps away, fallback wins
		{"LIFE-生命-X", "life--------x"}, // one dash per non-ASCII byte, kept inside
		{"DESK.TOP", "desk-top"},
		{"", "dyj"},
		{"---", "dyj"},
	}
	for _, c := range cases {
		if got := asciiHostName(c.in); got != c.want {
			t.Errorf("asciiHostName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// A CJK machine name must not crash self-signed certificate creation:
// DNS SANs are IA5String and x509.CreateCertificate rejects non-ASCII.
func TestLoadOrCreateTLSCertCJKHost(t *testing.T) {
	dir := t.TempDir()
	if _, err := loadOrCreateTLSCert(dir, "dyj on 生命", "生命"); err != nil {
		t.Fatalf("loadOrCreateTLSCert with CJK host: %v", err)
	}
	// Second call must reuse the persisted pair.
	if _, err := loadOrCreateTLSCert(dir, "dyj on 生命", "生命"); err != nil {
		t.Fatalf("loadOrCreateTLSCert reuse: %v", err)
	}
}
