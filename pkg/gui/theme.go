package gui

import (
	"runtime"

	"github.com/egoist/mygo/ui"
)

// defaultUIFont returns a system font family with full Vietnamese Unicode
// diacritic coverage (ă, â, đ, ê, ô, ơ, ư and all tone marks) to prevent
// missing-glyph boxes on Windows.
func defaultUIFont() string {
	if runtime.GOOS == "windows" {
		return "Segoe UI"
	}
	return ""
}

// Palette holds the semantic design tokens for MagicDown's native UI.
type Palette struct {
	Dark bool

	Canvas        ui.Color
	Sidebar       ui.Color
	Header        ui.Color
	Card          ui.Color
	CardHover     ui.Color
	CardSelected  ui.Color
	Modal         ui.Color
	InputBg       ui.Color
	Border        ui.Color
	BorderStrong  ui.Color
	NavHover      ui.Color
	NavActive     ui.Color
	BadgeBg       ui.Color

	TextPrimary   ui.Color
	TextSecondary ui.Color
	TextMuted     ui.Color

	Accent          ui.Color
	AccentHover     ui.Color
	AccentFill      ui.Color
	AccentFillHover ui.Color
	AccentSoft      ui.Color
	AccentText      ui.Color

	Success       ui.Color
	SuccessSoft   ui.Color
	Warning       ui.Color
	WarningSoft   ui.Color
	Danger        ui.Color
	DangerSoft    ui.Color
	Purple        ui.Color
	PurpleSoft    ui.Color
	TrackBg       ui.Color

	UITheme       *ui.Theme
}

// DarkPalette returns the default sleek dark mode design system (Linear/Raycast inspired).
func DarkPalette() Palette {
	t := ui.DarkTheme()
	t.Background = ui.Hex("#0B0D12")
	t.Surface = ui.Hex("#161B26")
	t.SurfaceHover = ui.Hex("#1E2534")
	t.SurfacePressed = ui.Hex("#252E40")
	t.Border = ui.Hex("#252C3D")
	t.Text = ui.Hex("#F1F5F9")
	t.TextMuted = ui.Hex("#8494AB")
	t.Accent = ui.Hex("#3B82F6")
	t.AccentHover = ui.Hex("#2563EB")
	t.AccentPressed = ui.Hex("#1D4ED8")
	t.AccentText = ui.Hex("#FFFFFF")
	t.Danger = ui.Hex("#EF4444")
	t.Warning = ui.Hex("#F59E0B")
	t.Success = ui.Hex("#10B981")
	t.Radius = 8
	t.Spacing = 4
	t.FontSize = 13
	t.Font = defaultUIFont()

	return Palette{
		Dark:          true,
		Canvas:        ui.Hex("#0B0D12"),
		Sidebar:       ui.Hex("#10131B"),
		Header:        ui.Hex("#0E1118"),
		Card:          ui.Hex("#141923"),
		CardHover:     ui.Hex("#19202D"),
		CardSelected:  ui.Hex("#162136"),
		Modal:         ui.Hex("#141923"),
		InputBg:       ui.Hex("#0D1017"),
		Border:        ui.Hex("#212838"),
		BorderStrong:  ui.Hex("#2F3950"),
		NavHover:      ui.RGBA(255, 255, 255, 0.05),
		NavActive:     ui.RGBA(59, 130, 246, 0.16),
		BadgeBg:       ui.RGBA(255, 255, 255, 0.07),
		TextPrimary:   ui.Hex("#F1F5F9"),
		TextSecondary: ui.Hex("#94A3B8"),
		TextMuted:     ui.Hex("#8494AB"),
		Accent:          ui.Hex("#3B82F6"),
		AccentHover:     ui.Hex("#60A5FA"),
		AccentFill:      ui.Hex("#2563EB"),
		AccentFillHover: ui.Hex("#1D4ED8"),
		AccentSoft:      ui.RGBA(59, 130, 246, 0.15),
		AccentText:      ui.Hex("#FFFFFF"),
		Success:       ui.Hex("#10B981"),
		SuccessSoft:   ui.RGBA(16, 185, 129, 0.15),
		Warning:       ui.Hex("#F59E0B"),
		WarningSoft:   ui.RGBA(245, 158, 11, 0.15),
		Danger:        ui.Hex("#EF4444"),
		DangerSoft:    ui.RGBA(239, 68, 68, 0.15),
		Purple:        ui.Hex("#A855F7"),
		PurpleSoft:    ui.RGBA(168, 85, 247, 0.15),
		TrackBg:       ui.Hex("#202736"),
		UITheme:       t,
	}
}

// LightPalette returns a crisp modern light theme.
func LightPalette() Palette {
	t := ui.LightTheme()
	t.Background = ui.Hex("#F8FAFC")
	t.Surface = ui.Hex("#FFFFFF")
	t.SurfaceHover = ui.Hex("#F1F5F9")
	t.SurfacePressed = ui.Hex("#E2E8F0")
	t.Border = ui.Hex("#E2E8F0")
	t.Text = ui.Hex("#0F172A")
	t.TextMuted = ui.Hex("#526075")
	t.Accent = ui.Hex("#2563EB")
	t.AccentHover = ui.Hex("#1D4ED8")
	t.AccentPressed = ui.Hex("#1E40AF")
	t.AccentText = ui.Hex("#FFFFFF")
	t.Danger = ui.Hex("#DC2626")
	t.Warning = ui.Hex("#D97706")
	t.Success = ui.Hex("#059669")
	t.Radius = 8
	t.Spacing = 4
	t.FontSize = 13
	t.Font = defaultUIFont()

	return Palette{
		Dark:          false,
		Canvas:        ui.Hex("#F8FAFC"),
		Sidebar:       ui.Hex("#F1F5F9"),
		Header:        ui.Hex("#FFFFFF"),
		Card:          ui.Hex("#FFFFFF"),
		CardHover:     ui.Hex("#F8FAFC"),
		CardSelected:  ui.Hex("#EFF6FF"),
		Modal:         ui.Hex("#FFFFFF"),
		InputBg:       ui.Hex("#F8FAFC"),
		Border:        ui.Hex("#E2E8F0"),
		BorderStrong:  ui.Hex("#CBD5E1"),
		NavHover:      ui.RGBA(15, 23, 42, 0.05),
		NavActive:     ui.RGBA(37, 99, 235, 0.12),
		BadgeBg:       ui.RGBA(15, 23, 42, 0.06),
		TextPrimary:   ui.Hex("#0F172A"),
		TextSecondary: ui.Hex("#475569"),
		TextMuted:     ui.Hex("#526075"),
		Accent:          ui.Hex("#2563EB"),
		AccentHover:     ui.Hex("#1D4ED8"),
		AccentFill:      ui.Hex("#2563EB"),
		AccentFillHover: ui.Hex("#1D4ED8"),
		AccentSoft:      ui.RGBA(37, 99, 235, 0.12),
		AccentText:      ui.Hex("#FFFFFF"),
		Success:       ui.Hex("#059669"),
		SuccessSoft:   ui.RGBA(5, 150, 105, 0.12),
		Warning:       ui.Hex("#D97706"),
		WarningSoft:   ui.RGBA(217, 119, 6, 0.12),
		Danger:        ui.Hex("#DC2626"),
		DangerSoft:    ui.RGBA(220, 38, 38, 0.12),
		Purple:        ui.Hex("#7C3AED"),
		PurpleSoft:    ui.RGBA(124, 58, 237, 0.12),
		TrackBg:       ui.Hex("#E2E8F0"),
		UITheme:       t,
	}
}
