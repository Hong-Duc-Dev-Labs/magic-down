package gui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"

	"magicdown/pkg/engine"
	"magicdown/pkg/ipc"
)

// WindowController abstracts native window caption actions so the UI remains decoupled and testable.
type WindowController interface {
	Minimize()
	ToggleMaximize()
	IsMaximized() bool
	Close()
}

// App holds the UI state for MagicDown's MyGo Native UI desktop application.
type App struct {
	engine    *engine.Engine
	ipcServer *ipc.Server
	winCtrl   WindowController
	maximized bool

	darkMode       bool
	filterStatus   string       // "All", "Downloading", "Completed", "Queued", "Failed"
	filterCategory FileCategory // CatAll or specific category
	searchQuery    string
	selectedTaskID string

	// Add / Download Info Dialog state
	showAddModal       bool
	inputFromExtension bool
	inputURL           string
	inputAudioURL      string
	inputFilename      string
	inputCategory      string
	inputSaveDir       string
	inputSegments      int
	inputEstimatedSize int64
	inputReferrer      string
	inputCookies       string
	inputHeaders       map[string]string
	inputConvertFormat string
	inputPlan          *engine.DownloadPlan
	lastAutoTarget     string
	addError           string

	// Incoming extension downloads waiting for the Download Info Dialog
	pendingMu       sync.Mutex
	pendingIncoming []ipc.AddDownloadRequest

	// Settings modal state
	showSettingsModal     bool
	settingDownloadDir    string
	settingMaxConcurrent  int
	settingSegments       int
	settingMaxRetries     string
	settingSpeedLimit     string
	settingAutoCategorize bool
	settingsError         string

	statusMessage string
}

// NewApp creates a new MagicDown UI state instance with sleek dark mode enabled by default.
func NewApp(eng *engine.Engine, srv *ipc.Server) *App {
	cfg := eng.Config()
	return &App{
		engine:                eng,
		ipcServer:             srv,
		darkMode:              true,
		filterStatus:          "All",
		filterCategory:        CatAll,
		inputCategory:         engine.CategoryOther,
		inputSaveDir:          cfg.DownloadDir,
		inputSegments:         cfg.DefaultSegments,
		settingDownloadDir:    cfg.DownloadDir,
		settingMaxConcurrent:  cfg.MaxConcurrentDownloads,
		settingSegments:       cfg.DefaultSegments,
		settingMaxRetries:     strconv.Itoa(cfg.MaxRetries),
		settingSpeedLimit:     strconv.FormatInt(cfg.SpeedLimitKBps, 10),
		settingAutoCategorize: cfg.AutoCategorize,
	}
}

// EnqueueIncomingDownload queues a download received over IPC so the UI opens
// the Download Info Dialog on the next frame.
func (a *App) EnqueueIncomingDownload(req ipc.AddDownloadRequest) {
	a.pendingMu.Lock()
	a.pendingIncoming = append(a.pendingIncoming, req)
	a.pendingMu.Unlock()
}

func (a *App) popPendingIncoming() {
	if a.showAddModal {
		return
	}
	a.pendingMu.Lock()
	if len(a.pendingIncoming) == 0 {
		a.pendingMu.Unlock()
		return
	}
	req := a.pendingIncoming[0]
	a.pendingIncoming = a.pendingIncoming[1:]
	a.pendingMu.Unlock()

	a.openIncomingDownloadDialog(req)
}

func (a *App) openIncomingDownloadDialog(req ipc.AddDownloadRequest) {
	cfg := a.engine.Config()
	a.inputFromExtension = true
	a.inputURL = strings.TrimSpace(req.URL)
	a.inputAudioURL = strings.TrimSpace(req.AudioURL)
	a.inputReferrer = req.Referrer
	a.inputCookies = req.Cookies
	a.inputHeaders = req.Headers
	a.inputConvertFormat = req.ConvertFormat
	a.inputEstimatedSize = req.TotalBytes
	a.inputPlan = nil
	if len(req.Plan) > 0 {
		if parsed, err := engine.ParseDownloadPlan(req.Plan, req.URL); err == nil {
			a.inputPlan = parsed
			if a.inputEstimatedSize <= 0 {
				a.inputEstimatedSize = parsed.KnownSize()
			}
		}
	}

	filename := strings.TrimSpace(req.Filename)
	if filename == "" {
		filename = engine.ExtractFilename(a.inputURL, nil)
		if a.inputAudioURL != "" {
			filename = engine.ReplaceExtension(filename, "mp4")
		}
	}
	a.inputFilename = filename

	cat := req.Category
	if cat == "" {
		if a.inputAudioURL != "" {
			cat = engine.CategoryVideo
		} else {
			cat = engine.CategoryForFilename(a.inputFilename, a.inputURL)
		}
	}
	a.inputCategory = cat
	if strings.TrimSpace(req.SaveDir) != "" {
		a.inputSaveDir = strings.TrimSpace(req.SaveDir)
	} else {
		a.inputSaveDir = engine.ResolveSaveDir(cfg.DownloadDir, cat, cfg.AutoCategorize)
	}
	a.inputSegments = req.NumSegments
	if a.inputSegments != 4 && a.inputSegments != 8 && a.inputSegments != 16 {
		a.inputSegments = cfg.DefaultSegments
		if a.inputSegments != 4 && a.inputSegments != 8 && a.inputSegments != 16 {
			a.inputSegments = 8
		}
	}
	a.lastAutoTarget = a.inputFilename + "|" + a.inputURL
	a.addError = ""
	a.showSettingsModal = false
	a.showAddModal = true
}

// SetWindowController attaches the native MyGo window controller for custom title bar buttons.
func (a *App) SetWindowController(ctrl WindowController) {
	a.winCtrl = ctrl
}

