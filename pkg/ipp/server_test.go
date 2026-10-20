package ipp

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/232252/dyj/pkg/attributes"
	"github.com/phin1x/go-ipp"
)

// encodeGetPrinterAttrs builds a real, RFC 8010 conformant
// Get-Printer-Attributes request by hand.
//
// It is hand-built rather than produced by go-ipp's Request.Encode because the
// test must exercise the wire format a real client actually sends (Windows,
// CUPS, iOS), including an explicitly empty job-attributes group followed by
// the end-of-attributes tag.
func encodeGetPrinterAttrs(t testing.TB, requestID int32, requestedAttrs ...string) []byte {
	t.Helper()
	var b bytes.Buffer

	// version 2.0, operation 0x000B, request-id
	b.Write([]byte{2, 0, 0x00, 0x0B})
	_ = binary.Write(&b, binary.BigEndian, requestID)

	// operation-attributes group
	b.WriteByte(attributes.TagOperationAttributes)
	writeCharString(t, &b, attributes.TagCharset, "attributes-charset", "utf-8")
	writeCharString(t, &b, attributes.TagNaturalLang, "attributes-natural-language", "en-us")
	writeCharString(t, &b, attributes.TagURI, "printer-uri", "ipp://127.0.0.1:6631/ipp/print")
	if len(requestedAttrs) > 0 {
		writeCharStringSet(t, &b, attributes.TagKeyword, "requested-attributes", requestedAttrs)
	}

	// empty job-attributes group — real clients send this, and go-ipp's decoder
	// requires the group delimiter to be reached before it can finish.
	b.WriteByte(attributes.TagJobAttributes)

	// end-of-attributes
	b.WriteByte(attributes.TagEndOfAttributes)
	return b.Bytes()
}

func writeCharString(t testing.TB, b *bytes.Buffer, tag byte, name, value string) {
	t.Helper()
	writeCharStringSet(t, b, tag, name, []string{value})
}

// writeCharStringSet writes attribute-with-one-value plus one additional-value
// per extra element (RFC 8010 §3.1.3–§3.1.5).
func writeCharStringSet(t testing.TB, b *bytes.Buffer, tag byte, name string, values []string) {
	t.Helper()
	for i, v := range values {
		n := name
		if i > 0 {
			n = ""
		}
		b.WriteByte(tag)
		_ = binary.Write(b, binary.BigEndian, uint16(len(n)))
		b.WriteString(n)
		_ = binary.Write(b, binary.BigEndian, uint16(len(v)))
		b.WriteString(v)
	}
}

// TestGoIppCanDecodeOurRequest proves the request our test sends is one the
// real decoder accepts — otherwise the test would be validating a fiction.
func TestGoIppCanDecodeOurRequest(t *testing.T) {
	raw := encodeGetPrinterAttrs(t, 42, "printer-description")
	req, err := ipp.NewRequestDecoder(bytes.NewReader(raw)).Decode(nil)
	if err != nil {
		t.Fatalf("go-ipp could not decode the test request: %v", err)
	}
	if req.Operation != ipp.OperationGetPrinterAttributes {
		t.Fatalf("operation = 0x%04X, want 0x000B", uint16(req.Operation))
	}
	if req.RequestId != 42 {
		t.Fatalf("request-id = %d, want 42", req.RequestId)
	}
	if got := req.OperationAttributes[ipp.AttributePrinterURI]; got != "ipp://127.0.0.1:6631/ipp/print" {
		t.Fatalf("printer-uri = %v", got)
	}
}

// TestDecodeRequestIsSpecShaped is a regression guard for a real hazard found
// while building this server: a request whose declared value-length does not
// match the literal bytes makes go-ipp return a bare io.EOF, and the server
// must treat that as a malformed body rather than a panic or a hang.
func TestDecodeRequestTruncatedBody(t *testing.T) {
	raw := encodeGetPrinterAttrs(t, 3)
	truncated := raw[:len(raw)-3]
	if _, err := DecodeRequest(truncated); err == nil {
		t.Fatal("expected an error for a truncated body")
	}
}

