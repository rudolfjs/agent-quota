package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

const quickViewBorderHex = "#84CC16"

type quickViewMetric struct {
	ID           string
	ProviderName string
	MetricName   string
	ExtraUsage   bool
}

func (m quickViewMetric) displayName() string {
	if m.ExtraUsage {
		return "Extra Usage"
	}
	return displayMetricName(m.ProviderName, m.MetricName)
}

func (m quickViewMetric) menuLabel(palette appPalette) string {
	theme := themeForProvider(m.ProviderName, palette)
	return lipgloss.JoinHorizontal(
		lipgloss.Center,
		providerTitleStyle(theme).Render(providerDisplayName(m.ProviderName)),
		subtleStyle(palette).Render(": "),
		providerTitleStyle(theme).Render(m.displayName()),
	)
}

func (m quickViewMetric) contentLabel(palette appPalette) string {
	theme := themeForProvider(m.ProviderName, palette)
	return lipgloss.JoinHorizontal(
		lipgloss.Center,
		providerTitleStyle(theme).Render(providerDisplayName(m.ProviderName)),
		subtleStyle(palette).Render(" • "),
		providerTitleStyle(theme).Render(m.displayName()),
	)
}

func quickViewMetricID(providerName, metricName string) string {
	providerName = strings.ToLower(strings.TrimSpace(providerName))
	return providerName + ":" + normalizeMetricName(providerName, metricName)
}

func parseQuickViewMetricID(id string) (quickViewMetric, bool) {
	parts := strings.SplitN(strings.TrimSpace(strings.ToLower(id)), ":", 2)
	if len(parts) != 2 || parts[0] == "" || strings.TrimSpace(parts[1]) == "" {
		return quickViewMetric{}, false
	}
	metricName := normalizeMetricName(parts[0], strings.TrimSpace(parts[1]))
	metric := quickViewMetric{
		ID:           parts[0] + ":" + metricName,
		ProviderName: parts[0],
		MetricName:   metricName,
	}
	metric.ExtraUsage = metric.MetricName == "extra_usage"
	return metric, true
}

func (m Model) availableQuickViewMetrics() []quickViewMetric {
	metrics := make([]quickViewMetric, 0)
	seen := make(map[string]struct{})
	for _, p := range m.providers {
		name := strings.ToLower(p.Name())
		result, ok := m.results[name]
		if !ok {
			result, ok = m.cachedResults[name]
		}
		if !ok || result.Status != "ok" {
			continue
		}
		for _, window := range result.Windows {
			if !isQuickViewMetricSupported(name, window.Name) {
				continue
			}
			metric := quickViewMetric{
				ID:           quickViewMetricID(name, window.Name),
				ProviderName: name,
				MetricName:   window.Name,
			}
			if _, ok := seen[metric.ID]; ok {
				continue
			}
			seen[metric.ID] = struct{}{}
			metrics = append(metrics, metric)
		}
		if result.ExtraUsage != nil && result.ExtraUsage.Enabled {
			metric := quickViewMetric{
				ID:           quickViewMetricID(name, "extra_usage"),
				ProviderName: name,
				MetricName:   "extra_usage",
				ExtraUsage:   true,
			}
			if _, ok := seen[metric.ID]; ok {
				continue
			}
			seen[metric.ID] = struct{}{}
			metrics = append(metrics, metric)
		}
	}
	return metrics
}

func (m Model) quickViewContent() string {
	width := max(m.viewport.Width(), minViewportWidth)
	sections := make([]string, 0, len(m.settings.QuickView))
	for _, id := range m.settings.QuickView {
		section, ok := m.renderQuickViewMetricByID(id, width)
		if !ok {
			continue
		}
		sections = append(sections, section)
	}

	switch {
	case len(m.settings.QuickView) == 0:
		sections = []string{
			windowStyle(m.palette).Render("Quick View"),
			subtleStyle(m.palette).Render("No metrics selected."),
			subtleStyle(m.palette).Render("Esc → Quick View to choose metrics."),
		}
	case len(sections) == 0:
		sections = []string{
			windowStyle(m.palette).Render("Quick View"),
			subtleStyle(m.palette).Render("Waiting for quota data."),
		}
	}

	return quickViewBoxStyle().Width(width).Render(strings.Join(sections, "\n\n"))
}

