package provider_test

import (
	"encoding/json"
	"testing"
	"time"

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
