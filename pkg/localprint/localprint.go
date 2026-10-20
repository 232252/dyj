// Package localprint enumerates the printers installed on THIS machine —
// the input of the share-side wizard (research/13 §4.5). Windows shells
// down to winspool.drv (EnumPrintersW); Linux/macOS reuse the CUPS query.
//
// The virtual-printer filter is pure and table-driven so it is testable on
// every platform: wizard lists hide "Microsoft Print to PDF"-style queues
// by default, with an explicit "show virtual" escape hatch in the UI.
package localprint

import "strings"

// Printer is one locally installed queue.
type Printer struct {
	Name    string `json:"name"`
	Driver  string `json:"driver,omitempty"`
	Port    string `json:"port,omitempty"`
	Default bool   `json:"default,omitempty"`
	Virtual bool   `json:"virtual"`
	// ProbeErr is set by ProbeAll when the queue cannot be opened.
	ProbeErr string `json:"probe_err,omitempty"`
}

// virtualPortNames never carry paper. Keys are lower-case.
var virtualPortNames = map[string]bool{
	"portprompt:": true, // "Prompt for filename" (Print to PDF et al.)
	"nul:":        true,
	"shrfax:":     true, // Windows Fax
}

// virtualDriverNames are document writers, not printers. Keys are
// lower-case.
var virtualDriverNames = map[string]bool{
	"microsoft print to pdf":        true,
	"microsoft xps document writer": true,
	"microsoft print to pdf (v4)":   true,
}

// virtualNameExact and virtualNameHints catch the stragglers that have no
// distinctive driver or port.
var (
	virtualNameExact = map[string]bool{"fax": true}
	virtualNameHints = []string{"onenote"}
)

// IsVirtual reports whether a queue writes to a file/dialog/fax instead of
// a physical device. All comparisons are case-insensitive.
func IsVirtual(name, driver, port string) bool {
	port = strings.ToLower(strings.TrimSpace(port))
	if virtualPortNames[port] {
		return true
	}
	driver = strings.ToLower(strings.TrimSpace(driver))
	if virtualDriverNames[driver] {
		return true
	}
	n := strings.ToLower(strings.TrimSpace(name))
	if virtualNameExact[n] {
		return true
	}
	for _, h := range virtualNameHints {
		if strings.Contains(n, h) {
			return true
		}
	}
	// Catch-alls for third-party document writers whose driver or port
	// carries the word "virtual" (measured: Kingsoft/WPS "导出为WPS PDF"
	// on "Kingsoft Virtual Printer Port").
	if strings.Contains(port, "virtual") || strings.Contains(driver, "virtual") {
		return true
	}
	return false
}

// Slug derives the stable URI key for a queue name: lowercase, [a-z0-9-],
// collapsed separators, with a safe fallback (identical rules to the TLS
// host folding in cmd/dyj).
func Slug(name string) string {
	var b strings.Builder
	lastDash := false
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'A' && c <= 'Z':
			c += 'a' - 'A'
			b.WriteByte(c)
			lastDash = false
		case (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9'):
			b.WriteByte(c)
			lastDash = false
		default:
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "printer"
	}
	return out
}

// ProbeAll opens each queue in turn and fills ProbeErr for the ones that
// refuse — catching "shared but unopenable" inside the wizard (checkpoint
// SHARE-2) instead of at first print.
func ProbeAll(ps []Printer) []Printer {
	for i := range ps {
		if err := Probe(ps[i].Name); err != nil {
			ps[i].ProbeErr = err.Error()
		}
	}
	return ps
}
