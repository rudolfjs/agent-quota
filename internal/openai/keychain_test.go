package openai

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
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
	data          []byte
	err           error
}

func (s *testKeychain) Exists(context.Context) (bool, error) {
	s.probes++
	return s.err == nil, s.err
}
func (s *testKeychain) Read(context.Context) ([]byte, error) { s.reads++; return s.data, s.err }

func TestConfiguredCredentialStore(t *testing.T) {
	denied := apierrors.NewAuthError("Grant Keychain access", nil)
	for _, tc := range []struct {
		name, config, wantToken string
		storeErr                error
		wantKind                string
		wantAvailable           bool
		wantProbes, wantReads   int
	}{
		{"absent defaults to file", "", "fixture-file", denied, "", true, 0, 0},
		{"omitted defaults to file", "model = 'example'", "fixture-file", nil, "", true, 0, 0},
		{"file bypasses stale keychain", `cli_auth_credentials_store = "file"`, "fixture-file", nil, "", true, 0, 0},
		{"file bypasses locked keychain", `cli_auth_credentials_store = "file"`, "fixture-file", denied, "", true, 0, 0},
		{"keyring", `cli_auth_credentials_store = 'keyring' # comment`, "fixture-keychain", nil, "", true, 1, 1},
		{"keyring missing ignores file", `cli_auth_credentials_store = "keyring"`, "", credential.ErrNotFound, "auth", false, 1, 1},
		{"keyring locked ignores file", `cli_auth_credentials_store = "keyring"`, "", denied, "auth", true, 1, 1},
		{"auto prefers keychain", `"cli_auth_credentials_store" = "auto"`, "fixture-keychain", nil, "", true, 1, 1},
		{"auto missing uses file", `cli_auth_credentials_store = "auto"`, "fixture-file", credential.ErrNotFound, "", true, 1, 1},
		{"auto locked reports denial", `cli_auth_credentials_store = "auto"`, "", denied, "auth", true, 1, 1},
		{"ephemeral ignores saved credentials", `cli_auth_credentials_store = "ephemeral"`, "", nil, "auth", false, 0, 0},
		{"nested key is not root setting", "[profiles.example]\ncli_auth_credentials_store = 'keyring'", "fixture-file", nil, "", true, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("CODEX_HOME", home)
			if err := os.WriteFile(filepath.Join(home, "auth.json"), []byte(`{"tokens":{"access_token":"fixture-file","refresh_token":"fixture-refresh"}}`), 0o600); err != nil {
				t.Fatal(err)
			}
			if tc.config != "" {
				if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(tc.config), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.wantToken == "" || r.Header.Get("Authorization") != "Bearer "+tc.wantToken {
					t.Error("quota request used the wrong credential store")
				}
				_, _ = w.Write([]byte(`{"plan_type":"plus"}`))
			}))
			defer srv.Close()
			o := New(WithUsageURL(srv.URL))
			store := &testKeychain{data: []byte(`{"tokens":{"access_token":"fixture-keychain","refresh_token":"fixture-refresh"}}`), err: tc.storeErr}
			o.credentials.Keychain = store
			if o.Available() != tc.wantAvailable || store.reads != 0 {
				t.Fatal("incorrect metadata-only discovery for configured backend")
			}
			_, err := o.FetchQuota(t.Context())
			var dom *apierrors.DomainError
			if tc.wantKind == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if !errors.As(err, &dom) || dom.Kind != tc.wantKind {
				t.Fatalf("expected %s error, got %v", tc.wantKind, err)
			}
			if store.probes != tc.wantProbes || store.reads != tc.wantReads {
				t.Fatalf("Keychain probes/reads = %d/%d, want %d/%d", store.probes, store.reads, tc.wantProbes, tc.wantReads)
			}
		})
	}
}

