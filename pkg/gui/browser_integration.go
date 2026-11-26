package gui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"

	"magicdown/pkg/i18n"
	"magicdown/pkg/nativemessaging"
)

func (a *App) refreshBrowsers() {
	if a.detectBrowsersFn != nil {
		a.detectedBrowsers = a.detectBrowsersFn()
	}
}

func (a *App) browserIntegrationStatuses() []nativemessaging.BrowserIntegrationStatus {
	cfg, _ := nativemessaging.LoadExtensionIDConfig("")
	merged := make(map[string]nativemessaging.ExtensionConnectionState, len(a.browserConnStates))
	for k, v := range a.browserConnStates {
		merged[k] = v
	}
	if a.ipcServer != nil {
		for browserID, rec := range a.ipcServer.GetHandshakeSnapshots() {
			cur := merged[browserID]
			cur.Connected = rec.Connected
			cur.ExtensionVersion = rec.ExtensionVersion
			cur.LastHandshakeAt = rec.LastSeenAt
			if rec.Connected {
				cur.Waiting = false
				cur.LastError = ""
			}
			merged[browserID] = cur
		}
	}
	return nativemessaging.BuildBrowserIntegrationStatuses(a.detectedBrowsers, cfg, merged)
}

func (a *App) resolvedExtensionDir() string {
	if strings.TrimSpace(a.extensionDir) != "" {
		return a.extensionDir
	}
	if resolved := nativemessaging.ResolveExtensionDir(); resolved != "" {
		a.extensionDir = resolved
		return resolved
	}
	if localAppData := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); localAppData != "" {
		return filepath.Join(localAppData, "Programs", "MagicDown", "extension")
	}
	return `%LOCALAPPDATA%\Programs\MagicDown\extension`
}

func (a *App) installExtensionForBrowser(c *ui.Context, st nativemessaging.BrowserIntegrationStatus) {
	// 1. Ensure Native Messaging Host is registered for this browser first.
	if !st.HostValid && a.integrateBrowsersFn != nil {
		results := a.integrateBrowsersFn([]nativemessaging.BrowserInfo{st.Browser}, "", "", false)
		for _, r := range results {
			a.browserResults[r.BrowserID] = r
			if r.Err != nil {
				a.browserConnStates[st.Browser.ID] = nativemessaging.ExtensionConnectionState{
					LastError: r.Err.Error(),
				}
				a.nativeRegMsg = i18n.Tf("settings.native_err", r.Err.Error())
				a.refreshBrowsers()
				return
			}
		}
		a.refreshBrowsers()
	}

	// 2. Copy the unpacked extension folder path to the clipboard.
	extDir := a.resolvedExtensionDir()
	if c != nil && extDir != "" {
		c.WriteClipboard(extDir)
	}

	// 3. Open the browser's extensions page (chrome://extensions / edge://extensions).
	if a.openExtensionsPageFn != nil {
		if err := a.openExtensionsPageFn(st.Browser); err != nil {
			a.browserConnStates[st.Browser.ID] = nativemessaging.ExtensionConnectionState{
				LastError: err.Error(),
			}
			a.nativeRegMsg = i18n.Tf("settings.native_err", err.Error())
			return
		}
	}

	// 4. Show concise 3-step Load unpacked guide and wait for real HELLO/WELCOME handshake.
	a.installGuideBrowserID = st.Browser.ID
	a.browserConnStates[st.Browser.ID] = nativemessaging.ExtensionConnectionState{
		Waiting: true,
	}
	a.nativeRegMsg = i18n.Tf("browser.unpacked_opened", st.Browser.Name)
}

func (a *App) reconnectBrowser(st nativemessaging.BrowserIntegrationStatus) {
	if a.integrateBrowsersFn != nil {
		results := a.integrateBrowsersFn([]nativemessaging.BrowserInfo{st.Browser}, "", "", false)
		for _, r := range results {
			a.browserResults[r.BrowserID] = r
			if r.Err != nil {
				a.browserConnStates[st.Browser.ID] = nativemessaging.ExtensionConnectionState{
					LastError: r.Err.Error(),
				}
				a.nativeRegMsg = i18n.Tf("settings.native_err", r.Err.Error())
				a.refreshBrowsers()
				return
			}
		}
	}
	a.refreshBrowsers()
	if a.nativeStatusFn != nil {
		a.nativeStatus = a.nativeStatusFn()
	}
	cur := a.browserConnStates[st.Browser.ID]
	if !cur.Connected {
		cur.LastError = ""
		cur.Waiting = true
		a.browserConnStates[st.Browser.ID] = cur
	}
	a.nativeRegMsg = i18n.Tf("browser.reconnected", st.Browser.Name)
}

func integrationStateLabelAndColor(state nativemessaging.IntegrationState, pal Palette) (string, ui.Color) {
	switch state {
	case nativemessaging.StateConnected:
		return i18n.T("browser.state_connected"), pal.Success
	case nativemessaging.StateWaitingForConnection:
		return i18n.T("browser.state_waiting"), pal.Accent
	case nativemessaging.StateInstallationRequired:
		return i18n.T("browser.state_install_required"), pal.Warning
	case nativemessaging.StateHostNotRegistered:
		return i18n.T("browser.state_host_missing"), pal.Warning
	case nativemessaging.StateConnectionError:
		return i18n.T("browser.state_error"), pal.Danger
	case nativemessaging.StateUnsupportedBrowser:
		return i18n.T("browser.state_unsupported"), pal.TextMuted
	default:
		return i18n.T("browser.state_not_installed"), pal.TextMuted
	}
}

