package gui

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"

	"magicdown/pkg/engine"
	"magicdown/pkg/i18n"
	"magicdown/pkg/power"
)

func (a *App) openTaskDetails(task *engine.Task) {
	if task == nil {
		return
	}
	a.cancelDeleteTask()
	a.cancelDuplicateModal()
	a.closeRefreshModal()
	a.closeBatchModal()
	a.showAddModal = false
	a.showSettingsModal = false
	a.showOnboardingModal = false

	a.detailsTaskID = task.ID
	a.detailsSpeedLimit = strconv.FormatInt(task.SpeedLimitKBps, 10)
	a.detailsSpeedLimitErr = ""

	a.checksumMu.Lock()
	if a.checksumTaskID != task.ID {
		if a.checksumCancel != nil {
			a.checksumCancel()
			a.checksumCancel = nil
		}
		a.checksumTaskID = task.ID
		a.checksumRunning = false
		a.checksumHashes = nil
		a.checksumErr = ""
		a.detailsExpectedHash = ""
	}
	a.checksumMu.Unlock()
}

func (a *App) closeTaskDetails() {
	a.detailsTaskID = ""
	a.detailsSpeedLimitErr = ""
	a.checksumMu.Lock()
	if a.checksumCancel != nil {
		a.checksumCancel()
		a.checksumCancel = nil
	}
	a.checksumRunning = false
	a.checksumMu.Unlock()
}

func (a *App) applyTaskSpeedLimit() {
	if a.detailsTaskID == "" || a.engine == nil {
		return
	}
	val, err := strconv.ParseInt(strings.TrimSpace(a.detailsSpeedLimit), 10, 64)
	if err != nil || val < 0 {
		a.detailsSpeedLimitErr = i18n.T("details.err_speed_limit")
		return
	}
	if val > 1048576 {
		val = 1048576
	}
	if err := a.engine.SetTaskSpeedLimit(a.detailsTaskID, val); err != nil {
		a.detailsSpeedLimitErr = err.Error()
		return
	}
	a.detailsSpeedLimit = strconv.FormatInt(val, 10)
	a.detailsSpeedLimitErr = ""
	name := a.detailsTaskID
	if task, err := a.engine.GetTask(a.detailsTaskID); err == nil && task != nil && task.Filename != "" {
		name = task.Filename
	}
	a.statusMessage = i18n.Tf("msg.task_limit_updated", name)
}

