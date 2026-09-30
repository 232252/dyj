//go:build !windows

package sysqueue

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/sq199/dyj/pkg/driver/cups"
)

// lpadminPath resolves the tool once (absolute path — the exec contract of
// this repo refuses relative program paths).
func lpadminPath() (string, error) {
	p, err := exec.LookPath("lpadmin")
	if err != nil {
		return "", fmt.Errorf("sysqueue: lpadmin not found (install cups-client): %w", err)
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	return abs, nil
}

// Install runs lpadmin -m everywhere — the driverless IPP queue.
func Install(spec Spec) (Result, error) {
	lpadmin, err := lpadminPath()
	if err != nil {
		return Result{Cmd: "lpadmin -p " + spec.Name + " -E -v " + spec.Describe() + " -m everywhere"}, err
	}
	cmd := &exec.Cmd{
		Path:      lpadmin,
		Args:      []string{lpadmin, "-p", spec.Name, "-E", "-v", spec.IPPURL, "-m", "everywhere"},
		WaitDelay: 15 * time.Second,
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err = cmd.Run()
	res := Result{Cmd: "lpadmin -p " + spec.Name + " -E -v " + spec.Describe() + " -m everywhere", Output: firstLines(out.String(), 6)}
	if err == nil {
		return res, nil
	}
	msg := firstLines(out.String(), 3)
	if errors.Is(err, os.ErrPermission) || out.Len() == 0 {
		// Root is the norm for lpadmin; surface the runnable command.
		return res, fmt.Errorf("lpadmin needs privileges — run: %s (sudo)", res.Cmd)
	}
	return res, fmt.Errorf("lpadmin: %s", msg)
}

// InstallAuto picks a free name; no elevation dance on unix — the error
// tells the user to run the printed command with sudo.
func InstallAuto(spec Spec) (string, Result, error) {
	name := uniqueName(spec.Name, func(n string) bool {
		ok, _ := Exists(n)
		return ok
	})
	spec.Name = name
	res, err := Install(spec)
	return name, res, err
}

// Exists checks the CUPS queue set.
func Exists(name string) (bool, error) {
	qs, err := cups.Queues()
	if err != nil {
		return false, err
	}
	for _, q := range qs {
		if q.Name == name {
			return true, nil
		}
	}
	return false, nil
}

// Remove runs lpadmin -x.
func Remove(name string) error {
	lpadmin, err := lpadminPath()
	if err != nil {
		return err
	}
	cmd := &exec.Cmd{
		Path:      lpadmin,
		Args:      []string{lpadmin, "-x", name},
		WaitDelay: 15 * time.Second,
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("lpadmin -x: %s", firstLines(out.String(), 3))
	}
	return nil
}

// RunInstallHelper has no role on unix (no UAC); the command refuses.
func RunInstallHelper(specPath, resPath string) error {
	return errors.New("sysqueue: setup-helper is Windows-only")
}
