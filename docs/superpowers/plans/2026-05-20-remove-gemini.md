# Remove Gemini provider — implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove the Gemini OAuth-quota provider from `agent-quota` entirely, in a single PR against `main`. Do NOT merge.

**Architecture:** Surgical removal across 22 files, mirroring how `jules` was previously removed. Pure subtraction of one provider plus its TUI-specific theme/grouping/window-reset logic. Existing user `settings.json` files self-heal on load via the existing `isRemovedProviderName()` denylist pattern. Test stubs that used `"gemini"` as a generic third-provider name are renamed to `"fake"` to preserve multi-provider test coverage; tests that asserted Gemini-specific behavior (purple theme, 24h-reset, model grouping) are deleted.

**Tech Stack:** Go 1.25, `charm.land/bubbletea/v2` (TUI), `lipgloss/v2` (styling), `cobra` + `fang/v2` (CLI), `changie` (changelogs), `lefthook` (git hooks), `golangci-lint` (lint), standard `go test -race`.

**Reference spec:** `docs/superpowers/specs/2026-05-20-remove-gemini-design.md` (planning artifact; will be deleted by the final task before the PR is pushed).

**Ordering rationale:** Spec lists 7 logical commits but their stated order would break tests mid-stream (test assertions on `"Gemini"` display names would fail once the TUI cases are removed). The plan reorders so test renames happen *before* the TUI deletions they correspond to.

**Planning-doc deletion timing:** The spec said "delete before PR is raised". The plan does the deletion in Task 11 — *after* CI is green on the initial PR push — to avoid an earlier task deleting the file an executor is driving from. The PR's final diff against `main` for `docs/superpowers/` is zero after Task 11's followup push.

---

## File structure

**Delete:**
- `internal/gemini/gemini.go`
- `internal/gemini/gemini_test.go`
- `internal/gemini/refresh.go`
- `internal/gemini/refresh_test.go`
- `internal/gemini/security_test.go`
- `docs/superpowers/specs/2026-05-20-remove-gemini-design.md` (final task only)

**Modify (Go source):**
- `cmd/agent-quota/main.go` — drop gemini import, registration, flag help mentions
- `cmd/agent-quota/main_test.go` — rewrite `TestFilterByModel_*` fixtures, drop gemini from `TestNewRegistry_doesNotRegisterJules` list
- `internal/provider/provider.go` — drop gemini from `Name()` docstring example
- `internal/tui/styles.go` — drop `geminiColorHex` constant, `case "gemini":` theme branch, and gemini bar in `logoBarsView()`
- `internal/tui/provider_card.go` — drop gemini case in `providerDisplayName`, drop gemini-prefix branch in `windowDuration`
- `internal/tui/quick_view.go` — drop gemini branch in `metricDisplayFor`, delete `geminiModelGroup()` function, trim gemini example from `prettyRawName` comment
- `internal/tui/model.go` — drop `"gemini"` from default `providerNames` slice
- `internal/config/config.go` — extend `isRemovedProviderName()` to include `"gemini"`

**Modify (Go tests):**
- `internal/tui/menu_test.go` — rename stubs `"gemini"` → `"fake"`, update assertions, fix negative-assertion trap, update `isMenuBodyLine` helper
- `internal/tui/model_test.go` — rename stubs and drop `"Gemini"` from display-name lists
- `internal/tui/provider_card_test.go` — rename incidental gemini fixture; **delete** `TestRenderProviderCard_geminiKeepsPurpleTheme` and the three `gemini-*` rows in `TestWindowDuration`
- `internal/cli/status_test.go` — rename
- `internal/config/config_test.go` — rename (and add a new `TestLoad_dropsRemovedGeminiProviderFromConfig` test in the config self-heal task)
- `internal/config/settings_test.go` — rename
- `internal/provider/registry_test.go` — rename

**Modify (docs + CI):**
- `README.md` — drop gemini from JSON examples and login-required bullet list
- `CLAUDE.md` — drop `internal/gemini/` arch entry and the "Gemini Code Assist API" section
- `.github/workflows/jules-swe-dispatch.yml` — replace `feat(provider): add Gemini usage provider` example
- `.github/workflows/jules-security-dispatch.yml` — drop "Gemini" from credential-safety provider list

**Create:**
- `.changes/unreleased/removed-*.yaml` — changie `Removed` fragment

**Preserve (do NOT touch):**
- `CHANGELOG.md` v0.1.0 entry
- `.changes/0.1.0.md`

---

## Task 1: Drop provider package + wiring

**Files:**
- Delete: `internal/gemini/gemini.go`, `internal/gemini/gemini_test.go`, `internal/gemini/refresh.go`, `internal/gemini/refresh_test.go`, `internal/gemini/security_test.go`
- Modify: `cmd/agent-quota/main.go:21`, `cmd/agent-quota/main.go:125-126`, `cmd/agent-quota/main.go:283`
- Modify: `cmd/agent-quota/main_test.go:14-74` and `:117-129`
- Modify: `internal/provider/provider.go:17`

- [ ] **Step 1: Delete the gemini package directory**

```bash
git rm -r internal/gemini/
```

Expected: 5 files deleted (`gemini.go`, `gemini_test.go`, `refresh.go`, `refresh_test.go`, `security_test.go`).

