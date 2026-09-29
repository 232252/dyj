package tray

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// fakeAdmin is the /admin/api slice the tray talks to.
type fakeAdmin struct {
	accepting bool
	pauses    int
	resumes   int
}

func (f *fakeAdmin) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/admin/api/status":
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"accepting": f.accepting, "queued": 2, "version": "test",
		})
	case "/admin/api/pause":
		f.pauses++
		f.accepting = false
		w.WriteHeader(http.StatusOK)
	case "/admin/api/resume":
		f.resumes++
		f.accepting = true
		w.WriteHeader(http.StatusOK)
	default:
		http.NotFound(w, r)
	}
}

func newTestController(t *testing.T, f *fakeAdmin) (*Controller, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	c := NewController(Options{
		AdminURL:     srv.URL,
		ExePath:      `C:\Program Files\dyj\dyj-tray.exe`,
		PollInterval: 10 * time.Millisecond,
	})
	return c, srv
}

func TestPollStateTransitions(t *testing.T) {
	f := &fakeAdmin{accepting: true}
	c, _ := newTestController(t, f)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	c.Poll(ctx)
	if c.State() != StateRunning {
		t.Fatalf("state = %v, want StateRunning", c.State())
	}
	if c.Queued() != 2 {
		t.Fatalf("queued = %d, want 2", c.Queued())
	}

	f.accepting = false
	c.Poll(ctx)
	if c.State() != StatePaused {
		t.Fatalf("state = %v, want StatePaused", c.State())
	}
}

func TestPollServerDown(t *testing.T) {
	c, srv := newTestController(t, &fakeAdmin{accepting: true})
	url := srv.URL
	srv.Close()
	c.opts.AdminURL = url
	c.Poll(context.Background())
	if c.State() != StateDown {
		t.Fatalf("state = %v, want StateDown", c.State())
	}
}

func TestSetPausedRoundTrip(t *testing.T) {
	f := &fakeAdmin{accepting: true}
	c, _ := newTestController(t, f)
	ctx := context.Background()
	c.Poll(ctx)

	if err := c.SetPaused(ctx, true); err != nil {
		t.Fatal(err)
	}
	if f.pauses != 1 || f.accepting {
		t.Fatalf("pause: calls=%d accepting=%v", f.pauses, f.accepting)
	}
	if c.State() != StatePaused {
		t.Fatalf("state after pause = %v", c.State())
	}

	if err := c.SetPaused(ctx, false); err != nil {
		t.Fatal(err)
	}
	if f.resumes != 1 || !f.accepting {
		t.Fatalf("resume: calls=%d accepting=%v", f.resumes, f.accepting)
	}
}

func TestIconBytesIsICOWithPNG(t *testing.T) {
	for st := range statusRGB {
		ico := IconBytes(st)
		// ICONDIR + ICONDIRENTRY + PNG payload.
		if len(ico) < 22+8 || ico[0] != 0 || ico[1] != 0 || ico[2] != 1 || ico[3] != 0 {
			t.Fatalf("state %v: bad ICONDIR", st)
		}
		if ico[4] != 1 || ico[5] != 0 {
			t.Fatalf("state %v: image count != 1", st)
		}
		if ico[6] != 32 { // width 32 (0 would mean 256)
			t.Fatalf("state %v: entry width != 32", st)
		}
		offset := uint32(ico[18]) | uint32(ico[19])<<8 | uint32(ico[20])<<16 | uint32(ico[21])<<24
		if offset != 22 {
			t.Fatalf("state %v: PNG offset = %d, want 22", st, offset)
		}
		if _, err := png.Decode(bytes.NewReader(ico[offset:])); err != nil {
			t.Fatalf("state %v: embedded PNG does not decode: %v", st, err)
		}
	}
}

func TestIconColorsDiffer(t *testing.T) {
	run := renderIcon(StateRunning, false)
	down := renderIcon(StateDown, false)
	if bytes.Equal(run, down) {
		t.Fatal("running and down icons are identical")
	}
}

func TestThemeFlipsInk(t *testing.T) {
	if bytes.Equal(renderIcon(StateRunning, false), renderIcon(StateRunning, true)) {
		t.Fatal("dark and light taskbar icons are identical")
	}
}

// decodeIconPNG unpacks the PNG payload from renderIcon output.
func decodeIconPNG(t *testing.T, ico []byte) image.Image {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(ico[22:]))
	if err != nil {
		t.Fatalf("payload does not decode: %v", err)
	}
	return img
}

func TestBadgeCarriesStateColor(t *testing.T) {
	// Badge center pixel must be exactly the state color at full opacity.
	for st, want := range statusRGB {
		img := decodeIconPNG(t, renderIcon(st, false))
		r, g, b, a := img.At(25, 25).RGBA()
		if a != 0xffff || uint8(r>>8) != want[0] || uint8(g>>8) != want[1] || uint8(b>>8) != want[2] {
			t.Fatalf("state %v: badge pixel = (%d,%d,%d,%d), want %v opaque", st, r>>8, g>>8, b>>8, a, want)
		}
	}
}

func TestGlyphIsDrawn(t *testing.T) {
	// A pixel on the printer body's top edge must carry glyph ink (not
	// transparent), so the icon is a brand mark, not a bare dot.
	img := decodeIconPNG(t, renderIcon(StateRunning, false))
	ink := 0
	for x := 8 * 4 / 3; x < 22*4/3; x++ {
		if _, _, _, a := img.At(x, 12).RGBA(); a != 0 {
			ink++
		}
	}
	if ink == 0 {
		t.Fatal("no opaque pixels along the printer body's top edge")
	}
}