// SetMaximized updates the cached maximize state (wired from Window.OnMaximize/OnUnmaximize).
func (a *App) SetMaximized(maxed bool) {
	a.maximized = maxed
}

func (a *App) palette() Palette {
	if a.darkMode {
		return DarkPalette()
	}
	return LightPalette()
}

// View renders the entire desktop interface for each frame.
func (a *App) View(c *ui.Context) {
	pal := a.palette()
	c.SetTheme(pal.UITheme)

	// Global keyboard shortcuts
	if c.Shortcut(ui.Cmd, ui.KeyN) && !a.showAddModal {
		a.openAddModal()
	}
	if c.Shortcut(0, ui.KeyEscape) {
		a.showAddModal = false
		a.showSettingsModal = false
	}

	// Open Download Info Dialog for any incoming extension download.
	a.popPendingIncoming()

	tasks := a.engine.GetTasks()

	// Animate smoothly while downloads are active; poll lightly otherwise for IPC arrivals.
	if a.engine.ActiveCount() > 0 {
		c.AnimationFrame()
	} else {
		c.After(250 * time.Millisecond)
	}

	var totalSpeed int64
	statusCounts := map[string]int{
		"All":         len(tasks),
		"Downloading": 0,
		"Completed":   0,
		"Queued":      0,
		"Failed":      0,
	}
	catCounts := map[FileCategory]int{
		CatVideo:      0,
		CatMusic:      0,
		CatCompressed: 0,
		CatDocuments:  0,
		CatPrograms:   0,
		CatOther:      0,
	}

	var selectedTask *engine.Task
	completedCount := 0
	for _, t := range tasks {
		if t.ID == a.selectedTaskID {
			selectedTask = t
		}
		totalSpeed += t.Speed
		switch t.Status {
		case engine.StatusDownloading, engine.StatusProbing, engine.StatusMuxing:
			statusCounts["Downloading"]++
		case engine.StatusCompleted:
			statusCounts["Completed"]++
			completedCount++
		case engine.StatusQueued, engine.StatusPaused:
			statusCounts["Queued"]++
		case engine.StatusFailed, engine.StatusCancelled:
			statusCounts["Failed"]++
		}
		catCounts[ClassifyFile(t.Filename, t.URL)]++
	}

	// Root window layout: Integrated Custom Title Bar + (Left Sidebar | Right Main Column)
	ui.Column(c).Fill().Background(pal.Canvas).TextColor(pal.TextPrimary).Children(func() {
		a.renderTitleBar(c, pal)

		ui.Row(c).Fill().Grow(1).AlignItems(ui.Stretch).Children(func() {
			a.renderSidebar(c, pal, statusCounts, catCounts)

			// Main right column: Header + List + Status Bar
			ui.Column(c).Fill().Grow(1).Children(func() {
				a.renderHeader(c, pal, selectedTask, completedCount)
				a.renderDownloadList(c, pal, tasks)
				a.renderStatusBar(c, pal, statusCounts["Downloading"], statusCounts["Queued"], totalSpeed)
			})
		})
	})

	// Modals
	a.renderAddModal(c, pal)
	a.renderSettingsModal(c, pal)
}

func (a *App) renderTitleBar(c *ui.Context, pal Palette) {
	ui.Row(c).
		Height(38).
		Shrink(0).
		Background(pal.Sidebar).
		BorderWidth(0, 0, 1, 0).
		BorderColor(pal.Border).
		AlignItems(ui.Stretch).
		Children(func() {
			// Draggable left + center title strip (supports drag & double-click to maximize)
			ui.Row(c).
				Grow(1).
				Padding(0, 14).
				Gap(8).
				AlignItems(ui.Center).
				DragWindow().
				Children(func() {
					ui.Row(c).Width(20).Height(20).Radius(5).Background(pal.Accent).Center().Shrink(0).Children(func() {
						ui.Icon(c, iconBolt).Width(12).Height(12).TextColor(pal.AccentText)
					})
					ui.Text(c, "MagicDown").FontSize(13).FontWeight(700).TextColor(pal.TextPrimary).SingleLine()
					ui.Box(c).Padding(1, 5).Radius(4).Background(pal.BadgeBg).Shrink(0).Children(func() {
						ui.Text(c, "v1.0").FontSize(10).FontWeight(600).TextColor(pal.TextMuted).SingleLine()
					})
					ui.Text(c, "—").FontSize(11).TextColor(pal.BorderStrong).SingleLine()
					ui.Text(c, "Multi-Segment Download Manager").FontSize(11).TextColor(pal.TextMuted).SingleLine()
					ui.Spacer(c)
				})

			// Native-style Minimize, Maximize/Restore (with Windows 11 Snap Layout), and Close buttons
			ui.Row(c).Shrink(0).AlignItems(ui.Stretch).Children(func() {
				// Minimize
				minBtn := ui.Row(c).Width(46).Center().Cursor(ui.CursorPointer).Focusable()
				minBg := ui.Transparent
				minFg := pal.TextSecondary
				if minBtn.Hovered() {
					minBg = pal.NavHover
					minFg = pal.TextPrimary
				}
				minBtn.Background(minBg).Children(func() {
					ui.Icon(c, iconWinMin).Width(11).Height(11).TextColor(minFg)
				})
				if minBtn.Clicked() && a.winCtrl != nil {
					a.winCtrl.Minimize()
				}

				// Maximize / Restore
				isMax := a.maximized
				if a.winCtrl != nil {
					isMax = a.winCtrl.IsMaximized()
				}
				maxIcon := iconWinMax
				if isMax {
					maxIcon = iconWinRestore
				}
				maxBtn := ui.Row(c).Width(46).Center().Cursor(ui.CursorPointer).Focusable().MaximizeButton()
				maxBg := ui.Transparent
				maxFg := pal.TextSecondary
				if maxBtn.Hovered() {
					maxBg = pal.NavHover
					maxFg = pal.TextPrimary
				}
				maxBtn.Background(maxBg).Children(func() {
					ui.Icon(c, maxIcon).Width(11).Height(11).TextColor(maxFg)
				})
				if maxBtn.Clicked() && a.winCtrl != nil {
					a.winCtrl.ToggleMaximize()
				}

				// Close (semantic Windows red hover)
				closeBtn := ui.Row(c).Width(46).Center().Cursor(ui.CursorPointer).Focusable()
				closeBg := ui.Transparent
				closeFg := pal.TextSecondary
				if closeBtn.Hovered() {
					closeBg = ui.Hex("#E81123")
					closeFg = ui.Hex("#FFFFFF")
				}
				closeBtn.Background(closeBg).Children(func() {
					ui.Icon(c, iconWinClose).Width(11).Height(11).TextColor(closeFg)
				})
				if closeBtn.Clicked() && a.winCtrl != nil {
					a.winCtrl.Close()
				}
			})
		})
}

