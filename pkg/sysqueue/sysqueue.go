// Package sysqueue installs, verifies and removes SYSTEM print queues that
// point at a remote IPP endpoint — the receive side's "add printer" step
// (research/13 §4.6). After Install returns, the queue is a plain OS
// citizen: WPS/Word/anything prints through it with dyj nowhere in the
// path.
//
// Windows: Add-Printer -IppUrl (Microsoft IPP Class Driver, driverless).
// The IPP URL — which carries the pairing token in its path — never
// appears in Result.Cmd/Result.Output unmasked; use Describe for logs.
//
// Linux/macOS: lpadmin -m everywhere.
package sysqueue

import (
	"errors"
	"strings"

	"github.com/sq199/dyj/pkg/logx"
)

// psQuote single-quotes for PowerShell (embedded quotes double).
func psQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

// Spec describes the queue to create.
type Spec struct {
	// Name is the queue name visible in print dialogs.
	Name string `json:"name"`
	// IPPURL is the target, e.g. ipp://host:6631/p/<token>/ipp/print/<key>.
	IPPURL string `json:"ipp_url"`
}

// Result reports what happened, safe for logs and UI.
type Result struct {
	// Cmd is the human/log-safe command shape (token masked).
	Cmd string `json:"cmd"`
	// Output is the (truncated) tool output.
	Output string `json:"output"`
	// Elevated is true when the work ran through the privileged helper.
	Elevated bool `json:"elevated"`
}

// ErrExists: the queue name is taken (InstallAuto retries with " (2)").
var ErrExists = errors.New("sysqueue: queue name already exists")

// Describe renders a spec log-safely: the pairing token inside the URL
// path is masked (logx contract).
func (s Spec) Describe() string { return logx.RedactPath(s.IPPURL) }

func firstLines(s string, n int) string {
	s = strings.TrimSpace(s)
	lines := strings.Split(s, "\n")
	if len(lines) <= n {
		return s
	}
	return strings.Join(lines[:n], "\n") + " …"
}

// uniqueName appends " (2)", " (3)", … when name is taken.
func uniqueName(name string, taken func(string) bool) string {
	if !taken(name) {
		return name
	}
	for i := 2; i < 99; i++ {
		candidate := name + " (" + itoa(i) + ")"
		if !taken(candidate) {
			return candidate
		}
	}
	return name + " (99)"
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	var b [8]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	return string(b[i:])
}
