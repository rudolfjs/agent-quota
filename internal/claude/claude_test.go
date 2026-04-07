package claude_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/schnetlerr/agent-quota/internal/claude"
	apierrors "github.com/schnetlerr/agent-quota/internal/errors"
)

func TestClaude_Name(t *testing.T) {
	c := claude.New()
	if got := c.Name(); got != "claude" {
		t.Errorf("Name() = %q, want %q", got, "claude")
	}
}

func TestClaude_Available_missingCreds(t *testing.T) {
	c := claude.New(claude.WithCredentialsPath("/nonexistent/.credentials.json"))
	if c.Available() {
		t.Error("Available() should be false when credentials file is missing")
	}
}

func TestClaude_Available_validCreds(t *testing.T) {
	dir := t.TempDir()
	credPath := writeCredFile(t, dir, validCredsPayload(time.Now().Add(time.Hour)))
	c := claude.New(claude.WithCredentialsPath(credPath))
	if !c.Available() {
		t.Error("Available() should be true when credentials file exists and is valid")
	}
}

func TestClaude_FetchQuota_success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(sampleUsageResponse)
	}))
	defer srv.Close()

	dir := t.TempDir()
	credPath := writeCredFile(t, dir, validCredsPayload(time.Now().Add(time.Hour)))

	c := claude.New(
		claude.WithCredentialsPath(credPath),
		claude.WithHTTPClient(http.DefaultClient),
		claude.WithBaseURL(srv.URL),
	)

	result, err := c.FetchQuota(t.Context())
	if err != nil {
		t.Fatalf("FetchQuota: %v", err)
	}

	if result.Provider != "claude" {
		t.Errorf("Provider = %q, want %q", result.Provider, "claude")
	}
	if result.Status != "ok" {
		t.Errorf("Status = %q, want %q", result.Status, "ok")
	}
	if result.Plan != "max" {
		t.Errorf("Plan = %q, want %q", result.Plan, "max")
	}
	if len(result.Windows) != 3 {
		t.Fatalf("len(Windows) = %d, want 3", len(result.Windows))
	}

	// Verify window names
	wantNames := []string{"five_hour", "seven_day", "seven_day_sonnet"}
	for i, wn := range wantNames {
		if result.Windows[i].Name != wn {
			t.Errorf("Windows[%d].Name = %q, want %q", i, result.Windows[i].Name, wn)
		}
	}

	// Verify five_hour utilization
	if result.Windows[0].Utilization != 0.35 {
		t.Errorf("Windows[0].Utilization = %f, want 0.35", result.Windows[0].Utilization)
	}

	// Verify ResetsAt was parsed
	if result.Windows[0].ResetsAt.IsZero() {
		t.Error("Windows[0].ResetsAt should not be zero")
	}

	// Verify extra usage
	if result.ExtraUsage == nil {
		t.Fatal("ExtraUsage should not be nil")
	}
	if !result.ExtraUsage.Enabled {
		t.Error("ExtraUsage.Enabled should be true")
	}
	if result.ExtraUsage.LimitUSD != 100.0 {
		t.Errorf("ExtraUsage.LimitUSD = %f, want 100.0", result.ExtraUsage.LimitUSD)
	}
	if result.ExtraUsage.UsedUSD != 25.50 {
		t.Errorf("ExtraUsage.UsedUSD = %f, want 25.50", result.ExtraUsage.UsedUSD)
	}

	// FetchedAt should be recent
	if time.Since(result.FetchedAt) > 5*time.Second {
		t.Errorf("FetchedAt too old: %v", result.FetchedAt)
	}
}

