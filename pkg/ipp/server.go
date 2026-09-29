package ipp

import (
	"compress/gzip"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/sq199/dyj/pkg/attributes"
)

// ContentTypeIPP is the only Content-Type we accept and emit.
const ContentTypeIPP = "application/ipp"

// Status codes (RFC 8011 appendix B).
const (
	StatusOK                          int16 = 0x0000
	StatusErrorBadRequest             int16 = 0x0400
	StatusErrorNotFound               int16 = 0x0406
	StatusErrorClientUnsupported      int16 = 0x040C
	StatusErrorAttributesNotSupported int16 = 0x040B
	StatusErrorDocumentAccess         int16 = 0x0412
	StatusErrorNotPossible            int16 = 0x0503
	StatusErrorOperationNotSupported  int16 = 0x0501
)

// Server is the HTTP(S) transport around the IPP operation handlers.
//
// It is deliberately a plain net/http server: one handler, one content
// type, no middleware stack. Everything protocol-specific lives in
// operations.go / template.go; document bytes stream straight from the
// HTTP body into the JobSink — a 100 MB raster job is never buffered.
type Server struct {
	printer Printer
	sink    JobSink
	log     *log.Logger
	http    *http.Server
	ln      net.Listener
}

// requestContext carries the per-request plumbing the operation
// handlers need beyond the decoded message.
type requestContext struct {
	body          io.Reader
	contentLength int64
	remoteAddr    string
}

// Options configures a Server.
type Options struct {
	Printer Printer
	// Sink receives jobs. nil keeps the honest "not accepting jobs"
	// prototype behaviour (0x0506 for every job operation).
	Sink JobSink
	// Status is an optional dynamic-state hook; Sink provides it when
	// it implements StatusProvider, so usually it is left nil.
	Logger *log.Logger
	// Admin, when set, is mounted at /admin (web management UI). IPP
	// traffic keeps any other path, so clients are unaffected.
	Admin http.Handler
}

// NewServer creates an IPP server for the given printer profile.
func NewServer(opts Options) *Server {
	if opts.Logger == nil {
		opts.Logger = log.Default()
	}
	s := &Server{printer: opts.Printer, sink: opts.Sink, log: opts.Logger}
	var handler http.Handler = http.HandlerFunc(s.handle)
	if opts.Admin != nil {
		mux := http.NewServeMux()
		mux.Handle("/admin", opts.Admin)
		mux.Handle("/admin/", opts.Admin)
		mux.Handle("/", handler)
		handler = mux
	}
	s.http = &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		MaxHeaderBytes:    1 << 16,
		// No ReadTimeout: a client streaming a 100 MB raster over a slow
		// link must not be cut off mid-body. Idle connections are reaped
		// by IdleTimeout; active bodies are bounded by the peer.
		IdleTimeout: 120 * time.Second,
	}
	return s
}

// Serve starts serving on ln.
func (s *Server) Serve(ln net.Listener) error {
	s.ln = ln
	err := s.http.Serve(ln)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// ServeTLS starts serving TLS on ln (for ipps:// / AirPrint-style
// transport security).
func (s *Server) ServeTLS(ln net.Listener, certFile, keyFile string) error {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return fmt.Errorf("ipp: load TLS keypair: %w", err)
	}
	s.ln = tls.NewListener(ln, &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	})
	err = s.http.Serve(s.ln)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// ListenAndServe binds addr and serves until Shutdown.
func (s *Server) ListenAndServe(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	return s.Serve(ln)
}

// Addr reports the bound address, or "" before serving.
func (s *Server) Addr() string {
	if s.ln == nil {
		return ""
	}
	return s.ln.Addr().String()
}

// Shutdown stops the server.
func (s *Server) Shutdown(ctx context.Context) error { return s.http.Shutdown(ctx) }

// Printer returns the served printer profile.
func (s *Server) Printer() Printer { return s.printer }

