package claude

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
		payload: `{"claudeAiOauth":{"accessToken":"tok","refreshToken":"r","expiresAt":9999999999999,"scopes":["s"],"subscriptionType":"pro"}}`,
	})
	creds, err := src.Read(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if creds.AccessToken != "tok" {
		t.Fatalf("got %q", creds.AccessToken)
	}
}

func TestKeychainSource_Read_ItemNotFound_ReturnsAuthError(t *testing.T) {
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
	c := New(WithCredentialsPath("/tmp/test-creds.json"))
	if _, ok := c.source.(fileSource); !ok {
		t.Fatalf("expected fileSource when WithCredentialsPath set, got %T", c.source)
	}
}
