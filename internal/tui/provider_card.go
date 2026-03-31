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

	var currentGroup string
	for _, w := range r.Windows {
		if !isQuickViewMetricSupported(r.Provider, w.Name) {
			continue
		}
		info := metricDisplayInfo(r.Provider, w.Name)
		if info.Group != currentGroup {
			currentGroup = info.Group
			if info.Group != "" {
				sections = append(sections, subtitleStyle(palette).Render(info.Group))
			}
		}
		sections = append(sections, renderWindow(info.Name, info.Group != "", w, width, theme, palette))
	}

	if r.ExtraUsage != nil && r.ExtraUsage.Enabled {
		sections = append(sections, renderExtraUsage(*r.ExtraUsage, width, theme, palette))
	}

	return cardStyle(theme).Width(width).Render(strings.Join(sections, "\n\n"))
}

func renderProviderHeader(r provider.QuotaResult, theme providerTheme, palette appPalette) string {
	providerPart := providerTitleStyle(theme).Render(providerLabel(r.Provider))
	plan := strings.ToUpper(r.Plan)
	status := strings.ToUpper(r.Status)

	badgeGroup := ""
	switch {
	case plan != "" && status != "":
		badgeGroup = lipgloss.JoinHorizontal(
			lipgloss.Center,
			providerBadgeStyle(theme).Padding(0, 0, 0, 1).Render(plan),
			renderJoinedStatusBadge(status, palette),
		)
	case plan != "":
		badgeGroup = providerBadgeStyle(theme).Render(plan)
	case status != "":
		badgeGroup = renderStatusBadge(r.Status, palette)
	}

	if badgeGroup == "" {
		return providerPart
	}
	return lipgloss.JoinHorizontal(lipgloss.Center, providerPart, " ", badgeGroup)
}

func renderStatusBadge(status string, palette appPalette) string {
	return renderStatusBadgeWithLeftPadding(status, palette, true)
}

func renderJoinedStatusBadge(status string, palette appPalette) string {
	return renderStatusBadgeWithLeftPadding(status, palette, false)
}

func renderStatusBadgeWithLeftPadding(status string, palette appPalette, leftPadding bool) string {
	if status == "" {
		return ""
	}

	style := statusWarnStyle(palette)
	label := strings.ToUpper(status)
	if !leftPadding {
		style = style.Padding(0, 1, 0, 0)
	}

	switch status {
	case "ok":
		style = statusOKStyle(palette)
		label = "OK"
	case "unavailable":
		style = statusWarnStyle(palette)
		label = "UNAVAILABLE"
	case "error":
		style = statusErrorStyle(palette)
		label = "ERROR"
	}
	if !leftPadding {
		style = style.Padding(0, 1, 0, 0)
	}
	return style.Render(label)
}

func providerLabel(name string) string {
	return providerDisplayName(name)
}

func providerDisplayName(name string) string {
	switch strings.ToLower(name) {
	case "claude":
		return "Claude"
	case "openai":
		return "OpenAI"
	case "gemini":
		return "Gemini"
	case "copilot":
		return "Copilot"
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

// renderWindow renders a single quota window. name is the human-friendly display
// name; inGroup=true adds a 2-space indent so the entry sits under a group header.
func renderWindow(name string, inGroup bool, w provider.Window, width int, theme providerTheme, palette appPalette) string {
	usedPct := percent(w.Utilization)
	resetStr := formatRelativeTime(w.ResetsAt)

	barWidth := width
	indent := ""
	if inGroup {
		barWidth = max(width-2, 12)
		indent = "  "
	}

	return lipgloss.JoinVertical(
		lipgloss.Left,
		indent+windowStyle(palette).Render(name),
		indent+renderQuotaBar(theme, w.Utilization, barWidth),
		indent+subtleStyle(palette).Render(fmt.Sprintf("%d%% used • resets %s", usedPct, resetStr)),
	)
}

func renderExtraUsage(extra provider.ExtraUsage, width int, theme providerTheme, palette appPalette) string {
	if extra.LimitUSD <= 0 {
		return fmt.Sprintf("Spend: $%.2f", extra.UsedUSD)
	}

	heading := lipgloss.JoinHorizontal(
		lipgloss.Center,
		windowStyle(palette).Render("Extra Usage"),
		subtleStyle(palette).Render(" • "),
		quotaLabelStyle(theme, extra.Utilization).Render(fmt.Sprintf("$%.2f / $%.2f", extra.UsedUSD, extra.LimitUSD)),
	)

	return lipgloss.JoinVertical(
		lipgloss.Left,
		heading,
		renderQuotaBar(theme, extra.Utilization, width),
		subtleStyle(palette).Render(fmt.Sprintf("%d%% of spend limit used", percent(extra.Utilization))),
	)
}

// newProgressBar creates a progress.Model configured with the given provider theme.
// Callers that store bars in the model use this to initialise new bars.
func newProgressBar(theme providerTheme) progress.Model {
	bar := progress.New(
		progress.WithFillCharacters(progress.DefaultFullCharFullBlock, progress.DefaultEmptyCharBlock),
		progress.WithColorFunc(func(total, _ float64) color.Color {
			return lipgloss.Color(quotaBarColorHex(theme, total))
		}),
	)
	bar.ShowPercentage = false
	bar.EmptyColor = lipgloss.Color(theme.TrackHex)
	return bar
}

// renderQuotaBar renders a static progress bar that fills as utilization increases.
// Quota changes are infrequent, so direct ViewAs rendering is easier to read than
// a spring animation.
func renderQuotaBar(theme providerTheme, utilization float64, width int) string {
	barWidth := max(width-10, 12)
	b := newProgressBar(theme)
	b.SetWidth(barWidth)
	return b.ViewAs(utilization)
}

func quotaBarColorHex(theme providerTheme, utilization float64) string {
	switch {
	case utilization > 0.80:
		return dangerColorHex
	case utilization > 0.50:
		return warningColorHex
	default:
		return theme.BarHex
	}
}

func quotaLabelStyle(theme providerTheme, utilization float64) lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(quotaBarColorHex(theme, utilization)))
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
