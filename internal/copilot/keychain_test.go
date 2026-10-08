package copilot

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rudolfjs/agent-quota/internal/credential"
	apierrors "github.com/rudolfjs/agent-quota/internal/errors"
)

type testKeychain struct {
	reads, probes int
	err           error
}

func (s *testKeychain) Exists(context.Context) (bool, error) {
	s.probes++
	return s.err == nil, s.err
}
func (s *testKeychain) Read(context.Context) ([]byte, error) {
	s.reads++
	return []byte("fixture-keychain-token\n"), s.err
}

func setupKeychainProvider(t *testing.T, config string, store *testKeychain) *Copilot {
	t.Helper()
	for _, key := range []string{"COPILOT_GITHUB_TOKEN", "GH_TOKEN", "GITHUB_TOKEN"} {
		t.Setenv(key, "")
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	c := New(WithConfigPath(path))
	c.keychainFor = func(host, login string) credential.Store {
		if host != "https://github.com" || login != "test-user" {
			t.Error("unexpected Copilot Keychain account")
		}
		return store
	}
	return c
}

const keychainConfig = `{"last_logged_in_user":{"host":"https://github.com","login":"test-user"},"copilot_tokens":{"https://github.com:test-user":"fixture-file-token"}}`

func TestCopilotKeychainQuota(t *testing.T) {
	store := &testKeychain{}
	c := setupKeychainProvider(t, keychainConfig, store)
	if !c.Available() || store.reads != 0 {
		t.Fatal("availability must not request a Keychain password")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture-keychain-token" {
			t.Error("expected Keychain token")
		}
		_, _ = w.Write([]byte(`{"copilot_plan":"individual"}`))
	}))
	defer srv.Close()
	c.baseURL = srv.URL
	result, err := c.FetchQuota(t.Context())
	if err != nil || result.Plan != "individual" {
		t.Fatalf("Copilot Keychain quota failed: %v", err)
	}
}

func TestCopilotKeychainPrecedence(t *testing.T) {
	denied := apierrors.NewAuthError("Grant Keychain access", nil)
	for _, tc := range []struct {
		name, env, config, want string
		storeErr, wantErr       error
	}{
		{"environment wins", "fixture-env-token", keychainConfig, "fixture-env-token", denied, nil},
		{"missing uses file", "", keychainConfig, "fixture-file-token", credential.ErrNotFound, nil},
		{"denied does not fall back", "", keychainConfig, "", denied, denied},
		{"legacy plaintext preference", "", `{"store_token_plaintext":true,` + keychainConfig[1:], "fixture-file-token", denied, nil},
		{"logged in users", "", `{"logged_in_users":[{"host":"https://github.com","login":"test-user"}]}`, "fixture-keychain-token", nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &testKeychain{err: tc.storeErr}
			c := setupKeychainProvider(t, tc.config, store)
			t.Setenv("COPILOT_GITHUB_TOKEN", tc.env)
			token, _, err := c.resolveToken(t.Context(), false)
			if token != tc.want || !errors.Is(err, tc.wantErr) {
				t.Fatal("unexpected Copilot credential selection")
			}
		})
	}
}

func TestCopilotPlaintextSettings(t *testing.T) {
	legacyConfig := `{"store_token_plaintext":true,` + keychainConfig[1:]
	denied := apierrors.NewAuthError("Grant Keychain access", nil)
	for _, tc := range []struct {
		name, config, settings string
		storeErr               error
		wantPlaintext          bool
	}{
		{"bypasses denied keychain", keychainConfig, `{"storeTokenPlaintext":true}`, denied, true},
		{"bypasses stale keychain", keychainConfig, `{"storeTokenPlaintext":true}`, nil, true},
		{"comments and trailing comma", keychainConfig, "{\n// preference\n\"storeTokenPlaintext\":true, /* keep tokens in config.json */\n}", denied, true},
		{"false overrides legacy", legacyConfig, `{"storeTokenPlaintext":false}`, nil, false},
		{"default overrides legacy", legacyConfig, `{}`, nil, false},
		{"absent uses legacy", legacyConfig, "", denied, true},
		{"absent defaults to keychain", keychainConfig, "", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &testKeychain{err: tc.storeErr}
			c := setupKeychainProvider(t, tc.config, store)
			if tc.settings != "" {
				path := filepath.Join(filepath.Dir(c.configPath), "settings.json")
				if err := os.WriteFile(path, []byte(tc.settings), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if !c.Available() || store.reads != 0 {
				t.Fatal("discovery must succeed without reading Keychain passwords")
			}
			want := "fixture-keychain-token"
			if tc.wantPlaintext {
				want = "fixture-file-token"
			}
			token, host, err := c.resolveToken(t.Context(), false)
			if err != nil || token != want || host != "https://github.com" {
				t.Fatal("settings selected the wrong credential source")
			}
			if tc.wantPlaintext && (store.reads != 0 || store.probes != 0) {
				t.Fatal("plaintext preference must bypass all Keychain access")
			}
		})
	}
}

func TestCopilotSettingsErrors(t *testing.T) {
	for _, tc := range []struct {
		name, settings string
		unreadable     bool
	}{
		{"malformed", `{"storeTokenPlaintext":true,"SENTINEL_SETTINGS_SECRET":`, false},
		{"invalid type", `{"storeTokenPlaintext":"SENTINEL_SETTINGS_SECRET"}`, false},
		{"unreadable", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &testKeychain{}
			c := setupKeychainProvider(t, keychainConfig, store)
			path := filepath.Join(filepath.Dir(c.configPath), "settings.json")
			var err error
			if tc.unreadable {
				err = os.Mkdir(path, 0o700)
			} else {
				err = os.WriteFile(path, []byte(tc.settings), 0o600)
			}
			if err != nil {
				t.Fatal(err)
			}
			_, err = c.FetchQuota(t.Context())
			var dom *apierrors.DomainError
			if !errors.As(err, &dom) || dom.Kind != "config" || store.reads != 0 {
				t.Fatal("invalid settings must return a config error before reading Keychain")
			}
			for e := err; e != nil; e = errors.Unwrap(e) {
				if strings.Contains(e.Error(), "SENTINEL_SETTINGS_SECRET") {
					t.Fatal("settings contents leaked into error chain")
				}
			}
			t.Setenv("COPILOT_GITHUB_TOKEN", "fixture-env-token")
			token, _, err := c.resolveToken(t.Context(), false)
			if err != nil || token != "fixture-env-token" {
				t.Fatal("environment token must take precedence over settings errors")
			}
		})
	}
}

func TestCopilotDeniedIsVisible(t *testing.T) {
	denied := apierrors.NewAuthError("Grant Keychain access", nil)
	c := setupKeychainProvider(t, keychainConfig, &testKeychain{err: denied})
	if !c.Available() {
		t.Fatal("denied access must surface as an error, not disappear")
	}
	if _, err := c.FetchQuota(t.Context()); !errors.Is(err, denied) {
		t.Fatal("Keychain auth error was hidden")
	}
}

func TestExplicitConfigPathDisablesKeychain(t *testing.T) {
	c := New(WithConfigPath(filepath.Join(t.TempDir(), "config.json")))
	if c.keychainFor != nil {
		t.Fatal("explicit config path must stay file-only")
	}
}