func TestClaude_FetchQuota_extraUsageDisabled(t *testing.T) {
	resp := map[string]any{
		"five_hour":            map[string]any{"utilization": 0.1, "resets_at": "2025-03-29T20:00:00Z"},
		"seven_day":            map[string]any{"utilization": 0.1, "resets_at": "2025-04-02T00:00:00Z"},
		"seven_day_oauth_apps": map[string]any{"utilization": 0.0, "resets_at": "2025-04-02T00:00:00Z"},
		"seven_day_opus":       map[string]any{"utilization": 0.0, "resets_at": "2025-04-02T00:00:00Z"},
		"seven_day_sonnet":     map[string]any{"utilization": 0.0, "resets_at": "2025-04-02T00:00:00Z"},
		"extra_usage":          map[string]any{"is_enabled": false},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	dir := t.TempDir()
	credPath := writeCredFile(t, dir, validCredsPayload(time.Now().Add(time.Hour)))

	c := claude.New(
		claude.WithCredentialsPath(credPath),
		claude.WithHTTPClient(http.DefaultClient),
		claude.WithBaseURL(srv.URL),
	)

	result, err := c.FetchQuota(t.Context())
	if err != nil {
		t.Fatalf("FetchQuota: %v", err)
	}
	if result.ExtraUsage != nil {
		t.Error("ExtraUsage should be nil when disabled")
	}
}

func TestClaude_FetchQuota_missingCreds(t *testing.T) {
	c := claude.New(claude.WithCredentialsPath("/nonexistent/.credentials.json"))

	_, err := c.FetchQuota(t.Context())
	if err == nil {
		t.Fatal("expected error for missing credentials")
	}

	var domErr *apierrors.DomainError
	if !errors.As(err, &domErr) {
		t.Fatalf("expected *DomainError, got %T: %v", err, err)
	}
}

func TestClaude_FetchQuota_401_retryOnce(t *testing.T) {
	var calls atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n == 1 {
			// First call: 401
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
			return
		}
		// Second call after refresh attempt: also fail to verify retry logic.
		// In a real scenario this would succeed, but since RefreshToken will
		// fail (no claude CLI), we just verify that we get an auth error.
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	defer srv.Close()

	dir := t.TempDir()
	credPath := writeCredFile(t, dir, validCredsPayload(time.Now().Add(time.Hour)))

	c := claude.New(
		claude.WithCredentialsPath(credPath),
		claude.WithHTTPClient(http.DefaultClient),
		claude.WithBaseURL(srv.URL),
	)

	_, err := c.FetchQuota(t.Context())
	if err == nil {
		t.Fatal("expected error for 401 response")
	}

	var domErr *apierrors.DomainError
	if !errors.As(err, &domErr) {
		t.Fatalf("expected *DomainError, got %T: %v", err, err)
	}
	if domErr.Kind != "auth" {
		t.Errorf("Kind = %q, want %q", domErr.Kind, "auth")
	}
}

func TestClaude_FetchQuota_serverError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	dir := t.TempDir()
	credPath := writeCredFile(t, dir, validCredsPayload(time.Now().Add(time.Hour)))

	c := claude.New(
		claude.WithCredentialsPath(credPath),
		claude.WithHTTPClient(http.DefaultClient),
		claude.WithBaseURL(srv.URL),
	)

	_, err := c.FetchQuota(t.Context())
	if err == nil {
		t.Fatal("expected error for 500 response")
	}

	var domErr *apierrors.DomainError
	if !errors.As(err, &domErr) {
		t.Fatalf("expected *DomainError, got %T: %v", err, err)
	}
	if domErr.Kind != "api" {
		t.Errorf("Kind = %q, want %q", domErr.Kind, "api")
	}
}

func TestClaude_FetchQuota_rateLimitBackoff(t *testing.T) {
	var calls atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"rate_limited"}`))
	}))
	defer srv.Close()

	dir := t.TempDir()
	credPath := writeCredFile(t, dir, validCredsPayload(time.Now().Add(time.Hour)))
	backoffPath := dir + "/backoff.json"

	c := claude.New(
		claude.WithCredentialsPath(credPath),
		claude.WithBackoffPath(backoffPath),
		claude.WithHTTPClient(http.DefaultClient),
		claude.WithBaseURL(srv.URL),
	)

	// First call should hit the server and return 429
	_, err := c.FetchQuota(t.Context())
	if err == nil {
		t.Fatal("expected error for 429 response")
	}

	if calls.Load() != 1 {
		t.Fatalf("expected 1 API call, got %d", calls.Load())
	}

	// Second call should fail immediately without hitting the server
	_, err = c.FetchQuota(t.Context())
	if err == nil {
		t.Fatal("expected error for cached backoff")
	}

	if calls.Load() != 1 {
		t.Fatalf("expected API calls to remain 1, got %d", calls.Load())
	}

	var domErr *apierrors.DomainError
	if !errors.As(err, &domErr) {
		t.Fatalf("expected *DomainError, got %T", err)
	}
	if domErr.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("StatusCode = %d, want 429", domErr.StatusCode)
	}
	if domErr.RetryAfter == 0 {
		t.Fatal("expected RetryAfter > 0 in cached backoff error")
	}
}

func TestClaude_FetchQuota_rateLimitClear(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n == 1 {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(sampleUsageResponse)
			return
		}
		t.Fatal("unexpected second call")
	}))
	defer srv.Close()

	dir := t.TempDir()
	credPath := writeCredFile(t, dir, validCredsPayload(time.Now().Add(time.Hour)))
	backoffPath := dir + "/backoff.json"

	// Pre-seed an expired backoff state
	_ = os.WriteFile(backoffPath, []byte(`{"retry_after_end":"2000-01-01T00:00:00Z"}`), 0600)

	c := claude.New(
		claude.WithCredentialsPath(credPath),
		claude.WithBackoffPath(backoffPath),
		claude.WithHTTPClient(http.DefaultClient),
		claude.WithBaseURL(srv.URL),
	)

	_, err := c.FetchQuota(t.Context())
	if err != nil {
		t.Fatalf("FetchQuota: %v", err)
	}

	if calls.Load() != 1 {
		t.Fatalf("expected 1 API call, got %d", calls.Load())
	}

	// Verify backoff state was cleared
	if _, err := os.Stat(backoffPath); !os.IsNotExist(err) {
		t.Fatalf("expected backoff file to be removed, err = %v", err)
	}
}

// validCredsPayload returns a credential file payload with the given expiry.
func validCredsPayload(expiry time.Time) map[string]any {
	return map[string]any{
		"claudeAiOauth": map[string]any{
			"accessToken":      "tok_test_valid",
			"refreshToken":     "ref_test",
			"expiresAt":        expiry.UnixMilli(),
			"scopes":           []string{"api"},
			"subscriptionType": "max",
			"rateLimitTier":    "max_claude_pro",
		},
	}
}
