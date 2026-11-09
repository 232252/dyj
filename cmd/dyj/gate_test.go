package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/232252/dyj/pkg/attributes"
	"github.com/232252/dyj/pkg/driver"
	"github.com/232252/dyj/pkg/ipp"
	"github.com/232252/dyj/pkg/logx"
	"github.com/232252/dyj/pkg/pairing"
	"github.com/232252/dyj/pkg/spooler"
)

// newGateNode builds a node with a live in-memory pairing store and a fake
// clock; advance moves the clock the open-printing window is measured
// against. pairing keeps its own real clock — the test never lives long
// enough for the PIN to rotate.
func newGateNode(t *testing.T) (n *node, token string, advance func(time.Duration)) {
	t.Helper()
	store, err := pairing.Open("", pairing.Limits{}, logx.New(io.Discard, logx.LevelDebug))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	pin, _ := store.Current()
	token, err = store.Pair(pin, "dev1", "pad", "10.0.0.9")
	if err != nil {
		t.Fatal(err)
	}
	cur := time.Unix(1_700_000_000, 0)
	n = &node{pair: store, log: logx.New(io.Discard, logx.LevelDebug)}
	n.nowFn = func() time.Time { return cur }
	return n, token, func(d time.Duration) { cur = cur.Add(d) }
}

// TestGateLoopbackTokenAndOpenWindow pins the data-plane gate order
// (ADR-0014): loopback, then the open-printing window/flag — checked
// BEFORE the token check, because a tokenless request must reach it —
// then a valid pairing token.
func TestGateLoopbackTokenAndOpenWindow(t *testing.T) {
	n, token, advance := newGateNode(t)
	g := n.gate()
	req := func(remote string) *http.Request {
		return &http.Request{RemoteAddr: remote}
	}

	if !g(req("127.0.0.1:55555"), "") {
		t.Fatal("loopback must pass")
	}
	if !g(req("[::1]:55555"), "") {
		t.Fatal("loopback v6 must pass")
	}
	if g(req("10.0.0.10:4444"), "") {
		t.Fatal("closed + tokenless must be denied")
	}
	if !g(req("10.0.0.10:4444"), token) {
		t.Fatal("valid token must pass from anywhere")
	}
	if g(req("10.0.0.10:4444"), "bogus") {
		t.Fatal("bogus token must be denied")
	}

	// 开放窗口:任意来源免令牌放行;过期(惰性清零)后回到拒绝。
	n.openSetWindow(10 * time.Minute)
	if !g(req("10.0.0.10:4444"), "") {
		t.Fatal("open window must allow tokenless from any ip")
	}
	if !g(req("10.0.0.9:4444"), "") {
		t.Fatal("open window must allow tokenless from the paired ip too")
	}
	advance(10 * time.Minute)
	if g(req("10.0.0.10:4444"), "") {
		t.Fatal("expired window must be denied")
	}

	// 常开标志(cfg.OpenPrint,持久化)同样放行。
	n.mu.Lock()
	n.cfg = serveOptions{OpenPrint: true}
	n.mu.Unlock()
	if !g(req("10.0.0.10:4444"), "") {
		t.Fatal("always-on must allow tokenless")
	}
}

// TestGateLogsAllowForOpenPrint keeps the open-mode allow auditable, the
// mirror image of the deny.ipp line.
func TestGateLogsAllowForOpenPrint(t *testing.T) {
	n, _, _ := newGateNode(t)
	var buf bytes.Buffer
	n.log = logx.New(&buf, logx.LevelInfo)
	n.openSetWindow(time.Minute)
	n.gate()(&http.Request{RemoteAddr: "10.0.0.7:1"}, "")
	if !strings.Contains(buf.String(), "allow.ipp") {
		t.Fatalf("allow.ipp not logged:\n%s", buf.String())
	}
}

// TestOpenPrintEndToEnd drives the real Router behind n.gate() with a LAN
// source address: tokenless Get-Printer-Attributes is refused while
// closed, answered while the open window runs, and refused again after the
// DELETE. The request is forged into the handler (real RemoteAddr is
// always loopback in-process) but the gate, the 401 path and the IPP
// decoding are the production ones.
func TestOpenPrintEndToEnd(t *testing.T) {
	n, _, _ := newGateNode(t)
	backend, err := driver.NewFile(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	spool, err := spooler.New(spooler.Options{Dir: t.TempDir(), Backend: backend})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = spool.Close(ctx)
	}()
	p := ipp.DefaultPrinter()
	p.Name = "gate e2e"
	p.Key = "gate"
	p.AcceptingJobs = true
	rt := ipp.NewRouter(ipp.RouterOptions{
		Entries: []ipp.Entry{{Printer: p, Sink: spool}},
		Gate:    n.gate(),
	})

	post := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "http://dyj.test/ipp/print", bytes.NewReader(ippGetPrinterFrame(t)))
		r.RemoteAddr = "10.0.0.10:4400" // 非 loopback:必须过 gate
		r.Header.Set("Content-Type", "application/ipp")
		w := httptest.NewRecorder()
		rt.ServeHTTP(w, r)
		return w
	}

	if w := post(); w.Code != http.StatusUnauthorized {
		t.Fatalf("closed: want 401, got %d", w.Code)
	}

	if w := openPost(t, n, `{"duration_sec":600}`); w.Code != http.StatusOK {
		t.Fatalf("open POST: %d %s", w.Code, w.Body.String())
	}
	w := post()
	if w.Code != http.StatusOK {
		t.Fatalf("open window: want 200, got %d", w.Code)
	}
	if msg, err := attributes.DecodeMessageStream(bytes.NewReader(w.Body.Bytes())); err != nil || msg.Code != 0 {
		t.Fatalf("open window: bad IPP answer: %v code 0x%04X", err, msgCode(msg, err))
	}

	dw := httptest.NewRecorder()
	n.apiOpen(dw, httptest.NewRequest(http.MethodDelete, "/admin/api/open", nil))
	if dw.Code != http.StatusOK {
		t.Fatalf("open DELETE: %d", dw.Code)
	}
	if w := post(); w.Code != http.StatusUnauthorized {
		t.Fatalf("closed again: want 401, got %d", w.Code)
	}
}

// ippGetPrinterFrame encodes a minimal Get-Printer-Attributes request with
// the shared attribute encoder (the single wire truth).
func ippGetPrinterFrame(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	b.Write([]byte{2, 0, 0x00, 0x0B}) // version 2.0, Get-Printer-Attributes
	_ = binary.Write(&b, binary.BigEndian, int32(1)) // request-id 1
	b.WriteByte(attributes.TagOperationAttributes)
	attrs := []attributes.Attribute{
		attributes.Text(attributes.TagCharset, "attributes-charset", "utf-8"),
		attributes.Text(attributes.TagNaturalLang, "attributes-natural-language", "en-us"),
		attributes.URI("printer-uri", "ipp://dyj.test/ipp/print"),
	}
	if err := attributes.WriteAttributes(&b, attrs); err != nil {
		t.Fatal(err)
	}
	b.WriteByte(attributes.TagEndOfAttributes)
	return b.Bytes()
}

func msgCode(msg *attributes.Message, err error) uint16 {
	if err != nil || msg == nil {
		return 0xFFFF
	}
	return msg.Code
}

// openPost posts a JSON body to n.apiOpen (the same handler the admin API
// mounts) and returns the recorder.
func openPost(t *testing.T, n *node, body string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	n.apiOpen(w, httptest.NewRequest(http.MethodPost, "/admin/api/open", strings.NewReader(body)))
	return w
}
