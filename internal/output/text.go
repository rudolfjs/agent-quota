package output

import (
	"fmt"
	"io"
	"math"
	"time"

	"github.com/rudolfjs/agent-quota/internal/provider"
)

// WriteText writes a human-readable summary of results to w.
// The now parameter is used to compute relative reset times.
func WriteText(w io.Writer, results []provider.QuotaResult, now time.Time) error {
	for i, r := range results {
		if i > 0 {
			if _, err := fmt.Fprintln(w); err != nil {
				return err
			}
		}

		header := r.Provider
		if r.Plan != "" {
			header += " [" + r.Plan + "]"
		}
		header += " " + r.Status
		if _, err := fmt.Fprintln(w, header); err != nil {
			return err
		}
		if r.Error != nil && r.Error.Message != "" {
			if _, err := fmt.Fprintf(w, "  %s\n", r.Error.Message); err != nil {
				return err
			}
		}
		if r.Status != "ok" {
			continue
		}

		for _, win := range r.Windows {
			pct := int(math.Round(win.Utilization * 100))
			remaining := formatDuration(win.ResetsAt.Sub(now))
			if _, err := fmt.Fprintf(w, "  %-12s %d%% used  (resets in %s)\n", win.Name+":", pct, remaining); err != nil {
				return err
			}
		}

		if r.ExtraUsage != nil && r.ExtraUsage.Enabled {
			if _, err := fmt.Fprintf(w, "  extra: $%.2f / $%.2f USD\n", r.ExtraUsage.UsedUSD, r.ExtraUsage.LimitUSD); err != nil {
				return err
			}
		}
	}
	return nil
}

// formatDuration produces a compact human-readable duration like "4h30m" or "3d2h".
func formatDuration(d time.Duration) string {
	if d <= 0 {
		return "0m"
	}

	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60

	switch {
	case days > 0 && hours > 0:
		return fmt.Sprintf("%dd%dh", days, hours)
	case days > 0:
		return fmt.Sprintf("%dd", days)
	case hours > 0 && minutes > 0:
		return fmt.Sprintf("%dh%dm", hours, minutes)
	case hours > 0:
		return fmt.Sprintf("%dh", hours)
	default:
		return fmt.Sprintf("%dm", minutes)
	}
}
