package gui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"magicdown/pkg/engine"
	"magicdown/pkg/ipc"
)

// App holds the UI state for the MagicDown MyGo Native UI desktop application.
type App struct {
	engine    *engine.Engine
	ipcServer *ipc.Server

	filterStatus   string
	selectedTaskID string

	// Add URL modal state
	showAddModal  bool
	inputURL      string
	inputFilename string
	inputSegments string
	addError      string

	// Settings modal state
	showSettingsModal    bool
	settingDownloadDir   string
	settingMaxConcurrent string
	settingSegments      string
	settingsError        string

	statusMessage string
}

// NewApp creates a new MagicDown UI state instance.
func NewApp(eng *engine.Engine, srv *ipc.Server) *App {
	cfg := eng.Config()
	return &App{
		engine:               eng,
		ipcServer:            srv,
		filterStatus:         "All",
		inputSegments:        strconv.Itoa(cfg.DefaultSegments),
		settingDownloadDir:   cfg.DownloadDir,
		settingMaxConcurrent: strconv.Itoa(cfg.MaxConcurrentDownloads),
		settingSegments:      strconv.Itoa(cfg.DefaultSegments),
	}
}

// View renders the entire desktop interface for each frame.
func (a *App) View(c *ui.Context) {
	tasks := a.engine.GetTasks()

	// Schedule refresh: animate smoothly while downloads are active, poll lightly otherwise for IPC arrivals.
	if a.engine.ActiveCount() > 0 {
		c.AnimationFrame()
	} else {
		c.After(250 * time.Millisecond)
	}

	var totalSpeed int64
	counts := map[string]int{
		"All":         len(tasks),
		"Downloading": 0,
		"Queued":      0,
		"Paused":      0,
		"Completed":   0,
	}
	for _, t := range tasks {
		totalSpeed += t.Speed
		switch t.Status {
		case engine.StatusDownloading, engine.StatusProbing:
			counts["Downloading"]++
		case engine.StatusQueued:
			counts["Queued"]++
		case engine.StatusPaused, engine.StatusFailed, engine.StatusCancelled:
			counts["Paused"]++
		case engine.StatusCompleted:
			counts["Completed"]++
		}
	}

	// Main vertical container
	ui.Column(c).Fill().Children(func() {
		a.renderToolbar(c, totalSpeed)
		ui.Divider(c)

		// Main body: Sidebar + Download List
		ui.Row(c).Fill().Grow(1).Children(func() {
			a.renderSidebar(c, counts, totalSpeed)
			ui.Divider(c)
			a.renderDownloadList(c, tasks)
		})

		ui.Divider(c)
		a.renderFooter(c, tasks)
	})

	// Modals
	a.renderAddURLModal(c)
	a.renderSettingsModal(c)
}

func (a *App) renderToolbar(c *ui.Context, totalSpeed int64) {
	ui.Row(c).Padding(10, 14).Gap(8).Children(func() {
		if ui.PrimaryButton(c, "+ Add URL").Clicked() {
			cfg := a.engine.Config()
			a.inputURL = ""
			a.inputFilename = ""
			a.inputSegments = strconv.Itoa(cfg.DefaultSegments)
			a.addError = ""
			a.showAddModal = true
		}

		if ui.Button(c, "Resume").Clicked() {
			if a.selectedTaskID != "" {
				if err := a.engine.ResumeTask(a.selectedTaskID); err != nil {
					a.statusMessage = err.Error()
				} else {
					a.statusMessage = "Resumed selected download"
				}
			} else {
				a.engine.ResumeAll()
				a.statusMessage = "Resumed all paused downloads"
			}
		}

		if ui.Button(c, "Pause").Clicked() {
			if a.selectedTaskID != "" {
				if err := a.engine.PauseTask(a.selectedTaskID); err != nil {
					a.statusMessage = err.Error()
				} else {
					a.statusMessage = "Paused selected download"
				}
			} else {
				a.engine.PauseAll()
				a.statusMessage = "Paused all active downloads"
			}
		}

		if ui.Button(c, "Cancel").Clicked() {
			if a.selectedTaskID != "" {
				if err := a.engine.CancelTask(a.selectedTaskID); err != nil {
					a.statusMessage = err.Error()
				} else {
					a.statusMessage = "Cancelled selected download"
				}
			}
		}

		if ui.Button(c, "Delete").Clicked() {
			if a.selectedTaskID != "" {
				if err := a.engine.DeleteTask(a.selectedTaskID, false); err != nil {
					a.statusMessage = err.Error()
				} else {
					a.selectedTaskID = ""
					a.statusMessage = "Removed download from list"
				}
			}
		}

		if ui.Button(c, "Settings").Clicked() {
			cfg := a.engine.Config()
			a.settingDownloadDir = cfg.DownloadDir
			a.settingMaxConcurrent = strconv.Itoa(cfg.MaxConcurrentDownloads)
			a.settingSegments = strconv.Itoa(cfg.DefaultSegments)
			a.settingsError = ""
			a.showSettingsModal = true
		}

		ui.Spacer(c)

		ipcPort := ipc.DefaultPort
		if a.ipcServer != nil {
			ipcPort = a.ipcServer.Port()
		}
		infoText := fmt.Sprintf("IPC :%d  |  Active: %d  |  Speed: %s",
			ipcPort, a.engine.ActiveCount(), FormatSpeed(totalSpeed))
		ui.Text(c, infoText).FontSize(12).TextColor(ui.RGB(130, 140, 155)).SingleLine()
	})
}

