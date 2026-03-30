package claude_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/schnetlerr/agent-quota/internal/claude"
)

func TestFetchUsage_normalizesWholeNumberUtilization(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"five_hour": map[string]any{
				"utilization": 12,
				"resets_at":   "2025-03-29T20:00:00Z",
			},
			"seven_day": map[string]any{
				"utilization": 41,
				"resets_at":   "2025-04-02T00:00:00Z",
			},
			"seven_day_oauth_apps": map[string]any{
				"utilization": 0,
				"resets_at":   "2025-04-02T00:00:00Z",
			},
			"seven_day_opus": map[string]any{
				"utilization": 0,
				"resets_at":   "2025-04-02T00:00:00Z",
			},
			"seven_day_sonnet": map[string]any{
				"utilization": 14,
				"resets_at":   "2025-04-02T00:00:00Z",
			},
			"extra_usage": map[string]any{
				"is_enabled":    true,
				"monthly_limit": 1000.0,
				"used_credits":  485.0,
				"utilization":   48.5,
				"currency":      "USD",
			},
		})
	}))
	defer srv.Close()

	client := claude.NewAPIClient(srv.URL, http.DefaultClient)
	resp, err := client.FetchUsage(t.Context(), "tok_test")
	if err != nil {
		t.Fatalf("FetchUsage: %v", err)
	}

	if resp.FiveHour.Utilization != 0.12 {
		t.Fatalf("FiveHour.Utilization = %f, want 0.12", resp.FiveHour.Utilization)
	}
	if resp.SevenDay.Utilization != 0.41 {
		t.Fatalf("SevenDay.Utilization = %f, want 0.41", resp.SevenDay.Utilization)
	}
	if resp.SevenDaySonnet.Utilization != 0.14 {
		t.Fatalf("SevenDaySonnet.Utilization = %f, want 0.14", resp.SevenDaySonnet.Utilization)
	}
	if resp.ExtraUsage.Utilization != 0.485 {
		t.Fatalf("ExtraUsage.Utilization = %f, want 0.485", resp.ExtraUsage.Utilization)
	}
}

func TestFetchUsage_normalizesCentBasedExtraUsageAmounts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"five_hour": map[string]any{
				"utilization": 12,
				"resets_at":   "2025-03-29T20:00:00Z",
			},
			"seven_day": map[string]any{
				"utilization": 41,
				"resets_at":   "2025-04-02T00:00:00Z",
			},
			"seven_day_oauth_apps": map[string]any{
				"utilization": 0,
				"resets_at":   "2025-04-02T00:00:00Z",
			},
			"seven_day_opus": map[string]any{
				"utilization": 0,
				"resets_at":   "2025-04-02T00:00:00Z",
			},
			"seven_day_sonnet": map[string]any{
				"utilization": 14,
				"resets_at":   "2025-04-02T00:00:00Z",
			},
			"extra_usage": map[string]any{
				"is_enabled":    true,
				"monthly_limit": 1000.0,
				"used_credits":  400.0,
				"utilization":   40.0,
				"currency":      "USD",
			},
		})
	}))
	defer srv.Close()

	client := claude.NewAPIClient(srv.URL, http.DefaultClient)
	resp, err := client.FetchUsage(t.Context(), "tok_test")
	if err != nil {
		t.Fatalf("FetchUsage: %v", err)
	}

	if resp.ExtraUsage.MonthlyLimit != 10.0 {
		t.Fatalf("ExtraUsage.MonthlyLimit = %f, want 10.0", resp.ExtraUsage.MonthlyLimit)
	}
	if resp.ExtraUsage.UsedCredits != 4.0 {
		t.Fatalf("ExtraUsage.UsedCredits = %f, want 4.0", resp.ExtraUsage.UsedCredits)
	}
	if resp.ExtraUsage.Utilization != 0.4 {
		t.Fatalf("ExtraUsage.Utilization = %f, want 0.4", resp.ExtraUsage.Utilization)
	}
}
