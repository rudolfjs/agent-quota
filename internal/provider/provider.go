// Package provider defines the Provider interface and shared domain types
// used across all AI provider implementations.
package provider

import (
	"context"
	"errors"
	"math"
	"time"

	apierrors "github.com/schnetlerr/agent-quota/internal/errors"
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

// BackoffResetter is an optional interface for providers that persist local
// rate-limit cooldown state. Manual refresh and --force use this to clear the
// local cooldown before attempting a real fetch.
type BackoffResetter interface {
	ResetBackoff() error
}

// ForceRetryKey is a context key that signals a fetch was triggered by an
// explicit user action (--force flag or ctrl+r). When present in the context,
// providers should skip re-persisting backoff state on 429 responses, because
// the user explicitly chose to retry despite the rate limit.
type ForceRetryKey struct{}

// QuotaResult is the normalised usage snapshot returned by any provider.
type QuotaResult struct {
	Provider   string        `json:"provider"`
	Status     string        `json:"status"`         // "ok", "error", "unavailable"
	Plan       string        `json:"plan,omitempty"` // e.g. "max", "pro", "free"
	Windows    []Window      `json:"windows"`
	ExtraUsage *ExtraUsage   `json:"extra_usage,omitempty"`
	Error      *ErrorDetails `json:"error,omitempty"`
	FetchedAt  time.Time     `json:"fetched_at"`
}

// ErrorDetails carries safe user-facing metadata for a failed fetch.
type ErrorDetails struct {
	Kind              string `json:"kind,omitempty"`
	Message           string `json:"message,omitempty"`
	StatusCode        int    `json:"status_code,omitempty"`
	RetryAfterSeconds int    `json:"retry_after_seconds,omitempty"`
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

// ErrorResult builds a safe error result for headless output.
func ErrorResult(providerName string, err error, fetchedAt time.Time) QuotaResult {
	result := QuotaResult{
		Provider:  providerName,
		Status:    "error",
		FetchedAt: fetchedAt,
	}
	if err == nil {
		return result
	}

	details := &ErrorDetails{Message: "unexpected error"}
	var domErr *apierrors.DomainError
	if errors.As(err, &domErr) {
		details.Kind = domErr.Kind
		details.Message = domErr.Message
		details.StatusCode = domErr.StatusCode
		if domErr.RetryAfter > 0 {
			details.RetryAfterSeconds = max(1, int(math.Ceil(domErr.RetryAfter.Seconds())))
		}
	}
	result.Error = details
	return result
}
