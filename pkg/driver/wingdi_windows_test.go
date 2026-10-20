//go:build windows

package driver

import (
	"encoding/binary"
	"testing"
)

func TestMatchPaperCutsSizeSegment(t *testing.T) {
	cases := []struct {
		media string
		want  uint16
		ok    bool
	}{
		{"iso_a4_210x297mm", 9, true},
		{"iso_a4", 9, true},
		{"na_letter_8.5x11in", 1, true},
		{"na_legal", 5, true},
		{"iso_a5_148x210mm", 11, true},
		{"na_index-4x6_4x6in", 70, true},
		{"custom_w60_h40mm", 0, false},
		{"", 0, false},
	}
	for _, tc := range cases {
		got, ok := matchPaper(tc.media)
		if ok != tc.ok || got != tc.want {
			t.Errorf("matchPaper(%q) = (%d, %v), want (%d, %v)", tc.media, got, ok, tc.want, tc.ok)
		}
	}
}

func TestPatchDevModeSetsFieldsAndValues(t *testing.T) {
	dm := make([]byte, 220)
	base := uint32(0x00100000)
	binary.LittleEndian.PutUint32(dm[devModeOffsetFields:], base)

	patchDevMode(dm, PrintJob{Media: "iso_a4_210x297mm", Copies: 3})

	if got := binary.LittleEndian.Uint32(dm[devModeOffsetFields:]); got != base|dmPaperSize|dmCopies {
		t.Errorf("dmFields = 0x%08X, want 0x%08X", got, base|dmPaperSize|dmCopies)
	}
	if got := binary.LittleEndian.Uint16(dm[devModeOffsetPaperSize:]); got != 9 {
		t.Errorf("dmPaperSize = %d, want 9 (DMPAPER_A4)", got)
	}
	if got := binary.LittleEndian.Uint16(dm[devModeOffsetCopies:]); got != 3 {
		t.Errorf("dmCopies = %d, want 3", got)
	}
}

func TestPatchDevModeLeavesUntouchedOnNoOps(t *testing.T) {
	dm := make([]byte, 220)
	for i := range dm {
		dm[i] = 0xAB
	}
	before := string(dm)
	// Unknown media, single copy, no orientation: nothing may change —
	// a wrong DEVMODE is worse than an unpatched one.
	patchDevMode(dm, PrintJob{Media: "custom_w60_h40mm", Copies: 1})
	if string(dm) != before {
		t.Error("patchDevMode mutated the DEVMODE on a no-op job")
	}
}
