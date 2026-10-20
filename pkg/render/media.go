// media.go decodes PWG self-describing media size names
// (PWG 5101.1: "na_letter_8.5x11in", "iso_a4_210x297mm", ...) into
// millimetre dimensions. Parsing the name — instead of a lookup table —
// means every size a client names is honored, not just the ones we
// remembered to list.
package render

import (
	"fmt"
	"strconv"
	"strings"
)

// Media is a page size in millimetres.
type Media struct {
	WidthMM  float64
	HeightMM float64
	// Name is the PWG name the size came from ("" for anonymous sizes).
	Name string
}

// MediaFromPWG parses a PWG 5101.1 self-describing media name. The
// trailing "<w>x<h><unit>" segment carries the dimensions; units are mm
// or in.
func MediaFromPWG(name string) (Media, error) {
	// The size segment is the last "_"-separated field for na_/iso_
	// names ("na_letter_8.5x11in") but the second for plain "custom_..."
	// style names; scanning for the LAST field containing 'x' is
	// simplest and covers both.
	seg := ""
	for _, f := range strings.Split(name, "_") {
		if strings.Contains(f, "x") {
			seg = f
		}
	}
	if seg == "" {
		return Media{}, fmt.Errorf("render: media name %q has no size segment", name)
	}
	dims, unit, found := strings.Cut(seg, "x")
	if !found {
		return Media{}, fmt.Errorf("render: media name %q has malformed size %q", name, seg)
	}
	w, err := strconv.ParseFloat(dims, 64)
	if err != nil {
		return Media{}, fmt.Errorf("render: media name %q has malformed width %q", name, dims)
	}
	var h float64
	switch {
	case strings.HasSuffix(unit, "mm"):
		h, err = strconv.ParseFloat(strings.TrimSuffix(unit, "mm"), 64)
		if err != nil {
			return Media{}, fmt.Errorf("render: media name %q has malformed height %q", name, unit)
		}
		return Media{WidthMM: w, HeightMM: h, Name: name}, nil
	case strings.HasSuffix(unit, "in"):
		h, err = strconv.ParseFloat(strings.TrimSuffix(unit, "in"), 64)
		if err != nil {
			return Media{}, fmt.Errorf("render: media name %q has malformed height %q", name, unit)
		}
		return Media{WidthMM: w * 25.4, HeightMM: h * 25.4, Name: name}, nil
	default:
		return Media{}, fmt.Errorf("render: media name %q has unknown unit in %q (want mm|in)", name, unit)
	}
}

// PixelSize converts the media into pixel dimensions at the given dpi,
// rounded down (printers clip, they do not extrapolate).
func (m Media) PixelSize(dpi int) (w, h int) {
	return int(m.WidthMM * float64(dpi) / 25.4), int(m.HeightMM * float64(dpi) / 25.4)
}

// A4 is the default page for text rendering.
func A4() Media {
	m, _ := MediaFromPWG("iso_a4_210x297mm")
	return m
}
