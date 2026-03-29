package main

import (
	"errors"
	"testing"
	"time"

	"github.com/schnetlerr/agent-quota/internal/config"
	apierrors "github.com/schnetlerr/agent-quota/internal/errors"
)

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
