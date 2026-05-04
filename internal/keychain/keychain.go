// Package keychain reads OAuth tokens from the macOS system Keychain
// via /usr/bin/security. On non-darwin platforms every Read returns
// ErrUnsupported.
package keychain

import (
	"errors"
)

var (
	// ErrNotFound is returned when the requested service/account combination
	// does not exist in the Keychain.
	ErrNotFound = errors.New("keychain: item not found")

	// ErrAccessDenied is returned when the user denies the Keychain access
	// prompt (or the session is non-interactive and access is not granted).
	ErrAccessDenied = errors.New("keychain: access denied by user")

	// ErrUnsupported is returned on platforms where Keychain is not available.
	ErrUnsupported = errors.New("keychain: unsupported platform")
)

// Reader reads secrets from the macOS Keychain. The zero value is unusable;
// always construct via New.
type Reader struct {
	// securityPath overrides the path to the security(1) binary. Tests set
	// this via WithSecurityPath; production resolves "/usr/bin/security".
	securityPath string
}

// Option configures a Reader.
type Option func(*Reader)

// WithSecurityPath overrides the path to the security(1) binary. Used by
// tests to inject a fake shim; not for production callers.
func WithSecurityPath(p string) Option {
	return func(r *Reader) { r.securityPath = p }
}

// New constructs a Reader. On darwin it resolves the security binary via
// $AGENT_QUOTA_SECURITY_PATH, falling back to /usr/bin/security.
func New(opts ...Option) *Reader {
	r := &Reader{}
	for _, o := range opts {
		o(r)
	}
	return r
}
