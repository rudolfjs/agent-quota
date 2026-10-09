package selfupdate

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"

	apierrors "github.com/rudolfjs/agent-quota/internal/errors"
)

// UninstallOptions configures Uninstall.
type UninstallOptions struct {
	// BinaryPath is the binary to remove. Empty means the running
	// executable, with symlinks such as aq resolved.
	BinaryPath string

	// ConfigDirs are agent-quota's own config directories. Each must be
	// named "agent-quota"; anything else is refused.
	ConfigDirs []string

	// Yes corresponds to --yes: skip the confirmation prompt.
	Yes bool

	// Confirm asks the user whether to proceed. Required unless Yes is set.
	Confirm func() (bool, error)

	// Out receives user-facing progress messages. Nil writes to os.Stdout.
	Out io.Writer
}

// UninstallResult summarizes what happened.
type UninstallResult struct {
	// Removed lists the paths deleted, in removal order.
	Removed []string
	// Kept lists an aq file left alone because it is not our shortcut.
	Kept []string
	// Cancelled is true when the user declined the prompt.
	Cancelled bool
}

type uninstallTarget struct {
	path string
	dir  bool
}

// Uninstall removes the agent-quota binary, the aq shortcut beside it and
// agent-quota's config directories. Provider credentials are never touched.
func Uninstall(ctx context.Context, opts UninstallOptions) (*UninstallResult, error) {
	return uninstall(ctx, opts, runtime.GOOS, runtime.GOARCH)
}

func uninstall(ctx context.Context, opts UninstallOptions, goos, goarch string) (*UninstallResult, error) {
	if !supportedPlatform(goos, goarch) {
		return nil, apierrors.NewConfigError(
			fmt.Sprintf("uninstall supports Linux x86_64 and macOS Intel/Apple Silicon, not %s/%s", goos, goarch),
			errors.New("unsupported platform"),
		)
	}
	if err := ctx.Err(); err != nil {
		return nil, apierrors.NewConfigError("uninstall cancelled", err)
	}
	out := opts.Out
	if out == nil {
		out = os.Stdout
	}

	binary, err := resolveBinaryPath(opts.BinaryPath)
	if err != nil {
		return nil, err
	}
	for _, dir := range opts.ConfigDirs {
		if filepath.Base(dir) != "agent-quota" {
			return nil, apierrors.NewConfigError(
				fmt.Sprintf("refusing to remove unexpected config directory %s", dir),
				errors.New("config directory not named agent-quota"),
			)
		}
	}

	result := &UninstallResult{}
	// Binary first: a permission failure there stops before anything else
	// is removed.
	targets := []uninstallTarget{{path: binary}}
	aq := filepath.Join(filepath.Dir(binary), "aq")
	if ours, exists := isShortcutTo(aq, binary); ours {
		targets = append(targets, uninstallTarget{path: aq})
	} else if exists {
		result.Kept = append(result.Kept, aq)
	}
	for _, dir := range opts.ConfigDirs {
		if _, err := os.Lstat(dir); err == nil {
			targets = append(targets, uninstallTarget{path: dir, dir: true})
		}
	}

	printf(out, "this will remove:\n")
	for _, t := range targets {
		printf(out, "  %s\n", t.path)
	}
	for _, p := range result.Kept {
		printf(out, "keeping %s: not a shortcut to agent-quota\n", p)
	}
	printf(out, "provider credentials (Claude, Codex, Copilot files and Keychain entries) are not touched\n")

	if !opts.Yes {
		if opts.Confirm == nil {
			return nil, apierrors.NewConfigError("confirmation required; re-run with --yes", errors.New("no confirmation prompt available"))
		}
		ok, err := opts.Confirm()
		if err != nil {
			return nil, apierrors.NewConfigError("could not read confirmation; re-run with --yes to skip the prompt", err)
		}
		if !ok {
			printf(out, "uninstall cancelled; nothing removed\n")
			result.Cancelled = true
			return result, nil
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, apierrors.NewConfigError("uninstall cancelled", err)
	}

	for _, t := range targets {
		remove := os.Remove
		if t.dir {
			remove = os.RemoveAll
		}
		if err := remove(t.path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return result, removeError(t.path, err)
		}
		result.Removed = append(result.Removed, t.path)
		printf(out, "removed %s\n", t.path)
	}
	printf(out, "agent-quota uninstalled\n")
	return result, nil
}

// isShortcutTo reports whether path is a symlink resolving to binary, and
// whether anything exists at path at all. install.sh creates a relative
// link and make local-install an absolute one; both resolve the same.
func isShortcutTo(path, binary string) (ours, exists bool) {
	info, err := os.Lstat(path)
	if err != nil {
		return false, false
	}
	if info.Mode()&fs.ModeSymlink == 0 {
		return false, true
	}
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false, true
	}
	want, err := filepath.EvalSymlinks(binary)
	if err != nil {
		return false, true
	}
	return target == want, true
}

func removeError(path string, err error) error {
	if errors.Is(err, fs.ErrPermission) {
		return apierrors.NewConfigError(fmt.Sprintf("permission denied removing %s; re-run with sudo", path), err)
	}
	return apierrors.NewConfigError(fmt.Sprintf("failed to remove %s", path), err)
}