func (a *App) openAddModal() {
	cfg := a.engine.Config()
	a.inputFromExtension = false
	a.inputURL = ""
	a.inputAudioURL = ""
	a.inputFilename = ""
	a.inputCategory = engine.CategoryOther
	a.inputSaveDir = engine.ResolveSaveDir(cfg.DownloadDir, a.inputCategory, cfg.AutoCategorize)
	a.inputSegments = cfg.DefaultSegments
	if a.inputSegments != 4 && a.inputSegments != 8 && a.inputSegments != 16 {
		a.inputSegments = 8
	}
	a.inputEstimatedSize = 0
	a.inputReferrer = ""
	a.inputCookies = ""
	a.inputHeaders = nil
	a.inputConvertFormat = ""
	a.inputPlan = nil
	a.lastAutoTarget = ""
	a.addError = ""
	a.showSettingsModal = false
	a.showAddModal = true
}

func (a *App) openSettingsModal() {
	cfg := a.engine.Config()
	a.settingDownloadDir = cfg.DownloadDir
	a.settingMaxConcurrent = cfg.MaxConcurrentDownloads
	a.settingSegments = cfg.DefaultSegments
	a.settingMaxRetries = strconv.Itoa(cfg.MaxRetries)
	a.settingSpeedLimit = strconv.FormatInt(cfg.SpeedLimitKBps, 10)
	a.settingAutoCategorize = cfg.AutoCategorize
	a.settingsError = ""
	a.showAddModal = false
	a.showSettingsModal = true
}

func (a *App) renderSidebar(c *ui.Context, pal Palette, statusCounts map[string]int, catCounts map[FileCategory]int) {
	ui.Column(c).
		Width(220).
		Shrink(0).
		Background(pal.Sidebar).
		BorderWidth(0, 1, 0, 0).
		BorderColor(pal.Border).
		Padding(14, 12, 12, 12).
		Gap(14).
		Children(func() {
			// Status section
			ui.Column(c).Gap(3).Children(func() {
				ui.Text(c, "DOWNLOADS").FontSize(10).FontWeight(700).TextColor(pal.TextMuted).Padding(0, 10, 4, 10)
				statusNav := []struct {
					key   string
					label string
					icon  *ui.SVG
				}{
					{"All", "All Downloads", iconInbox},
					{"Downloading", "Downloading", iconDownload},
					{"Completed", "Completed", iconCheck},
					{"Queued", "Queued & Paused", iconClock},
					{"Failed", "Failed", iconAlert},
				}
				for _, item := range statusNav {
					active := a.filterStatus == item.key && a.filterCategory == CatAll
					if navPill(c, pal, item.icon, item.label, statusCounts[item.key], active) {
						a.filterStatus = item.key
						a.filterCategory = CatAll
					}
				}
			})

			// Categories section
			ui.Column(c).Gap(3).Children(func() {
				ui.Text(c, "CATEGORIES").FontSize(10).FontWeight(700).TextColor(pal.TextMuted).Padding(0, 10, 4, 10)
				catNav := []struct {
					cat   FileCategory
					label string
					icon  *ui.SVG
				}{
					{CatVideo, "Video", iconMedia},
					{CatMusic, "Music", iconMedia},
					{CatCompressed, "Compressed", iconArchive},
					{CatDocuments, "Documents", iconDoc},
					{CatPrograms, "Programs", iconProgram},
					{CatOther, "Other", iconFile},
				}
				for _, item := range catNav {
					active := a.filterCategory == item.cat
					if navPill(c, pal, item.icon, item.label, catCounts[item.cat], active) {
						if a.filterCategory == item.cat {
							a.filterCategory = CatAll
						} else {
							a.filterCategory = item.cat
							a.filterStatus = "All"
						}
					}
				}
			})

			ui.Spacer(c)

			// Bottom pinned utility controls
			ui.Column(c).Gap(3).BorderWidth(1, 0, 0, 0).BorderColor(pal.Border).Padding(10, 0, 0, 0).Children(func() {
				themeIcon := iconSun
				themeLabel := "Light Mode"
				if !a.darkMode {
					themeIcon = iconMoon
					themeLabel = "Dark Mode"
				}
				if navPill(c, pal, themeIcon, themeLabel, -1, false) {
					a.darkMode = !a.darkMode
				}
				if navPill(c, pal, iconSettings, "Settings", -1, a.showSettingsModal) {
					a.openSettingsModal()
				}
			})
		})
}

