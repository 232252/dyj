// launch.go owns the "just run dyj" experience (research/13 §4.1):
//
//	dyj            (no args) state machine — unconfigured → bootstrap +
//	               browser wizard; configured → run the role (share serves,
//	               use shows the added-printer manager)
//	dyj open       open the admin UI in the browser (desktop shortcut)
//	dyj setup-helper  internal: elevated queue-install helper (Windows)
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sq199/dyj/pkg/service"
)

// cmdLaunch is the no-argument entry: read the persisted role and run the
// node accordingly. A missing/broken service.json means first run —
// bootstrap with the wizard opening in the browser.
func cmdLaunch() error {
	o := serveOptions{Port: 6631}
	if raw, err := service.LoadOptions(); err == nil {
		parsed, perr := parseServeOptionsJSON([]byte(raw))
		if perr != nil {
			// Corrupt config must still reach the wizard to be repaired.
			fmt.Fprintf(os.Stderr, "dyj: 已保存的配置无效（%v），进入引导模式\n", perr)
		} else {
			o = parsed
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runNode(ctx, o, modeLaunch)
}

// serveFromJSON is the service entry: options arrive as the JSON persisted
// by `dyj service install` / the admin PUT.
func serveFromJSON(ctx context.Context, raw string) error {
	o, err := parseServeOptionsJSON([]byte(raw))
	if err != nil {
		return fmt.Errorf("persisted service options are invalid: %w", err)
	}
	return runNode(ctx, o, modeService)
}

// cmdOpen finds the running node (probing the configured port upward) and
// opens its admin UI in the default browser.
func cmdOpen(args []string) error {
	fs := flag.NewFlagSet("open", flag.ContinueOnError)
	port := fs.Int("port", 0, "admin port (default: probe from the saved config)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	base := *port
	if base == 0 {
		base = 6631
		if raw, err := service.LoadOptions(); err == nil {
			var o serveOptions
			if json.Unmarshal([]byte(raw), &o) == nil && o.Port > 0 {
				base = o.Port
			}
		}
	}
	client := &http.Client{Timeout: 800 * time.Millisecond}
	for p := base; p < base+10; p++ {
		resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/admin/api/status", p))
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				openBrowser(fmt.Sprintf("http://127.0.0.1:%d/admin", p))
				return nil
			}
		}
	}
	return errors.New("dyj 未在运行：先启动 dyj（开始菜单/桌面图标，或运行 dyj）")
}

// cmdSetupHelper is the elevated one-shot child on Windows (research/13
// §4.6): install the queue named in --spec, report via --result.
func cmdSetupHelper(args []string) error {
	fs := flag.NewFlagSet("setup-helper", flag.ContinueOnError)
	specPath := fs.String("spec", "", "queue spec JSON file")
	resPath := fs.String("result", "", "result JSON file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *specPath == "" || *resPath == "" {
		return errors.New("setup-helper: --spec and --result are required")
	}
	return runSetupHelper(*specPath, *resPath)
}
