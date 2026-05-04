package gemini

import (
	"context"
	"encoding/json"
	"errors"

	apierrors "github.com/rudolfjs/agent-quota/internal/errors"
	"github.com/rudolfjs/agent-quota/internal/keychain"
)

const (
	geminiKeychainService = "gemini-cli-oauth"
	geminiKeychainAccount = "main-account"
)

type keychainReader interface {
	Read(ctx context.Context, service, account string) (string, error)
}

type keychainSource struct {
	reader keychainReader
}

type keychainCredentials struct {
	Token struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken,omitempty"`
		TokenType    string `json:"tokenType,omitempty"`
		Scope        string `json:"scope,omitempty"`
		ExpiresAt    int64  `json:"expiresAt,omitempty"`
	} `json:"token"`
}

func newKeychainSource(reader keychainReader) *keychainSource {
	return &keychainSource{reader: reader}
}

func (k *keychainSource) Read(ctx context.Context) (oauthCredentials, error) {
	raw, err := k.reader.Read(ctx, geminiKeychainService, geminiKeychainAccount)
	if err != nil {
		switch {
		case errors.Is(err, keychain.ErrNotFound):
			return oauthCredentials{}, apierrors.NewAuthError(
				"Gemini CLI is not signed in on this machine; run `gemini` to authenticate",
				err,
			)
		case errors.Is(err, keychain.ErrAccessDenied):
			return oauthCredentials{}, apierrors.NewAuthError(
				`Keychain access denied; grant access to the "gemini-cli-oauth" entry and retry`,
				err,
			)
		default:
			return oauthCredentials{}, apierrors.NewConfigError("failed to read Gemini credentials from Keychain", err)
		}
	}

	var stored keychainCredentials
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		return oauthCredentials{}, apierrors.NewConfigError("failed to parse Gemini credentials from Keychain", err)
	}
	return oauthCredentials{
		AccessToken:  stored.Token.AccessToken,
		RefreshToken: stored.Token.RefreshToken,
		TokenType:    stored.Token.TokenType,
		Scope:        stored.Token.Scope,
		ExpiryDate:   stored.Token.ExpiresAt,
	}, nil
}

func (k *keychainSource) Refresh(ctx context.Context) error {
	return runGeminiCLI(ctx)
}

func defaultKeychainSource() *keychainSource {
	return newKeychainSource(keychain.New())
}
