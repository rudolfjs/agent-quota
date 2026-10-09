package selfupdate

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	apierrors "github.com/rudolfjs/agent-quota/internal/errors"
)

type installFixture struct {
	binDir, binary, aq, configParent, configDir string
}

// newInstallFixture lays out an install.sh-style install: a binary, a
// relative aq symlink and a populated config dir, all under t.TempDir().
func newInstallFixture(t *testing.T) installFixture {
	t.Helper()
	root := t.TempDir()
	f := installFixture{
		binDir:       filepath.Join(root, "bin"),
		configParent: filepath.Join(root, "config"),
	}
	f.binary = filepath.Join(f.binDir, "agent-quota")
	f.aq = filepath.Join(f.binDir, "aq")
	f.configDir = filepath.Join(f.configParent, "agent-quota")
	mustMkdir(t, f.binDir)
	mustMkdir(t, f.configDir)
	mustWrite(t, f.binary, "binary")
	mustWrite(t, filepath.Join(f.configDir, "settings.json"), "{}")
	mustWrite(t, filepath.Join(f.configParent, "other-tool.json"), "{}")
	if err := os.Symlink("agent-quota", f.aq); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f installFixture) options(out *bytes.Buffer) UninstallOptions {
	return UninstallOptions{BinaryPath: f.binary, ConfigDirs: []string{f.configDir}, Yes: true, Out: out}
}

func TestUninstallRemovesBinaryShortcutAndConfig(t *testing.T) {
	f := newInstallFixture(t)
	var out bytes.Buffer

	res, err := uninstall(t.Context(), f.options(&out), "linux", "amd64")
	if err != nil {
		t.Fatalf("uninstall: %v", err)
	}

	for _, p := range []string{f.aq, f.configDir, f.binary} {
		assertGone(t, p)
	}
	assertExists(t, filepath.Join(f.configParent, "other-tool.json"))
	if len(res.Removed) != 3 {
		t.Fatalf("Removed = %v, want 3 paths", res.Removed)
	}
	if !strings.Contains(out.String(), "not touched") {
		t.Fatalf("output does not mention provider credentials are kept:\n%s", out.String())
	}
}

func TestUninstallRemovesAbsoluteShortcut(t *testing.T) {
	f := newInstallFixture(t)
	mustRemove(t, f.aq)
	// make local-install creates an absolute symlink.
	if err := os.Symlink(f.binary, f.aq); err != nil {
		t.Fatal(err)
	}

	if _, err := uninstall(t.Context(), f.options(&bytes.Buffer{}), "darwin", "arm64"); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	assertGone(t, f.aq)
}

func TestUninstallKeepsForeignShortcut(t *testing.T) {
	for name, setup := range map[string]func(t *testing.T, f installFixture){
		"regular file": func(t *testing.T, f installFixture) {
			mustRemove(t, f.aq)
			mustWrite(t, f.aq, "someone else's aq")
		},
		"symlink elsewhere": func(t *testing.T, f installFixture) {
			mustRemove(t, f.aq)
			other := filepath.Join(f.binDir, "other")
			mustWrite(t, other, "other")
			if err := os.Symlink(other, f.aq); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newInstallFixture(t)
			setup(t, f)
			var out bytes.Buffer

			res, err := uninstall(t.Context(), f.options(&out), "linux", "amd64")
			if err != nil {
				t.Fatalf("uninstall: %v", err)
			}
			assertExists(t, f.aq)
			assertGone(t, f.binary)
			if !slices.Contains(res.Kept, f.aq) || !strings.Contains(out.String(), f.aq) {
				t.Fatalf("foreign aq not reported as kept: %v\n%s", res.Kept, out.String())
			}
		})
	}
}

func TestUninstallToleratesMissingShortcutAndConfig(t *testing.T) {
	f := newInstallFixture(t)
	mustRemove(t, f.aq)
	if err := os.RemoveAll(f.configDir); err != nil {
		t.Fatal(err)
	}

	res, err := uninstall(t.Context(), f.options(&bytes.Buffer{}), "linux", "amd64")
	if err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	assertGone(t, f.binary)
	if !slices.Equal(res.Removed, []string{f.binary}) {
		t.Fatalf("Removed = %v, want only binary", res.Removed)
	}
}

