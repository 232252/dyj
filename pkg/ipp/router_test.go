package ipp

import (
	"bytes"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// buildGetPrinterRequest builds a minimal but well-formed IPP
// Get-Printer-Attributes request (2.0, op 0x000B, id 7).
func buildGetPrinterRequest() []byte {
	var b bytes.Buffer
	b.Write([]byte{0x02, 0x00, 0x00, 0x0B, 0, 0, 0, 7}) // ver, op, reqID
	b.WriteByte(byte(GroupOperation))
	// attributes-charset utf-8
	b.WriteByte(byte(TagCharset))
	b.Write([]byte{0, byte(len("attributes-charset"))})
	b.WriteString("attributes-charset")
	b.Write([]byte{0, 5})
	b.WriteString("utf-8")
	// attributes-natural-language en-us
	b.WriteByte(byte(TagNaturalLang))
	b.Write([]byte{0, byte(len("attributes-natural-language"))})
	b.WriteString("attributes-natural-language")
	b.Write([]byte{0, 5})
	b.WriteString("en-us")
	b.WriteByte(0x03) // end-of-attributes
	return b.Bytes()
}

func postIPP(t *testing.T, url string, body []byte) *http.Response {
	t.Helper()
	resp, err := http.Post(url, ContentTypeIPP, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	return resp
}

func TestRouterTokenPrefixStrippedAndRouted(t *testing.T) {
	a := DefaultPrinter()
	a.Name = "Alpha"
	a.Key = "alpha"
	b := DefaultPrinter()
	b.Name = "Beta"
	b.Key = "beta"
	rt := NewRouter(RouterOptions{
		Entries: []Entry{{Printer: a}, {Printer: b}},
		Gate:    func(r *http.Request, token string) bool { return token == "sekrit" },
		Logger:  log.New(io.Discard, "", 0),
	})
	ts := httptest.NewServer(rt)
	defer ts.Close()

	// Wrong token → 401.
	if resp := postIPP(t, ts.URL+"/p/wrong/ipp/print/alpha", buildGetPrinterRequest()); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong token: status %d, want 401", resp.StatusCode)
	}
	// Right token + key → 200 from the addressed printer.
	if resp := postIPP(t, ts.URL+"/p/sekrit/ipp/print/beta", buildGetPrinterRequest()); resp.StatusCode != http.StatusOK {
		t.Fatalf("good token: status %d, want 200", resp.StatusCode)
	}
	// No token → gate sees "" → denied.
	if resp := postIPP(t, ts.URL+"/ipp/print", buildGetPrinterRequest()); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no token: status %d, want 401", resp.StatusCode)
	}
}

func TestRouterFallsBackToDefaultEntry(t *testing.T) {
	a := DefaultPrinter()
	a.Key = "alpha"
	rt := NewRouter(RouterOptions{Entries: []Entry{{Printer: a}}})
	ts := httptest.NewServer(rt)
	defer ts.Close()

	for _, path := range []string{"/", "/ipp/print", "/printers/whatever", "/ipp/print/not-a-key"} {
		if resp := postIPP(t, ts.URL+path, buildGetPrinterRequest()); resp.StatusCode != http.StatusOK {
			t.Fatalf("path %s: status %d, want 200 (default entry)", path, resp.StatusCode)
		}
	}
}

func TestRouterKeyRouting(t *testing.T) {
	a := DefaultPrinter()
	a.Name = "Alpha"
	a.Key = "alpha"
	b := DefaultPrinter()
	b.Name = "Beta"
	b.Key = "beta"
	rt := NewRouter(RouterOptions{Entries: []Entry{{Printer: a}, {Printer: b}}})
	ts := httptest.NewServer(rt)
	defer ts.Close()

	for _, path := range []string{"/ipp/print/beta", "/printers/beta", "/beta"} {
		if resp := postIPP(t, ts.URL+path, buildGetPrinterRequest()); resp.StatusCode != http.StatusOK {
			t.Fatalf("path %s: status %d, want 200", path, resp.StatusCode)
		}
	}
}

func TestRouterBootstrapModeAnswersNotAccepting(t *testing.T) {
	rt := NewRouter(RouterOptions{})
	ts := httptest.NewServer(rt)
	defer ts.Close()

	if resp := postIPP(t, ts.URL+"/ipp/print", buildGetPrinterRequest()); resp.StatusCode != http.StatusOK {
		t.Fatalf("POST: status %d, want 200 with IPP error body", resp.StatusCode)
	}
	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "not configured") {
		t.Fatalf("GET body should explain the bootstrap state, got %q", body)
	}
}

func TestRouterGateNilAllowsAll(t *testing.T) {
	a := DefaultPrinter()
	rt := NewRouter(RouterOptions{Entries: []Entry{{Printer: a}}})
	ts := httptest.NewServer(rt)
	defer ts.Close()
	if resp := postIPP(t, ts.URL+"/ipp/print", buildGetPrinterRequest()); resp.StatusCode != http.StatusOK {
		t.Fatalf("nil gate must allow, got %d", resp.StatusCode)
	}
}

func TestRouterDenialLogRedactsToken(t *testing.T) {
	var buf bytes.Buffer
	a := DefaultPrinter()
	rt := NewRouter(RouterOptions{
		Entries: []Entry{{Printer: a}},
		Gate:    func(r *http.Request, token string) bool { return false },
		Logger:  log.New(&buf, "", 0),
	})
	ts := httptest.NewServer(rt)
	defer ts.Close()
	postIPP(t, ts.URL+"/p/TOPSECRET/ipp/print", buildGetPrinterRequest())
	if strings.Contains(buf.String(), "TOPSECRET") {
		t.Fatalf("token leaked into denial log: %q", buf.String())
	}
	if !strings.Contains(buf.String(), "/p/****") {
		t.Fatalf("denial log missing masked path: %q", buf.String())
	}
}

func TestTokenPath(t *testing.T) {
	if got := TokenPath("AB12", ""); got != "/p/AB12/ipp/print" {
		t.Fatalf("single: %s", got)
	}
	if got := TokenPath("AB12", "epson"); got != "/p/AB12/ipp/print/epson" {
		t.Fatalf("keyed: %s", got)
	}
}
