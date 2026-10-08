// Package credential reads provider-owned credential stores. It never writes
// to Keychain or copies Keychain secrets to disk.
package credential

import (
	"context"
	"errors"
	"os"
	"time"

	apierrors "github.com/rudolfjs/agent-quota/internal/errors"
	"github.com/rudolfjs/agent-quota/internal/fileutil"
)

// ErrNotFound distinguishes a missing Keychain item from denied/locked access.
// Only a missing item permits fallback to a credentials file.
var ErrNotFound = errors.New("keychain item not found")

// Store supports a secret read and a metadata-only availability check.
type Store interface {
	Read(context.Context) ([]byte, error)
	Exists(context.Context) (bool, error)
}

// Source prefers Keychain when configured, falling back to Path only when the
// item is absent. Explicit provider path overrides use a nil Keychain.
// An empty Path disables file fallback for providers configured as keyring-only.
type Source struct {
	Path     string
	Label    string
	Keychain Store
}

func (s Source) Read(ctx context.Context) (data []byte, fromKeychain bool, err error) {
	if err := ctx.Err(); err != nil {
		return nil, false, apierrors.NewAuthError("credential read cancelled", err)
	}
	var missing error
	if s.Keychain != nil {
		data, err = s.Keychain.Read(ctx)
		if err == nil {
			return data, true, nil
		}
		if !errors.Is(err, ErrNotFound) {
			return nil, false, err
		}
		missing = err
	}
	if s.Path == "" {
		return nil, false, apierrors.NewAuthError(s.Label+" authentication is not configured in the selected credential store", missing)
	}
	fileutil.WarnInsecurePermissions(s.Path)
	data, err = os.ReadFile(s.Path)
	if err != nil {
		if missing != nil && errors.Is(err, os.ErrNotExist) {
			return nil, false, missing
		}
		return nil, false, apierrors.NewConfigError("failed to read "+s.Label+" credentials file", err)
	}
	return data, false, nil
}

// Available never requests a Keychain password (and therefore never prompts).
// An inaccessible store is treated as potentially available so FetchQuota can
// report the actionable error, rather than silently hiding the provider.
func (s Source) Available(validate func([]byte) bool) bool {
	if s.Keychain != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		exists, err := s.Keychain.Exists(ctx)
		if exists || (err != nil && !errors.Is(err, ErrNotFound)) {
			return true
		}
	}
	if s.Path == "" {
		return false
	}
	fileutil.WarnInsecurePermissions(s.Path)
	data, err := os.ReadFile(s.Path)
	return err == nil && validate(data)
}
