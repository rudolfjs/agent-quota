package config_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/schnetlerr/agent-quota/internal/config"
	"github.com/schnetlerr/agent-quota/internal/provider"
)

func TestLoadQuotaCache_missingFileReturnsEmptyResults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "quota-cache.json")

	got, err := config.LoadQuotaCache(path)
	if err != nil {
		t.Fatalf("LoadQuotaCache() error = %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("LoadQuotaCache() = %v, want empty map", got)
	}
}

func TestSaveQuotaCache_roundTripsSuccessfulResultsOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "quota-cache.json")
	reset := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)

	results := map[string]provider.QuotaResult{
		"claude": {
			Provider: "claude",
			Status:   "ok",
			Plan:     "max",
			Windows: []provider.Window{{
				Name:        "five_hour",
				Utilization: 0.35,
				ResetsAt:    reset,
			}},
			FetchedAt: reset,
		},
		"openai": {
			Provider:  "openai",
			Status:    "error",
			FetchedAt: reset,
		},
	}

	if err := config.SaveQuotaCache(path, results); err != nil {
		t.Fatalf("SaveQuotaCache() error = %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("os.Stat() error = %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("cache file mode = %o, want 600", info.Mode().Perm())
	}

	got, err := config.LoadQuotaCache(path)
	if err != nil {
		t.Fatalf("LoadQuotaCache() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(LoadQuotaCache()) = %d, want 1", len(got))
	}

	claude, ok := got["claude"]
	if !ok {
		t.Fatalf("LoadQuotaCache() missing claude result: %v", got)
	}
	if claude.Status != "ok" {
		t.Fatalf("claude status = %q, want ok", claude.Status)
	}
	if claude.Plan != "max" {
		t.Fatalf("claude plan = %q, want max", claude.Plan)
	}
	if len(claude.Windows) != 1 {
		t.Fatalf("len(claude.Windows) = %d, want 1", len(claude.Windows))
	}
	if _, ok := got["openai"]; ok {
		t.Fatalf("LoadQuotaCache() unexpectedly included non-ok result: %v", got)
	}
}