- [ ] **Step 2: Drop the gemini import in `cmd/agent-quota/main.go`**

In the imports block (around line 21), delete this line:

```go
	"github.com/rudolfjs/agent-quota/internal/gemini"
```

- [ ] **Step 3: Update `--provider` and `--model` flag help text**

Around line 125-126 of `cmd/agent-quota/main.go`, change:

```go
	rootCmd.Flags().StringVarP(&providerFlag, "provider", "p", "", "specific provider to query (e.g. claude, openai, gemini, copilot)")
	rootCmd.Flags().StringVarP(&modelFlag, "model", "m", "", "filter output to a specific model window (e.g. gemini-3-flash-preview)")
```

to:

```go
	rootCmd.Flags().StringVarP(&providerFlag, "provider", "p", "", "specific provider to query (e.g. claude, openai, copilot)")
	rootCmd.Flags().StringVarP(&modelFlag, "model", "m", "", "filter output to a specific model window (e.g. gpt-4o)")
```

- [ ] **Step 4: Drop the gemini registry registration**

Around line 283 of `cmd/agent-quota/main.go`, delete this line:

```go
	registry.Register(gemini.New())
```

- [ ] **Step 5: Rewrite `TestFilterByModel_*` fixtures in `cmd/agent-quota/main_test.go`**

Replace lines 14–74 (the four `TestFilterByModel_*` functions) with these provider-agnostic versions using OpenAI fixtures:

```go
func TestFilterByModel_keepsMatchingWindows(t *testing.T) {
	results := []provider.QuotaResult{{
		Provider: "openai",
		Windows: []provider.Window{
			{Name: "gpt-4o"},
			{Name: "gpt-4o-mini"},
		},
	}}
	got := filterByModel(results, "gpt-4o")
	if len(got[0].Windows) != 1 {
		t.Fatalf("len(windows) = %d, want 1", len(got[0].Windows))
	}
	if got[0].Windows[0].Name != "gpt-4o" {
		t.Errorf("window name = %q, want %q", got[0].Windows[0].Name, "gpt-4o")
	}
}

func TestFilterByModel_emptySliceWhenNoMatch(t *testing.T) {
	results := []provider.QuotaResult{{
		Provider: "openai",
		Windows:  []provider.Window{{Name: "gpt-4o"}},
	}}
	got := filterByModel(results, "gpt-4o-mini")
	if got[0].Windows == nil {
		t.Error("Windows should be empty slice, not nil")
	}
	if len(got[0].Windows) != 0 {
		t.Errorf("len(windows) = %d, want 0", len(got[0].Windows))
	}
}

func TestFilterByModel_noopWhenModelEmpty(t *testing.T) {
	results := []provider.QuotaResult{{
		Provider: "openai",
		Windows:  []provider.Window{{Name: "gpt-4o"}, {Name: "gpt-4o-mini"}},
	}}
	got := filterByModel(results, "")
	if len(got[0].Windows) != 2 {
		t.Errorf("len(windows) = %d, want 2 (no filter when model is empty)", len(got[0].Windows))
	}
}

func TestFilterByModel_filtersAcrossMultipleProviders(t *testing.T) {
	results := []provider.QuotaResult{
		{
			Provider: "openai",
			Windows:  []provider.Window{{Name: "gpt-4o"}, {Name: "gpt-4o-mini"}},
		},
		{
			Provider: "claude",
			Windows:  []provider.Window{{Name: "five_hour"}, {Name: "weekly"}},
		},
	}
	got := filterByModel(results, "gpt-4o")
	if len(got[0].Windows) != 1 || got[0].Windows[0].Name != "gpt-4o" {
		t.Errorf("openai: unexpected windows %v", got[0].Windows)
	}
	if len(got[1].Windows) != 0 {
		t.Errorf("claude: expected 0 windows, got %d", len(got[1].Windows))
	}
}
```

- [ ] **Step 6: Drop gemini from `TestNewRegistry_doesNotRegisterJules`**

Around line 124 of `cmd/agent-quota/main_test.go`, change:

```go
	for _, name := range []string{"claude", "openai", "gemini", "copilot"} {
```

to:

```go
	for _, name := range []string{"claude", "openai", "copilot"} {
```

- [ ] **Step 7: Update provider interface docstring**

In `internal/provider/provider.go` around line 17, change:

```go
	// Name returns the provider's identifier (e.g., "claude", "openai", "gemini").
```

to:

```go
	// Name returns the provider's identifier (e.g., "claude", "openai", "copilot").
```

- [ ] **Step 8: Verify build, vet, and tests pass**

Run:

```bash
gofmt -w cmd/agent-quota/main.go cmd/agent-quota/main_test.go internal/provider/provider.go
go vet ./...
go build ./cmd/agent-quota/
go test -race -count=1 ./...
```

Expected: vet clean, build succeeds, all tests pass. Note: the TUI tests still reference `stubProvider{name: "gemini"}` and assert `Contains(view, "Gemini")` — these still pass because `providerDisplayName` still has its `case "gemini":` branch (we delete it in Task 3).

- [ ] **Step 9: Commit**