func (a *App) renderHeader(c *ui.Context, pal Palette, selected *engine.Task, completedCount int) {
	canResume := selected != nil && (selected.Status == engine.StatusPaused || selected.Status == engine.StatusFailed || selected.Status == engine.StatusQueued)
	canPause := selected != nil && (selected.Status == engine.StatusDownloading || selected.Status == engine.StatusProbing)
	canDelete := selected != nil
	canClearDone := completedCount > 0

	ui.Row(c).
		Height(60).
		Shrink(0).
		Padding(0, 20).
		Gap(10).
		Background(pal.Header).
		BorderWidth(0, 0, 1, 0).
		BorderColor(pal.Border).
		AlignItems(ui.Center).
		Children(func() {
			// Search field container
			ui.Row(c).
				Width(260).
				Padding(0, 10).
				Gap(7).
				Radius(8).
				Background(pal.InputBg).
				Border(1, pal.Border).
				AlignItems(ui.Center).
				Children(func() {
					ui.Icon(c, iconSearch).Width(14).Height(14).TextColor(pal.TextMuted).Shrink(0)
					ui.TextInput(c, &a.searchQuery).
						Placeholder("Search downloads...").
						Background(ui.Transparent).
						Border(0, ui.Transparent).
						Padding(6, 0).
						Grow(1)
				})

			ui.Spacer(c)

			// Contextual action buttons
			if toolbarButton(c, pal, iconPlay, "Resume", !canResume, false) {
				if err := a.engine.ResumeTask(selected.ID); err != nil {
					a.statusMessage = err.Error()
				} else {
					a.statusMessage = "Resumed " + selected.Filename
				}
			}

			if toolbarButton(c, pal, iconPause, "Pause", !canPause, false) {
				if err := a.engine.PauseTask(selected.ID); err != nil {
					a.statusMessage = err.Error()
				} else {
					a.statusMessage = "Paused " + selected.Filename
				}
			}

			if toolbarButton(c, pal, iconTrash, "Delete", !canDelete, true) {
				if err := a.engine.DeleteTask(selected.ID, false); err != nil {
					a.statusMessage = err.Error()
				} else {
					a.statusMessage = "Removed " + selected.Filename
					a.selectedTaskID = ""
				}
			}

			if toolbarButton(c, pal, iconCheck, "Clear Done", !canClearDone, false) {
				removed := 0
				for _, t := range a.engine.GetTasks() {
					if t.Status == engine.StatusCompleted {
						if err := a.engine.DeleteTask(t.ID, false); err == nil {
							removed++
							if a.selectedTaskID == t.ID {
								a.selectedTaskID = ""
							}
						}
					}
				}
				a.statusMessage = fmt.Sprintf("Cleared %d completed download(s)", removed)
			}

			// Primary CTA
			if primaryCTA(c, pal, iconPlus, "New Download") {
				a.openAddModal()
			}
		})
}

func (a *App) filterTasks(tasks []*engine.Task) []*engine.Task {
	query := strings.ToLower(strings.TrimSpace(a.searchQuery))
	filtered := make([]*engine.Task, 0, len(tasks))
	for _, t := range tasks {
		// Category filter
		if a.filterCategory != CatAll && ClassifyFile(t.Filename, t.URL) != a.filterCategory {
			continue
		}
		// Status filter
		switch a.filterStatus {
		case "Downloading":
			if t.Status != engine.StatusDownloading && t.Status != engine.StatusProbing {
				continue
			}
		case "Completed":
			if t.Status != engine.StatusCompleted {
				continue
			}
		case "Queued":
			if t.Status != engine.StatusQueued && t.Status != engine.StatusPaused {
				continue
			}
		case "Failed":
			if t.Status != engine.StatusFailed && t.Status != engine.StatusCancelled {
				continue
			}
		}
		// Search filter
		if query != "" {
			if !strings.Contains(strings.ToLower(t.Filename), query) &&
				!strings.Contains(strings.ToLower(t.URL), query) {
				continue
			}
		}
		filtered = append(filtered, t)
	}
	return filtered
}

func (a *App) renderDownloadList(c *ui.Context, pal Palette, allTasks []*engine.Task) {
	filtered := a.filterTasks(allTasks)

	if len(filtered) == 0 {
		a.renderEmptyState(c, pal, len(allTasks) == 0)
		return
	}

	ui.Scroll(c).Fill().Grow(1).Children(func() {
		ui.Column(c).Fill().Padding(16, 20).Gap(10).Children(func() {
			for _, task := range filtered {
				a.renderDownloadCard(c, pal, task)
			}
		})
	})
}

func (a *App) renderEmptyState(c *ui.Context, pal Palette, noDownloadsAtAll bool) {
	ui.Column(c).Fill().Grow(1).Center().Padding(40).Gap(12).Children(func() {
		ui.Row(c).Width(56).Height(56).Radius(14).Background(pal.Card).Border(1, pal.Border).Center().Children(func() {
			ui.Icon(c, iconDownload).Width(24).Height(24).TextColor(pal.Accent)
		})

		title := "No downloads yet"
		subtitle := "Add a URL or click any file in Chrome/Edge to accelerate downloads with multi-segment streams."
		if !noDownloadsAtAll {
			title = "No matching downloads"
			subtitle = "Try clearing your search query or switching to another status or category."
		}

		ui.Text(c, title).FontSize(16).FontWeight(600).TextColor(pal.TextPrimary)
		ui.Text(c, subtitle).FontSize(13).TextColor(pal.TextSecondary).MaxWidth(420)

		ui.Box(c).Height(4)
		if noDownloadsAtAll {
			if primaryCTA(c, pal, iconPlus, "Add New Download") {
				a.openAddModal()
			}
		} else if toolbarButton(c, pal, iconRefresh, "Reset Filters", false, false) {
			a.filterStatus = "All"
			a.filterCategory = CatAll
			a.searchQuery = ""
		}
	})
}

