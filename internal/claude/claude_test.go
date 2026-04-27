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

	"github.com/rudolfjs/agent-quota/internal/claude"
	apierrors "github.com/rudolfjs/agent-quota/internal/errors"
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
		claude.WithBackoffPath(dir+"/backoff.json"),
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
		claude.WithBackoffPath(dir+"/backoff.json"),
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
		claude.WithBackoffPath(dir+"/backoff.json"),
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
		claude.WithBackoffPath(dir+"/backoff.json"),
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
	if _, err := os.Stat(backoffPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected backoff file to be removed, err = %v", err)
	}
}

func TestClaude_ResetBackoff_clearsPersistedCooldown(t *testing.T) {
	dir := t.TempDir()
	credPath := writeCredFile(t, dir, validCredsPayload(time.Now().Add(time.Hour)))
	backoffPath := dir + "/backoff.json"

	if err := os.WriteFile(backoffPath, []byte(`{"retry_after_end":"2099-01-01T00:00:00Z"}`), 0o600); err != nil {
		t.Fatalf("WriteFile(backoff): %v", err)
	}

	c := claude.New(
		claude.WithCredentialsPath(credPath),
		claude.WithBackoffPath(backoffPath),
	)

	if err := c.ResetBackoff(); err != nil {
		t.Fatalf("ResetBackoff() error = %v", err)
	}

	if _, err := os.Stat(backoffPath); !errors.Is(err, os.ErrNotExist) {
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

// TestClaude_FetchQuota_forceDoesNotRePersistBackoff verifies that after a
// manual reset (simulating --force / ctrl+r), a subsequent 429 does NOT
// re-save the backoff file. Currently FetchQuota unconditionally persists
// backoff on 429, defeating the user's explicit "retry now" action.
func TestClaude_FetchQuota_forceDoesNotRePersistBackoff(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"rate_limited"}`))
	}))
	defer srv.Close()

	dir := t.TempDir()
	credPath := writeCredFile(t, dir, validCredsPayload(time.Now().Add(time.Hour)))
	backoffPath := dir + "/backoff.json"

	// Seed an existing backoff file (simulating a prior 429).
	if err := os.WriteFile(backoffPath, []byte(`{"retry_after_end":"2099-01-01T00:00:00Z"}`), 0o600); err != nil {
		t.Fatalf("seed backoff file: %v", err)
	}

	c := claude.New(
		claude.WithCredentialsPath(credPath),
		claude.WithBackoffPath(backoffPath),
		claude.WithHTTPClient(http.DefaultClient),
		claude.WithBaseURL(srv.URL),
	)

	// Step 1: User triggers --force / ctrl+r: clear the backoff.
	if err := c.ResetBackoff(); err != nil {
		t.Fatalf("ResetBackoff: %v", err)
	}

	// Verify the file is gone after reset.
	if _, err := os.Stat(backoffPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected backoff file removed after ResetBackoff, stat err = %v", err)
	}

	// Step 2: FetchQuota fires and gets a 429 again.
	_, err := c.FetchQuota(t.Context())
	if err == nil {
		t.Fatal("expected error for 429 response")
	}

	// Bug assertion: after a forced retry, the backoff file should NOT be
	// re-created. The user explicitly asked to retry — persisting backoff
	// again would lock them out immediately, defeating the purpose.
	if _, statErr := os.Stat(backoffPath); statErr == nil {
		t.Fatal("BUG: backoff file was re-created after forced retry; " +
			"--force / ctrl+r should suppress backoff persistence on the immediate fetch")
	}
}

// TestClaude_FetchQuota_retryAfterCapped verifies that an absurdly large
// Retry-After value from the API is capped at a reasonable maximum (5 minutes).
// Currently the CLI trusts the server-provided duration blindly, so a
// Retry-After of 86400 (24 hours) locks the user out for a full day.
func TestClaude_FetchQuota_retryAfterCapped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "86400") // 24 hours — absurdly long
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

	_, err := c.FetchQuota(t.Context())
	if err == nil {
		t.Fatal("expected error for 429 response")
	}

	// Read the persisted backoff file and check the deadline.
	data, readErr := os.ReadFile(backoffPath)
	if readErr != nil {
		t.Fatalf("expected backoff file to exist: %v", readErr)
	}

	var state struct {
		RetryAfterEnd time.Time `json:"retry_after_end"`
	}
	if jsonErr := json.Unmarshal(data, &state); jsonErr != nil {
		t.Fatalf("unmarshal backoff: %v", jsonErr)
	}

	maxCap := 5 * time.Minute
	deadline := time.Until(state.RetryAfterEnd)
	if deadline > maxCap+10*time.Second {
		t.Fatalf("BUG: persisted backoff deadline is %v in the future (want <= %v); "+
			"Retry-After should be capped at %v to prevent unreasonable lockouts",
			deadline.Round(time.Second), maxCap, maxCap)
	}
}
