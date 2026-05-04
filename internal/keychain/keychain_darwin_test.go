//go:build darwin

package keychain

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// shellQuote wraps a string in single quotes for embedding in a shell script.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// itoa converts an int to its decimal string representation.
func itoa(n int) string {
	return strconv.Itoa(n)
}

// writeFakeSecurity writes a shell shim that prints stdout, stderr, and
// exits with the given status. Returns its absolute path.
//
// NOTE: These tests are darwin-only and run on macos-latest CI.
// The implementing agent is on Linux and cannot execute them locally.
func writeFakeSecurity(t *testing.T, stdout, stderr string, exitCode int) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "security")
	body := fmt.Sprintf("#!/bin/sh\nprintf %%s %s\nprintf %%s %s 1>&2\nexit %s\n",
		shellQuote(stdout), shellQuote(stderr), itoa(exitCode))
	if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRead_Darwin_HappyPath(t *testing.T) {
	fake := writeFakeSecurity(t, "tok-12345\n", "", 0)
	got, err := New(WithSecurityPath(fake)).Read(t.Context(),
		"Claude Code-credentials", "user@example.com")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != "tok-12345" { // trailing newline must be trimmed
		t.Fatalf("got %q, want %q", got, "tok-12345")
	}
}

func TestRead_Darwin_ItemNotFound(t *testing.T) {
	// security(1) exits 44 with "The specified item could not be found in the keychain."
	fake := writeFakeSecurity(t, "",
		"security: SecKeychainSearchCopyNext: The specified item could not be found in the keychain.\n",
		44)
	_, err := New(WithSecurityPath(fake)).Read(t.Context(), "x", "y")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestRead_Darwin_AccessDenied(t *testing.T) {
	// exit 51 + "User interaction is not allowed" path.
	fake := writeFakeSecurity(t, "",
		"security: SecKeychainSearchCopyNext: User interaction is not allowed.\n",
		51)
	_, err := New(WithSecurityPath(fake)).Read(t.Context(), "x", "y")
	if !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("want ErrAccessDenied, got %v", err)
	}
}

func TestRead_Darwin_UserCancelled(t *testing.T) {
	fake := writeFakeSecurity(t, "",
		"security: SecKeychainSearchCopyNext: User cancelled the operation.\n",
		51)
	_, err := New(WithSecurityPath(fake)).Read(t.Context(), "x", "y")
	if !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("want ErrAccessDenied, got %v", err)
	}
}

func TestRead_Darwin_NeverLeaksToken(t *testing.T) {
	// Even if security somehow embeds the token in stderr, our error must not.
	fake := writeFakeSecurity(t, "tok-secret\n",
		"garbage stderr that mentions tok-secret\n", 0)
	got, err := New(WithSecurityPath(fake)).Read(t.Context(), "x", "y")
	if err != nil {
		t.Fatal(err)
	}
	if got != "tok-secret" {
		t.Fatalf("trim mismatch: got %q", got)
	}
}

func TestRead_Darwin_ExitCodeFallback_44(t *testing.T) {
	// When stderr doesn't contain the expected string but exit code is 44.
	fake := writeFakeSecurity(t, "", "some other error\n", 44)
	_, err := New(WithSecurityPath(fake)).Read(t.Context(), "x", "y")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound from exit code 44, got %v", err)
	}
}

func TestRead_Darwin_ExitCodeFallback_51(t *testing.T) {
	// When stderr doesn't contain the expected string but exit code is 51.
	fake := writeFakeSecurity(t, "", "some other error\n", 51)
	_, err := New(WithSecurityPath(fake)).Read(t.Context(), "x", "y")
	if !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("want ErrAccessDenied from exit code 51, got %v", err)
	}
}

func TestRead_Darwin_UnknownError(t *testing.T) {
	fake := writeFakeSecurity(t, "", "unknown problem\n", 99)
	_, err := New(WithSecurityPath(fake)).Read(t.Context(), "x", "y")
	if err == nil {
		t.Fatal("expected error")
	}
	// Must not be one of the typed sentinels
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrAccessDenied) {
		t.Fatalf("unexpected sentinel error: %v", err)
	}
}
