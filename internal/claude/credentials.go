package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	apierrors "github.com/rudolfjs/agent-quota/internal/errors"
	"github.com/rudolfjs/agent-quota/internal/fileutil"
)

// credentialSource abstracts where Claude OAuth credentials live.
// Two concrete implementations: fileSource (Linux/default) and keychainSource (darwin).
type credentialSource interface {
	Read(ctx context.Context) (*OAuthCredentials, error)
	Refresh(ctx context.Context) error
}

// fileSource reads credentials from ~/.claude/.credentials.json.
type fileSource struct {
	path string
}

func (f fileSource) Read(_ context.Context) (*OAuthCredentials, error) {
	creds, err := ReadCredentials(f.path)
	if err != nil {
		return nil, apierrors.NewConfigError("failed to read Claude credentials", err)
	}
	return &creds, nil
}

func (f fileSource) Refresh(ctx context.Context) error {
	return RefreshToken(ctx, f.path)
}

// credentialsFile mirrors the structure of ~/.claude/.credentials.json.
type credentialsFile struct {
	ClaudeAIOAuth OAuthCredentials `json:"claudeAiOauth"`
}

// OAuthCredentials holds the Claude OAuth token data.
type OAuthCredentials struct {
	AccessToken      string   `json:"accessToken"`
	RefreshToken     string   `json:"refreshToken"`
	ExpiresAt        int64    `json:"expiresAt"` // epoch milliseconds
	Scopes           []string `json:"scopes"`
	SubscriptionType string   `json:"subscriptionType"` // "max", "pro", "free", etc.
	RateLimitTier    string   `json:"rateLimitTier"`
}

// IsExpired reports whether the access token has expired or has no expiry set.
// A 60-second buffer is applied so tokens aren't used right at the edge.
func (c OAuthCredentials) IsExpired() bool {
	if c.ExpiresAt == 0 {
		return true
	}
	expiry := time.UnixMilli(c.ExpiresAt)
	return time.Now().After(expiry.Add(-60 * time.Second))
}

// ReadCredentials reads and parses OAuth credentials from the given file path.
// Returns a domain-safe error (wrapping the raw cause) on any failure.
func ReadCredentials(path string) (OAuthCredentials, error) {
	fileutil.WarnInsecurePermissions(path)
	data, err := os.ReadFile(path)
	if err != nil {
		return OAuthCredentials{}, fmt.Errorf("read credentials file: %w", err)
	}
	var f credentialsFile
	if err := json.Unmarshal(data, &f); err != nil {
		return OAuthCredentials{}, fmt.Errorf("parse credentials file: %w", err)
	}
	return f.ClaudeAIOAuth, nil
}

// DefaultCredentialsPath returns the default path to the Claude credentials file.
// Returns an error if the user home directory cannot be determined.
func DefaultCredentialsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory for Claude credentials: %w", err)
	}
	return home + "/.claude/.credentials.json", nil
}
