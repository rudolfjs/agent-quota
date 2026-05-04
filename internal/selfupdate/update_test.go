package selfupdate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	apierrors "github.com/rudolfjs/agent-quota/internal/errors"
)

// newTestReleaseServers spins up two httptest servers: one impersonates the
// GitHub releases API, the other serves the release assets (archive +
// checksums). Returning the API/asset URL pair lets tests mirror Options.
type testServers struct {
	api         *httptest.Server
	assets      *httptest.Server
	archiveHash string
}

func newTestReleaseServers(t *testing.T, tag string, archive []byte, checksumName string) *testServers {
	t.Helper()

	sum := sha256.Sum256(archive)
	hash := hex.EncodeToString(sum[:])

	archiveFilename := assetArchiveName(tag)
	assetMux := http.NewServeMux()
	assetMux.HandleFunc("/"+tag+"/"+archiveFilename, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		_, _ = w.Write(archive)
	})
	assetMux.HandleFunc("/"+tag+"/checksums.txt", func(w http.ResponseWriter, r *http.Request) {
		filename := archiveFilename
		if checksumName != "" {
			filename = checksumName
		}
		_, _ = fmt.Fprintf(w, "%s  %s\n", hash, filename)
	})
	assets := httptest.NewServer(assetMux)

	apiMux := http.NewServeMux()
	apiMux.HandleFunc("/repos/rudolfjs/agent-quota/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(Release{TagName: tag, HTMLURL: assets.URL, Prerelease: false})
	})
	apiMux.HandleFunc("/repos/rudolfjs/agent-quota/releases", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]Release{{TagName: tag, HTMLURL: assets.URL, Prerelease: false}})
	})
	api := httptest.NewServer(apiMux)

	t.Cleanup(func() {
		api.Close()
		assets.Close()
	})
	return &testServers{api: api, assets: assets, archiveHash: hash}
}

func assetArchiveName(tag string) string {
	version := strings.TrimPrefix(tag, "v")
	return fmt.Sprintf("agent-quota_%s_%s_%s.tar.gz", version, runtime.GOOS, runtime.GOARCH)
}

// buildTarGzArchive returns a gzipped tarball containing a single "agent-quota"
// entry whose body is the given marker. Tests use a distinctive marker so
// they can assert the installed binary came from the archive.
func buildTarGzArchive(t *testing.T, marker []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	hdr := &tar.Header{
		Name:     "agent-quota",
		Mode:     0o755,
		Size:     int64(len(marker)),
		Typeflag: tar.TypeReg,
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatalf("tar header: %v", err)
	}
	if _, err := tw.Write(marker); err != nil {
		t.Fatalf("tar body: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gz close: %v", err)
	}
	return buf.Bytes()
}

