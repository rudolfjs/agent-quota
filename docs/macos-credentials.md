# macOS credential storage

`agent-quota` supports Claude Code, ChatGPT/Codex, and GitHub Copilot CLI. Gemini is not a provider. Linux continues to use the existing files/environment variables; Linux Secret Service is not queried.

## Audited formats

| Provider | macOS generic-password service | Account | Secret value | File fallback |
| --- | --- | --- | --- | --- |
| Claude Code | `Claude Code-credentials` | `$USER`, otherwise the OS username | Same JSON object as the file: `claudeAiOauth.accessToken`, `refreshToken`, `expiresAt`, etc. | `~/.claude/.credentials.json` |
| ChatGPT / Codex | `Codex Auth` | `cli\|` + first 16 hex characters of SHA-256 of the canonical Codex home path | Same JSON as `auth.json`: `tokens.access_token`, `refresh_token`, `account_id`, etc. | `~/.codex/auth.json` |
| GitHub Copilot CLI | `copilot-cli` | `<host>:<login>`; e.g. `https://github.com:alice` | Raw GitHub OAuth token, **not JSON** | `copilot_tokens` in `~/.copilot/config.json` |

Audit references:

- Claude Code 2.1.295, installed on the development Mac: its bundled Keychain reader uses `security find-generic-password` with the service/account above.
- [Codex auth storage implementation](https://github.com/openai/codex/blob/rust-v0.159.1/codex-rs/login/src/auth/storage.rs): service `Codex Auth`, `compute_store_key`, and the serialized auth structure.
- `@github/copilot` 0.0.420's distributed `index.js`: keytar service `copilot-cli`, account constructed from host/login, with plaintext fallback. The service is also confirmed in [Copilot CLI issue #4273](https://github.com/github/copilot-cli/issues/4273) for 1.0.73/1.0.75. Copilot is not installed on the development Mac, so its integration is fixture-tested, not live-account-tested.

Provider storage is not a public compatibility API. This supports the direct generic-password entries above, not arbitrary credential helpers, IDE-specific stores, or Codex's newer optional encrypted/secrets backend. API keys are not subscription quota credentials. GitHub CLI's own Keychain entries are not assumed to be Copilot CLI tokens.

### Home directories and account selection

- Claude honors `CLAUDE_CONFIG_DIR` for its file. A custom directory adds `-<first 8 SHA-256 hex characters>` to the Keychain service; the hash uses the NFC-normalized directory string, matching the CLI. `CLAUDE_SECURESTORAGE_CONFIG_DIR`, when set, overrides the directory used for the Keychain suffix (an empty value selects the default service).
- Codex honors `CODEX_HOME`. Symlinks are resolved before hashing; if canonicalization fails, the supplied path is hashed as Codex does.
- Copilot uses `last_logged_in_user`, then `logged_in_users`, preserving the exact host string (including scheme). No arbitrary account is selected from Keychain if this metadata is missing.

## Selection and refresh

Claude and Codex prefer their matching Keychain entry on macOS and fall back to files **only when the entry is absent**. A locked/denied store, malformed value, or command failure does not trigger fallback. This prevents quietly using a stale login when permission was denied. The Codex credential-store setting in `config.toml` is not interpreted: if both direct stores exist, Keychain wins.

Copilot resolves `COPILOT_GITHUB_TOKEN`, `GH_TOKEN`, then `GITHUB_TOKEN` first, as before. If `store_token_plaintext` is enabled in its config, it uses file tokens. Otherwise it checks the selected account's Keychain entry, with per-account file fallback when absent, before trying another configured account.

Explicit Go provider options (`WithCredentialsPath`, `WithAuthPath`, `WithConfigPath`) stay file-only, including on macOS. Normal tests use these overrides and injected stores, so they never read a developer's real Keychain.

- **Claude:** the existing CLI refresh is retained, followed by re-reading the selected store. Keychain refresh does not poll a credentials-file modification time. CLI execution is bounded to 30 seconds.
- **Codex:** the existing file-backed OAuth refresh/persistence is retained. On Keychain-backed HTTP 401, `aq` requests `codex login` instead. Rotating a refresh token without safely persisting it back into Codex's store could invalidate its login; this feature deliberately does not write Keychain items or export them into `auth.json`.
- **Copilot:** no automatic refresh was present; authentication errors still request `copilot login`.

## Permissions and security

The reader executes **`/usr/bin/security` by absolute path**, without a shell, passing only service and account selectors. Passwords are captured in memory, never passed as arguments or included in error messages. stderr is discarded, output is bounded, and numeric OSStatus exit codes are mapped to domain errors. Cancellation/timeout and denied/locked access have actionable messages.

`Available()` performs metadata-only queries without `-w`/`-g`. It never asks for a password. Actual quota fetches use `-w`; password reads are serialized and bounded to 60 seconds (including waiting for another prompt). This also applies to JSON/headless mode: use an interactive desktop session to approve access first.

macOS controls whether a prompt appears. With this CGO-free implementation, the requester can be shown as **security**, not **agent-quota**. “Always Allow” authorizes that system tool for the item, potentially for other callers too. Choose “Allow” if you do not want persistent authorization. Already-authorized entries can be read without a new prompt; provider rewrites can cause a prompt to reappear. `aq` never adjusts ACLs, unlocks Keychain with a stored password, or suppresses consent dialogs.

## Testing

Automated tests cover read versus metadata arguments, missing/denied/cancelled access, output limits, credential formats, precedence, source-specific refresh, and token-safe errors. No live provider account is required. CI runs race tests and CGO-free builds on Linux x86_64, Intel macOS, and Apple Silicon macOS.

Optional **read-only** smoke tests on a signed-in Mac (may prompt):

```sh
AQ_TEST_LIVE_KEYCHAIN=1 go test -count=1 -run '^TestLive.*Read$' -v ./internal/claude ./internal/openai
```

These tests validate credentials without printing tokens or making provider HTTP calls. Claude reads its Keychain entry directly; Codex reports whether it used Keychain or the file fallback. A missing Codex entry means its Keychain path has not been live-verified, even when the file fallback passes. Copilot requires a separate signed-in installation for live validation.
