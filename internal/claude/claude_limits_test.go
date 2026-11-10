package claude_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rudolfjs/agent-quota/internal/claude"
	"github.com/rudolfjs/agent-quota/internal/provider"
)

// fetchQuotaFromJSON serves body as the usage API response and returns the
// converted quota result.
func fetchQuotaFromJSON(t *testing.T, body string) provider.QuotaResult {
	t.Helper()
	if !json.Valid([]byte(body)) {
		t.Fatal("test fixture is not valid JSON")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

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
	return result
}

func windowNames(windows []provider.Window) []string {
	names := make([]string, len(windows))
	for i, w := range windows {
		names[i] = w.Name
	}
	return names
}

func assertWindowNames(t *testing.T, windows []provider.Window, want ...string) {
	t.Helper()
	got := windowNames(windows)
	if len(got) != len(want) {
		t.Fatalf("window names = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("window names = %v, want %v", got, want)
		}
	}
}

// Mirrors the response shape observed in November 2026: legacy per-model
// fields are null and the model-scoped weekly limit lives in limits.
func TestClaude_FetchQuota_limitsArrayReplacesSonnetWindow(t *testing.T) {
	result := fetchQuotaFromJSON(t, `{
		"five_hour": null,
		"seven_day": {"utilization": 5.0, "resets_at": "2026-11-16T10:00:00.110835+00:00"},
		"seven_day_oauth_apps": null,
		"seven_day_opus": null,
		"seven_day_sonnet": null,
		"extra_usage": {"is_enabled": false},
		"limits": [
			{"kind": "weekly_all", "group": "weekly", "percent": 5,
			 "resets_at": "2026-11-16T10:00:00.110835+00:00", "scope": null},
			{"kind": "weekly_scoped", "group": "weekly", "percent": 9,
			 "resets_at": "2026-11-16T10:00:00.111015+00:00",
			 "scope": {"model": {"id": null, "display_name": "Fable"}, "surface": null}}
		]
	}`)

	assertWindowNames(t, result.Windows, "five_hour", "seven_day", "seven_day_fable")

	if got := result.Windows[0]; got.Utilization != 0 || !got.ResetsAt.IsZero() {
		t.Errorf("five_hour = %+v, want zero utilization and no reset for a null window", got)
	}
	if got := result.Windows[1].Utilization; got != 0.05 {
		t.Errorf("seven_day utilization = %v, want 0.05", got)
	}
	fable := result.Windows[2]
	if fable.Utilization != 0.09 {
		t.Errorf("seven_day_fable utilization = %v, want 0.09", fable.Utilization)
	}
	wantReset := time.Date(2026, 11, 16, 10, 0, 0, 111015000, time.UTC)
	if !fable.ResetsAt.Equal(wantReset) {
		t.Errorf("seven_day_fable resets_at = %v, want %v", fable.ResetsAt, wantReset)
	}
}

func TestClaude_FetchQuota_limitsArrayEdgeCases(t *testing.T) {
	result := fetchQuotaFromJSON(t, `{
		"five_hour": {"utilization": 20, "resets_at": "2026-11-09T20:00:00Z"},
		"seven_day": {"utilization": 40, "resets_at": "2026-11-16T10:00:00Z"},
		"seven_day_sonnet": {"utilization": 30, "resets_at": "2026-11-16T10:00:00Z"},
		"limits": [
			{"kind": "weekly_scoped", "percent": 1, "scope": {"model": {"display_name": "Opus 5.5"}}},
			{"kind": "weekly_scoped", "percent": 150, "scope": {"model": {"id": "claude-next", "display_name": " "}}},
			{"kind": "weekly_scoped", "percent": 2, "scope": {"model": {"display_name": "Opus 5.5"}}},
			{"kind": "weekly_scoped", "percent": 3, "scope": null},
			{"kind": "some_future_kind", "percent": 4}
		]
	}`)

	// No weekly_all entry: seven_day falls back to the legacy field. Scoped
	// limits are present, so the legacy sonnet window is not added.
	assertWindowNames(t, result.Windows, "five_hour", "seven_day", "seven_day_opus_5_5", "seven_day_claude_next")

	if got := result.Windows[1].Utilization; got != 0.4 {
		t.Errorf("seven_day utilization = %v, want 0.4 from the legacy field", got)
	}
	// percent 1 must mean 1%, not 100%.
	if got := result.Windows[2].Utilization; got != 0.01 {
		t.Errorf("seven_day_opus_5_5 utilization = %v, want 0.01", got)
	}
	if got := result.Windows[3].Utilization; got != 1 {
		t.Errorf("seven_day_claude_next utilization = %v, want clamped to 1", got)
	}
}

func TestClaude_FetchQuota_legacyResponseOmitsNullWindows(t *testing.T) {
	result := fetchQuotaFromJSON(t, `{
		"five_hour": {"utilization": 0.1, "resets_at": "2025-03-29T20:00:00Z"},
		"seven_day": {"utilization": 0.2, "resets_at": "2025-04-02T00:00:00Z"},
		"seven_day_sonnet": null
	}`)

	assertWindowNames(t, result.Windows, "five_hour", "seven_day")
}
