package tui

// Security regression tests for the TUI rendering layer.
// These tests are in package tui (not tui_test) to access unexported methods.

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	apierrors "github.com/schnetlerr/agent-quota/internal/errors"
	"github.com/schnetlerr/agent-quota/internal/provider"
)

// TestBodyContent_rawError_doesNotLeakInternals verifies that when a provider
// returns a raw (non-DomainError) error, the TUI renders a generic message
// rather than the raw error string. This prevents internal details (file paths,
// URLs, token fragments) from reaching the terminal.
func TestBodyContent_rawError_doesNotLeakInternals(t *testing.T) {
	sentinel := "secret_credential_fragment_xyz"
	rawErr := fmt.Errorf("internal detail: %s", sentinel)

	p := &stubProvider{name: "test"}
	m := New([]provider.Provider{p})
	m.errors["test"] = rawErr

	content := m.bodyContent()

	if strings.Contains(content, sentinel) {
		t.Errorf("bodyContent() leaked internal error detail %q to terminal output", sentinel)
	}
	if !strings.Contains(content, "unexpected error") {
		t.Errorf("bodyContent() should render generic message for raw error, got:\n%s", content)
	}
}

// TestBodyContent_domainError_rendersSafeMessage verifies that a DomainError's
// safe user-facing message IS rendered (not hidden) while the internal cause is
// not exposed.
func TestBodyContent_domainError_rendersSafeMessage(t *testing.T) {
	internalCause := "raw_cause_should_not_appear"
	safeMsg := "Claude token is expired"

	domErr := apierrors.NewAuthError(safeMsg, errors.New(internalCause))

	p := &stubProvider{name: "test"}
	m := New([]provider.Provider{p})
	m.errors["test"] = domErr

	content := m.bodyContent()

	if strings.Contains(content, internalCause) {
		t.Errorf("bodyContent() exposed internal cause %q", internalCause)
	}
	if !strings.Contains(content, safeMsg) {
		t.Errorf("bodyContent() should render DomainError safe message %q, got:\n%s", safeMsg, content)
	}
}
