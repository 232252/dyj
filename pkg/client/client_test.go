package client

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/232252/dyj/pkg/attributes"
)

// group is one response attribute group for the builder below.
type group struct {
	tag   byte
	attrs []attributes.Attribute
}

// ippResponse builds a minimal IPP response frame: version 2.0, the given
// status code, request id 1, a mandatory operation group, then the caller's
// groups in order.
func ippResponse(status uint16, groups ...group) []byte {
	var b bytes.Buffer
	b.Write([]byte{2, 0, byte(status >> 8), byte(status)})
	_ = binary.Write(&b, binary.BigEndian, int32(1))
	b.WriteByte(attributes.TagOperationAttributes)
	_ = attributes.WriteAttributes(&b, []attributes.Attribute{
		attributes.Text(attributes.TagCharset, "attributes-charset", "utf-8"),
		attributes.Text(attributes.TagNaturalLang, "attributes-natural-language", "en-us"),
	})
	for _, g := range groups {
		b.WriteByte(g.tag)
		_ = attributes.WriteAttributes(&b, g.attrs)
	}
	b.WriteByte(attributes.TagEndOfAttributes)
	return b.Bytes()
}

func serveIPP(t *testing.T, status uint16, groups ...group) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/ipp")
		_, _ = w.Write(ippResponse(status, groups...))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// ippURI maps a test server's http URL onto the ipp scheme the parser wants.
func ippURI(rawURL, scheme, path string) string {
	host := strings.TrimPrefix(strings.TrimPrefix(rawURL, "http://"), "https://")
	return scheme + "://" + host + path
}

func TestParseRejectsBadURIs(t *testing.T) {
	for _, uri := range []string{
		"",
		"host:631/ipp/print",
		"http://host:631/ipp/print",
		"ipp://",
		"ipp:///ipp/print",
		"ipp://host:631",
		"ipp://host:631/",
		"ipp://host:notaport/ipp/print",
	} {
		if _, err := Parse(uri, "dyj"); err == nil {
			t.Errorf("Parse(%q) = nil error, want rejection", uri)
		}
	}
	c, err := Parse("ipp://host:631/ipp/print", "dyj")
	if err != nil {
		t.Fatalf("good URI rejected: %v", err)
	}
	if c.URI != "ipp://host:631/ipp/print" || c.User != "dyj" || c.HTTP == nil {
		t.Errorf("client = %+v", c)
	}
}

func TestGetPrinterAttributes(t *testing.T) {
	var gotOp uint16
	var gotURI, gotWant string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		msg, err := attributes.DecodeMessageStream(bytes.NewReader(body))
		if err != nil {
			t.Errorf("server could not decode request: %v", err)
		} else {
			gotOp = msg.Code
			gotURI = attributes.GroupFirstString(msg.Operation(), "printer-uri")
			gotWant = attributes.GroupFirstString(msg.Operation(), "requested-attributes")
		}
		w.Header().Set("Content-Type", "application/ipp")
		_, _ = w.Write(ippResponse(0, group{attributes.TagPrinterAttributes,
			[]attributes.Attribute{attributes.Name("printer-name", "office")}}))
	}))
	defer srv.Close()

	c, err := Parse(ippURI(srv.URL, "ipp", "/ipp/print"), "dyj")
	if err != nil {
		t.Fatal(err)
	}
	msg, err := c.GetPrinterAttributes("printer-name")
	if err != nil {
		t.Fatal(err)
	}
	if name := attributes.GroupFirstString(msg.Group(attributes.TagPrinterAttributes), "printer-name"); name != "office" {
		t.Errorf("printer-name = %q", name)
	}
	if gotOp != 0x000B {
		t.Errorf("request op-id = 0x%04X, want 0x000B", gotOp)
	}
	if gotURI != c.URI {
		t.Errorf("request printer-uri = %q, want %q", gotURI, c.URI)
	}
	if gotWant != "printer-name" {
		t.Errorf("requested-attributes = %q", gotWant)
	}
}

func TestPrintJobRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		name       string
		opt        PrintOptions
		wantCopies bool
	}{{
		name: "plain",
		opt:  PrintOptions{JobName: "resume.pdf"},
	}, {
		name: "gzip with copies",
		opt:  PrintOptions{JobName: "resume.pdf", Copies: 3, Gzip: true},
	}} {
		t.Run(tc.name, func(t *testing.T) {
			doc := "the actual document bytes"
			var compression, copiesOK, docOK bool
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				msg, err := attributes.DecodeMessageStream(bytes.NewReader(body))
				if err != nil {
					t.Errorf("server could not decode request: %v", err)
					return
				}
				compression = attributes.GroupFirstString(msg.Operation(), "compression") == "gzip"
				if g := msg.Job(); g != nil {
					if v, ok := attributes.GroupInt(g, "copies"); ok && v == 3 {
						copiesOK = true
					}
				}
				raw, _ := io.ReadAll(msg.DataReader)
				if tc.opt.Gzip {
					if zr, err := gzip.NewReader(bytes.NewReader(raw)); err == nil {
						raw, _ = io.ReadAll(zr)
					}
				}
				docOK = string(raw) == doc
				w.Header().Set("Content-Type", "application/ipp")
				_, _ = w.Write(ippResponse(0, group{attributes.TagJobAttributes,
					[]attributes.Attribute{attributes.Integer("job-id", 7)}}))
			}))
			defer srv.Close()

			c, err := Parse(ippURI(srv.URL, "ipp", "/ipp/print"), "dyj")
			if err != nil {
				t.Fatal(err)
			}
			id, err := c.PrintJob(strings.NewReader(doc), int64(len(doc)), "application/pdf", tc.opt)
			if err != nil {
				t.Fatal(err)
			}
			if id != 7 {
				t.Errorf("job id = %d, want 7", id)
			}
			if compression != tc.opt.Gzip {
				t.Errorf("compression=gzip declared = %v, want %v", compression, tc.opt.Gzip)
			}
			if !docOK {
				t.Error("server received wrong document bytes")
			}
			if tc.wantCopies && !copiesOK {
				t.Error("request carried no copies=3 job attribute")
			}
		})
	}
}

func TestPrintJobServerRejects(t *testing.T) {
	srv := serveIPP(t, 0x0400)
	c, err := Parse(ippURI(srv.URL, "ipp", "/ipp/print"), "dyj")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.PrintJob(strings.NewReader("x"), 1, "application/pdf", PrintOptions{}); err == nil {
		t.Fatal("Print-Job with 0x0400 status must fail")
	} else if !strings.Contains(err.Error(), "0x0400") {
		t.Errorf("error %q lacks status code", err)
	}
}

func TestJobsParsesRows(t *testing.T) {
	srv := serveIPP(t, 0,
		group{attributes.TagJobAttributes, []attributes.Attribute{
			attributes.Integer("job-id", 11), attributes.Integer("job-state", 5),
			attributes.Name("job-name", "report"), attributes.Name("job-originating-user-name", "alice"),
			attributes.MimeType("document-format", "application/pdf"),
		}},
		group{attributes.TagJobAttributes, []attributes.Attribute{
			attributes.Integer("job-id", 12), attributes.Integer("job-state", 9),
			attributes.Name("job-name", "photo"),
		}},
	)
	c, err := Parse(ippURI(srv.URL, "ipp", "/ipp/print"), "dyj")
	if err != nil {
		t.Fatal(err)
	}
	rows, err := c.Jobs("not-completed")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	want := []JobRow{
		{ID: 11, State: 5, Name: "report", User: "alice", Format: "application/pdf"},
		{ID: 12, State: 9, Name: "photo"},
	}
	for i, w := range want {
		if rows[i] != w {
			t.Errorf("row %d = %+v, want %+v", i, rows[i], w)
		}
	}
}

func TestCancelRoundTrip(t *testing.T) {
	var gotID int32
	var gotOp uint16
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		msg, err := attributes.DecodeMessageStream(bytes.NewReader(body))
		if err != nil {
			t.Errorf("server could not decode request: %v", err)
			return
		}
		gotOp = msg.Code
		gotID, _ = attributes.GroupInt(msg.Operation(), "job-id")
		_, _ = w.Write(ippResponse(0))
	}))
	defer srv.Close()

	c, err := Parse(ippURI(srv.URL, "ipp", "/ipp/print"), "dyj")
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Cancel(42); err != nil {
		t.Fatal(err)
	}
	if gotOp != 0x0008 {
		t.Errorf("request op-id = 0x%04X, want 0x0008 (Cancel-Job)", gotOp)
	}
	if gotID != 42 {
		t.Errorf("request job-id = %d, want 42", gotID)
	}
}

func TestHTTPStatusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c, err := Parse(ippURI(srv.URL, "ipp", "/p/dead/ipp/print"), "dyj")
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.GetPrinterAttributes()
	var he *HTTPStatusError
	if !errors.As(err, &he) {
		t.Fatalf("error %v is not HTTPStatusError", err)
	}
	if he.Status != http.StatusUnauthorized {
		t.Errorf("status = %d", he.Status)
	}
	want := "client: HTTP 401 from " + ippURI(srv.URL, "http", "/p/dead/ipp/print")
	if he.Error() != want {
		t.Errorf("message = %q, want %q", he.Error(), want)
	}
}

func TestParseInsecureAcceptsSelfSigned(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(ippResponse(0, group{attributes.TagPrinterAttributes,
			[]attributes.Attribute{attributes.Name("printer-name", "airprint")}}))
	}))
	defer srv.Close()

	uri := ippURI(srv.URL, "ipps", "/ipp/print")
	c, err := ParseInsecure(uri, "dyj")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.GetPrinterAttributes(); err != nil {
		t.Errorf("insecure client over self-signed TLS: %v", err)
	}
	strict, err := Parse(uri, "dyj")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := strict.GetPrinterAttributes(); err == nil {
		t.Error("verifying client must reject the self-signed certificate")
	}
}
