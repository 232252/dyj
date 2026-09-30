package main

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestNormalizeMigratesLegacySinglePrinter(t *testing.T) {
	o := normalizeServeOptions(serveOptions{
		Printer: "EPSON L3250 Series",
		Backend: "wingdi",
		Port:    0,
	})
	if len(o.Printers) != 1 {
		t.Fatalf("want 1 printer, got %d", len(o.Printers))
	}
	p := o.Printers[0]
	if p.Queue != "EPSON L3250 Series" || p.Backend != "wingdi" || p.Key != "epson-l3250-series" {
		t.Fatalf("migrated spec: %+v", p)
	}
	if o.Printer != "" || o.Backend != "" {
		t.Fatalf("legacy fields must clear: %+v", o)
	}
	if o.Port != 6631 {
		t.Fatalf("default port: %d", o.Port)
	}
}

func TestNormalizeSynthesizesQueuelessFileBackend(t *testing.T) {
	o := normalizeServeOptions(serveOptions{Backend: "file", Name: "e2e-printer"})
	if len(o.Printers) != 1 || o.Printers[0].Name != "e2e-printer" || o.Printers[0].Queue != "" {
		t.Fatalf("file backend without queue: %+v", o.Printers)
	}
}

func TestNormalizeDedupKeys(t *testing.T) {
	o := normalizeServeOptions(serveOptions{
		Printers: []printerSpec{
			{Name: "HP 1010"},
			{Name: "hp 1010"}, // same slug
		},
	})
	if o.Printers[0].Key != "hp-1010" || o.Printers[1].Key != "hp-1010-2" {
		t.Fatalf("keys: %q %q", o.Printers[0].Key, o.Printers[1].Key)
	}
}

func TestNormalizeRoleValidation(t *testing.T) {
	if normalizeServeOptions(serveOptions{Role: "bogus"}).Role != "" {
		t.Fatal("bogus role must fold to bootstrap")
	}
	for _, r := range []string{"", "share", "use", "both"} {
		if normalizeServeOptions(serveOptions{Role: r}).Role != r {
			t.Fatalf("role %q must survive", r)
		}
	}
}

func TestParseServeOptionsJSON(t *testing.T) {
	bad := []string{
		`not json`,
		`{"role":"wizard"}`,
		`{"port":-1}`,
		`{"port":70000}`,
		`{"tls_port":6631,"port":6631}`,
		`{"printers":[{"name":"x","backend":"laser"}]}`,
		`{"backend":"laser","printers":[{"name":"x"}]}`,
	}
	for _, raw := range bad {
		if _, err := parseServeOptionsJSON([]byte(raw)); err == nil {
			t.Errorf("must reject %s", raw)
		}
	}
	good := `{"role":"share","port":6631,"printers":[{"name":"办公室","queue":"EPSON"}]}`
	o, err := parseServeOptionsJSON([]byte(good))
	if err != nil {
		t.Fatal(err)
	}
	if o.Role != roleShare || len(o.Printers) != 1 || o.Printers[0].Queue != "EPSON" {
		t.Fatalf("parsed: %+v", o)
	}
	// Round-trip through JSON keeps the shape (admin config GET/PUT).
	b, _ := json.Marshal(o)
	var back serveOptions
	if err := json.Unmarshal(b, &back); err != nil || len(back.Printers) != 1 {
		t.Fatalf("round trip: %v %+v", err, back)
	}
}

func TestParseServeOptionsDefaultsPort(t *testing.T) {
	o, err := parseServeOptionsJSON([]byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if o.Port != 6631 {
		t.Fatalf("empty config must default the port, got %d", o.Port)
	}
}

func TestIsLoopbackRequest(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:123", "[::1]:44", "localhost:5"} {
		r := httptest.NewRequest("POST", "/", nil)
		r.RemoteAddr = addr
		if !isLoopbackRequest(r) {
			if addr == "localhost:5" {
				continue // "localhost" is not an IP literal; acceptable
			}
			t.Errorf("%s must be loopback", addr)
		}
	}
	r := httptest.NewRequest("POST", "/", nil)
	r.RemoteAddr = "192.168.1.9:5555"
	if isLoopbackRequest(r) {
		t.Fatal("LAN address must not be loopback")
	}
}
