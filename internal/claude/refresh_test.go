package claude_test

import (
	"context"
	"errors"
	"testing"

	"github.com/rudolfjs/agent-quota/internal/claude"
	apierrors "github.com/rudolfjs/agent-quota/internal/errors"
)

func TestRefreshToken_execFailure_returnsAuthError(t *testing.T) {
	// Never invoke a developer's real CLI or change their credentials.
	dir := t.TempDir()
	t.Setenv("AGENT_QUOTA_CLAUDE_PATH", dir+"/missing-claude")
	credPath := dir + "/nonexistent-credentials.json"

	err := claude.RefreshToken(t.Context(), credPath)
	if err == nil {
		t.Fatal("expected exec failure")
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
	t.Setenv("AGENT_QUOTA_CLAUDE_PATH", "/bin/sh")
	ctx, cancel := context.WithCancel(t.Context())
	cancel() // cancel immediately

	err := claude.RefreshToken(ctx, t.TempDir()+"/creds.json")
	if err == nil {
		t.Fatal("expected cancellation")
	}

	var domErr *apierrors.DomainError
	if !errors.As(err, &domErr) {
		t.Fatalf("expected *DomainError, got %T: %v", err, err)
	}
}