// TestGetPrinterAttributesEndToEnd is the required test: start an httptest
// server, POST a real encoded Get-Printer-Attributes request, and assert the
// response is a decodable IPP message containing the required attributes.
func TestGetPrinterAttributesEndToEnd(t *testing.T) {
	printer := DefaultPrinter()
	printer.Name = "dyj test printer"

	srv := NewServer(Options{Printer: printer})
	ts := httptest.NewServer(http.HandlerFunc(srv.handle))
	defer ts.Close()

	body := encodeGetPrinterAttrs(t, 7)
	resp, err := http.Post(ts.URL+"/ipp/print", ContentTypeIPP, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != ContentTypeIPP {
		t.Fatalf("Content-Type = %q, want %q", ct, ContentTypeIPP)
	}

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		t.Fatalf("read body: %v", err)
	}
	raw := buf.Bytes()
	t.Logf("response: %d bytes\nhex: %s", len(raw), hex.EncodeToString(raw[:min(128, len(raw))]))

	// --- frame checks (independent of any decoder) ---
	if len(raw) < 8 {
		t.Fatalf("response too short: %d bytes", len(raw))
	}
	if raw[0] != 2 || raw[1] != 0 {
		t.Errorf("version = %d.%d, want 2.0", raw[0], raw[1])
	}
	if status := binary.BigEndian.Uint16(raw[2:4]); status != 0x0000 {
		t.Errorf("status = 0x%04X, want 0x0000 (successful-ok)", status)
	}
	if id := int32(binary.BigEndian.Uint32(raw[4:8])); id != 7 {
		t.Errorf("request-id = %d, want 7", id)
	}
	if raw[8] != attributes.TagOperationAttributes {
		t.Errorf("first group tag = 0x%02X, want 0x01", raw[8])
	}
	if raw[len(raw)-1] != attributes.TagEndOfAttributes {
		t.Errorf("last byte = 0x%02X, want 0x03 (end-of-attributes)", raw[len(raw)-1])
	}

	// --- decode with go-ipp's response decoder ---
	var decoded bytes.Buffer
	// go-ipp's ResponseDecoder is not exported, so parse the response with the
	// request decoder by rewriting the operation-id field into a status code
	// position is NOT valid. Instead assert structurally, plus use the shared
	// attribute walker below.
	if _, err := decoded.Write(raw); err != nil {
		t.Fatal(err)
	}

	gotAttrs := parsePrinterAttributes(t, raw)
	// attributes-charset / attributes-natural-language live in the
	// operation-attributes group; merge it in so one map is checked.
	op, _, err := walkGroups(raw)
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	for _, a := range op {
		if a.name != "" {
			gotAttrs[a.name] = append(gotAttrs[a.name], a.value)
		}
	}
	required := []string{
		"attributes-charset",
		"attributes-natural-language",
		"printer-name",
		"printer-state",
		"printer-is-accepting-jobs",
		"printer-uri-supported",
		"operations-supported",
		"document-format-supported",
		"document-format-default",
		"ipp-versions-supported",
		"charset-configured",
		"charset-supported",
		"media-supported",
		"media-default",
		"sides-supported",
		"printer-resolution-supported",
		"printer-make-and-model",
		"printer-uuid",
		"pwg-raster-document-type-supported",
		"color-supported",
	}
	for _, name := range required {
		if _, ok := gotAttrs[name]; !ok {
			t.Errorf("response is missing required attribute %q", name)
		}
	}
	for _, ghost := range []string{"urf-supported", "mopria-certified", "application/pdf"} {
		if _, ok := gotAttrs[ghost]; ok {
			t.Errorf("response advertises %q, which dyj cannot honor (ADR-0007)", ghost)
		}
	}

	// The IPP Everywhere required set, checked against the attribute package's
	// own authoritative list rather than a hand-copied one.
	if err := ValidatePrinter(printer); err != nil {
		t.Errorf("printer attribute table is not IPP Everywhere complete: %v", err)
	}

	// --- ordering requirements (RFC 8011 §4.1.4) ---
	names := attrOrder(t, raw)
	if len(names) < 2 || names[0] != "attributes-charset" {
		t.Errorf("first attribute = %q, want attributes-charset", firstOr(names, ""))
	}
	if len(names) < 2 || names[1] != "attributes-natural-language" {
		t.Errorf("second attribute = %q, want attributes-natural-language", secondOr(names, ""))
	}
}