func (a *App) startChecksumComputation(taskID, savePath string) {
	if taskID == "" || strings.TrimSpace(savePath) == "" {
		return
	}
	a.checksumMu.Lock()
	if a.checksumRunning && a.checksumTaskID == taskID {
		a.checksumMu.Unlock()
		return
	}
	if a.checksumCancel != nil {
		a.checksumCancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.checksumTaskID = taskID
	a.checksumCancel = cancel
	a.checksumRunning = true
	a.checksumHashes = nil
	a.checksumErr = ""
	a.checksumMu.Unlock()
	a.invalidateUI()

	go func() {
		hashes, err := engine.ComputeFileHashes(ctx, savePath)
		a.checksumMu.Lock()
		if a.checksumTaskID == taskID {
			a.checksumRunning = false
			a.checksumCancel = nil
			if err != nil {
				if ctx.Err() == nil {
					a.checksumErr = err.Error()
				}
			} else {
				h := hashes
				a.checksumHashes = &h
			}
		}
		a.checksumMu.Unlock()
		a.invalidateUI()
	}()
}

func (a *App) renderTaskDetailsModal(c *ui.Context, pal Palette) {
	if a.detailsTaskID == "" || a.engine == nil {
		return
	}
	task, err := a.engine.GetTask(a.detailsTaskID)
	if err != nil || task == nil {
		a.closeTaskDetails()
		return
	}

	_, winH := c.Size()
	modalH := float32(560)
	if winH > 0 {
		if maxAvail := winH - 90; maxAvail < modalH {
			modalH = maxAvail
		}
		if modalH < 360 {
			modalH = 360
		}
	}

	name := task.Filename
	if name == "" {
		name = extractHost(task.URL)
	}

	open := true
	ui.Modal(c, &open, func() {
		ui.Column(c).Width(600).Height(modalH).Clip().Gap(12).Children(func() {
			// Fixed Header
			ui.Row(c).Shrink(0).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
				ui.Row(c).Gap(10).AlignItems(ui.Center).Grow(1).Children(func() {
					ui.Row(c).Width(32).Height(32).Radius(8).Background(pal.AccentSoft).Center().Shrink(0).Children(func() {
						ui.Icon(c, iconInfo).Width(16).Height(16).TextColor(pal.Accent)
					})
					ui.Column(c).Gap(1).Grow(1).Children(func() {
						ui.Text(c, i18n.T("details.title")).FontSize(16).FontWeight(700).TextColor(pal.TextPrimary).SingleLine()
						ui.Text(c, name).FontSize(12).TextColor(pal.TextMuted).SingleLine().Ellipsis("...")
					})
				})
				ui.Row(c).Gap(8).AlignItems(ui.Center).Shrink(0).Children(func() {
					statusBadge(c, pal, task.Status)
					if rowIconButton(c, pal, iconClose, false) {
						a.closeTaskDetails()
					}
				})
			})

			ui.Divider(c)

			// Scrollable Body
			ui.Scroll(c).Fill().Grow(1).Clip().Children(func() {
				ui.Column(c).Fill().Gap(14).Padding(2, 6, 4, 0).Children(func() {
					// 1. Overall Metrics Grid
					sizeText := FormatBytes(task.DownloadedBytes)
					if task.TotalBytes > 0 {
						sizeText = fmt.Sprintf("%s / %s", FormatBytes(task.DownloadedBytes), FormatBytes(task.TotalBytes))
					}
					conns := len(task.Segments)
					if conns == 0 {
						conns = 1
					}
					rangeLabel := i18n.T("details.ranges_no")
					if task.SupportsRanges {
						rangeLabel = i18n.T("details.ranges_yes")
					}

					ui.Row(c).Gap(8).Children(func() {
						metricCard(c, pal, i18n.T("details.downloaded"), sizeText)
						metricCard(c, pal, i18n.T("details.speed"), FormatSpeed(task.Speed))
						metricCard(c, pal, i18n.T("details.eta"), FormatETA(task.ETA))
						metricCard(c, pal, i18n.T("details.connections"), fmt.Sprintf("%d", conns))
					})

					// 2. Source URL & Save Path
					ui.Column(c).Padding(10, 12).Radius(8).Background(pal.InputBg).Border(1, pal.Border).Gap(8).Children(func() {
						ui.Row(c).Gap(8).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
							ui.Column(c).Gap(2).Grow(1).Children(func() {
								ui.Text(c, i18n.T("details.url_label")).FontSize(10).FontWeight(700).TextColor(pal.TextMuted)
								ui.Text(c, task.URL).FontSize(11).TextColor(pal.TextSecondary).SingleLine().Ellipsis("...")
							})
							ui.Row(c).Gap(6).AlignItems(ui.Center).Shrink(0).Children(func() {
								ui.Box(c).Padding(2, 7).Radius(999).Background(pal.BadgeBg).Children(func() {
									ui.Text(c, rangeLabel).FontSize(10).FontWeight(600).TextColor(pal.TextMuted).SingleLine()
								})
								if toolbarButton(c, pal, iconClipboard, i18n.T("details.copy"), false, false) {
									c.WriteClipboard(task.URL)
									a.statusMessage = i18n.T("msg.copied")
								}
							})
						})

						if task.SavePath != "" {
							ui.Row(c).Gap(8).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
								ui.Column(c).Gap(2).Grow(1).Children(func() {
									ui.Text(c, i18n.T("details.save_path_label")).FontSize(10).FontWeight(700).TextColor(pal.TextMuted)
									ui.Text(c, task.SavePath).FontSize(11).TextColor(pal.TextSecondary).SingleLine().Ellipsis("...")
								})
								ui.Row(c).Gap(6).AlignItems(ui.Center).Shrink(0).Children(func() {
									if task.Status == engine.StatusCompleted {
										if toolbarButton(c, pal, iconFile, i18n.T("details.open_file"), false, false) {
											if a.powerCtrl != nil {
												go func(p string) { _ = a.powerCtrl.OpenFile(p) }(task.SavePath)
											}
										}
									}
									if toolbarButton(c, pal, iconFolder, i18n.T("details.reveal"), false, false) {
										a.revealTaskInFileManager(task)
									}
									if toolbarButton(c, pal, iconClipboard, i18n.T("details.copy"), false, false) {
										c.WriteClipboard(task.SavePath)
										a.statusMessage = i18n.T("msg.copied")
									}
								})
							})
						}
					})

					// 3. Live Segment Visualizer (Track + Per-Segment Table)
					if len(task.Segments) > 0 {
						a.renderSegmentVisualizer(c, pal, task)
					}

					// 4. Per-Task Speed Limit Control
					ui.Column(c).Padding(10, 12).Radius(8).Background(pal.InputBg).Border(1, pal.Border).Gap(6).Children(func() {
						ui.Row(c).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
							ui.Text(c, i18n.T("details.speed_limit_label")).FontSize(12).FontWeight(600).TextColor(pal.TextSecondary)
							ui.Text(c, i18n.T("details.speed_limit_hint")).FontSize(10).TextColor(pal.TextMuted)
						})
						ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
							ui.TextInput(c, &a.detailsSpeedLimit).
								Background(pal.Card).
								Border(1, pal.Border).
								Radius(7).
								Grow(1)
							if toolbarButton(c, pal, iconCheck, i18n.T("details.apply_limit"), false, false) {
								a.applyTaskSpeedLimit()
							}
						})
						if a.detailsSpeedLimitErr != "" {
							ui.Text(c, a.detailsSpeedLimitErr).FontSize(11).TextColor(pal.Danger)
						}
					})

					// 5. SHA-256 / MD5 Checksum Verifier (when Completed)
					if task.Status == engine.StatusCompleted && task.SavePath != "" {
						a.renderChecksumSection(c, pal, task)
					}
				})
			})

			ui.Divider(c)

			// Fixed Footer
			ui.Row(c).Shrink(0).Gap(8).Justify(ui.End).Children(func() {
				if toolbarButton(c, pal, nil, i18n.T("details.close"), false, false) {
					a.closeTaskDetails()
				}
			})
		})
	})
	if !open {
		a.closeTaskDetails()
	}
}

