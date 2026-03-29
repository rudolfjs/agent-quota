package tui

import (
	"fmt"
	"math"
	"strings"
	"time"

	"charm.land/bubbles/v2/progress"

	"github.com/schnetlerr/agent-quota/internal/provider"
)

// RenderProviderCard renders a single provider's QuotaResult as a styled card string.
// It is a sub-component, so it returns a string, not tea.View.
func RenderProviderCard(r provider.QuotaResult, width int) string {
	var b strings.Builder

	b.WriteString(titleStyle.Render(r.Provider))
	b.WriteString("\n")

	if r.Plan != "" {
		b.WriteString("  Plan: ")
		b.WriteString(r.Plan)
		b.WriteString("\n")
	}
	if r.Status != "" {
		line := "  Status: " + r.Status
		if r.Status == "error" || r.Status == "unavailable" {
			b.WriteString(errorStyle.Render(line))
		} else {
			b.WriteString(line)
		}
		b.WriteString("\n")
	}
	if r.Status != "ok" {
		return cardStyle.Width(width).Render(b.String())
	}

	for _, w := range r.Windows {
		b.WriteString(renderWindow(w, width))
		b.WriteString("\n")
	}

	if r.ExtraUsage != nil && r.ExtraUsage.Enabled {
		b.WriteString(fmt.Sprintf("  Spend: $%.2f / $%.2f", r.ExtraUsage.UsedUSD, r.ExtraUsage.LimitUSD))
		b.WriteString("\n")
	}

	return cardStyle.Width(width).Render(b.String())
}

func renderWindow(w provider.Window, width int) string {
	usedPct := percent(w.Utilization)
	remaining := clampPercent(1 - w.Utilization)
	remainingPct := percent(remaining)
	resetStr := formatRelativeTime(w.ResetsAt)

	bar := progress.New()
	bar.ShowPercentage = false
	barWidth := width - 10
	if barWidth < 12 {
		barWidth = 12
	}
	bar.SetWidth(barWidth)

	return fmt.Sprintf("  %s %d%% left\n  %s\n  %s",
		windowStyle.Render(w.Name),
		remainingPct,
		bar.ViewAs(remaining),
		subtleStyle.Render(fmt.Sprintf("%d%% used • resets %s", usedPct, resetStr)),
	)
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