```bash
git add -A
git commit -m "$(cat <<'EOF'
refactor(provider): drop gemini provider registration and package

Delete the internal/gemini package, drop its registration in
cmd/agent-quota/main.go, scrub gemini from flag help text and the
provider.Provider docstring, and rewrite the TestFilterByModel fixtures
with OpenAI/Claude windows so the filter tests no longer depend on
gemini model identifiers.
EOF
)"
```

---

## Task 2: Rename test stubs `"gemini"` → `"fake"` and delete gemini-specific assertions

**Files:**
- Modify: `internal/tui/menu_test.go` (15+ sites)
- Modify: `internal/tui/model_test.go` (6+ sites)
- Modify: `internal/tui/provider_card_test.go` (delete 1 test, delete 3 table rows, rename 1 fixture)
- Modify: `internal/cli/status_test.go:38`
- Modify: `internal/config/config_test.go:161` (rename only; line 49/58 left for Task 4)
- Modify: `internal/config/settings_test.go:37-38, 69, 83, 88, 122, 131`
- Modify: `internal/provider/registry_test.go:48`

This task does NOT touch production code — only test stubs and assertions. After this task, no test mentions `"gemini"` or `"Gemini"` except the line 49/58 case-folding fixture in `config_test.go` (handled in Task 4).

- [ ] **Step 1: Rename stubs in `internal/cli/status_test.go`**

Around line 38, change:

```go
		&statusProvider{name: "gemini", available: true},
```

to:

```go
		&statusProvider{name: "fake", available: true},
```

- [ ] **Step 2: Rename stubs in `internal/provider/registry_test.go`**

Around line 48, change:

```go
	reg.Register(&fakeProvider{name: "gemini", available: true})
```

to:

```go
	reg.Register(&fakeProvider{name: "fake", available: true})
```

(Verified: no existing `name: "fake"` collisions in this file — surrounding stubs use `"claude"`, `"openai"`, `"a"`, `"b"`.)

- [ ] **Step 3: Rename stubs in `internal/config/config_test.go`**

Around line 161, change:

```go
	reg.Register(&fakeProvider{name: "gemini", available: true})
```

to:

```go
	reg.Register(&fakeProvider{name: "fake", available: true})
```

Leave lines 49 and 58 alone — those test case-folding normalization and will be updated in Task 4 alongside the production change.

- [ ] **Step 4: Rename stubs in `internal/config/settings_test.go`**

Update these specific occurrences:

- Line 37: `ProviderOrder: []string{"claude", "openai", "gemini"}` → `ProviderOrder: []string{"claude", "openai", "fake"}`
- Line 38: `QuickView: []string{"claude:five_hour", "gemini:gemini-3-pro-preview"}` → `QuickView: []string{"claude:five_hour", "fake:metric_a"}`
- Line 69: `&fakeProvider{name: "gemini", available: true}` → `&fakeProvider{name: "fake", available: true}`
- Line 83: same as line 69
- Line 88: `want := []string{"openai", "claude", "gemini"}` → `want := []string{"openai", "claude", "fake"}`
- Line 122: file content `[" Claude : five_hour ","gemini:gemini-3-pro-preview","","GEMINI:gemini-3-pro-preview"]` → `[" Claude : five_hour ","fake:metric_a","","FAKE:metric_a"]`
- Line 131: `want := []string{"claude:five_hour", "gemini:gemini-3-pro-preview"}` → `want := []string{"claude:five_hour", "fake:metric_a"}`

- [ ] **Step 5: Rename stubs in `internal/tui/menu_test.go`**

There are ~15 occurrences. Apply these patterns:

- Every `&stubProvider{name: "gemini"}` → `&stubProvider{name: "fake"}` (sites at lines 78, 120, 133, 207, 683, 743, 783, 818)
- Every `"gemini"` in a `wantUp`/`wantDown`/`want` string slice (lines 752, 794, 806, 852) → `"fake"`
- Line 94: `if !strings.Contains(view, "Claude") || !strings.Contains(view, "Gemini") || !strings.Contains(view, "Copilot")` → `if !strings.Contains(view, "Claude") || !strings.Contains(view, "fake") || !strings.Contains(view, "Copilot")`
- Line 100: `if !strings.Contains(view, "● Claude") || !strings.Contains(view, "● Gemini") || !strings.Contains(view, "● Copilot")` → `if !strings.Contains(view, "● Claude") || !strings.Contains(view, "● fake") || !strings.Contains(view, "● Copilot")`
- Line 229: `m.menuCursor = 1 // Gemini` → `m.menuCursor = 1 // fake`
- Line 769: `if !strings.Contains(view, "1. Claude") || !strings.Contains(view, "2. OpenAI") || !strings.Contains(view, "3. Gemini")` → `if !strings.Contains(view, "1. Claude") || !strings.Contains(view, "2. OpenAI") || !strings.Contains(view, "3. fake")`
- Line 775 (negative-assertion trap): `if strings.Contains(view, "Current order") || strings.Contains(view, "Claude → OpenAI → Gemini")` → `if strings.Contains(view, "Current order") || strings.Contains(view, "Claude → OpenAI → fake")`
- Line 840: `if !strings.Contains(view, "1. Claude") || !strings.Contains(view, "2. Gemini") || !strings.Contains(view, "3. OpenAI  PICKED")` → `if !strings.Contains(view, "1. Claude") || !strings.Contains(view, "2. fake") || !strings.Contains(view, "3. OpenAI  PICKED")`
- Lines 936–937 in `isMenuBodyLine` helper: change `strings.Contains(trimmed, "● Gemini")` and `strings.Contains(trimmed, "○ Gemini")` to `strings.Contains(trimmed, "● fake")` and `strings.Contains(trimmed, "○ fake")`

