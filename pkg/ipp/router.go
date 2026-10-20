// router.go multiplexes one IPP Server per shared printer behind a single
// HTTP handler, and carries the two cross-cutting concerns of the v0.3 data
// plane (ADR-0010 D3/D6):
//
//   - pairing tokens travel in the URI path (/p/<token>/ipp/print/<key>),
//     because the Windows IPP Class Driver cannot send custom headers;
//   - routing picks the printer profile by the key segment, falling back
//     to the default (single) printer so legacy paths keep working.
//
// Server itself keeps its single-printer brain (operations.go untouched);
// the Router owns one inner Server per printer and delegates after gate +
// prefix work. An empty router (bootstrap mode) serves an honest
// "not configured yet" answer on every path.
package ipp

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
)

// Gate decides whether a request may reach the IPP data plane. token is the
// value found in the /p/<token>/ path prefix ("" when absent). Returning
// false yields a 401. The loopback exemption and token verification live in
// the wiring (cmd/dyj), not here — nil Gate allows everything (tests,
// embedded use).
type Gate func(r *http.Request, token string) bool

// Entry is one shared printer behind the Router.
type Entry struct {
	Printer Printer
	Sink    JobSink
}

// RouterOptions configures a Router.
type RouterOptions struct {
	// Entries is the printer set; the first entry is also the default for
	// paths that carry no key. May be empty (bootstrap mode).
	Entries []Entry
	// Gate, when set, is consulted before any IPP dispatch.
	Gate Gate
	// Logger for denials and routing anomalies; optional.
	Logger *log.Logger
}

// Router multiplexes printers behind one handler.
type Router struct {
	opts    RouterOptions
	inner   []*Server
	byKey   map[string]*Server
	def     *Server
	entries []Entry
}

// NewRouter builds the per-printer servers. Printers with duplicate keys
// keep the first occurrence (the caller's config validation should have
// caught that already).
func NewRouter(opts RouterOptions) *Router {
	if opts.Logger == nil {
		opts.Logger = log.Default()
	}
	rt := &Router{opts: opts, byKey: map[string]*Server{}, entries: opts.Entries}
	for _, e := range opts.Entries {
		s := NewServer(Options{Printer: e.Printer, Sink: e.Sink, Logger: opts.Logger})
		rt.inner = append(rt.inner, s)
		if e.Printer.Key != "" {
			if _, dup := rt.byKey[e.Printer.Key]; !dup {
				rt.byKey[e.Printer.Key] = s
			}
		}
		if rt.def == nil {
			rt.def = s
		}
	}
	return rt
}

// Entries exposes the configured entries (status reporting).
func (rt *Router) Entries() []Entry { return rt.entries }

// tokenPrefix is the URI marker for pairing tokens (ADR-0010 D3).
const tokenPrefix = "/p/"

// ServeHTTP implements the gate + strip + route pipeline.
func (rt *Router) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	token := ""
	if strings.HasPrefix(path, tokenPrefix) {
		rest := strings.TrimPrefix(path, tokenPrefix)
		if i := strings.IndexByte(rest, '/'); i >= 0 {
			token, path = rest[:i], rest[i:]
		} else {
			token, path = rest, "/"
		}
	}
	if rt.opts.Gate != nil && !rt.opts.Gate(r, token) {
		rt.opts.Logger.Printf("ipp: denied %s %s from %s", r.Method, logxRedact(r.URL.Path), r.RemoteAddr)
		w.Header().Set("Content-Type", ContentTypeIPP)
		w.Header().Set("WWW-Authenticate", `Basic realm="dyj paired"`)
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	s := rt.pick(path)
	if s == nil {
		rt.serveUnconfigured(w, r)
		return
	}
	// The inner Server sees the cleaned path; its permissive routing is
	// untouched.
	orig := r.URL.Path
	r.URL.Path = path
	defer func() { r.URL.Path = orig }()
	s.handle(w, r)
}

// pick resolves the printer for a token-stripped path: any path segment
// matching a printer key selects it; otherwise the default entry.
func (rt *Router) pick(path string) *Server {
	if rt.def == nil {
		return nil
	}
	for _, seg := range strings.Split(strings.Trim(path, "/"), "/") {
		if s, ok := rt.byKey[seg]; ok {
			return s
		}
	}
	return rt.def
}

// serveUnconfigured answers while the node has no printers yet (bootstrap):
// GET explains, every IPP operation honestly reports not-accepting.
func (rt *Router) serveUnconfigured(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprint(w, "dyj — not configured yet\nopen the admin UI to share a printer\n")
		return
	}
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "ipp: only POST carries IPP requests", http.StatusMethodNotAllowed)
		return
	}
	head := make([]byte, 8)
	n, _ := io.ReadFull(r.Body, head)
	writeIPP(w, errorResponse(headerRequestID(head[:n]), StatusErrorNotAcceptingJobs,
		"dyj: no printer is shared yet"), rt.opts.Logger)
}

// logxRedact keeps tokens out of log lines without importing pkg/logx
// (which would invert the layering: logx is a leaf, ipp sits above it).
// Same contract as logx.RedactPath.
func logxRedact(path string) string {
	if !strings.HasPrefix(path, tokenPrefix) {
		return path
	}
	rest := strings.TrimPrefix(path, tokenPrefix)
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		return tokenPrefix + "****" + rest[i:]
	}
	return tokenPrefix + "****"
}

// TokenPath renders the URI path a paired client should use.
func TokenPath(token, key string) string {
	if key == "" {
		return tokenPrefix + token + "/ipp/print"
	}
	return tokenPrefix + token + "/ipp/print/" + key
}
