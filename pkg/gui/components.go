package gui

import (
	"fmt"
	"net/url"

	"github.com/egoist/mygo/ui"

	"magicdown/pkg/engine"
	"magicdown/pkg/i18n"
)

// FileCategory classifies downloads for sidebar filtering and icon badges.
type FileCategory string

const (
	CatAll        FileCategory = "All"
	CatVideo      FileCategory = "Video"
	CatMusic      FileCategory = "Music"
	CatCompressed FileCategory = "Compressed"
	CatDocuments  FileCategory = "Documents"
	CatPrograms   FileCategory = "Programs"
	CatOther      FileCategory = "Other"
)

// ClassifyFile determines a download's category from its filename or URL.
func ClassifyFile(filename, rawURL string) FileCategory {
	return FileCategory(engine.CategoryForFilename(filename, rawURL))
}

func categoryVisual(cat FileCategory, pal Palette) (*ui.SVG, ui.Color, ui.Color) {
	switch cat {
	case CatVideo:
		return iconMedia, pal.Purple, pal.PurpleSoft
	case CatMusic:
		return iconMedia, pal.Accent, pal.AccentSoft
	case CatCompressed:
		return iconArchive, pal.Warning, pal.WarningSoft
	case CatDocuments:
		return iconDoc, pal.Accent, pal.AccentSoft
	case CatPrograms:
		return iconProgram, pal.Success, pal.SuccessSoft
	default:
		return iconFile, pal.TextSecondary, pal.BadgeBg
	}
}

func extractHost(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return rawURL
	}
	return u.Host
}

// navPill renders a sleek sidebar navigation item with icon, label, and live count badge.
func navPill(c *ui.Context, pal Palette, icon *ui.SVG, label string, count int, active bool) bool {
	row := ui.Row(c).
		Padding(7, 10).
		Gap(9).
		Radius(7).
		AlignItems(ui.Center).
		Cursor(ui.CursorPointer).
		Focusable()

	bg := ui.Transparent
	fg := pal.TextSecondary
	iconColor := pal.TextMuted
	badgeBg := pal.BadgeBg
	badgeFg := pal.TextMuted

	if active {
		bg = pal.NavActive
		fg = pal.TextPrimary
		iconColor = pal.Accent
		badgeBg = pal.AccentSoft
		badgeFg = pal.Accent
	} else if row.Hovered() {
		bg = pal.NavHover
		fg = pal.TextPrimary
		iconColor = pal.TextSecondary
	}

	row.Background(bg).Children(func() {
		ui.Icon(c, icon).Width(15).Height(15).TextColor(iconColor).Shrink(0)
		t := ui.Text(c, label).FontSize(13).TextColor(fg).Grow(1).SingleLine().Ellipsis("...")
		if active {
			t.FontWeight(600)
		} else {
			t.FontWeight(500)
		}
		if count >= 0 {
			ui.Box(c).Padding(1, 6).Radius(999).Background(badgeBg).Shrink(0).Children(func() {
				ui.Text(c, fmt.Sprintf("%d", count)).
					FontSize(11).
					FontWeight(600).
					FontFeatures("tnum").
					TextColor(badgeFg).
					SingleLine()
			})
		}
	})

	return row.Clicked()
}

// toolbarButton renders a modern ghost/secondary action button with optional icon and disabled state.
func toolbarButton(c *ui.Context, pal Palette, icon *ui.SVG, label string, disabled bool, danger bool) bool {
	btn := ui.Row(c).
		Padding(6, 10).
		Gap(6).
		Radius(7).
		AlignItems(ui.Center).
		Border(1, pal.Border).
		Disabled(disabled)

	bg := pal.Card
	fg := pal.TextPrimary
	iconColor := pal.TextSecondary
	if disabled {
		bg = ui.Transparent
		fg = pal.TextMuted
		iconColor = pal.TextMuted
	} else {
		btn.Cursor(ui.CursorPointer).Focusable()
		if btn.Hovered() {
			if danger {
				bg = pal.DangerSoft
				fg = pal.Danger
				iconColor = pal.Danger
				btn.Border(1, pal.Danger)
			} else {
				bg = pal.CardHover
				btn.Border(1, pal.BorderStrong)
			}
		}
	}

	btn.Background(bg).Children(func() {
		if icon != nil {
			ui.Icon(c, icon).Width(14).Height(14).TextColor(iconColor).Shrink(0)
		}
		ui.Text(c, label).FontSize(12).FontWeight(500).TextColor(fg).SingleLine()
	})

	return !disabled && btn.Clicked()
}

