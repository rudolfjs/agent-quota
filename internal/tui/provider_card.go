package tui

import (
	"fmt"
	"image/color"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"charm.land/bubbles/v2/progress"
	"charm.land/lipgloss/v2"

	"github.com/rudolfjs/agent-quota/internal/provider"
)

// RenderProviderCard renders a single provider's QuotaResult as a styled card string.
// It is a sub-component, so it returns a string, not tea.View.
func RenderProviderCard(r provider.QuotaResult, width int) string {
	return renderProviderCardWithPalette(r, width, newPalette(true), true)
}

func renderProviderCardWithPalette(r provider.QuotaResult, width int, palette appPalette, showGuide bool) string {
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
		sections = append(sections, renderWindow(info.Name, info.Group != "", w, width, theme, palette, showGuide))
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
func renderWindow(name string, inGroup bool, w provider.Window, width int, theme providerTheme, palette appPalette, showGuide bool) string {
	usedPct := percent(w.Utilization)
	resetStr := formatRelativeTime(w.ResetsAt)

	barWidth := width
	indent := ""
	if inGroup {
		barWidth = max(width-2, 12)
		indent = "  "
	}

	guide := -1.0
	if showGuide {
		guide = budgetGuide(w.Name, w.ResetsAt)
	}
	subtitle := fmt.Sprintf("%d%% used • resets %s", usedPct, resetStr)
	if guide >= 0 {
		subtitle = fmt.Sprintf("%d%% used • guide %d%% • resets %s", usedPct, percent(guide), resetStr)
	}

	return lipgloss.JoinVertical(
		lipgloss.Left,
		indent+windowStyle(palette).Render(name),
		indent+renderQuotaBar(theme, w.Utilization, barWidth, guide),
		indent+subtleStyle(palette).Render(subtitle),
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
		renderQuotaBar(theme, extra.Utilization, width, -1),
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

// renderQuotaBar renders a static progress bar with an optional budget guide marker.
// guide < 0 means no guide; guide in [0,1] places a fluorescent blue │ at that position.
func renderQuotaBar(theme providerTheme, utilization float64, width int, guide float64) string {
	barWidth := max(width-10, 12)
	b := newProgressBar(theme)
	b.SetWidth(barWidth)
	bar := b.ViewAs(utilization)
	if guide < 0 || guide > 1 {
		return bar
	}
	return injectGuideMarker(bar, guidePosition(utilization, guide, barWidth))
}

// guidePosition snaps the guide marker cell so visual ordering matches the
// numeric comparison between utilization and guide: when guide ≤ util the
// marker sits inside the filled region; when guide > util it sits past it.
// At saturation (fw == barWidth) there is no empty cell left, so the marker
// is clamped to the last cell even though the "past fill" invariant can no
// longer hold at cell resolution.
func guidePosition(utilization, guide float64, barWidth int) int {
	// math.Round matches bubbles/progress barView (progress.go:360 in v2.1.0);
	// using a different rounding mode here would desync fw from what the bar
	// actually renders and the snap logic would misfire.
	fw := int(math.Round(utilization * float64(barWidth)))
	pos := int(math.Round(guide * float64(barWidth)))
	if guide <= utilization && pos >= fw {
		pos = fw - 1
	}
	if guide > utilization && pos < fw {
		pos = fw
	}
	return max(0, min(pos, barWidth-1))
}

// injectGuideMarker splices a fluorescent blue │ into the progress bar at the
// caller-supplied cell index. Callers are responsible for snapping the index
// into range via guidePosition.
//
// With WithColorFunc the bar wraps each filled cell individually:
//
//	\x1b[color]█\x1b[m\x1b[color]█\x1b[m…\x1b[track-color]░░░…\x1b[m
//
// The empty section is a single multi-char span. We handle both cases by
// replacing only the target character, closing/re-opening the active ANSI span
// around the marker.
func injectGuideMarker(bar string, guidePos int) string {
	guideStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(guideColorHex)).Bold(true)
	marker := guideStyle.Render("│")

	b := []byte(bar)
	visibleIdx := 0
	byteIdx := 0
	var activeColor string // most recent color escape (non-reset)

	for byteIdx < len(b) {
		// Capture ANSI escape sequences.
		if b[byteIdx] == 0x1b && byteIdx+1 < len(b) && b[byteIdx+1] == '[' {
			j := byteIdx + 2
			for j < len(b) && (b[j] < 'A' || b[j] > 'Z') && (b[j] < 'a' || b[j] > 'z') {
				j++
			}
			if j < len(b) {
				j++ // include terminator
			}
			seq := string(b[byteIdx:j])
			if seq != "\x1b[m" && seq != "\x1b[0m" {
				activeColor = seq
			} else {
				activeColor = ""
			}
			byteIdx = j
			continue
		}

		if visibleIdx == guidePos {
			_, size := utf8.DecodeRune(b[byteIdx:])

			// Build: [before char] + reset + marker + re-open color + [after char]
			var sb strings.Builder
			sb.Write(b[:byteIdx])
			sb.WriteString("\x1b[m") // close current color span
			sb.WriteString(marker)
			if activeColor != "" {
				sb.WriteString(activeColor) // re-open the span for remaining chars
			}
			sb.Write(b[byteIdx+size:])
			return sb.String()
		}

		_, size := utf8.DecodeRune(b[byteIdx:])
		visibleIdx++
		byteIdx += size
	}

	return bar
}

// windowDuration returns the total duration for a known window name.
// Returns 0 for unrecognised windows.
func windowDuration(name string) time.Duration {
	// Gemini model windows (e.g. "gemini-2.5-pro") use a 24-hour reset.
	if strings.HasPrefix(name, "gemini-") {
		return 24 * time.Hour
	}
	base := name
	// Strip known prefixes (e.g. "codex_spark_five_hour" → "five_hour").
	for _, suffix := range []string{"_five_hour", "_seven_day"} {
		if strings.HasSuffix(name, suffix) && len(name) > len(suffix) {
			base = strings.TrimPrefix(suffix, "_")
			break
		}
	}
	switch base {
	case "five_hour":
		return 5 * time.Hour
	case "seven_day", "seven_day_sonnet", "seven_day_opus", "seven_day_oauth_apps":
		return 7 * 24 * time.Hour
	default:
		return 0
	}
}

// budgetGuide computes the linear budget guide position for a window.
// Returns a value in [0,1] representing how far through the window we are,
// or -1 if the window duration is unknown.
func budgetGuide(windowName string, resetsAt time.Time) float64 {
	dur := windowDuration(windowName)
	if dur == 0 {
		return -1
	}
	remaining := time.Until(resetsAt)
	if remaining < 0 {
		return 1.0
	}
	elapsed := dur - remaining
	if elapsed < 0 {
		return 0.0
	}
	return clampPercent(float64(elapsed) / float64(dur))
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
