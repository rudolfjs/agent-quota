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
	if err := clearBackoffState(path); err != nil {
		t.Fatalf("clearBackoffState() error = %v", err)
	}
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

// TestBackoffState_maxCap verifies that saveBackoffState (or whatever capping
// mechanism exists) enforces a maximum duration on the persisted deadline.
// If the API sends Retry-After: 86400, the saved backoff should be capped
// at a sane maximum (e.g. 5 minutes) — not 24 hours.
func TestBackoffState_maxCap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "backoff.json")

	// Save a backoff deadline 24 hours in the future — way too long.
	absurdEnd := time.Now().Add(24 * time.Hour)
	if err := saveBackoffState(path, absurdEnd); err != nil {
		t.Fatalf("saveBackoffState: %v", err)
	}

	// Read back the persisted value.
	readEnd := readBackoffState(path)
	if readEnd.IsZero() {
		t.Fatal("expected non-zero backoff time after save")
	}

	// The maximum cap should be 5 minutes.
	maxCap := 5 * time.Minute
	remaining := time.Until(readEnd)

	if remaining > maxCap+10*time.Second {
		t.Fatalf("BUG: readBackoffState returned a deadline %v in the future; "+
			"saveBackoffState should cap the duration at %v, but it saved the full 24h value",
			remaining.Round(time.Second), maxCap)
	}
}
