//go:build !windows

package localprint

import (
	"fmt"

	"github.com/232252/dyj/pkg/driver/cups"
)

// List maps the CUPS queue set (lpstat -p) onto the platform-independent
// shape. CUPS queues are never "virtual" in the Windows sense.
func List() ([]Printer, error) {
	qs, err := cups.Queues()
	if err != nil {
		return nil, fmt.Errorf("localprint: %w", err)
	}
	out := make([]Printer, 0, len(qs))
	for _, q := range qs {
		out = append(out, Printer{Name: q.Name, Driver: "cups", Port: "cups"})
	}
	return out, nil
}

// Probe checks the queue still exists in CUPS.
func Probe(name string) error {
	qs, err := cups.Queues()
	if err != nil {
		return fmt.Errorf("localprint: %w", err)
	}
	for _, q := range qs {
		if q.Name == name {
			return nil
		}
	}
	return fmt.Errorf("cups queue %q not found (lpstat -p)", name)
}
