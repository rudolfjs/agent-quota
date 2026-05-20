# Remove Gemini provider — design spec

**Date:** 2026-05-20
**Branch:** `feat/remove-gemini`
**Goal:** Remove the Gemini OAuth-quota provider from `agent-quota` entirely. Single PR against `main`. Do NOT merge.

> **Note:** this spec is a planning artifact for the session. It is committed to the working branch so the design can be reviewed adversarially, then deleted in a final commit before the PR is raised. The shipped PR diff against `main` is zero for this file.

---

## 1. Scope and approach

Surgical removal in a single PR with logical commits, mirroring how `jules` was previously removed via `isRemovedProviderName` in `internal/config/config.go`. Net result:

- Delete the `internal/gemini/` package.
- Strip every Gemini-specific code path, theme constant, model-grouping helper, and 24-hour-window heuristic from the TUI.
- Add `"gemini"` to `isRemovedProviderName()` so users' `settings.json` self-heals on next load (drops stale entries from `providers`, `provider_order`, and `quick_view`).
- Rename `"gemini"` test stubs to `"fake"` where the tests exercise generic 3+-provider behavior. Delete tests that assert Gemini-specific behavior (purple theme, 24h-reset, model grouping).
- Scrub Gemini from `README.md`, `CLAUDE.md`, and the two Jules dispatch workflow prompts.
- Preserve historical release notes: `CHANGELOG.md` v0.1.0 entry and `.changes/0.1.0.md` are untouched.
- Add a `removed` changie fragment under `.changes/unreleased/`.

## 2. Touchpoint inventory

### 2.1 Delete

- `internal/gemini/` — entire directory:
  - `gemini.go`, `gemini_test.go`, `refresh.go`, `refresh_test.go`, `security_test.go`

### 2.2 Edit — wiring

- `cmd/agent-quota/main.go`:
  - Line 21: drop `"github.com/rudolfjs/agent-quota/internal/gemini"` import
  - Line 125: `--provider` flag help — remove `gemini` from the example list
  - Line 126: `--model` flag help — replace `gemini-3-flash-preview` example with an OpenAI or Claude window name
  - Line 283: drop `registry.Register(gemini.New())`
- `cmd/agent-quota/main_test.go`:
  - Lines 16, 18–19, 33–34, 47–48, 59–60: `TestFilterByModel_*` tests use `Provider: "gemini"` with `gemini-3-*` windows. The function under test (`filterByModel`) is provider-agnostic; rewrite fixtures using a `Provider: "openai"` with window names that already exist in the OpenAI provider (e.g. `gpt-4o`, `gpt-4o-mini`) so the test reads as exercising a real provider's filter behavior.
  - Line 124: `TestNewRegistry_doesNotRegisterJules` iterates `[]string{"claude", "openai", "gemini", "copilot"}`. Drop `"gemini"` from the slice.

### 2.3 Edit — TUI surgical deletions

- `internal/tui/styles.go`:
  - Line 13: delete `geminiColorHex = "#8B5CF6"` constant
  - Lines 154–172: delete the `case "gemini":` branch in `themeForProvider`
  - Line 445: in `logoBarsView()`, delete the single line `providerTitleStyle(themeForProvider("gemini", palette)).Render("▌"),` — the startup logo becomes a 3-bar render (claude, openai, copilot)
- `internal/tui/provider_card.go`:
  - Lines 127–128: delete `case "gemini": return "Gemini"` in `providerDisplayName`
  - Lines 311–314: delete the `if strings.HasPrefix(name, "gemini-") { return 24 * time.Hour }` branch in `windowDuration`, including the leading comment
- `internal/tui/quick_view.go`:
  - Lines 230–238: delete the `if prov == "gemini" { ... geminiModelGroup ... }` branch in `metricDisplayFor`, including the leading comment
  - Lines 242–269: delete the entire `geminiModelGroup()` function
  - Line 295: comment block in `prettyRawName` references `"gemini-2.5-flash" → "Gemini 2.5 Flash"` — delete that fragment from the comment, keeping the `"premium_interactions" → "Premium Interactions"` example
- `internal/tui/model.go`:
  - Line 751: drop `"gemini"` from the default `providerNames = []string{"claude", "openai", "gemini", "copilot"}`

### 2.4 Edit — settings self-heal

- `internal/config/config.go`:
  - Line 129 — extend `isRemovedProviderName()`:
    ```go
    case "jules", "gemini":
        return true
    ```
  - This single change handles `providers`, `provider_order` (via `normalizeProviders`) AND `quick_view` (via `normalizeQuickViewMetricID`, which calls `isRemovedProviderName` at settings.go:200). No new code paths needed. File self-heals on next `SaveSettings` call.

### 2.5 Edit — tests (rename `"gemini"` → `"fake"`, delete Gemini-specific assertions)

- **Rename** stub provider name from `"gemini"` to `"fake"` in:
  - `internal/cli/status_test.go:38`
  - `internal/config/config_test.go` lines 49, 58, 161
  - `internal/config/settings_test.go` lines 37–38, 69, 83, 88, 122, 131
  - `internal/provider/registry_test.go:48`
  - `internal/tui/menu_test.go` (~15 stub sites + assertion sites)
  - `internal/tui/model_test.go` (~6 stub sites)