func (m Model) renderQuickViewMetricByID(id string, width int) (string, bool) {
	metric, ok := parseQuickViewMetricID(id)
	if !ok {
		return "", false
	}
	if !metric.ExtraUsage && !isQuickViewMetricSupported(metric.ProviderName, metric.MetricName) {
		return "", false
	}

	result, ok := m.results[metric.ProviderName]
	if !ok {
		result, ok = m.cachedResults[metric.ProviderName]
	}
	if !ok || result.Status != "ok" {
		return "", false
	}

	theme := themeForProvider(metric.ProviderName, m.palette)
	heading := metric.contentLabel(m.palette)
	lines := []string{heading}
	if metric.ExtraUsage {
		if result.ExtraUsage == nil || !result.ExtraUsage.Enabled {
			return "", false
		}
		lines = append(lines, renderQuotaBar(theme, clampPercent(result.ExtraUsage.Utilization), width-4))
		lines = append(lines, subtleStyle(m.palette).Render(fmt.Sprintf("$%.2f / $%.2f used", result.ExtraUsage.UsedUSD, result.ExtraUsage.LimitUSD)))
		if rs, ok := m.retryStates[metric.ProviderName]; ok {
			lines = append(lines, errorStyle(m.palette).Render(fmt.Sprintf("stale • retry in %ds", rs.secondsLeft)))
		}
		return strings.Join(lines, "\n"), true
	}

	for _, window := range result.Windows {
		if window.Name != metric.MetricName {
			continue
		}
		utilization := clampPercent(window.Utilization)
		lines = append(lines, renderQuotaBar(theme, utilization, width-4))
		lines = append(lines, subtleStyle(m.palette).Render(fmt.Sprintf("%d%% used • resets %s", percent(utilization), formatRelativeTime(window.ResetsAt))))
		if rs, ok := m.retryStates[metric.ProviderName]; ok {
			lines = append(lines, errorStyle(m.palette).Render(fmt.Sprintf("stale • retry in %ds", rs.secondsLeft)))
		}
		return strings.Join(lines, "\n"), true
	}
	return "", false
}

// metricDisplay holds the pretty display info for a quota metric.
type metricDisplay struct {
	Group string // subheader group name, empty = standalone
	Name  string // human-friendly metric name
}

// knownMetricDisplays maps "provider:metric" to a metricDisplay for well-known metrics.
var knownMetricDisplays = map[string]metricDisplay{
	"claude:five_hour":             {Name: "Session"},
	"claude:seven_day":             {Group: "Weekly Limits", Name: "All Models"},
	"claude:seven_day_sonnet":      {Group: "Weekly Limits", Name: "Sonnet"},
	"openai:five_hour":             {Name: "Session"},
	"openai:seven_day":             {Group: "Weekly Limits", Name: "All Models"},
	"copilot:chat":                 {Name: "Chat"},
	"copilot:completions":          {Name: "Completions"},
	"copilot:premium_interactions": {Name: "Premium Interactions"},
}