- [ ] **Step 6: Rename stubs in `internal/tui/model_test.go`**

- Line 183: `for _, want := range []string{"Claude", "OpenAI", "Gemini", "Copilot"}` → `for _, want := range []string{"Claude", "OpenAI", "Copilot"}` (drop Gemini entirely — `providerDisplayName("fake")` returns `"fake"` lowercase, not a capital-cased name; no equivalent display name to assert)
- Line 443: change `if !strings.Contains(got, "Claude") || !strings.Contains(got, "OpenAI") || !strings.Contains(got, "Gemini")` to `if !strings.Contains(got, "Claude") || !strings.Contains(got, "OpenAI") || !strings.Contains(got, "Copilot")`. Context: this is in a `headerView()` test asserting provider chips appear. After Task 3 drops gemini from the default `providerNames`, the header renders Claude/OpenAI/Copilot, so substitute Copilot for Gemini in the assertion.
- Lines 557, 581: `&stubProvider{name: "gemini"}` → `&stubProvider{name: "fake"}`
- Lines 564, 588: `for _, name := range []string{"claude", "gemini"}` → `for _, name := range []string{"claude", "fake"}`

- [ ] **Step 7: Update `internal/tui/provider_card_test.go`**

This file has THREE distinct changes:

1. **Rename incidental fixture** at line 188 in `TestRenderProviderCard_unavailableStatus`:

```go
	r := provider.QuotaResult{
		Provider:  "gemini",
		Status:    "unavailable",
		FetchedAt: time.Now(),
	}
```

to:

```go
	r := provider.QuotaResult{
		Provider:  "fake",
		Status:    "unavailable",
		FetchedAt: time.Now(),
	}
```

2. **Delete `TestRenderProviderCard_geminiKeepsPurpleTheme`** (lines 291–306, the entire function):

```go
func TestRenderProviderCard_geminiKeepsPurpleTheme(t *testing.T) {
	r := provider.QuotaResult{
		Provider: "gemini",
		// ... existing body ...
	}
	got := RenderProviderCard(r, 60)
	// ... assertions ...
	if /* ... */ {
		t.Fatalf("expected Gemini card to keep purple theme, got:\n%q", got)
	}
}
```

3. **Delete the three `gemini-*` rows** in the `TestWindowDuration` table around lines 381–383:

```go
		{"gemini-2.5-pro", 24 * time.Hour},
		{"gemini-2.5-flash", 24 * time.Hour},
		{"gemini-3.1-pro-preview", 24 * time.Hour},
```

Delete these three rows entirely. Keep the surrounding table structure intact.

- [ ] **Step 8: Verify**

```bash
gofmt -w internal/tui/menu_test.go internal/tui/model_test.go internal/tui/provider_card_test.go internal/cli/status_test.go internal/config/config_test.go internal/config/settings_test.go internal/provider/registry_test.go
go vet ./...
go test -race -count=1 ./...
```

Expected: all tests pass. Note: production code in `internal/tui/` still has `case "gemini":` branches; those are now dead-code paths but compile cleanly.

- [ ] **Step 9: Commit**

```bash
git add -A
git commit -m "$(cat <<'EOF'
test: rename gemini stubs to fake and remove gemini-specific assertions

Rename the "gemini" stub provider name to "fake" across config, TUI, CLI,
and registry tests so the multi-provider test scenarios survive the
gemini removal. Drop TestRenderProviderCard_geminiKeepsPurpleTheme and
the three gemini-prefixed rows from TestWindowDuration since they
exercise code paths slated for deletion. Update the negative-assertion
trap in menu_test.go so the "Current order" guard catches the renamed
sequence.
EOF
)"
```

---

## Task 3: Strip Gemini-specific TUI code

**Files:**
- Modify: `internal/tui/styles.go:13`, `:154-172`, `:445`
- Modify: `internal/tui/provider_card.go:127-128`, `:311-314`
- Modify: `internal/tui/quick_view.go:230-238`, `:242-269`, `:295`
- Modify: `internal/tui/model.go:751`

- [ ] **Step 1: Drop `geminiColorHex` constant from `internal/tui/styles.go`**

Around line 13, delete:

```go
	geminiColorHex    = "#8B5CF6"
```

- [ ] **Step 2: Drop the `case "gemini":` branch from `themeForProvider()`**

Around lines 154–172 in `internal/tui/styles.go`, delete the entire block:

```go
	case "gemini":
		if palette.IsDark {
			return providerTheme{
				BorderHex:  geminiColorHex,
				TitleHex:   geminiColorHex,
				BadgeBGHex: "#DDD6FE",
				BadgeFGHex: "#312E81",
				BarHex:     geminiColorHex,
				TrackHex:   "#334155",
				ChipBGHex:  "#312E81",
				ChipFGHex:  "#DDD6FE",
			}
		}
		return providerTheme{
			BorderHex:  "#7C3AED",
			TitleHex:   "#7C3AED",
			BadgeBGHex: "#E9D5FF",
			BadgeFGHex: "#581C87",
			BarHex:     geminiColorHex,
			TrackHex:   "#E2E8F0",
			ChipBGHex:  "#E9D5FF",
			ChipFGHex:  "#581C87",
		}
```