func (a *App) renderBrowserIntegrationSection(c *ui.Context, pal Palette) {
	statuses := a.browserIntegrationStatuses()
	ui.Column(c).Gap(8).Padding(10, 12).Radius(8).Background(pal.InputBg).Border(1, pal.Border).Children(func() {
		ui.Column(c).Gap(2).Children(func() {
			ui.Text(c, i18n.T("browser.section_title")).FontSize(12).FontWeight(600).TextColor(pal.TextPrimary)
			ui.Text(c, i18n.T("browser.section_sub")).FontSize(11).TextColor(pal.TextMuted)
		})

		installedCount := 0
		for _, st := range statuses {
			if !st.Browser.Installed {
				continue
			}
			installedCount++
			curSt := st
			label, badgeColor := integrationStateLabelAndColor(curSt.State, pal)

			title := curSt.Browser.Name
			if curSt.Browser.Version != "" {
				title = fmt.Sprintf("%s %s", curSt.Browser.Name, curSt.Browser.Version)
			}
			hostLabel := i18n.T("browser.host_missing")
			if curSt.HostValid {
				hostLabel = i18n.T("browser.host_ok")
			}

			ui.Column(c).Gap(8).Padding(10, 12).Radius(7).Background(pal.Canvas).Border(1, pal.Border).Children(func() {
				ui.Row(c).AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(8).Children(func() {
					ui.Column(c).Gap(2).Grow(1).Children(func() {
						ui.Text(c, title).FontSize(12).FontWeight(600).TextColor(pal.TextPrimary).SingleLine().Ellipsis("...")
						ui.Text(c, hostLabel).FontSize(11).TextColor(pal.TextMuted).SingleLine()
					})
					ui.Box(c).Padding(2, 8).Radius(999).Background(pal.BadgeBg).Shrink(0).Children(func() {
						ui.Text(c, label).FontSize(11).FontWeight(600).TextColor(badgeColor).SingleLine()
					})
				})

				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					if curSt.Browser.Family == nativemessaging.BrowserFamilyChromium {
						if toolbarButton(c, pal, nil, i18n.T("browser.btn_install"), false, false) {
							a.installExtensionForBrowser(c, curSt)
						}
						if toolbarButton(c, pal, nil, i18n.T("browser.btn_reconnect"), false, false) {
							a.reconnectBrowser(curSt)
						}
					}
					if toolbarButton(c, pal, nil, i18n.T("browser.btn_troubleshoot"), false, a.troubleshootBrowserID == curSt.Browser.ID) {
						if a.troubleshootBrowserID == curSt.Browser.ID {
							a.troubleshootBrowserID = ""
						} else {
							a.troubleshootBrowserID = curSt.Browser.ID
						}
					}
				})

				if a.installGuideBrowserID == curSt.Browser.ID && curSt.State != nativemessaging.StateConnected {
					extDir := a.resolvedExtensionDir()
					ui.Column(c).Gap(4).Padding(8, 10).Radius(6).Background(pal.InputBg).Border(1, pal.Accent).Children(func() {
						ui.Row(c).AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(8).Children(func() {
							ui.Text(c, i18n.Tf("browser.guide_title", curSt.Browser.Name)).FontSize(11).FontWeight(600).TextColor(pal.TextPrimary)
							if toolbarButton(c, pal, nil, i18n.T("onboarding.copy_path"), false, false) {
								c.WriteClipboard(extDir)
								a.nativeRegMsg = i18n.T("onboarding.copied_path")
							}
						})
						ui.Text(c, i18n.T("browser.guide_step1")).FontSize(10).TextColor(pal.TextSecondary)
						ui.Text(c, i18n.T("browser.guide_step2")).FontSize(10).TextColor(pal.TextSecondary)
						ui.Text(c, i18n.Tf("browser.guide_step3", extDir)).FontSize(10).TextColor(pal.TextSecondary)
					})
				}

				if a.troubleshootBrowserID == curSt.Browser.ID {
					ui.Column(c).Gap(3).Padding(8, 10).Radius(6).Background(pal.InputBg).Border(1, pal.Border).Children(func() {
						ui.Text(c, i18n.Tf("browser.diag_title", curSt.Browser.Name)).FontSize(11).FontWeight(600).TextColor(pal.TextPrimary)
						ui.Text(c, i18n.Tf("browser.diag_exe", curSt.Browser.ExecutablePath)).FontSize(10).TextColor(pal.TextMuted)
						ui.Text(c, i18n.Tf("browser.diag_manifest", curSt.ManifestPath)).FontSize(10).TextColor(pal.TextMuted)
						ui.Text(c, i18n.Tf("browser.diag_log", curSt.LogPath)).FontSize(10).TextColor(pal.TextMuted)
						if curSt.Connection.LastError != "" {
							ui.Text(c, curSt.Connection.LastError).FontSize(10).TextColor(pal.Danger)
						}
						ui.Text(c, i18n.T("browser.diag_hint")).FontSize(10).TextColor(pal.TextSecondary)
					})
				}
			})
		}

		if installedCount == 0 {
			ui.Text(c, i18n.T("onboarding.no_browser")).FontSize(11).TextColor(pal.TextMuted)
		}
	})
}
