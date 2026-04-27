package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rudolfjs/agent-quota/internal/config"
	apierrors "github.com/rudolfjs/agent-quota/internal/errors"
	"github.com/rudolfjs/agent-quota/internal/provider"
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

	got, err := resolveTUIRefreshInterval(cfg, config.Settings{}, 0, false)
	if err != nil {
		t.Fatalf("resolveTUIRefreshInterval() error = %v", err)
	}
	if got != 12*time.Minute {
		t.Fatalf("resolveTUIRefreshInterval() = %v, want %v", got, 12*time.Minute)
	}
}

func TestResolveTUIRefreshInterval_usesFlagOverrideWhenSet(t *testing.T) {
	cfg := config.Config{TUI: config.TUIConfig{RefreshMinutes: 12}}

	got, err := resolveTUIRefreshInterval(cfg, config.Settings{}, 5, true)
	if err != nil {
		t.Fatalf("resolveTUIRefreshInterval() error = %v", err)
	}
	if got != 5*time.Minute {
		t.Fatalf("resolveTUIRefreshInterval() = %v, want %v", got, 5*time.Minute)
	}
}

func TestResolveTUIRefreshInterval_rejectsTooSmallFlagOverride(t *testing.T) {
	cfg := config.Config{TUI: config.TUIConfig{RefreshMinutes: 12}}

	_, err := resolveTUIRefreshInterval(cfg, config.Settings{}, 2, true)
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

func TestNewRegistry_doesNotRegisterJules(t *testing.T) {
	reg := newRegistry()

	if _, ok := reg.Get("jules"); ok {
		t.Fatal("registry unexpectedly contains jules provider")
	}

	for _, name := range []string{"claude", "openai", "gemini", "copilot"} {
		if _, ok := reg.Get(name); !ok {
			t.Fatalf("registry missing provider %q", name)
		}
	}
}

type fetchProvider struct {
	name       string
	available  bool
	result     provider.QuotaResult
	err        error
	called     bool
	resetCalls int
	resetErr   error
}

func (p *fetchProvider) Name() string    { return p.name }
func (p *fetchProvider) Available() bool { return p.available }
func (p *fetchProvider) FetchQuota(_ context.Context) (provider.QuotaResult, error) {
	p.called = true
	if p.err != nil {
		return provider.QuotaResult{}, p.err
	}
	return p.result, nil
}
func (p *fetchProvider) ResetBackoff() error {
	p.resetCalls++
	return p.resetErr
}

func TestFetchResults_forceResetsProviderBackoffBeforeFetching(t *testing.T) {
	providers := []provider.Provider{
		&fetchProvider{
			name:      "claude",
			available: true,
			result:    provider.QuotaResult{Provider: "claude", Status: "ok"},
		},
	}

	results, err := fetchResults(t.Context(), providers, true)
	if err != nil {
		t.Fatalf("fetchResults() error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}

	p := providers[0].(*fetchProvider)
	if p.resetCalls != 1 {
		t.Fatalf("resetCalls = %d, want 1", p.resetCalls)
	}
	if !p.called {
		t.Fatal("expected FetchQuota() to be called after reset")
	}
}

func TestFetchResults_forceReturnsResetErrors(t *testing.T) {
	providers := []provider.Provider{
		&fetchProvider{
			name:      "claude",
			available: true,
			resetErr:  apierrors.NewConfigError("failed to clear Claude rate-limit backoff state", errors.New("permission denied")),
		},
	}

	results, err := fetchResults(t.Context(), providers, true)
	if err != nil {
		t.Fatalf("fetchResults() error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if results[0].Status != "error" {
		t.Fatalf("results[0].Status = %q, want %q", results[0].Status, "error")
	}
	if results[0].Error == nil {
		t.Fatal("results[0].Error should be populated")
	}
	if results[0].Error.Message != "failed to clear Claude rate-limit backoff state" {
		t.Fatalf("results[0].Error.Message = %q, want safe reset error", results[0].Error.Message)
	}
	p := providers[0].(*fetchProvider)
	if p.called {
		t.Fatal("FetchQuota() should not be called when reset fails")
	}
}
