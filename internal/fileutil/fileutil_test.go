package fileutil_test

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rudolfjs/agent-quota/internal/fileutil"
)

// TestAtomicWriteFile_writesCorrectContent verifies that AtomicWriteFile
// produces a file with the expected content and permissions.
func TestAtomicWriteFile_writesCorrectContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "creds.json")
	data := []byte(`{"token":"abc"}`)

	if err := fileutil.AtomicWriteFile(path, data, 0o600); err != nil {
		t.Fatalf("AtomicWriteFile: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Errorf("file content = %q, want %q", got, data)
	}
}

// TestAtomicWriteFile_setsPermissions verifies that the written file has
// the requested permission mode (0600 for credentials).
func TestAtomicWriteFile_setsPermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "creds.json")

	if err := fileutil.AtomicWriteFile(path, []byte(`{}`), 0o600); err != nil {
		t.Fatalf("AtomicWriteFile: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("file mode = %04o, want 0600", mode)
	}
}

// TestAtomicWriteFile_leavesNoTempFileOnSuccess verifies the temp file is
// cleaned up after a successful rename.
func TestAtomicWriteFile_leavesNoTempFileOnSuccess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "creds.json")

	if err := fileutil.AtomicWriteFile(path, []byte(`{}`), 0o600); err != nil {
		t.Fatalf("AtomicWriteFile: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".tmp-") {
			t.Errorf("temp file %q was not cleaned up after successful write", e.Name())
		}
	}
}

// TestWarnInsecurePermissions_worldReadable verifies that a warning is emitted
// when a file is world-readable (mode 0644).
func TestWarnInsecurePermissions_worldReadable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "creds.json")
	if err := os.WriteFile(path, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(old) })

	fileutil.WarnInsecurePermissions(path)

	if !strings.Contains(buf.String(), "insecure permissions") {
		t.Errorf("expected insecure-permissions warning, got: %s", buf.String())
	}
}

// TestWarnInsecurePermissions_groupWritable verifies that a warning is emitted
// when a file grants group or other non-owner bits without world-readability.
func TestWarnInsecurePermissions_groupWritable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "creds.json")
	if err := os.WriteFile(path, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o622); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(old) })

	fileutil.WarnInsecurePermissions(path)

	if !strings.Contains(buf.String(), "insecure permissions") {
		t.Errorf("expected insecure-permissions warning for 0622 file, got: %s", buf.String())
	}
}

// TestWarnInsecurePermissions_restrictive verifies that no warning is emitted
// for a properly restricted file (mode 0600).
func TestWarnInsecurePermissions_restrictive(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "creds.json")
	if err := os.WriteFile(path, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(old) })

	fileutil.WarnInsecurePermissions(path)

	if buf.Len() > 0 {
		t.Errorf("unexpected log output for 0600 file: %s", buf.String())
	}
}
