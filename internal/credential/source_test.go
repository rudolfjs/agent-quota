package credential

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	apierrors "github.com/rudolfjs/agent-quota/internal/errors"
)

type fakeStore struct {
	data   []byte
	err    error
	exists bool
	reads  int
}

func (s *fakeStore) Read(context.Context) ([]byte, error) { s.reads++; return s.data, s.err }
func (s *fakeStore) Exists(context.Context) (bool, error) { return s.exists, s.err }

func TestSourcePrecedence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.json")
	if err := os.WriteFile(path, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	denied := apierrors.NewAuthError("access denied", nil)
	for _, tc := range []struct {
		name     string
		store    Store
		want     string
		keychain bool
		err      error
	}{
		{"file only", nil, "file", false, nil},
		{"keychain first", &fakeStore{data: []byte("keychain")}, "keychain", true, nil},
		{"missing fallback", &fakeStore{err: ErrNotFound}, "file", false, nil},
		{"denied does not fall back", &fakeStore{err: denied}, "", false, denied},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, keychain, err := (Source{Path: path, Keychain: tc.store}).Read(t.Context())
			if string(data) != tc.want || keychain != tc.keychain || !errors.Is(err, tc.err) {
				t.Fatal("unexpected credential source selection")
			}
		})
	}
}

func TestSourceMissingBoth(t *testing.T) {
	missing := apierrors.NewAuthError("not signed in", ErrNotFound)
	_, _, err := (Source{Path: filepath.Join(t.TempDir(), "absent"), Keychain: &fakeStore{err: missing}}).Read(t.Context())
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected missing auth error: %v", err)
	}
}

func TestSourceAvailableDoesNotReadSecrets(t *testing.T) {
	for _, store := range []*fakeStore{{exists: true}, {err: errors.New("locked")}} {
		s := Source{Keychain: store}
		if !s.Available(func([]byte) bool { t.Fatal("file validation should not run"); return false }) {
			t.Fatal("expected potentially available")
		}
		if store.reads != 0 {
			t.Fatal("availability read a password")
		}
	}
}
