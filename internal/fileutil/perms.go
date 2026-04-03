package fileutil

import (
	"log/slog"
	"os"
)

// WarnInsecurePermissions logs a warning if the file at path is readable by
// group or other (mode & 0o077 != 0). This mirrors the behavior of SSH when
// it finds an overly permissive private key file. The function is best-effort:
// if the stat fails it logs a debug entry and returns silently.
func WarnInsecurePermissions(path string) {
	info, err := os.Stat(path)
	if err != nil {
		slog.Debug("could not stat credential file for permission check", "path", path, "error", err)
		return
	}
	if info.Mode()&0o077 != 0 {
		slog.Warn("credential file has insecure permissions — it should be readable only by its owner (0600)",
			"path", path,
			"mode", info.Mode(),
		)
	}
}