- [ ] **Step 3: Drop the gemini bar from `logoBarsView()`**

Around line 445 in `internal/tui/styles.go`, delete this single line so the logo renders 3 bars instead of 4:

```go
		providerTitleStyle(themeForProvider("gemini", palette)).Render("▌"),
```

The resulting block should be:

```go
func logoBarsView(palette appPalette) string {
	return lipgloss.JoinHorizontal(
		lipgloss.Center,
		providerTitleStyle(themeForProvider("claude", palette)).Render("▌"),
		providerTitleStyle(themeForProvider("openai", palette)).Render("▌"),
		providerTitleStyle(themeForProvider("copilot", palette)).Render("▌"),
	)
}
```

- [ ] **Step 4: Drop the gemini case from `providerDisplayName()`**

Around lines 127–128 in `internal/tui/provider_card.go`, delete:

```go
	case "gemini":
		return "Gemini"
```

- [ ] **Step 5: Drop the gemini-prefix 24h-reset branch from `windowDuration()`**

Around lines 310–314 in `internal/tui/provider_card.go`, delete:

```go
	// Gemini model windows (e.g. "gemini-2.5-pro") use a 24-hour reset.
	if strings.HasPrefix(name, "gemini-") {
		return 24 * time.Hour
	}
```

- [ ] **Step 6: Drop the gemini branch from `metricDisplayFor()`**

Around lines 230–238 in `internal/tui/quick_view.go`, delete:

```go
	// Gemini model IDs (e.g. "gemini-2.5-flash") group by major version.
	if prov == "gemini" {
		if group, ok := geminiModelGroup(normalized); ok {
			return metricDisplay{
				Group: group,
				Name:  prettyRawName(normalized),
			}
		}
	}
```

- [ ] **Step 7: Delete the `geminiModelGroup()` function**

Around lines 242–269 in `internal/tui/quick_view.go`, delete the entire function including its leading comment:

```go
// geminiModelGroup returns the display group ("Gemini 2", "Gemini 3", …) for a
// Gemini model ID like "gemini-2.5-flash". Returns ok=false for unrecognised IDs.
func geminiModelGroup(modelID string) (string, bool) {
	if !strings.HasPrefix(modelID, "gemini-") {
		return "", false
	}
	rest := strings.TrimPrefix(modelID, "gemini-")
	// ... rest of function body ...
	return fmt.Sprintf("Gemini %s", major), true
}
```

- [ ] **Step 8: Trim the gemini example from `prettyRawName` comment**

Around line 295 in `internal/tui/quick_view.go`, change:

```go
// "gemini-2.5-flash" → "Gemini 2.5 Flash", "premium_interactions" → "Premium Interactions".
```

to:

```go
// "premium_interactions" → "Premium Interactions".
```

- [ ] **Step 9: Drop gemini from default providerNames in `internal/tui/model.go`**

Around line 751, change:

```go
		providerNames = []string{"claude", "openai", "gemini", "copilot"}
```

to:

```go
		providerNames = []string{"claude", "openai", "copilot"}
```

- [ ] **Step 10: Verify**

```bash
gofmt -w internal/tui/styles.go internal/tui/provider_card.go internal/tui/quick_view.go internal/tui/model.go
go vet ./...
golangci-lint run ./...
go build ./cmd/agent-quota/
go test -race -count=1 ./...
```

Expected: all clean. Pay attention to `golangci-lint` — if it complains about unused identifiers, something was missed.

- [ ] **Step 11: Commit**

```bash
git add -A
git commit -m "$(cat <<'EOF'
refactor(tui): remove gemini-specific theme, model grouping, and reset rule

Drop the geminiColorHex constant and the purple-themed case branch in
themeForProvider, the providerDisplayName case, the geminiModelGroup
helper plus its call site in metricDisplayFor, the 24-hour reset branch
in windowDuration, the gemini sample bar in logoBarsView, the prettyRawName
comment example, and the default providerNames entry. After this commit
the TUI has no gemini references.
EOF
)"
```

---

## Task 4: Add gemini to `isRemovedProviderName` (settings self-heal, TDD)

This is the only task with genuine new behavior. We use red-green TDD: write a failing test for the new filter, see it fail, add `"gemini"` to the denylist, see it pass.

**Files:**
- Modify: `internal/config/config_test.go` (add new test + update existing `TestLoad_normalizesProviders` expected value)
- Modify: `internal/config/config.go:129-136`

- [ ] **Step 1: Write the failing test**

Add this test to `internal/config/config_test.go` (near the existing `TestLoad_dropsRemovedJulesProviderFromConfig` around line 64):

