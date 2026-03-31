package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	apierrors "github.com/schnetlerr/agent-quota/internal/errors"
	"github.com/schnetlerr/agent-quota/internal/fileutil"
	"github.com/schnetlerr/agent-quota/internal/provider"
)

const (
	MinimumTUIRefreshMinutes = 5
	DefaultTUIRefreshMinutes = 15
)

// TUIConfig holds dashboard-specific settings.
type TUIConfig struct {
	RefreshMinutes int `json:"refresh_minutes"`
}

// Config holds user-configurable CLI settings.
type Config struct {
	Providers []string  `json:"providers"`
	TUI       TUIConfig `json:"tui"`
}

// DefaultPath returns the default provider-selection config file path.
// Prefers XDG config dir; falls back to ~/.config. Returns an error only if
// neither os.UserConfigDir nor os.UserHomeDir can be determined.
func DefaultPath() (string, error) {
	if dir, err := os.UserConfigDir(); err == nil && dir != "" {
		return filepath.Join(dir, "agent-quota", "providers.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine config directory: %w", err)
	}
	return filepath.Join(home, ".config", "agent-quota", "providers.json"), nil
}

// Load reads the config file. A missing file is treated as an empty config.
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return defaultConfig(), nil
		}
		return Config{}, apierrors.NewConfigError("failed to read agent-quota config", err)
	}

	cfg := defaultConfig()
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, apierrors.NewConfigError("failed to parse agent-quota config", err)
	}
	cfg.Providers = normalizeProviders(cfg.Providers)
	cfg.TUI.RefreshMinutes = normalizeRefreshMinutes(cfg.TUI.RefreshMinutes)
	return cfg, nil
}

// LoadDefault loads the config from the default path.
func LoadDefault() (Config, error) {
	path, err := DefaultPath()
	if err != nil {
		// If we cannot find the home directory, treat as empty config rather than
		// failing — a missing config file is non-fatal, and this preserves that behavior.
		return defaultConfig(), nil
	}
	return Load(path)
}

// TUIRefreshInterval returns the configured dashboard auto-refresh interval.
func (c Config) TUIRefreshInterval() time.Duration {
	return time.Duration(normalizeRefreshMinutes(c.TUI.RefreshMinutes)) * time.Minute
}

// SelectProviders resolves the provider scope for a command invocation.
// An explicit provider name overrides config. Configured provider order is preserved.
func SelectProviders(reg *provider.Registry, providerName string, cfg Config) ([]provider.Provider, error) {
	providerName = normalizeProviderName(providerName)
	if providerName != "" {
		p, ok := reg.Get(providerName)
		if !ok {
			return nil, apierrors.NewConfigError("unknown provider: "+providerName, fmt.Errorf("provider %q is not registered", providerName))
		}
		return []provider.Provider{p}, nil
	}

	if len(cfg.Providers) == 0 {
		return reg.Available(), nil
	}

	selected := make([]provider.Provider, 0, len(cfg.Providers))
	for _, name := range cfg.Providers {
		p, ok := reg.Get(name)
		if !ok {
			return nil, apierrors.NewConfigError("agent-quota config references an unknown provider", fmt.Errorf("provider %q is not registered", name))
		}
		selected = append(selected, p)
	}
	return selected, nil
}

func normalizeProviders(names []string) []string {
	seen := make(map[string]struct{}, len(names))
	result := make([]string, 0, len(names))
	for _, name := range names {
		name = normalizeProviderName(name)
		if name == "" || isRemovedProviderName(name) {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		result = append(result, name)
	}
	return result
}

func normalizeProviderName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func isRemovedProviderName(name string) bool {
	switch normalizeProviderName(name) {
	case "jules":
		return true
	default:
		return false
	}
}

// Save persists provider-selection config to disk using an atomic write.
func Save(path string, cfg Config) error {
	cfg.Providers = normalizeProviders(cfg.Providers)
	cfg.TUI.RefreshMinutes = normalizeRefreshMinutes(cfg.TUI.RefreshMinutes)

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return apierrors.NewConfigError("failed to encode agent-quota config", err)
	}
	data = append(data, '\n')

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return apierrors.NewConfigError("failed to create agent-quota config directory", err)
	}
	if err := fileutil.AtomicWriteFile(path, data, 0o600); err != nil {
		return apierrors.NewConfigError("failed to persist agent-quota config", err)
	}
	return nil
}

func defaultConfig() Config {
	return Config{
		TUI: TUIConfig{RefreshMinutes: DefaultTUIRefreshMinutes},
	}
}

func NormalizeTUIRefreshMinutes(minutes int) int {
	return normalizeRefreshMinutes(minutes)
}

func normalizeRefreshMinutes(minutes int) int {
	if minutes <= 0 {
		return DefaultTUIRefreshMinutes
	}
	if minutes < MinimumTUIRefreshMinutes {
		return MinimumTUIRefreshMinutes
	}
	return minutes
}
