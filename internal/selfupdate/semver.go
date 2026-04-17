package selfupdate

import (
	"fmt"
	"strconv"
	"strings"
)

// semver is a minimal SemVer representation sufficient for comparing
// agent-quota release tags (all shaped like `vX.Y.Z` or `vX.Y.Z-prerelease`).
// Build metadata (+build.N) is accepted but ignored per SemVer 2.0.0.
type semver struct {
	major, minor, patch int
	prerelease          string // identifier after "-", empty for stable
}

// parseSemver accepts "vX.Y.Z", "X.Y.Z", or "...-prerelease[+build]".
// Returns an error on any shape it can't cleanly parse.
func parseSemver(raw string) (semver, error) {
	s := strings.TrimSpace(raw)
	s = strings.TrimPrefix(s, "v")
	if s == "" {
		return semver{}, fmt.Errorf("empty version string")
	}

	if core, _, ok := strings.Cut(s, "+"); ok {
		s = core
	}
	core, pre, _ := strings.Cut(s, "-")

	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return semver{}, fmt.Errorf("expected MAJOR.MINOR.PATCH, got %q", core)
	}
	ints := [3]int{}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return semver{}, fmt.Errorf("invalid numeric component %q in %q", p, raw)
		}
		ints[i] = n
	}
	return semver{major: ints[0], minor: ints[1], patch: ints[2], prerelease: pre}, nil
}

// compareSemver returns -1 if a < b, 0 if a == b, +1 if a > b.
// Per SemVer 2.0.0 precedence: core numbers compared first; a prerelease
// version has lower precedence than the same core without one.
func compareSemver(a, b semver) int {
	for _, pair := range [3][2]int{{a.major, b.major}, {a.minor, b.minor}, {a.patch, b.patch}} {
		if pair[0] != pair[1] {
			if pair[0] < pair[1] {
				return -1
			}
			return 1
		}
	}
	// Cores equal — a stable release outranks any prerelease.
	switch {
	case a.prerelease == "" && b.prerelease == "":
		return 0
	case a.prerelease == "" && b.prerelease != "":
		return 1
	case a.prerelease != "" && b.prerelease == "":
		return -1
	}
	return strings.Compare(a.prerelease, b.prerelease)
}

// isPrerelease returns true if the tag has a prerelease identifier.
func isPrerelease(raw string) bool {
	v, err := parseSemver(raw)
	if err != nil {
		return false
	}
	return v.prerelease != ""
}
