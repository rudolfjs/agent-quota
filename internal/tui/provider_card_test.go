package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/schnetlerr/agent-quota/internal/provider"
)

func TestRenderProviderCard_containsProviderName(t *testing.T) {
	r := provider.QuotaResult{
		Provider: "claude",
		Status:   "ok",
		Windows: []provider.Window{
			{
				Name:        "five_hour",
				Utilization: 0.35,
				ResetsAt:    time.Now().Add(2 * time.Hour),
			},
		},
		FetchedAt: time.Now(),
	}

	got := RenderProviderCard(r, 60)

	if !strings.Contains(got, "Claude") {
		t.Errorf("expected card to contain provider name 'Claude', got:\n%s", got)
	}
}

func TestRenderProviderCard_containsWindowName(t *testing.T) {
	r := provider.QuotaResult{
		Provider: "claude",
		Status:   "ok",
		Windows: []provider.Window{
			{
				Name:        "five_hour",
				Utilization: 0.35,
				ResetsAt:    time.Now().Add(2 * time.Hour),
			},
		},
		FetchedAt: time.Now(),
	}

	got := RenderProviderCard(r, 60)

	if !strings.Contains(got, "Session") {
		t.Errorf("expected card to contain pretty window name 'Session', got:\n%s", got)
	}
}

func TestRenderProviderCard_containsRemainingAndUsedPercent(t *testing.T) {
	r := provider.QuotaResult{
		Provider: "claude",
		Status:   "ok",
		Windows: []provider.Window{
			{
				Name:        "five_hour",
				Utilization: 0.35,
				ResetsAt:    time.Now().Add(2 * time.Hour),
			},
		},
		FetchedAt: time.Now(),
	}

	got := RenderProviderCard(r, 60)

	if !strings.Contains(got, "35% used") {
		t.Errorf("expected card to contain utilization '35%% used', got:\n%s", got)
	}
}

func TestRenderProviderCard_extraUsageEnabled(t *testing.T) {
	r := provider.QuotaResult{
		Provider: "claude",
		Status:   "ok",
		Windows: []provider.Window{
			{
				Name:        "five_hour",
				Utilization: 0.10,
				ResetsAt:    time.Now().Add(1 * time.Hour),
			},
		},
		ExtraUsage: &provider.ExtraUsage{
			Enabled:     true,
			LimitUSD:    100.0,
			UsedUSD:     42.50,
			Utilization: 0.425,
		},
		FetchedAt: time.Now(),
	}

	got := RenderProviderCard(r, 60)

	if !strings.Contains(got, "$42.50") {
		t.Errorf("expected card to contain spend '$42.50', got:\n%s", got)
	}
	if !strings.Contains(got, "$100.00") {
		t.Errorf("expected card to contain limit '$100.00', got:\n%s", got)
	}
}

func TestRenderProviderCard_errorStatus(t *testing.T) {
	r := provider.QuotaResult{
		Provider:  "claude",
		Status:    "error",
		FetchedAt: time.Now(),
	}

	got := RenderProviderCard(r, 60)

	if !strings.Contains(got, "ERROR") {
		t.Errorf("expected card to contain 'ERROR', got:\n%s", got)
	}
}

func TestRenderProviderCard_copilotUsesFriendlyName(t *testing.T) {
	r := provider.QuotaResult{
		Provider: "copilot",
		Status:   "ok",
		Windows: []provider.Window{{
			Name:        "premium_interactions",
			Utilization: 0.25,
			ResetsAt:    time.Now().Add(2 * time.Hour),
		}},
		FetchedAt: time.Now(),
	}

	got := RenderProviderCard(r, 60)

	if !strings.Contains(got, "Copilot") {
		t.Errorf("expected card to contain provider name 'Copilot', got:\n%s", got)
	}
}

func TestRenderProviderCard_openAICodexWindowsUseSparkDisplayName(t *testing.T) {
	r := provider.QuotaResult{
		Provider: "openai",
		Status:   "ok",
		Windows: []provider.Window{{
			Name:        "codex_bengalfox_five_hour",
			Utilization: 0.35,
			ResetsAt:    time.Now().Add(2 * time.Hour),
		}},
		FetchedAt: time.Now(),
	}

	got := RenderProviderCard(r, 60)

	if !strings.Contains(got, "Codex Spark") {
		t.Fatalf("expected card to contain group header 'Codex Spark', got:\n%s", got)
	}
	if !strings.Contains(got, "Session") {
		t.Fatalf("expected card to contain pretty metric name 'Session', got:\n%s", got)
	}
	if strings.Contains(got, "codex_bengalfox_five_hour") {
		t.Fatalf("expected card not to contain legacy window name, got:\n%s", got)
	}
}

