package main

import (
	"errors"
	"testing"
	"time"

	"github.com/schnetlerr/agent-quota/internal/config"
	apierrors "github.com/schnetlerr/agent-quota/internal/errors"
	"github.com/schnetlerr/agent-quota/internal/provider"
)

func TestFilterByModel_keepsMatchingWindows(t *testing.T) {
	results := []provider.QuotaResult{{
		Provider: "gemini",
		Windows: []provider.Window{
			{Name: "gemini-3-flash-preview"},
			{Name: "gemini-3-pro"},
		},
	}}
	got := filterByModel(results, "gemini-3-flash-preview")
	if len(got[0].Windows) != 1 {
		t.Fatalf("len(windows) = %d, want 1", len(got[0].Windows))
	}
	if got[0].Windows[0].Name != "gemini-3-flash-preview" {
		t.Errorf("window name = %q, want %q", got[0].Windows[0].Name, "gemini-3-flash-preview")
	}
}

func TestFilterByModel_emptySliceWhenNoMatch(t *testing.T) {
	results := []provider.QuotaResult{{
		Provider: "gemini",
		Windows:  []provider.Window{{Name: "gemini-3-pro"}},
	}}
	got := filterByModel(results, "gemini-3-flash-preview")
	if got[0].Windows == nil {
		t.Error("Windows should be empty slice, not nil")
	}
	if len(got[0].Windows) != 0 {
		t.Errorf("len(windows) = %d, want 0", len(got[0].Windows))
	}
}

func TestFilterByModel_noopWhenModelEmpty(t *testing.T) {
	results := []provider.QuotaResult{{
		Provider: "gemini",
		Windows:  []provider.Window{{Name: "gemini-3-pro"}, {Name: "gemini-3-flash"}},
	}}
	got := filterByModel(results, "")
	if len(got[0].Windows) != 2 {
		t.Errorf("len(windows) = %d, want 2 (no filter when model is empty)", len(got[0].Windows))
	}
}

func TestFilterByModel_filtersAcrossMultipleProviders(t *testing.T) {
	results := []provider.QuotaResult{
		{
			Provider: "gemini",
			Windows:  []provider.Window{{Name: "gemini-3-flash-preview"}, {Name: "gemini-3-pro"}},
		},
		{
			Provider: "openai",
			Windows:  []provider.Window{{Name: "gpt-4o"}, {Name: "o3"}},
		},
	}
	got := filterByModel(results, "gemini-3-flash-preview")
	if len(got[0].Windows) != 1 || got[0].Windows[0].Name != "gemini-3-flash-preview" {
		t.Errorf("gemini: unexpected windows %v", got[0].Windows)
	}
	if len(got[1].Windows) != 0 {
		t.Errorf("openai: expected 0 windows, got %d", len(got[1].Windows))
	}
}

func TestResolveTUIRefreshInterval_usesConfigWhenFlagNotSet(t *testing.T) {
	cfg := config.Config{TUI: config.TUIConfig{RefreshMinutes: 12}}

	got, err := resolveTUIRefreshInterval(cfg, 0, false)
	if err != nil {
		t.Fatalf("resolveTUIRefreshInterval() error = %v", err)
	}
	if got != 12*time.Minute {
		t.Fatalf("resolveTUIRefreshInterval() = %v, want %v", got, 12*time.Minute)
	}
}

func TestResolveTUIRefreshInterval_usesFlagOverrideWhenSet(t *testing.T) {
	cfg := config.Config{TUI: config.TUIConfig{RefreshMinutes: 12}}

	got, err := resolveTUIRefreshInterval(cfg, 2, true)
	if err != nil {
		t.Fatalf("resolveTUIRefreshInterval() error = %v", err)
	}
	if got != 2*time.Minute {
		t.Fatalf("resolveTUIRefreshInterval() = %v, want %v", got, 2*time.Minute)
	}
}

func TestResolveTUIRefreshInterval_rejectsNonPositiveFlagOverride(t *testing.T) {
	cfg := config.Config{TUI: config.TUIConfig{RefreshMinutes: 12}}

	_, err := resolveTUIRefreshInterval(cfg, 0, true)
	if err == nil {
		t.Fatal("resolveTUIRefreshInterval() error = nil, want error")
	}

	var domErr *apierrors.DomainError
	if !errors.As(err, &domErr) {
		t.Fatalf("error type = %T, want DomainError", err)
	}
	if domErr.Kind != "config" {
		t.Fatalf("Kind = %q, want %q", domErr.Kind, "config")
	}
}
