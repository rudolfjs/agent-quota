package openai

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	apierrors "github.com/rudolfjs/agent-quota/internal/errors"
	"github.com/rudolfjs/agent-quota/internal/keychain"
)

type fakeKeychainReader struct {
	payload string
	err     error
}

func (f fakeKeychainReader) Read(_ context.Context, _, _ string) (string, error) {
	return f.payload, f.err
}

func validAuthJSON(t *testing.T) string {
	t.Helper()
	data, err := json.Marshal(authFile{
		AuthMode: "oauth",
		Tokens: authTokens{
			AccessToken:  "at-test",
			RefreshToken: "rt-test",
			IDToken:      "id-test",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestKeychainSource_Read_HappyPath(t *testing.T) {
	o := New(WithKeychainReader(
		fakeKeychainReader{payload: validAuthJSON(t)},
		"cli|test16chars1",
	))
	if !o.Available() {
		t.Fatal("Available() = false, want true")
	}
}

func TestKeychainSource_Read_NotFound_ReturnsAuthError(t *testing.T) {
	o := New(WithKeychainReader(
		fakeKeychainReader{err: keychain.ErrNotFound},
		"cli|test16chars1",
	))
	_, err := o.readAuth(t.Context())
	var de *apierrors.DomainError
	if !errors.As(err, &de) || de.Kind != "auth" {
		t.Fatalf("want auth domain error, got %v", err)
	}
}

func TestKeychainSource_Read_AccessDenied_ReturnsAuthError(t *testing.T) {
	o := New(WithKeychainReader(
		fakeKeychainReader{err: keychain.ErrAccessDenied},
		"cli|test16chars1",
	))
	_, err := o.readAuth(t.Context())
	var de *apierrors.DomainError
	if !errors.As(err, &de) || de.Kind != "auth" {
		t.Fatalf("want auth domain error, got %v", err)
	}
}

func TestWithAuthPath_UsesFileSource(t *testing.T) {
	o := New(WithAuthPath(filepath.Join(t.TempDir(), "auth.json")))
	if o.keychainSource != nil {
		t.Fatal("expected keychainSource to be nil when WithAuthPath is used on Linux")
	}
}

func TestCodexKeychainAccount_IsReproducible(t *testing.T) {
	a1, err := codexKeychainAccount()
	if err != nil {
		t.Fatal(err)
	}
	a2, err := codexKeychainAccount()
	if err != nil {
		t.Fatal(err)
	}
	if a1 != a2 {
		t.Fatalf("codexKeychainAccount not deterministic: %q != %q", a1, a2)
	}
	if len(a1) != len("cli|")+16 {
		t.Fatalf("account length = %d, want %d", len(a1), len("cli|")+16)
	}
}
