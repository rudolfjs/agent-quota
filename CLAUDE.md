# agent-quota

CLI tool that tracks AI provider OAuth subscription quotas. Pretty TUI for humans, headless JSON for scripts/agents.

See [CONTRIBUTING.md](CONTRIBUTING.md) for build, test, CI, changie, and release workflows.

## Architecture

- `cmd/agent-quota/` — entrypoint, wires cobra + fang, registers providers
- `internal/cli/` — CLI commands (root, status), flag definitions, output mode resolution
- `internal/provider/` — Provider interface, registry, domain types (QuotaResult, Window, ExtraUsage)
- `internal/claude/` — Claude OAuth API client, credential reading, token refresh via CLI
- `internal/config/` — Configuration, settings, response caching
- `internal/copilot/` — GitHub Copilot usage provider
- `internal/errors/` — Domain error types (auth, network, api, config)
- `internal/fileutil/` — Atomic file writes (0o600 perms), insecure-permission warnings
- `internal/gemini/` — Google Gemini usage provider
- `internal/openai/` — OpenAI usage provider
- `internal/tui/` — Bubbletea v2 TUI model, provider cards, lipgloss styles
- `internal/output/` — JSON and text formatters for headless mode
- `internal/version/` — Build-time version injection, claude CLI version detection

### Adding a New Provider

1. Create `internal/<name>/` package
2. Implement `provider.Provider` (Name, FetchQuota, Available)
3. Register in `cmd/agent-quota/main.go`

## Tech Stack Constraints

### Charm TUI v2 (MANDATORY)

- Import: `charm.land/bubbletea/v2`, `charm.land/lipgloss/v2`, `charm.land/bubbles/v2/*`
- **NEVER** use `github.com/charmbracelet/*` import paths — always `charm.land/*` v2
- `View()` returns `tea.View` via `tea.NewView(s)` — **NEVER** return a raw string
- `tea.Quit` without parentheses — it is a `func() Msg` used as a `Cmd` value
- `tea.KeyPressMsg` for key press handling (NOT `tea.KeyMsg`)
- AltScreen: `v.AltScreen = true` on the View struct, NOT via program option
- Sub-components have `View() string` — only the root model returns `tea.View`

### Fang v2

- Import: `charm.land/fang/v2`
- Wrap cobra commands with `fang.Execute(ctx, rootCmd, ...options)`
- Do NOT set `SilenceUsage` or `SilenceErrors` — fang handles that

### Security

- All errors crossing trust boundaries MUST use domain error types (`internal/errors`)
- User-facing messages via `DomainError.Message` — never expose raw `err.Error()`
- **Never** log or display access tokens, even partially (redact as `[REDACTED]`)
- Use `log/slog` exclusively for diagnostics

### Modern Go (1.25)

- `t.Context()` in tests, `slices`/`maps` stdlib packages
- `errors.As` / `errors.Is` for error checking
- `context.Context` on all I/O operations

## Claude OAuth API

- **Endpoint**: `GET https://api.anthropic.com/api/oauth/usage`
- **Headers**: `Authorization: Bearer <token>`, `anthropic-beta: oauth-2025-04-20`, `User-Agent: claude-code/<version>`
- **Credentials**: `~/.claude/.credentials.json` → field `claudeAiOauth.accessToken`
- **Token refresh**: exec `claude` CLI (refreshes automatically), then re-read credentials
