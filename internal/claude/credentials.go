package claude

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"time"

	"github.com/rudolfjs/agent-quota/internal/credential"
	apierrors "github.com/rudolfjs/agent-quota/internal/errors"
	"golang.org/x/text/unicode/norm"
)

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
	data, _, err := (credential.Source{Path: path, Label: "Claude"}).Read(context.Background())
	if err != nil {
		return OAuthCredentials{}, err
	}
	return parseCredentials(data)
}

func parseCredentials(data []byte) (OAuthCredentials, error) {
	var f credentialsFile
	if err := json.Unmarshal(data, &f); err != nil {
		// JSON type errors can contain credential values; discard the raw error.
		return OAuthCredentials{}, apierrors.NewConfigError("failed to parse Claude credentials", errors.New("invalid credential JSON"))
	}
	return f.ClaudeAIOAuth, nil
}

func (c *Claude) readCredentials(ctx context.Context) (OAuthCredentials, bool, error) {
	data, fromKeychain, err := c.credentials.Read(ctx)
	if err != nil {
		return OAuthCredentials{}, false, err
	}
	creds, err := parseCredentials(data)
	if err == nil && creds.AccessToken == "" {
		err = apierrors.NewAuthError("Claude authentication is not configured; run `claude` to sign in", nil)
	}
	return creds, fromKeychain, err
}

func defaultKeychain() credential.Store {
	service, account := keychainIdentity()
	return credential.NewKeychain(service, account, "Claude Code")
}

func keychainIdentity() (service, account string) {
	service = "Claude Code-credentials"
	configDir := os.Getenv("CLAUDE_CONFIG_DIR")
	if secureDir, set := os.LookupEnv("CLAUDE_SECURESTORAGE_CONFIG_DIR"); set {
		configDir = secureDir
	}
	if configDir != "" {
		hash := sha256.Sum256([]byte(norm.NFC.String(configDir)))
		service += fmt.Sprintf("-%x", hash[:4])
	}
	account = os.Getenv("USER")
	if account == "" {
		if current, err := user.Current(); err == nil {
			account = current.Username
		} else {
			account = "claude-code-user"
		}
	}
	return service, account
}

// DefaultCredentialsPath returns the default path to the Claude credentials file.
// Returns an error if the user home directory cannot be determined.
func DefaultCredentialsPath() (string, error) {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, ".credentials.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory for Claude credentials: %w", err)
	}
	return home + "/.claude/.credentials.json", nil
}
