package gemini

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"time"

	apierrors "github.com/schnetlerr/agent-quota/internal/errors"
)

// resolveGeminiBinary returns the path to the gemini CLI binary.
// It checks AGENT_QUOTA_GEMINI_PATH first, then falls back to a PATH lookup.
func resolveGeminiBinary() (string, error) {
	if p := os.Getenv("AGENT_QUOTA_GEMINI_PATH"); p != "" {
		slog.Debug("using gemini binary from env override", "path", p)
		return p, nil
	}
	path, err := exec.LookPath("gemini")
	if err != nil {
		return "", err
	}
	slog.Debug("resolved gemini binary", "path", path)
	return path, nil
}

// RefreshToken runs the gemini CLI in headless mode to trigger an OAuth
// token refresh, then waits up to 5 seconds for the credentials file to
// be updated. Returns a domain error if the binary is missing or the
// credentials file is not updated after the CLI exits.
func RefreshToken(ctx context.Context, credPath string) error {
	var oldMtime time.Time
	if info, err := os.Stat(credPath); err == nil {
		oldMtime = info.ModTime()
	}

	geminiPath, err := resolveGeminiBinary()
	if err != nil {
		return apierrors.NewAuthError("gemini CLI not found; cannot refresh token", err)
	}

	cmd := exec.CommandContext(ctx, geminiPath, "-p", "")
	out, execErr := cmd.CombinedOutput()
	if execErr != nil {
		slog.Debug("gemini CLI exec completed with error", "error", execErr, "output_bytes", len(out))
	}

	// Check immediately whether the credentials file was updated.
	info, statErr := os.Stat(credPath)
	if statErr == nil && info.ModTime().After(oldMtime) {
		return nil
	}

	// If the CLI failed and creds weren't updated, return immediately.
	if execErr != nil {
		return apierrors.NewAuthError("Gemini authentication expired; run `gemini` to re-authenticate", execErr)
	}

	// CLI succeeded but creds not yet on disk — poll briefly for async write.
	deadline := time.Now().Add(5 * time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			info, statErr := os.Stat(credPath)
			if statErr == nil && info.ModTime().After(oldMtime) {
				return nil
			}
		}
	}

	return nil
}
