package tui

import (
	"strings"
	"testing"
	"time"

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

	if !strings.Contains(got, "claude") {
		t.Errorf("expected card to contain provider name 'claude', got:\n%s", got)
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

	if !strings.Contains(got, "five_hour") {
		t.Errorf("expected card to contain window name 'five_hour', got:\n%s", got)
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

	if !strings.Contains(got, "65% left") {
		t.Errorf("expected card to contain remaining quota '65%% left', got:\n%s", got)
	}
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

	if !strings.Contains(got, "Status: error") {
		t.Errorf("expected card to contain 'Status: error', got:\n%s", got)
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

	if !strings.Contains(got, "Plan: plus") {
		t.Errorf("expected card to contain 'Plan: plus', got:\n%s", got)
	}
	if !strings.Contains(got, "Status: ok") {
		t.Errorf("expected card to contain 'Status: ok', got:\n%s", got)
	}
}

func TestRenderProviderCard_unavailableStatus(t *testing.T) {
	r := provider.QuotaResult{
		Provider:  "gemini",
		Status:    "unavailable",
		FetchedAt: time.Now(),
	}

	got := RenderProviderCard(r, 60)

	if !strings.Contains(got, "Status: unavailable") {
		t.Errorf("expected card to contain 'Status: unavailable', got:\n%s", got)
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

	if !strings.Contains(got, "five_hour") {
		t.Errorf("expected card to contain 'five_hour', got:\n%s", got)
	}
	if !strings.Contains(got, "seven_day") {
		t.Errorf("expected card to contain 'seven_day', got:\n%s", got)
	}
	if !strings.Contains(got, "65% left") {
		t.Errorf("expected card to contain '65%% left', got:\n%s", got)
	}
	if !strings.Contains(got, "28% left") {
		t.Errorf("expected card to contain '28%% left', got:\n%s", got)
	}
	if !strings.Contains(got, "35% used") {
		t.Errorf("expected card to contain '35%% used', got:\n%s", got)
	}
	if !strings.Contains(got, "72% used") {
		t.Errorf("expected card to contain '72%% used', got:\n%s", got)
	}
}
