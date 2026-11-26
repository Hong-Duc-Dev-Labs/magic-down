package gui

import (
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"magicdown/pkg/engine"
	"magicdown/pkg/i18n"
	"magicdown/pkg/ipc"
)

// DefaultClipboardExtensions lists direct file extensions intercepted when the
// opt-in Clipboard Monitor is enabled in Settings.
var DefaultClipboardExtensions = []string{
	".zip", ".rar", ".7z", ".tar", ".gz", ".bz2", ".xz",
	".exe", ".msi", ".iso", ".dmg", ".apk",
	".mp4", ".mkv", ".webm", ".mp3", ".flac",
	".pdf",
}

// IsMonitorableDownloadURL reports whether raw is a single-line direct HTTP/HTTPS
// URL whose path extension matches one of exts.
func IsMonitorableDownloadURL(raw string, exts []string) bool {
	s := strings.TrimSpace(raw)
	if s == "" || strings.ContainsAny(s, "\r\n \t") {
		return false
	}
	lower := strings.ToLower(s)
	if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		return false
	}
	u, err := url.ParseRequestURI(s)
	if err != nil || u.Host == "" {
		return false
	}
	ext := strings.ToLower(filepath.Ext(u.Path))
	if ext == "" {
		return false
	}
	if len(exts) == 0 {
		exts = DefaultClipboardExtensions
	}
	for _, allowed := range exts {
		norm := strings.ToLower(strings.TrimSpace(allowed))
		if norm == "" {
			continue
		}
		if !strings.HasPrefix(norm, ".") {
			norm = "." + norm
		}
		if ext == norm {
			return true
		}
	}
	return false
}

// pollClipboardMonitor lightly polls the OS clipboard when opt-in monitoring is
// enabled and no modal is open, debounced by last-seen text and 500ms interval.
func (a *App) pollClipboardMonitor(c *ui.Context, anyModalOpen bool) {
	if anyModalOpen || a.engine == nil || !a.engine.Config().MonitorClipboard {
		return
	}
	now := time.Now()
	if !a.lastClipboardCheck.IsZero() && now.Sub(a.lastClipboardCheck) < 500*time.Millisecond {
		return
	}
	a.lastClipboardCheck = now

	clip := strings.TrimSpace(c.ReadClipboard())
	if clip == "" || clip == a.lastClipboardText {
		return
	}
	a.lastClipboardText = clip

	if !IsMonitorableDownloadURL(clip, DefaultClipboardExtensions) {
		return
	}
	if a.engine.FindDuplicate(clip, "") != nil {
		return
	}
	a.EnqueueIncomingDownload(ipc.AddDownloadRequest{
		URL:       clip,
		AutoStart: true,
	})
}

// --- Batch URL Pattern Import Modal ---

func (a *App) openBatchModal() {
	cfg := a.engine.Config()
	a.cancelDeleteTask()
	a.cancelDuplicateModal()
	a.closeRefreshModal()
	a.closeTaskDetails()
	a.showAddModal = false
	a.showSettingsModal = false
	a.showOnboardingModal = false
	a.batchInputText = ""
	a.batchSaveDir = cfg.DownloadDir
	a.batchError = ""
	a.showBatchModal = true
}

func (a *App) closeBatchModal() {
	a.showBatchModal = false
	a.batchError = ""
}

func (a *App) submitBatchImport(autoStart bool) {
	urls, _, err := engine.ExpandBatchURLs(a.batchInputText)
	if err != nil {
		a.batchError = err.Error()
		return
	}
	if len(urls) == 0 {
		a.batchError = i18n.T("batch.err_empty")
		return
	}
	saveDir := strings.TrimSpace(a.batchSaveDir)
	reqs := make([]engine.AddTaskRequest, 0, len(urls))
	for _, u := range urls {
		reqs = append(reqs, engine.AddTaskRequest{
			URL:         u,
			SaveDir:     saveDir,
			AutoStart:   autoStart,
			StartPaused: !autoStart,
		})
	}
	added, err := a.engine.AddTasksBatch(reqs)
	if err != nil {
		a.batchError = err.Error()
		return
	}
	a.cancelPowerCountdown()
	a.closeBatchModal()
	a.statusMessage = i18n.Tf("msg.batch_added", len(added))
}

