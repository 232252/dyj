package attributes

import "testing"

func TestDeriveUUID(t *testing.T) {
	a := DeriveUUID("dyj-printer:office")
	if a != DeriveUUID("dyj-printer:office") {
		t.Errorf("DeriveUUID not deterministic: %s vs %s", a, DeriveUUID("dyj-printer:office"))
	}
	if a == DeriveUUID("dyj-printer:home") {
		t.Errorf("different names derived the same UUID: %s", a)
	}
	if len(a) != 36 {
		t.Fatalf("length = %d, want 36: %s", len(a), a)
	}
	for i, c := range []byte(a) {
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				t.Errorf("byte %d = %q, want '-'", i, c)
			}
		default:
			if !isHexLower(c) {
				t.Errorf("byte %d = %q, want lowercase hex", i, c)
			}
		}
	}
	// RFC 9562: version nibble 8 (custom) and variant nibble 10xx.
	if a[14] != '8' {
		t.Errorf("version nibble = %q, want '8': %s", a[14], a)
	}
	switch a[19] {
	case '8', '9', 'a', 'b':
	default:
		t.Errorf("variant nibble = %q, want 8/9/a/b: %s", a[19], a)
	}
}

func isHexLower(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f'
}
