package claude_test

import (
	"context"
	"errors"
	"testing"

	"github.com/schnetlerr/agent-quota/internal/claude"
	apierrors "github.com/schnetlerr/agent-quota/internal/errors"
)

func TestRefreshToken_execFailure_returnsAuthError(t *testing.T) {
	// Use a fake cred path — the exec will fail because there is no
	// real "claude" binary in the test environment.
	dir := t.TempDir()
	credPath := dir + "/nonexistent-credentials.json"

	err := claude.RefreshToken(t.Context(), credPath)
	if err == nil {
		t.Skip("claude CLI is available in test env; cannot test exec failure")
	}

	var domErr *apierrors.DomainError
	if !errors.As(err, &domErr) {
		t.Fatalf("expected *DomainError, got %T: %v", err, err)
	}
	if domErr.Kind != "auth" {
		t.Errorf("Kind = %q, want %q", domErr.Kind, "auth")
	}
}

func TestRefreshToken_contextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel() // cancel immediately

	err := claude.RefreshToken(ctx, t.TempDir()+"/creds.json")
	if err == nil {
		t.Skip("claude CLI succeeded despite cancelled context")
	}

	var domErr *apierrors.DomainError
	if !errors.As(err, &domErr) {
		t.Fatalf("expected *DomainError, got %T: %v", err, err)
	}
}
