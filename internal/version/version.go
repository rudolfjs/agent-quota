// Package version provides build-time version information and
// detects the installed claude CLI version for use in HTTP User-Agent headers.
package version

import (
	"log/slog"
	"os"
	"os/exec"
	"strings"
)

// FallbackClaudeVersion is used when the claude CLI is not installed or
// its version cannot be determined. The literal version string is intentionally
// "unknown" rather than a real version number to avoid misrepresenting the
// client to the Anthropic API when the CLI is absent.
const FallbackClaudeVersion = "unknown"

// Build-time variables. Injected via:
//
//	go build -ldflags "-X github.com/rudolfjs/agent-quota/internal/version.buildVersion=v1.2.3 \
//	                    -X github.com/rudolfjs/agent-quota/internal/version.buildCommit=abc1234"
var (
	buildVersion = "dev"
	buildCommit  = ""
)

// String returns the full version string, including commit if available.
func String() string {
	if buildCommit != "" {
		return buildVersion + " (" + buildCommit + ")"
	}
	return buildVersion
}

// ResolveClaudeBinary returns the absolute path to the claude CLI binary.
// It checks AGENT_QUOTA_CLAUDE_PATH first, then falls back to a PATH lookup.
// Logging the resolved path at debug level lets users audit which binary is used.
func ResolveClaudeBinary() (string, error) {
	if p := os.Getenv("AGENT_QUOTA_CLAUDE_PATH"); p != "" {
		slog.Debug("using claude binary from env override", "path", p)
		return p, nil
	}
	path, err := exec.LookPath("claude")
	if err != nil {
		return "", err
	}
	slog.Debug("resolved claude binary", "path", path)
	return path, nil
}

// ClaudeCLIVersion returns the version of the installed claude CLI,
// or FallbackClaudeVersion if it cannot be determined.
// The result is used in the HTTP User-Agent header sent to the Anthropic API.
func ClaudeCLIVersion() string {
	claudePath, err := ResolveClaudeBinary()
	if err != nil {
		slog.Debug("claude CLI not found in PATH", "error", err)
		return FallbackClaudeVersion
	}
	out, err := exec.Command(claudePath, "--version").Output()
	if err != nil {
		slog.Debug("claude CLI version detection failed", "error", err)
		return FallbackClaudeVersion
	}
	// Output is typically: "2.1.87 (Claude Code)" — take the first field.
	parts := strings.Fields(strings.TrimSpace(string(out)))
	if len(parts) > 0 && parts[0] != "" {
		return parts[0]
	}
	return FallbackClaudeVersion
}