func (a *App) renderDownloadCard(c *ui.Context, pal Palette, task *engine.Task) {
	isSelected := a.selectedTaskID == task.ID
	cat := ClassifyFile(task.Filename, task.URL)
	catIcon, catFg, catBg := categoryVisual(cat, pal)

	card := ui.Column(c).
		Padding(14, 16).
		Gap(10).
		Radius(10).
		Cursor(ui.CursorPointer).
		Focusable()

	bg := pal.Card
	border := pal.Border
	if isSelected {
		bg = pal.CardSelected
		border = pal.Accent
	} else if card.Hovered() {
		bg = pal.CardHover
		border = pal.BorderStrong
	}

	name := task.Filename
	if name == "" {
		name = extractHost(task.URL)
	}
	host := extractHost(task.URL)
	pct := task.ProgressPercentage()

	barColor := pal.Accent
	switch task.Status {
	case engine.StatusCompleted:
		barColor = pal.Success
	case engine.StatusPaused, engine.StatusQueued:
		barColor = pal.Warning
	case engine.StatusFailed, engine.StatusCancelled:
		barColor = pal.Danger
	}

	segLabel := "1 stream"
	if task.SupportsRanges && len(task.Segments) > 1 {
		segLabel = fmt.Sprintf("%d segs", len(task.Segments))
	} else if task.NumSegments > 1 {
		segLabel = fmt.Sprintf("%d segs", task.NumSegments)
	}

	card.Background(bg).Border(1, border).Children(func() {
		// Top row: Category icon + Filename/Host + Status Pill + Inline quick actions
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			// File category badge
			ui.Row(c).Width(38).Height(38).Radius(9).Background(catBg).Center().Shrink(0).Children(func() {
				ui.Icon(c, catIcon).Width(18).Height(18).TextColor(catFg)
			})

			// Title + metadata
			ui.Column(c).Gap(3).Grow(1).Children(func() {
				ui.Text(c, name).FontSize(14).FontWeight(600).TextColor(pal.TextPrimary).SingleLine().Ellipsis("...")
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, host).FontSize(12).TextColor(pal.TextSecondary).SingleLine().Ellipsis("...")
					ui.Box(c).Padding(1, 6).Radius(4).Background(pal.BadgeBg).Shrink(0).Children(func() {
						ui.Text(c, segLabel).FontSize(10).FontWeight(600).FontFeatures("tnum").TextColor(pal.TextMuted).SingleLine()
					})
					if task.Retries > 0 && (task.Status == engine.StatusDownloading || task.Status == engine.StatusProbing) {
						ui.Box(c).Padding(1, 6).Radius(4).Background(pal.WarningSoft).Shrink(0).Children(func() {
							ui.Text(c, fmt.Sprintf("retry %d", task.Retries)).FontSize(10).FontWeight(600).FontFeatures("tnum").TextColor(pal.Warning).SingleLine()
						})
					}
					if task.ErrorMessage != "" {
						ui.Text(c, task.ErrorMessage).FontSize(11).TextColor(pal.Danger).SingleLine().Ellipsis("...")
					}
				})
			})

			// Status badge
			statusBadge(c, pal, task.Status)

			// Inline quick action icons
			ui.Row(c).Gap(2).AlignItems(ui.Center).Shrink(0).Children(func() {
				switch task.Status {
				case engine.StatusDownloading, engine.StatusProbing:
					if rowIconButton(c, pal, iconPause, false) {
						_ = a.engine.PauseTask(task.ID)
						a.statusMessage = "Paused " + name
					}
				case engine.StatusPaused, engine.StatusQueued:
					if rowIconButton(c, pal, iconPlay, false) {
						_ = a.engine.ResumeTask(task.ID)
						a.statusMessage = "Resumed " + name
					}
				case engine.StatusFailed, engine.StatusCancelled:
					if rowIconButton(c, pal, iconRefresh, false) {
						_ = a.engine.ResumeTask(task.ID)
						a.statusMessage = "Retrying " + name
					}
				}

				if task.SavePath != "" {
					if rowIconButton(c, pal, iconFolder, false) {
						openInFileManager(task.SavePath)
						a.statusMessage = "Opened folder for " + name
					}
				}

				if rowIconButton(c, pal, iconTrash, true) {
					_ = a.engine.DeleteTask(task.ID, false)
					if a.selectedTaskID == task.ID {
						a.selectedTaskID = ""
					}
					a.statusMessage = "Removed " + name
				}
			})
		})

		// Bottom row: Rounded progress bar + right-aligned tabular metrics
		ui.Row(c).Gap(14).AlignItems(ui.Center).Children(func() {
			ui.Progress(c, pct/100.0).
				Height(6).
				Radius(999).
				Background(pal.TrackBg).
				TextColor(barColor).
				Grow(1)

			// Tabular metrics
			sizeText := FormatBytes(task.DownloadedBytes)
			if task.TotalBytes > 0 {
				sizeText = fmt.Sprintf("%s / %s", FormatBytes(task.DownloadedBytes), FormatBytes(task.TotalBytes))
			}

			ui.Row(c).Gap(10).AlignItems(ui.Center).Shrink(0).Children(func() {
				ui.Text(c, sizeText).FontSize(12).FontFeatures("tnum").TextColor(pal.TextSecondary).SingleLine()

				if task.Status == engine.StatusDownloading && task.Speed > 0 {
					ui.Text(c, "•").FontSize(11).TextColor(pal.TextMuted)
					ui.Text(c, FormatSpeed(task.Speed)).FontSize(12).FontWeight(600).FontFeatures("tnum").TextColor(pal.Accent).SingleLine()
					if task.ETA > 0 {
						ui.Text(c, "•").FontSize(11).TextColor(pal.TextMuted)
						ui.Text(c, FormatETA(task.ETA)).FontSize(12).FontFeatures("tnum").TextColor(pal.TextSecondary).SingleLine()
					}
				}

				ui.Text(c, fmt.Sprintf("%.1f%%", pct)).
					FontSize(12).
					FontWeight(600).
					FontFeatures("tnum").
					TextColor(pal.TextPrimary).
					MinWidth(52).
					SingleLine()
			})
		})
	})

	if card.Clicked() {
		a.selectedTaskID = task.ID
	}
}

