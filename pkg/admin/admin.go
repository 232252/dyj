// Package admin serves the web management UI and its JSON API on the
// same HTTP port as IPP (under /admin). The UI is a no-build vanilla
// Web Components app embedded with go:embed — one control per file, no
// npm, no build chain (docs/research/10 §2.3).
//
// Auth model (ADR-0008, the P0 decision):
//
//   - Requests from the loopback interface have FULL access: whoever is
//     on the machine already owns it, and the tray + local browser must
//     work with zero setup.
//   - Remote (LAN) requests get read-only status and job visibility —
//     consistent with the printer itself being LAN-shared — and every
//     WRITE (config, pause/resume, cancel, diagnostics, log stream)
//     requires the X-Dyj-Token header to match the token persisted at
//     <stateDir>/admin-token (0600). The token is generated at serve
//     start and printed once in the banner; the web UI stores it in
//     localStorage after the first entry.
package admin

import (
	crand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sq199/dyj/pkg/ipp"
)

// SpoolControl is the slice of the spooler the admin API drives.
// *spooler.Spooler satisfies it; the interface keeps this package from
// importing spooler (and its driver tree) directly.
type SpoolControl interface {
	Status() (accepting bool, queued int32, reasons []string)
	SetAccepting(accepting bool)
	Snapshot(whichJobs string) []ipp.JobView
	Cancel(id int32) error
}

// Status is the static (per-process) half of GET /api/status; dynamic
// accept/queue state is pulled from SpoolControl by the handler.
type Status struct {
	Version     string   `json:"version"`
	PrinterName string   `json:"printer_name"`
	PrinterInfo string   `json:"printer_info"`
	URIs        []string `json:"uris"`
	Backend     string   `json:"backend"`
	Port        int      `json:"port"`
	TLSPort     int      `json:"tls_port,omitempty"`
	MDNS        bool     `json:"mdns"`
	AdminURL    string   `json:"admin_url"`
	Formats     []string `json:"formats,omitempty"`
}

// Options configures the admin handler.
type Options struct {
	// StatusFn returns the static status; nil status fields are absent.
	StatusFn func() Status
	// ConfigGet returns the live serve options (JSON-marshalable).
	ConfigGet func() any
	// ConfigPut validates and persists a new options JSON. It MUST NOT
	// apply them live: config changes take effect on service restart,
	// and the API answers restart_required=true accordingly.
	ConfigPut func(raw []byte) error
	// Spool drives pause/resume/cancel/jobs.
	Spool SpoolControl
	// Logs is the ring buffer behind GET /api/logs (SSE).
	Logs *LogRing
	// Token gates non-loopback writes ("" disables the gate).
	Token string
	// Web is the embedded UI tree (index.html at its root).
	Web fs.FS
	// Diagnostics runs the doctor checks (nil omits the endpoint).
	Diagnostics func(any) any
}

// Handler implements http.Handler for the /admin subtree.
type Handler struct {
	opts Options
	web  http.Handler
}

// New builds the handler. Panics on a nil Spool: without it there is no
// admin API to serve.
func New(opts Options) *Handler {
	if opts.Spool == nil {
		panic("admin: nil SpoolControl")
	}
	h := &Handler{opts: opts}
	if opts.Web != nil {
		h.web = http.StripPrefix("/admin", http.FileServer(http.FS(opts.Web)))
	}
	return h
}

// jobStateNames renders RFC 8011 §5.3.8 job-state values for humans.
var jobStateNames = map[int32]string{
	3: "pending", 4: "held", 5: "processing", 7: "canceled", 8: "aborted", 9: "completed",
}

type jobJSON struct {
	ID          int32    `json:"id"`
	State       int32    `json:"state"`
	StateName   string   `json:"state_name"`
	Reasons     []string `json:"reasons"`
	Name        string   `json:"name"`
	User        string   `json:"user"`
	Format      string   `json:"format,omitempty"`
	SizeKOctets int32    `json:"size_kb"`
	Copies      int      `json:"copies"`
}

