package main

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
	"github.com/egoist/mygo/plugins/updater/native"
	"github.com/egoist/mygo/ui"
)

type app struct {
	collectors       *collectors
	status           *mygo.MenuItem
	window           *mygo.Window
	address, message string
	quitting         bool
	stopped          bool
}

func (a *app) settings() {
	if a.window == nil {
		a.window = mygo.NewWindow(mygo.WindowOptions{
			Title: "Atrace 服务器", Width: 460, Height: 200, Hidden: true, DisableResize: true,
			Content: ui.View(a.settingsView),
		})
		a.window.OnClose(func(e *mygo.CloseEvent) {
			if !a.quitting {
				e.PreventDefault()
				a.window.Hide()
			}
		})
	}
	a.address = a.collectors.config().server
	a.message = ""
	a.window.Update(func() {})
	a.window.Center()
	a.window.Show()
	a.window.Focus()
}
func (a *app) settingsView(c *ui.Context) {
	if c.Shortcut(0, ui.KeyEscape) {
		a.window.Hide()
	}
	theme := c.Theme()
	ui.Column(c).Fill().Padding(24).Gap(12).Children(func() {
		ui.Text(c, "Atrace 服务器").FontSize(18)
		ui.TextInput(c.Key("server"), &a.address).Label("服务器地址").Placeholder("https://zexi.me")
		if a.message != "" {
			ui.Text(c, a.message).FontSize(12).TextColor(theme.TextMuted)
		}
		ui.Row(c).Gap(8).Children(func() {
			ui.Spacer(c)
			if ui.Button(c, "取消").Clicked() {
				a.window.Hide()
			}
			if ui.PrimaryButton(c, "保存").Clicked() {
				address, err := serverAddress(a.address)
				if err != nil {
					a.message = err.Error()
					return
				}
				a.message = "正在保存…"
				go func() {
					err := a.collectors.setServer(address)
					mygo.RunOnMain(func() {
						if err != nil {
							a.message = err.Error()
							a.window.Update(func() {})
						} else {
							a.window.Hide()
						}
					})
				}()
			}
		})
	})
}
func (a *app) ready() {
	cfg, err := loadSettings()
	if err != nil {
		log.Fatal(err)
	}
	output, err := os.OpenFile(filepath.Join(filepath.Dir(cfg.file), "logs", "app.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		log.Fatal(err)
	}
	log.SetOutput(output)
	a.status = &mygo.MenuItem{Label: "正在连接… · " + cfg.server, Click: func(*mygo.MenuItem, *mygo.Window) { a.settings() }}
	a.collectors = newCollectors(cfg, func(label string) {
		// A worker must never wait on the UI while shutdown is reaping workers.
		go mygo.RunOnMain(func() {
			if !a.quitting {
				a.status.SetLabel(label + " · " + a.collectors.config().server)
			}
		})
	})
	menu := mygo.NewMenu([]*mygo.MenuItem{
		a.status,
		{Label: "打开 Atrace", Click: func(*mygo.MenuItem, *mygo.Window) {
			if err := mygo.Shell.OpenExternal(a.collectors.config().server); err != nil {
				mygo.Dialog.Error("打开 Atrace", err.Error())
			}
		}},
		{Label: "打开采集器目录", Click: func(*mygo.MenuItem, *mygo.Window) {
			directory := a.collectors.config().directory
			if err := os.MkdirAll(directory, 0700); err != nil {
				mygo.Dialog.Error("采集器目录", err.Error())
				return
			}
			if err := mygo.Shell.OpenPath(directory); err != nil {
				mygo.Dialog.Error("采集器目录", err.Error())
			}
		}},
		{Label: "复制 CLI 说明", Click: func(*mygo.MenuItem, *mygo.Window) {
			path := filepath.Join(cfg.bin, "..", "Resources", "bin", "atrace")
			quoted := "'" + strings.ReplaceAll(path, "'", "'\\''") + "'"
			mygo.Clipboard.WriteText(quoted + " --help")
		}},
		updater.MenuItem(), mygo.Separator(),
		{Label: "退出", Click: func(*mygo.MenuItem, *mygo.Window) { mygo.App.Quit() }},
	})
	tray, err := mygo.NewTray(mygo.TrayOptions{ID: "atrace", Icon: trayIcon(), IconIsTemplate: true, ToolTip: "Atrace", Menu: menu})
	if err != nil {
		log.Fatal(err)
	}
	mygo.App.OnBeforeQuit(func(e *mygo.QuitEvent) {
		if a.stopped {
			return
		}
		e.PreventDefault()
		if a.quitting {
			return
		}
		a.quitting = true
		go func() { a.collectors.stop(); mygo.RunOnMain(func() { a.stopped = true; mygo.App.Quit() }) }()
	})
	mygo.App.OnWillQuit(func(*mygo.QuitEvent) { tray.Destroy() })
	a.collectors.start()
	// Route process-manager shutdown through the same graceful collector cleanup.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() { <-sig; mygo.App.Quit() }()
}
func trayIcon() []byte {
	img := image.NewNRGBA(image.Rect(0, 0, 32, 32))
	ink := image.NewUniform(color.NRGBA{A: 255})
	for _, r := range []image.Rectangle{image.Rect(5, 8, 8, 25), image.Rect(24, 8, 27, 25), image.Rect(5, 23, 27, 26), image.Rect(5, 7, 27, 10), image.Rect(12, 16, 20, 19)} {
		draw.Draw(img, r, ink, image.Point{}, draw.Src)
	}
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}
func main() {
	mygo.App.SetName("Atrace")
	mygo.App.SetActivationPolicy(mygo.ActivationPolicyAccessory)
	if !mygo.App.RequestSingleInstanceLock() {
		return
	}
	mygo.Use(native.Plugin)
	a := &app{}
	mygo.App.WhenReady(a.ready)
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