func (a *App) renderStatusBar(c *ui.Context, pal Palette, activeCount, queuedCount int, totalSpeed int64) {
	ipcPort := ipc.DefaultPort
	ipcConnected := false
	if a.ipcServer != nil {
		ipcPort = a.ipcServer.Port()
		ipcConnected = ipcPort > 0
	}

	dotColor := pal.Success
	ipcLabel := fmt.Sprintf("Extension IPC :%d Ready", ipcPort)
	if !ipcConnected {
		dotColor = pal.Warning
		ipcLabel = "IPC Offline"
	}

	ui.Row(c).
		Height(34).
		Shrink(0).
		Padding(0, 20).
		Gap(12).
		Background(pal.Header).
		BorderWidth(1, 0, 0, 0).
		BorderColor(pal.Border).
		AlignItems(ui.Center).
		Children(func() {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Shrink(0).Children(func() {
				ui.Box(c).Width(7).Height(7).Radius(999).Background(dotColor)
				ui.Text(c, ipcLabel).FontSize(11).FontWeight(500).TextColor(pal.TextSecondary).SingleLine()
			})

			if a.statusMessage != "" {
				ui.Text(c, "|").FontSize(11).TextColor(pal.BorderStrong)
				ui.Text(c, a.statusMessage).FontSize(11).TextColor(pal.TextMuted).SingleLine().Ellipsis("...")
			}

			ui.Spacer(c)

			summary := fmt.Sprintf("Active: %d   •   Queued: %d   •   Speed: %s",
				activeCount, queuedCount, FormatSpeed(totalSpeed))
			ui.Text(c, summary).FontSize(11).FontWeight(500).FontFeatures("tnum").TextColor(pal.TextSecondary).SingleLine()
		})
}

func (a *App) renderAddModal(c *ui.Context, pal Palette) {
	if !a.showAddModal {
		return
	}
	if c.Shortcut(0, ui.KeyEnter) {
		a.submitAddDownload(true)
		return
	}

	ui.Modal(c, &a.showAddModal, func() {
		ui.Column(c).Width(520).Gap(16).Children(func() {
			// Auto-update category and save dir when URL or filename changes
			autoTarget := strings.TrimSpace(a.inputFilename) + "|" + strings.TrimSpace(a.inputURL)
			if autoTarget != a.lastAutoTarget {
				a.lastAutoTarget = autoTarget
				cfg := a.engine.Config()
				detectedCat := engine.CategoryForFilename(strings.TrimSpace(a.inputFilename), strings.TrimSpace(a.inputURL))
				if a.inputAudioURL != "" {
					detectedCat = engine.CategoryVideo
				}
				a.inputCategory = detectedCat
				a.inputSaveDir = engine.ResolveSaveDir(cfg.DownloadDir, detectedCat, cfg.AutoCategorize)
			}

			// Modal Header
			modalTitle := "New Download"
			modalSub := "Enter a direct HTTP/HTTPS link to accelerate"
			if a.inputFromExtension {
				modalTitle = "Download File Info"
				modalSub = "Review destination, category, and filename before starting"
			}
			ui.Row(c).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
				ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
					ui.Row(c).Width(32).Height(32).Radius(8).Background(pal.AccentSoft).Center().Children(func() {
						ui.Icon(c, iconPlus).Width(16).Height(16).TextColor(pal.Accent)
					})
					ui.Column(c).Gap(1).Children(func() {
						ui.Text(c, modalTitle).FontSize(16).FontWeight(700).TextColor(pal.TextPrimary)
						ui.Text(c, modalSub).FontSize(12).TextColor(pal.TextMuted)
					})
				})
				if rowIconButton(c, pal, iconClose, false) {
					a.showAddModal = false
				}
			})

			ui.Divider(c)

			// Estimated size & Auto-Mux badge bar
			if a.inputEstimatedSize > 0 || a.inputAudioURL != "" {
				ui.Row(c).Padding(8, 10).Radius(7).Background(pal.InputBg).Border(1, pal.Border).Gap(10).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
					sizeLabel := "Estimated Size: Unknown"
					if a.inputEstimatedSize > 0 {
						sizeLabel = "Estimated Size: " + FormatBytes(a.inputEstimatedSize)
					}
					ui.Text(c, sizeLabel).FontSize(12).FontWeight(600).TextColor(pal.TextSecondary)
					if a.inputAudioURL != "" {
						ui.Box(c).Padding(2, 7).Radius(999).Background(pal.PurpleSoft).Children(func() {
							ui.Text(c, "Video + Audio (Auto-Mux MP4)").FontSize(11).FontWeight(600).TextColor(pal.Purple)
						})
					}
				})
			}

			// URL Field + Clipboard Paste button
			ui.Column(c).Gap(6).Children(func() {
				ui.Text(c, "Download URL").FontSize(12).FontWeight(600).TextColor(pal.TextSecondary)
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.TextInput(c, &a.inputURL).
						Placeholder("https://example.com/archive.zip").
						Background(pal.InputBg).
						Border(1, pal.Border).
						Radius(7).
						Grow(1)
					if toolbarButton(c, pal, iconClipboard, "Paste", false, false) {
						clip := strings.TrimSpace(c.ReadClipboard())
						if clip != "" {
							a.inputURL = clip
							a.addError = ""
						}
					}
				})
				if strings.TrimSpace(a.inputFilename) == "" && strings.HasPrefix(strings.TrimSpace(a.inputURL), "http") {
					preview := engine.ExtractFilename(strings.TrimSpace(a.inputURL), nil)
					ui.Text(c, "Auto-detected filename: "+preview).FontSize(11).TextColor(pal.Accent)
				}
			})

			// Filename
			ui.Column(c).Gap(6).Children(func() {
				ui.Text(c, "Filename").FontSize(12).FontWeight(600).TextColor(pal.TextSecondary)
				ui.TextInput(c, &a.inputFilename).
					Placeholder("Leave blank to auto-detect from server or URL").
					Background(pal.InputBg).
					Border(1, pal.Border).
					Radius(7)
			})

			// Category Selector
			ui.Column(c).Gap(6).Children(func() {
				ui.Text(c, "Category").FontSize(12).FontWeight(600).TextColor(pal.TextSecondary)
				ui.Row(c).Gap(6).Children(func() {
					cfg := a.engine.Config()
					for _, cat := range engine.AllCategories {
						if segmentOption(c, pal, cat, a.inputCategory == cat) {
							a.inputCategory = cat
							a.inputSaveDir = engine.ResolveSaveDir(cfg.DownloadDir, cat, cfg.AutoCategorize)
						}
					}
				})
			})

			// Save Dir
			ui.Column(c).Gap(6).Children(func() {
				ui.Text(c, "Save Directory").FontSize(12).FontWeight(600).TextColor(pal.TextSecondary)
				ui.TextInput(c, &a.inputSaveDir).
					Background(pal.InputBg).
					Border(1, pal.Border).
					Radius(7)
			})

			// Segment Count Selector
			ui.Column(c).Gap(6).Children(func() {
				ui.Text(c, "Concurrent Segments").FontSize(12).FontWeight(600).TextColor(pal.TextSecondary)
				ui.Row(c).Gap(8).Children(func() {
					for _, seg := range []int{4, 8, 16} {
						if segmentOption(c, pal, fmt.Sprintf("%d connections", seg), a.inputSegments == seg) {
							a.inputSegments = seg
						}
					}
				})
			})

			if a.addError != "" {
				ui.Row(c).Padding(8, 10).Radius(6).Background(pal.DangerSoft).Gap(6).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, iconAlert).Width(14).Height(14).TextColor(pal.Danger)
					ui.Text(c, a.addError).FontSize(12).TextColor(pal.Danger)
				})
			}

			ui.Divider(c)

			// Modal Footer
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Spacer(c)
				if toolbarButton(c, pal, nil, "Cancel", false, false) {
					a.showAddModal = false
				}
				if toolbarButton(c, pal, iconClock, "Download Later", false, false) {
					a.submitAddDownload(false)
				}
				if primaryCTA(c, pal, iconDownload, "Start Download") {
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

	segs := a.inputSegments
	if segs < 1 || segs > 16 {
		segs = a.engine.Config().DefaultSegments
	}

	task, err := a.engine.AddTask(engine.AddTaskRequest{
		URL:           rawURL,
		AudioURL:      strings.TrimSpace(a.inputAudioURL),
		Category:      a.inputCategory,
		Filename:      strings.TrimSpace(a.inputFilename),
		SaveDir:       strings.TrimSpace(a.inputSaveDir),
		NumSegments:   segs,
		TotalBytes:    a.inputEstimatedSize,
		Referrer:      a.inputReferrer,
		Cookies:       a.inputCookies,
		Headers:       a.inputHeaders,
		ConvertFormat: a.inputConvertFormat,
		Plan:          a.inputPlan,
		AutoStart:     autoStart,
		StartPaused:   !autoStart,
	})
	if err != nil {
		a.addError = err.Error()
		return
	}

	a.selectedTaskID = task.ID
	a.statusMessage = fmt.Sprintf("Added download: %s", task.URL)
	a.showAddModal = false
}