func (a *App) renderSidebar(c *ui.Context, counts map[string]int, totalSpeed int64) {
	categories := []struct {
		key   string
		label string
	}{
		{"All", "All Downloads"},
		{"Downloading", "Downloading"},
		{"Queued", "Queued"},
		{"Paused", "Paused / Stopped"},
		{"Completed", "Completed"},
	}

	ui.Column(c).Width(200).Shrink(0).Padding(12).Gap(6).Background(ui.RGBA(128, 128, 128, 0.06)).Children(func() {
		ui.Text(c, "MagicDown").FontSize(18).Bold()
		ui.Text(c, "Download Manager").FontSize(11).TextColor(ui.RGB(130, 140, 155))
		ui.Box(c).Height(8)

		for _, cat := range categories {
			label := fmt.Sprintf("%s (%d)", cat.label, counts[cat.key])
			var btn ui.Element
			if a.filterStatus == cat.key {
				btn = ui.PrimaryButton(c, label)
			} else {
				btn = ui.Button(c, label)
			}
			if btn.Clicked() {
				a.filterStatus = cat.key
			}
		}

		ui.Spacer(c)
		ui.Divider(c)
		ui.Box(c).Height(4)
		cfg := a.engine.Config()
		ui.Text(c, fmt.Sprintf("Max Concurrent: %d", cfg.MaxConcurrentDownloads)).FontSize(11).TextColor(ui.RGB(130, 140, 155))
		ui.Text(c, fmt.Sprintf("Default Segments: %d", cfg.DefaultSegments)).FontSize(11).TextColor(ui.RGB(130, 140, 155))
		ui.Text(c, fmt.Sprintf("Total Speed: %s", FormatSpeed(totalSpeed))).FontSize(11).TextColor(ui.RGB(130, 140, 155))
	})
}

func (a *App) filterTasks(tasks []*engine.Task) []*engine.Task {
	if a.filterStatus == "All" {
		return tasks
	}
	filtered := make([]*engine.Task, 0, len(tasks))
	for _, t := range tasks {
		switch a.filterStatus {
		case "Downloading":
			if t.Status == engine.StatusDownloading || t.Status == engine.StatusProbing {
				filtered = append(filtered, t)
			}
		case "Queued":
			if t.Status == engine.StatusQueued {
				filtered = append(filtered, t)
			}
		case "Paused":
			if t.Status == engine.StatusPaused || t.Status == engine.StatusFailed || t.Status == engine.StatusCancelled {
				filtered = append(filtered, t)
			}
		case "Completed":
			if t.Status == engine.StatusCompleted {
				filtered = append(filtered, t)
			}
		}
	}
	return filtered
}

func (a *App) renderDownloadList(c *ui.Context, allTasks []*engine.Task) {
	filtered := a.filterTasks(allTasks)

	ui.Column(c).Fill().Grow(1).Children(func() {
		// Table Header
		ui.Row(c).Padding(8, 12).Gap(10).Background(ui.RGBA(128, 128, 128, 0.08)).Children(func() {
			ui.Text(c, "Name").Bold().FontSize(12).Width(240).SingleLine()
			ui.Text(c, "Size").Bold().FontSize(12).Width(130).SingleLine()
			ui.Text(c, "Progress").Bold().FontSize(12).Grow(1).SingleLine()
			ui.Text(c, "Speed").Bold().FontSize(12).Width(90).SingleLine()
			ui.Text(c, "Status").Bold().FontSize(12).Width(95).SingleLine()
			ui.Text(c, "ETA").Bold().FontSize(12).Width(70).SingleLine()
		})
		ui.Divider(c)

		if len(filtered) == 0 {
			ui.Column(c).Fill().Center().Padding(32).Gap(8).Children(func() {
				ui.Text(c, "No downloads in this category").FontSize(15).Bold().TextColor(ui.RGB(130, 140, 155))
				ui.Text(c, "Click '+ Add URL' or download a file from Chrome/Edge with the MagicDown extension.").FontSize(12).TextColor(ui.RGB(130, 140, 155))
			})
			return
		}

		// Scrollable rows
		ui.Scroll(c).Fill().Grow(1).Children(func() {
			ui.Column(c).Fill().Children(func() {
				for _, task := range filtered {
					a.renderTaskRow(c, task)
					ui.Divider(c)
				}
			})
		})
	})
}

