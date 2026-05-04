package openai

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

// codexKeychainAccount computes the Keychain account key used by the Codex CLI.
// Codex uses: "cli|" + first 16 hex chars of SHA-256(canonical_codex_home_path).
func codexKeychainAccount() (string, error) {
	home := os.Getenv("CODEX_HOME")
	if home == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("cannot determine home directory for Codex: %w", err)
		}
		home = filepath.Join(userHome, ".codex")
	}

	// Codex resolves symlinks before hashing to get the canonical path.
	canonical, err := filepath.EvalSymlinks(home)
	if err != nil {
		// If the directory doesn't exist yet, use the unresolved path.
		canonical = home
	}

	sum := sha256.Sum256([]byte(canonical))
	hex16 := hex.EncodeToString(sum[:])[:16]
	return "cli|" + hex16, nil
}
