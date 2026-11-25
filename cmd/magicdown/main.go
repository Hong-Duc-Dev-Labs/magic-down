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
	flag.Parse()

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
	eng.SetOnTaskCompleted(func(task *engine.Task) {
		notify.NotifyDownloadComplete(task.Filename)
	})

	// Start local IPC server for Browser Extension <-> Native Host <-> Desktop App
	ipcServer := ipc.NewServer(eng, *ipcPort)
	if err := ipcServer.Start(); err != nil {
		log.Printf("warning: failed to start IPC server on port %d: %v", *ipcPort, err)
	}
	defer ipcServer.Stop()

	appUI := gui.NewApp(eng, ipcServer)
	ipcServer.SetOnIncomingDownload(appUI.EnqueueIncomingDownload)

	mygo.App.WhenReady(func() {
		w := mygo.NewWindow(mygo.WindowOptions{
			Title:             "MagicDown",
			Width:             1120,
			Height:            700,
			MinWidth:          920,
			MinHeight:         580,
			TitleBarStyle:     mygo.TitleBarHidden,
			TitleBarHeight:    38,
			HideWindowButtons: true,
			Content:           ui.View(appUI.View),
		})
		_ = w.SetIcon(appIconPNG)
		appUI.SetWindowController(w)
		w.OnMaximize(func() { appUI.SetMaximized(true) })
		w.OnUnmaximize(func() { appUI.SetMaximized(false) })
	})

	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