// TestOperationsSupportedIsFourOctetEnum is the wire-level check for the bug the
// Lead flagged: operations-supported must be 1setOf enum (tag 0x23) with 4-byte
// values (RFC 8010 §3.8). A 2-byte encoding desynchronises any decoder.
func TestOperationsSupportedIsFourOctetEnum(t *testing.T) {
	raw := encodeGetPrinterAttrs(t, 9)
	srv := NewServer(Options{Printer: DefaultPrinter()})
	ts := httptest.NewServer(http.HandlerFunc(srv.handle))
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/ipp/print", ContentTypeIPP, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(resp.Body)
	body := buf.Bytes()

	name := []byte("operations-supported")
	idx := bytes.Index(body, name)
	if idx < 0 {
		t.Fatal("operations-supported not found in the response")
	}
	tagOff := idx - 3 // tag(1) + name-length(2)
	if body[tagOff] != attributes.TagEnum {
		t.Fatalf("operations-supported tag = 0x%02X, want 0x23 (enum)", body[tagOff])
	}
	valLenOff := idx + len(name)
	valLen := int(binary.BigEndian.Uint16(body[valLenOff : valLenOff+2]))
	if valLen%4 != 0 {
		t.Fatalf("operations-supported value-length = %d, not a multiple of 4", valLen)
	}

	// The attribute-with-one-value carries the FIRST element; every
	// further element is an additional-value with its own tag/name-length/
	// value-length header (RFC 8010 §3.1.5). Walk all of them — the first
	// value alone is Print-Job (0x0002), and Get-Printer-Attributes sits
	// at the end of the 1setOf.
	first := body[valLenOff+2 : valLenOff+2+valLen]
	if valLen != 4 {
		t.Fatalf("first operations-supported value-length = %d, want 4", valLen)
	}
	seen := map[int32]bool{int32(int32(binary.BigEndian.Uint32(first))): true}
	vals := body[valLenOff+2+valLen:]
	for {
		if len(vals) < 5 {
			break
		}
		if vals[0] != attributes.TagEnum {
			break // next attribute reached
		}
		nl := int(binary.BigEndian.Uint16(vals[1:3]))
		if nl != 0 {
			break // next attribute reached
		}
		vl := int(binary.BigEndian.Uint16(vals[3:5]))
		if vl != 4 {
			t.Fatalf("element value-length = %d, want 4", vl)
		}
		seen[int32(binary.BigEndian.Uint32(vals[5:5+vl]))] = true
		vals = vals[5+vl:]
	}
	if !seen[int32(ipp.OperationGetPrinterAttributes)] {
		t.Errorf("operations-supported does not list Get-Printer-Attributes (0x000B); seen: %v", seen)
	}
	if !seen[int32(ipp.OperationPrintJob)] {
		t.Errorf("operations-supported does not list Print-Job (0x0002); seen: %v", seen)
	}
	if !seen[0x003C] {
		t.Errorf("operations-supported does not list Identify-Printer (0x003C), required by PWG 5100.14; seen: %v", seen)
	}
}