func TestRun_installsLatestRelease(t *testing.T) {
	if !selfUpdateSupported() {
		t.Skipf("self-update not supported on %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	marker := []byte("AGENT_QUOTA_FAKE_BINARY_v0.3.0\n")
	archive := buildTarGzArchive(t, marker)
	servers := newTestReleaseServers(t, "v0.3.0", archive, "")

	dir := t.TempDir()
	binPath := filepath.Join(dir, "agent-quota")
	if err := os.WriteFile(binPath, []byte("old"), 0o755); err != nil {
		t.Fatalf("seed binary: %v", err)
	}

	var out bytes.Buffer
	result, err := Run(context.Background(), Options{
		CurrentVersion: "v0.2.2",
		APIBaseURL:     servers.api.URL,
		AssetBaseURL:   servers.assets.URL + "/v0.3.0",
		BinaryPath:     binPath,
		Out:            &out,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !result.Decision.ShouldUpdate {
		t.Fatalf("expected ShouldUpdate=true, got %+v", result.Decision)
	}
	if result.InstalledPath != binPath {
		t.Fatalf("InstalledPath = %q, want %q", result.InstalledPath, binPath)
	}
	installed, err := os.ReadFile(binPath)
	if err != nil {
		t.Fatalf("read installed binary: %v", err)
	}
	if !bytes.Equal(installed, marker) {
		t.Fatalf("installed bytes = %q, want %q", installed, marker)
	}
	info, err := os.Stat(binPath)
	if err != nil {
		t.Fatalf("stat installed binary: %v", err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("installed binary has no executable bit set: mode=%#o", info.Mode().Perm())
	}
}

// TestRun_installedBinaryIsExecutable is the regression guard for the
// self-update permission-denied bug: os.CreateTemp produces a 0o600 staging
// file, and O_CREATE|O_TRUNC only applies the perm argument on file
// creation, so the installed binary must be explicitly chmod'd to stay
// executable after the atomic rename.
func TestRun_installedBinaryIsExecutable(t *testing.T) {
	if !selfUpdateSupported() {
		t.Skipf("self-update not supported on %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	archive := buildTarGzArchive(t, []byte("new-binary-payload"))
	servers := newTestReleaseServers(t, "v0.3.0", archive, "")

	dir := t.TempDir()
	binPath := filepath.Join(dir, "agent-quota")
	if err := os.WriteFile(binPath, []byte("old"), 0o755); err != nil {
		t.Fatalf("seed binary: %v", err)
	}

	if _, err := Run(context.Background(), Options{
		CurrentVersion: "v0.2.2",
		APIBaseURL:     servers.api.URL,
		AssetBaseURL:   servers.assets.URL + "/v0.3.0",
		BinaryPath:     binPath,
		Out:            io.Discard,
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	info, err := os.Stat(binPath)
	if err != nil {
		t.Fatalf("stat installed binary: %v", err)
	}
	mode := info.Mode().Perm()
	if mode&0o100 == 0 {
		t.Fatalf("installed binary is not executable by owner: mode=%#o", mode)
	}
	if mode != 0o755 {
		t.Fatalf("installed binary mode = %#o, want 0o755", mode)
	}
}

func TestRun_noUpdateWhenAlreadyOnLatest(t *testing.T) {
	if !selfUpdateSupported() {
		t.Skipf("self-update not supported on %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	archive := buildTarGzArchive(t, []byte("marker"))
	servers := newTestReleaseServers(t, "v0.3.0", archive, "")

	dir := t.TempDir()
	binPath := filepath.Join(dir, "agent-quota")
	original := []byte("original-binary-bytes")
	if err := os.WriteFile(binPath, original, 0o755); err != nil {
		t.Fatalf("seed binary: %v", err)
	}

	result, err := Run(context.Background(), Options{
		CurrentVersion: "v0.3.0",
		APIBaseURL:     servers.api.URL,
		AssetBaseURL:   servers.assets.URL + "/v0.3.0",
		BinaryPath:     binPath,
		Out:            io.Discard,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Decision.ShouldUpdate {
		t.Fatalf("expected ShouldUpdate=false, got %+v", result.Decision)
	}
	current, err := os.ReadFile(binPath)
	if err != nil {
		t.Fatalf("read binary: %v", err)
	}
	if !bytes.Equal(current, original) {
		t.Fatalf("binary was modified when no update was expected: %q", current)
	}
}

func TestRun_checkOnlySkipsInstall(t *testing.T) {
	if !selfUpdateSupported() {
		t.Skipf("self-update not supported on %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	archive := buildTarGzArchive(t, []byte("new"))
	servers := newTestReleaseServers(t, "v0.3.0", archive, "")

	dir := t.TempDir()
	binPath := filepath.Join(dir, "agent-quota")
	original := []byte("still-old")
	if err := os.WriteFile(binPath, original, 0o755); err != nil {
		t.Fatalf("seed binary: %v", err)
	}

	result, err := Run(context.Background(), Options{
		CurrentVersion: "v0.2.2",
		APIBaseURL:     servers.api.URL,
		AssetBaseURL:   servers.assets.URL + "/v0.3.0",
		BinaryPath:     binPath,
		CheckOnly:      true,
		Out:            io.Discard,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !result.Decision.ShouldUpdate {
		t.Fatal("expected decision to report an available update")
	}
	if result.InstalledPath != "" {
		t.Fatalf("InstalledPath should be empty in --check mode, got %q", result.InstalledPath)
	}
	current, err := os.ReadFile(binPath)
	if err != nil {
		t.Fatalf("read binary: %v", err)
	}
	if !bytes.Equal(current, original) {
		t.Fatal("binary was modified in --check mode")
	}
}

func TestRun_checksumMismatchAbortsInstall(t *testing.T) {
	if !selfUpdateSupported() {
		t.Skipf("self-update not supported on %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	archive := buildTarGzArchive(t, []byte("good"))
	// Server returns a checksum line for the wrong file → mismatch.
	servers := newTestReleaseServers(t, "v0.3.0", archive, "totally-different-filename.tar.gz")

	dir := t.TempDir()
	binPath := filepath.Join(dir, "agent-quota")
	original := []byte("pristine")
	if err := os.WriteFile(binPath, original, 0o755); err != nil {
		t.Fatalf("seed binary: %v", err)
	}

	_, err := Run(context.Background(), Options{
		CurrentVersion: "v0.2.2",
		APIBaseURL:     servers.api.URL,
		AssetBaseURL:   servers.assets.URL + "/v0.3.0",
		BinaryPath:     binPath,
		Out:            io.Discard,
	})
	if err == nil {
		t.Fatal("expected error when checksum entry is missing for archive")
	}
	var dErr *apierrors.DomainError
	if !errors.As(err, &dErr) {
		t.Fatalf("expected DomainError, got %T: %v", err, err)
	}
	current, err := os.ReadFile(binPath)
	if err != nil {
		t.Fatalf("read binary: %v", err)
	}
	if !bytes.Equal(current, original) {
		t.Fatal("binary was modified despite checksum failure")
	}
}
