package cups

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sq199/dyj/pkg/driver"
)

func TestValidToken(t *testing.T) {
	for _, ok := range []string{"EPSON_L3250", "dyj-printer.2", "a/b#c", "Office:HP"} {
		if err := validToken(ok, "queue"); err != nil {
			t.Errorf("validToken(%q) = %v, want nil", ok, err)
		}
	}
	for _, bad := range []string{"", "-o", "--help", "a b", "a\tb", "x;rm", "$(x)", "a\nb", "示例"} {
		if err := validToken(bad, "queue"); err == nil {
			t.Errorf("validToken(%q) = nil, want error", bad)
		}
	}
}

func TestNewRejectsOptionLikeQueue(t *testing.T) {
	if _, err := New("-o"); err == nil {
		t.Fatal("New(\"-o\") should reject option-like queue names")
	}
}

func TestParseQueues(t *testing.T) {
	out := `printer EPSON_L3250 is idle.  enabled since Mon Sep 28 09:00:00 2026
printer DYJ_FILE is idle.  enabled since Mon Sep 28 09:00:00 2026
printer BROKEN One disabled since Sun Sep 27 01:02:03 2026 -
	Bad queue line that should be ignored
`
	qs := parseQueues(out)
	if len(qs) != 3 {
		t.Fatalf("got %d queues, want 3: %+v", len(qs), qs)
	}
	if qs[0].Name != "EPSON_L3250" || !qs[0].Idle || !qs[0].Ready {
		t.Errorf("qs[0] = %+v", qs[0])
	}
	if qs[2].Name != "BROKEN" || qs[2].Ready {
		t.Errorf("disabled queue parsed wrong: %+v", qs[2])
	}
	if qs[2].Idle {
		t.Errorf("disabled queue should not be idle: %+v", qs[2])
	}
}

// seamBackend builds a backend without the LookPath gate so the print
// path is testable on any OS (Windows dev machines have no lp).
func seamBackend(t *testing.T, queue string, recorded *[]string) *Backend {
	t.Helper()
	b := &Backend{Queue: queue, lpPath: "/usr/bin/lp", lpstatPath: "/usr/bin/lpstat"}
	b.run = func(ctx context.Context, argv []string) (string, error) {
		*recorded = append(*recorded, strings.Join(argv, " "))
		path := argv[len(argv)-1]
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		if !strings.HasPrefix(string(data), "%PDF-1.4") {
			return "", fmt.Errorf("temp file is not a PDF: %q", data[:16])
		}
		if !strings.Contains(string(data), "/Im0 Do") {
			return "", fmt.Errorf("PDF has no page content stream")
		}
		return "request id is " + queue + "-42 (1 file(s))", nil
	}
	return b
}

func testBackend(t *testing.T, recorded *[]string) *Backend {
	t.Helper()
	return seamBackend(t, "TESTQ", recorded)
}

func textDoc(s string) driver.Document {
	return driver.Document{
		Format: "text/plain",
		Size:   int64(len(s)),
		Open: func() (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader(s)), nil
		},
	}
}

func TestPrintSubmitsPDFWithJobOptions(t *testing.T) {
	var recorded []string
	b := testBackend(t, &recorded)

	err := b.Print(context.Background(), driver.PrintJob{
		ID: 7, User: "alice", Name: "notes",
		Media: "iso_a4_210x297mm", Copies: 2, Sides: "two-sided-long-edge", Color: true,
	}, textDoc("hello dyj\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(recorded) != 1 {
		t.Fatalf("lp invoked %d times, want 1", len(recorded))
	}
	argv := recorded[0]
	for _, want := range []string{
		"-d TESTQ",
		"-t dyj 7 notes",
		"-n 2",
		"-o media=A4",
		"-o sides=two-sided-long-edge",
		"-o print-color-mode=color",
	} {
		if !strings.Contains(argv, want) {
			t.Errorf("lp args missing %q: %s", want, argv)
		}
	}
}

func TestPrintUnknownMediaOmitted(t *testing.T) {
	var recorded []string
	b := testBackend(t, &recorded)
	err := b.Print(context.Background(), driver.PrintJob{
		ID: 1, Name: "x", Media: "oe_weird_123x456mm",
	}, textDoc("x\n"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(recorded[0], "media=") {
		t.Errorf("unknown PWG media must be omitted, got: %s", recorded[0])
	}
}

func TestPrintRogueSidesDropped(t *testing.T) {
	var recorded []string
	b := testBackend(t, &recorded)
	// A sides value that somehow bypassed the template validator must
	// not reach lp as an option.
	err := b.Print(context.Background(), driver.PrintJob{
		ID: 1, Name: "x", Sides: "-oSystemCommand",
	}, textDoc("x\n"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(recorded[0], "sides=") {
		t.Errorf("rogue sides value reached lp: %s", recorded[0])
	}
}

func TestPrintLPFailureSurfaces(t *testing.T) {
	b := seamBackend(t, "TESTQ", &[]string{})
	b.run = func(ctx context.Context, argv []string) (string, error) {
		return "lp: Error - queue not found.", errors.New("exit status 1")
	}
	err := b.Print(context.Background(), driver.PrintJob{Name: "x"}, textDoc("x\n"))
	if err == nil || !strings.Contains(err.Error(), "queue not found") {
		t.Fatalf("error should carry lp's first output line, got %v", err)
	}
}

func TestMediaForCUPS(t *testing.T) {
	cases := map[string]string{
		"iso_a4_210x297mm":         "A4",
		"iso_a3_297x420mm":         "A3",
		"na_letter_8.5x11in":       "Letter",
		"na_legal_8.5x14in":        "Legal",
		"na_index-4x6_4x6in":       "4x6",
		"iso_dl_110x220mm":         "EnvDL",
		"na_number-10_4.125x9.5in": "Env10",
		"custom_1x2in_1x2in":       "",
		"oe_weird_123x456mm":       "",
		"na_unknown-name_1x2in":    "",
		"":                         "",
		"nosize":                   "",
	}
	for in, want := range cases {
		if got := mediaForCUPS(in); got != want {
			t.Errorf("mediaForCUPS(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestJobTitleSanitized(t *testing.T) {
	got := jobTitle(driver.PrintJob{ID: 3, Name: "bad\tcontrol\x01name"})
	if !strings.HasPrefix(got, "dyj 3 bad") || strings.ContainsAny(got, "\t\x01") {
		t.Errorf("control characters survived: %q", got)
	}
	long := strings.Repeat("x", 200)
	if got := jobTitle(driver.PrintJob{ID: 1, Name: long}); len(got) > 100 {
		t.Errorf("title not truncated: %d bytes", len(got))
	}
	if got := jobTitle(driver.PrintJob{ID: 1}); got != "dyj 1 job" {
		t.Errorf("empty name default = %q", got)
	}
	_ = filepath.Join // keep import if tests evolve
}
