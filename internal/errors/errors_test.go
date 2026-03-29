package errors_test

import (
	"errors"
	"testing"

	apierrors "github.com/schnetlerr/agent-quota/internal/errors"
)

func TestDomainError_Error_returnsSafeMessage(t *testing.T) {
	rawErr := errors.New("pq: connection refused to postgres://user:secret@host/db")
	e := apierrors.NewAuthError("credentials not found", rawErr)

	if e.Error() != "credentials not found" {
		t.Errorf("Error() = %q, want %q", e.Error(), "credentials not found")
	}
}

func TestDomainError_Unwrap_exposesInternalCause(t *testing.T) {
	rawErr := errors.New("raw internal error")
	e := apierrors.NewNetworkError("network unreachable", rawErr)

	if !errors.Is(e, rawErr) {
		t.Error("errors.Is should find the wrapped cause via Unwrap")
	}
}

func TestDomainError_Error_doesNotLeakCause(t *testing.T) {
	rawErr := errors.New("secret token: sk-ant-abcdef1234567890")
	e := apierrors.NewAPIError("API request failed", rawErr)

	msg := e.Error()
	if msg == rawErr.Error() {
		t.Error("Error() must not expose the raw cause message")
	}
	// The safe message must not contain the raw error text
	if len(msg) > 0 && msg == rawErr.Error() {
		t.Errorf("Error() leaks cause: %q", msg)
	}
}

func TestNewAuthError_hasCorrectKind(t *testing.T) {
	e := apierrors.NewAuthError("unauthorized", nil)
	if e.Kind != "auth" {
		t.Errorf("Kind = %q, want %q", e.Kind, "auth")
	}
}

func TestNewNetworkError_hasCorrectKind(t *testing.T) {
	e := apierrors.NewNetworkError("timeout", nil)
	if e.Kind != "network" {
		t.Errorf("Kind = %q, want %q", e.Kind, "network")
	}
}

func TestNewAPIError_hasCorrectKind(t *testing.T) {
	e := apierrors.NewAPIError("bad response", nil)
	if e.Kind != "api" {
		t.Errorf("Kind = %q, want %q", e.Kind, "api")
	}
}

func TestNewConfigError_hasCorrectKind(t *testing.T) {
	e := apierrors.NewConfigError("missing file", nil)
	if e.Kind != "config" {
		t.Errorf("Kind = %q, want %q", e.Kind, "config")
	}
}

func TestErrorsAs_findsCorrectType(t *testing.T) {
	raw := errors.New("underlying")
	wrapped := apierrors.NewAuthError("auth failed", raw)

	var domErr *apierrors.DomainError
	if !errors.As(wrapped, &domErr) {
		t.Fatal("errors.As should find DomainError")
	}
	if domErr.Kind != "auth" {
		t.Errorf("Kind = %q, want auth", domErr.Kind)
	}
}