// metricDisplayInfo returns the group and pretty name for a quota metric. It
// normalizes legacy API names (e.g. codex_bengalfox_) before lookup.
func metricDisplayInfo(providerName, metricName string) metricDisplay {
	normalized := normalizeMetricName(providerName, metricName)
	key := strings.ToLower(strings.TrimSpace(providerName)) + ":" + normalized
	if d, ok := knownMetricDisplays[key]; ok {
		return d
	}
	prov := strings.ToLower(strings.TrimSpace(providerName))
	// OpenAI prefixed windows: e.g. codex_spark_five_hour, code_review_seven_day.
	// The prefix becomes the group header; the base becomes the metric name.
	if prov == "openai" {
		if prefix, base, ok := splitOpenAIPrefixedMetric(normalized); ok {
			return metricDisplay{
				Group: prettyRawName(prefix),
				Name:  baseMetricPrettyName(base),
			}
		}
	}
	// Gemini model IDs (e.g. "gemini-2.5-flash") group by major version.
	if prov == "gemini" {
		if group, ok := geminiModelGroup(normalized); ok {
			return metricDisplay{
				Group: group,
				Name:  prettyRawName(normalized),
			}
		}
	}
	return metricDisplay{Name: prettyRawName(normalized)}
}

// geminiModelGroup returns the display group ("Gemini 2", "Gemini 3", …) for a
// Gemini model ID like "gemini-2.5-flash". Returns ok=false for unrecognised IDs.
func geminiModelGroup(modelID string) (string, bool) {
	if !strings.HasPrefix(modelID, "gemini-") {
		return "", false
	}
	rest := strings.TrimPrefix(modelID, "gemini-")
	// rest = "2.5-flash", "3.1-pro", etc. Extract the major version digit(s).
	dotIdx := strings.Index(rest, ".")
	hyphenIdx := strings.Index(rest, "-")
	end := len(rest)
	if dotIdx >= 0 {
		end = dotIdx
	}
	if hyphenIdx >= 0 && hyphenIdx < end {
		end = hyphenIdx
	}
	major := rest[:end]
	if major == "" {
		return "", false
	}
	for _, c := range major {
		if c < '0' || c > '9' {
			return "", false
		}
	}
	return fmt.Sprintf("Gemini %s", major), true
}

// splitOpenAIPrefixedMetric splits "codex_spark_five_hour" into ("codex_spark", "five_hour").
// Returns ok=false when the metric has no recognised base suffix.
func splitOpenAIPrefixedMetric(metricName string) (prefix, base string, ok bool) {
	for _, suffix := range []string{"_five_hour", "_seven_day"} {
		if strings.HasSuffix(metricName, suffix) && len(metricName) > len(suffix) {
			return strings.TrimSuffix(metricName, suffix), strings.TrimPrefix(suffix, "_"), true
		}
	}
	return "", "", false
}

// baseMetricPrettyName returns the human-friendly name for a base window name like "five_hour".
func baseMetricPrettyName(base string) string {
	switch base {
	case "five_hour":
		return "Session"
	case "seven_day":
		return "All Models"
	default:
		return prettyRawName(base)
	}
}

// prettyRawName converts raw API identifiers to title-cased human-readable labels.
// "gemini-2.5-flash" → "Gemini 2.5 Flash", "premium_interactions" → "Premium Interactions".
func prettyRawName(s string) string {
	s = strings.ReplaceAll(s, "-", " ")
	s = strings.ReplaceAll(s, "_", " ")
	words := strings.Fields(s)
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}

func displayMetricName(providerName, metricName string) string {
	return metricDisplayInfo(providerName, metricName).Name
}

func normalizeMetricName(providerName, metricName string) string {
	providerName = strings.ToLower(strings.TrimSpace(providerName))
	metricName = strings.TrimSpace(metricName)

	if providerName == "openai" && strings.HasPrefix(metricName, "codex_bengalfox_") {
		return "codex_spark_" + strings.TrimPrefix(metricName, "codex_bengalfox_")
	}
	return metricName
}

func isQuickViewMetricSupported(providerName, metricName string) bool {
	providerName = strings.ToLower(strings.TrimSpace(providerName))
	metricName = strings.TrimSpace(metricName)

	if providerName == "claude" {
		switch metricName {
		case "seven_day_oauth_apps", "seven_day_opus":
			return false
		}
	}
	return true
}

func quickViewBoxStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(quickViewBorderHex)).
		Padding(1)
}
