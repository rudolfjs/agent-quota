// Package errors provides domain error types for agent-quota.
// These types separate safe user-facing messages from raw internal causes,
// ensuring sensitive details never cross trust boundaries.
package errors

import "time"

// DomainError wraps an internal error with a safe user-facing message.
// The Error() method returns only Message — if this error is accidentally
// serialized to output, only the safe message is exposed.
type DomainError struct {
	Kind       string        // "auth", "network", "api", "config"
	Message    string        // safe for user display
	Cause      error         // original error — log internally, never show to user
	StatusCode int           // HTTP status code, if applicable (0 means not set)
	RetryAfter time.Duration // server-provided backoff hint, if applicable
}

// Error returns only the safe user-facing message.
func (e *DomainError) Error() string { return e.Message }

// Unwrap allows errors.Is / errors.As to traverse the error chain.
func (e *DomainError) Unwrap() error { return e.Cause }

// NewAuthError creates a domain error for authentication failures.
// Use when credentials are missing, invalid, or expired.
func NewAuthError(msg string, cause error) *DomainError {
	return &DomainError{Kind: "auth", Message: msg, Cause: cause}
}

// NewNetworkError creates a domain error for network-level failures.
// Use when HTTP requests fail due to connectivity or timeout.
func NewNetworkError(msg string, cause error) *DomainError {
	return &DomainError{Kind: "network", Message: msg, Cause: cause}
}

// NewAPIError creates a domain error for API-level failures.
// Use when the provider returns an unexpected status code or malformed response.
func NewAPIError(msg string, cause error) *DomainError {
	return &DomainError{Kind: "api", Message: msg, Cause: cause}
}

// NewConfigError creates a domain error for configuration failures.
// Use when a required file is missing, unreadable, or malformed.
func NewConfigError(msg string, cause error) *DomainError {
	return &DomainError{Kind: "config", Message: msg, Cause: cause}
}
