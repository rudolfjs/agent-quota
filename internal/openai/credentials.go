package openai

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"

	"github.com/rudolfjs/agent-quota/internal/credential"
	apierrors "github.com/rudolfjs/agent-quota/internal/errors"
)

// Read the root setting on each fetch so switching Codex accounts/storage does
// not leave a stale Keychain entry authoritative. Explicit auth paths bypass it.
func (o *OpenAI) credentialSource(ctx context.Context) (credential.Source, error) {
	source := o.credentials
	if o.configPath == "" {
		return source, nil
	}
	mode, err := readCredentialStoreMode(ctx, o.configPath)
	if err != nil {
		return credential.Source{}, err
	}
	switch mode {
	case "file":
		source.Keychain = nil
	case "keyring":
		if source.Keychain == nil {
			return credential.Source{}, apierrors.NewConfigError("Codex keyring storage is only supported on macOS", nil)
		}
		source.Path = ""
	case "auto":
		// Retain file fallback for an absent item. Denied/locked Keychain access
		// still surfaces an auth error, as for the other providers.
	case "ephemeral":
		return credential.Source{}, apierrors.NewAuthError("Codex uses in-memory credentials; no saved login is available to agent-quota", nil)
	}
	return source, nil
}

func readCredentialStoreMode(ctx context.Context, path string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", apierrors.NewConfigError("Codex settings read cancelled", err)
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "file", nil
	}
	if err != nil {
		return "", apierrors.NewConfigError("failed to read Codex settings file", err)
	}
	settings := struct {
		Store string `toml:"cli_auth_credentials_store"`
	}{Store: "file"}
	if err := toml.Unmarshal(data, &settings); err != nil {
		// TOML diagnostics can contain sensitive values from elsewhere in config.
		return "", apierrors.NewConfigError("failed to parse Codex settings file", errors.New("invalid Codex settings TOML"))
	}
	switch settings.Store {
	case "file", "keyring", "auto", "ephemeral":
		return settings.Store, nil
	default:
		return "", apierrors.NewConfigError("invalid Codex cli_auth_credentials_store; expected file, keyring, auto, or ephemeral", nil)
	}
}

func codexKeychain(home string) credential.Store {
	return credential.NewKeychain("Codex Auth", codexAccount(home), "Codex")
}

// Codex keys its auth entry by the first 16 hex characters of SHA-256 of the
// canonical CODEX_HOME path, not by the macOS username.
func codexAccount(home string) string {
	if canonical, err := filepath.EvalSymlinks(home); err == nil {
		if absolute, err := filepath.Abs(canonical); err == nil {
			home = absolute
		}
	}
	digest := sha256.Sum256([]byte(home))
	return fmt.Sprintf("cli|%x", digest[:8])
}
