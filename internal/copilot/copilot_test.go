package copilot_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rudolfjs/agent-quota/internal/copilot"
)

func TestCopilot_Available_missingConfig(t *testing.T) {
	t.Setenv("COPILOT_GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")

	p := copilot.New(copilot.WithConfigPath(filepath.Join(t.TempDir(), "config.json")))
	if p.Available() {
		t.Fatal("Available() = true, want false for missing config.json")
	}
}

func TestCopilot_Available_envToken(t *testing.T) {
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("COPILOT_GITHUB_TOKEN", "github_pat_test")

	p := copilot.New(copilot.WithConfigPath(filepath.Join(t.TempDir(), "config.json")))
	if !p.Available() {
		t.Fatal("Available() = false, want true when COPILOT_GITHUB_TOKEN is set")
	}
}

func TestCopilot_FetchQuota_success(t *testing.T) {
	t.Setenv("COPILOT_GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")

	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/copilot_internal/user" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"copilot_plan":         "business",
			"quota_reset_date_utc": "2026-04-01T00:00:00Z",
			"quota_snapshots": map[string]any{
				"chat": map[string]any{
					"entitlement":       300,
					"percent_remaining": 75,
				},
				"completions": map[string]any{
					"entitlement":       2000,
					"percent_remaining": 50,
				},
				"premium_interactions": map[string]any{
					"entitlement":       300,
					"percent_remaining": 25,
					"overage_permitted": true,
					"overage_count":     12,
				},
			},
		})
	}))
	defer srv.Close()

	configPath := writeConfigFile(t, t.TempDir(), map[string]any{
		"last_logged_in_user": map[string]any{
			"host":  "https://github.com",
			"login": "octocat",
		},
		"copilot_tokens": map[string]any{
			"https://github.com:octocat": "tok_valid",
		},
	})

	p := copilot.New(
		copilot.WithConfigPath(configPath),
		copilot.WithHTTPClient(http.DefaultClient),
		copilot.WithBaseURL(srv.URL),
	)

	result, err := p.FetchQuota(t.Context())
	if err != nil {
		t.Fatalf("FetchQuota() error = %v", err)
	}

	if gotAuth != "Bearer tok_valid" {
		t.Fatalf("Authorization = %q, want %q", gotAuth, "Bearer tok_valid")
	}
	if result.Provider != "copilot" {
		t.Fatalf("Provider = %q, want %q", result.Provider, "copilot")
	}
	if result.Status != "ok" {
		t.Fatalf("Status = %q, want %q", result.Status, "ok")
	}
	if result.Plan != "business" {
		t.Fatalf("Plan = %q, want %q", result.Plan, "business")
	}
	if len(result.Windows) != 3 {
		t.Fatalf("len(Windows) = %d, want 3", len(result.Windows))
	}
	if result.Windows[0].Name != "premium_interactions" {
		t.Fatalf("Windows[0].Name = %q, want %q", result.Windows[0].Name, "premium_interactions")
	}
	if result.Windows[1].Name != "chat" {
		t.Fatalf("Windows[1].Name = %q, want %q", result.Windows[1].Name, "chat")
	}
	if result.Windows[2].Name != "completions" {
		t.Fatalf("Windows[2].Name = %q, want %q", result.Windows[2].Name, "completions")
	}
	if result.Windows[0].Utilization != 0.75 {
		t.Fatalf("Windows[0].Utilization = %f, want 0.75", result.Windows[0].Utilization)
	}
	if result.Windows[1].Utilization != 0.25 {
		t.Fatalf("Windows[1].Utilization = %f, want 0.25", result.Windows[1].Utilization)
	}
	if result.Windows[2].Utilization != 0.50 {
		t.Fatalf("Windows[2].Utilization = %f, want 0.50", result.Windows[2].Utilization)
	}
	wantReset := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	for i, window := range result.Windows {
		if !window.ResetsAt.Equal(wantReset) {
			t.Fatalf("Windows[%d].ResetsAt = %v, want %v", i, window.ResetsAt, wantReset)
		}
	}
}

func TestCopilot_Available_configFile(t *testing.T) {
	t.Setenv("COPILOT_GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")

	configPath := writeConfigFile(t, t.TempDir(), map[string]any{
		"last_logged_in_user": map[string]any{
			"host":  "https://github.com",
			"login": "octocat",
		},
		"copilot_tokens": map[string]any{
			"https://github.com:octocat": "tok_from_file",
		},
	})

	p := copilot.New(copilot.WithConfigPath(configPath))
	if !p.Available() {
		t.Fatal("Available() = false, want true when config file has valid token")
	}
}

func TestCopilot_FetchQuota_unauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	t.Setenv("COPILOT_GITHUB_TOKEN", "tok_expired")
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")

	p := copilot.New(
		copilot.WithConfigPath(filepath.Join(t.TempDir(), "config.json")),
		copilot.WithHTTPClient(http.DefaultClient),
		copilot.WithBaseURL(srv.URL),
	)

	_, err := p.FetchQuota(t.Context())
	if err == nil {
		t.Fatal("FetchQuota() error = nil, want auth error on 401")
	}
}

func TestCopilot_FetchQuota_unlimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"copilot_plan": "business",
			"quota_snapshots": map[string]any{
				"completions": map[string]any{
					"unlimited": true,
				},
			},
		})
	}))
	defer srv.Close()

	t.Setenv("COPILOT_GITHUB_TOKEN", "tok_unlimited")
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")

	p := copilot.New(
		copilot.WithConfigPath(filepath.Join(t.TempDir(), "config.json")),
		copilot.WithHTTPClient(http.DefaultClient),
		copilot.WithBaseURL(srv.URL),
	)

	result, err := p.FetchQuota(t.Context())
	if err != nil {
		t.Fatalf("FetchQuota() error = %v", err)
	}
	if len(result.Windows) != 1 {
		t.Fatalf("len(Windows) = %d, want 1", len(result.Windows))
	}
	if result.Windows[0].Utilization != 0 {
		t.Fatalf("Windows[0].Utilization = %f, want 0 for unlimited snapshot", result.Windows[0].Utilization)
	}
}

func writeConfigFile(t *testing.T, dir string, payload map[string]any) string {
	t.Helper()

	path := filepath.Join(dir, "config.json")
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("os.WriteFile: %v", err)
	}
	return path
}
