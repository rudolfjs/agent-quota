package selfupdate

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	apierrors "github.com/schnetlerr/agent-quota/internal/errors"
)

// maxArchiveBytes caps the release archive size to guard against a runaway
// download. Real archives are under 30 MiB.
const maxArchiveBytes = 128 << 20 // 128 MiB

// assetNames derives the expected archive and checksum filenames for a
// given release tag and runtime target. Matches the layout produced by
// goreleaser in this repo (see scripts/install.sh:203-204).
func assetNames(tag string) (archive, checksums string) {
	version := strings.TrimPrefix(tag, "v")
	archive = fmt.Sprintf("agent-quota_%s_%s_%s.tar.gz", version, runtime.GOOS, runtime.GOARCH)
	checksums = "checksums.txt"
	return
}

// fetchToTemp streams the response body to a newly created temp file and
// returns its absolute path. The caller is responsible for removing it.
func fetchToTemp(ctx context.Context, client *http.Client, url, pattern string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", apierrors.NewNetworkError("failed to build download request", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", apierrors.NewNetworkError("download request failed", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", apierrors.NewAPIError(fmt.Sprintf("download %s returned HTTP %d", url, resp.StatusCode), fmt.Errorf("HTTP %d", resp.StatusCode))
	}

	f, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", apierrors.NewConfigError("failed to create temp file", err)
	}
	path := f.Name()
	limited := io.LimitReader(resp.Body, maxArchiveBytes)
	if _, err := io.Copy(f, limited); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return "", apierrors.NewNetworkError("failed to stream download", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return "", apierrors.NewConfigError("failed to finalize download", err)
	}
	return path, nil
}

// verifyChecksum reads checksums.txt for the entry matching assetName
// (the upstream release filename, not the local temp path) and compares
// its sha256 to the file at archivePath. Returns nil on match.
func verifyChecksum(archivePath, checksumsPath, assetName string) error {
	want, err := findExpectedChecksum(checksumsPath, assetName)
	if err != nil {
		return err
	}

	f, err := os.Open(archivePath)
	if err != nil {
		return apierrors.NewConfigError("failed to open archive for checksum", err)
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return apierrors.NewConfigError("failed to hash archive", err)
	}
	got := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(got, want) {
		return apierrors.NewAPIError("archive checksum mismatch — refusing to install", fmt.Errorf("got %s, want %s", got, want))
	}
	return nil
}

func findExpectedChecksum(path, name string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", apierrors.NewConfigError("failed to open checksums file", err)
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		// goreleaser format: "<sha256>  <filename>"
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		if fields[1] == name {
			return fields[0], nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", apierrors.NewConfigError("failed to read checksums file", err)
	}
	return "", apierrors.NewAPIError("checksums.txt has no entry for "+name, errors.New("missing checksum"))
}

// extractBinary finds and writes the "agent-quota" binary from a tar.gz
// archive to dest. Returns the written path on success.
func extractBinary(archivePath, dest string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return apierrors.NewConfigError("failed to open archive", err)
	}
	defer func() { _ = f.Close() }()

	gr, err := gzip.NewReader(f)
	if err != nil {
		return apierrors.NewAPIError("failed to decompress archive", err)
	}
	defer func() { _ = gr.Close() }()

	tr := tar.NewReader(gr)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return apierrors.NewAPIError("failed to read archive entry", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		if filepath.Base(hdr.Name) != "agent-quota" {
			continue
		}
		out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			return apierrors.NewConfigError("failed to open destination for extracted binary", err)
		}
		if _, err := io.Copy(out, io.LimitReader(tr, maxArchiveBytes)); err != nil {
			_ = out.Close()
			_ = os.Remove(dest)
			return apierrors.NewAPIError("failed to extract binary", err)
		}
		if err := out.Close(); err != nil {
			return apierrors.NewConfigError("failed to finalize extracted binary", err)
		}
		return nil
	}
	return apierrors.NewAPIError("agent-quota binary not found inside archive", errors.New("missing binary"))
}

// swapBinary atomically replaces dst with the file at src via rename(2).
// On Linux this is safe even while the current binary is executing — the
// kernel keeps the original inode resident until all file descriptors /
// running processes release it. The new binary takes effect on the next
// invocation.
func swapBinary(src, dst string) error {
	// Rename must be on the same filesystem. CreateTemp defaults to $TMPDIR,
	// which may be tmpfs even when dst lives on /usr/local — handle that
	// by moving src into dst's directory first.
	dir := filepath.Dir(dst)
	staged, err := os.CreateTemp(dir, ".agent-quota.tmp-")
	if err != nil {
		return apierrors.NewConfigError("failed to create staging file next to binary", err)
	}
	stagedPath := staged.Name()
	if err := staged.Close(); err != nil {
		_ = os.Remove(stagedPath)
		return apierrors.NewConfigError("failed to close staging file", err)
	}

	if err := copyFile(src, stagedPath, 0o755); err != nil {
		_ = os.Remove(stagedPath)
		return err
	}

	if err := os.Rename(stagedPath, dst); err != nil {
		_ = os.Remove(stagedPath)
		return apierrors.NewConfigError("failed to swap binary into place", err)
	}
	return nil
}

func copyFile(src, dst string, perm os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return apierrors.NewConfigError("failed to open source for copy", err)
	}
	defer func() { _ = in.Close() }()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return apierrors.NewConfigError("failed to open destination for copy", err)
	}
	// O_CREATE only applies perm when the file is newly created; swapBinary
	// hands us a path returned by os.CreateTemp (mode 0o600), so re-chmod
	// explicitly or the installed binary ends up non-executable.
	if err := out.Chmod(perm); err != nil {
		_ = out.Close()
		return apierrors.NewConfigError("failed to set permissions on copied file", err)
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return apierrors.NewConfigError("failed to copy file contents", err)
	}
	if err := out.Close(); err != nil {
		return apierrors.NewConfigError("failed to finalize copied file", err)
	}
	return nil
}
