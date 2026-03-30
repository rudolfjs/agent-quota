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
		ProviderOrder: []string{"claude", "openai", "gemini"},
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
