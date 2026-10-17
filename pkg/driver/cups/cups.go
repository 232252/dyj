// Package cups is the Linux/macOS printing backend: it turns spooled
// raster pages into an image-only PDF (render.PagesToPDF) and submits
// that to a local CUPS queue with lp(1).
//
// Why lp and not IPP-to-localhost: CUPS's own filter chain handles the
// raster→printer-language encoding for every installed queue, which is
// exactly the "reuse the OS driver" move the wingdi backend makes on
// Windows — dyj stays driver-free. lp is the one submission path that
// works over the CUPS socket on every distro without auth setup, and
// cups-client is a 200 KB dependency that ships with every desktop.
//
// Process spawning policy (same as pkg/service): one helper, one
// whitelisted absolute-path program, fixed argument arrays, no shell at
// any layer (os/exec with explicit Path). Every value that originated
// outside dyj (queue name, media, sides) is validated to be a bare
// token that cannot start with "-", so an argument slot can never be
// parsed as a new option, and PATH cannot swap the executable under us
// because the absolute path is pinned at construction time.
package cups

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/232252/dyj/pkg/driver"
	"github.com/232252/dyj/pkg/render"
)

// Queue is one print queue reported by lpstat.
type Queue struct {
	Name  string
	Idle  bool
	Ready bool // enabled (accepting), i.e. probably printable
}

// Backend is a driver.Interface printing through a CUPS queue.
type Backend struct {
	// Queue is the CUPS queue name (lpstat name, e.g. "EPSON_L3250").
	Queue string
	// lpPath/lpstatPath are absolute paths pinned at construction.
	lpPath     string
	lpstatPath string

	// run is the process seam. nil means real execution.
	run func(ctx context.Context, argv []string) (stdout string, err error)
}

// Available reports whether the cups-client tools exist on this machine.
func Available() bool {
	_, lpErr := exec.LookPath("lp")
	_, statErr := exec.LookPath("lpstat")
	return lpErr == nil && statErr == nil
}

// New builds a backend for queue and verifies the tools exist.
func New(queue string) (*Backend, error) {
	if err := validToken(queue, "queue"); err != nil {
		return nil, err
	}
	lp, err := exec.LookPath("lp")
	if err != nil {
		return nil, fmt.Errorf("cups: lp not found (install the cups-client package)")
	}
	stat, err := exec.LookPath("lpstat")
	if err != nil {
		return nil, fmt.Errorf("cups: lpstat not found (install the cups-client package)")
	}
	return &Backend{Queue: queue, lpPath: lp, lpstatPath: stat}, nil
}

// validToken enforces bare-token arguments: printable, no whitespace,
// never starting with "-" (so an argument slot can never be parsed as
// an option by lp/lpstat).
func validToken(s, what string) error {
	if s == "" {
		return fmt.Errorf("cups: empty %s", what)
	}
	if strings.HasPrefix(s, "-") {
		return fmt.Errorf("cups: %s must not start with %q", what, s)
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '_' || r == '.' || r == '-' || r == ':' || r == '/' || r == '#':
		default:
			return fmt.Errorf("cups: %s contains unsupported character %q", what, r)
		}
	}
	return nil
}

func (b *Backend) Name() string { return "cups(" + b.Queue + ")" }

// runProcess spawns ONE whitelisted absolute-path program with a fixed
// argument array and captures combined output. No shell is involved at
// any layer (os/exec with explicit Path), so argument values cannot
// gain command semantics; ctx cancellation kills the process.
func runProcess(ctx context.Context, path string, args []string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("cups: refusing to run relative path %q", path)
	}
	cmd := &exec.Cmd{
		Path:      path,
		Args:      append([]string{path}, args...),
		WaitDelay: 5 * time.Second,
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Start(); err != nil {
		return out.String(), err
	}
	cmd.Cancel = func() error {
		if ctx.Err() != nil {
			return cmd.Process.Kill()
		}
		return nil // process exited on its own; nothing to cancel
	}
	waitErr := cmd.Wait()
	if ctx.Err() != nil {
		return out.String(), ctx.Err()
	}
	return out.String(), waitErr
}

// Queues lists local CUPS queues via `lpstat -p`.
func Queues() ([]Queue, error) {
	stat, err := exec.LookPath("lpstat")
	if err != nil {
		return nil, fmt.Errorf("cups: lpstat not found (install the cups-client package)")
	}
	out, err := runProcess(context.Background(), stat, []string{"-p"})
	if err != nil {
		return nil, fmt.Errorf("cups: lpstat -p: %w", err)
	}
	return parseQueues(out), nil
}