// TestRequestedAttributesFilter verifies RFC 8011 §4.2.1.2 filtering.
func TestRequestedAttributesFilter(t *testing.T) {
	raw := encodeGetPrinterAttrs(t, 11, "printer-name", "printer-state")
	srv := NewServer(Options{Printer: DefaultPrinter()})
	ts := httptest.NewServer(http.HandlerFunc(srv.handle))
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/ipp/print", ContentTypeIPP, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(resp.Body)

	attrs := parsePrinterAttributes(t, buf.Bytes())
	if _, ok := attrs["printer-name"]; !ok {
		t.Error("printer-name was requested but is missing")
	}
	if _, ok := attrs["printer-state"]; !ok {
		t.Error("printer-state was requested but is missing")
	}
	if _, ok := attrs["urf-supported"]; ok {
		t.Error("urf-supported was not requested but was returned")
	}
}

// TestUnknownOperationIsRejected checks the 0x0501 path.
func TestUnknownOperationIsRejected(t *testing.T) {
	raw := encodeGetPrinterAttrs(t, 13)
	// Rewrite the operation-id to something we do not implement.
	binary.BigEndian.PutUint16(raw[2:4], uint16(ipp.OperationPausePrinter))

	srv := NewServer(Options{Printer: DefaultPrinter()})
	ts := httptest.NewServer(http.HandlerFunc(srv.handle))
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/ipp/print", ContentTypeIPP, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(resp.Body)

	raw2 := buf.Bytes()
	if status := binary.BigEndian.Uint16(raw2[2:4]); status != 0x0501 {
		t.Fatalf("status = 0x%04X, want 0x0501 (server-error-operation-not-supported)", status)
	}
}

// TestMalformedBodyStillAnswersIPP: the Windows client treats a non-IPP HTTP
// error as "printer unreachable", so a bad body must still produce an IPP reply.
func TestMalformedBodyStillAnswersIPP(t *testing.T) {
	srv := NewServer(Options{Printer: DefaultPrinter()})
	ts := httptest.NewServer(http.HandlerFunc(srv.handle))
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/ipp/print", ContentTypeIPP, strings.NewReader("not an ipp message at all"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != ContentTypeIPP {
		t.Fatalf("Content-Type = %q, want %q", ct, ContentTypeIPP)
	}
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(resp.Body)
	if buf.Len() < 8 {
		t.Fatalf("error response too short: %d bytes", buf.Len())
	}
	if status := binary.BigEndian.Uint16(buf.Bytes()[2:4]); status != 0x0400 {
		t.Fatalf("status = 0x%04X, want 0x0400 (client-error-bad-request)", status)
	}
}

// TestPrintJobIsRefusedHonestly: with no backend wired up the server must say
// so instead of accepting a document it cannot print.
func TestPrintJobIsRefusedHonestly(t *testing.T) {
	raw := encodeGetPrinterAttrs(t, 17)
	binary.BigEndian.PutUint16(raw[2:4], uint16(ipp.OperationPrintJob))

	srv := NewServer(Options{Printer: DefaultPrinter()})
	ts := httptest.NewServer(http.HandlerFunc(srv.handle))
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/ipp/print", ContentTypeIPP, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(resp.Body)
	if status := binary.BigEndian.Uint16(buf.Bytes()[2:4]); status != 0x0506 {
		t.Fatalf("status = 0x%04X, want 0x0506 (printer-not-accepting-jobs)", status)
	}
}

