package claude_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/schnetlerr/agent-quota/internal/claude"
)

// writeCredFile writes a credentials JSON file to dir/.credentials.json.
func writeCredFile(t *testing.T, dir string, payload map[string]any) string {
	t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, ".credentials.json")
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestReadCredentials_validFile(t *testing.T) {
	dir := t.TempDir()
	expiresAt := time.Now().Add(time.Hour).UnixMilli()
	writeCredFile(t, dir, map[string]any{
		"claudeAiOauth": map[string]any{
			"accessToken":      "tok_valid",
			"refreshToken":     "ref_valid",
			"expiresAt":        expiresAt,
			"scopes":           []string{"api"},
			"subscriptionType": "max",
			"rateLimitTier":    "max_claude_pro",
		},
	})

	creds, err := claude.ReadCredentials(filepath.Join(dir, ".credentials.json"))
	if err != nil {
		t.Fatalf("ReadCredentials: %v", err)
	}
	if creds.AccessToken != "tok_valid" {
		t.Errorf("AccessToken = %q, want %q", creds.AccessToken, "tok_valid")
	}
	if creds.SubscriptionType != "max" {
		t.Errorf("SubscriptionType = %q, want %q", creds.SubscriptionType, "max")
	}
}

func TestReadCredentials_missingFile(t *testing.T) {
	_, err := claude.ReadCredentials("/nonexistent/.credentials.json")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestReadCredentials_malformedJSON(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".credentials.json")
	if err := os.WriteFile(p, []byte(`{bad json`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := claude.ReadCredentials(p)
	if err == nil {
		t.Fatal("expected error for malformed JSON, got nil")
	}
}

func TestCredentials_IsExpired_future(t *testing.T) {
	creds := claude.OAuthCredentials{
		AccessToken: "tok",
		ExpiresAt:   time.Now().Add(time.Hour).UnixMilli(),
	}
	if creds.IsExpired() {
		t.Error("credentials with future expiry should not be expired")
	}
}

func TestCredentials_IsExpired_past(t *testing.T) {
	creds := claude.OAuthCredentials{
		AccessToken: "tok",
		ExpiresAt:   time.Now().Add(-time.Minute).UnixMilli(),
	}
	if !creds.IsExpired() {
		t.Error("credentials with past expiry should be expired")
	}
}

func TestCredentials_IsExpired_zeroMeansExpired(t *testing.T) {
	creds := claude.OAuthCredentials{AccessToken: "tok", ExpiresAt: 0}
	if !creds.IsExpired() {
		t.Error("zero ExpiresAt should be treated as expired")
	}
}
