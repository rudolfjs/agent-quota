package config_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/rudolfjs/agent-quota/internal/config"
	apierrors "github.com/rudolfjs/agent-quota/internal/errors"
	"github.com/rudolfjs/agent-quota/internal/provider"
)

type fakeProvider struct {
	name      string
	available bool
}

func (f *fakeProvider) Name() string    { return f.name }
func (f *fakeProvider) Available() bool { return f.available }
func (f *fakeProvider) FetchQuota(_ context.Context) (provider.QuotaResult, error) {
	return provider.QuotaResult{Provider: f.name, Status: "ok"}, nil
}

func TestLoad_missingFileReturnsEmptyConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.Providers) != 0 {
		t.Fatalf("Providers = %v, want empty", cfg.Providers)
	}
	if cfg.TUI.RefreshMinutes != config.DefaultTUIRefreshMinutes {
		t.Fatalf("TUI.RefreshMinutes = %d, want %d", cfg.TUI.RefreshMinutes, config.DefaultTUIRefreshMinutes)
	}
	if cfg.TUIRefreshInterval() != 15*time.Minute {
		t.Fatalf("TUIRefreshInterval() = %v, want %v", cfg.TUIRefreshInterval(), 15*time.Minute)
	}
}

func TestLoad_normalizesProviders(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"providers":[" Claude ","openai","","OPENAI","gemini"]}`), 0o600); err != nil {
		t.Fatalf("os.WriteFile: %v", err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	want := []string{"claude", "openai", "gemini"}
	if !reflect.DeepEqual(cfg.Providers, want) {
		t.Fatalf("Providers = %v, want %v", cfg.Providers, want)
	}
}

func TestLoad_dropsRemovedJulesProviderFromConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"providers":["claude","jules","openai"]}`), 0o600); err != nil {
		t.Fatalf("os.WriteFile: %v", err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	want := []string{"claude", "openai"}
	if !reflect.DeepEqual(cfg.Providers, want) {
		t.Fatalf("Providers = %v, want %v", cfg.Providers, want)
	}
}

func TestLoad_normalizesTUIRefreshMinutes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"tui":{"refresh_minutes":15}}`), 0o600); err != nil {
		t.Fatalf("os.WriteFile: %v", err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.TUI.RefreshMinutes != 15 {
		t.Fatalf("TUI.RefreshMinutes = %d, want 15", cfg.TUI.RefreshMinutes)
	}
	if cfg.TUIRefreshInterval() != 15*time.Minute {
		t.Fatalf("TUIRefreshInterval() = %v, want %v", cfg.TUIRefreshInterval(), 15*time.Minute)
	}
}

func TestLoad_nonPositiveTUIRefreshMinutesUseDefault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"tui":{"refresh_minutes":0}}`), 0o600); err != nil {
		t.Fatalf("os.WriteFile: %v", err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.TUI.RefreshMinutes != config.DefaultTUIRefreshMinutes {
		t.Fatalf("TUI.RefreshMinutes = %d, want %d", cfg.TUI.RefreshMinutes, config.DefaultTUIRefreshMinutes)
	}
}

func TestLoad_clampsTUIRefreshMinutesToMinimum(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"tui":{"refresh_minutes":5}}`), 0o600); err != nil {
		t.Fatalf("os.WriteFile: %v", err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.TUI.RefreshMinutes != config.MinimumTUIRefreshMinutes {
		t.Fatalf("TUI.RefreshMinutes = %d, want %d", cfg.TUI.RefreshMinutes, config.MinimumTUIRefreshMinutes)
	}
}

func TestLoad_invalidJSONReturnsConfigError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"providers":[`), 0o600); err != nil {
		t.Fatalf("os.WriteFile: %v", err)
	}

	_, err := config.Load(path)
	if err == nil {
		t.Fatal("Load() error = nil, want error")
	}

	var domErr *apierrors.DomainError
	if !errors.As(err, &domErr) {
		t.Fatalf("error type = %T, want DomainError", err)
	}
	if domErr.Kind != "config" {
		t.Fatalf("Kind = %q, want %q", domErr.Kind, "config")
	}
}

func TestSelectProviders_usesConfigOrder(t *testing.T) {
	reg := provider.NewRegistry()
	reg.Register(&fakeProvider{name: "claude", available: true})
	reg.Register(&fakeProvider{name: "openai", available: false})
	reg.Register(&fakeProvider{name: "gemini", available: true})

	got, err := config.SelectProviders(reg, "", config.Config{Providers: []string{"openai", "claude"}})
	if err != nil {
		t.Fatalf("SelectProviders() error = %v", err)
	}

	want := []string{"openai", "claude"}
	if names := providerNames(got); !reflect.DeepEqual(names, want) {
		t.Fatalf("provider names = %v, want %v", names, want)
	}
}

func TestSelectProviders_explicitProviderOverridesConfig(t *testing.T) {
	reg := provider.NewRegistry()
	reg.Register(&fakeProvider{name: "claude", available: true})
	reg.Register(&fakeProvider{name: "openai", available: true})

	got, err := config.SelectProviders(reg, "openai", config.Config{Providers: []string{"claude"}})
	if err != nil {
		t.Fatalf("SelectProviders() error = %v", err)
	}

	want := []string{"openai"}
	if names := providerNames(got); !reflect.DeepEqual(names, want) {
		t.Fatalf("provider names = %v, want %v", names, want)
	}
}

func TestSelectProviders_unknownConfiguredProviderReturnsConfigError(t *testing.T) {
	reg := provider.NewRegistry()
	reg.Register(&fakeProvider{name: "claude", available: true})

	_, err := config.SelectProviders(reg, "", config.Config{Providers: []string{"bogus"}})
	if err == nil {
		t.Fatal("SelectProviders() error = nil, want error")
	}

	var domErr *apierrors.DomainError
	if !errors.As(err, &domErr) {
		t.Fatalf("error type = %T, want DomainError", err)
	}
	if domErr.Kind != "config" {
		t.Fatalf("Kind = %q, want %q", domErr.Kind, "config")
	}
}

func providerNames(providers []provider.Provider) []string {
	names := make([]string, len(providers))
	for i, p := range providers {
		names[i] = p.Name()
	}
	return names
}
