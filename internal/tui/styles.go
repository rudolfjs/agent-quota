// Package tui provides the Bubbletea v2 TUI for displaying provider quota data.
package tui

import (
	"os"
	"strings"

	"charm.land/lipgloss/v2"
)

const (
	claudeColorHex    = "#DE7356"
	geminiColorHex    = "#8B5CF6"
	openAIColorHex    = "#FFFFFF"
	openAIBarColorHex = "#9CA3AF"
	successColorHex   = "#22C55E"
	warningColorHex   = "#F59E0B"
	dangerColorHex    = "#EF4444"
)

type appPalette struct {
	IsDark bool

	TitleHex         string
	SubtitleHex      string
	WindowHex        string
	MutedHex         string
	ErrorHex         string
	ScrollTrackHex   string
	ScrollThumbHex   string
	LogoBorderHex    string
	LogoTextHex      string
	CompactLogoBGHex string

	StatusOKBadgeBGHex   string
	StatusOKBadgeFGHex   string
	StatusWarnBadgeBGHex string
	StatusWarnBadgeFGHex string
	StatusErrBadgeBGHex  string
	StatusErrBadgeFGHex  string
}

type providerTheme struct {
	BorderHex  string
	TitleHex   string
	BadgeBGHex string
	BadgeFGHex string
	BarHex     string
	TrackHex   string
	ChipBGHex  string
	ChipFGHex  string
}

func detectDarkBackground() bool {
	return lipgloss.HasDarkBackground(os.Stdin, os.Stdout)
}

func newPalette(isDark bool) appPalette {
	if isDark {
		return appPalette{
			IsDark:               true,
			TitleHex:             "#F8FAFC",
			SubtitleHex:          "#94A3B8",
			WindowHex:            "#E2E8F0",
			MutedHex:             "#94A3B8",
			ErrorHex:             dangerColorHex,
			ScrollTrackHex:       "#475569",
			ScrollThumbHex:       "#CBD5E1",
			LogoBorderHex:        "#CBD5E1",
			LogoTextHex:          "#F8FAFC",
			CompactLogoBGHex:     "#334155",
			StatusOKBadgeBGHex:   "#DCFCE7",
			StatusOKBadgeFGHex:   "#166534",
			StatusWarnBadgeBGHex: "#FEF3C7",
			StatusWarnBadgeFGHex: "#92400E",
			StatusErrBadgeBGHex:  "#FEE2E2",
			StatusErrBadgeFGHex:  "#991B1B",
		}
	}

	return appPalette{
		IsDark:               false,
		TitleHex:             "#0F172A",
		SubtitleHex:          "#475569",
		WindowHex:            "#1E293B",
		MutedHex:             "#64748B",
		ErrorHex:             "#DC2626",
		ScrollTrackHex:       "#CBD5E1",
		ScrollThumbHex:       "#64748B",
		LogoBorderHex:        "#94A3B8",
		LogoTextHex:          "#0F172A",
		CompactLogoBGHex:     "#E2E8F0",
		StatusOKBadgeBGHex:   "#DCFCE7",
		StatusOKBadgeFGHex:   "#166534",
		StatusWarnBadgeBGHex: "#FEF3C7",
		StatusWarnBadgeFGHex: "#92400E",
		StatusErrBadgeBGHex:  "#FEE2E2",
		StatusErrBadgeFGHex:  "#991B1B",
	}
}

