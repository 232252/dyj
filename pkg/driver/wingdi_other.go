//go:build !windows

package driver

import (
	"context"
	"errors"

	"io"
)

// WingDI is a Windows-only backend; referencing the type off-Windows
// compiles (cmd/dyj references it on every platform) but constructing
// and using it fails with an actionable message.
type WingDI struct{ Printer string }

// Name implements Interface (never actually reachable off-Windows).
func (w *WingDI) Name() string { return "wingdi:" + w.Printer }

// Print always errors off-Windows.
func (w *WingDI) Print(ctx context.Context, job PrintJob, doc Document) error {
	_, _, _ = ctx, job, doc
	return errors.New("driver: wingdi backend requires Windows")
}

var _ Interface = (*WingDI)(nil)
var _ io.Writer = io.Discard