func metricCard(c *ui.Context, pal Palette, label, value string) {
	ui.Column(c).Grow(1).Padding(8, 10).Radius(8).Background(pal.InputBg).Border(1, pal.Border).Gap(2).Children(func() {
		ui.Text(c, label).FontSize(10).FontWeight(700).TextColor(pal.TextMuted).SingleLine()
		ui.Text(c, value).FontSize(12).FontWeight(600).FontFeatures("tnum").TextColor(pal.TextPrimary).SingleLine().Ellipsis("...")
	})
}

func (a *App) renderSegmentVisualizer(c *ui.Context, pal Palette, task *engine.Task) {
	ui.Column(c).Padding(10, 12).Radius(8).Background(pal.InputBg).Border(1, pal.Border).Gap(8).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
			ui.Text(c, i18n.T("details.segments_title")).FontSize(12).FontWeight(600).TextColor(pal.TextPrimary)
			ui.Text(c, i18n.Tf("card.multi_segs", len(task.Segments))).FontSize(11).FontFeatures("tnum").TextColor(pal.TextMuted)
		})

		// Multi-segment horizontal colored track
		ui.Row(c).Height(12).Gap(2).Radius(6).Children(func() {
			for _, seg := range task.Segments {
				segLen := seg.End - seg.Start + 1
				pct := 0.0
				if segLen > 0 {
					pct = float64(seg.Downloaded) / float64(segLen)
					if pct > 1.0 {
						pct = 1.0
					}
					if pct < 0.0 {
						pct = 0.0
					}
				}
				barColor := pal.Accent
				switch seg.Status {
				case "done":
					barColor = pal.Success
				case "error":
					barColor = pal.Danger
				case "idle":
					barColor = pal.Warning
				}
				ui.Progress(c, pct).
					Height(12).
					Radius(3).
					Background(pal.TrackBg).
					TextColor(barColor).
					Grow(1)
			}
		})

		// Compact per-segment status table
		ui.Column(c).Gap(4).Children(func() {
			ui.Row(c).Padding(2, 4).Gap(8).Children(func() {
				ui.Text(c, i18n.T("details.seg_col_id")).Width(28).FontSize(10).FontWeight(700).TextColor(pal.TextMuted)
				ui.Text(c, i18n.T("details.seg_col_range")).Grow(1).FontSize(10).FontWeight(700).TextColor(pal.TextMuted)
				ui.Text(c, i18n.T("details.seg_col_progress")).Width(130).FontSize(10).FontWeight(700).TextColor(pal.TextMuted)
				ui.Text(c, i18n.T("details.seg_col_status")).Width(80).FontSize(10).FontWeight(700).TextColor(pal.TextMuted)
			})
			for _, seg := range task.Segments {
				segLen := seg.End - seg.Start + 1
				statusColor := pal.Accent
				switch seg.Status {
				case "done":
					statusColor = pal.Success
				case "error":
					statusColor = pal.Danger
				case "idle":
					statusColor = pal.Warning
				}
				ui.Row(c).Padding(3, 4).Radius(5).Background(pal.Card).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, fmt.Sprintf("#%d", seg.Index+1)).Width(28).FontSize(11).FontWeight(600).FontFeatures("tnum").TextColor(pal.TextSecondary)
					ui.Text(c, fmt.Sprintf("%s – %s", FormatBytes(seg.Start), FormatBytes(seg.End))).Grow(1).FontSize(11).FontFeatures("tnum").TextColor(pal.TextSecondary).SingleLine()
					ui.Text(c, fmt.Sprintf("%s / %s", FormatBytes(seg.Downloaded), FormatBytes(segLen))).Width(130).FontSize(11).FontFeatures("tnum").TextColor(pal.TextPrimary).SingleLine()
					ui.Text(c, seg.Status).Width(80).FontSize(11).FontWeight(600).TextColor(statusColor).SingleLine()
				})
			}
		})
	})
}

