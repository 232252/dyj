package pairing

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/232252/dyj/pkg/logx"
)

type fakeClock struct{ now time.Time }

func (c *fakeClock) Add(d time.Duration) { c.now = c.now.Add(d) }

func newTestStore(t *testing.T, path string, lim Limits) (*Store, *fakeClock, *bytes.Buffer) {
	t.Helper()
	clock := &fakeClock{now: time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)}
	var buf bytes.Buffer
	events := logx.New(&buf, logx.LevelDebug)
	s, err := Open(path, lim, events)
	if err != nil {
		t.Fatal(err)
	}
	// Open stamps the first PIN with the wall clock, before the test
	// clock is installed. Re-base it or a fixture date in the past never
	// expires relative to that stamp.
	s.now = func() time.Time { return clock.now }
	s.mu.Lock()
	s.rotateLocked("test-clock")
	s.mu.Unlock()
	return s, clock, &buf
}

func TestPINRollsOverTime(t *testing.T) {
	s, clock, _ := newTestStore(t, "", Limits{})
	pin1, ttl := s.Current()
	if len(pin1) != 4 || ttl <= 0 {
		t.Fatalf("pin %q ttl %d", pin1, ttl)
	}
	clock.Add(11 * time.Minute)
	pin2, _ := s.Current()
	if pin1 == pin2 {
		t.Fatal("pin must rotate after TTL")
	}
	// Old pin no longer pairs.
	if _, err := s.Pair(pin1, "dev1", "n", "1.1.1.1"); !errors.Is(err, ErrBadPin) {
		t.Fatalf("old pin: err=%v, want ErrBadPin", err)
	}
}

func TestManualRotateInvalidatesOldPin(t *testing.T) {
	s, _, _ := newTestStore(t, "", Limits{})
	old, _ := s.Current()
	s.Rotate()
	if _, err := s.Pair(old, "d", "n", "1.1.1.1"); !errors.Is(err, ErrBadPin) {
		t.Fatalf("err=%v, want ErrBadPin", err)
	}
}

func TestPairIssuesVerifiableToken(t *testing.T) {
	s, _, _ := newTestStore(t, "", Limits{})
	pin, _ := s.Current()
	tok, err := s.Pair(pin, "dev1", "小米本", "192.168.1.23")
	if err != nil {
		t.Fatal(err)
	}
	if id, ok := s.Verify(tok); !ok || id != "dev1" {
		t.Fatalf("verify: id=%q ok=%v", id, ok)
	}
	if _, ok := s.Verify("bogus"); ok {
		t.Fatal("bogus token must not verify")
	}
}

func TestRePairRotatesToken(t *testing.T) {
	s, _, _ := newTestStore(t, "", Limits{})
	pin, _ := s.Current()
	tok1, _ := s.Pair(pin, "dev1", "n", "1.1.1.1")
	tok2, _ := s.Pair(pin, "dev1", "n2", "1.1.1.1")
	if _, ok := s.Verify(tok1); ok {
		t.Fatal("old token must die on re-pair")
	}
	if _, ok := s.Verify(tok2); !ok {
		t.Fatal("new token must verify")
	}
	if got := s.List(); len(got) != 1 || got[0].Name != "n2" {
		t.Fatalf("device list: %+v", got)
	}
}

func TestRevokeKillsToken(t *testing.T) {
	s, _, _ := newTestStore(t, "", Limits{})
	pin, _ := s.Current()
	tok, _ := s.Pair(pin, "dev1", "n", "1.1.1.1")
	if !s.Revoke("dev1") {
		t.Fatal("revoke should succeed")
	}
	if _, ok := s.Verify(tok); ok {
		t.Fatal("revoked token must not verify")
	}
	if s.Revoke("dev1") {
		t.Fatal("double revoke should report false")
	}
}

func TestRateLimitLocksAfterMaxFails(t *testing.T) {
	lim := Limits{MaxFails: 3, LockDuration: 30 * time.Minute}
	s, clock, buf := newTestStore(t, "", lim)
	pin, _ := s.Current()
	for i := 0; i < 3; i++ {
		if _, err := s.Pair("0009", "d", "n", "9.9.9.9"); !errors.Is(err, ErrBadPin) {
			t.Fatalf("attempt %d: err=%v", i, err)
		}
	}
	// Correct pin is also refused while locked.
	var le *LockedError
	_, err := s.Pair(pin, "d", "n", "9.9.9.9")
	if !errors.As(err, &le) {
		t.Fatalf("want LockedError, got %v", err)
	}
	if !strings.Contains(buf.String(), "evt=pair.lock") {
		t.Fatalf("lock event missing: %q", buf.String())
	}
	// Other IPs are unaffected.
	if _, err := s.Pair(pin, "dev2", "n", "8.8.8.8"); err != nil {
		t.Fatalf("unrelated ip locked: %v", err)
	}
	// After the lock expires the IP may try again (with the — meanwhile
	// rolled — current PIN).
	clock.Add(31 * time.Minute)
	pin2, _ := s.Current()
	if pin2 == pin {
		t.Fatal("pin should have rolled during the lock")
	}
	if _, err := s.Pair(pin2, "dev3", "n", "9.9.9.9"); err != nil {
		t.Fatalf("after lock: %v", err)
	}
	if !strings.Contains(buf.String(), "evt=pair.unlock") {
		t.Fatalf("unlock event missing: %q", buf.String())
	}
}

