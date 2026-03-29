// Package fileutil provides file-system helpers shared across providers.
package fileutil

import (
	"os"
	"path/filepath"
)

// AtomicWriteFile writes data to path atomically using a temp-file + rename
// pattern. If the process crashes between the write and the rename, the
// original file is left intact. The temp file is created in the same directory
// as path so that the rename stays on the same filesystem.
func AtomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()

	// Clean up the temp file on any failure before rename.
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	ok = true
	return nil
}