func (a *App) renderSettingsModal(c *ui.Context, pal Palette) {
	if !a.showSettingsModal {
		return
	}

	ui.Modal(c, &a.showSettingsModal, func() {
		ui.Column(c).Width(520).Gap(16).Children(func() {
			// Header
			ui.Row(c).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
				ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
					ui.Row(c).Width(32).Height(32).Radius(8).Background(pal.AccentSoft).Center().Children(func() {
						ui.Icon(c, iconSettings).Width(16).Height(16).TextColor(pal.Accent)
					})
					ui.Column(c).Gap(1).Children(func() {
						ui.Text(c, "Settings").FontSize(16).FontWeight(700).TextColor(pal.TextPrimary)
						ui.Text(c, "Configure downloads, concurrency, and browser integration").FontSize(12).TextColor(pal.TextMuted)
					})
				})
				if rowIconButton(c, pal, iconClose, false) {
					a.showSettingsModal = false
				}
			})

			ui.Divider(c)

			// Download Directory
			ui.Column(c).Gap(6).Children(func() {
				ui.Text(c, "Default Download Directory").FontSize(12).FontWeight(600).TextColor(pal.TextSecondary)
				ui.TextInput(c, &a.settingDownloadDir).
					Background(pal.InputBg).
					Border(1, pal.Border).
					Radius(7)
			})

			// Max Concurrent Downloads
			ui.Column(c).Gap(6).Children(func() {
				ui.Text(c, "Max Concurrent Downloads").FontSize(12).FontWeight(600).TextColor(pal.TextSecondary)
				ui.Row(c).Gap(8).Children(func() {
					for _, n := range []int{1, 2, 3, 5, 8} {
						if segmentOption(c, pal, fmt.Sprintf("%d", n), a.settingMaxConcurrent == n) {
							a.settingMaxConcurrent = n
						}
					}
				})
			})

			// Default Segments
			ui.Column(c).Gap(6).Children(func() {
				ui.Text(c, "Default Segments per File").FontSize(12).FontWeight(600).TextColor(pal.TextSecondary)
				ui.Row(c).Gap(8).Children(func() {
					for _, seg := range []int{4, 8, 16} {
						if segmentOption(c, pal, fmt.Sprintf("%d connections", seg), a.settingSegments == seg) {
							a.settingSegments = seg
						}
					}
				})
			})

			// Reliability & throughput limits
			ui.Row(c).Gap(16).Children(func() {
				ui.Column(c).Gap(6).Grow(1).Children(func() {
					ui.Text(c, "Max Retries per Download").FontSize(12).FontWeight(600).TextColor(pal.TextSecondary)
					ui.TextInput(c, &a.settingMaxRetries).
						Background(pal.InputBg).
						Border(1, pal.Border).
						Radius(7)
					ui.Text(c, "0 = no retry").FontSize(10).TextColor(pal.TextMuted)
				})
				ui.Column(c).Gap(6).Grow(1).Children(func() {
					ui.Text(c, "Speed Limit (KB/s)").FontSize(12).FontWeight(600).TextColor(pal.TextSecondary)
					ui.TextInput(c, &a.settingSpeedLimit).
						Background(pal.InputBg).
						Border(1, pal.Border).
						Radius(7)
					ui.Text(c, "0 = unlimited").FontSize(10).TextColor(pal.TextMuted)
				})
			})

			// Auto-Categorize toggle
			ui.Column(c).Gap(6).Children(func() {
				ui.Text(c, "Auto-Categorize Subfolders").FontSize(12).FontWeight(600).TextColor(pal.TextSecondary)
				ui.Row(c).Gap(8).Children(func() {
					if segmentOption(c, pal, "Enabled (Video, Music, ...)", a.settingAutoCategorize) {
						a.settingAutoCategorize = true
					}
					if segmentOption(c, pal, "Disabled", !a.settingAutoCategorize) {
						a.settingAutoCategorize = false
					}
				})
			})

			// Appearance toggle
			ui.Column(c).Gap(6).Children(func() {
				ui.Text(c, "Appearance").FontSize(12).FontWeight(600).TextColor(pal.TextSecondary)
				ui.Row(c).Gap(8).Children(func() {
					if segmentOption(c, pal, "Dark Theme", a.darkMode) {
						a.darkMode = true
					}
					if segmentOption(c, pal, "Light Theme", !a.darkMode) {
						a.darkMode = false
					}
				})
			})

			// Native Messaging & IPC Status Info Card
			ipcPort := ipc.DefaultPort
			if a.ipcServer != nil {
				ipcPort = a.ipcServer.Port()
			}
			ui.Row(c).Padding(10, 12).Radius(8).Background(pal.InputBg).Border(1, pal.Border).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
				ui.Column(c).Gap(2).Children(func() {
					ui.Text(c, "Chrome / Edge Native Messaging").FontSize(12).FontWeight(600).TextColor(pal.TextPrimary)
					ui.Text(c, fmt.Sprintf("Host: com.magicdown.downloader  •  IPC 127.0.0.1:%d", ipcPort)).FontSize(11).FontFeatures("tnum").TextColor(pal.TextMuted)
				})
				ui.Row(c).Padding(3, 8).Radius(999).Background(pal.SuccessSoft).Gap(5).AlignItems(ui.Center).Children(func() {
					ui.Box(c).Width(6).Height(6).Radius(999).Background(pal.Success)
					ui.Text(c, "Listening").FontSize(11).FontWeight(600).TextColor(pal.Success)
				})
			})

			if a.settingsError != "" {
				ui.Row(c).Padding(8, 10).Radius(6).Background(pal.DangerSoft).Gap(6).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, iconAlert).Width(14).Height(14).TextColor(pal.Danger)
					ui.Text(c, a.settingsError).FontSize(12).TextColor(pal.Danger)
				})
			}

			ui.Divider(c)

			ui.Row(c).Gap(8).Children(func() {
				ui.Spacer(c)
				if toolbarButton(c, pal, nil, "Cancel", false, false) {
					a.showSettingsModal = false
				}
				if primaryCTA(c, pal, iconCheck, "Save Changes") {
					a.saveSettings()
				}
			})
		})
	})
}