func (a *App) renderBatchModal(c *ui.Context, pal Palette) {
	if !a.showBatchModal {
		return
	}
	previewURLs, _, previewErr := engine.ExpandBatchURLs(a.batchInputText)

	ui.Modal(c, &a.showBatchModal, func() {
		ui.Column(c).Width(560).Gap(14).Children(func() {
			ui.Row(c).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
				ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
					ui.Row(c).Width(32).Height(32).Radius(8).Background(pal.AccentSoft).Center().Shrink(0).Children(func() {
						ui.Icon(c, iconDownload).Width(16).Height(16).TextColor(pal.Accent)
					})
					ui.Column(c).Gap(1).Children(func() {
						ui.Text(c, i18n.T("batch.title")).FontSize(16).FontWeight(700).TextColor(pal.TextPrimary)
						ui.Text(c, i18n.T("batch.subtitle")).FontSize(12).TextColor(pal.TextMuted)
					})
				})
				if rowIconButton(c, pal, iconClose, false) {
					a.closeBatchModal()
				}
			})

			ui.Divider(c)

			ui.Column(c).Gap(6).Children(func() {
				ui.Row(c).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
					ui.Text(c, i18n.T("batch.urls_label")).FontSize(12).FontWeight(600).TextColor(pal.TextSecondary)
					if toolbarButton(c, pal, iconClipboard, i18n.T("add.paste"), false, false) {
						if clip := strings.TrimSpace(c.ReadClipboard()); clip != "" {
							a.batchInputText = clip
							a.batchError = ""
						}
					}
				})
				ui.TextInput(c, &a.batchInputText).
					Placeholder(i18n.T("batch.urls_placeholder")).
					Background(pal.InputBg).
					Border(1, pal.Border).
					Radius(7)
				ui.Text(c, i18n.T("batch.wildcard_hint")).FontSize(10).TextColor(pal.TextMuted)
			})

			// Live Preview Summary
			if strings.TrimSpace(a.batchInputText) != "" {
				if previewErr != nil {
					ui.Row(c).Padding(8, 10).Radius(6).Background(pal.DangerSoft).Gap(6).AlignItems(ui.Center).Children(func() {
						ui.Icon(c, iconAlert).Width(14).Height(14).TextColor(pal.Danger).Shrink(0)
						ui.Text(c, previewErr.Error()).FontSize(11).TextColor(pal.Danger)
					})
				} else if len(previewURLs) > 0 {
					ui.Column(c).Padding(8, 10).Radius(7).Background(pal.InputBg).Border(1, pal.Border).Gap(4).Children(func() {
						ui.Text(c, i18n.Tf("batch.preview_count", len(previewURLs))).FontSize(11).FontWeight(600).FontFeatures("tnum").TextColor(pal.Accent)
						limit := len(previewURLs)
						if limit > 4 {
							limit = 4
						}
						for i := 0; i < limit; i++ {
							ui.Text(c, previewURLs[i]).FontSize(10).TextColor(pal.TextSecondary).SingleLine().Ellipsis("...")
						}
						if len(previewURLs) > 4 {
							ui.Text(c, i18n.Tf("batch.preview_more", len(previewURLs)-4)).FontSize(10).TextColor(pal.TextMuted)
						}
					})
				}
			}

			// Save Dir
			ui.Column(c).Gap(6).Children(func() {
				ui.Text(c, i18n.T("add.savedir_label")).FontSize(12).FontWeight(600).TextColor(pal.TextSecondary)
				ui.TextInput(c, &a.batchSaveDir).
					Background(pal.InputBg).
					Border(1, pal.Border).
					Radius(7)
			})

			if a.batchError != "" {
				ui.Row(c).Padding(8, 10).Radius(6).Background(pal.DangerSoft).Gap(6).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, iconAlert).Width(14).Height(14).TextColor(pal.Danger).Shrink(0)
					ui.Text(c, a.batchError).FontSize(12).TextColor(pal.Danger)
				})
			}

			ui.Divider(c)

			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Spacer(c)
				if toolbarButton(c, pal, nil, i18n.T("add.cancel"), false, false) {
					a.closeBatchModal()
				}
				if toolbarButton(c, pal, iconClock, i18n.T("add.download_later"), false, false) {
					a.submitBatchImport(false)
				}
				if primaryCTA(c, pal, iconDownload, i18n.T("batch.start_btn")) {
					a.submitBatchImport(true)
				}
			})
		})
	})
}
