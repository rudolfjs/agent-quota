package gemini_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/schnetlerr/agent-quota/internal/gemini"
)

func TestGemini_Available_missingCreds(t *testing.T) {
	p := gemini.New(gemini.WithCredentialsPath(filepath.Join(t.TempDir(), "oauth_creds.json")))
	if p.Available() {
		t.Fatal("Available() = true, want false for missing oauth_creds.json")
	}
}

func TestGemini_FetchQuota_success(t *testing.T) {
	var gotProject string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1internal:loadCodeAssist":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"currentTier": map[string]any{
					"id":   "standard-tier",
					"name": "Gemini Code Assist",
				},
				"paidTier": map[string]any{
					"id":   "g1-pro-tier",
					"name": "Gemini Code Assist in Google One AI Pro",
				},
				"cloudaicompanionProject": "test-project",
			})
		case "/v1internal:retrieveUserQuota":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode quota body: %v", err)
			}
			gotProject = body["project"].(string)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"buckets": []map[string]any{
					{
						"modelId":           "gemini-2.5-flash",
						"tokenType":         "REQUESTS",
						"remainingFraction": 0.75,
						"resetTime":         "2026-03-30T00:49:32Z",
					},
					{
						"modelId":           "gemini-2.5-pro",
						"tokenType":         "REQUESTS",
						"remainingFraction": 0.50,
						"resetTime":         "2026-03-30T07:06:48Z",
					},
				},
			})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	credPath := writeCredFile(t, t.TempDir(), map[string]any{
		"access_token":  "tok_valid",
		"refresh_token": "ref_valid",
		"expiry_date":   time.Now().Add(time.Hour).UnixMilli(),
	})

	p := gemini.New(
		gemini.WithCredentialsPath(credPath),
		gemini.WithHTTPClient(http.DefaultClient),
		gemini.WithBaseURL(srv.URL),
		gemini.WithTokenURL(srv.URL+"/oauth2/token"),
	)

	result, err := p.FetchQuota(t.Context())
	if err != nil {
		t.Fatalf("FetchQuota() error = %v", err)
	}
	if gotProject != "test-project" {
		t.Fatalf("quota project = %q, want %q", gotProject, "test-project")
	}
	if result.Provider != "gemini" {
		t.Fatalf("Provider = %q, want %q", result.Provider, "gemini")
	}
	if result.Status != "ok" {
		t.Fatalf("Status = %q, want %q", result.Status, "ok")
	}
	if result.Plan != "g1-pro-tier" {
		t.Fatalf("Plan = %q, want %q", result.Plan, "g1-pro-tier")
	}
	if len(result.Windows) != 2 {
		t.Fatalf("len(Windows) = %d, want 2", len(result.Windows))
	}
	if result.Windows[0].Name != "gemini-2.5-flash" {
		t.Fatalf("Windows[0].Name = %q, want %q", result.Windows[0].Name, "gemini-2.5-flash")
	}
	if result.Windows[0].Utilization != 0.25 {
		t.Fatalf("Windows[0].Utilization = %f, want 0.25", result.Windows[0].Utilization)
	}
	if result.Windows[1].Utilization != 0.50 {
		t.Fatalf("Windows[1].Utilization = %f, want 0.50", result.Windows[1].Utilization)
	}
}

func TestGemini_FetchQuota_refreshesExpiredToken(t *testing.T) {
	var refreshCalls atomic.Int32
	var loadCalls atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth2/token":
			refreshCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "tok_new",
				"expires_in":   3600,
				"token_type":   "Bearer",
			})
		case "/v1internal:loadCodeAssist":
			loadCalls.Add(1)
			if got := r.Header.Get("Authorization"); got != "Bearer tok_new" {
				t.Fatalf("Authorization = %q, want %q", got, "Bearer tok_new")
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"currentTier":             map[string]any{"id": "standard-tier"},
				"cloudaicompanionProject": "test-project",
			})
		case "/v1internal:retrieveUserQuota":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"buckets": []map[string]any{{
					"modelId":           "gemini-2.5-flash",
					"tokenType":         "REQUESTS",
					"remainingFraction": 1.0,
					"resetTime":         "2026-03-30T00:49:32Z",
				}},
			})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	credPath := writeCredFile(t, dir, map[string]any{
		"access_token":  "tok_old",
		"refresh_token": "ref_valid",
		"expiry_date":   time.Now().Add(-time.Hour).UnixMilli(),
	})

	p := gemini.New(
		gemini.WithCredentialsPath(credPath),
		gemini.WithHTTPClient(http.DefaultClient),
		gemini.WithBaseURL(srv.URL),
		gemini.WithTokenURL(srv.URL+"/oauth2/token"),
	)

	if _, err := p.FetchQuota(t.Context()); err != nil {
		t.Fatalf("FetchQuota() error = %v", err)
	}
	if refreshCalls.Load() != 1 {
		t.Fatalf("refresh calls = %d, want 1", refreshCalls.Load())
	}
	if loadCalls.Load() != 1 {
		t.Fatalf("load calls = %d, want 1", loadCalls.Load())
	}

	got := readJSONFile(t, credPath)
	if got["access_token"] != "tok_new" {
		t.Fatalf("saved access_token = %v, want %q", got["access_token"], "tok_new")
	}
}

func writeCredFile(t *testing.T, dir string, payload map[string]any) string {
	t.Helper()
	path := filepath.Join(dir, "oauth_creds.json")
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