- **Update display-name assertions** in TUI tests — `providerDisplayName("fake")` falls through to lowercase `"fake"`:
  - `internal/tui/menu_test.go:94, 100, 769, 840, 936–937` (e.g. `● Gemini` / `○ Gemini` markers)
  - `internal/tui/menu_test.go:775` — **negative** assertion; update so it would still meaningfully fail if regressed (don't let the rename quietly preserve it)
  - `internal/tui/menu_test.go:229` — comment `// Gemini` → `// fake`
  - `internal/tui/model_test.go:183` — drop `"Gemini"` from the capital-case display name list
  - `internal/tui/model_test.go:443` — drop `"Gemini"` from generic Contains check
- **Delete** tests that exercise Gemini-specific code:
  - `internal/tui/provider_card_test.go::TestRenderProviderCard_geminiKeepsPurpleTheme` (lines 291–306)
  - `internal/tui/provider_card_test.go` table rows for `{"gemini-2.5-pro", 24h}`, `{"gemini-2.5-flash", 24h}`, `{"gemini-3.1-pro-preview", 24h}` (lines 381–383)
  - Any `TestGeminiModelGroup` / `TestQuickViewGeminiGroup` cases if present (search before deleting; `geminiModelGroup` is package-private)
- **Repurpose** `TestRenderProviderCard_unavailableStatus` (line 188) — provider is `"gemini"` but the test isn't Gemini-specific; rename fixture to `"fake"`.

### 2.6 Edit — provider interface docstring

- `internal/provider/provider.go:17` — docstring example `(e.g., "claude", "openai", "gemini")` → drop gemini.

### 2.7 Edit — docs

- `README.md`:
  - Line 81: drop `"gemini"` from the JSON `providers` example
  - Line 89: drop `"gemini"` from the JSON `provider_order` example
  - Line 101: delete the `Gemini: gemini CLI login` bullet
- `CLAUDE.md`:
  - Line 17: delete the `internal/gemini/` arch entry
  - Lines 67–73: delete the entire "Gemini Code Assist API" section

### 2.8 Edit — Jules dispatch workflows

- `.github/workflows/jules-swe-dispatch.yml:175` — replace `feat(provider): add Gemini usage provider` example with something current (e.g. `fix(tui): correct copilot card spacing`)
- `.github/workflows/jules-security-dispatch.yml:90` — drop "Gemini" from the credential-safety provider list

### 2.9 Add

- `.changes/unreleased/removed-<changie-generated>.yaml` — changie `removed` fragment: "Remove Gemini provider — gemini OAuth quota tracking is no longer supported. Existing `settings.json` entries referencing `gemini` are silently dropped on next launch."

### 2.10 Preserve (intentionally untouched)

- `CHANGELOG.md` v0.1.0 entry
- `.changes/0.1.0.md`

## 3. Settings migration

Adding `"gemini"` to `isRemovedProviderName()` is the **entire** migration. Trace through for a user with this `settings.json`:

```json
{
  "providers": ["claude", "gemini", "openai"],
  "provider_order": ["openai", "claude", "gemini"],
  "quick_view": ["claude:five_hour", "gemini:gemini-3-pro-preview"]
}
```

After `LoadSettings`:
- `providers` → `["claude", "openai"]` (via `normalizeProviders` → `isRemovedProviderName` filter, `config.go:113`)
- `provider_order` → `["openai", "claude"]` (same path)
- `quick_view` → `["claude:five_hour"]` (via `normalizeQuickViewMetricID` → `isRemovedProviderName` filter, `settings.go:200`)

The cleaned in-memory `Settings` drives the TUI. The disk file isn't rewritten until the next `SaveSettings` call (any TUI state change), at which point `SaveSettings` re-normalizes before writing — so `settings.json` self-heals.

### Edge cases

- **Only Gemini in `providers`** — normalizes to `[]`, which `ApplyProviderSelection` treats as "all providers" via the `len(selected) == 0` fallback at `settings.go:110`. User sees full provider dashboard instead of empty.
- **`--provider gemini` CLI flag** — registry lookup returns "not found", existing error path handles it. No new code.
- **`--model gemini-3-flash-preview`** — passes through as a model filter, matches nothing. Existing behavior.

## 4. Test rename strategy

**Pattern:** `"gemini"` → `"fake"` everywhere a test uses gemini as a generic third-provider stub name. `providerDisplayName("fake")` falls through to the lowercase `default: return name` branch — so view assertions become `Contains(view, "fake")` instead of `Contains(view, "Gemini")`. No production-code special-case for the stub.

**Why rename instead of delete:**
- Tests cover generic 3+-provider behavior (multi-step ordering, menu pagination, quickview picker scrolling). Deleting them reduces coverage on real code that still ships.

**What gets deleted:**
- `TestRenderProviderCard_geminiKeepsPurpleTheme` — the gemini theme no longer exists.
- The three `gemini-*` rows in `provider_card_test.go::TestWindowDuration` — they exercise the deleted 24h-reset branch.

**What gets repurposed:**
- `TestRenderProviderCard_unavailableStatus` uses `"gemini"` as the provider, but the test asserts unavailable-status rendering — provider name is incidental. Rename to `"fake"`.

**Negative-assertion trap (`menu_test.go:775`):**
The assertion `!Contains(view, "Claude → OpenAI → Gemini")` is a guard against "Current order" leaking into a specific view. After rename, that exact string naturally won't appear (it'd be `"Claude → OpenAI → fake"`), so the assertion passes for the wrong reason. Update the assertion to test against the *post-rename* sequence so a real regression still fails the test.

