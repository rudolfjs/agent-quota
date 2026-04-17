package selfupdate

import "testing"

func TestParseSemver(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in      string
		want    semver
		wantErr bool
	}{
		{"v1.2.3", semver{major: 1, minor: 2, patch: 3}, false},
		{"0.2.2", semver{major: 0, minor: 2, patch: 2}, false},
		{"v1.0.0-rc.1", semver{major: 1, minor: 0, patch: 0, prerelease: "rc.1"}, false},
		{"v1.2.3+build.7", semver{major: 1, minor: 2, patch: 3}, false},
		{"v1.2.3-beta.2+build.9", semver{major: 1, minor: 2, patch: 3, prerelease: "beta.2"}, false},
		{"", semver{}, true},
		{"v1.2", semver{}, true},
		{"v1.2.x", semver{}, true},
		{"v-1.0.0", semver{}, true},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			got, err := parseSemver(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("parseSemver(%q) = %+v, want %+v", tc.in, got, tc.want)
			}
		})
	}
}

func TestCompareSemver(t *testing.T) {
	t.Parallel()
	cases := []struct {
		a, b string
		want int
	}{
		{"v1.2.3", "v1.2.3", 0},
		{"v1.2.3", "v1.2.4", -1},
		{"v1.2.4", "v1.2.3", 1},
		{"v1.2.3", "v2.0.0", -1},
		{"v1.2.3-rc.1", "v1.2.3", -1},
		{"v1.2.3", "v1.2.3-rc.1", 1},
		{"v1.2.3-alpha", "v1.2.3-beta", -1},
	}
	for _, tc := range cases {
		t.Run(tc.a+"_vs_"+tc.b, func(t *testing.T) {
			t.Parallel()
			a, err := parseSemver(tc.a)
			if err != nil {
				t.Fatalf("parse %q: %v", tc.a, err)
			}
			b, err := parseSemver(tc.b)
			if err != nil {
				t.Fatalf("parse %q: %v", tc.b, err)
			}
			if got := compareSemver(a, b); got != tc.want {
				t.Fatalf("compare(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

func TestIsPrerelease(t *testing.T) {
	t.Parallel()
	if isPrerelease("v1.2.3") {
		t.Fatal("v1.2.3 is not a prerelease")
	}
	if !isPrerelease("v1.2.3-rc.1") {
		t.Fatal("v1.2.3-rc.1 is a prerelease")
	}
	if isPrerelease("not-a-version") {
		t.Fatal("unparseable inputs should default to false")
	}
}
