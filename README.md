# agent-quota

CLI tool that fetches AI provider usage/quota data.
Pretty TUI for humans, headless JSON for scripts and agents.

## Install

### Prebuilt binary

The standard release path is:
- GitHub Actions builds binaries on tagged releases
- GitHub Releases hosts the archives and checksums
- `install.sh` downloads the correct archive for your OS/arch

Install the latest release to `~/.local/bin`:

```bash
curl -fsSL https://raw.githubusercontent.com/schnetlerr/agent-quota/main/install.sh | sh
```

Install to `/usr/local/bin` instead:

```bash
curl -fsSL https://raw.githubusercontent.com/schnetlerr/agent-quota/main/install.sh | BIN_DIR=/usr/local/bin sh
```

Install a specific version:

```bash
curl -fsSL https://raw.githubusercontent.com/schnetlerr/agent-quota/main/install.sh | VERSION=v0.1.0 sh
```

### Install with Go

```bash
go install github.com/schnetlerr/agent-quota/cmd/agent-quota@latest
```

### Build from source

```bash
go build -o agent-quota ./cmd/agent-quota/
```

## Usage

```bash
./agent-quota                 # pretty TUI dashboard
./agent-quota --refresh-minutes 2
./agent-quota --json          # one-shot JSON
./agent-quota -p claude       # one-shot JSON for a single provider
./agent-quota status          # one-shot JSON for scripts
```

## Config

Default config path:

```text
~/.config/agent-quota/config.json
```

Example:

```json
{
  "providers": ["claude", "gemini", "openai"],
  "tui": {
    "refresh_minutes": 5
  }
}
```

## Development

```bash
make hooks-install
make release-check
make build
```

## Changelog workflow

This repo uses `changie`.

### For normal PRs

Add one unreleased fragment per logical change:

```bash
changie new --interactive=false --kind Added --body 'New `agent-quota` feature description'
```

### For releases

1. Batch unreleased entries into a release file:

```bash
changie batch 0.1.0
```

2. Merge release notes into `CHANGELOG.md`:

```bash
changie merge
```

3. Commit those files, then tag the commit already merged to `main` and push:

```bash
git tag v0.1.0
git push origin main --tags
```

The GitHub release workflow verifies the tag commit is on `main`, rebuilds from GitHub-hosted runners, and publishes release assets.
