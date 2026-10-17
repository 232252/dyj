//go:build windows

package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"time"

	"github.com/232252/dyj/pkg/driver"
)

// printPNGToQueue renders through wingdi — the same GDI path production
// jobs take, so a passing test page proves the whole chain.
func printPNGToQueue(ctx context.Context, queue string, pngBytes []byte) error {
	backend := &driver.WingDI{Printer: queue}
	doc := driver.Document{
		Format: "image/png",
		Size:   int64(len(pngBytes)),
		Open: func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(pngBytes)), nil
		},
	}
	job := driver.PrintJob{ID: 0, User: "dyj", Name: "dyj test page"}
	if err := backend.Print(ctx, job, doc); err != nil {
		return fmt.Errorf("wingdi: %w", err)
	}
	// The spooler queue accepts the job synchronously; give the physical
	// device a beat before reporting success to the wizard.
	select {
	case <-ctx.Done():
	case <-time.After(300 * time.Millisecond):
	}
	return nil
}
