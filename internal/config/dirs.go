package config

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
)

const appDirName = "agent-quota"

// primaryDir is where agent-quota writes its config and cache files.
// Prefers the OS config dir; falls back to ~/.config.
func primaryDir() (string, error) {
	if dir, err := os.UserConfigDir(); err == nil && dir != "" {
		return filepath.Join(dir, appDirName), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", appDirName), nil
}

// Dirs returns every directory agent-quota may have written to: the primary
// config dir first, then the ~/.config fallback when it differs.
func Dirs() ([]string, error) {
	primary, err := primaryDir()
	if err != nil {
		return nil, fmt.Errorf("cannot determine config directory: %w", err)
	}
	dirs := []string{primary}
	if home, err := os.UserHomeDir(); err == nil {
		if fallback := filepath.Join(home, ".config", appDirName); !slices.Contains(dirs, fallback) {
			dirs = append(dirs, fallback)
		}
	}
	return dirs, nil
}
