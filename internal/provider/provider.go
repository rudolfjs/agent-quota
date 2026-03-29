// Package provider defines the Provider interface and shared domain types
// used across all AI provider implementations.
package provider

import (
	"context"
	"time"
)

// Provider fetches quota and usage data from an AI provider.
// Implementations live in internal/<name>/ and are registered in main.go.
type Provider interface {
	// Name returns the provider's identifier (e.g., "claude", "openai", "gemini").
	Name() string

	// FetchQuota retrieves current usage and quota information.
	// Returns a domain error (never a raw error) on failure.
	FetchQuota(ctx context.Context) (QuotaResult, error)

	// Available reports whether this provider's credentials are configured
	// on the current machine. Returns false if credentials are missing.
	Available() bool
}

// QuotaResult is the normalised usage snapshot returned by any provider.
type QuotaResult struct {
	Provider   string      `json:"provider"`
	Status     string      `json:"status"`         // "ok", "error", "unavailable"
	Plan       string      `json:"plan,omitempty"` // e.g. "max", "pro", "free"
	Windows    []Window    `json:"windows"`
	ExtraUsage *ExtraUsage `json:"extra_usage,omitempty"`
	FetchedAt  time.Time   `json:"fetched_at"`
}

// Window represents a single rate-limit window (e.g. 5-hour, 7-day).
type Window struct {
	Name        string    `json:"name"`
	Utilization float64   `json:"utilization"` // 0.0–1.0
	ResetsAt    time.Time `json:"resets_at"`
}

// IsValid reports whether the window's utilization is in the valid range [0, 1].
func (w Window) IsValid() bool {
	return w.Utilization >= 0.0 && w.Utilization <= 1.0
}

// ExtraUsage represents pay-as-you-go / overage billing.
type ExtraUsage struct {
	Enabled     bool    `json:"enabled"`
	LimitUSD    float64 `json:"limit_usd"`
	UsedUSD     float64 `json:"used_usd"`
	Utilization float64 `json:"utilization"` // 0.0–1.0
}
