package config_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/schnetlerr/agent-quota/internal/config"
	"github.com/schnetlerr/agent-quota/internal/provider"
)

func TestLoadSettings_missingFileReturnsEmptySettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings")

	got, err := config.LoadSettings(path)
	if err != nil {
		t.Fatalf("LoadSettings() error = %v", err)
	}
	if len(got.ProviderOrder) != 0 {
		t.Fatalf("ProviderOrder = %v, want empty", got.ProviderOrder)
	}
	if got.TUI.HideHeader {
		t.Fatal("TUI.HideHeader = true, want false")
	}
	if got.TUI.RefreshMinutes != 0 {
		t.Fatalf("TUI.RefreshMinutes = %d, want 0", got.TUI.RefreshMinutes)
	}
}

func TestSaveSettings_roundTripsJSONSettingsFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings")
	want := config.Settings{
		Providers:     []string{"claude", "openai"},
		ProviderOrder: []string{"claude", "openai", "gemini"},
		QuickView:     []string{"claude:five_hour", "gemini:gemini-3-pro-preview"},
		TUI: config.TUISettings{
			HideHeader:     true,
			RefreshMinutes: 10,
		},
	}

	if err := config.SaveSettings(path, want); err != nil {
		t.Fatalf("SaveSettings() error = %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("os.Stat() error = %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("settings file mode = %o, want 600", info.Mode().Perm())
	}

	got, err := config.LoadSettings(path)
	if err != nil {
		t.Fatalf("LoadSettings() error = %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("LoadSettings() = %#v, want %#v", got, want)
	}
}

func TestApplyProviderSelection_filtersProvidersAndPreservesOrder(t *testing.T) {
	providers := []provider.Provider{
		&fakeProvider{name: "claude", available: true},
		&fakeProvider{name: "gemini", available: true},
		&fakeProvider{name: "openai", available: true},
	}

	got := config.ApplyProviderSelection(providers, []string{"openai", "claude"})
	want := []string{"claude", "openai"}
	if names := providerNames(got); !reflect.DeepEqual(names, want) {
		t.Fatalf("provider names = %v, want %v", names, want)
	}
}

func TestApplyProviderOrder_reordersProvidersAndPreservesUnknownTail(t *testing.T) {
	providers := []provider.Provider{
		&fakeProvider{name: "claude", available: true},
		&fakeProvider{name: "gemini", available: true},
		&fakeProvider{name: "openai", available: true},
	}

	got := config.ApplyProviderOrder(providers, []string{"openai", "claude"})
	want := []string{"openai", "claude", "gemini"}
	if names := providerNames(got); !reflect.DeepEqual(names, want) {
		t.Fatalf("provider names = %v, want %v", names, want)
	}
}

func TestSettingsRefreshMinutesValueCanBeAppliedAsDuration(t *testing.T) {
	settings := config.Settings{TUI: config.TUISettings{RefreshMinutes: 15}}

	got := time.Duration(settings.TUI.RefreshMinutes) * time.Minute
	if got != 15*time.Minute {
		t.Fatalf("refresh duration = %v, want %v", got, 15*time.Minute)
	}
}

func TestLoadSettings_clampsRefreshMinutesToMinimum(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(path, []byte(`{"tui":{"refresh_minutes":5}}`), 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}

	got, err := config.LoadSettings(path)
	if err != nil {
		t.Fatalf("LoadSettings() error = %v", err)
	}
	if got.TUI.RefreshMinutes != config.MinimumTUIRefreshMinutes {
		t.Fatalf("TUI.RefreshMinutes = %d, want %d", got.TUI.RefreshMinutes, config.MinimumTUIRefreshMinutes)
	}
}

func TestLoadSettings_normalizesQuickViewMetricIDs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(path, []byte(`{"quick_view":[" Claude : five_hour ","gemini:gemini-3-pro-preview","","GEMINI:gemini-3-pro-preview"]}`), 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}

	got, err := config.LoadSettings(path)
	if err != nil {
		t.Fatalf("LoadSettings() error = %v", err)
	}

	want := []string{"claude:five_hour", "gemini:gemini-3-pro-preview"}
	if !reflect.DeepEqual(got.QuickView, want) {
		t.Fatalf("QuickView = %v, want %v", got.QuickView, want)
	}
}

func TestLoadSettings_dropsRemovedJulesEntries(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	payload := `{"providers":["claude","jules"],"provider_order":["claude","jules","openai"],"quick_view":["claude:five_hour","jules:daily","openai:five_hour"]}`
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}

	got, err := config.LoadSettings(path)
	if err != nil {
		t.Fatalf("LoadSettings() error = %v", err)
	}

	if want := []string{"claude"}; !reflect.DeepEqual(got.Providers, want) {
		t.Fatalf("Providers = %v, want %v", got.Providers, want)
	}
	if want := []string{"claude", "openai"}; !reflect.DeepEqual(got.ProviderOrder, want) {
		t.Fatalf("ProviderOrder = %v, want %v", got.ProviderOrder, want)
	}
	if want := []string{"claude:five_hour", "openai:five_hour"}; !reflect.DeepEqual(got.QuickView, want) {
		t.Fatalf("QuickView = %v, want %v", got.QuickView, want)
	}
}
