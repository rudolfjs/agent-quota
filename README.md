# Agent Quota Dashboard

CLI tool that tracks AI provider **OAuth subscription** quotas and usage limits — the rate limits you hit when using tools like Claude Code, GitHub Copilot, and other AI assistants through their CLI/IDE integrations.

Pretty TUI for humans, headless JSON for scripts and agents.

> **Not for API usage.** This tool reads the OAuth-based subscription quotas exposed by provider CLIs, not API key billing. If you pay per-token via the API, this isn't the tool for you.

> Supported platforms: Linux x86_64, macOS Intel, and macOS Apple Silicon.
> Windows users should run `aq` under WSL2; PowerShell is unsupported because the provider CLIs and credential stores are not consistent there.

## Quick View Example

<p align="center">
  <img src="docs/img/qexample.png" alt="Agent Quota TUI example">
</p>

## Install

### Prebuilt binary

The standard release path is:
- GitHub Actions builds Linux x86_64, macOS Intel, and macOS Apple Silicon binaries on tagged releases
- GitHub Releases hosts the archives and checksums
- `install.sh` downloads the correct archive for your supported platform

Install the latest release into `~/.local/bin`:

```bash
curl -fsSL https://raw.githubusercontent.com/rudolfjs/agent-quota/main/scripts/install.sh | sh
```

Custom installation instructions:

```bash
# /usr/local/bin
curl -fsSL https://raw.githubusercontent.com/rudolfjs/agent-quota/main/scripts/install.sh | BIN_DIR=/usr/local/bin sh
# Install a specific release version:
curl -fsSL https://raw.githubusercontent.com/rudolfjs/agent-quota/main/scripts/install.sh | VERSION=v0.1.1 sh
# Skip the confirmation prompt:
curl -fsSL https://raw.githubusercontent.com/rudolfjs/agent-quota/main/scripts/install.sh | YES=1 sh
```

### Install with Go or build from source

Source builds are supported on Linux x86_64, macOS Intel, and macOS Apple Silicon.

```bash
# Go
go install github.com/rudolfjs/agent-quota/cmd/agent-quota@latest
# Source
go build -o agent-quota ./cmd/agent-quota/
```

## Usage

`aq` is installed as a short alias for `agent-quota`.

```bash
aq                            # pretty TUI dashboard
aq --refresh-minutes 5
aq --json                     # one-shot JSON
aq -p claude                  # one-shot JSON for a single provider
aq -p copilot                 # GitHub Copilot CLI quota
aq status                     # one-shot JSON for scripts
```

## Config

Default config paths:

```text
~/.config/agent-quota/providers.json
~/.config/agent-quota/settings.json
```

Provider selection example:

```json
{
  "providers": ["claude", "gemini", "openai", "copilot"]
}
```

TUI settings example:

```json
{
  "provider_order": ["claude", "openai", "gemini", "copilot"],
  "tui": {
    "hide_header": false,
    "refresh_minutes": 15
  }
}
```

## Provider setup

- Claude: `claude` CLI login (if 429 or 403 errors occur, re-authenticate Claude Code to get a new OAuth token)
- OpenAI: `codex login`
- Gemini: `gemini` CLI login
- Copilot: `copilot login`

### macOS Keychain

On macOS, provider CLIs store OAuth credentials in the system Keychain. The first `aq` read for a provider can show a prompt such as `agent-quota wants to access ... in your keychain`; choose **Always Allow** so future quota checks can run unattended.

Credential entries read by `aq`:

- Claude: service `Claude Code-credentials`; value is the same JSON shape as `~/.claude/.credentials.json`
- Gemini: service `gemini-cli-oauth`, account `main-account`; value is the Gemini CLI OAuth token record
- OpenAI Codex: service `Codex Auth`, account `cli|<16 hex chars>` where the suffix is the first 16 hex characters of the SHA-256 digest of the canonical resolved `CODEX_HOME` path; value is the Codex auth JSON
- Copilot: service `gh:github.com`; value is the GitHub CLI token used by Copilot

## Development

```bash
make install-deps     # first time: install tools, hooks, and module deps
make release-check
make build
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for the full development, changie, and release workflow.