## 5. Hooks resolution (lefthook)

`lefthook.yml` defines:

| Hook | Commands | Strategy |
|---|---|---|
| pre-commit | `gofmt -l`, `go vet ./...`, `golangci-lint run ./...` | Run `gofmt -w` on edited files belt-and-braces; deletions can't introduce vet/lint issues; verify no `unused` complaints on orphaned helpers |
| commit-msg | Conventional Commits regex | Use `refactor:` / `test:` / `docs:` / `ci:` / `chore:` prefixes |
| pre-push | `go test -race -count=1 ./...`, `go build ./cmd/agent-quota/`, changie fragment check | Tests pass after stub renames; build clean once all package references are scrubbed; changie fragment added |

**Risk: `golangci-lint`'s `unused` linter.** After deleting the gemini case in `themeForProvider`, the `geminiColorHex` constant has no remaining callers. The deletion must be complete — constant and all callers gone in the same commit. Same for `geminiModelGroup()`: function and call site go together.

## 6. Verification

**Local (pre-push):**
1. `gofmt -l .` — no output
2. `go vet ./...` — clean
3. `golangci-lint run ./...` — clean
4. `go test -race -count=1 ./...` — green
5. `go build ./cmd/agent-quota/` — clean binary
6. `git diff --name-only origin/main HEAD | xargs grep -i gemini` — empty (no surviving references in changed files)
7. Manual: `./agent-quota --provider gemini` returns clean "unknown provider" error

**CI (post-push, on PR):**
- Standard PR checks (lefthook-mirroring CI, CodeQL) all green
- Jules dispatch workflows aren't CI gates (manual dispatch only)

**Acceptance:**
- All PR checks green
- **Do NOT merge** per session goal

## 7. Commit structure

One logical commit per concern, all on `feat/remove-gemini`. Each commit compiles and passes tests independently — no broken intermediate state.

1. `refactor(provider): drop gemini provider registration and package`
   - Delete `internal/gemini/`
   - Remove import + registration + flag-help references in `cmd/agent-quota/main.go`
   - Rewrite `cmd/agent-quota/main_test.go` filter tests with non-gemini fixtures; drop gemini from `TestNewRegistry_doesNotRegisterJules`
   - Drop gemini from `internal/provider/provider.go` docstring

2. `refactor(tui): remove gemini-specific theme, model grouping, and 24h-reset branch`
   - `styles.go`: drop `geminiColorHex` and the `"gemini"` theme case + sample call site
   - `provider_card.go`: drop `providerDisplayName` gemini case + `windowDuration` gemini-prefix branch
   - `quick_view.go`: drop `metricDisplayFor` gemini branch + `geminiModelGroup()` function + comment example
   - `model.go`: drop gemini from default `providerNames`

3. `refactor(config): add gemini to isRemovedProviderName for settings self-heal`

4. `test: rename gemini stubs to fake and remove gemini-specific assertions`
   - Rename across 7 test files
   - Delete `TestRenderProviderCard_geminiKeepsPurpleTheme` and gemini rows in `TestWindowDuration`
   - Fix negative-assertion trap in `menu_test.go:775`

5. `docs: remove gemini from README and CLAUDE.md`

6. `ci(jules): scrub gemini from dispatch prompts`

7. `chore(changie): add removed fragment for gemini provider`

8. (Final, after codex review) `chore: remove planning spec` — deletes this spec doc

## 8. Out of scope

Tracked separately, not part of this PR:
- [#3](https://github.com/rudolfjs/agent-quota/issues/3) — generalized self-heal for any provider that becomes unavailable at runtime (the `isRemovedProviderName` approach used here handles only statically-known removed providers)

## 9. Review checkpoints

Per session workflow:
- [x] Approach approved (Approach A: surgical, single PR)
- [x] Section 1 (touchpoint inventory) approved, with mid-flight corrections from self-verification
- [x] Section 2 (settings + tests + hooks) approved
- [x] Section 3 (verification + commit structure) approved
- [ ] Spec self-review (next)
- [ ] User reviews spec file
- [ ] `/codex:rescue` adversarial review of spec/plan — loop until resolved
- [ ] Execute the plan
- [ ] `/codex:rescue` adversarial review of executed work — loop until resolved
- [ ] Delete this spec doc
- [ ] Push branch + raise PR
- [ ] CI green