func jobToJSON(v ipp.JobView) jobJSON {
	return jobJSON{
		ID: v.ID, State: v.State,
		StateName: jobStateNames[v.State],
		Reasons:   v.Reasons, Name: v.Name, User: v.User,
		Format: v.Format, SizeKOctets: v.SizeKOctets, Copies: v.Copies,
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/admin")
	if path == "" || path == "/" {
		path = "/index.html"
	}
	if strings.HasPrefix(path, "/api/") {
		h.serveAPI(w, r, strings.TrimPrefix(path, "/api/"))
		return
	}
	if h.web == nil {
		http.Error(w, "admin UI not embedded", http.StatusNotFound)
		return
	}
	h.web.ServeHTTP(w, r)
}

// fromLoopback reports whether the request originates on the machine.
func fromLoopback(r *http.Request) bool {
	host := r.RemoteAddr
	if i := strings.LastIndex(host, "]"); i >= 0 { // [::1]:port
		host = host[:i+1]
	} else if i := strings.LastIndex(host, ":"); i >= 0 {
		host = host[:i]
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

type apiError struct {
	Error string `json:"error"`
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func (h *Handler) serveAPI(w http.ResponseWriter, r *http.Request, ep string) {
	// Remote read-only surface: status and the job list. Everything
	// else (writes AND the log/diagnostic detail) needs the token.
	remoteWrite := !fromLoopback(r)
	if remoteWrite && ep != "status" && ep != "jobs" {
		if h.opts.Token == "" || r.Header.Get("X-Dyj-Token") != h.opts.Token {
			w.Header().Set("WWW-Authenticate", `Token realm="dyj admin"`)
			writeJSON(w, http.StatusUnauthorized, apiError{"token required for this endpoint from a non-local client"})
			return
		}
	}

	switch r.Method {
	case http.MethodGet:
		switch ep {
		case "status":
			h.handleStatus(w, r)
		case "jobs":
			h.handleJobs(w, r)
		case "config":
			h.handleConfigGet(w)
		case "logs":
			h.handleLogs(w, r)
		case "diagnostics":
			h.handleDiagnostics(w, r)
		default:
			writeJSON(w, http.StatusNotFound, apiError{"unknown endpoint " + ep})
		}
	case http.MethodPost:
		switch ep {
		case "pause":
			h.opts.Spool.SetAccepting(false)
			writeJSON(w, http.StatusOK, map[string]any{"accepting": false})
		case "resume":
			h.opts.Spool.SetAccepting(true)
			writeJSON(w, http.StatusOK, map[string]any{"accepting": true})
		default:
			if id, ok := strings.CutPrefix(ep, "jobs/"); ok && strings.HasSuffix(id, "/cancel") {
				h.handleCancel(w, strings.TrimSuffix(id, "/cancel"))
				return
			}
			writeJSON(w, http.StatusNotFound, apiError{"unknown endpoint " + ep})
		}
	case http.MethodPut:
		if ep == "config" {
			h.handleConfigPut(w, r)
			return
		}
		writeJSON(w, http.StatusNotFound, apiError{"unknown endpoint " + ep})
	default:
		w.Header().Set("Allow", "GET, POST, PUT")
		writeJSON(w, http.StatusMethodNotAllowed, apiError{"method not allowed"})
	}
}

func (h *Handler) handleStatus(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{}
	if h.opts.StatusFn != nil {
		for k, v := range structToMap(h.opts.StatusFn()) {
			out[k] = v
		}
	}
	accepting, queued, reasons := h.opts.Spool.Status()
	out["accepting"] = accepting
	out["queued"] = queued
	if reasons == nil {
		reasons = []string{}
	}
	out["reasons"] = reasons
	out["remote_write_requires_token"] = !fromLoopback(r)
	writeJSON(w, http.StatusOK, out)
}

func structToMap(v any) map[string]any {
	m := map[string]any{}
	if v == nil {
		return m
	}
	data, err := json.Marshal(v)
	if err != nil {
		return m
	}
	_ = json.Unmarshal(data, &m)
	return m
}

func (h *Handler) handleJobs(w http.ResponseWriter, r *http.Request) {
	which := r.URL.Query().Get("which")
	if which == "" {
		which = "all"
	}
	rows := h.opts.Spool.Snapshot(which)
	out := make([]jobJSON, 0, len(rows))
	for _, v := range rows {
		out = append(out, jobToJSON(v))
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobs": out})
}

func (h *Handler) handleCancel(w http.ResponseWriter, idStr string) {
	id, err := strconv.ParseInt(idStr, 10, 32)
	if err != nil || id <= 0 {
		writeJSON(w, http.StatusBadRequest, apiError{"bad job id"})
		return
	}
	switch err := h.opts.Spool.Cancel(int32(id)); {
	case err == nil:
		writeJSON(w, http.StatusOK, map[string]any{"canceled": id})
	case errors.Is(err, ipp.ErrUnknownJob):
		writeJSON(w, http.StatusNotFound, apiError{"no such job"})
	default:
		writeJSON(w, http.StatusConflict, apiError{err.Error()})
	}
}

func (h *Handler) handleConfigGet(w http.ResponseWriter) {
	if h.opts.ConfigGet == nil {
		writeJSON(w, http.StatusNotImplemented, apiError{"config not wired"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"config": h.opts.ConfigGet(),
		// Config changes take effect when the service restarts; the UI
		// shows a "重启服务生效" hint instead of pretending live-apply.
		"restart_required_on_change": true,
	})
}

func (h *Handler) handleConfigPut(w http.ResponseWriter, r *http.Request) {
	if h.opts.ConfigPut == nil {
		writeJSON(w, http.StatusNotImplemented, apiError{"config not wired"})
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<16))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{"unreadable body"})
		return
	}
	if err := h.opts.ConfigPut(raw); err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"saved": true, "restart_required": true})
}

func (h *Handler) handleLogs(w http.ResponseWriter, r *http.Request) {
	if h.opts.Logs == nil {
		writeJSON(w, http.StatusNotImplemented, apiError{"logs not wired"})
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusNotImplemented, apiError{"streaming unsupported"})
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)

	lines := h.opts.Logs.Snapshot()
	for _, l := range lines {
		fmt.Fprintf(w, "data: %s\n\n", strings.ReplaceAll(l, "\n", " "))
	}
	flusher.Flush()

	live, stop := h.opts.Logs.Subscribe()
	defer h.opts.Logs.Unsubscribe(stop)
	ch := live
	heartbeat := newTicker(15 * timeSecond)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case line, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", strings.ReplaceAll(line, "\n", " "))
			flusher.Flush()
		case <-heartbeat.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}