func (a *App) renderTaskRow(c *ui.Context, task *engine.Task) {
	isSelected := a.selectedTaskID == task.ID
	bg := ui.Transparent
	if isSelected {
		bg = ui.RGBA(59, 130, 246, 0.16)
	}

	pct := task.ProgressPercentage()
	name := task.Filename
	if name == "" {
		name = task.URL
	}

	sizeText := FormatBytes(task.DownloadedBytes)
	if task.TotalBytes > 0 {
		sizeText = fmt.Sprintf("%s / %s", FormatBytes(task.DownloadedBytes), FormatBytes(task.TotalBytes))
	}

	row := ui.Row(c).Padding(10, 12).Gap(10).Background(bg).Focusable().Children(func() {
		ui.Text(c, name).Width(240).SingleLine().Ellipsis("...")
		ui.Text(c, sizeText).FontSize(12).Width(130).SingleLine()

		// Progress column: bar + percentage text
		ui.Column(c).Grow(1).Gap(4).Children(func() {
			ui.Row(c).Gap(8).Children(func() {
				ui.Progress(c, pct/100.0).Grow(1)
				ui.Text(c, fmt.Sprintf("%.1f%%", pct)).FontSize(11).Width(44).SingleLine()
			})
		})

		ui.Text(c, FormatSpeed(task.Speed)).FontSize(12).Width(90).SingleLine()
		ui.Text(c, string(task.Status)).FontSize(12).Bold().TextColor(statusColor(task.Status)).Width(95).SingleLine()
		ui.Text(c, FormatETA(task.ETA)).FontSize(12).Width(70).SingleLine()
	})

	if row.Clicked() {
		a.selectedTaskID = task.ID
	}
}

func statusColor(st engine.TaskStatus) ui.Color {
	switch st {
	case engine.StatusDownloading, engine.StatusProbing:
		return ui.RGB(59, 130, 246) // Blue
	case engine.StatusCompleted:
		return ui.RGB(34, 197, 94) // Green
	case engine.StatusPaused:
		return ui.RGB(245, 158, 11) // Amber
	case engine.StatusFailed, engine.StatusCancelled:
		return ui.RGB(239, 68, 68) // Red
	default:
		return ui.RGB(148, 163, 184) // Slate
	}
}

func (a *App) renderFooter(c *ui.Context, tasks []*engine.Task) {
	footerText := a.statusMessage
	if a.selectedTaskID != "" {
		for _, t := range tasks {
			if t.ID == a.selectedTaskID {
				segInfo := "Single-stream"
				if t.SupportsRanges && len(t.Segments) > 1 {
					segInfo = fmt.Sprintf("%d segments", len(t.Segments))
				}
				footerText = fmt.Sprintf("Selected: %s  |  %s  |  Save: %s", t.Filename, segInfo, t.SavePath)
				if t.ErrorMessage != "" {
					footerText += "  |  Error: " + t.ErrorMessage
				}
				break
			}
		}
	}
	if footerText == "" {
		footerText = fmt.Sprintf("Ready  |  Download folder: %s", a.engine.Config().DownloadDir)
	}

	ui.Row(c).Padding(6, 12).Children(func() {
		ui.Text(c, footerText).FontSize(11).TextColor(ui.RGB(130, 140, 155)).SingleLine().Ellipsis("...")
	})
}

