package selfupdate

import (
	"runtime"
	"testing"
)

func requireSupportedPlatform(t *testing.T) {
	t.Helper()
	if !supportedPlatform(runtime.GOOS, runtime.GOARCH) {
		t.Skipf("unsupported platform %s/%s", runtime.GOOS, runtime.GOARCH)
	}
}

func TestSupportedPlatform(t *testing.T) {
	for _, tc := range []struct {
		os, arch string
		want     bool
	}{
		{"linux", "amd64", true},
		{"darwin", "amd64", true},
		{"darwin", "arm64", true},
		{"linux", "arm64", false},
		{"windows", "amd64", false},
		{"darwin", "386", false},
	} {
		if got := supportedPlatform(tc.os, tc.arch); got != tc.want {
			t.Errorf("supportedPlatform(%s, %s) = %v", tc.os, tc.arch, got)
		}
	}
}

func TestAssetNames(t *testing.T) {
	archive, checksums := assetNames("v1.2.3")
	want := "agent-quota_1.2.3_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz"
	if archive != want || checksums != "checksums.txt" {
		t.Fatalf("unexpected asset names: %s, %s", archive, checksums)
	}
}