func TestRenderProviderCard_containsPlanAndStatus(t *testing.T) {
	r := provider.QuotaResult{
		Provider: "openai",
		Plan:     "plus",
		Status:   "ok",
		Windows: []provider.Window{{
			Name:        "five_hour",
			Utilization: 0.35,
			ResetsAt:    time.Now().Add(2 * time.Hour),
		}},
		FetchedAt: time.Now(),
	}

	got := RenderProviderCard(r, 60)

	if !strings.Contains(got, "PLUS") {
		t.Errorf("expected card to contain 'PLUS', got:\n%s", got)
	}
	if !strings.Contains(got, "OK") {
		t.Errorf("expected card to contain 'OK', got:\n%s", got)
	}
}

func TestRenderProviderCard_unavailableStatus(t *testing.T) {
	r := provider.QuotaResult{
		Provider:  "gemini",
		Status:    "unavailable",
		FetchedAt: time.Now(),
	}

	got := RenderProviderCard(r, 60)

	if !strings.Contains(got, "UNAVAILABLE") {
		t.Errorf("expected card to contain 'UNAVAILABLE', got:\n%s", got)
	}
}

func TestRenderProviderCard_rendersFriendlyProviderHeader(t *testing.T) {
	r := provider.QuotaResult{
		Provider: "openai",
		Plan:     "plus",
		Status:   "ok",
		Windows: []provider.Window{{
			Name:        "five_hour",
			Utilization: 0.35,
			ResetsAt:    time.Now().Add(2 * time.Hour),
		}},
		FetchedAt: time.Now(),
	}

	got := RenderProviderCard(r, 60)
	plain := ansi.Strip(got)
	normalized := strings.Join(strings.Fields(plain), " ")

	if !strings.Contains(got, "OpenAI") {
		t.Fatalf("expected card to contain friendly provider name 'OpenAI', got:\n%s", got)
	}
	if !strings.Contains(got, "PLUS") {
		t.Fatalf("expected card to contain plan badge 'PLUS', got:\n%s", got)
	}
	if !strings.Contains(got, "OK") {
		t.Fatalf("expected card to contain status badge 'OK', got:\n%s", got)
	}
	if !strings.Contains(normalized, "OpenAI PLUSOK") {
		t.Fatalf("expected stripped header to include spacing before the joined badges, got:\n%s", plain)
	}
}

func TestRenderProviderCard_lowRemainingUsesDangerColor(t *testing.T) {
	r := provider.QuotaResult{
		Provider: "claude",
		Status:   "ok",
		Windows: []provider.Window{{
			Name:        "five_hour",
			Utilization: 0.85,
			ResetsAt:    time.Now().Add(2 * time.Hour),
		}},
		FetchedAt: time.Now(),
	}

	got := RenderProviderCard(r, 60)

	if !strings.Contains(got, "\x1b[38;2;239;68;68m") {
		t.Fatalf("expected low remaining window to use red styling, got:\n%q", got)
	}
}

func TestRenderProviderCard_claudeUsesBrandColor(t *testing.T) {
	r := provider.QuotaResult{
		Provider: "claude",
		Status:   "ok",
		Windows: []provider.Window{{
			Name:        "five_hour",
			Utilization: 0.35,
			ResetsAt:    time.Now().Add(2 * time.Hour),
		}},
		FetchedAt: time.Now(),
	}

	got := RenderProviderCard(r, 60)

	if !strings.Contains(got, "\x1b[38;2;222;115;86m") {
		t.Fatalf("expected Claude card to use brand color #DE7356, got:\n%q", got)
	}
}