func (a *App) saveSettings() {
	dir := strings.TrimSpace(a.settingDownloadDir)
	if dir == "" {
		a.settingsError = "Download directory cannot be empty."
		return
	}
	maxC := a.settingMaxConcurrent
	if maxC < 1 || maxC > 20 {
		maxC = 3
	}
	segs := a.settingSegments
	if segs < 1 || segs > 16 {
		segs = 8
	}
	retries, err := strconv.Atoi(strings.TrimSpace(a.settingMaxRetries))
	if err != nil || retries < 0 {
		a.settingsError = "Max retries must be a number of 0 or greater (0 = no retry)."
		return
	}
	if retries > 20 {
		retries = 20
	}
	speedKBps, err := strconv.ParseInt(strings.TrimSpace(a.settingSpeedLimit), 10, 64)
	if err != nil || speedKBps < 0 {
		a.settingsError = "Speed limit must be 0 (unlimited) or a positive number of KB/s."
		return
	}
	if speedKBps > 1048576 {
		speedKBps = 1048576
	}

	cfg := a.engine.Config()
	cfg.DownloadDir = dir
	cfg.MaxConcurrentDownloads = maxC
	cfg.DefaultSegments = segs
	cfg.MaxRetries = retries
	cfg.SpeedLimitKBps = speedKBps
	cfg.AutoCategorize = a.settingAutoCategorize
	a.engine.UpdateConfig(cfg)

	a.settingMaxRetries = strconv.Itoa(retries)
	a.settingSpeedLimit = strconv.FormatInt(speedKBps, 10)
	a.statusMessage = "Settings saved"
	a.showSettingsModal = false
}

func openInFileManager(targetPath string) {
	go func() {
		if runtime.GOOS == "windows" {
			if _, err := os.Stat(targetPath); err == nil {
				_ = exec.Command("explorer.exe", "/select,"+targetPath).Start()
				return
			}
			_ = exec.Command("explorer.exe", filepath.Dir(targetPath)).Start()
		}
	}()
}
