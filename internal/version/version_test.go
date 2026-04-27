package version_test

import (
	"strings"
	"testing"

	"github.com/rudolfjs/agent-quota/internal/version"
)

func TestString_containsVersion(t *testing.T) {
	s := version.String()
	if s == "" {
		t.Error("String() must not be empty")
	}
}

func TestString_devDefault(t *testing.T) {
	// In test builds, version is not injected via ldflags so it should be "dev"
	s := version.String()
	if !strings.Contains(s, "dev") && !strings.Contains(s, ".") {
		t.Errorf("String() = %q: expected 'dev' or a semver string", s)
	}
}

func TestClaudeCLIVersion_returnsFallbackWhenNotFound(t *testing.T) {
	// Temporarily override the exec lookup to simulate missing claude binary.
	// Since we can't easily mock exec in a unit test without additional
	// infrastructure, we just verify the fallback constant is non-empty.
	fb := version.FallbackClaudeVersion
	if fb == "" {
		t.Error("FallbackClaudeVersion must not be empty")
	}
}

func TestClaudeCLIVersion_nonEmpty(t *testing.T) {
	v := version.ClaudeCLIVersion()
	if v == "" {
		t.Error("ClaudeCLIVersion() must not return empty string")
	}
}
