package claude

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBackoffState(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "backoff.json")

	// Read non-existent state
	end := readBackoffState(path)
	if !end.IsZero() {
		t.Fatalf("expected zero time for non-existent backoff file, got %v", end)
	}

	// Save backoff state
	now := time.Now()
	expectedEnd := now.Add(5 * time.Minute)
	if err := saveBackoffState(path, expectedEnd); err != nil {
		t.Fatalf("failed to save backoff state: %v", err)
	}

	// Verify file permissions (should be 0o600)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("failed to stat backoff file: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("backoff file permissions = %v, want 0600", info.Mode().Perm())
	}

	// Read saved state
	readEnd := readBackoffState(path)
	// Compare Unix milliseconds instead of using time.Equal because
	// json marshal/unmarshal truncates to nanoseconds or milliseconds.
	if readEnd.UnixMilli() != expectedEnd.UnixMilli() {
		t.Fatalf("read backoff end ms %v, want %v", readEnd.UnixMilli(), expectedEnd.UnixMilli())
	}

	// Clear backoff state
	clearBackoffState(path)
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected backoff file to be removed, err = %v", err)
	}

	// Read cleared state
	clearedEnd := readBackoffState(path)
	if !clearedEnd.IsZero() {
		t.Fatalf("expected zero time after clearing backoff file, got %v", clearedEnd)
	}
}

func TestSaveBackoffState_CreatesDirectory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "dir", "backoff.json")

	expectedEnd := time.Now().Add(5 * time.Minute)
	if err := saveBackoffState(path, expectedEnd); err != nil {
		t.Fatalf("failed to save backoff state: %v", err)
	}

	// Read saved state
	readEnd := readBackoffState(path)
	// Compare Unix milliseconds instead of using time.Equal because
	// json marshal/unmarshal truncates to nanoseconds or milliseconds.
	if readEnd.UnixMilli() != expectedEnd.UnixMilli() {
		t.Fatalf("read backoff end ms %v, want %v", readEnd.UnixMilli(), expectedEnd.UnixMilli())
	}
}
