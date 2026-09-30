//go:build windows

package tray

import (
	"context"
	"fmt"
	"os"
	"time"

	"fyne.io/systray"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// Run starts the tray loop and blocks until Quit. The service is never
// touched: quitting the tray only removes the icon. A named mutex keeps
// the icon single-instance — the installer writes an HKLM Run entry and
// the in-tray toggle writes HKCU, so two autostart entries can coexist.
func Run(opts Options) error {
	muName, muErr := windows.UTF16PtrFromString("Local\\dyj-tray-singleton")
	if muErr != nil {
		return fmt.Errorf("tray: mutex name: %w", muErr)
	}
	mu, err := windows.CreateMutex(nil, true, muName)
	switch {
	case err == nil:
		defer func() { _ = windows.ReleaseMutex(mu); _ = windows.CloseHandle(mu) }()
	case err == windows.ERROR_ALREADY_EXISTS:
		return nil // another tray is showing the icon; stay out of the way
	default:
		// Mutex creation can fail on constrained setups; degrade to the
		// old behavior instead of refusing to run.
	}
	if opts.ExePath == "" {
		exe, err := os.Executable()
		if err != nil {
			return fmt.Errorf("tray: resolving executable: %w", err)
		}
		opts.ExePath = exe
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c := NewController(opts)
	autostart := &runKeyAutostart{exePath: opts.ExePath}

	var (
		itemOpen    *systray.MenuItem
		itemPin     *systray.MenuItem
		itemPause   *systray.MenuItem
		itemStartup *systray.MenuItem
		itemQuit    *systray.MenuItem
	)

	onReady := func() {
		refreshTaskbarTheme()
		systray.SetIcon(IconBytes(StateDown))
		systray.SetTooltip("dyj — 连接中…")

		itemOpen = systray.AddMenuItem("打开管理页", "在浏览器中打开 dyj 管理页")
		itemPin = systray.AddMenuItem("配对码：…", "接收端添加打印机时输入的 4 位数字")
		itemPin.Disable()
		systray.AddSeparator()
		itemPause = systray.AddMenuItem("暂停共享", "暂停接受新的打印作业（队列中的作业会打完）")
		itemStartup = systray.AddMenuItem("登录时启动托盘", "当前用户登录后自动显示托盘图标")
		systray.AddSeparator()
		itemQuit = systray.AddMenuItem("退出托盘", "只退出托盘图标，后台打印服务继续运行")
		// Share actions wait for the first role poll (research/14 §3.7):
		// receiver machines never see 配对码/暂停共享 at all.
		itemPin.Hide()
		itemPause.Hide()

		itemStartup.Check()
		if on, err := autostart.Enabled(); err == nil && on {
			itemStartup.Check()
		} else {
			itemStartup.Uncheck()
		}

		go func() {
			for range itemOpen.ClickedCh {
				if err := shellOpen(c.opts.AdminURL + "/admin"); err != nil {
					systray.SetTooltip("dyj — 无法打开浏览器: " + err.Error())
				}
			}
		}()
		go func() {
			for range itemPause.ClickedCh {
				paused := c.State() == StateRunning
				if err := c.SetPaused(ctx, paused); err != nil {
					systray.SetTooltip("dyj — 操作失败: " + err.Error())
				}
				refresh(c, itemPause)
			}
		}()
		go func() {
			for range itemStartup.ClickedCh {
				on, err := autostart.Enabled()
				if err != nil {
					continue
				}
				if on {
					_ = autostart.Disable()
					itemStartup.Uncheck()
				} else {
					if err := autostart.Enable(); err != nil {
						systray.SetTooltip("dyj — 写入自启动失败: " + err.Error())
					}
					itemStartup.Check()
				}
			}
		}()
		go func() {
			<-itemQuit.ClickedCh
			cancel()
			systray.Quit()
		}()

		// Status polling → icon/tooltip/menu text.
		go func() {
			last := State(-1)
			lastPin := ""
			lastRole := "?"
			for {
				st := c.State()
				pin := c.Pin()
				role := c.Role()
				if role != lastRole {
					sharing := role == "share" || role == "both"
					if sharing {
						itemPin.Show()
						itemPause.Show()
					} else {
						itemPin.Hide()
						itemPause.Hide()
					}
					lastRole = role
				}
				if st != last || pin != lastPin {
					refresh(c, itemPause)
					if itemPin != nil {
						if pin != "" {
							itemPin.SetTitle("配对码：" + pin)
						} else {
							itemPin.SetTitle("配对码：未共享")
						}
					}
					last, lastPin = st, pin
				}
				select {
				case <-ctx.Done():
					return
				case <-time.After(c.opts.PollInterval):
					c.Poll(ctx)
				}
			}
		}()
		c.Poll(ctx)
	}

	onExit := func() { cancel() }
	systray.Run(onReady, onExit)
	return nil
}

// refresh repaints icon, tooltip and the pause label from state.
func refresh(c *Controller, itemPause *systray.MenuItem) {
	refreshTaskbarTheme()
	st := c.State()
	systray.SetIcon(IconBytes(st))
	queued := c.Queued()
	q := ""
	if queued > 0 {
		q = fmt.Sprintf("，队列 %d 个作业", queued)
	}
	switch st {
	case StateRunning:
		systray.SetTooltip("dyj — 共享中" + q)
		if itemPause != nil {
			itemPause.SetTitle("暂停共享")
		}
	case StatePaused:
		systray.SetTooltip("dyj — 已暂停" + q)
		if itemPause != nil {
			itemPause.SetTitle("恢复共享")
		}
	default:
		systray.SetTooltip("dyj — 服务未运行（后台服务未启动或正在重启）")
		if itemPause != nil {
			itemPause.SetTitle("暂停共享")
			itemPause.Disable()
		}
	}
	if st != StateDown && itemPause != nil {
		itemPause.Enable()
	}
}

// refreshTaskbarTheme picks glyph ink from the shell's AppsUseLightTheme
// (HKCU Themes\Personalize). Best-effort: unreadable registry keeps the
// current ink. Called on ready and on every state repaint, which is when
// the theme may plausibly have flipped.
func refreshTaskbarTheme() {
	k, err := registry.OpenKey(registry.CURRENT_USER,
		`Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, registry.QUERY_VALUE)
	if err != nil {
		return
	}
	defer k.Close()
	v, _, err := k.GetIntegerValue("AppsUseLightTheme")
	if err != nil {
		return
	}
	SetLightTaskbar(v != 0)
}

// runKeyAutostart toggles HKCU\...\CurrentVersion\Run for the tray exe.
type runKeyAutostart struct{ exePath string }

const runKeyPath = `Software\Microsoft\Windows\CurrentVersion\Run`
const runValueName = "dyj-tray"

func (r *runKeyAutostart) Enabled() (bool, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE)
	if err != nil {
		return false, err
	}
	defer k.Close()
	v, _, err := k.GetStringValue(runValueName)
	if err == registry.ErrNotExist {
		return false, nil
	}
	return err == nil && v != "", err
}

func (r *runKeyAutostart) Enable() error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	// Quoted so paths with spaces survive parsing.
	return k.SetStringValue(runValueName, `"`+r.exePath+`"`)
}

func (r *runKeyAutostart) Disable() error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	return k.DeleteValue(runValueName)
}
