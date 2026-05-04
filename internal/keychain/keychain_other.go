//go:build !darwin

package keychain

import "context"

// Read returns ErrUnsupported on non-darwin platforms.
func (r *Reader) Read(_ context.Context, _, _ string) (string, error) {
	return "", ErrUnsupported
}
