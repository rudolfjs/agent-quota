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

	apierrors "github.com/rudolfjs/agent-quota/internal/errors"
)

type testKeychain struct {
	reads int
	data  []byte
}

func (s *testKeychain) Exists(context.Context) (bool, error) { return true, nil }
func (s *testKeychain) Read(context.Context) ([]byte, error) { s.reads++; return s.data, nil }

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
	data, fromKeychain, err := o.credentials.Read(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	auth, err := parseAuth(data)
	if err != nil || auth.Tokens.AccessToken == "" {
		t.Fatal("store does not contain valid Codex OAuth credentials")
	}
	t.Logf("credential source: keychain=%t (false means auth.json fallback)", fromKeychain)
}
