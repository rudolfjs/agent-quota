package openai_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/schnetlerr/agent-quota/internal/openai"
)

func TestOpenAI_Available_missingAuth(t *testing.T) {
	p := openai.New(openai.WithAuthPath(filepath.Join(t.TempDir(), "auth.json")))
	if p.Available() {
		t.Fatal("Available() = true, want false for missing auth.json")
	}
}

func TestOpenAI_FetchQuota_success(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/backend-api/wham/usage" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"rateLimits": map[string]any{
				"limitId":   "codex",
				"limitName": "Codex",
				"planType":  "pro",
				"primary": map[string]any{
					"usedPercent":        35,
					"windowDurationMins": 300,
					"resetsAt":           time.Date(2026, 3, 29, 22, 0, 0, 0, time.UTC).Unix(),
				},
				"secondary": map[string]any{
					"usedPercent":        12,
					"windowDurationMins": 7 * 24 * 60,
					"resetsAt":           time.Date(2026, 4, 2, 0, 0, 0, 0, time.UTC).UnixMilli(),
				},
			},
		})
	}))
	defer srv.Close()

	authPath := writeAuthFile(t, t.TempDir(), map[string]any{
		"auth_mode":      "chatgpt",
		"OPENAI_API_KEY": nil,
		"tokens": map[string]any{
			"id_token":      "id_tok",
			"access_token":  "tok_valid",
			"refresh_token": "ref_valid",
			"account_id":    "acct_123",
		},
		"last_refresh": time.Now().UTC().Format(time.RFC3339),
	})

	p := openai.New(
		openai.WithAuthPath(authPath),
		openai.WithHTTPClient(http.DefaultClient),
		openai.WithUsageURL(srv.URL+"/backend-api/wham/usage"),
		openai.WithTokenURL(srv.URL+"/oauth/token"),
	)

	result, err := p.FetchQuota(t.Context())
	if err != nil {
		t.Fatalf("FetchQuota() error = %v", err)
	}

	if gotAuth != "Bearer tok_valid" {
		t.Fatalf("Authorization = %q, want %q", gotAuth, "Bearer tok_valid")
	}
	if result.Provider != "openai" {
		t.Fatalf("Provider = %q, want %q", result.Provider, "openai")
	}
	if result.Status != "ok" {
		t.Fatalf("Status = %q, want %q", result.Status, "ok")
	}
	if result.Plan != "pro" {
		t.Fatalf("Plan = %q, want %q", result.Plan, "pro")
	}
	if len(result.Windows) != 2 {
		t.Fatalf("len(Windows) = %d, want 2", len(result.Windows))
	}
	if result.Windows[0].Name != "five_hour" {
		t.Fatalf("Windows[0].Name = %q, want %q", result.Windows[0].Name, "five_hour")
	}
	if result.Windows[1].Name != "seven_day" {
		t.Fatalf("Windows[1].Name = %q, want %q", result.Windows[1].Name, "seven_day")
	}
	if result.Windows[0].Utilization != 0.35 {
		t.Fatalf("Windows[0].Utilization = %f, want 0.35", result.Windows[0].Utilization)
	}
	if result.Windows[1].Utilization != 0.12 {
		t.Fatalf("Windows[1].Utilization = %f, want 0.12", result.Windows[1].Utilization)
	}
	if result.Windows[0].ResetsAt.IsZero() || result.Windows[1].ResetsAt.IsZero() {
		t.Fatal("expected reset times to be parsed")
	}
}