func TestFailuresOutsideWindowDecay(t *testing.T) {
	lim := Limits{MaxFails: 3, FailWindow: time.Minute}
	s, clock, _ := newTestStore(t, "", lim)
	for i := 0; i < 2; i++ {
		_, _ = s.Pair("0000", "d", "n", "7.7.7.7")
	}
	clock.Add(2 * time.Minute) // first two failures aged out
	for i := 0; i < 2; i++ {
		if _, err := s.Pair("0000", "d", "n", "7.7.7.7"); !errors.Is(err, ErrBadPin) {
			t.Fatalf("should still be ErrBadPin (aged window), got %v", err)
		}
	}
}

func TestPersistenceRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "paired-devices.json")
	s, _, _ := newTestStore(t, path, Limits{})
	pin, _ := s.Current()
	tok, _ := s.Pair(pin, "devA", "pad", "1.2.3.4")
	s.Close()

	s2, _, _ := newTestStore(t, path, Limits{})
	if id, ok := s2.Verify(tok); !ok || id != "devA" {
		t.Fatalf("token must survive restart: id=%q ok=%v", id, ok)
	}
	if got := s2.List(); len(got) != 1 || got[0].Name != "pad" {
		t.Fatalf("loaded devices: %+v", got)
	}
	// The file must not contain the token in clear text, only its hash.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), tok) {
		t.Fatal("token stored in clear text")
	}
}

func TestEventsRedactNothingSensitive(t *testing.T) {
	s, _, buf := newTestStore(t, "", Limits{})
	pin, _ := s.Current()
	tok, _ := s.Pair(pin, "devX", "n", "1.1.1.1")
	out := buf.String()
	if strings.Contains(out, tok) {
		t.Fatal("token leaked into events")
	}
	if strings.Contains(out, "\""+pin+"\"") || strings.HasSuffix(strings.TrimSpace(strings.Split(out, "\n")[0]), "pin="+pin) {
		t.Fatal("pin leaked into events")
	}
}

// ---------------------------------------------------------------------------
// HTTP handler
// ---------------------------------------------------------------------------

func newTestHandler(t *testing.T) (*Store, *httptest.Server) {
	t.Helper()
	s, _, _ := newTestStore(t, "", Limits{})
	h := &Handler{
		Store:      s,
		Printers:   func() []SharedPrinter { return []SharedPrinter{{Key: "epson", Name: "Epson"}} },
		ServerName: func() string { return "lab" },
	}
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	return s, ts
}

func postPair(t *testing.T, url string, body string) (int, map[string]any) {
	t.Helper()
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func TestHandlerPairFlow(t *testing.T) {
	s, ts := newTestHandler(t)
	pin, _ := s.Current()

	code, out := postPair(t, ts.URL+"/pair", `{"pin":"0000","device_id":"d1","device_name":"本"}`)
	if code != http.StatusUnauthorized || out["error"] != "bad_pin" {
		t.Fatalf("bad pin: %d %v", code, out)
	}
	code, out = postPair(t, ts.URL+"/pair", `{"pin":"`+pin+`","device_id":"d1","device_name":"本"}`)
	if code != http.StatusOK {
		t.Fatalf("good pin: %d %v", code, out)
	}
	tok, _ := out["token"].(string)
	if tok == "" {
		t.Fatal("no token in response")
	}
	printers, _ := out["printers"].([]any)
	if len(printers) != 1 {
		t.Fatalf("printers: %v", printers)
	}
	p0 := printers[0].(map[string]any)
	if p0["uri_path"] != "/p/"+tok+"/ipp/print/epson" {
		t.Fatalf("uri_path: %v", p0)
	}
	if id, ok := s.Verify(tok); !ok || id != "d1" {
		t.Fatalf("handler-issued token must verify")
	}
}

func TestHandlerValidation(t *testing.T) {
	s, ts := newTestHandler(t)
	if code, out := postPair(t, ts.URL+"/pair", `not json`); code != http.StatusBadRequest {
		t.Fatalf("bad json: %d %v", code, out)
	}
	if code, out := postPair(t, ts.URL+"/pair", `{"device_id":"d"}`); code != http.StatusBadRequest || out["error"] != "pin_required" {
		t.Fatalf("no pin: %d %v", code, out)
	}
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/pair", nil)
	if resp, err := http.DefaultClient.Do(req); err != nil || resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("GET /pair must 405, got %v %v", err, resp)
	}
	// Lockout shape.
	for i := 0; i < 10; i++ {
		postPair(t, ts.URL+"/pair", `{"pin":"0001","device_id":"d","device_name":"n"}`)
	}
	pin, _ := s.Current()
	code, out := postPair(t, ts.URL+"/pair", `{"pin":"`+pin+`","device_id":"d","device_name":"n"}`)
	if code != http.StatusForbidden || out["error"] != "locked" {
		t.Fatalf("locked: %d %v", code, out)
	}
	if ra, ok := out["retry_after_sec"].(float64); !ok || ra <= 0 {
		t.Fatalf("retry_after_sec: %v", out)
	}
}
