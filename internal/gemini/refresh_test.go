package gemini_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	apierrors "github.com/rudolfjs/agent-quota/internal/errors"
	"github.com/rudolfjs/agent-quota/internal/gemini"
)

func TestRefreshToken_missingBinary_returnsAuthError(t *testing.T) {
	// Point to a nonexistent binary so LookPath fails.
	t.Setenv("AGENT_QUOTA_GEMINI_PATH", "/nonexistent/gemini-binary")
	t.Setenv("PATH", t.TempDir()) // empty PATH to prevent fallback

	err := gemini.RefreshToken(t.Context(), filepath.Join(t.TempDir(), "creds.json"))
	if err == nil {
		t.Skip("gemini CLI is available in test env; cannot test exec failure")
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

	err := gemini.RefreshToken(ctx, filepath.Join(t.TempDir(), "creds.json"))
	if err == nil {
		t.Skip("gemini CLI succeeded despite cancelled context")
	}

	var domErr *apierrors.DomainError
	if !errors.As(err, &domErr) {
		t.Fatalf("expected *DomainError, got %T: %v", err, err)
	}
}

func TestRefreshToken_envOverride(t *testing.T) {
	dir := t.TempDir()
	credPath := filepath.Join(dir, "oauth_creds.json")

	// Create a fake gemini script that writes new creds.
	fakeGemini := filepath.Join(dir, "fake-gemini")
	script := `#!/bin/sh
printf '{"access_token":"refreshed"}' > ` + credPath + "\n"
	if err := os.WriteFile(fakeGemini, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake gemini script: %v", err)
	}
	t.Setenv("AGENT_QUOTA_GEMINI_PATH", fakeGemini)

	// Create initial creds file so the mtime check works.
	if err := os.WriteFile(credPath, []byte(`{"access_token":"old"}`), 0o600); err != nil {
		t.Fatalf("write initial creds: %v", err)
	}

	if err := gemini.RefreshToken(t.Context(), credPath); err != nil {
		t.Fatalf("RefreshToken() error = %v", err)
	}

	data, err := os.ReadFile(credPath)
	if err != nil {
		t.Fatalf("read creds after refresh: %v", err)
	}
	if got := string(data); got != `{"access_token":"refreshed"}` {
		t.Errorf("creds after refresh = %q, want refreshed token", got)
	}
}