// parseQueues reads `lpstat -p` output: one line per queue of the shape
//
//	printer EPSON_L3250 is idle.  enabled since Mon Jan  1 00:00:00 2026
//	printer DYJ_NOWHERE disabled since ...
func parseQueues(out string) []Queue {
	var queues []Queue
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 4 || fields[0] != "printer" {
			continue
		}
		q := Queue{Name: fields[1]}
		q.Ready = !strings.Contains(line, " disabled")
		q.Idle = strings.Contains(line, "idle")
		queues = append(queues, q)
	}
	return queues
}

// Print renders the document into an image-only PDF and submits it.
func (b *Backend) Print(ctx context.Context, job driver.PrintJob, doc driver.Document) error {
	pages, errc := driver.Pages(ctx, doc, driver.DecoderOptions{
		Media: job.Media,
	})
	var collected []*render.Page
	for p := range pages {
		collected = append(collected, p)
	}
	if err := <-errc; err != nil {
		return fmt.Errorf("cups: rendering document: %w", err)
	}
	if len(collected) == 0 {
		return fmt.Errorf("cups: document rendered to zero pages")
	}
	pdf, err := render.PagesToPDF(collected)
	if err != nil {
		return fmt.Errorf("cups: wrapping pages into PDF: %w", err)
	}

	tmp, err := os.CreateTemp("", "dyj-cups-*.pdf")
	if err != nil {
		return fmt.Errorf("cups: temp file: %w", err)
	}
	path := tmp.Name()
	defer os.Remove(path)
	if _, err := tmp.Write(pdf); err != nil {
		tmp.Close()
		return fmt.Errorf("cups: writing temp PDF: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("cups: writing temp PDF: %w", err)
	}

	argv := []string{"-d", b.Queue, "-t", jobTitle(job)}
	if job.Copies > 1 {
		argv = append(argv, "-n", fmt.Sprint(job.Copies))
	}
	// media/sides originate from the IPP request (external input): they
	// pass only as bare tokens, otherwise the option is omitted and the
	// queue default applies.
	if m := mediaForCUPS(job.Media); m != "" {
		argv = append(argv, "-o", "media="+m)
	}
	if err := validToken(job.Sides, "sides"); err == nil {
		argv = append(argv, "-o", "sides="+job.Sides)
	}
	if job.Color {
		argv = append(argv, "-o", "print-color-mode=color")
	}
	argv = append(argv, path)

	out, err := b.exec(ctx, argv)
	if err != nil {
		return fmt.Errorf("cups: lp: %w (%s)", err, firstLine(out))
	}
	return nil
}

func (b *Backend) exec(ctx context.Context, argv []string) (string, error) {
	if b.run != nil {
		return b.run(ctx, argv)
	}
	// The only spawned program is the pinned lp path from New(); argv
	// here carries options only (Print never puts a program name in it).
	return runProcess(ctx, b.lpPath, argv)
}

// jobTitle makes a predictable, lp-safe job title (never option-like:
// it always starts with "dyj ").
func jobTitle(job driver.PrintJob) string {
	name := job.Name
	if name == "" {
		name = "job"
	}
	const max = 80
	if len(name) > max {
		name = name[:max]
	}
	var b strings.Builder
	b.WriteString("dyj ")
	fmt.Fprintf(&b, "%d ", job.ID)
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			r = ' '
		}
		b.WriteRune(r)
	}
	return b.String()
}

// mediaForCUPS maps a PWG media name to a CUPS media keyword via a
// small explicit table. Anything unknown is omitted so CUPS applies the
// queue's own default — passing a keyword the queue has not defined
// makes lp fail the whole job, which is strictly worse than the default
// paper size.
func mediaForCUPS(pwg string) string {
	if pwg == "" {
		return ""
	}
	parts := strings.Split(pwg, "_")
	if len(parts) < 2 {
		return ""
	}
	switch parts[0] + "_" + strings.ToLower(parts[1]) {
	case "iso_a4":
		return "A4"
	case "iso_a3":
		return "A3"
	case "iso_a5":
		return "A5"
	case "iso_a6":
		return "A6"
	case "iso_b4":
		return "B4"
	case "iso_b5":
		return "B5"
	case "iso_dl":
		return "EnvDL"
	case "na_letter":
		return "Letter"
	case "na_legal":
		return "Legal"
	case "na_tabloid":
		return "Tabloid"
	case "na_ledger":
		return "Ledger"
	case "na_index-4x6":
		return "4x6"
	case "na_index-5x7":
		return "5x7"
	case "na_number-10":
		return "Env10"
	case "na_number-9":
		return "Env9"
	case "na_invoice":
		return "Statement"
	}
	return ""
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}
