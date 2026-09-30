//go:build !windows

package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// printPNGToQueue submits via lp(1) — the same submission path CUPS
// production jobs take.
func printPNGToQueue(ctx context.Context, queue string, pngBytes []byte) error {
	lp, err := exec.LookPath("lp")
	if err != nil {
		return fmt.Errorf("lp not found (install cups-client): %w", err)
	}
	abs, err := filepath.Abs(lp)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp("", "dyj-testpage-*.png")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(pngBytes); err != nil {
		tmp.Close()
		return err
	}
	tmp.Close()
	cmd := &exec.Cmd{
		Path:      abs,
		Args:      []string{abs, "-d", queue, tmp.Name()},
		WaitDelay: 30 * time.Second,
	}
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return fmt.Errorf("lp: %s", firstLine(out))
	}
	return nil
}

func firstLine(b []byte) string {
	s := string(b)
	for i, c := range s {
		if c == '\n' {
			return s[:i]
		}
	}
	return s
}
