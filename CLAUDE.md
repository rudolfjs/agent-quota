# agent-quota

CLI tool that fetches AI provider usage/quota data. Pretty TUI for humans, headless JSON for scripts/agents.

## Build & Run

```bash
go build -o agent-quota ./cmd/agent-quota/
go test -race ./...
./agent-quota --help
./agent-quota -p claude          # headless JSON output
./agent-quota                    # pretty TUI (requires TTY)
```

## Architecture

- `cmd/agent-quota/` — main entrypoint, wires cobra + fang, registers providers
- `internal/cli/` — CLI commands (root, status), flag definitions, output mode resolution
- `internal/provider/` — Provider interface, registry, domain types (QuotaResult, Window, ExtraUsage)
- `internal/claude/` — Claude OAuth API client, credential reading, token refresh via CLI
- `internal/config/` — Configuration management, settings, response caching
- `internal/copilot/` — GitHub Copilot usage provider
- `internal/errors/` — Domain error types (auth, network, api, config)
- `internal/fileutil/` — Atomic file writes (0o600 perms), insecure-permission warnings
- `internal/gemini/` — Google Gemini usage provider
- `internal/openai/` — OpenAI usage provider
- `internal/tui/` — Bubbletea v2 TUI model, provider cards, lipgloss styles
- `internal/output/` — JSON and text formatters for headless mode
- `internal/version/` — Build-time version injection, claude CLI version detection

## Tech Stack Constraints

### Charm TUI v2 (MANDATORY)

- Import: `charm.land/bubbletea/v2`, `charm.land/lipgloss/v2`, `charm.land/bubbles/v2/*`
- **NEVER** use `github.com/charmbracelet/*` import paths — always `charm.land/*` v2
- `View()` returns `tea.View` via `tea.NewView(s)` — **NEVER** return a raw string
- `tea.Quit` without parentheses — it is a `func() Msg` used as a `Cmd` value
- `tea.KeyPressMsg` for key press handling (NOT `tea.KeyMsg`)
- AltScreen: `v.AltScreen = true` on the View struct, NOT via program option
- Sub-components (spinner, progress, etc.) have `View() string` — only the root model returns `tea.View`

### Fang v2

- Import: `charm.land/fang/v2`
- Wrap cobra commands with `fang.Execute(ctx, rootCmd, ...options)`
- Do NOT set `SilenceUsage` or `SilenceErrors` — fang handles that

### Secure Error Handling

- All errors crossing trust boundaries MUST use domain error types (`internal/errors`)
- User-facing messages via `DomainError.Message` — never expose raw `err.Error()` to users
- Log raw errors via `slog.Debug("...", "error", err.Cause)`
- **Never** log or display access tokens, even partially (redact as `[REDACTED]`)

### Logging

- Use `log/slog` exclusively — no `log` or `fmt.Printf` for diagnostics
- Structured fields: `slog.String("provider", name)`, `slog.Int("status_code", code)`

### Testing

- TDD: write test file first, watch it fail, then implement
- Every package has a `_test.go` file
- HTTP tests use `net/http/httptest`
- File I/O tests use `t.TempDir()`
- Use `t.Context()` (Go 1.25) instead of manual `context.WithTimeout`
- TUI tests use `tea.NewProgram` with `tea.WithInput`, `tea.WithOutput`, `tea.WithContext`

### Modern Go (1.25)

- Use `t.Context()` in tests — no manual context setup
- Use `slices` and `maps` packages from stdlib
- No deprecated patterns: no `ioutil`, no `io/ioutil`
- `errors.As` / `errors.Is` for error checking, not type assertions
- `context.Context` on all I/O operations

## Claude OAuth API

- **Endpoint**: `GET https://api.anthropic.com/api/oauth/usage`
- **Headers**: `Authorization: Bearer <token>`, `anthropic-beta: oauth-2025-04-20`, `User-Agent: claude-code/<version>`
- **Credentials**: `~/.claude/.credentials.json` → field `claudeAiOauth.accessToken`
- **Token refresh**: exec `claude` CLI (no args needed — it refreshes automatically), then re-read credentials file
- **Response fields**: `five_hour`, `seven_day`, `seven_day_oauth_apps`, `seven_day_opus`, `seven_day_sonnet` + `extra_usage`

## Adding a New Provider

1. Create `internal/<name>/` package
2. Implement the `provider.Provider` interface (Name, FetchQuota, Available)
3. Register in `cmd/agent-quota/main.go`: `registry.Register(<name>.New())`

## Makefile Targets

- `make build` — build binary
- `make test` — `go test -race -count=1 ./...`
- `make lint` — `go vet` + `golangci-lint`
- `make fmt` — check gofmt compliance
- `make changie-check` — validate changie fragments exist
- `make release-check` — full pre-release gate (fmt + lint + test + changie + build)
- `make ci` — alias for `release-check`
- `make hooks-install` — `lefthook install`
- `make local-install` — install binary to `~/.local/bin/` (Linux x86_64 only)

## Lefthook Git Hooks

Run `lefthook install` after cloning.

- `pre-commit` (parallel): gofmt, go vet, golangci-lint
- `commit-msg`: conventional commits format required (`feat|fix|docs|...`)
- `pre-push` (parallel): go test -race, go build

## CI Pipeline (`.github/workflows/ci.yml`)

Runs on PR and push-to-main. Three parallel jobs:
- **go-checks** — gofmt, go vet, golangci-lint, test, build, install script syntax
- **lefthook** — runs `pre-commit` and `pre-push` hooks in CI
- **changie** — PRs touching `cmd/`, `internal/`, `go.mod`, `go.sum`, `scripts/install.sh`, or `lefthook.yml` (excluding `_test.go`) require a changie fragment in `.changes/unreleased/`

## Changie Changelog Management

- Config: `.changie.yaml`
- Fragments go in `.changes/unreleased/` (YAML files)
- Kinds: Added (minor), Changed (major), Deprecated (minor), Removed (major), Fixed (patch), Security (patch)
- Release notes: `.changes/<version>.md` — required before tagging

## Release Pipeline (`.github/workflows/release.yml`)

Triggered by `v*` tags on main:
1. **verify-tag** — tag must be on main, `.changes/<version>.md` must exist, tests + build pass
2. **build** — cross-compile linux/amd64 with `-ldflags` version injection, package as `.tar.gz`
3. **publish** — GitHub Release with changie notes, artifacts, checksums, and `scripts/install.sh`

## Jules Dispatch Workflows

Comment-triggered via `@jules-*` mentions (OWNER/MEMBER only):
- `@jules-issue` on issues — triage assessment, no code changes
- `@jules-swe` on issues/PRs — implementation with tests and PR
- `@jules-security` on PRs — security review of PR diff (Sentinel)
