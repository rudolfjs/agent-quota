package selfupdate

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"

	apierrors "github.com/schnetlerr/agent-quota/internal/errors"
)

// Options tune Run without adding a dozen positional arguments.
type Options struct {
	// OwnerRepo identifies the upstream repo as "owner/repo".
	// Defaults to "rudolfjs/agent-quota" when empty.
	OwnerRepo string

	// CurrentVersion is the version string baked into the running binary.
	// Typically pass version.String()'s first field; "dev" is special-cased.
	CurrentVersion string

	// APIBaseURL overrides https://api.github.com for tests.
	APIBaseURL string

	// AssetBaseURL overrides https://github.com/<owner>/<repo>/releases/download
	// for tests. When empty the real GitHub download path is used.
	AssetBaseURL string

	// HTTPClient lets callers inject a custom transport (tests, proxies).
	HTTPClient *http.Client

	// AllowPrerelease corresponds to --pre.
	AllowPrerelease bool

	// Force corresponds to --force (reinstall current version).
	Force bool

	// CheckOnly corresponds to --check: report the decision but do not
	// download or install anything.
	CheckOnly bool

	// BinaryPath is the path to replace. Empty means "auto-detect via
	// os.Executable + EvalSymlinks", which is what the CLI always does —
	// tests supply a path to a scratch file.
	BinaryPath string

	// Out receives user-facing progress messages. Nil writes to os.Stdout.
	Out io.Writer
}

// Result summarizes what happened.
type Result struct {
	// Decision records why we did or didn't update.
	Decision UpdateDecision
	// Latest is the tag we considered for install (may be equal to current).
	Latest string
	// InstalledPath is the file that was overwritten; empty when no install
	// occurred (check-only or already up to date).
	InstalledPath string
}

const defaultOwnerRepo = "rudolfjs/agent-quota"

// Run executes the self-update pipeline end-to-end.
//
// Flow:
//  1. Resolve latest release (with --pre falling back to List()).
//  2. Apply policy; bail early if no update is needed.
//  3. Download the platform archive + checksums.txt.
//  4. Verify sha256.
//  5. Extract the binary into a staging dir.
//  6. Atomically swap it into the current executable's path.
func Run(ctx context.Context, opts Options) (*Result, error) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		return nil, apierrors.NewConfigError(
			fmt.Sprintf("self-update only supports linux/amd64 today, not %s/%s", runtime.GOOS, runtime.GOARCH),
			errors.New("unsupported platform"),
		)
	}

	ownerRepo := opts.OwnerRepo
	if ownerRepo == "" {
		ownerRepo = defaultOwnerRepo
	}
	httpClient := opts.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	out := opts.Out
	if out == nil {
		out = os.Stdout
	}

	printf(out, "checking %s for updates...\n", ownerRepo)
	releases := NewReleaseClient(opts.APIBaseURL, ownerRepo, httpClient)

	rel, err := pickRelease(ctx, releases, opts.AllowPrerelease)
	if err != nil {
		return nil, err
	}
	printf(out, "latest release: %s\n", rel.TagName)

	decision, err := decideUpdate(PolicyInput{
		Current:         opts.CurrentVersion,
		Latest:          rel.TagName,
		AllowPrerelease: opts.AllowPrerelease,
		Force:           opts.Force,
	})
	if err != nil {
		return nil, apierrors.NewConfigError("failed to evaluate update policy", err)
	}
	printf(out, "%s\n", decision.Reason)

	result := &Result{Decision: decision, Latest: rel.TagName}

	if !decision.ShouldUpdate || opts.CheckOnly {
		return result, nil
	}

	dst, err := resolveBinaryPath(opts.BinaryPath)
	if err != nil {
		return nil, err
	}

	archive, checksums := assetNames(rel.TagName)
	base := opts.AssetBaseURL
	if base == "" {
		base = fmt.Sprintf("https://github.com/%s/releases/download/%s", ownerRepo, rel.TagName)
	}

	printf(out, "downloading %s...\n", archive)
	archivePath, err := fetchToTemp(ctx, httpClient, base+"/"+archive, "agent-quota-archive-*.tar.gz")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.Remove(archivePath) }()

	printf(out, "verifying checksum...\n")
	checksumsPath, err := fetchToTemp(ctx, httpClient, base+"/"+checksums, "agent-quota-checksums-*.txt")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.Remove(checksumsPath) }()

	if err := verifyChecksum(archivePath, checksumsPath, archive); err != nil {
		return nil, err
	}

	staged, err := os.CreateTemp("", "agent-quota-bin-*")
	if err != nil {
		return nil, apierrors.NewConfigError("failed to create staging file", err)
	}
	stagedPath := staged.Name()
	_ = staged.Close()
	defer func() { _ = os.Remove(stagedPath) }()

	printf(out, "extracting binary...\n")
	if err := extractBinary(archivePath, stagedPath); err != nil {
		return nil, err
	}

	printf(out, "installing to %s...\n", dst)
	if err := swapBinary(stagedPath, dst); err != nil {
		return nil, err
	}

	result.InstalledPath = dst
	printf(out, "updated to %s\n", rel.TagName)
	return result, nil
}

func pickRelease(ctx context.Context, client *ReleaseClient, allowPre bool) (*Release, error) {
	if !allowPre {
		return client.Latest(ctx)
	}
	releases, err := client.List(ctx)
	if err != nil {
		return nil, err
	}
	for _, rel := range releases {
		if rel.Draft {
			continue
		}
		r := rel
		return &r, nil
	}
	return nil, apierrors.NewAPIError("no suitable release found", errors.New("empty releases list"))
}

// resolveBinaryPath returns the absolute path of the binary we should
// overwrite. When override is set (tests) it is used directly. Otherwise
// we use os.Executable + EvalSymlinks so that `aq` (a symlink) is resolved
// to the real agent-quota binary before swapping.
func resolveBinaryPath(override string) (string, error) {
	if override != "" {
		abs, err := filepath.Abs(override)
		if err != nil {
			return "", apierrors.NewConfigError("failed to resolve override binary path", err)
		}
		return abs, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", apierrors.NewConfigError("failed to locate current executable", err)
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return "", apierrors.NewConfigError("failed to resolve executable symlinks", err)
	}
	return resolved, nil
}

func printf(out io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(out, format, args...)
}