func TestOpenAI_FetchQuota_currentUsageSchema(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/backend-api/wham/usage" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"plan_type": "plus",
			"rate_limit": map[string]any{
				"primary_window": map[string]any{
					"used_percent":         1,
					"limit_window_seconds": 300 * 60,
					"reset_at":             time.Date(2026, 3, 29, 22, 0, 0, 0, time.UTC).Unix(),
				},
				"secondary_window": map[string]any{
					"used_percent":         45,
					"limit_window_seconds": 7 * 24 * 60 * 60,
					"reset_at":             time.Date(2026, 4, 2, 0, 0, 0, 0, time.UTC).Unix(),
				},
			},
			"additional_rate_limits": []map[string]any{{
				"limit_name":      "GPT-5.3-Codex-Spark",
				"metered_feature": "codex_bengalfox",
				"rate_limit": map[string]any{
					"primary_window": map[string]any{
						"used_percent":         0,
						"limit_window_seconds": 300 * 60,
						"reset_at":             time.Date(2026, 3, 29, 23, 0, 0, 0, time.UTC).Unix(),
					},
					"secondary_window": map[string]any{
						"used_percent":         0,
						"limit_window_seconds": 7 * 24 * 60 * 60,
						"reset_at":             time.Date(2026, 4, 5, 0, 0, 0, 0, time.UTC).Unix(),
					},
				},
			}},
			"code_review_rate_limit": map[string]any{
				"primary_window": map[string]any{
					"used_percent":         12,
					"limit_window_seconds": 7 * 24 * 60 * 60,
					"reset_at":             time.Date(2026, 4, 6, 0, 0, 0, 0, time.UTC).Unix(),
				},
			},
		})
	}))
	defer srv.Close()

	authPath := writeAuthFile(t, t.TempDir(), map[string]any{
		"auth_mode":      "chatgpt",
		"OPENAI_API_KEY": nil,
		"tokens": map[string]any{
			"id_token":      "id_tok",
			"access_token":  "tok_valid",
			"refresh_token": "ref_valid",
			"account_id":    "acct_123",
		},
	})

	p := openai.New(
		openai.WithAuthPath(authPath),
		openai.WithHTTPClient(http.DefaultClient),
		openai.WithUsageURL(srv.URL+"/backend-api/wham/usage"),
		openai.WithTokenURL(srv.URL+"/oauth/token"),
	)

	result, err := p.FetchQuota(t.Context())
	if err != nil {
		t.Fatalf("FetchQuota() error = %v", err)
	}
	if result.Plan != "plus" {
		t.Fatalf("Plan = %q, want %q", result.Plan, "plus")
	}
	if len(result.Windows) != 5 {
		t.Fatalf("len(Windows) = %d, want 5", len(result.Windows))
	}
	if result.Windows[0].Name != "five_hour" {
		t.Fatalf("Windows[0].Name = %q, want %q", result.Windows[0].Name, "five_hour")
	}
	if result.Windows[1].Name != "seven_day" {
		t.Fatalf("Windows[1].Name = %q, want %q", result.Windows[1].Name, "seven_day")
	}
	if result.Windows[2].Name != "codex_spark_five_hour" {
		t.Fatalf("Windows[2].Name = %q, want %q", result.Windows[2].Name, "codex_spark_five_hour")
	}
	if result.Windows[3].Name != "codex_spark_seven_day" {
		t.Fatalf("Windows[3].Name = %q, want %q", result.Windows[3].Name, "codex_spark_seven_day")
	}
	if result.Windows[4].Name != "code_review_seven_day" {
		t.Fatalf("Windows[4].Name = %q, want %q", result.Windows[4].Name, "code_review_seven_day")
	}
	if result.Windows[0].Utilization != 0.01 {
		t.Fatalf("Windows[0].Utilization = %f, want 0.01", result.Windows[0].Utilization)
	}
	if result.Windows[1].Utilization != 0.45 {
		t.Fatalf("Windows[1].Utilization = %f, want 0.45", result.Windows[1].Utilization)
	}
	if result.Windows[4].Utilization != 0.12 {
		t.Fatalf("Windows[4].Utilization = %f, want 0.12", result.Windows[4].Utilization)
	}
}

func TestOpenAI_FetchQuota_refreshesAfter401(t *testing.T) {
	var usageCalls atomic.Int32
	var refreshCalls atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/backend-api/wham/usage":
			call := usageCalls.Add(1)
			if call == 1 {
				if got := r.Header.Get("Authorization"); got != "Bearer tok_old" {
					t.Fatalf("first Authorization = %q, want %q", got, "Bearer tok_old")
				}
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":{"message":"expired"},"status":401}`))
				return
			}
			if got := r.Header.Get("Authorization"); got != "Bearer tok_new" {
				t.Fatalf("second Authorization = %q, want %q", got, "Bearer tok_new")
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"rateLimits": map[string]any{
					"planType": "plus",
					"primary": map[string]any{
						"usedPercent":        10,
						"windowDurationMins": 300,
						"resetsAt":           time.Date(2026, 3, 29, 22, 0, 0, 0, time.UTC).Unix(),
					},
				},
			})
		case "/oauth/token":
			refreshCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "tok_new",
				"refresh_token": "ref_new",
			})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	authPath := writeAuthFile(t, dir, map[string]any{
		"auth_mode":      "chatgpt",
		"OPENAI_API_KEY": nil,
		"tokens": map[string]any{
			"id_token":      "id_tok",
			"access_token":  "tok_old",
			"refresh_token": "ref_old",
			"account_id":    "acct_123",
		},
	})

	p := openai.New(
		openai.WithAuthPath(authPath),
		openai.WithHTTPClient(http.DefaultClient),
		openai.WithUsageURL(srv.URL+"/backend-api/wham/usage"),
		openai.WithTokenURL(srv.URL+"/oauth/token"),
	)

	result, err := p.FetchQuota(t.Context())
	if err != nil {
		t.Fatalf("FetchQuota() error = %v", err)
	}
	if result.Plan != "plus" {
		t.Fatalf("Plan = %q, want %q", result.Plan, "plus")
	}
	if usageCalls.Load() != 2 {
		t.Fatalf("usage calls = %d, want 2", usageCalls.Load())
	}
	if refreshCalls.Load() != 1 {
		t.Fatalf("refresh calls = %d, want 1", refreshCalls.Load())
	}

	got := readJSONFile(t, authPath)
	tokens := got["tokens"].(map[string]any)
	if tokens["access_token"] != "tok_new" {
		t.Fatalf("saved access_token = %v, want %q", tokens["access_token"], "tok_new")
	}
	if tokens["refresh_token"] != "ref_new" {
		t.Fatalf("saved refresh_token = %v, want %q", tokens["refresh_token"], "ref_new")
	}
}

func writeAuthFile(t *testing.T, dir string, payload map[string]any) string {
	t.Helper()
	path := filepath.Join(dir, "auth.json")
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatalf("os.WriteFile: %v", err)
	}
	return path
}

func readJSONFile(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	return got
}
