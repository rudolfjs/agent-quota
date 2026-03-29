package claude

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"time"

	apierrors "github.com/schnetlerr/agent-quota/internal/errors"
	"github.com/schnetlerr/agent-quota/internal/version"
)

// RefreshToken runs the claude CLI (no args) to trigger an OAuth token refresh,
// then waits up to 3 seconds for the credentials file to be updated.
// Returns a domain error if exec fails.
func RefreshToken(ctx context.Context, credPath string) error {
	// Record the current mtime so we can detect when the file is updated.
	var oldMtime time.Time
	if info, err := os.Stat(credPath); err == nil {
		oldMtime = info.ModTime()
	}

	claudePath, err := version.ResolveClaudeBinary()
	if err != nil {
		return apierrors.NewAuthError("claude CLI not found; cannot refresh token", err)
	}
	cmd := exec.CommandContext(ctx, claudePath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		slog.Debug("claude CLI exec failed", "error", err, "output_bytes", len(out))
		return apierrors.NewAuthError("failed to refresh Claude token", err)
	}

	// Wait up to 3s for the credentials file mtime to change.
	deadline := time.Now().Add(3 * time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			info, err := os.Stat(credPath)
			if err == nil && info.ModTime().After(oldMtime) {
				return nil
			}
		}
	}

	return nil
}
