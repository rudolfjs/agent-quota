package claude

import (
	"context"
	"encoding/json"
	"errors"

	apierrors "github.com/rudolfjs/agent-quota/internal/errors"
	"github.com/rudolfjs/agent-quota/internal/keychain"
)

const claudeKeychainService = "Claude Code-credentials"

// keychainReader is the minimal interface keychainSource needs.
// Defined here (not in package keychain) so tests can fake it without import cycles.
type keychainReader interface {
	Read(ctx context.Context, service, account string) (string, error)
}

type keychainSource struct {
	reader keychainReader
}

func newKeychainSource(reader keychainReader) *keychainSource {
	return &keychainSource{reader: reader}
}

func (k *keychainSource) Read(ctx context.Context) (*OAuthCredentials, error) {
	// account="" finds the first entry for this service (most users have one account).
	raw, err := k.reader.Read(ctx, claudeKeychainService, "")
	if err != nil {
		switch {
		case errors.Is(err, keychain.ErrNotFound):
			return nil, apierrors.NewAuthError(
				"Claude Code is not signed in on this machine; run `claude` to authenticate",
				err,
			)
		case errors.Is(err, keychain.ErrAccessDenied):
			return nil, apierrors.NewAuthError(
				`Keychain access denied; grant access to the "Claude Code-credentials" entry and retry`,
				err,
			)
		default:
			return nil, apierrors.NewConfigError("failed to read Claude credentials from Keychain", err)
		}
	}

	var f credentialsFile
	if err := json.Unmarshal([]byte(raw), &f); err != nil {
		return nil, apierrors.NewConfigError("failed to parse Claude credentials from Keychain", err)
	}
	return &f.ClaudeAIOAuth, nil
}

func (k *keychainSource) Refresh(ctx context.Context) error {
	// The claude CLI updates the Keychain entry on macOS directly.
	// No file-based change detection needed.
	return runClaudeCLI(ctx)
}

// defaultKeychainSource returns a production keychainSource using the real
// /usr/bin/security binary (or $AGENT_QUOTA_SECURITY_PATH override).
func defaultKeychainSource() *keychainSource {
	return newKeychainSource(keychain.New())
}