func (h *Handler) handleDiagnostics(w http.ResponseWriter, r *http.Request) {
	if h.opts.Diagnostics == nil {
		writeJSON(w, http.StatusNotImplemented, apiError{"diagnostics not wired"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"checks": h.opts.Diagnostics(r)})
}

// ---------------------------------------------------------------------------
// Token
// ---------------------------------------------------------------------------

// TokenBytes is the length of a generated admin token (32 bytes → 64 hex
// chars, ~256 bits — beyond brute force for a LAN tool).
const TokenBytes = 32

// NewToken generates a fresh random admin token.
func NewToken() (string, error) {
	b := make([]byte, TokenBytes)
	if _, err := cryptoRead(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// small indirections keep the imports flat and tests deterministic.
var (
	cryptoRead = func(b []byte) (int, error) { return crand.Read(b) }
	newTicker  = time.NewTicker
	timeSecond = time.Second
)

// LoadOrCreateToken reads the persisted admin token or creates one on
// first run. The file is 0600: it lives in the state dir next to the
// TLS key and never leaves the machine.
func LoadOrCreateToken(path string) (string, error) {
	if data, err := os.ReadFile(path); err == nil {
		if t := strings.TrimSpace(string(data)); len(t) == 2*TokenBytes && isHex(t) {
			return t, nil
		}
	}
	t, err := NewToken()
	if err != nil {
		return "", fmt.Errorf("admin: generating token: %w", err)
	}
	if err := os.WriteFile(path, []byte(t+"\n"), 0o600); err != nil {
		return "", fmt.Errorf("admin: persisting token: %w", err)
	}
	return t, nil
}

func isHex(s string) bool {
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f':
		default:
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// LogRing: bounded in-memory log tail + live subscribers for SSE.
// ---------------------------------------------------------------------------

type ringSub chan string

// LogRing is an io.Writer the process logger fans into: it keeps the
// last Capacity lines and pushes each new line to SSE subscribers.
// It never blocks the logger — a slow subscriber drops its lines.
type LogRing struct {
	mu       sync.Mutex
	lines    []string
	capacity int
	subs     map[ringSub]struct{}
}

// NewLogRing keeps the last capacity lines (default 500).
func NewLogRing(capacity int) *LogRing {
	if capacity <= 0 {
		capacity = 500
	}
	return &LogRing{capacity: capacity, subs: map[ringSub]struct{}{}}
}

// Write implements io.Writer; partial writes (a logger writing half a
// line) are buffered internally until a newline arrives.
func (r *LogRing) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, line := range strings.Split(string(p), "\n") {
		if line == "" {
			continue
		}
		r.lines = append(r.lines, line)
		if len(r.lines) > r.capacity {
			r.lines = r.lines[len(r.lines)-r.capacity:]
		}
		for ch := range r.subs {
			select {
			case ch <- line:
			default: // slow subscriber: drop rather than block the logger
			}
		}
	}
	return len(p), nil
}

// Snapshot copies the retained lines (oldest first).
func (r *LogRing) Snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.lines))
	copy(out, r.lines)
	return out
}

// Subscribe registers a live channel (buffered 64); the stop channel
// is returned for Unsubscribe.
func (r *LogRing) Subscribe() (<-chan string, ringSub) {
	ch := make(ringSub, 64)
	r.mu.Lock()
	r.subs[ch] = struct{}{}
	r.mu.Unlock()
	return ch, ch
}

// Unsubscribe removes a channel added by Subscribe.
func (r *LogRing) Unsubscribe(ch ringSub) {
	r.mu.Lock()
	if r.subs != nil {
		delete(r.subs, ch)
	}
	close(ch)
	r.mu.Unlock()
}
