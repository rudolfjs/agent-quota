package provider_test

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	apierrors "github.com/schnetlerr/agent-quota/internal/errors"
	"github.com/schnetlerr/agent-quota/internal/provider"
)

func TestQuotaResult_JSONRoundtrip(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	r := provider.QuotaResult{
		Provider:  "claude",
		Status:    "ok",
		Plan:      "max",
		FetchedAt: now,
		Windows: []provider.Window{
			{Name: "five_hour", Utilization: 0.35, ResetsAt: now.Add(time.Hour)},
			{Name: "seven_day", Utilization: 0.12, ResetsAt: now.Add(7 * 24 * time.Hour)},
		},
		ExtraUsage: &provider.ExtraUsage{
			Enabled:     true,
			LimitUSD:    100.0,
			UsedUSD:     25.50,
			Utilization: 0.255,
		},
	}

	b, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var got provider.QuotaResult
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if got.Provider != r.Provider {
		t.Errorf("Provider = %q, want %q", got.Provider, r.Provider)
	}
	if got.Status != r.Status {
		t.Errorf("Status = %q, want %q", got.Status, r.Status)
	}
	if len(got.Windows) != 2 {
		t.Errorf("len(Windows) = %d, want 2", len(got.Windows))
	}
	if got.ExtraUsage == nil {
		t.Fatal("ExtraUsage should not be nil after roundtrip")
	}
	if got.ExtraUsage.LimitUSD != 100.0 {
		t.Errorf("ExtraUsage.LimitUSD = %f, want 100.0", got.ExtraUsage.LimitUSD)
	}
}

func TestQuotaResult_NoExtraUsage_OmittedFromJSON(t *testing.T) {
	r := provider.QuotaResult{
		Provider:  "claude",
		Status:    "ok",
		FetchedAt: time.Now(),
	}

	b, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	// extra_usage should be absent (omitempty)
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["extra_usage"]; ok {
		t.Error("extra_usage should be omitted when nil")
	}
}

func TestWindow_UtilizationRange(t *testing.T) {
	tests := []struct {
		utilization float64
		wantValid   bool
	}{
		{0.0, true},
		{0.5, true},
		{1.0, true},
		{-0.1, false},
		{1.1, false},
	}

	for _, tc := range tests {
		w := provider.Window{Name: "test", Utilization: tc.utilization}
		got := w.IsValid()
		if got != tc.wantValid {
			t.Errorf("Window{Utilization: %f}.IsValid() = %v, want %v", tc.utilization, got, tc.wantValid)
		}
	}
}

func TestErrorResult_usesSafeDomainErrorDetails(t *testing.T) {
	now := time.Now().UTC()
	domErr := apierrors.NewAPIError("Claude API rate limit exceeded (HTTP 429), retry after 2m", errors.New("HTTP 429"))
	domErr.StatusCode = 429
	domErr.RetryAfter = 2 * time.Minute

	got := provider.ErrorResult("claude", domErr, now)

	if got.Status != "error" {
		t.Fatalf("Status = %q, want %q", got.Status, "error")
	}
	if got.Error == nil {
		t.Fatal("Error should be populated for error results")
	}
	if got.Error.Kind != "api" {
		t.Fatalf("Error.Kind = %q, want %q", got.Error.Kind, "api")
	}
	if got.Error.Message != domErr.Message {
		t.Fatalf("Error.Message = %q, want %q", got.Error.Message, domErr.Message)
	}
	if got.Error.StatusCode != 429 {
		t.Fatalf("Error.StatusCode = %d, want 429", got.Error.StatusCode)
	}
	if got.Error.RetryAfterSeconds != 120 {
		t.Fatalf("Error.RetryAfterSeconds = %d, want 120", got.Error.RetryAfterSeconds)
	}
}

func TestErrorResult_sanitizesUnexpectedErrors(t *testing.T) {
	got := provider.ErrorResult("claude", errors.New("raw internal failure"), time.Now().UTC())

	if got.Error == nil {
		t.Fatal("Error should be populated for unexpected failures")
	}
	if got.Error.Message != "unexpected error" {
		t.Fatalf("Error.Message = %q, want %q", got.Error.Message, "unexpected error")
	}
	if got.Error.Kind != "" {
		t.Fatalf("Error.Kind = %q, want empty string", got.Error.Kind)
	}
}
