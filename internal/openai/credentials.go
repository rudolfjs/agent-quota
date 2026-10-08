package openai

import (
	"crypto/sha256"
	"fmt"
	"path/filepath"

	"github.com/rudolfjs/agent-quota/internal/credential"
)

func codexKeychain(home string) credential.Store {
	return credential.NewKeychain("Codex Auth", codexAccount(home), "Codex")
}

// Codex keys its auth entry by the first 16 hex characters of SHA-256 of the
// canonical CODEX_HOME path, not by the macOS username.
func codexAccount(home string) string {
	if canonical, err := filepath.EvalSymlinks(home); err == nil {
		if absolute, err := filepath.Abs(canonical); err == nil {
			home = absolute
		}
	}
	digest := sha256.Sum256([]byte(home))
	return fmt.Sprintf("cli|%x", digest[:8])
}
