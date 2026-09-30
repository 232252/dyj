//go:build windows

package sysqueue

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/windows"
)

// errAccessDenied marks the "need the elevated helper" case: research/12
// measured that Add-Printer -IppUrl refuses unelevated callers.
var errAccessDenied = errors.New("access denied (administrator required)")

func powershellPath() string {
	root := os.Getenv("SystemRoot")
	if root == "" {
		root = `C:\Windows`
	}
	return filepath.Join(root, "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
}

// runPS executes one PowerShell script snippet. The interpreter path is
// absolute by construction; arguments are passed via Args (never through a
// shell string).
func runPS(script string) (string, error) {
	ps := powershellPath()
	cmd := &exec.Cmd{
		Path:      ps,
		Args:      []string{ps, "-NoProfile", "-NonInteractive", "-Command", script},
		WaitDelay: 30 * time.Second,
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	return out.String(), err
}

// Install creates the queue via Add-Printer -IppUrl (Microsoft IPP Class
// Driver). ErrExists when the name is taken; an access-denied failure is
// wrapped so InstallAuto can retry through the elevated helper.
func Install(spec Spec) (Result, error) {
	script := fmt.Sprintf(
		"try { Add-Printer -ErrorAction Stop -IppUrl %s -Name %s; 'OK' } catch { $_.Exception.Message; exit 1 }",
		psQuote(spec.IPPURL), psQuote(spec.Name))
	out, err := runPS(script)
	res := Result{Cmd: "Add-Printer -IppUrl " + psQuote(spec.Describe()) + " -Name " + psQuote(spec.Name), Output: firstLines(out, 6)}
	if err != nil {
		if strings.Contains(strings.ToLower(out), "access is denied") || strings.Contains(strings.ToLower(out), "拒绝访问") {
			return res, errAccessDenied
		}
		if strings.Contains(strings.ToLower(out), "already exists") || strings.Contains(out, "已存在") || strings.Contains(out, "打印机已存在") {
			return res, ErrExists
		}
		return res, fmt.Errorf("Add-Printer: %s", firstLines(out, 3))
	}
	return res, nil
}

// InstallAuto picks a free name and installs, transparently going through
// the elevated helper when the process itself is not elevated.
func InstallAuto(spec Spec) (name string, res Result, err error) {
	name = uniqueName(spec.Name, func(n string) bool {
		ok, _ := Exists(n)
		return ok
	})
	spec.Name = name
	res, err = Install(spec)
	if err == nil {
		return name, res, nil
	}
	if errors.Is(err, errAccessDenied) {
		res, err = InstallElevated(spec)
		res.Elevated = true
	}
	return name, res, err
}

// Exists reports whether a queue with that name is present.
func Exists(name string) (bool, error) {
	script := fmt.Sprintf(
		"try { if (Get-Printer -Name %s -ErrorAction Stop) { 'yes' } else { 'no' } } catch { 'no' }",
		psQuote(name))
	out, err := runPS(script)
	if err != nil {
		return false, fmt.Errorf("Get-Printer: %s", firstLines(out, 3))
	}
	return strings.TrimSpace(out) == "yes", nil
}

// Remove deletes the queue.
func Remove(name string) error {
	script := fmt.Sprintf(
		"try { Remove-Printer -ErrorAction Stop -Name %s; 'OK' } catch { $_.Exception.Message; exit 1 }",
		psQuote(name))
	out, err := runPS(script)
	if err != nil {
		return fmt.Errorf("Remove-Printer: %s", firstLines(out, 3))
	}
	return nil
}

// ---------------------------------------------------------------------------
// Elevated helper protocol
// ---------------------------------------------------------------------------

// helperResult is the JSON contract between an unelevated wizard process
// and the elevated `dyj setup-helper --install-queue` relaunch.
type helperResult struct {
	OK     bool   `json:"ok"`
	Error  string `json:"error,omitempty"`
	Cmd    string `json:"cmd,omitempty"`
	Output string `json:"output,omitempty"`
}

// InstallElevated re-runs this binary as administrator with the
// setup-helper arguments (one UAC prompt), then polls the result file the
// helper writes. Spec and result travel through temp files because the
// elevated child has no usable stdin/stdout pipe.
func InstallElevated(spec Spec) (Result, error) {
	exe, err := os.Executable()
	if err != nil {
		return Result{}, fmt.Errorf("locating dyj binary: %w", err)
	}
	stamp := time.Now().UnixNano()
	dir := os.TempDir()
	specPath := filepath.Join(dir, fmt.Sprintf("dyj-pair-spec-%d.json", stamp))
	resPath := filepath.Join(dir, fmt.Sprintf("dyj-pair-res-%d.json", stamp))
	defer os.Remove(specPath)
	defer os.Remove(resPath)
	if err := writeJSONFile(specPath, spec); err != nil {
		return Result{}, err
	}

	verb, _ := windows.UTF16PtrFromString("runas")
	file, _ := windows.UTF16PtrFromString(exe)
	args, _ := windows.UTF16PtrFromString(fmt.Sprintf("setup-helper --install-queue --spec %q --result %q", specPath, resPath))
	cwd, _ := windows.UTF16PtrFromString(dir)
	if err := windows.ShellExecute(0, verb, file, args, cwd, windows.SW_SHOWNORMAL); err != nil {
		return Result{}, fmt.Errorf("elevation refused: %w", err)
	}

	deadline := time.Now().Add(120 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(300 * time.Millisecond)
		data, err := os.ReadFile(resPath)
		if err != nil {
			continue // helper still running (or UAC still on screen)
		}
		var hr helperResult
		if err := json.Unmarshal(data, &hr); err != nil {
			return Result{}, fmt.Errorf("helper result unreadable: %w", err)
		}
		res := Result{Cmd: hr.Cmd, Output: firstLines(hr.Output, 6), Elevated: true}
		if !hr.OK {
			return res, fmt.Errorf("Add-Printer (elevated): %s", hr.Error)
		}
		return res, nil
	}
	return Result{}, fmt.Errorf("elevated helper timed out after 120s")
}

// RunInstallHelper is the setup-helper entry: install the spec, write the
// result file. Called from cmd/dyj with the parsed flags.
func RunInstallHelper(specPath, resPath string) error {
	data, err := os.ReadFile(specPath)
	if err != nil {
		return writeHelperResult(resPath, helperResult{OK: false, Error: "spec unreadable: " + err.Error()})
	}
	var spec Spec
	if err := json.Unmarshal(data, &spec); err != nil {
		return writeHelperResult(resPath, helperResult{OK: false, Error: "spec invalid: " + err.Error()})
	}
	res, err := Install(spec)
	hr := helperResult{OK: err == nil, Cmd: res.Cmd, Output: res.Output}
	if err != nil {
		hr.Error = err.Error()
	}
	return writeHelperResult(resPath, hr)
}

func writeJSONFile(path string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func writeHelperResult(path string, hr helperResult) error {
	return writeJSONFile(path, hr)
}
