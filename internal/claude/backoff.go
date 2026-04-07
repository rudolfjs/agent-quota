package claude

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/schnetlerr/agent-quota/internal/fileutil"
)

type backoffState struct {
	RetryAfterEnd time.Time `json:"retry_after_end"`
}

// defaultBackoffPath returns the default path to the Claude backoff state file.
func defaultBackoffPath() (string, error) {
	if dir, err := os.UserConfigDir(); err == nil && dir != "" {
		return filepath.Join(dir, "agent-quota", "claude_backoff.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory for Claude backoff state: %w", err)
	}
	return filepath.Join(home, ".config", "agent-quota", "claude_backoff.json"), nil
}

// readBackoffState reads the backoff state from the given path.
// It returns a zero time if the file does not exist or cannot be read.
func readBackoffState(path string) time.Time {
	data, err := os.ReadFile(path)
	if err != nil {
		return time.Time{}
	}
	var state backoffState
	if err := json.Unmarshal(data, &state); err != nil {
		return time.Time{}
	}
	return state.RetryAfterEnd
}

// saveBackoffState saves the given retry-after deadline to the specified path.
func saveBackoffState(path string, end time.Time) error {
	state := backoffState{RetryAfterEnd: end}
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return fileutil.AtomicWriteFile(path, data, 0o600)
}

// clearBackoffState removes the backoff state file if it exists.
func clearBackoffState(path string) error {
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