func TestCredentialStoreConfigErrors(t *testing.T) {
	for _, config := range []string{
		`cli_auth_credentials_store = "SENTINEL_SECRET`,
		`cli_auth_credentials_store = ["SENTINEL_SECRET"]`,
		`cli_auth_credentials_store = "SENTINEL_SECRET"`,
		`cli_auth_credentials_store = ""`,
		"unreadable",
	} {
		t.Run(config, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("CODEX_HOME", home)
			path := filepath.Join(home, "config.toml")
			var err error
			if config == "unreadable" {
				err = os.Mkdir(path, 0o700)
			} else {
				err = os.WriteFile(path, []byte(config), 0o600)
			}
			if err != nil {
				t.Fatal(err)
			}
			o := New()
			store := &testKeychain{}
			o.credentials.Keychain = store
			_, err = o.FetchQuota(t.Context())
			var dom *apierrors.DomainError
			if !errors.As(err, &dom) || dom.Kind != "config" || o.Available() || store.reads != 0 || store.probes != 0 {
				t.Fatal("invalid config must fail before accessing credentials")
			}
			for cause := err; cause != nil; cause = errors.Unwrap(cause) {
				if strings.Contains(cause.Error(), "SENTINEL_SECRET") {
					t.Fatal("config contents leaked into error chain")
				}
			}
			// An explicit auth path bypasses even a broken adjacent config file.
			explicit := New(WithAuthPath(filepath.Join(home, "auth.json")))
			if _, err := explicit.credentialSource(t.Context()); err != nil {
				t.Fatal("explicit auth path must bypass Codex settings")
			}
		})
	}
}

func TestCredentialStoreModeReload(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	o := New()
	o.credentials.Keychain = &testKeychain{}
	for _, mode := range []string{"keyring", "file", "auto"} {
		if err := os.WriteFile(o.configPath, fmt.Appendf(nil, "cli_auth_credentials_store = %q", mode), 0o600); err != nil {
			t.Fatal(err)
		}
		source, err := o.credentialSource(t.Context())
		if err != nil || (source.Keychain != nil) != (mode != "file") || (source.Path != "") != (mode != "keyring") {
			t.Fatalf("did not reload storage mode %s", mode)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := o.credentialSource(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("configuration read must honor cancellation")
	}
}

func TestKeychainQuotaAndReadOnlyRefresh(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusUnauthorized} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			store := &testKeychain{data: []byte(`{"tokens":{"access_token":"fixture-access","refresh_token":"fixture-refresh"}}`)}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/usage" {
					t.Error("must not rotate Keychain refresh token")
					w.WriteHeader(500)
					return
				}
				if r.Header.Get("Authorization") != "Bearer fixture-access" {
					t.Error("missing Keychain access token")
				}
				w.WriteHeader(status)
				if status == http.StatusOK {
					_, _ = w.Write([]byte(`{"plan_type":"plus"}`))
				}
			}))
			defer srv.Close()
			path := filepath.Join(t.TempDir(), "auth.json")
			o := New(WithAuthPath(path), WithUsageURL(srv.URL+"/usage"), WithTokenURL(srv.URL+"/token"))
			o.credentials.Keychain = store
			if !o.Available() || store.reads != 0 {
				t.Fatal("discovery must not read secrets")
			}
			result, err := o.FetchQuota(t.Context())
			if status == http.StatusOK {
				if err != nil || result.Plan != "plus" {
					t.Fatalf("quota fetch failed: %v", err)
				}
			} else {
				var dom *apierrors.DomainError
				if !errors.As(err, &dom) || dom.Kind != "auth" || !strings.Contains(dom.Message, "codex login") {
					t.Fatalf("expected re-login guidance: %v", err)
				}
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatal("must not persist Keychain secrets to auth.json")
			}
		})
	}
}

func TestCodexAccountCanonicalPath(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "home")
	link := filepath.Join(dir, "link")
	if err := os.Mkdir(real, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	canonical, err := filepath.EvalSymlinks(real)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(canonical))
	want := fmt.Sprintf("cli|%x", digest[:8])
	if codexAccount(link) != want || codexAccount(real) != want {
		t.Fatal("Codex key must hash canonical home")
	}
	if got := codexAccount("/nonexistent/codex-home"); got != "cli|b5d85b424b15c4d9" {
		t.Fatal("missing home fallback mismatch")
	}
}

func TestExplicitAuthPathDisablesKeychain(t *testing.T) {
	o := New(WithAuthPath(filepath.Join(t.TempDir(), "auth.json")))
	if o.credentials.Keychain != nil {
		t.Fatal("explicit auth path must stay file-only")
	}
}

func TestLiveCredentialRead(t *testing.T) {
	if os.Getenv("AQ_TEST_LIVE_KEYCHAIN") != "1" {
		t.Skip("set AQ_TEST_LIVE_KEYCHAIN=1 for local read-only smoke test")
	}
	o := New()
	if o.credentials.Keychain == nil {
		t.Skip("macOS only")
	}
	source, err := o.credentialSource(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	data, fromKeychain, err := source.Read(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	auth, err := parseAuth(data)
	if err != nil || auth.Tokens.AccessToken == "" {
		t.Fatal("store does not contain valid Codex OAuth credentials")
	}
	t.Logf("credential source: keychain=%t (false means auth.json fallback)", fromKeychain)
}