func themeForProvider(name string, palette appPalette) providerTheme {
	switch strings.ToLower(name) {
	case "claude":
		if palette.IsDark {
			return providerTheme{
				BorderHex:  claudeColorHex,
				TitleHex:   claudeColorHex,
				BadgeBGHex: "#FCE8E2",
				BadgeFGHex: "#7C2D12",
				BarHex:     claudeColorHex,
				TrackHex:   "#334155",
				ChipBGHex:  "#5C2E22",
				ChipFGHex:  "#FFD9CF",
			}
		}
		return providerTheme{
			BorderHex:  "#C85D3F",
			TitleHex:   "#C85D3F",
			BadgeBGHex: "#FCE8E2",
			BadgeFGHex: "#7C2D12",
			BarHex:     claudeColorHex,
			TrackHex:   "#E2E8F0",
			ChipBGHex:  claudeColorHex,
			ChipFGHex:  "#FFF7ED",
		}

	case "openai":
		if palette.IsDark {
			return providerTheme{
				BorderHex:  openAIColorHex,
				TitleHex:   openAIColorHex,
				BadgeBGHex: "#334155",
				BadgeFGHex: "#F8FAFC",
				BarHex:     openAIBarColorHex,
				TrackHex:   "#475569",
				ChipBGHex:  "#334155",
				ChipFGHex:  "#F8FAFC",
			}
		}
		return providerTheme{
			BorderHex:  "#0F172A",
			TitleHex:   "#0F172A",
			BadgeBGHex: "#E2E8F0",
			BadgeFGHex: "#0F172A",
			BarHex:     "#6B7280",
			TrackHex:   "#E2E8F0",
			ChipBGHex:  "#E2E8F0",
			ChipFGHex:  "#0F172A",
		}

	case "gemini":
		if palette.IsDark {
			return providerTheme{
				BorderHex:  geminiColorHex,
				TitleHex:   geminiColorHex,
				BadgeBGHex: "#DDD6FE",
				BadgeFGHex: "#312E81",
				BarHex:     geminiColorHex,
				TrackHex:   "#334155",
				ChipBGHex:  "#312E81",
				ChipFGHex:  "#DDD6FE",
			}
		}
		return providerTheme{
			BorderHex:  "#7C3AED",
			TitleHex:   "#7C3AED",
			BadgeBGHex: "#E9D5FF",
			BadgeFGHex: "#581C87",
			BarHex:     geminiColorHex,
			TrackHex:   "#E2E8F0",
			ChipBGHex:  "#E9D5FF",
			ChipFGHex:  "#581C87",
		}
	default:
		if palette.IsDark {
			return providerTheme{
				BorderHex:  palette.SubtitleHex,
				TitleHex:   palette.WindowHex,
				BadgeBGHex: "#334155",
				BadgeFGHex: "#E2E8F0",
				BarHex:     palette.SubtitleHex,
				TrackHex:   "#334155",
				ChipBGHex:  "#334155",
				ChipFGHex:  "#E2E8F0",
			}
		}
		return providerTheme{
			BorderHex:  palette.MutedHex,
			TitleHex:   palette.TitleHex,
			BadgeBGHex: "#E2E8F0",
			BadgeFGHex: palette.TitleHex,
			BarHex:     palette.MutedHex,
			TrackHex:   "#E2E8F0",
			ChipBGHex:  "#E2E8F0",
			ChipFGHex:  palette.TitleHex,
		}
	}
}

func titleStyle(palette appPalette) lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(palette.TitleHex))
}

func subtitleStyle(palette appPalette) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(palette.SubtitleHex))
}

func windowStyle(palette appPalette) lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(palette.WindowHex))
}

func errorStyle(palette appPalette) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(palette.ErrorHex))
}

func subtleStyle(palette appPalette) lipgloss.Style {
	style := lipgloss.NewStyle().Foreground(lipgloss.Color(palette.MutedHex))
	if palette.IsDark {
		style = style.Faint(true)
	}
	return style
}

func scrollTrackStyle(palette appPalette) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(palette.ScrollTrackHex))
}

func scrollThumbStyle(palette appPalette) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(palette.ScrollThumbHex))
}

func logoBadgeStyle(palette appPalette) lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(palette.LogoBorderHex)).
		Padding(0, 2)
}

func logoTextStyle(palette appPalette) lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(palette.LogoTextHex))
}

func compactLogoStyle(palette appPalette) lipgloss.Style {
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(palette.LogoTextHex)).
		Background(lipgloss.Color(palette.CompactLogoBGHex)).
		Padding(0, 1)
}

func statusOKStyle(palette appPalette) lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(palette.StatusOKBadgeFGHex)).Background(lipgloss.Color(palette.StatusOKBadgeBGHex)).Padding(0, 1)
}

func statusWarnStyle(palette appPalette) lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(palette.StatusWarnBadgeFGHex)).Background(lipgloss.Color(palette.StatusWarnBadgeBGHex)).Padding(0, 1)
}

func statusErrorStyle(palette appPalette) lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(palette.StatusErrBadgeFGHex)).Background(lipgloss.Color(palette.StatusErrBadgeBGHex)).Padding(0, 1)
}

func cardStyle(theme providerTheme) lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(theme.BorderHex)).
		Padding(1)
}

