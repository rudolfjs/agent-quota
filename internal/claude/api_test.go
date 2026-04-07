package claude_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/schnetlerr/agent-quota/internal/claude"
	apierrors "github.com/schnetlerr/agent-quota/internal/errors"
)

// sampleUsageResponse is a known-good API response fixture.
var sampleUsageResponse = map[string]any{
	"five_hour": map[string]any{
		"utilization": 0.35,
		"resets_at":   "2025-03-29T20:00:00Z",
	},
	"seven_day": map[string]any{
		"utilization": 0.12,
		"resets_at":   "2025-04-02T00:00:00Z",
	},
	"seven_day_oauth_apps": map[string]any{
		"utilization": 0.05,
		"resets_at":   "2025-04-02T00:00:00Z",
	},
	"seven_day_opus": map[string]any{
		"utilization": 0.0,
		"resets_at":   "2025-04-02T00:00:00Z",
	},
	"seven_day_sonnet": map[string]any{
		"utilization": 0.08,
		"resets_at":   "2025-04-02T00:00:00Z",
	},
	"extra_usage": map[string]any{
		"is_enabled":    true,
		"monthly_limit": 100.0,
		"used_credits":  25.50,
		"utilization":   0.255,
		"currency":      "USD",
	},
}

func TestFetchUsage_sendsRequiredHeaders(t *testing.T) {
	var gotAuth, gotBeta, gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotBeta = r.Header.Get("anthropic-beta")
		gotUA = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(sampleUsageResponse)
	}))
	defer srv.Close()

	client := claude.NewAPIClient(srv.URL, http.DefaultClient)
	_, err := client.FetchUsage(t.Context(), "tok_test")
	if err != nil {
		t.Fatalf("FetchUsage: %v", err)
	}

	if gotAuth != "Bearer tok_test" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer tok_test")
	}
	if gotBeta != "oauth-2025-04-20" {
		t.Errorf("anthropic-beta = %q, want %q", gotBeta, "oauth-2025-04-20")
	}
	if !strings.HasPrefix(gotUA, "claude-code/") {
		t.Errorf("User-Agent = %q, want prefix %q", gotUA, "claude-code/")
	}
}

func TestFetchUsage_parsesResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(sampleUsageResponse)
	}))
	defer srv.Close()

	client := claude.NewAPIClient(srv.URL, http.DefaultClient)
	resp, err := client.FetchUsage(t.Context(), "tok_test")
	if err != nil {
		t.Fatalf("FetchUsage: %v", err)
	}

	if resp.FiveHour.Utilization != 0.35 {
		t.Errorf("FiveHour.Utilization = %f, want 0.35", resp.FiveHour.Utilization)
	}
	if resp.SevenDay.Utilization != 0.12 {
		t.Errorf("SevenDay.Utilization = %f, want 0.12", resp.SevenDay.Utilization)
	}
	if !resp.ExtraUsage.IsEnabled {
		t.Error("ExtraUsage.IsEnabled should be true")
	}
	if resp.ExtraUsage.UsedCredits != 25.50 {
		t.Errorf("ExtraUsage.UsedCredits = %f, want 25.50", resp.ExtraUsage.UsedCredits)
	}
}

func TestFetchUsage_non200_returnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	defer srv.Close()

	client := claude.NewAPIClient(srv.URL, http.DefaultClient)
	_, err := client.FetchUsage(t.Context(), "bad_tok")
	if err == nil {
		t.Fatal("expected error for 401 response, got nil")
	}
}

func TestFetchUsage_rateLimitedCarriesRetryAfter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"rate_limited"}`))
	}))
	defer srv.Close()

	client := claude.NewAPIClient(srv.URL, http.DefaultClient)
	_, err := client.FetchUsage(t.Context(), "tok")
	if err == nil {
		t.Fatal("expected error for 429 response, got nil")
	}

	var domErr *apierrors.DomainError
	if !errors.As(err, &domErr) {
		t.Fatalf("error type = %T, want DomainError", err)
	}
	if domErr.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("StatusCode = %d, want %d", domErr.StatusCode, http.StatusTooManyRequests)
	}
	if domErr.RetryAfter != 120*time.Second {
		t.Fatalf("RetryAfter = %v, want %v", domErr.RetryAfter, 120*time.Second)
	}
	if !strings.Contains(domErr.Message, "429") {
		t.Fatalf("Message = %q, want HTTP status code in user-facing message", domErr.Message)
	}
}

func TestFetchUsage_unexpectedStatus_includesCodeInMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"internal"}`))
	}))
	defer srv.Close()

	client := claude.NewAPIClient(srv.URL, http.DefaultClient)
	_, err := client.FetchUsage(t.Context(), "tok")
	if err == nil {
		t.Fatal("expected error for 500 response, got nil")
	}

	var domErr *apierrors.DomainError
	if !errors.As(err, &domErr) {
		t.Fatalf("error type = %T, want DomainError", err)
	}
	if domErr.StatusCode != http.StatusInternalServerError {
		t.Fatalf("StatusCode = %d, want %d", domErr.StatusCode, http.StatusInternalServerError)
	}
	if !strings.Contains(domErr.Message, "500") {
		t.Fatalf("Message = %q, want HTTP status code in user-facing message", domErr.Message)
	}
}

func TestFetchUsage_malformedJSON_returnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{bad json`))
	}))
	defer srv.Close()

	client := claude.NewAPIClient(srv.URL, http.DefaultClient)
	_, err := client.FetchUsage(t.Context(), "tok")
	if err == nil {
		t.Fatal("expected error for malformed JSON, got nil")
	}
}

func TestFetchUsage_contextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// never responds
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(t.Context())
	cancel() // cancel immediately

	client := claude.NewAPIClient(srv.URL, http.DefaultClient)
	_, err := client.FetchUsage(ctx, "tok")
	if err == nil {
		t.Fatal("expected error for cancelled context, got nil")
	}
}
