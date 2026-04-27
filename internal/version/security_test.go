package version_test

// Security regression tests for the version package.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rudolfjs/agent-quota/internal/version"
)

// TestResolveClaudeBinary_envOverride verifies that AGENT_QUOTA_CLAUDE_PATH
// takes precedence over PATH lookup. This lets users pin the binary path in
// restricted environments and is the mechanism by which PATH hijacking can
// be mitigated.
func TestResolveClaudeBinary_envOverride(t *testing.T) {
	// Create a fake executable in a temp dir.
	dir := t.TempDir()
	fakePath := filepath.Join(dir, "claude")
	if err := os.WriteFile(fakePath, []byte("#!/bin/sh\necho fake"), 0o700); err != nil {
		t.Fatal(err)
	}

	t.Setenv("AGENT_QUOTA_CLAUDE_PATH", fakePath)

	resolved, err := version.ResolveClaudeBinary()
	if err != nil {
		t.Fatalf("ResolveClaudeBinary() error = %v", err)
	}
	if resolved != fakePath {
		t.Errorf("ResolveClaudeBinary() = %q, want %q", resolved, fakePath)
	}
}

// TestResolveClaudeBinary_missingBinaryReturnsError verifies that an error is
// returned when the claude binary is not present on PATH and no env override
// is set. Callers can use this to fail fast with a clear message rather than
// executing a potentially hijacked binary.
func TestResolveClaudeBinary_missingBinaryReturnsError(t *testing.T) {
	// Override PATH to an empty dir so no claude binary can be found.
	t.Setenv("PATH", t.TempDir())
	t.Setenv("AGENT_QUOTA_CLAUDE_PATH", "")

	_, err := version.ResolveClaudeBinary()
	if err == nil {
		t.Error("expected error when claude binary is not on PATH, got nil")
	}
}
