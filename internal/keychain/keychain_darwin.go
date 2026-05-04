//go:build darwin

package keychain

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const defaultSecurityPath = "/usr/bin/security"

// Read invokes `security find-generic-password -s <service> -a <account> -w`
// and returns the password value with trailing whitespace trimmed.
//
// The -w flag tells security to print only the password to stdout, not the
// full attribute dump. We never log the password or stderr at info level —
// stderr can include the account name which is PII-adjacent.
func (r *Reader) Read(ctx context.Context, service, account string) (string, error) {
	bin := r.securityPath
	if bin == "" {
		if env := os.Getenv("AGENT_QUOTA_SECURITY_PATH"); env != "" {
			bin = env
		} else {
			bin = defaultSecurityPath
		}
	}

	args := []string{"find-generic-password", "-s", service}
	if account != "" {
		args = append(args, "-a", account)
	}
	args = append(args, "-w")
	cmd := exec.CommandContext(ctx, bin, args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", ctxErr
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return "", classifyExitError(exitErr.ExitCode(), stderr.String())
		}
		return "", errors.Join(errors.New("keychain: security invocation failed"), err)
	}

	return strings.TrimRight(stdout.String(), "\r\n "), nil
}

// classifyExitError maps known security(1) failures to sentinel errors.
// We pattern-match on stderr for portability across macOS versions.
func classifyExitError(code int, stderr string) error {
	s := strings.ToLower(stderr)
	switch {
	case strings.Contains(s, "specified item could not be found"):
		return ErrNotFound
	case strings.Contains(s, "user interaction is not allowed"),
		strings.Contains(s, "user cancelled"),
		strings.Contains(s, "user canceled"):
		return ErrAccessDenied
	case code == 44:
		return ErrNotFound
	case code == 51, code == -128:
		return ErrAccessDenied
	default:
		// Never include raw stderr in the error message — it can contain
		// service or account names. Surface the exit code only.
		return fmt.Errorf("keychain: security exited with non-zero status: %d", code)
	}
}