```go
func TestLoad_dropsRemovedGeminiProviderFromConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"providers":["claude","gemini","openai"]}`), 0o600); err != nil {
		t.Fatalf("os.WriteFile: %v", err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	want := []string{"claude", "openai"}
	if !reflect.DeepEqual(cfg.Providers, want) {
		t.Fatalf("Providers = %v, want %v", cfg.Providers, want)
	}
}
```

- [ ] **Step 2: Update `TestLoad_normalizesProviders` to reflect gemini being filtered**

Around lines 49 and 58 of `internal/config/config_test.go`, this existing test currently expects gemini to survive normalization. Update the expected value.

Change line 58 from:

```go
	want := []string{"claude", "openai", "gemini"}
```

to:

```go
	want := []string{"claude", "openai"}
```

(Leave line 49 as-is — the fixture intentionally includes `"gemini"` to verify it gets dropped.)

- [ ] **Step 3: Run tests — verify they fail**

```bash
go test -race -count=1 ./internal/config/...
```

Expected: `TestLoad_dropsRemovedGeminiProviderFromConfig` FAILS with `Providers = [claude gemini openai], want [claude openai]`. `TestLoad_normalizesProviders` FAILS similarly.

- [ ] **Step 4: Add gemini to `isRemovedProviderName`**

In `internal/config/config.go` around lines 129–136, change:

```go
func isRemovedProviderName(name string) bool {
	switch normalizeProviderName(name) {
	case "jules":
		return true
	default:
		return false
	}
}
```

to:

```go
func isRemovedProviderName(name string) bool {
	switch normalizeProviderName(name) {
	case "jules", "gemini":
		return true
	default:
		return false
	}
}
```

- [ ] **Step 5: Run tests — verify they pass**

```bash
go test -race -count=1 ./internal/config/...
```

Expected: both previously failing tests now pass.

- [ ] **Step 6: Run the full test suite**

```bash
gofmt -w internal/config/config.go internal/config/config_test.go
go vet ./...
golangci-lint run ./...
go test -race -count=1 ./...
```

Expected: all clean.

- [ ] **Step 7: Commit**

```bash
git add -A
git commit -m "$(cat <<'EOF'
refactor(config): add gemini to isRemovedProviderName for settings self-heal

Extend the existing removed-providers denylist (which already includes
jules) so users' settings.json and config files self-heal on next load:
any "gemini" entry in providers, provider_order, or quick_view is
silently filtered. SaveSettings re-normalizes before writing, so the
file persists clean on the next TUI state change.
EOF
)"
```

---

## Task 5: Documentation cleanup

**Files:**
- Modify: `README.md:81, 89, 101`
- Modify: `CLAUDE.md:17, 67-73`

- [ ] **Step 1: Drop gemini from `README.md` JSON examples and login bullet**

Open `README.md`. Find the JSON example around line 81:

```json
  "providers": ["claude", "gemini", "openai", "copilot"]
```

Change to:

```json
  "providers": ["claude", "openai", "copilot"]
```

Find the JSON example around line 89:

```json
  "provider_order": ["claude", "openai", "gemini", "copilot"],
```

Change to:

```json
  "provider_order": ["claude", "openai", "copilot"],
```

Find the login-required bullet list (around line 101) and delete the line:

```markdown
- Gemini: `gemini` CLI login
```

- [ ] **Step 2: Drop the Gemini architecture entry from `CLAUDE.md`**

Around line 17, delete this bullet:

```markdown
- `internal/gemini/` — Google Gemini usage provider
```

- [ ] **Step 3: Delete the "Gemini Code Assist API" section from `CLAUDE.md`**

Around lines 67–73, delete the entire section including the heading:

```markdown
## Gemini Code Assist API

- **Endpoints**: `POST https://cloudcode-pa.googleapis.com/v1internal:loadCodeAssist`, `POST .../v1internal:retrieveUserQuota`
- **Headers**: `Authorization: Bearer <token>`, `User-Agent: agent-quota`
- **Credentials**: `~/.gemini/oauth_creds.json` (written by the `gemini` CLI)
- **Token refresh**: exec `gemini -p ""` (refreshes on startup), then re-read credentials
- **Binary override**: `AGENT_QUOTA_GEMINI_PATH` env var (for testing / custom installs)
```

If a blank line precedes or follows that block and now creates double blank lines, collapse to a single blank line.

- [ ] **Step 4: Verify**

```bash
grep -i "gemini" README.md CLAUDE.md
```

Expected: no output (no gemini references remain).

- [ ] **Step 5: Commit**

```bash
git add README.md CLAUDE.md
git commit -m "$(cat <<'EOF'
docs: remove gemini from README and CLAUDE.md

Drop gemini from the JSON examples and login-required bullet in
README.md. Delete the internal/gemini architecture entry and the
"Gemini Code Assist API" section from CLAUDE.md.
EOF
)"
```

---

## Task 6: Scrub Gemini from Jules dispatch workflows

**Files:**
- Modify: `.github/workflows/jules-swe-dispatch.yml:175`
- Modify: `.github/workflows/jules-security-dispatch.yml:90`

These are AI-dispatch prompt files (not CI gates) — they instruct the Jules agent on context. Leaving gemini references would mislead future dispatched agents.

- [ ] **Step 1: Update the SWE dispatch example commit**

In `.github/workflows/jules-swe-dispatch.yml` around line 175, change:

```yaml
            - `feat(provider): add Gemini usage provider`
```

to:

```yaml
            - `feat(tui): add provider card pagination`