// handle is the single HTTP entry point. Every path is served: clients are
// remarkably inconsistent about the resource path ("/", "/ipp/print",
// "/printers/dyj", "/ipp/printer", ...) and rejecting a path costs us the
// whole "add printer" flow for no benefit.
func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		// Browsers and some probes GET the admin URL from the TXT record.
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintf(w, "dyj IPP endpoint\nPOST IPP here (Content-Type: application/ipp)\nprinter: %s\nuri: %s\n",
			s.printer.Name, s.printer.primaryURI())
		return
	}
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST, GET, HEAD")
		http.Error(w, "ipp: only POST carries IPP requests", http.StatusMethodNotAllowed)
		return
	}

	body, err := decodeBody(r)
	if err != nil {
		s.log.Printf("ipp: body setup failed from %s: %v", r.RemoteAddr, err)
		http.Error(w, "ipp: cannot read request body", http.StatusBadRequest)
		return
	}

	msg, err := DecodeRequestStream(body)
	if err != nil {
		// A malformed IPP body must still get an IPP-shaped answer where
		// possible: the Windows client treats a non-IPP 4xx as "unreachable".
		s.log.Printf("ipp: decode failed from %s: %v", r.RemoteAddr, err)
		resp := NewResponse(StatusErrorBadRequest, requestIDOf(r))
		resp.Groups = []Group{{Tag: GroupOperation, Attrs: append(BuildOperationAttrs(),
			Attr{Tag: TagText, Name: "status-message", Values: [][]byte{Text("dyj: malformed IPP request")}})}}
		s.writeIPP(w, resp)
		return
	}

	s.log.Printf("ipp: %s op=%s(0x%04X) reqID=%d from=%s",
		r.URL.Path, DescribeOperation(msg.Code), msg.Code, msg.RequestID, r.RemoteAddr)

	rc := &requestContext{body: msg.DataReader, contentLength: r.ContentLength, remoteAddr: r.RemoteAddr}
	resp := s.dispatch(msg, rc)
	if resp == nil {
		resp = NewResponse(StatusErrorOperationNotSupported, msg.RequestID)
		resp.Groups = []Group{{Tag: GroupOperation, Attrs: append(BuildOperationAttrs(),
			Attr{Tag: TagText, Name: "status-message", Values: [][]byte{Text("dyj: operation not supported")}})}}
	}
	s.writeIPP(w, resp)
}

// maxRequestBody bounds the IPP document stream. A full PWG raster job
// can be large; 128 MiB covers every realistic photo/label/document job
// while capping what a hostile peer can push into the spool. Note there
// is deliberately no ReadTimeout: a client streaming 100 MB over a slow
// link is a NORMAL print job, not an attack, and the size cap plus the
// header/idle timeouts are the resource boundary that matters.
const maxRequestBody = 128 << 20 // 128 MiB

// decodeBody returns the request body reader, transparently
// decompressing when the client used HTTP-level Content-Encoding: gzip.
// (IPP-level compression is separate and handled per-document in
// openDocument — a client may legitimately use both.)
func decodeBody(r *http.Request) (io.Reader, error) {
	var inner io.Reader = io.LimitReader(r.Body, maxRequestBody+1)
	switch r.Header.Get("Content-Encoding") {
	case "", "identity":
		return inner, nil
	case "gzip":
		return gzip.NewReader(inner)
	default:
		return nil, fmt.Errorf("unsupported Content-Encoding %q", r.Header.Get("Content-Encoding"))
	}
}

// requestIDOf fishes the request-id out of a body we failed to decode,
// so the error response can still be correlated by the client.
func requestIDOf(r *http.Request) int32 {
	br, ok := r.Body.(interface{ Peek(int) ([]byte, error) })
	if !ok {
		return 0
	}
	head, err := br.Peek(8)
	if err != nil || len(head) < 8 {
		return 0
	}
	return int32(uint32(head[4])<<24 | uint32(head[5])<<16 | uint32(head[6])<<8 | uint32(head[7]))
}

// writeIPP emits the response bytes with the mandatory content type.
func (s *Server) writeIPP(w http.ResponseWriter, resp *Response) {
	buf, err := resp.Encode()
	if err != nil {
		s.log.Printf("ipp: response encode failed: %v", err)
		http.Error(w, "ipp: response encode failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", ContentTypeIPP)
	w.Header().Set("Content-Length", fmt.Sprint(len(buf)))
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(buf); err != nil {
		s.log.Printf("ipp: write failed: %v", err)
	}
}

// NormalizePath is used by tests to assert the permissive routing.
func NormalizePath(p string) string {
	if p == "" {
		return "/"
	}
	if p[0] != '/' {
		return "/" + p
	}
	return p
}

// SchemeHostOf extracts scheme://host:port from an ipp(s) URI.
func SchemeHostOf(uri string) (host string, secure bool) {
	secure = len(uri) >= 5 && uri[:5] == "ipps:"
	s := uri
	if i := indexSubstring(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	if i := indexByteStr(s, '/'); i >= 0 {
		s = s[:i]
	}
	return s, secure
}

func indexSubstring(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func indexByteStr(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}

// StatusProvider lets a JobSink hand its Status method to the Server
// without the server importing the concrete type.
type StatusProvider interface {
	Status() (accepting bool, queued int32, reasons []string)
}

// statusHook extracts the printerStatus from a sink, if it provides one.
func statusHook(sink JobSink) printerStatus {
	if sp, ok := sink.(StatusProvider); ok {
		return sp.Status
	}
	return nil
}

// BuildOperationAttrs returns the operation-attributes group of a
// response: attributes-charset first, then attributes-natural-language —
// the order is a hard requirement (RFC 8011 §4.1.4) and pkg/attributes
// deliberately leaves this group to the server.
func BuildOperationAttrs() []Attr {
	return []Attr{
		attributes.Text(attributes.TagCharset, "attributes-charset", "utf-8"),
		attributes.Text(attributes.TagNaturalLang, "attributes-natural-language", "en-us"),
	}
}