func TestRenderProviderCard_openAIUsesWhiteHeadingAndGrayBars(t *testing.T) {
	r := provider.QuotaResult{
		Provider: "openai",
		Status:   "ok",
		Windows: []provider.Window{{
			Name:        "five_hour",
			Utilization: 0.35,
			ResetsAt:    time.Now().Add(2 * time.Hour),
		}},
		FetchedAt: time.Now(),
	}

	got := RenderProviderCard(r, 60)

	if !strings.Contains(got, "\x1b[38;2;255;255;255m") {
		t.Fatalf("expected OpenAI card to use white heading/border styling, got:\n%q", got)
	}
	if !strings.Contains(got, "\x1b[38;2;156;163;175m") {
		t.Fatalf("expected OpenAI card to use gray bar styling, got:\n%q", got)
	}
}

func TestRenderProviderCard_geminiKeepsPurpleTheme(t *testing.T) {
	r := provider.QuotaResult{
		Provider: "gemini",
		Status:   "ok",
		Windows: []provider.Window{{
			Name:        "five_hour",
			Utilization: 0.35,
			ResetsAt:    time.Now().Add(2 * time.Hour),
		}},
		FetchedAt: time.Now(),
	}

	got := RenderProviderCard(r, 60)

	if !strings.Contains(got, "\x1b[38;2;139;92;246m") {
		t.Fatalf("expected Gemini card to keep purple theme, got:\n%q", got)
	}
}

func TestRenderProviderCard_openAILightThemeUsesDarkHeadingAndSoftTrack(t *testing.T) {
	r := provider.QuotaResult{
		Provider: "openai",
		Status:   "ok",
		Windows: []provider.Window{{
			Name:        "five_hour",
			Utilization: 0.35,
			ResetsAt:    time.Now().Add(2 * time.Hour),
		}},
		FetchedAt: time.Now(),
	}

	got := renderProviderCardWithPalette(r, 60, newPalette(false), true)

	if !strings.Contains(got, "15;23;42") {
		t.Fatalf("expected OpenAI light theme to use dark heading color, got:\n%q", got)
	}
	if !strings.Contains(got, "226;232;240") {
		t.Fatalf("expected OpenAI light theme to use a soft gray track, got:\n%q", got)
	}
}

func TestRenderProviderCard_multipleWindows(t *testing.T) {
	r := provider.QuotaResult{
		Provider: "claude",
		Status:   "ok",
		Windows: []provider.Window{
			{
				Name:        "five_hour",
				Utilization: 0.35,
				ResetsAt:    time.Now().Add(2 * time.Hour),
			},
			{
				Name:        "seven_day",
				Utilization: 0.72,
				ResetsAt:    time.Now().Add(48 * time.Hour),
			},
		},
		FetchedAt: time.Now(),
	}

	got := RenderProviderCard(r, 60)

	if !strings.Contains(got, "Session") {
		t.Errorf("expected card to contain pretty name 'Session', got:\n%s", got)
	}
	if !strings.Contains(got, "Weekly Limits") {
		t.Errorf("expected card to contain group header 'Weekly Limits', got:\n%s", got)
	}
	if !strings.Contains(got, "All Models") {
		t.Errorf("expected card to contain pretty name 'All Models', got:\n%s", got)
	}
	if !strings.Contains(got, "35% used") {
		t.Errorf("expected card to contain '35%% used', got:\n%s", got)
	}
	if !strings.Contains(got, "72% used") {
		t.Errorf("expected card to contain '72%% used', got:\n%s", got)
	}
}

