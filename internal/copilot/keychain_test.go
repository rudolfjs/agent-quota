package copilot_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/rudolfjs/agent-quota/internal/copilot"
	"github.com/rudolfjs/agent-quota/internal/keychain"
)

type fakeKeychain struct {
	token string
	err   error
}

func (f fakeKeychain) Read(_ context.Context, _, _ string) (string, error) {
	return f.token, f.err
}

func TestCopilot_Available_keychainFallback(t *testing.T) {
	t.Setenv("COPILOT_GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")

	// Missing config file + keychain returns a token → Available
	p := copilot.New(
		copilot.WithConfigPath(filepath.Join(t.TempDir(), "config.json")),
		copilot.WithKeychainFallback(fakeKeychain{token: "gh_tok_keychain"}),
	)
	if !p.Available() {
		t.Fatal("Available() = false, want true when keychain fallback provides token")
	}
}

func TestCopilot_Available_keychainFallback_NotFound(t *testing.T) {
	t.Setenv("COPILOT_GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")

	p := copilot.New(
		copilot.WithConfigPath(filepath.Join(t.TempDir(), "config.json")),
		copilot.WithKeychainFallback(fakeKeychain{err: keychain.ErrNotFound}),
	)
	if p.Available() {
		t.Fatal("Available() = true, want false when keychain returns ErrNotFound")
	}
}

func TestCopilot_FetchQuota_keychainFallback_success(t *testing.T) {
	t.Setenv("COPILOT_GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")

	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"copilot_plan":"pro"}`))
	}))
	defer srv.Close()

	p := copilot.New(
		copilot.WithConfigPath(filepath.Join(t.TempDir(), "config.json")),
		copilot.WithKeychainFallback(fakeKeychain{token: "gh_tok_keychain"}),
		copilot.WithHTTPClient(http.DefaultClient),
		copilot.WithBaseURL(srv.URL),
	)

	_, err := p.FetchQuota(t.Context())
	if err != nil {
		t.Fatalf("FetchQuota: %v", err)
	}
	if gotAuth != "Bearer gh_tok_keychain" {
		t.Fatalf("Authorization = %q, want %q", gotAuth, "Bearer gh_tok_keychain")
	}
}
