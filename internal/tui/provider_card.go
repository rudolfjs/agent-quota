package tui

import (
	"fmt"
	"image/color"
	"math"
	"strings"
	"time"

	"charm.land/bubbles/v2/progress"
	"charm.land/lipgloss/v2"

	"github.com/schnetlerr/agent-quota/internal/provider"
)

// RenderProviderCard renders a single provider's QuotaResult as a styled card string.
// It is a sub-component, so it returns a string, not tea.View.
func RenderProviderCard(r provider.QuotaResult, width int) string {
	return renderProviderCardWithPalette(r, width, newPalette(true))
}

func renderProviderCardWithPalette(r provider.QuotaResult, width int, palette appPalette) string {
	theme := themeForProvider(r.Provider, palette)
	sections := []string{renderProviderHeader(r, theme, palette)}

	if r.Status != "ok" {
		if detail := providerStatusDetail(r.Status); detail != "" {
			sections = append(sections, subtleStyle(palette).Render(detail))
		}
		return cardStyle(theme).Width(width).Render(strings.Join(sections, "\n\n"))
	}

	for _, w := range r.Windows {
		sections = append(sections, renderWindow(w, width, theme, palette))
	}

	if r.ExtraUsage != nil && r.ExtraUsage.Enabled {
		sections = append(sections, renderExtraUsage(*r.ExtraUsage, width, theme, palette))
	}

	return cardStyle(theme).Width(width).Render(strings.Join(sections, "\n\n"))
}

func renderProviderHeader(r provider.QuotaResult, theme providerTheme, palette appPalette) string {
	parts := []string{providerTitleStyle(theme).Render(providerLabel(r.Provider))}
	if r.Plan != "" {
		parts = append(parts, providerBadgeStyle(theme).Render(strings.ToUpper(r.Plan)))
	}
	if badge := renderStatusBadge(r.Status, palette); badge != "" {
		parts = append(parts, badge)
	}
	return lipgloss.JoinHorizontal(lipgloss.Center, parts...)
}

func renderStatusBadge(status string, palette appPalette) string {
	if status == "" {
		return ""
	}

	switch status {
	case "ok":
		return statusOKStyle(palette).Render("OK")
	case "unavailable":
		return statusWarnStyle(palette).Render("UNAVAILABLE")
	case "error":
		return statusErrorStyle(palette).Render("ERROR")
	default:
		return statusWarnStyle(palette).Render(strings.ToUpper(status))
	}
}

func providerLabel(name string) string {
	return providerIcon(name) + " " + providerDisplayName(name)
}

func providerIcon(name string) string {
	switch strings.ToLower(name) {
	case "claude":
		return "✦"
	case "openai":
		return "◎"
	case "gemini":
		return "◆"
	default:
		return "•"
	}
}

func providerDisplayName(name string) string {
	switch strings.ToLower(name) {
	case "claude":
		return "Claude"
	case "openai":
		return "OpenAI"
	case "gemini":
		return "Gemini"
	default:
		return name
	}
}

func providerStatusDetail(status string) string {
	switch status {
	case "unavailable":
		return "Credentials not configured on this machine."
	case "error":
		return "Unable to fetch latest quota data."
	default:
		return ""
	}
}

func renderWindow(w provider.Window, width int, theme providerTheme, palette appPalette) string {
	usedPct := percent(w.Utilization)
	remaining := clampPercent(1 - w.Utilization)
	remainingPct := percent(remaining)
	resetStr := formatRelativeTime(w.ResetsAt)

	heading := lipgloss.JoinHorizontal(
		lipgloss.Center,
		windowStyle(palette).Render(w.Name),
		subtleStyle(palette).Render(" • "),
		quotaLabelStyle(theme, remaining).Render(fmt.Sprintf("%d%% left", remainingPct)),
	)

	return lipgloss.JoinVertical(
		lipgloss.Left,
		heading,
		renderQuotaBar(theme, remaining, width),
		subtleStyle(palette).Render(fmt.Sprintf("%d%% used • resets %s", usedPct, resetStr)),
	)
}

func renderExtraUsage(extra provider.ExtraUsage, width int, theme providerTheme, palette appPalette) string {
	if extra.LimitUSD <= 0 {
		return fmt.Sprintf("Spend: $%.2f", extra.UsedUSD)
	}

	remaining := clampPercent(1 - extra.Utilization)
	heading := lipgloss.JoinHorizontal(
		lipgloss.Center,
		windowStyle(palette).Render("extra usage"),
		subtleStyle(palette).Render(" • "),
		quotaLabelStyle(theme, remaining).Render(fmt.Sprintf("$%.2f / $%.2f", extra.UsedUSD, extra.LimitUSD)),
	)

	return lipgloss.JoinVertical(
		lipgloss.Left,
		heading,
		renderQuotaBar(theme, remaining, width),
		subtleStyle(palette).Render(fmt.Sprintf("%d%% of spend limit used", percent(extra.Utilization))),
	)
}

func renderQuotaBar(theme providerTheme, remaining float64, width int) string {
	bar := progress.New(
		progress.WithFillCharacters(progress.DefaultFullCharFullBlock, progress.DefaultEmptyCharBlock),
		progress.WithColorFunc(func(total, _ float64) color.Color {
			return lipgloss.Color(quotaBarColorHex(theme, total))
		}),
	)
	bar.ShowPercentage = false
	bar.EmptyColor = lipgloss.Color(theme.TrackHex)
	barWidth := width - 10
	if barWidth < 12 {
		barWidth = 12
	}
	bar.SetWidth(barWidth)
	return bar.ViewAs(remaining)
}

func quotaBarColorHex(theme providerTheme, remaining float64) string {
	switch {
	case remaining < 0.20:
		return dangerColorHex
	case remaining < 0.50:
		return warningColorHex
	default:
		return theme.BarHex
	}
}

func quotaLabelStyle(theme providerTheme, remaining float64) lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(quotaBarColorHex(theme, remaining)))
}

func clampPercent(value float64) float64 {
	return math.Max(0, math.Min(1, value))
}

func percent(value float64) int {
	return int(math.Round(clampPercent(value) * 100))
}

func formatRelativeTime(t time.Time) string {
	d := time.Until(t)
	if d < 0 {
		return "expired"
	}
	if d < time.Minute {
		return "in <1m"
	}
	if d < time.Hour {
		return fmt.Sprintf("in %dm", int(d.Minutes()))
	}
	hours := int(d.Hours())
	if hours < 24 {
		return fmt.Sprintf("in %dh", hours)
	}
	return fmt.Sprintf("in %dd %dh", hours/24, hours%24)
}
