package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	apierrors "github.com/schnetlerr/agent-quota/internal/errors"
	"github.com/schnetlerr/agent-quota/internal/fileutil"
	"github.com/schnetlerr/agent-quota/internal/provider"
)

// TUISettings holds persisted dashboard preferences changed from the TUI.
type TUISettings struct {
	HideHeader     bool `json:"hide_header,omitempty"`
	RefreshMinutes int  `json:"refresh_minutes,omitempty"`
}

// Settings holds persisted interactive dashboard preferences.
type Settings struct {
	Providers     []string    `json:"providers,omitempty"`
	ProviderOrder []string    `json:"provider_order,omitempty"`
	QuickView     []string    `json:"quick_view,omitempty"`
	TUI           TUISettings `json:"tui,omitempty"`
}

// DefaultSettingsPath returns the default path for persisted TUI settings.
func DefaultSettingsPath() (string, error) {
	if dir, err := os.UserConfigDir(); err == nil && dir != "" {
		return filepath.Join(dir, "agent-quota", "settings.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine settings directory: %w", err)
	}
	return filepath.Join(home, ".config", "agent-quota", "settings.json"), nil
}

// LoadSettings reads persisted TUI settings. A missing file is treated as empty settings.
func LoadSettings(path string) (Settings, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return defaultSettings(), nil
		}
		return Settings{}, apierrors.NewConfigError("failed to read agent-quota settings", err)
	}

	settings := defaultSettings()
	if err := json.Unmarshal(data, &settings); err != nil {
		return Settings{}, apierrors.NewConfigError("failed to parse agent-quota settings", err)
	}
	settings.Providers = normalizeProviders(settings.Providers)
	settings.ProviderOrder = normalizeProviders(settings.ProviderOrder)
	settings.QuickView = normalizeQuickViewMetricIDs(settings.QuickView)
	settings.TUI.RefreshMinutes = normalizeOptionalRefreshMinutes(settings.TUI.RefreshMinutes)
	return settings, nil
}

// LoadSettingsDefault loads settings from the default settings path.
func LoadSettingsDefault() (Settings, error) {
	path, err := DefaultSettingsPath()
	if err != nil {
		return defaultSettings(), nil
	}
	return LoadSettings(path)
}

// SaveSettings persists TUI settings to disk using an atomic write.
func SaveSettings(path string, settings Settings) error {
	settings.Providers = normalizeProviders(settings.Providers)
	settings.ProviderOrder = normalizeProviders(settings.ProviderOrder)
	settings.QuickView = normalizeQuickViewMetricIDs(settings.QuickView)
	settings.TUI.RefreshMinutes = normalizeOptionalRefreshMinutes(settings.TUI.RefreshMinutes)

	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return apierrors.NewConfigError("failed to encode agent-quota settings", err)
	}
	data = append(data, '\n')

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return apierrors.NewConfigError("failed to create agent-quota settings directory", err)
	}
	if err := fileutil.AtomicWriteFile(path, data, 0o600); err != nil {
		return apierrors.NewConfigError("failed to persist agent-quota settings", err)
	}
	return nil
}

// normalizeOptionalRefreshMinutes clamps a raw refresh-minutes value to the
// nearest supported option, returning 0 (manual) when minutes is non-positive.
func normalizeOptionalRefreshMinutes(minutes int) int {
	if minutes <= 0 {
		return 0
	}
	return normalizeRefreshMinutes(minutes)
}

// ApplyProviderSelection filters providers using a persisted preferred set.
// The original provider order is preserved. An empty selection means all providers.
func ApplyProviderSelection(providers []provider.Provider, selected []string) []provider.Provider {
	if len(providers) == 0 {
		return nil
	}
	selected = normalizeProviders(selected)
	if len(selected) == 0 {
		return append([]provider.Provider(nil), providers...)
	}

	allowed := make(map[string]struct{}, len(selected))
	for _, name := range selected {
		allowed[name] = struct{}{}
	}

	filtered := make([]provider.Provider, 0, len(providers))
	for _, p := range providers {
		if _, ok := allowed[normalizeProviderName(p.Name())]; ok {
			filtered = append(filtered, p)
		}
	}
	if len(filtered) == 0 {
		return append([]provider.Provider(nil), providers...)
	}
	return filtered
}

// ApplyProviderOrder reorders providers using a persisted preferred order.
// Providers not mentioned in order keep their existing relative order and are
// appended after ordered providers.
func ApplyProviderOrder(providers []provider.Provider, order []string) []provider.Provider {
	if len(providers) == 0 || len(order) == 0 {
		return append([]provider.Provider(nil), providers...)
	}

	indexByName := make(map[string]provider.Provider, len(providers))
	for _, p := range providers {
		indexByName[normalizeProviderName(p.Name())] = p
	}

	ordered := make([]provider.Provider, 0, len(providers))
	used := make(map[string]struct{}, len(providers))
	for _, name := range normalizeProviders(order) {
		p, ok := indexByName[name]
		if !ok {
			continue
		}
		if _, seen := used[name]; seen {
			continue
		}
		ordered = append(ordered, p)
		used[name] = struct{}{}
	}

	for _, p := range providers {
		name := normalizeProviderName(p.Name())
		if _, seen := used[name]; seen {
			continue
		}
		ordered = append(ordered, p)
	}
	return ordered
}

func defaultSettings() Settings {
	return Settings{}
}

func normalizeQuickViewMetricIDs(ids []string) []string {
	seen := make(map[string]struct{}, len(ids))
	result := make([]string, 0, len(ids))
	for _, id := range ids {
		id = normalizeQuickViewMetricID(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	return result
}

func normalizeQuickViewMetricID(id string) string {
	id = strings.TrimSpace(strings.ToLower(id))
	if id == "" {
		return ""
	}
	parts := strings.SplitN(id, ":", 2)
	if len(parts) != 2 {
		return ""
	}
	providerName := normalizeProviderName(parts[0])
	metricName := strings.TrimSpace(parts[1])
	if providerName == "" || metricName == "" || isRemovedProviderName(providerName) {
		return ""
	}
	return providerName + ":" + metricName
}
