package credential

import (
	"context"
	"errors"
	"io"
	"runtime"
	"slices"
	"strings"
	"testing"

	apierrors "github.com/rudolfjs/agent-quota/internal/errors"
)

type exitStatus int

func (e exitStatus) Error() string { return "secret-command-output" }
func (e exitStatus) ExitCode() int { return int(e) }

func TestNewKeychainPlatformSelection(t *testing.T) {
	store := NewKeychain("test-service", "test-account", "Test")
	if (store != nil) != (runtime.GOOS == "darwin") {
		t.Fatal("Keychain must only be enabled on macOS")
	}
}

func TestKeychainReadAndMetadata(t *testing.T) {
	var calls [][]string
	k := &keychain{service: "test service", account: "cli|1234", label: "Test", run: func(ctx context.Context, args []string) ([]byte, error) {
		calls = append(calls, slices.Clone(args))
		return []byte("  {\"token\":\"fixture\"}\n"), nil
	}}
	exists, err := k.Exists(t.Context())
	if err != nil || !exists {
		t.Fatal("metadata lookup failed")
	}
	data, err := k.Read(t.Context())
	if err != nil || string(data) != `{"token":"fixture"}` {
		t.Fatal("secret lookup failed")
	}
	base := []string{"find-generic-password", "-s", "test service", "-a", "cli|1234"}
	if !slices.Equal(calls[0], base) || !slices.Equal(calls[1], append(base, "-w")) {
		t.Fatalf("unexpected security arguments: %v", calls)
	}
}

func TestKeychainErrorsAreSafe(t *testing.T) {
	for _, tc := range []struct {
		code    int
		kind    string
		missing bool
	}{
		{44, "auth", true}, {51, "auth", false}, {36, "auth", false},
		{53, "auth", false}, {128, "auth", false}, {1, "config", false},
		{35, "config", false}, {37, "config", false},
	} {
		k := &keychain{label: "Test", run: func(context.Context, []string) ([]byte, error) {
			return []byte("secret-stdout"), exitStatus(tc.code)
		}}
		data, err := k.Read(t.Context())
		var dom *apierrors.DomainError
		if data != nil || !errors.As(err, &dom) || dom.Kind != tc.kind || errors.Is(err, ErrNotFound) != tc.missing {
			t.Fatalf("incorrect error mapping for exit %d", tc.code)
		}
		for e := err; e != nil; e = errors.Unwrap(e) {
			if strings.Contains(e.Error(), "secret-") {
				t.Fatal("command output leaked into error chain")
			}
		}
	}
}

func TestKeychainMissingMetadata(t *testing.T) {
	k := &keychain{run: func(context.Context, []string) ([]byte, error) { return nil, exitStatus(44) }}
	if exists, err := k.Exists(t.Context()); exists || err != nil {
		t.Fatal("missing metadata should not be an error")
	}
}

func TestKeychainCancellation(t *testing.T) {
	k := &keychain{run: func(ctx context.Context, _ []string) ([]byte, error) { <-ctx.Done(); return nil, ctx.Err() }}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := k.Read(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation: %v", err)
	}
}

func TestBoundedOutput(t *testing.T) {
	var b boundedBuffer
	_, err := io.Copy(&b, strings.NewReader(strings.Repeat("x", (1<<20)+1)))
	if err == nil || len(b.Bytes()) > 1<<20 {
		t.Fatal("credential output was not bounded")
	}
}