// primaryCTA renders the accent primary action button with icon.
func primaryCTA(c *ui.Context, pal Palette, icon *ui.SVG, label string) bool {
	btn := ui.Row(c).
		Padding(7, 13).
		Gap(6).
		Radius(7).
		AlignItems(ui.Center).
		Cursor(ui.CursorPointer).
		Focusable()

	bg := pal.Accent
	if btn.Hovered() {
		bg = pal.AccentHover
	}
	btn.Background(bg).Children(func() {
		if icon != nil {
			ui.Icon(c, icon).Width(14).Height(14).TextColor(pal.AccentText).Shrink(0)
		}
		ui.Text(c, label).FontSize(12).FontWeight(600).TextColor(pal.AccentText).SingleLine()
	})

	return btn.Clicked()
}

// rowIconButton renders a compact 28x28 rounded inline icon action button on download cards.
func rowIconButton(c *ui.Context, pal Palette, icon *ui.SVG, danger bool) bool {
	btn := ui.Row(c).
		Width(28).
		Height(28).
		Radius(6).
		Center().
		Cursor(ui.CursorPointer).
		Focusable().
		Shrink(0)

	bg := ui.Transparent
	fg := pal.TextSecondary
	if btn.Hovered() {
		if danger {
			bg = pal.DangerSoft
			fg = pal.Danger
		} else {
			bg = pal.NavHover
			fg = pal.TextPrimary
		}
	}
	btn.Background(bg).Children(func() {
		ui.Icon(c, icon).Width(14).Height(14).TextColor(fg)
	})
	return btn.Clicked()
}

// statusBadge renders a compact semantic status pill with a leading colored dot.
func statusBadge(c *ui.Context, pal Palette, st engine.TaskStatus) {
	var fg, bg ui.Color
	switch st {
	case engine.StatusDownloading, engine.StatusProbing:
		fg, bg = pal.Accent, pal.AccentSoft
	case engine.StatusCompleted:
		fg, bg = pal.Success, pal.SuccessSoft
	case engine.StatusPaused, engine.StatusQueued:
		fg, bg = pal.Warning, pal.WarningSoft
	case engine.StatusFailed, engine.StatusCancelled:
		fg, bg = pal.Danger, pal.DangerSoft
	default:
		fg, bg = pal.TextSecondary, pal.BadgeBg
	}

	ui.Row(c).Padding(3, 8).Gap(5).Radius(999).Background(bg).AlignItems(ui.Center).Shrink(0).Children(func() {
		ui.Box(c).Width(6).Height(6).Radius(999).Background(fg).Shrink(0)
		ui.Text(c, i18n.T("status."+string(st))).FontSize(11).FontWeight(600).TextColor(fg).SingleLine()
	})
}

// segmentOption renders a pill option for segmented selectors (e.g., 4 / 8 / 16 segments).
func segmentOption(c *ui.Context, pal Palette, label string, active bool) bool {
	btn := ui.Row(c).
		Padding(6, 14).
		Radius(6).
		Center().
		Cursor(ui.CursorPointer).
		Focusable()

	bg := pal.InputBg
	fg := pal.TextSecondary
	border := pal.Border
	if active {
		bg = pal.AccentSoft
		fg = pal.Accent
		border = pal.Accent
	} else if btn.Hovered() {
		bg = pal.CardHover
		fg = pal.TextPrimary
	}

	btn.Background(bg).Border(1, border).Children(func() {
		t := ui.Text(c, label).FontSize(12).TextColor(fg).SingleLine()
		if active {
			t.FontWeight(600)
		} else {
			t.FontWeight(500)
		}
	})
	return btn.Clicked()
}