func (a *App) renderChecksumSection(c *ui.Context, pal Palette, task *engine.Task) {
	a.checksumMu.Lock()
	running := a.checksumRunning && a.checksumTaskID == task.ID
	var hashes *engine.FileHashes
	if a.checksumHashes != nil && a.checksumTaskID == task.ID {
		cp := *a.checksumHashes
		hashes = &cp
	}
	errMsg := ""
	if a.checksumTaskID == task.ID {
		errMsg = a.checksumErr
	}
	a.checksumMu.Unlock()

	ui.Column(c).Padding(10, 12).Radius(8).Background(pal.InputBg).Border(1, pal.Border).Gap(8).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, iconShield).Width(14).Height(14).TextColor(pal.Accent)
				ui.Text(c, i18n.T("details.checksum_title")).FontSize(12).FontWeight(600).TextColor(pal.TextPrimary)
			})
			btnLabel := i18n.T("details.checksum_compute")
			if running {
				btnLabel = i18n.T("details.checksum_computing")
			}
			if toolbarButton(c, pal, iconShield, btnLabel, running, false) {
				a.startChecksumComputation(task.ID, task.SavePath)
			}
		})

		if errMsg != "" {
			ui.Text(c, errMsg).FontSize(11).TextColor(pal.Danger)
		}

		if hashes != nil {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
				ui.Column(c).Gap(1).Grow(1).Children(func() {
					ui.Text(c, "SHA-256").FontSize(10).FontWeight(700).TextColor(pal.TextMuted)
					ui.Text(c, hashes.SHA256).FontSize(11).FontFeatures("tnum").TextColor(pal.TextPrimary).SingleLine().Ellipsis("...")
				})
				if toolbarButton(c, pal, iconClipboard, i18n.T("details.copy"), false, false) {
					c.WriteClipboard(hashes.SHA256)
					a.statusMessage = i18n.T("msg.copied")
				}
			})

			ui.Row(c).Gap(8).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
				ui.Column(c).Gap(1).Grow(1).Children(func() {
					ui.Text(c, "MD5").FontSize(10).FontWeight(700).TextColor(pal.TextMuted)
					ui.Text(c, hashes.MD5).FontSize(11).FontFeatures("tnum").TextColor(pal.TextSecondary).SingleLine().Ellipsis("...")
				})
				if toolbarButton(c, pal, iconClipboard, i18n.T("details.copy"), false, false) {
					c.WriteClipboard(hashes.MD5)
					a.statusMessage = i18n.T("msg.copied")
				}
			})

			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.TextInput(c, &a.detailsExpectedHash).
					Placeholder(i18n.T("details.checksum_expected")).
					Background(pal.Card).
					Border(1, pal.Border).
					Radius(7).
					Grow(1)
				if strings.TrimSpace(a.detailsExpectedHash) != "" {
					matched, algo := engine.CompareFileHash(*hashes, a.detailsExpectedHash)
					if matched {
						ui.Box(c).Padding(4, 10).Radius(999).Background(pal.SuccessSoft).Shrink(0).Children(func() {
							ui.Text(c, i18n.Tf("details.checksum_match", algo)).FontSize(11).FontWeight(600).TextColor(pal.Success).SingleLine()
						})
					} else {
						ui.Box(c).Padding(4, 10).Radius(999).Background(pal.DangerSoft).Shrink(0).Children(func() {
							ui.Text(c, i18n.T("details.checksum_mismatch")).FontSize(11).FontWeight(600).TextColor(pal.Danger).SingleLine()
						})
					}
				}
			})
		}
	})
}

