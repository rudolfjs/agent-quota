package gemini

import (
	"context"
	"errors"
	"testing"

	apierrors "github.com/rudolfjs/agent-quota/internal/errors"
	"github.com/rudolfjs/agent-quota/internal/keychain"
)

type fakeKeychain struct {
	payload string
	err     error
}

func (f fakeKeychain) Read(_ context.Context, _, _ string) (string, error) {
	return f.payload, f.err
}

func TestKeychainSource_Read_HappyPath(t *testing.T) {
	src := newKeychainSource(fakeKeychain{
		payload: `{"serverName":"main-account","token":{"accessToken":"tok","refreshToken":"ref","tokenType":"Bearer","scope":"s","expiresAt":9999999999999},"updatedAt":1}`,
	})

	creds, err := src.Read(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if creds.AccessToken != "tok" {
		t.Fatalf("AccessToken = %q, want tok", creds.AccessToken)
	}
	if creds.RefreshToken != "ref" {
		t.Fatalf("RefreshToken = %q, want ref", creds.RefreshToken)
	}
	if creds.ExpiryDate != 9999999999999 {
		t.Fatalf("ExpiryDate = %d, want 9999999999999", creds.ExpiryDate)
	}
}

func TestKeychainSource_Read_NotFound_ReturnsAuthError(t *testing.T) {
	src := newKeychainSource(fakeKeychain{err: keychain.ErrNotFound})
	_, err := src.Read(t.Context())
	var de *apierrors.DomainError
	if !errors.As(err, &de) || de.Kind != "auth" {
		t.Fatalf("want auth domain error, got %v", err)
	}
}

func TestKeychainSource_Read_AccessDenied_ReturnsAuthError(t *testing.T) {
	src := newKeychainSource(fakeKeychain{err: keychain.ErrAccessDenied})
	_, err := src.Read(t.Context())
	var de *apierrors.DomainError
	if !errors.As(err, &de) || de.Kind != "auth" {
		t.Fatalf("want auth domain error, got %v", err)
	}
}

func TestKeychainSource_Read_MalformedJSON_ReturnsConfigError(t *testing.T) {
	src := newKeychainSource(fakeKeychain{payload: `{bad json`})
	_, err := src.Read(t.Context())
	var de *apierrors.DomainError
	if !errors.As(err, &de) || de.Kind != "config" {
		t.Fatalf("want config domain error, got %v", err)
	}
}

func TestNew_WithCredentialsPath_UsesFileSource(t *testing.T) {
	g := New(WithCredentialsPath("/tmp/gemini-creds.json"))
	if _, ok := g.source.(fileSource); !ok {
		t.Fatalf("expected fileSource when WithCredentialsPath set, got %T", g.source)
	}
}