func (a *App) renderAddURLModal(c *ui.Context) {
	if !a.showAddModal {
		return
	}
	ui.Modal(c, &a.showAddModal, func() {
		ui.Column(c).Width(480).Gap(12).Children(func() {
			ui.Text(c, "Add New Download").FontSize(18).Bold()

			ui.Column(c).Gap(4).Children(func() {
				ui.Text(c, "URL").FontSize(12).Bold()
				ui.TextInput(c, &a.inputURL).Placeholder("https://example.com/file.zip").Width(480)
			})

			ui.Column(c).Gap(4).Children(func() {
				ui.Text(c, "Filename (optional)").FontSize(12).Bold()
				ui.TextInput(c, &a.inputFilename).Placeholder("Leave blank to auto-detect from server/URL").Width(480)
			})

			ui.Column(c).Gap(4).Children(func() {
				ui.Text(c, "Concurrent Segments (1-16)").FontSize(12).Bold()
				ui.TextInput(c, &a.inputSegments).Width(120)
			})

			if a.addError != "" {
				ui.Text(c, a.addError).FontSize(12).TextColor(ui.RGB(239, 68, 68))
			}

			ui.Row(c).Gap(8).Children(func() {
				ui.Spacer(c)
				if ui.Button(c, "Cancel").Clicked() {
					a.showAddModal = false
				}
				if ui.Button(c, "Add to Queue").Clicked() {
					a.submitAddDownload(false)
				}
				if ui.PrimaryButton(c, "Download Now").Clicked() {
					a.submitAddDownload(true)
				}
			})
		})
	})
}

func (a *App) submitAddDownload(autoStart bool) {
	rawURL := strings.TrimSpace(a.inputURL)
	if rawURL == "" || (!strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://")) {
		a.addError = "Please enter a valid http:// or https:// URL."
		return
	}

	segs, err := strconv.Atoi(strings.TrimSpace(a.inputSegments))
	if err != nil || segs < 1 || segs > 16 {
		segs = a.engine.Config().DefaultSegments
	}

	task, err := a.engine.AddTask(engine.AddTaskRequest{
		URL:         rawURL,
		Filename:    strings.TrimSpace(a.inputFilename),
		NumSegments: segs,
		AutoStart:   autoStart,
	})
	if err != nil {
		a.addError = err.Error()
		return
	}

	a.selectedTaskID = task.ID
	a.statusMessage = fmt.Sprintf("Added download: %s", task.URL)
	a.showAddModal = false
}

func (a *App) renderSettingsModal(c *ui.Context) {
	if !a.showSettingsModal {
		return
	}
	ui.Modal(c, &a.showSettingsModal, func() {
		ui.Column(c).Width(480).Gap(12).Children(func() {
			ui.Text(c, "MagicDown Settings").FontSize(18).Bold()

			ui.Column(c).Gap(4).Children(func() {
				ui.Text(c, "Download Directory").FontSize(12).Bold()
				ui.TextInput(c, &a.settingDownloadDir).Width(480)
			})

			ui.Row(c).Gap(16).Children(func() {
				ui.Column(c).Gap(4).Children(func() {
					ui.Text(c, "Max Concurrent Downloads").FontSize(12).Bold()
					ui.TextInput(c, &a.settingMaxConcurrent).Width(140)
				})
				ui.Column(c).Gap(4).Children(func() {
					ui.Text(c, "Default Segments").FontSize(12).Bold()
					ui.TextInput(c, &a.settingSegments).Width(140)
				})
			})

			if a.settingsError != "" {
				ui.Text(c, a.settingsError).FontSize(12).TextColor(ui.RGB(239, 68, 68))
			}

			ui.Row(c).Gap(8).Children(func() {
				ui.Spacer(c)
				if ui.Button(c, "Cancel").Clicked() {
					a.showSettingsModal = false
				}
				if ui.PrimaryButton(c, "Save Settings").Clicked() {
					a.saveSettings()
				}
			})
		})
	})
}

func (a *App) saveSettings() {
	maxC, err := strconv.Atoi(strings.TrimSpace(a.settingMaxConcurrent))
	if err != nil || maxC < 1 || maxC > 20 {
		a.settingsError = "Max concurrent downloads must be between 1 and 20."
		return
	}
	segs, err := strconv.Atoi(strings.TrimSpace(a.settingSegments))
	if err != nil || segs < 1 || segs > 16 {
		a.settingsError = "Default segments must be between 1 and 16."
		return
	}
	dir := strings.TrimSpace(a.settingDownloadDir)
	if dir == "" {
		a.settingsError = "Download directory cannot be empty."
		return
	}

	cfg := a.engine.Config()
	cfg.DownloadDir = dir
	cfg.MaxConcurrentDownloads = maxC
	cfg.DefaultSegments = segs
	a.engine.UpdateConfig(cfg)

	a.statusMessage = "Settings saved"
	a.showSettingsModal = false
}
