package credential

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"time"

	apierrors "github.com/rudolfjs/agent-quota/internal/errors"
)

// NewKeychain returns a macOS reader, or nil on other platforms. Using the
// system security utility avoids CGO and permits cross-compiled Mac releases.
// Access prompts belong to security, and are controlled by the item's ACL.
func NewKeychain(service, account, label string) Store {
	if runtime.GOOS != "darwin" {
		return nil
	}
	return &keychain{service: service, account: account, label: label, run: runSecurity}
}

type keychain struct {
	service string
	account string
	label   string
	run     func(context.Context, []string) ([]byte, error)
}

// Serialize password reads to avoid simultaneous OS permission dialogs.
var promptSlot = make(chan struct{}, 1)

func (k *keychain) Read(ctx context.Context) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	select {
	case promptSlot <- struct{}{}:
		defer func() { <-promptSlot }()
	case <-ctx.Done():
		return nil, k.commandError(ctx, ctx.Err())
	}
	return k.query(ctx, true)
}

func (k *keychain) Exists(ctx context.Context) (bool, error) {
	_, err := k.query(ctx, false)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}

func (k *keychain) query(ctx context.Context, password bool) ([]byte, error) {
	args := []string{"find-generic-password", "-s", k.service, "-a", k.account}
	if password {
		args = append(args, "-w")
	}
	data, err := k.run(ctx, args)
	if err != nil {
		return nil, k.commandError(ctx, err)
	}
	if !password {
		return nil, nil // metadata is not used or logged
	}
	return bytes.TrimSpace(data), nil
}

func (k *keychain) commandError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return apierrors.NewAuthError("Keychain read cancelled or timed out; unlock your login Keychain and retry", ctx.Err())
	}
	// security returns OSStatus modulo 256. Do not include stdout/stderr (or
	// an exec.ExitError containing stderr) in the error chain or diagnostics.
	var exit interface{ ExitCode() int }
	if errors.As(err, &exit) {
		code := exit.ExitCode()
		switch code {
		case 44: // errSecItemNotFound (-25300)
			return apierrors.NewAuthError(k.label+" is not signed in on this machine; sign in with its CLI, then retry", ErrNotFound)
		case 35, 36, 37, 128: // auth failed, interaction disallowed, unavailable, user cancelled
			return apierrors.NewAuthError("Grant Keychain access for the "+k.label+" entry and unlock your login Keychain, then retry", fmt.Errorf("security exit status %d", code))
		default:
			return apierrors.NewConfigError("failed to read "+k.label+" from macOS Keychain", fmt.Errorf("security exit status %d", code))
		}
	}
	return apierrors.NewConfigError("could not run /usr/bin/security to read macOS Keychain", errors.New("keychain command failed"))
}

func runSecurity(ctx context.Context, args []string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "/usr/bin/security", args...)
	var out boundedBuffer
	cmd.Stdout = &out
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// Credential blobs are small; bound output even if a store is corrupt.
type boundedBuffer struct{ data bytes.Buffer }

func (b *boundedBuffer) Bytes() []byte { return b.data.Bytes() }

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if b.data.Len()+len(p) > 1<<20 {
		return 0, errors.New("keychain output too large")
	}
	return b.data.Write(p)
}
