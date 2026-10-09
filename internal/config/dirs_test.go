package config

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestDirsIncludesPrimaryAndHomeFallback(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")

	userDir, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("UserConfigDir: %v", err)
	}
	dirs, err := Dirs()
	if err != nil {
		t.Fatalf("Dirs: %v", err)
	}

	if len(dirs) == 0 || dirs[0] != filepath.Join(userDir, "agent-quota") {
		t.Fatalf("Dirs() = %v, want primary %s first", dirs, filepath.Join(userDir, "agent-quota"))
	}
	if !slices.Contains(dirs, filepath.Join(home, ".config", "agent-quota")) {
		t.Fatalf("Dirs() = %v, want ~/.config fallback", dirs)
	}
	if len(slices.Compact(slices.Clone(dirs))) != len(dirs) {
		t.Fatalf("Dirs() = %v contains duplicates", dirs)
	}
}

func TestDirsMatchesDefaultPaths(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")

	dirs, err := Dirs()
	if err != nil {
		t.Fatalf("Dirs: %v", err)
	}
	for _, fn := range []func() (string, error){DefaultPath, DefaultSettingsPath, DefaultQuotaCachePath} {
		path, err := fn()
		if err != nil {
			t.Fatalf("default path: %v", err)
		}
		if filepath.Dir(path) != dirs[0] {
			t.Fatalf("%s is outside primary config dir %s", path, dirs[0])
		}
	}
}