func TestUninstallListsTargetsBeforeConfirming(t *testing.T) {
	f := newInstallFixture(t)
	var out bytes.Buffer
	opts := f.options(&out)
	opts.Yes = false
	opts.Confirm = func() (bool, error) {
		for _, p := range []string{f.binary, f.aq, f.configDir} {
			if !strings.Contains(out.String(), p) {
				t.Errorf("%s not listed before confirmation:\n%s", p, out.String())
			}
		}
		return true, nil
	}

	if _, err := uninstall(t.Context(), opts, "linux", "amd64"); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	assertGone(t, f.binary)
}

func TestUninstallDeclinedChangesNothing(t *testing.T) {
	f := newInstallFixture(t)
	opts := f.options(&bytes.Buffer{})
	opts.Yes = false
	opts.Confirm = func() (bool, error) { return false, nil }

	res, err := uninstall(t.Context(), opts, "linux", "amd64")
	if err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if !res.Cancelled || len(res.Removed) != 0 {
		t.Fatalf("result = %+v, want cancelled with nothing removed", res)
	}
	for _, p := range []string{f.aq, f.configDir, f.binary} {
		assertExists(t, p)
	}
}

func TestUninstallErrorsLeaveFilesInPlace(t *testing.T) {
	for name, tc := range map[string]struct {
		mutate     func(*UninstallOptions)
		goos, arch string
		ctx        func(t *testing.T) context.Context
	}{
		"no confirmation without --yes": {
			mutate: func(o *UninstallOptions) { o.Yes = false },
			goos:   "linux", arch: "amd64",
		},
		"confirmation fails": {
			mutate: func(o *UninstallOptions) {
				o.Yes = false
				o.Confirm = func() (bool, error) { return false, errors.New("no tty") }
			},
			goos: "linux", arch: "amd64",
		},
		"unsupported platform": {
			goos: "linux", arch: "arm64",
		},
		"cancelled context": {
			goos: "linux", arch: "amd64",
			ctx: func(t *testing.T) context.Context {
				ctx, cancel := context.WithCancel(t.Context())
				cancel()
				return ctx
			},
		},
		"config dir not named agent-quota": {
			mutate: func(o *UninstallOptions) { o.ConfigDirs = []string{filepath.Dir(o.ConfigDirs[0])} },
			goos:   "linux", arch: "amd64",
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newInstallFixture(t)
			opts := f.options(&bytes.Buffer{})
			if tc.mutate != nil {
				tc.mutate(&opts)
			}
			ctx := t.Context()
			if tc.ctx != nil {
				ctx = tc.ctx(t)
			}

			_, err := uninstall(ctx, opts, tc.goos, tc.arch)
			var de *apierrors.DomainError
			if !errors.As(err, &de) {
				t.Fatalf("err = %v, want DomainError", err)
			}
			for _, p := range []string{f.aq, f.configDir, f.binary} {
				assertExists(t, p)
			}
		})
	}
}

func TestUninstallPermissionErrorSuggestsSudo(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("directory permissions are not enforced")
	}
	f := newInstallFixture(t)
	mustRemove(t, f.aq)
	if err := os.Chmod(f.binDir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(f.binDir, 0o755) })

	_, err := uninstall(t.Context(), f.options(&bytes.Buffer{}), "linux", "amd64")
	var de *apierrors.DomainError
	if !errors.As(err, &de) || !strings.Contains(de.Message, "sudo") {
		t.Fatalf("err = %v, want DomainError suggesting sudo", err)
	}
	// Nothing else is removed when the binary cannot be.
	assertExists(t, f.binary)
	assertExists(t, f.configDir)
}

func mustMkdir(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, p, s string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(s), 0o600); err != nil {
		t.Fatal(err)
	}
}

func mustRemove(t *testing.T, p string) {
	t.Helper()
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
}

func assertGone(t *testing.T, p string) {
	t.Helper()
	if _, err := os.Lstat(p); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("%s still exists (err=%v)", p, err)
	}
}

func assertExists(t *testing.T, p string) {
	t.Helper()
	if _, err := os.Lstat(p); err != nil {
		t.Fatalf("%s missing: %v", p, err)
	}
}
