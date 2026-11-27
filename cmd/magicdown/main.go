package main

import (
	_ "embed"
	"flag"
	"log"
	"os"
	"path/filepath"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"magicdown/pkg/engine"
	"magicdown/pkg/gui"
	"magicdown/pkg/i18n"
	"magicdown/pkg/ipc"
	"magicdown/pkg/nativemessaging"
	"magicdown/pkg/notify"
)

//go:embed icon.png
var appIconPNG []byte

func main() {
	// Support running as Chrome/Edge Native Messaging host via subcommand:
	//   magicdown.exe host [--port 28888]
	if len(os.Args) > 1 && os.Args[1] == "host" {
		hostFlags := flag.NewFlagSet("host", flag.ExitOnError)
		port := hostFlags.Int("port", ipc.DefaultPort, "MagicDown desktop app IPC port")
		_ = hostFlags.Parse(os.Args[2:])

		if err := nativemessaging.RunHostWithStdio(*port); err != nil {
			log.Fatalf("native messaging host error: %v", err)
		}
		return
	}

	ipcPort := flag.Int("port", ipc.DefaultPort, "Local IPC HTTP port")
	downloadDir := flag.String("dir", "", "Default download directory")
	startMinimized := flag.Bool("minimized", false, "Start hidden in the system tray")
	startTray := flag.Bool("tray", false, "Start hidden in the system tray (alias for --minimized)")
	startAutostart := flag.Bool("autostart", false, "Start hidden in the system tray (alias for --minimized)")
	flag.Parse()

	// Single-instance enforcement: if another MagicDown desktop instance is already
	// running (e.g. hidden in the system tray), wake it up and exit immediately.
	if !mygo.App.RequestSingleInstanceLock() {
		return
	}

	// Resolve download directory and state file path
	dlDir := *downloadDir
	if dlDir == "" {
		if home, err := os.UserHomeDir(); err == nil {
			dlDir = filepath.Join(home, "Downloads", "MagicDown")
		} else {
			dlDir = "downloads"
		}
	}

	statePath := filepath.Join(dlDir, ".magicdown_state.json")
	if cfgDir, err := os.UserConfigDir(); err == nil {
		appCfgDir := filepath.Join(cfgDir, "MagicDown")
		_ = os.MkdirAll(appCfgDir, 0755)
		statePath = filepath.Join(appCfgDir, "downloads.json")
	}

	eng, err := engine.NewEngine(engine.DefaultConfig(dlDir, statePath))
	if err != nil {
		log.Fatalf("failed to initialize download engine: %v", err)
	}
	defer eng.Close()

	// Start local IPC server for Browser Extension <-> Native Host <-> Desktop App
	ipcServer := ipc.NewServer(eng, *ipcPort)
	if err := ipcServer.Start(); err != nil {
		log.Printf("warning: failed to start IPC server on port %d: %v", *ipcPort, err)
	}
	defer ipcServer.Stop()

	appUI := gui.NewApp(eng, ipcServer)
	defer appUI.Close()
	ipcServer.SetOnIncomingDownload(appUI.EnqueueIncomingDownload)
	eng.SetOnTaskCompleted(func(task *engine.Task) {
		notify.NotifyDownloadComplete(task.Filename)
		appUI.HandleTaskCompleted(task)
	})
	eng.SetOnScheduleStop(func(stopAction string) {
		appUI.HandleScheduleStop(stopAction)
	})

	// Keep the process alive in the system tray when windows are hidden/closed.
	mygo.App.OnWindowAllClosed(func() {})

	// Wake the existing window when the user launches magicdown.exe a second time.
	mygo.App.OnSecondInstance(func(args []string, workingDir string) {
		appUI.WakeFromTray()
	})

	hideOnLaunch := *startMinimized || *startTray || *startAutostart
	if hideOnLaunch {
		appUI.PrepareHiddenLaunch()
	}

	var tray *mygo.Tray
	mygo.App.WhenReady(func() {
		w := mygo.NewWindow(mygo.WindowOptions{
			Title:             "MagicDown",
			Width:             1120,
			Height:            700,
			MinWidth:          920,
			MinHeight:         580,
			Hidden:            hideOnLaunch,
			TitleBarStyle:     mygo.TitleBarHidden,
			TitleBarHeight:    38,
			HideWindowButtons: true,
			Content:           ui.View(appUI.View),
		})
		_ = w.SetIcon(appIconPNG)
		appUI.SetWindowController(w)
		w.OnMaximize(func() { appUI.SetMaximized(true) })
		w.OnUnmaximize(func() { appUI.SetMaximized(false) })
		w.OnClose(func(e *mygo.CloseEvent) {
			if appUI.HandleCloseRequest() {
				e.PreventDefault()
			}
		})

		quitApp := func(*mygo.MenuItem, *mygo.Window) {
			appUI.SetQuitting(true)
			if tray != nil {
				tray.Destroy()
			}
			mygo.App.Quit()
		}
		appUI.SetQuitFunc(func() {
			quitApp(nil, nil)
		})
		appUI.StartAutoUpdateCheck()

		buildTrayMenu := func() *mygo.Menu {
			return mygo.NewMenu([]*mygo.MenuItem{
				{
					Label: i18n.T("tray.open"),
					Click: func(*mygo.MenuItem, *mygo.Window) { appUI.WakeFromTray() },
				},
				mygo.Separator(),
				{
					Label: i18n.T("tray.pause_all"),
					Click: func(*mygo.MenuItem, *mygo.Window) { eng.PauseAll() },
				},
				{
					Label: i18n.T("tray.resume_all"),
					Click: func(*mygo.MenuItem, *mygo.Window) { eng.ResumeAll() },
				},
				mygo.Separator(),
				{
					Label: i18n.T("tray.quit"),
					Click: quitApp,
				},
			})
		}

		var trayErr error
		tray, trayErr = mygo.NewTray(mygo.TrayOptions{
			ID:      "magicdown-tray",
			Icon:    appIconPNG,
			ToolTip: i18n.T("tray.tooltip"),
			Menu:    buildTrayMenu(),
		})
		if trayErr != nil {
			log.Printf("warning: failed to create system tray icon: %v", trayErr)
		} else {
			tray.OnClick(func() {
				appUI.WakeFromTray()
			})
			appUI.SetOnLanguageChange(func(lang i18n.Lang) {
				if tray != nil {
					tray.SetMenu(buildTrayMenu())
					tray.SetToolTip(i18n.T("tray.tooltip"))
				}
			})
		}

		if hideOnLaunch {
			if trayErr == nil {
				appUI.HideToTray()
			} else {
				appUI.WakeFromTray()
			}
		}
	})

	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
