//go:build windows

package service

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

// signalCtx is the interactive-run stop source (Ctrl+C).
func signalCtx() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

func exePath() (string, error) {
	p, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Abs(p)
}

// ServiceName is the Windows service name.
const ServiceName = "dyj"

// RunForeground detects the SCM: as a service it runs the handler loop;
// interactively it just serves (Ctrl+C works as usual).
func RunForeground(serve ServeFunc) error {
	isSvc, err := svc.IsWindowsService()
	if err != nil {
		return err
	}
	if !isSvc {
		ctx, stop := signalCtx()
		defer stop()
		return Run(ctx, serve)
	}
	return svc.Run(ServiceName, &handler{serve: serve})
}

type handler struct{ serve ServeFunc }

// Execute implements svc.Handler: it starts serving immediately and
// reports Running; an SCM Stop request cancels ctx and the server's
// graceful shutdown finishes the exit code.
func (h *handler) Execute(args []string, r <-chan svc.ChangeRequest, s chan<- svc.Status) (bool, uint32) {
	s <- svc.Status{State: svc.StartPending}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- h.serve(ctx, mustOptions()) }()

	s <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
	for {
		select {
		case err := <-done:
			if err != nil {
				s <- svc.Status{State: svc.Stopped}
				return false, 1
			}
			s <- svc.Status{State: svc.Stopped}
			return false, 0
		case c := <-r:
			switch c.Cmd {
			case svc.Interrogate:
				s <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				s <- svc.Status{State: svc.StopPending}
				cancel()
				// Bounded wait: the server shuts down within seconds.
				select {
				case err := <-done:
					if err != nil {
						s <- svc.Status{State: svc.Stopped}
						return false, 1
					}
					s <- svc.Status{State: svc.Stopped}
					return false, 0
				case <-time.After(30 * time.Second):
					s <- svc.Status{State: svc.Stopped}
					return false, 0
				}
			}
		}
	}
}

// imagePath is the ImagePath the SCM must launch: the quoted exe plus the
// subcommand that reaches svc.Run. A bare exe would fall into launch mode
// (banner + browser), never report Running and be killed by the SCM after
// 30 s (event 7009) — exactly what 0.4.0's first real install exposed.
func imagePath(exe string) string {
	return syscall.EscapeArg(exe) + " service run"
}

// Install registers the service. Options must already be persisted via
// SaveOptions. An existing registration is REPAIRED in place when its
// ImagePath is wrong (older builds registered the bare exe), so running
// the installer again upgrades a broken install instead of failing.
func Install() error {
	exe, err := exePath()
	if err != nil {
		return err
	}
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("service: connecting to SCM requires administrator rights: %w", err)
	}
	defer m.Disconnect()

	create := func() (*mgr.Service, error) {
		return m.CreateService(ServiceName, exe, mgr.Config{
			DisplayName: "dyj LAN printer sharing",
			Description: "Shares local printers over the LAN via IPP Everywhere + mDNS (driverless printing).",
			StartType:   mgr.StartAutomatic,
		}, "service", "run")
	}
	var s *mgr.Service
	if existing, serr := m.OpenService(ServiceName); serr == nil {
		// Existing registration: stop + delete, then recreate with the
		// correct command line. This is the upgrade path — 0.4.0's first
		// real install registered the bare exe without "service run",
		// which fell into launch mode and was killed by the SCM after
		// 30 s (event 7009); rerunning the installer must repair it.
		if st, qerr := existing.Query(); qerr == nil && st.State != svc.Stopped {
			_, _ = existing.Control(svc.Stop)
			waitStopped(existing)
		}
		_ = existing.Delete()
		existing.Close()
		// Deletion is asynchronous (marked-for-delete until the last
		// handle closes and the SCM settles); retry the recreate briefly.
		s, err = create()
		for i := 0; err != nil && i < 10; i++ {
			time.Sleep(500 * time.Millisecond)
			s, err = create()
		}
		if err != nil {
			return fmt.Errorf("service %q was registered but could not be updated: %w", ServiceName, err)
		}
	} else {
		s, err = create()
		if err != nil {
			return err
		}
	}
	defer s.Close()
	if err := s.SetRecoveryActions([]mgr.RecoveryAction{
		{Type: mgr.ServiceRestart, Delay: 10 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 30 * time.Second},
	}, 86400); err != nil {
		// Recovery actions are best-effort; some configurations refuse.
		_ = err
	}
	return nil
}

// Uninstall removes the service.
func Uninstall() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("service: connecting to SCM requires administrator rights: %w", err)
	}
	defer m.Disconnect()
	s, err := m.OpenService(ServiceName)
	if err != nil {
		return fmt.Errorf("service %q is not installed", ServiceName)
	}
	defer s.Close()
	if st, err := s.Query(); err == nil && st.State != svc.Stopped {
		_, _ = s.Control(svc.Stop)
		waitStopped(s)
	}
	return s.Delete()
}

// Start starts the installed service.
func Start() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("service: connecting to SCM requires administrator rights: %w", err)
	}
	defer m.Disconnect()
	s, err := m.OpenService(ServiceName)
	if err != nil {
		return fmt.Errorf("service %q is not installed", ServiceName)
	}
	defer s.Close()
	return s.Start()
}

// Stop stops the running service.
func Stop() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("service: connecting to SCM requires administrator rights: %w", err)
	}
	defer m.Disconnect()
	s, err := m.OpenService(ServiceName)
	if err != nil {
		return fmt.Errorf("service %q is not installed", ServiceName)
	}
	defer s.Close()
	if _, err := s.Control(svc.Stop); err != nil {
		return err
	}
	waitStopped(s)
	return nil
}

func waitStopped(s *mgr.Service) {
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		st, err := s.Query()
		if err != nil || st.State == svc.Stopped {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func mustOptions() string {
	raw, err := LoadOptions()
	if err != nil {
		// The handler cannot return an options error through svc.Run;
		// serving with defaults beats a service that refuses to start.
		return "{}"
	}
	return raw
}
