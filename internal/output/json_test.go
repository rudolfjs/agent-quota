package output_test

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/schnetlerr/agent-quota/internal/output"
	"github.com/schnetlerr/agent-quota/internal/provider"
)

func TestWriteJSON_SingleResult(t *testing.T) {
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
	if err := output.WriteJSON(&buf, results); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}

	// Single result should be a plain JSON object, not an array
	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("Unmarshal as object: %v", err)
	}

	if m["provider"] != "claude" {
		t.Errorf("provider = %v, want %q", m["provider"], "claude")
	}
	if m["status"] != "ok" {
		t.Errorf("status = %v, want %q", m["status"], "ok")
	}
	if m["fetched_at"] == nil {
		t.Error("fetched_at should be present")
	}

	windows, ok := m["windows"].([]any)
	if !ok {
		t.Fatalf("windows is not an array: %T", m["windows"])
	}
	if len(windows) != 2 {
		t.Errorf("len(windows) = %d, want 2", len(windows))
	}

	extra, ok := m["extra_usage"].(map[string]any)
	if !ok {
		t.Fatal("extra_usage should be present as object")
	}
	if extra["limit_usd"] != 100.0 {
		t.Errorf("extra_usage.limit_usd = %v, want 100.0", extra["limit_usd"])
	}
}

func TestWriteJSON_NilExtraUsage_Omitted(t *testing.T) {
	now := time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC)
	results := []provider.QuotaResult{
		{
			Provider:  "claude",
			Status:    "ok",
			FetchedAt: now,
		},
	}

	var buf bytes.Buffer
	if err := output.WriteJSON(&buf, results); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if _, ok := m["extra_usage"]; ok {
		t.Error("extra_usage should be omitted when nil")
	}
}

func TestWriteJSON_MultipleResults_Array(t *testing.T) {
	now := time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC)
	results := []provider.QuotaResult{
		{Provider: "claude", Status: "ok", FetchedAt: now},
		{Provider: "openai", Status: "ok", FetchedAt: now},
	}

	var buf bytes.Buffer
	if err := output.WriteJSON(&buf, results); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}

	var arr []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &arr); err != nil {
		t.Fatalf("Unmarshal as array: %v", err)
	}

	if len(arr) != 2 {
		t.Fatalf("len(arr) = %d, want 2", len(arr))
	}
	if arr[0]["provider"] != "claude" {
		t.Errorf("arr[0].provider = %v, want %q", arr[0]["provider"], "claude")
	}
	if arr[1]["provider"] != "openai" {
		t.Errorf("arr[1].provider = %v, want %q", arr[1]["provider"], "openai")
	}
}

func TestWriteJSON_ErrorResultIncludesSafeErrorDetails(t *testing.T) {
	now := time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC)
	results := []provider.QuotaResult{{
		Provider:  "claude",
		Status:    "error",
		FetchedAt: now,
		Error: &provider.ErrorDetails{
			Kind:              "api",
			Message:           "Claude API rate limit exceeded (HTTP 429), retry after 2m",
			StatusCode:        429,
			RetryAfterSeconds: 120,
		},
	}}

	var buf bytes.Buffer
	if err := output.WriteJSON(&buf, results); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	errObj, ok := m["error"].(map[string]any)
	if !ok {
		t.Fatal("error should be present as an object")
	}
	if errObj["message"] != "Claude API rate limit exceeded (HTTP 429), retry after 2m" {
		t.Fatalf("error.message = %v, want safe rate-limit message", errObj["message"])
	}
	if errObj["status_code"] != 429.0 {
		t.Fatalf("error.status_code = %v, want 429", errObj["status_code"])
	}
	if errObj["retry_after_seconds"] != 120.0 {
		t.Fatalf("error.retry_after_seconds = %v, want 120", errObj["retry_after_seconds"])
	}
}
