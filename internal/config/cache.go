package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	apierrors "github.com/rudolfjs/agent-quota/internal/errors"
	"github.com/rudolfjs/agent-quota/internal/fileutil"
	"github.com/rudolfjs/agent-quota/internal/provider"
)

type quotaCacheFile struct {
	Results []provider.QuotaResult `json:"results"`
}

// DefaultQuotaCachePath returns the default path for persisted quota snapshots.
func DefaultQuotaCachePath() (string, error) {
	if dir, err := os.UserConfigDir(); err == nil && dir != "" {
		return filepath.Join(dir, "agent-quota", "quota-cache.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine quota cache directory: %w", err)
	}
	return filepath.Join(home, ".config", "agent-quota", "quota-cache.json"), nil
}

// LoadQuotaCache reads persisted successful quota snapshots. A missing file is treated as empty cache.
func LoadQuotaCache(path string) (map[string]provider.QuotaResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]provider.QuotaResult{}, nil
		}
		return nil, apierrors.NewConfigError("failed to read quota cache", err)
	}

	var file quotaCacheFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, apierrors.NewConfigError("failed to parse quota cache", err)
	}

	results := make(map[string]provider.QuotaResult, len(file.Results))
	for _, result := range file.Results {
		name := normalizeProviderName(result.Provider)
		if name == "" || result.Status != "ok" {
			continue
		}
		result.Provider = name
		results[name] = result
	}
	return results, nil
}

// SaveQuotaCache persists the last successful quota snapshots to disk using an atomic write.
func SaveQuotaCache(path string, results map[string]provider.QuotaResult) error {
	providerNames := make([]string, 0, len(results))
	for name, result := range results {
		name = normalizeProviderName(name)
		if name == "" || result.Status != "ok" {
			continue
		}
		providerNames = append(providerNames, name)
	}
	slices.Sort(providerNames)

	file := quotaCacheFile{Results: make([]provider.QuotaResult, 0, len(providerNames))}
	for _, name := range providerNames {
		result := results[name]
		result.Provider = name
		file.Results = append(file.Results, result)
	}

	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return apierrors.NewConfigError("failed to encode quota cache", err)
	}
	data = append(data, '\n')

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return apierrors.NewConfigError("failed to create quota cache directory", err)
	}
	if err := fileutil.AtomicWriteFile(path, data, 0o600); err != nil {
		return apierrors.NewConfigError("failed to persist quota cache", err)
	}
	return nil
}