func providerTitleStyle(theme providerTheme) lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(theme.TitleHex))
}

func providerBadgeStyle(theme providerTheme) lipgloss.Style {
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(theme.BadgeFGHex)).
		Background(lipgloss.Color(theme.BadgeBGHex)).
		Padding(0, 1)
}

func providerChipStyle(theme providerTheme) lipgloss.Style {
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(theme.ChipFGHex)).
		Background(lipgloss.Color(theme.ChipBGHex)).
		Padding(0, 1)
}

func orderIdleTitleStyle(theme providerTheme) lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(theme.TitleHex))
}

func orderPickedTitleStyle(theme providerTheme) lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(theme.TitleHex))
}

func orderPickedAccentStyle(theme providerTheme) lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(theme.BorderHex))
}

func orderPickedBadgeStyle(theme providerTheme) lipgloss.Style {
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(theme.BadgeFGHex)).
		Background(lipgloss.Color(theme.BadgeBGHex)).
		Padding(0, 1)
}

func orderDimmedStyle(palette appPalette) lipgloss.Style {
	return subtleStyle(palette).Faint(true)
}

func menuBackdropStyle(palette appPalette, progress float64) lipgloss.Style {
	style := lipgloss.NewStyle().Faint(true)
	if progress < 1 {
		style = style.Faint(true)
	}
	if palette.IsDark {
		return style.Foreground(lipgloss.Color(palette.SubtitleHex))
	}
	return style.Foreground(lipgloss.Color(palette.MutedHex))
}

func menuBoxStyle(palette appPalette, progress float64) lipgloss.Style {
	border := menuBorderHex(palette)
	if progress < 1 {
		border = menuBorderMutedHex(palette)
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(border)).
		Background(lipgloss.Color(menuSurfaceHex(palette))).
		Padding(1, 1)
}

func menuSurfaceHex(palette appPalette) string {
	if palette.IsDark {
		return "#18181B"
	}
	return "#FAFAFA"
}

func menuBorderHex(palette appPalette) string {
	if palette.IsDark {
		return "#52525B"
	}
	return "#D4D4D8"
}

func menuBorderMutedHex(palette appPalette) string {
	if palette.IsDark {
		return "#3F3F46"
	}
	return "#E4E4E7"
}

func menuSectionStyle(palette appPalette, width int) lipgloss.Style {
	return lipgloss.NewStyle().
		Width(width).
		Background(lipgloss.Color(menuSurfaceHex(palette)))
}

func menuItemTitleStyle(palette appPalette) lipgloss.Style {
	fg := palette.TitleHex
	if palette.IsDark {
		fg = "#F8FAFC"
	}
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(fg)).
		Padding(0, 0, 0, 1)
}

func menuItemDescStyle(palette appPalette) lipgloss.Style {
	fg := palette.MutedHex
	if palette.IsDark {
		fg = "#A1A1AA"
	}
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(fg)).
		Padding(0, 0, 0, 3)
}

func menuSelectedTitleStyle(palette appPalette) lipgloss.Style {
	fg := palette.TitleHex
	if palette.IsDark {
		fg = "#F8FAFC"
	}
	return lipgloss.NewStyle().
		Bold(true).
		Border(lipgloss.Border{Left: "→"}, false, false, false, true).
		BorderForeground(lipgloss.Color(palette.LogoBorderHex)).
		Foreground(lipgloss.Color(fg)).
		Padding(0, 0, 0, 0)
}

func menuSelectedDescStyle(palette appPalette) lipgloss.Style {
	fg := palette.MutedHex
	if palette.IsDark {
		fg = "#A1A1AA"
	}
	return lipgloss.NewStyle().
		Border(lipgloss.Border{Left: " "}, false, false, false, true).
		BorderForeground(lipgloss.Color(palette.LogoBorderHex)).
		Foreground(lipgloss.Color(fg)).
		Padding(0, 0, 0, 2)
}

func logoBarsView(palette appPalette) string {
	return lipgloss.JoinHorizontal(
		lipgloss.Center,
		providerTitleStyle(themeForProvider("claude", palette)).Render("▌"),
		providerTitleStyle(themeForProvider("openai", palette)).Render("▌"),
		providerTitleStyle(themeForProvider("gemini", palette)).Render("▌"),
	)
}