```

(Any current-feeling example works; this replacement is a generic TUI feature so it doesn't reference a specific historical change.)

- [ ] **Step 2: Drop "Gemini" from the security dispatch credential list**

In `.github/workflows/jules-security-dispatch.yml` around lines 89–91, change:

```yaml
            This repo handles OAuth tokens and local credential files for Claude, OpenAI,
            Gemini, GitHub Copilot, and Jules, so credential safety and trust-boundary
            handling are the top priorities.
```

to:

```yaml
            This repo handles OAuth tokens and local credential files for Claude, OpenAI,
            GitHub Copilot, and Jules, so credential safety and trust-boundary
            handling are the top priorities.
```

- [ ] **Step 3: Verify**

```bash
grep -i "gemini" .github/workflows/
```

Expected: no output.

- [ ] **Step 4: Commit**

```bash
git add .github/workflows/jules-swe-dispatch.yml .github/workflows/jules-security-dispatch.yml
git commit -m "$(cat <<'EOF'
ci(jules): scrub gemini from dispatch prompts

The Jules SWE-dispatch workflow's example commit list and the
security-dispatch workflow's credential-safety provider list both
referenced Gemini. Replace the example and drop Gemini from the list so
future dispatched agents don't act on stale provider context.
EOF
)"
```

---

## Task 7: Add changie `Removed` fragment

**Files:**
- Create: `.changes/unreleased/removed-<auto-generated>.yaml`

- [ ] **Step 1: Generate the fragment via `changie new`**

If `changie` is installed (verify with `which changie`):

```bash
changie new --kind Removed --body "Remove Gemini provider — gemini OAuth quota tracking is no longer supported. Existing settings.json or config.json entries referencing gemini are silently dropped on next launch."
```

If `changie` is not available locally, hand-create a file at `.changes/unreleased/removed-gemini-provider.yaml` with this content:

```yaml
kind: Removed
body: Remove Gemini provider — gemini OAuth quota tracking is no longer supported. Existing settings.json or config.json entries referencing gemini are silently dropped on next launch.
time: 2026-05-20T00:00:00.000000+00:00
```

- [ ] **Step 2: Verify the fragment is present**

```bash
ls .changes/unreleased/
cat .changes/unreleased/removed-*.yaml
```

Expected: one new `.yaml` file beyond `.gitkeep`.

- [ ] **Step 3: Run the full local verification suite**

```bash
gofmt -l .
go vet ./...
golangci-lint run ./...
go test -race -count=1 ./...
go build ./cmd/agent-quota/
git diff --name-only --diff-filter=ACMR feat/remove-gemini^..HEAD | xargs grep -li "gemini" 2>/dev/null
```

Expected:
- gofmt: no output
- vet, lint, test, build: clean
- Final grep: only `CHANGELOG.md`, `.changes/0.1.0.md` (history — intentionally preserved), `docs/superpowers/specs/2026-05-20-remove-gemini-design.md` (will be deleted in the final task), and the new `.changes/unreleased/removed-*.yaml` (mentions gemini deliberately as the changelog entry).

- [ ] **Step 4: Commit**

```bash
git add .changes/unreleased/
git commit -m "$(cat <<'EOF'
chore(changie): add removed fragment for gemini provider

Record the removal in the next release notes via a Removed-kind changie
fragment. The fragment text documents the self-healing behaviour for
users with gemini entries in their settings.json or config.json.
EOF
)"
```

---

## Task 8: Codex adversarial review of executed work — loop until resolved

This task lives outside the normal write-test-implement cycle. Dispatch `/codex:rescue` with an adversarial prompt scoped to the diff between `feat/remove-gemini` and `main`. Address every concern codex raises, push fixup commits, and re-dispatch until codex has no remaining concerns.

- [ ] **Step 1: Stage the review prompt**

Compose a prompt for codex that includes:
- The branch + base ref
- The 7 commits shipped so far on this branch
- A pointer to the spec at `docs/superpowers/specs/2026-05-20-remove-gemini-design.md` (still present on the branch at this point)
- Explicit adversarial framing — "find what's broken, missed, or regressed"
- Specific things to verify: no surviving gemini references in changed files (except history + the changie fragment); tests still cover 3+-provider scenarios; settings.json self-heal works as described; no orphaned dead code; lefthook pre-push passes

- [ ] **Step 2: Dispatch `/codex:rescue`**

Use the `Agent` tool with `subagent_type: "codex:codex-rescue"` and the prompt from Step 1.

- [ ] **Step 3: Address every finding**

For each issue codex raises:
- If it's a legitimate gap or regression: write a fixup commit
- If it's noise or out of scope: write a one-line justification in the conversation
- Run the full verification suite after each fixup: `gofmt -l . && go vet ./... && golangci-lint run ./... && go test -race -count=1 ./... && go build ./cmd/agent-quota/`

- [ ] **Step 4: Re-dispatch codex on the updated diff**

Repeat until codex has no remaining findings.

---

## Task 9: Push branch and raise PR

- [ ] **Step 1: Final full verification before push**

```bash
gofmt -l .
go vet ./...
golangci-lint run ./...
go test -race -count=1 ./...
go build ./cmd/agent-quota/
```

Expected: all clean.

- [ ] **Step 2: Push the branch**

```bash
git push -u origin feat/remove-gemini
```

The `lefthook` pre-push hook will run the test+build+changie checks again. Expected: pass.

- [ ] **Step 3: Open the PR**

```bash
gh pr create --title "refactor: remove Gemini provider" --body "$(cat <<'EOF'
## Summary