func TestWindowDuration_knownWindows(t *testing.T) {
	tests := []struct {
		name string
		want time.Duration
	}{
		{"five_hour", 5 * time.Hour},
		{"seven_day", 7 * 24 * time.Hour},
		{"seven_day_sonnet", 7 * 24 * time.Hour},
		{"seven_day_opus", 7 * 24 * time.Hour},
		{"codex_spark_five_hour", 5 * time.Hour},
		{"codex_spark_seven_day", 7 * 24 * time.Hour},
		{"gemini-2.5-pro", 24 * time.Hour},
		{"gemini-2.5-flash", 24 * time.Hour},
		{"gemini-3.1-pro-preview", 24 * time.Hour},
		{"unknown_window", 0},
		{"chat", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := windowDuration(tt.name)
			if got != tt.want {
				t.Errorf("windowDuration(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestBudgetGuide_midwayThroughSevenDay(t *testing.T) {
	// 3.5 days remaining out of 7 = 50% elapsed
	resetsAt := time.Now().Add(3*24*time.Hour + 12*time.Hour)
	got := budgetGuide("seven_day", resetsAt)
	if got < 0.49 || got > 0.51 {
		t.Errorf("budgetGuide(seven_day, 3.5d remaining) = %.4f, want ~0.50", got)
	}
}

func TestBudgetGuide_earlyInFiveHour(t *testing.T) {
	// 4 hours remaining out of 5 = 20% elapsed
	resetsAt := time.Now().Add(4 * time.Hour)
	got := budgetGuide("five_hour", resetsAt)
	if got < 0.19 || got > 0.21 {
		t.Errorf("budgetGuide(five_hour, 4h remaining) = %.4f, want ~0.20", got)
	}
}

func TestBudgetGuide_expiredWindow(t *testing.T) {
	resetsAt := time.Now().Add(-1 * time.Hour)
	got := budgetGuide("seven_day", resetsAt)
	if got != 1.0 {
		t.Errorf("budgetGuide(expired) = %.4f, want 1.0", got)
	}
}

func TestBudgetGuide_unknownWindow(t *testing.T) {
	got := budgetGuide("chat", time.Now().Add(1*time.Hour))
	if got != -1 {
		t.Errorf("budgetGuide(unknown) = %.4f, want -1", got)
	}
}

func TestRenderProviderCard_showsGuideInSubtitle(t *testing.T) {
	// 2 hours remaining in a 5-hour window = 60% elapsed → guide 60%
	r := provider.QuotaResult{
		Provider: "claude",
		Status:   "ok",
		Windows: []provider.Window{{
			Name:        "five_hour",
			Utilization: 0.35,
			ResetsAt:    time.Now().Add(2 * time.Hour),
		}},
		FetchedAt: time.Now(),
	}

	got := RenderProviderCard(r, 60)
	if !strings.Contains(got, "guide 60%") {
		t.Errorf("expected card to contain 'guide 60%%', got:\n%s", ansi.Strip(got))
	}
}

func TestRenderProviderCard_noGuideForUnknownWindow(t *testing.T) {
	r := provider.QuotaResult{
		Provider: "copilot",
		Status:   "ok",
		Windows: []provider.Window{{
			Name:        "chat",
			Utilization: 0.25,
			ResetsAt:    time.Now().Add(2 * time.Hour),
		}},
		FetchedAt: time.Now(),
	}

	got := RenderProviderCard(r, 60)
	if strings.Contains(got, "guide") {
		t.Errorf("expected no guide for unknown window, got:\n%s", ansi.Strip(got))
	}
}

func TestInjectGuideMarker_placesMarkerAtCorrectPosition(t *testing.T) {
	theme := themeForProvider("claude", newPalette(true))
	bar := newProgressBar(theme)
	bar.SetWidth(40)
	barStr := bar.ViewAs(0.5)

	result := injectGuideMarker(barStr, 10) // cell index for 25% of 40
	// The result should contain the │ character from the guide marker.
	if !strings.Contains(result, "│") {
		t.Errorf("expected guide marker │ in bar, got:\n%q", result)
	}
	// Bar width should be preserved (same visible width).
	origWidth := ansi.StringWidth(barStr)
	resultWidth := ansi.StringWidth(result)
	if resultWidth != origWidth {
		t.Errorf("guide marker changed bar width from %d to %d", origWidth, resultWidth)
	}
}

func TestRenderQuotaBar_noGuideWhenNegative(t *testing.T) {
	theme := themeForProvider("claude", newPalette(true))
	bar := renderQuotaBar(theme, 0.5, 60, -1)
	if strings.Contains(bar, "│") {
		t.Errorf("expected no guide marker when guide=-1, got:\n%q", bar)
	}
}

func TestRenderQuotaBar_guideMarkerPresent(t *testing.T) {
	theme := themeForProvider("claude", newPalette(true))
	bar := renderQuotaBar(theme, 0.5, 60, 0.3)
	if !strings.Contains(bar, "│") {
		t.Errorf("expected guide marker │ when guide=0.3, got:\n%q", bar)
	}
}

func TestRenderQuotaBar_guideUsesBlueColor(t *testing.T) {
	theme := themeForProvider("claude", newPalette(true))
	bar := renderQuotaBar(theme, 0.5, 60, 0.3)
	// #00BFFF = RGB(0, 191, 255) → ANSI true color: 38;2;0;191;255
	if !strings.Contains(bar, "0;191;255") {
		t.Errorf("expected guide marker to use fluorescent blue (#00BFFF), got:\n%q", bar)
	}
}
