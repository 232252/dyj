// Package tray is the dyj system tray: a status icon plus quick
// actions (open admin page, pause/resume sharing, login autostart,
// quit). It is deliberately thin — all control lives in the running
// server's admin API on the loopback interface, so the tray can die and
// restart without touching the 7x24 service (docs/research/10 §1.3/2.4).
//
// Platform: Windows ships the tray in v1 (dyj-tray.exe, windowsgui
// subsystem). Linux/macOS use the web admin + .desktop/.app entries in
// v1 and may grow a tray helper later; the tray_*.go split keeps those
// builds free of the GUI dependency.
package tray

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// Options configures the tray.
type Options struct {
	// AdminURL is the loopback admin base ("http://127.0.0.1:6631").
	AdminURL string
	// ExePath is the tray executable for the autostart Run-key value.
	ExePath string
	// PollInterval defaults to 3s.
	PollInterval time.Duration
	// HTTPClient is injectable for tests (nil = default 2s timeout).
	HTTPClient *http.Client
}

// State is the tray's view of the server.
type State int

const (
	StateRunning State = iota // accepting
	StatePaused               // reachable, not accepting
	StateDown                 // unreachable
)

// statusResponse is the slice of GET /admin/api/status the tray needs.
type statusResponse struct {
	Accepting      bool             `json:"accepting"`
	Queued         int              `json:"queued"`
	Version        string           `json:"version"`
	PairingEnabled bool             `json:"pairing_enabled"`
	Printers       []struct {
		Name string `json:"name"`
	} `json:"printers"`
}

// pairingResponse is GET /admin/api/pairing (loopback has full access).
type pairingResponse struct {
	PIN       string `json:"pin"`
	RemainSec int    `json:"remain_sec"`
}

// Controller drives the tray: it polls status, exposes the current
// state, and performs the actions. The Windows glue (systray menus)
// sits on top of this in tray_windows.go; tests drive it directly.
type Controller struct {
	opts   Options
	http   *http.Client
	mu     sync.Mutex
	state  State
	queued int
	pin    string
}

// NewController builds a controller for opts.
func NewController(opts Options) *Controller {
	if opts.PollInterval <= 0 {
		opts.PollInterval = 3 * time.Second
	}
	if opts.AdminURL == "" {
		opts.AdminURL = "http://127.0.0.1:6631"
	}
	c := &Controller{opts: opts, http: opts.HTTPClient, state: StateDown}
	if c.http == nil {
		c.http = &http.Client{Timeout: 2 * time.Second}
	}
	return c
}

// State reports the last polled state.
func (c *Controller) State() State {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state
}

// Queued reports the last polled pending/processing job count.
func (c *Controller) Queued() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.queued
}

// Pin reports the current pairing code ("" when not sharing).
func (c *Controller) Pin() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.pin
}

// Poll refreshes state once. Exported for tests; PollLoop calls it.
func (c *Controller) Poll(ctx context.Context) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.opts.AdminURL+"/admin/api/status", nil)
	if err != nil {
		c.setState(StateDown, 0)
		return
	}
	resp, err := c.http.Do(req)
	if err != nil {
		c.setState(StateDown, 0)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		c.setState(StateDown, 0)
		return
	}
	var sr statusResponse
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil || json.Unmarshal(body, &sr) != nil {
		c.setState(StateDown, 0)
		return
	}
	if sr.Accepting {
		c.setState(StateRunning, sr.Queued)
	} else {
		c.setState(StatePaused, sr.Queued)
	}
	// Pairing code: only meaningful while printers are shared; the PIN is
	// loopback-readable (tray runs on the machine it guards).
	if sr.PairingEnabled && len(sr.Printers) > 0 {
		c.pollPin(ctx)
	} else {
		c.setPin("")
	}
}

func (c *Controller) pollPin(ctx context.Context) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.opts.AdminURL+"/admin/api/pairing", nil)
	if err != nil {
		return
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return
	}
	var pr pairingResponse
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err != nil || json.Unmarshal(body, &pr) != nil {
		return
	}
	c.setPin(pr.PIN)
}

func (c *Controller) setPin(pin string) {
	c.mu.Lock()
	c.pin = pin
	c.mu.Unlock()
}

func (c *Controller) setState(s State, queued int) {
	c.mu.Lock()
	c.state, c.queued = s, queued
	c.mu.Unlock()
}

// PollLoop refreshes until ctx is done.
func (c *Controller) PollLoop(ctx context.Context) {
	t := time.NewTicker(c.opts.PollInterval)
	defer t.Stop()
	for {
		c.Poll(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// SetPaused pauses (true) or resumes (false) sharing via the admin API.
func (c *Controller) SetPaused(ctx context.Context, paused bool) error {
	ep := "resume"
	if paused {
		ep = "pause"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.opts.AdminURL+"/admin/api/"+ep, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("tray: %s: %w", ep, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("tray: %s: HTTP %d", ep, resp.StatusCode)
	}
	c.Poll(ctx)
	return nil
}

// ---------------------------------------------------------------------------
// Icons: the brand mark rasterized at 32×32 (no asset files) with a
// status badge — green / gray / red, paired with tooltip text so color
// is never the only signal. Windows SetIcon receives an ICO container
// holding one PNG payload.
// ---------------------------------------------------------------------------

// statusRGB is the badge color per state.
var statusRGB = map[State][3]byte{
	StateRunning: {0x22, 0xC5, 0x5E},
	StatePaused:  {0x9C, 0xA3, 0xAF},
	StateDown:    {0xEF, 0x44, 0x44},
}

// The tray icon is the brand tile: blue rounded square + white printer
// glyph, so ink is theme-independent. Only the badge rim tracks the
// taskbar theme — it fakes a surface-colored cutout around the badge.
var (
	glyphInk   = [3]byte{0xFF, 0xFF, 0xFF}
	taskbarRim = map[bool][3]byte{
		false: {0x11, 0x18, 0x27}, // dark taskbar
		true:  {0xF3, 0xF4, 0xF6}, // light taskbar
	}
)

// taskbarLight tracks the shell theme; the Windows glue flips it via
// SetLightTaskbar when it reads AppsUseLightTheme.
var taskbarLight atomic.Bool

// SetLightTaskbar switches the badge rim for light-mode taskbars.
func SetLightTaskbar(light bool) { taskbarLight.Store(light) }

// IconBytes renders the 32×32 tray icon for s as ICO bytes.
func IconBytes(s State) []byte {
	return renderIcon(s, taskbarLight.Load())
}

func renderIcon(s State, light bool) []byte {
	png := render(glyphInk, taskbarRim[light], statusRGB[s])
	return icoWrap(png)
}

// icoWrap embeds one PNG into the minimal ICO container Windows accepts.
func icoWrap(png []byte) []byte {
	var out bytes.Buffer
	// ICONDIR: reserved=0, type=1 (icon), count=1.
	out.Write([]byte{0, 0, 1, 0, 1, 0})
	// ICONDIRENTRY: 32px (0 means 256), colors=0, planes=1, bpp=32.
	out.Write([]byte{32, 32, 0, 0, 1, 0, 32, 0})
	writeLE32(&out, uint32(len(png))) // bytes in resource
	writeLE32(&out, 22)               // offset = 6 + 16
	out.Write(png)
	return out.Bytes()
}

func writeLE32(b *bytes.Buffer, v uint32) {
	b.WriteByte(byte(v))
	b.WriteByte(byte(v >> 8))
	b.WriteByte(byte(v >> 16))
	b.WriteByte(byte(v >> 24))
}