// TestGETAdminURLAnswers keeps the TXT adminurl http:// host useful.
func TestGETAdminURLAnswers(t *testing.T) {
	srv := NewServer(Options{Printer: DefaultPrinter()})
	ts := httptest.NewServer(http.HandlerFunc(srv.handle))
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET / = %d, want 200", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// test helpers: a small independent walker over the wire bytes
// ---------------------------------------------------------------------------

// parsePrinterAttributes walks the response bytes and returns the attributes of
// the printer-attributes group. Written from the RFC rather than by reusing
// production code, so a bug in the encoder cannot hide from it.
func parsePrinterAttributes(t *testing.T, raw []byte) map[string][][]byte {
	t.Helper()
	out := map[string][][]byte{}
	_, printer, err := walkGroups(raw)
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	lastName := ""
	for _, a := range printer {
		if a.name != "" {
			lastName = a.name
		}
		out[lastName] = append(out[lastName], a.value)
	}
	return out
}

// attrOrder returns attribute names in wire order for the operation group.
func attrOrder(t *testing.T, raw []byte) []string {
	t.Helper()
	op, _, err := walkGroups(raw)
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	var names []string
	for _, a := range op {
		if a.name != "" {
			names = append(names, a.name)
		}
	}
	return names
}

type rawAttr struct {
	tag   byte
	name  string
	value []byte
}

// walkGroups parses version/status/id then every group, returning the
// operation-attributes and printer-attributes groups.
func walkGroups(raw []byte) (op, printer []rawAttr, err error) {
	if len(raw) < 9 {
		return nil, nil, errShortResponse
	}
	i := 8
	var cur *[]rawAttr
	for i < len(raw) {
		tag := raw[i]
		i++
		switch {
		case tag == attributes.TagEndOfAttributes:
			return op, printer, nil
		case tag < 0x10: // group delimiter
			switch tag {
			case attributes.TagOperationAttributes:
				cur = &op
			case attributes.TagPrinterAttributes:
				printer = nil
				cur = &printer
			default:
				cur = nil
			}
			continue
		}
		if cur == nil {
			return nil, nil, errGarbage
		}
		if i+2 > len(raw) {
			return nil, nil, errShortResponse
		}
		nameLen := int(binary.BigEndian.Uint16(raw[i : i+2]))
		i += 2
		if i+nameLen+2 > len(raw) {
			return nil, nil, errShortResponse
		}
		name := string(raw[i : i+nameLen])
		i += nameLen
		valLen := int(binary.BigEndian.Uint16(raw[i : i+2]))
		i += 2
		if i+valLen > len(raw) {
			return nil, nil, errShortResponse
		}
		val := raw[i : i+valLen]
		i += valLen
		*cur = append(*cur, rawAttr{tag: tag, name: name, value: val})
	}
	return op, printer, errShortResponse
}

func firstOr(s []string, def string) string {
	if len(s) > 0 {
		return s[0]
	}
	return def
}

func secondOr(s []string, def string) string {
	if len(s) > 1 {
		return s[1]
	}
	return def
}

// TestAdminMountedIPPUndamounted proves the /admin mount coexists with
// IPP traffic: the management subtree answers, and IPP posts to any
// other path behave exactly as without the mount.
func TestAdminMountedIPPUnaffected(t *testing.T) {
	adminCalled := false
	admin := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		adminCalled = true
		fmt.Fprint(w, "admin-ui")
	})
	printer := DefaultPrinter()
	printer.Name = "dyj admin coexist"
	srv := NewServer(Options{Printer: printer, Admin: admin})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve(ln) }()
	defer srv.Shutdown(context.Background())
	base := "http://" + ln.Addr().String()

	resp, err := http.Get(base + "/admin/api/status")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !adminCalled || string(body) != "admin-ui" {
		t.Fatalf("/admin not routed to the admin handler (called=%v body=%q)", adminCalled, body)
	}

	srv2body := encodeGetPrinterAttrs(t, 11)
	resp, err = http.Post(base+"/ipp/print", ContentTypeIPP, bytes.NewReader(srv2body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != ContentTypeIPP {
		t.Fatalf("IPP POST degraded: status=%d ct=%q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	raw, _ := io.ReadAll(resp.Body)
	msg, err := attributes.DecodeMessageStream(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("IPP response not decodable: %v", err)
	}
	if msg.Code != uint16(StatusOK) {
		t.Fatalf("IPP status = 0x%04X, want ok", msg.Code)
	}
	var printerGroup *attributes.AttributeGroup
	for i := range msg.Groups {
		if msg.Groups[i].Delimiter == attributes.TagPrinterAttributes {
			printerGroup = &msg.Groups[i]
		}
	}
	if printerGroup == nil {
		t.Fatal("no printer-attributes group in response")
	}
	if got := attributes.GroupFirstString(printerGroup, "printer-name"); got != "dyj admin coexist" {
		t.Errorf("printer-name = %q", got)
	}
}