- Remove the Gemini OAuth-quota provider entirely (package, registration, TUI theme, model grouping, 24h-reset branch).
- Self-heal existing user `settings.json` / `config.json` by adding `"gemini"` to the `isRemovedProviderName` denylist (mirroring the prior `"jules"` removal).
- Rename `"gemini"` test stubs to `"fake"` to preserve multi-provider test coverage; delete tests that asserted Gemini-specific behaviour (purple theme, 24h-reset, model grouping).
- Scrub README, CLAUDE.md, and Jules dispatch prompts. Add a `Removed` changie fragment.

## Test plan

- [x] `gofmt -l .` — clean
- [x] `go vet ./...` — clean
- [x] `golangci-lint run ./...` — clean
- [x] `go test -race -count=1 ./...` — green
- [x] `go build ./cmd/agent-quota/` — clean binary
- [x] No `gemini` references survive in any non-history changed file
- [x] User with `gemini` in `settings.json` loads cleanly (entries silently dropped on next launch)

## Out of scope

- Generalized runtime self-heal (when a provider becomes unavailable but isn't on the static denylist). Tracked separately as [#3](https://github.com/rudolfjs/agent-quota/issues/3).

## Do NOT merge

Per session instructions; this PR is a deliverable for review, not for merge.
EOF
)"
```

- [ ] **Step 4: Confirm PR URL is returned and printed back to the user**

---

## Task 10: Watch CI to green

- [ ] **Step 1: Poll CI status**

```bash
gh pr checks --watch
```

Or, if blocking is undesirable, check periodically with `gh pr checks`.

- [ ] **Step 2: If anything fails, fix and push**

For each failed check:
- Read the failure log: `gh run view <run-id> --log-failed`
- Make the targeted fix
- Commit and push

- [ ] **Step 3: Terminate when CI is fully green**

Do NOT merge. The acceptance criterion is "PR raised, CI green in GitHub" — merging is explicitly out of scope per the session goal.

---

## Task 11: Delete planning spec + plan as the final cleanup push

**Files:**
- Delete: `docs/superpowers/specs/2026-05-20-remove-gemini-design.md`
- Delete: `docs/superpowers/plans/2026-05-20-remove-gemini.md`

This task is intentionally the LAST step, after CI is already green on the initial PR push. Reason: deleting this plan file mid-execution would strip Tasks 9 and 10 out from under any executor that drives off the file. Running deletion at the very end is safe — by this point all executor steps are complete.

After this cleanup push, the PR diff against `main` for `docs/superpowers/` is zero, and the second CI run (triggered by the cleanup push) should stay green.

- [ ] **Step 1: Confirm CI is green on the initial push before deleting**

```bash
gh pr checks
```

Expected: all checks pass. Do NOT proceed if any check is red — fix the underlying issue first.

- [ ] **Step 2: Delete both planning files**

```bash
git rm docs/superpowers/specs/2026-05-20-remove-gemini-design.md
git rm docs/superpowers/plans/2026-05-20-remove-gemini.md
```

- [ ] **Step 3: Inspect the resulting docs tree**

```bash
ls docs/superpowers/specs/ 2>/dev/null || true
ls docs/superpowers/plans/ 2>/dev/null || true
ls docs/superpowers/ 2>/dev/null
```

If `specs/` and `plans/` directories are now empty, leave them — git tracks the absence of files, not empty directories.

- [ ] **Step 4: Commit and push**

```bash
git commit -m "$(cat <<'EOF'
chore: remove planning spec and implementation plan

Both the gemini-removal planning spec and the implementation plan were
session artifacts, not product artifacts. Net diff against main for
docs/superpowers/ is now zero.
EOF
)"
git push
```

- [ ] **Step 5: Verify the PR's net diff for the planning files is zero**

```bash
git diff main..HEAD -- docs/superpowers/specs/2026-05-20-remove-gemini-design.md docs/superpowers/plans/2026-05-20-remove-gemini.md
```

Expected: no output.

- [ ] **Step 6: Wait for CI to re-green on the cleanup commit**

```bash
gh pr checks --watch
```

Expected: all checks pass. The PR is now in its final shape. Do NOT merge.

---

## Self-review checklist

This section is for the executing agent to verify before signalling completion:

- [ ] All 7 production commits (Tasks 1–7) compile and pass `go test -race ./...` independently when checked out at that commit (no broken intermediate state)
- [ ] `golangci-lint run ./...` clean — specifically no `unused` complaints about helpers that lost their last caller
- [ ] No surviving `gemini` reference in any file outside `CHANGELOG.md`, `.changes/0.1.0.md`, and the new `.changes/unreleased/removed-*.yaml`
- [ ] The negative-assertion fix at `menu_test.go:775` references the post-rename sequence (`fake`), not the pre-rename one (`Gemini`)
- [ ] The Codex adversarial-review loop on the executed work (Task 8) terminated with codex having no remaining findings
- [ ] The planning spec AND plan are both deleted in Task 11's final cleanup push
- [ ] PR URL printed back to the user
- [ ] CI is fully green (both on the initial push and after Task 11's cleanup push)
- [ ] PR is NOT merged