// --- Post-Download Actions & 30s Cancelable Countdown ---

// SetPostDownloadAction sets the action to execute after all downloads finish.
func (a *App) SetPostDownloadAction(action power.Action) {
	act := power.NormalizeAction(string(action))
	a.powerMu.Lock()
	a.postAction = act
	a.powerMu.Unlock()
	if act == power.ActionNone && a.powerCountdown != nil {
		a.powerCountdown.Cancel()
	}
}

// PostDownloadAction returns the currently selected post-download action.
func (a *App) PostDownloadAction() power.Action {
	a.powerMu.RLock()
	defer a.powerMu.RUnlock()
	return a.postAction
}

// HandleTaskCompleted should be invoked whenever a download task completes.
// It starts the 30-second cancelable power countdown ONLY when both activeCount
// and queuedCount are 0 (checked atomically via engine.IsIdle()).
func (a *App) HandleTaskCompleted(_ *engine.Task) {
	a.sessionCompletedAny.Store(true)
	act := a.PostDownloadAction()
	if act == power.ActionNone || a.engine == nil || a.powerCountdown == nil {
		return
	}
	if !a.engine.IsIdle() {
		return
	}
	dur := a.powerCountdownDuration
	if dur <= 0 {
		dur = power.DefaultCountdownDuration
	}
	a.powerCountdown.Start(act, dur)
	a.BringToForeground()
}

// HandleScheduleStop is invoked by the engine Scheduler when a configured time
// window exits with a Sleep or Shutdown stop action.
func (a *App) HandleScheduleStop(stopAction string) {
	act := power.NormalizeAction(stopAction)
	if act == power.ActionNone || a.powerCountdown == nil {
		return
	}
	dur := a.powerCountdownDuration
	if dur <= 0 {
		dur = power.DefaultCountdownDuration
	}
	a.powerCountdown.Start(act, dur)
	a.BringToForeground()
}

func (a *App) cancelPowerCountdown() {
	if a.powerCountdown != nil {
		a.powerCountdown.Cancel()
	}
}

func (a *App) renderPowerCountdownModal(c *ui.Context, pal Palette) {
	if a.powerCountdown == nil {
		return
	}
	active, act, secs := a.powerCountdown.State()
	if !active {
		return
	}
	title := i18n.Tf("power.countdown_title_sleep", secs)
	if act == power.ActionShutdown {
		title = i18n.Tf("power.countdown_title_shutdown", secs)
	}

	open := true
	ui.Modal(c, &open, func() {
		ui.Column(c).Width(440).Gap(16).Children(func() {
			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Row(c).Width(34).Height(34).Radius(8).Background(pal.WarningSoft).Center().Shrink(0).Children(func() {
					ui.Icon(c, iconPower).Width(17).Height(17).TextColor(pal.Warning)
				})
				ui.Text(c, title).FontSize(16).FontWeight(700).FontFeatures("tnum").TextColor(pal.TextPrimary)
			})

			ui.Text(c, i18n.T("power.countdown_body")).FontSize(12).TextColor(pal.TextSecondary)

			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				if toolbarButton(c, pal, iconClose, i18n.T("power.cancel"), false, false) {
					a.cancelPowerCountdown()
					a.statusMessage = i18n.T("msg.power_cancelled")
				}
				if toolbarButton(c, pal, iconPower, i18n.T("power.run_now"), false, true) {
					a.cancelPowerCountdown()
					if a.powerCtrl != nil {
						go func(target power.Action) {
							_ = a.powerCtrl.Execute(target)
						}(act)
					}
				}
			})
		})
	})
	if !open {
		a.cancelPowerCountdown()
		a.statusMessage = i18n.T("msg.power_cancelled")
	}
}
