package claude

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type testKeychain struct {
	reads   int
	expired bool
}

func (s *testKeychain) Exists(context.Context) (bool, error) { return true, nil }
func (s *testKeychain) Read(context.Context) ([]byte, error) {
	s.reads++
	expiry := time.Now().Add(time.Hour)
	if s.expired && s.reads == 1 {
		expiry = time.Now().Add(-time.Hour)
	}
	return fmt.Appendf(nil, `{"claudeAiOauth":{"accessToken":"fixture-%d","expiresAt":%d,"subscriptionType":"pro"}}`, s.reads, expiry.UnixMilli()), nil
}

func TestKeychainRefreshRereadsStore(t *testing.T) {
	for _, expired := range []bool{false, true} {
		t.Run(fmt.Sprint("expired=", expired), func(t *testing.T) {
			dir := t.TempDir()
			cli := filepath.Join(dir, "claude")
			marker := filepath.Join(dir, "refreshed")
			script := "#!/bin/sh\nif [ \"$1\" = --version ]; then echo '1.0.0'; exit 0; fi\n: > '" + marker + "'\n"
			if err := os.WriteFile(cli, []byte(script), 0o700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("AGENT_QUOTA_CLAUDE_PATH", cli)
			store := &testKeychain{expired: expired}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") == "Bearer fixture-1" {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				if r.Header.Get("Authorization") != "Bearer fixture-2" {
					t.Error("did not use refreshed Keychain token")
				}
				_, _ = w.Write([]byte(`{"five_hour":{"utilization":0.25}}`))
			}))
			defer srv.Close()
			c := New(WithCredentialsPath(filepath.Join(dir, "absent")), WithBackoffPath(filepath.Join(dir, "backoff")), WithBaseURL(srv.URL))
			c.credentials.Keychain = store
			if !c.Available() || store.reads != 0 {
				t.Fatal("discovery must not read Keychain secrets")
			}
			result, err := c.FetchQuota(t.Context())
			if err != nil || result.Status != "ok" || store.reads != 2 {
				t.Fatalf("Keychain refresh failed: %v", err)
			}
			if _, err := os.Stat(marker); err != nil {
				t.Fatal("refresh CLI was not invoked")
			}
			if _, err := os.Stat(c.credPath); !os.IsNotExist(err) {
				t.Fatal("Keychain credentials must not be written to disk")
			}
		})
	}
}

func TestKeychainIdentity(t *testing.T) {
	t.Setenv("USER", "test-user")
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("CLAUDE_SECURESTORAGE_CONFIG_DIR", "")
	if err := os.Unsetenv("CLAUDE_SECURESTORAGE_CONFIG_DIR"); err != nil {
		t.Fatal(err)
	}
	service, account := keychainIdentity()
	if service != "Claude Code-credentials" || account != "test-user" {
		t.Fatal("incorrect default Claude Keychain identity")
	}
	// The CLI normalizes the configured directory before hashing it.
	t.Setenv("CLAUDE_CONFIG_DIR", "/tmp/cafe\u0301")
	digest := sha256.Sum256([]byte("/tmp/café"))
	service, _ = keychainIdentity()
	if service != fmt.Sprintf("Claude Code-credentials-%x", digest[:4]) {
		t.Fatal("custom Claude directory hash mismatch")
	}
	t.Setenv("CLAUDE_SECURESTORAGE_CONFIG_DIR", "")
	service, _ = keychainIdentity()
	if service != "Claude Code-credentials" {
		t.Fatal("empty secure-storage override must select the default service")
	}
	t.Setenv("CLAUDE_SECURESTORAGE_CONFIG_DIR", "/secure-dir")
	digest = sha256.Sum256([]byte("/secure-dir"))
	service, _ = keychainIdentity()
	if service != fmt.Sprintf("Claude Code-credentials-%x", digest[:4]) {
		t.Fatal("secure-storage override hash mismatch")
	}
}

func TestExplicitCredentialsPathDisablesKeychain(t *testing.T) {
	c := New(WithCredentialsPath(filepath.Join(t.TempDir(), "credentials")), WithBackoffPath(filepath.Join(t.TempDir(), "backoff")))
	if c.credentials.Keychain != nil {
		t.Fatal("explicit path must stay file-only")
	}
}

// Opt-in only: never prompts or touches developer credentials in ordinary CI.
func TestLiveKeychainRead(t *testing.T) {
	if os.Getenv("AQ_TEST_LIVE_KEYCHAIN") != "1" {
		t.Skip("set AQ_TEST_LIVE_KEYCHAIN=1 for local read-only smoke test")
	}
	store := defaultKeychain()
	if store == nil {
		t.Skip("macOS only")
	}
	data, err := store.Read(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	creds, err := parseCredentials(data)
	if err != nil || creds.AccessToken == "" {
		t.Fatal("Keychain entry is not valid Claude OAuth credentials")
	}
	// Never print the credential blob or any token value.
}
