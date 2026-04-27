package claude_test

// Security regression tests for the Claude provider.
// These tests codify critical security properties so they cannot regress.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rudolfjs/agent-quota/internal/claude"
	apierrors "github.com/rudolfjs/agent-quota/internal/errors"
)

// TestFetchUsage_oversizedResponse_returnsError verifies that the 1 MiB response
// body limit is enforced. A malicious or misbehaving server cannot exhaust
// process memory by sending an arbitrarily large response.
func TestFetchUsage_oversizedResponse_returnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		// Send > 1 MiB of valid-looking JSON that exceeds the read limit.
		// The decoder will fail mid-parse once the limit is reached.
		padding := strings.Repeat("x", (1<<20)+128)
		_, _ = fmt.Fprintf(w, `{"overflow":"%s"}`, padding)
	}))
	defer srv.Close()

	client := claude.NewAPIClient(srv.URL, &http.Client{})
	_, err := client.FetchUsage(t.Context(), "tok_test")
	if err == nil {
		t.Fatal("expected error for oversized response, got nil")
	}
}

// TestFetchQuota_resultExcludesCredentials verifies that QuotaResult does not
// contain the access token or refresh token from the credentials file. This
// prevents a accidental credential leak through JSON output or logs.
func TestFetchQuota_resultExcludesCredentials(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(sampleUsageResponse)
	}))
	defer srv.Close()

	dir := t.TempDir()
	// Use distinct sentinel values in the credential file so we can
	// detect them if they accidentally end up in the result.
	payload := map[string]any{
		"claudeAiOauth": map[string]any{
			"accessToken":      "SENTINEL_ACCESS_TOKEN",
			"refreshToken":     "SENTINEL_REFRESH_TOKEN",
			"expiresAt":        time.Now().Add(time.Hour).UnixMilli(),
			"scopes":           []string{"api"},
			"subscriptionType": "max",
		},
	}
	data, _ := json.Marshal(payload)
	credPath := filepath.Join(dir, ".credentials.json")
	if err := os.WriteFile(credPath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	c := claude.New(
		claude.WithCredentialsPath(credPath),
		claude.WithBackoffPath(dir+"/backoff.json"),
		claude.WithHTTPClient(&http.Client{}),
		claude.WithBaseURL(srv.URL),
	)
	result, err := c.FetchQuota(t.Context())
	if err != nil {
		t.Fatalf("FetchQuota: %v", err)
	}

	resultJSON, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	resultStr := string(resultJSON)

	if strings.Contains(resultStr, "SENTINEL_ACCESS_TOKEN") {
		t.Error("QuotaResult JSON contains access token — credential leak detected")
	}
	if strings.Contains(resultStr, "SENTINEL_REFRESH_TOKEN") {
		t.Error("QuotaResult JSON contains refresh token — credential leak detected")
	}
}

// TestFetchQuota_allErrorPaths_returnDomainError verifies that every error path
// in FetchQuota returns a *DomainError. This ensures raw internal errors (with
// file paths, URLs, or other sensitive details) never escape to callers.
func TestFetchQuota_allErrorPaths_returnDomainError(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		setup   func(t *testing.T) string // returns credPath
	}{
		{
			name: "http_500",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			setup: func(t *testing.T) string {
				t.Helper()
				dir := t.TempDir()
				return writeCredFile(t, dir, validCredsPayload(time.Now().Add(time.Hour)))
			},
		},
		{
			name: "malformed_json",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{bad json`))
			},
			setup: func(t *testing.T) string {
				t.Helper()
				dir := t.TempDir()
				return writeCredFile(t, dir, validCredsPayload(time.Now().Add(time.Hour)))
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(tc.handler)
			defer srv.Close()

			c := claude.New(
				claude.WithCredentialsPath(tc.setup(t)),
				claude.WithHTTPClient(&http.Client{}),
				claude.WithBaseURL(srv.URL),
			)
			_, err := c.FetchQuota(t.Context())
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

// TestReadCredentials_warnOnInsecurePermissions verifies that a warning is
// emitted when a credential file is world-readable. This mirrors SSH behavior.
func TestReadCredentials_warnOnInsecurePermissions(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".credentials.json")
	data, _ := json.Marshal(map[string]any{
		"claudeAiOauth": map[string]any{
			"accessToken": "tok",
			"expiresAt":   time.Now().Add(time.Hour).UnixMilli(),
		},
	})
	if err := os.WriteFile(p, data, 0o644 /* world-readable */); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(old) })

	_, _ = claude.ReadCredentials(p)

	if !strings.Contains(buf.String(), "insecure permissions") {
		t.Errorf("expected insecure-permissions warning in log output, got: %s", buf.String())
	}
}
