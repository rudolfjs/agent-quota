package output_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/schnetlerr/agent-quota/internal/output"
	"github.com/schnetlerr/agent-quota/internal/provider"
)

func TestWriteText_FullResult(t *testing.T) {
	now := time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC)
	results := []provider.QuotaResult{
		{
			Provider: "claude",
			Status:   "ok",
			Plan:     "max",
			Windows: []provider.Window{
				{Name: "five_hour", Utilization: 0.35, ResetsAt: now.Add(4*time.Hour + 30*time.Minute)},
				{Name: "seven_day", Utilization: 0.12, ResetsAt: now.Add(3*24*time.Hour + 2*time.Hour)},
			},
			ExtraUsage: &provider.ExtraUsage{
				Enabled:     true,
				LimitUSD:    100.0,
				UsedUSD:     25.50,
				Utilization: 0.255,
			},
			FetchedAt: now,
		},
	}

	var buf bytes.Buffer
	if err := output.WriteText(&buf, results, now); err != nil {
		t.Fatalf("WriteText: %v", err)
	}

	got := buf.String()

	checks := []string{
		"claude [max] ok",
		"five_hour:",
		"35% used",
		"resets in 4h30m",
		"seven_day:",
		"12% used",
		"resets in 3d2h",
		"$25.50 / $100.00 USD",
	}

	for _, want := range checks {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q\ngot:\n%s", want, got)
		}
	}
}

func TestWriteText_NoExtraUsage(t *testing.T) {
	now := time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC)
	results := []provider.QuotaResult{
		{
			Provider: "claude",
			Status:   "ok",
			Plan:     "pro",
			Windows: []provider.Window{
				{Name: "five_hour", Utilization: 0.50, ResetsAt: now.Add(2 * time.Hour)},
			},
			FetchedAt: now,
		},
	}

	var buf bytes.Buffer
	if err := output.WriteText(&buf, results, now); err != nil {
		t.Fatalf("WriteText: %v", err)
	}

	got := buf.String()

	if !strings.Contains(got, "claude [pro] ok") {
		t.Errorf("output missing header\ngot:\n%s", got)
	}
	if !strings.Contains(got, "50% used") {
		t.Errorf("output missing utilization\ngot:\n%s", got)
	}
	if strings.Contains(got, "extra") {
		t.Errorf("output should not contain extra usage line\ngot:\n%s", got)
	}
}

func TestWriteText_MultipleResults(t *testing.T) {
	now := time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC)
	results := []provider.QuotaResult{
		{Provider: "claude", Status: "ok", Plan: "max", FetchedAt: now},
		{Provider: "openai", Status: "ok", Plan: "plus", FetchedAt: now},
	}

	var buf bytes.Buffer
	if err := output.WriteText(&buf, results, now); err != nil {
		t.Fatalf("WriteText: %v", err)
	}

	got := buf.String()

	if !strings.Contains(got, "claude [max] ok") {
		t.Errorf("output missing claude header\ngot:\n%s", got)
	}
	if !strings.Contains(got, "openai [plus] ok") {
		t.Errorf("output missing openai header\ngot:\n%s", got)
	}
}

func TestWriteText_ErrorResultIncludesSafeMessage(t *testing.T) {
	now := time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC)
	results := []provider.QuotaResult{{
		Provider:  "claude",
		Status:    "error",
		FetchedAt: now,
		Error: &provider.ErrorDetails{
			Message:           "Claude API rate limit exceeded (HTTP 429), retry after 2m",
			StatusCode:        429,
			RetryAfterSeconds: 120,
		},
	}}

	var buf bytes.Buffer
	if err := output.WriteText(&buf, results, now); err != nil {
		t.Fatalf("WriteText: %v", err)
	}

	got := buf.String()
	if !strings.Contains(got, "claude error") {
		t.Fatalf("output missing error header\ngot:\n%s", got)
	}
	if !strings.Contains(got, "Claude API rate limit exceeded (HTTP 429), retry after 2m") {
		t.Fatalf("output missing safe error message\ngot:\n%s", got)
	}
}
