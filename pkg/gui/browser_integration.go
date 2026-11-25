package gui

import (
	"fmt"
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
	return nativemessaging.BuildBrowserIntegrationStatuses(a.detectedBrowsers, cfg, a.browserConnStates)
}

func (a *App) installExtensionForBrowser(st nativemessaging.BrowserIntegrationStatus) {
	// Ensure Native Messaging Host is registered for this browser first (never open --load-extension).
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

	if strings.TrimSpace(st.StoreURL) == "" {
		a.browserConnStates[st.Browser.ID] = nativemessaging.ExtensionConnectionState{
			Waiting: true,
		}
		a.nativeRegMsg = i18n.Tf("browser.store_url_missing", st.Browser.Name)
		return
	}

	if a.openStoreURLFn != nil {
		if err := a.openStoreURLFn(st.Browser, st.StoreURL); err != nil {
			a.browserConnStates[st.Browser.ID] = nativemessaging.ExtensionConnectionState{
				LastError: err.Error(),
			}
			a.nativeRegMsg = i18n.Tf("settings.native_err", err.Error())
			return
		}
	}

	a.browserConnStates[st.Browser.ID] = nativemessaging.ExtensionConnectionState{
		Waiting: true,
	}
	a.nativeRegMsg = i18n.Tf("browser.store_opened", st.Browser.Name)
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

			ui.Column(c).Gap(6).Padding(8, 10).Radius(6).Background(pal.Canvas).Border(1, pal.Border).Children(func() {
				ui.Row(c).AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(8).Children(func() {
					ui.Column(c).Gap(2).Children(func() {
						ui.Text(c, title).FontSize(12).FontWeight(600).TextColor(pal.TextPrimary)
						ui.Text(c, fmt.Sprintf("%s  •  %s", hostLabel, label)).FontSize(11).TextColor(badgeColor)
					})
					ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
						if curSt.Browser.Family == nativemessaging.BrowserFamilyChromium {
							if toolbarButton(c, pal, nil, i18n.T("browser.btn_install"), false, false) {
								a.installExtensionForBrowser(curSt)
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
				})

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
