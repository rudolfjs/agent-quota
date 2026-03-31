package copilot_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/schnetlerr/agent-quota/internal/copilot"
	apierrors "github.com/schnetlerr/agent-quota/internal/errors"
)

func validCopilotConfigPayload() map[string]any {
	return map[string]any{
		"last_logged_in_user": map[string]any{
			"host":  "https://github.com",
			"login": "octocat",
		},
		"copilot_tokens": map[string]any{
			"https://github.com:octocat": "tok_valid",
		},
	}
}

// TestFetchQuota_resultExcludesCredentials verifies that the token read from
// the config file does not appear in the serialised QuotaResult, preventing
// accidental credential leaks through JSON output or logs.
func TestFetchQuota_resultExcludesCredentials(t *testing.T) {
	t.Setenv("COPILOT_GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"copilot_plan":         "business",
			"quota_reset_date_utc": "2026-04-01T00:00:00Z",
			"quota_snapshots": map[string]any{
				"chat": map[string]any{"entitlement": 300, "percent_remaining": 75},
			},
		})
	}))
	defer srv.Close()

	dir := t.TempDir()
	configPath := writeConfigFile(t, dir, map[string]any{
		"last_logged_in_user": map[string]any{
			"host":  "https://github.com",
			"login": "octocat",
		},
		"copilot_tokens": map[string]any{
			"https://github.com:octocat": "SENTINEL_COPILOT_TOKEN",
		},
	})

	p := copilot.New(
		copilot.WithConfigPath(configPath),
		copilot.WithHTTPClient(&http.Client{}),
		copilot.WithBaseURL(srv.URL),
	)
	result, err := p.FetchQuota(t.Context())
	if err != nil {
		t.Fatalf("FetchQuota: %v", err)
	}

	resultJSON, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(resultJSON), "SENTINEL_COPILOT_TOKEN") {
		t.Error("QuotaResult JSON contains copilot token — credential leak detected")
	}
}

// TestFetchQuota_resultExcludesEnvCredentials verifies that a token sourced
// from the COPILOT_GITHUB_TOKEN environment variable does not appear in the
// serialised QuotaResult.
func TestFetchQuota_resultExcludesEnvCredentials(t *testing.T) {
	t.Setenv("COPILOT_GITHUB_TOKEN", "SENTINEL_ENV_TOKEN")
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"copilot_plan":         "individual",
			"quota_reset_date_utc": "2026-04-01T00:00:00Z",
		})
	}))
	defer srv.Close()

	p := copilot.New(
		copilot.WithHTTPClient(&http.Client{}),
		copilot.WithBaseURL(srv.URL),
	)
	result, err := p.FetchQuota(t.Context())
	if err != nil {
		t.Fatalf("FetchQuota: %v", err)
	}

	resultJSON, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(resultJSON), "SENTINEL_ENV_TOKEN") {
		t.Error("QuotaResult JSON contains env token — credential leak detected")
	}
}

func TestFetchQuota_oversizedResponse_returnsError(t *testing.T) {
	t.Setenv("COPILOT_GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		padding := strings.Repeat("x", (1<<20)+128)
		_, _ = fmt.Fprintf(w, `{"overflow":"%s"}`, padding)
	}))
	defer srv.Close()

	dir := t.TempDir()
	configPath := writeConfigFile(t, dir, validCopilotConfigPayload())
	p := copilot.New(
		copilot.WithConfigPath(configPath),
		copilot.WithHTTPClient(&http.Client{}),
		copilot.WithBaseURL(srv.URL),
	)
	_, err := p.FetchQuota(t.Context())
	if err == nil {
		t.Fatal("expected error for oversized response, got nil")
	}
}

func TestFetchQuota_allErrorPaths_returnDomainError(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{
			name: "http_500",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
		},
		{
			name: "malformed_json",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{bad`))
			},
		},
		{
			name: "http_401",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("COPILOT_GITHUB_TOKEN", "")
			t.Setenv("GH_TOKEN", "")
			t.Setenv("GITHUB_TOKEN", "")
			srv := httptest.NewServer(tc.handler)
			defer srv.Close()

			dir := t.TempDir()
			configPath := writeConfigFile(t, dir, validCopilotConfigPayload())
			p := copilot.New(
				copilot.WithConfigPath(configPath),
				copilot.WithHTTPClient(&http.Client{}),
				copilot.WithBaseURL(srv.URL),
			)
			_, err := p.FetchQuota(t.Context())
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			var domErr *apierrors.DomainError
			if !errors.As(err, &domErr) {
				t.Errorf("expected *DomainError, got %T: %v", err, err)
			}
		})
	}
}
