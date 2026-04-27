package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/rudolfjs/agent-quota/internal/config"
	apierrors "github.com/rudolfjs/agent-quota/internal/errors"
	"github.com/rudolfjs/agent-quota/internal/provider"
)

// stubProvider implements provider.Provider for testing.
type stubProvider struct {
	name       string
	result     provider.QuotaResult
	err        error
	fetchCalls int
	resetCalls int
	resetErr   error
}

func (s *stubProvider) Name() string { return s.name }
func (s *stubProvider) FetchQuota(_ context.Context) (provider.QuotaResult, error) {
	s.fetchCalls++
	return s.result, s.err
}
func (s *stubProvider) Available() bool { return true }
func (s *stubProvider) ResetBackoff() error {
	s.resetCalls++
	return s.resetErr
}

// contextCapturingStub extends stubProvider to capture the context passed to
// FetchQuota, allowing tests to inspect whether a forced-retry signal was
// propagated.
type contextCapturingStub struct {
	stubProvider
	captureCtx func(context.Context)
}

func (c *contextCapturingStub) FetchQuota(ctx context.Context) (provider.QuotaResult, error) {
	if c.captureCtx != nil {
		c.captureCtx(ctx)
	}
	c.fetchCalls++
	return c.result, c.err
}

func (c *contextCapturingStub) Name() string        { return c.name }
func (c *contextCapturingStub) Available() bool     { return true }
func (c *contextCapturingStub) ResetBackoff() error { return c.stubProvider.ResetBackoff() }

func TestNew_returnsModelWithProviders(t *testing.T) {
	p := &stubProvider{name: "test"}
	m := New([]provider.Provider{p})

	if len(m.providers) != 1 {
		t.Fatalf("expected 1 provider, got %d", len(m.providers))
	}
	if m.results == nil {
		t.Fatal("expected results map to be initialized")
	}
	if m.errors == nil {
		t.Fatal("expected errors map to be initialized")
	}
	if m.pending != 1 {
		t.Fatalf("pending = %d, want 1", m.pending)
	}
	if m.refreshInterval != 15*time.Minute {
		t.Fatalf("refreshInterval = %v, want %v", m.refreshInterval, 15*time.Minute)
	}
}

func TestInit_returnsNonNilCmd(t *testing.T) {
	p := &stubProvider{name: "test"}
	m := New([]provider.Provider{p})

	cmd := m.Init()

	if cmd == nil {
		t.Fatal("expected Init to return a non-nil Cmd")
	}
}

func TestUpdate_fetchResultMsg_storesResult(t *testing.T) {
	p := &stubProvider{name: "claude"}
	m := New([]provider.Provider{p})
	m.errors["claude"] = context.DeadlineExceeded

	msg := fetchResultMsg{
		providerName: "claude",
		result: provider.QuotaResult{
			Provider: "claude",
			Status:   "ok",
		},
	}

	updated, _ := m.Update(msg)
	model := updated.(Model)

	r, ok := model.results["claude"]
	if !ok {
		t.Fatal("expected result for 'claude' to be stored")
	}
	if r.Status != "ok" {
		t.Errorf("expected status 'ok', got %q", r.Status)
	}
	if _, ok := model.errors["claude"]; ok {
		t.Fatal("expected previous error for 'claude' to be cleared")
	}
	if model.pending != 0 {
		t.Fatalf("pending = %d, want 0", model.pending)
	}
}

func TestUpdate_fetchErrorMsg_storesError(t *testing.T) {
	p := &stubProvider{name: "claude"}
	m := New([]provider.Provider{p})
	m.results["claude"] = provider.QuotaResult{Provider: "claude", Status: "ok"}

	msg := fetchErrorMsg{
		providerName: "claude",
		err:          context.DeadlineExceeded,
	}

	updated, _ := m.Update(msg)
	model := updated.(Model)

	e, ok := model.errors["claude"]
	if !ok {
		t.Fatal("expected error for 'claude' to be stored")
	}
	if e != context.DeadlineExceeded {
		t.Errorf("expected DeadlineExceeded, got %v", e)
	}
	if _, ok := model.results["claude"]; ok {
		t.Fatal("expected previous result for 'claude' to be cleared")
	}
	if model.pending != 0 {
		t.Fatalf("pending = %d, want 0", model.pending)
	}
}

func TestNew_seedsCachedResults(t *testing.T) {
	reset := time.Now().Add(2 * time.Hour)
	cached := provider.QuotaResult{
		Provider: "claude",
		Status:   "ok",
		Plan:     "max",
		Windows: []provider.Window{{
			Name:        "five_hour",
			Utilization: 0.35,
			ResetsAt:    reset,
		}},
		FetchedAt: time.Now(),
	}

	m := New([]provider.Provider{&stubProvider{name: "claude"}}, WithCachedResults(map[string]provider.QuotaResult{"claude": cached}))

	got, ok := m.results["claude"]
	if !ok {
		t.Fatal("expected cached result for claude to be seeded into model")
	}
	if got.Plan != "max" {
		t.Fatalf("cached plan = %q, want max", got.Plan)
	}
	if !strings.Contains(m.bodyContent(), "35% used") {
		t.Fatalf("bodyContent() = %q, want cached quota bars to render", m.bodyContent())
	}
}

func TestProviderChipsView_defaultListExcludesJules(t *testing.T) {
	got := ansi.Strip(providerChipsView(newPalette(true), nil))

	if strings.Contains(got, "Jules") {
		t.Fatalf("providerChipsView() = %q, want Jules omitted from default chips", got)
	}
	for _, want := range []string{"Claude", "OpenAI", "Gemini", "Copilot"} {
		if !strings.Contains(got, want) {
			t.Fatalf("providerChipsView() = %q, want %q chip", got, want)
		}
	}
}

func TestUpdate_fetchErrorMsg_retryableKeepsCachedResultVisible(t *testing.T) {
	reset := time.Now().Add(2 * time.Hour)
	cached := provider.QuotaResult{
		Provider: "claude",
		Status:   "ok",
		Windows: []provider.Window{{
			Name:        "five_hour",
			Utilization: 0.35,
			ResetsAt:    reset,
		}},
		FetchedAt: time.Now(),
	}
	m := New([]provider.Provider{&stubProvider{name: "claude"}}, WithCachedResults(map[string]provider.QuotaResult{"claude": cached}))
	m.retryBackoff = func(string, int, time.Duration) time.Duration { return 2 * time.Minute }

	rateErr := apierrors.NewAPIError("rate limited", context.DeadlineExceeded)
	rateErr.StatusCode = 429

	updated, _ := m.Update(fetchErrorMsg{providerName: "claude", err: rateErr})
	model := updated.(Model)

	if _, ok := model.results["claude"]; !ok {
		t.Fatal("expected retryable error to keep cached result visible")
	}
	rs, ok := model.retryStates["claude"]
	if !ok {
		t.Fatal("expected retry state to be recorded")
	}
	if rs.secondsLeft != 120 {
		t.Fatalf("secondsLeft = %d, want 120", rs.secondsLeft)
	}
	if rs.attempt != 1 {
		t.Fatalf("attempt = %d, want 1", rs.attempt)
	}
	if _, ok := model.errors["claude"]; ok {
		t.Fatal("expected retryable error not to replace cached result with an error card")
	}
	body := model.bodyContent()
	if !strings.Contains(body, "35% used") {
		t.Fatalf("bodyContent() = %q, want cached quota bars to remain visible", body)
	}
	if !strings.Contains(body, "cooldown 2m") {
		t.Fatalf("bodyContent() = %q, want cooldown countdown", body)
	}
	if !strings.Contains(body, "ctrl+r retry now") {
		t.Fatalf("bodyContent() = %q, want manual retry hint", body)
	}
}

func TestUpdate_fetchErrorMsg_retryableHonorsRetryAfter(t *testing.T) {
	m := New([]provider.Provider{&stubProvider{name: "claude"}})
	m.retryBackoff = func(string, int, time.Duration) time.Duration { return time.Minute }

	rateErr := apierrors.NewAPIError("rate limited", context.DeadlineExceeded)
	rateErr.StatusCode = 429
	rateErr.RetryAfter = 90 * time.Second

	updated, _ := m.Update(fetchErrorMsg{providerName: "claude", err: rateErr})
	model := updated.(Model)

	rs := model.retryStates["claude"]
	if rs.secondsLeft != 90 {
		t.Fatalf("secondsLeft = %d, want 90", rs.secondsLeft)
	}
	if !strings.Contains(model.bodyContent(), "cooldown 1m 30s") {
		t.Fatalf("bodyContent() = %q, want retry-after cooldown countdown", model.bodyContent())
	}
}

func TestUpdate_qKeyPress_returnsQuitCmd(t *testing.T) {
	m := New(nil)

	msg := tea.KeyPressMsg{Code: 'q'}

	_, cmd := m.Update(msg)

	if cmd == nil {
		t.Fatal("expected quit Cmd, got nil")
	}

	// Execute the cmd and check if it returns a QuitMsg.
	result := cmd()
	if _, ok := result.(tea.QuitMsg); !ok {
		t.Errorf("expected QuitMsg, got %T", result)
	}
}

func TestUpdate_ctrlC_returnsQuitCmd(t *testing.T) {
	m := New(nil)

	msg := tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}

	_, cmd := m.Update(msg)

	if cmd == nil {
		t.Fatal("expected quit Cmd, got nil")
	}

	result := cmd()
	if _, ok := result.(tea.QuitMsg); !ok {
		t.Errorf("expected QuitMsg, got %T", result)
	}
}

func TestUpdate_windowSizeMsg_storesDimensions(t *testing.T) {
	m := New(nil)

	msg := tea.WindowSizeMsg{Width: 120, Height: 40}

	updated, _ := m.Update(msg)
	model := updated.(Model)

	if model.width != 120 {
		t.Errorf("expected width 120, got %d", model.width)
	}
	if model.height != 40 {
		t.Errorf("expected height 40, got %d", model.height)
	}
	if model.viewport.Width() <= 0 {
		t.Fatalf("viewport width = %d, want > 0", model.viewport.Width())
	}
	if model.viewport.Height() <= 0 {
		t.Fatalf("viewport height = %d, want > 0", model.viewport.Height())
	}
}

func TestView_returnsTeaview(t *testing.T) {
	m := New(nil)

	v := m.View()

	// Verify it's a tea.View (compilation proves the type).
	_ = v.Content
	_ = v.AltScreen
}

func TestView_altScreenEnabled(t *testing.T) {
	m := New(nil)

	v := m.View()

	if !v.AltScreen {
		t.Error("expected AltScreen to be true")
	}
}

func TestInit_schedulesRefreshTimer(t *testing.T) {
	p := &stubProvider{name: "claude"}
	m := New([]provider.Provider{p}, WithRefreshInterval(time.Minute))

	var got time.Duration
	m.tick = func(d time.Duration, fn func(time.Time) tea.Msg) tea.Cmd {
		got = d
		return func() tea.Msg { return fn(time.Unix(0, 0)) }
	}

	cmd := m.Init()
	if cmd == nil {
		t.Fatal("expected Init to return a non-nil Cmd")
	}
	if got != time.Minute {
		t.Fatalf("tick duration = %v, want %v", got, time.Minute)
	}
}

func TestUpdate_refreshTickStartsFetchWhenIdle(t *testing.T) {
	p := &stubProvider{name: "claude"}
	m := New([]provider.Provider{p}, WithRefreshInterval(time.Minute))
	m.pending = 0

	var got time.Duration
	m.tick = func(d time.Duration, fn func(time.Time) tea.Msg) tea.Cmd {
		got = d
		return func() tea.Msg { return fn(time.Unix(0, 0)) }
	}

	updated, cmd := m.Update(refreshTickMsg{})
	model := updated.(Model)

	if model.pending != 1 {
		t.Fatalf("pending = %d, want 1", model.pending)
	}
	if cmd == nil {
		t.Fatal("expected refresh tick to schedule commands")
	}
	if got != time.Minute {
		t.Fatalf("tick duration = %v, want %v", got, time.Minute)
	}
}

func TestUpdate_refreshTickSkipsProvidersInRetryBackoff(t *testing.T) {
	claudeProvider := &stubProvider{name: "claude"}
	openAIProvider := &stubProvider{name: "openai"}
	m := New([]provider.Provider{claudeProvider, openAIProvider}, WithRefreshInterval(4*time.Minute))
	m.pending = 0
	m.tick = nil
	m.retryStates["claude"] = retryState{statusCode: 429, secondsLeft: 600, attempt: 1, generation: 1}

	updated, cmd := m.Update(refreshTickMsg{})
	model := updated.(Model)

	if model.pending != 1 {
		t.Fatalf("pending = %d, want 1", model.pending)
	}
	if cmd == nil {
		t.Fatal("expected refresh tick to fetch non-backed-off providers")
	}

	_ = cmd()
	if claudeProvider.fetchCalls != 0 {
		t.Fatalf("claude fetchCalls = %d, want 0", claudeProvider.fetchCalls)
	}
	if openAIProvider.fetchCalls != 1 {
		t.Fatalf("openai fetchCalls = %d, want 1", openAIProvider.fetchCalls)
	}
}

func TestUpdate_refreshTickSkipsFetchWhilePending(t *testing.T) {
	p := &stubProvider{name: "claude"}
	m := New([]provider.Provider{p}, WithRefreshInterval(time.Minute))
	m.pending = 1

	var tickCalls int
	m.tick = func(d time.Duration, fn func(time.Time) tea.Msg) tea.Cmd {
		tickCalls++
		return func() tea.Msg { return fn(time.Unix(0, 0)) }
	}

	updated, cmd := m.Update(refreshTickMsg{})
	model := updated.(Model)

	if model.pending != 1 {
		t.Fatalf("pending = %d, want 1", model.pending)
	}
	if cmd == nil {
		t.Fatal("expected refresh tick to reschedule timer")
	}
	if tickCalls != 1 {
		t.Fatalf("tickCalls = %d, want 1", tickCalls)
	}
}

func TestHeaderView_rendersStyledLogoWhenWide(t *testing.T) {
	m := New(nil)
	m.width = 120

	got := m.headerView()
	if !strings.Contains(got, "AQ") {
		t.Fatalf("headerView() = %q, want styled AQ logo", got)
	}
	if !strings.Contains(got, "agent-quota") {
		t.Fatalf("headerView() = %q, want agent-quota wordmark", got)
	}
	if !strings.Contains(got, "Claude") || !strings.Contains(got, "OpenAI") || !strings.Contains(got, "Gemini") {
		t.Fatalf("headerView() = %q, want provider chips in header", got)
	}
	if strings.Contains(got, "__ _  __ _  ___ _ __ | |_") {
		t.Fatalf("headerView() = %q, should not render the old ASCII art banner", got)
	}
}

func TestView_showsSpinnerNextToAutoRefreshStatus(t *testing.T) {
	m := New(nil, WithRefreshInterval(7*time.Minute), WithDarkBackground(true))

	v := m.View()
	if !strings.Contains(v.Content, "refresh in") {
		t.Fatalf("View() = %q, want countdown refresh status", v.Content)
	}
	if !strings.Contains(v.Content, "(ctrl+r)") {
		t.Fatalf("View() = %q, want manual refresh hint next to timer", v.Content)
	}
	if !strings.Contains(v.Content, m.spinner.View()) {
		t.Fatalf("View() = %q, want spinner %q next to refresh status", v.Content, m.spinner.View())
	}
}

func TestUpdate_ctrlRTriggersManualRefreshWhenIdle(t *testing.T) {
	p := &stubProvider{name: "claude", result: provider.QuotaResult{Provider: "claude", Status: "ok", FetchedAt: time.Now()}}
	m := New([]provider.Provider{p}, WithRefreshInterval(7*time.Minute))
	m.pending = 0

	var got time.Duration
	m.tick = func(d time.Duration, fn func(time.Time) tea.Msg) tea.Cmd {
		got = d
		return func() tea.Msg { return fn(time.Unix(0, 0)) }
	}

	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	m = updated.(Model)

	if m.pending != 1 {
		t.Fatalf("pending = %d, want 1", m.pending)
	}
	if cmd == nil {
		t.Fatal("expected ctrl+r to schedule refresh commands")
	}
	if got != 7*time.Minute {
		t.Fatalf("tick duration = %v, want %v", got, 7*time.Minute)
	}
	msgs := runCmd(cmd)
	if p.fetchCalls != 1 {
		t.Fatalf("fetchCalls = %d, want 1", p.fetchCalls)
	}
	if len(msgs) == 0 {
		t.Fatal("expected ctrl+r refresh to emit messages")
	}
}

func TestUpdate_ctrlROverridesRetryBackoff(t *testing.T) {
	p := &stubProvider{name: "claude", result: provider.QuotaResult{Provider: "claude", Status: "ok", FetchedAt: time.Now()}}
	m := New([]provider.Provider{p})
	m.pending = 0
	m.tick = func(d time.Duration, fn func(time.Time) tea.Msg) tea.Cmd {
		return func() tea.Msg { return fn(time.Unix(0, 0)) }
	}

	// Simulate a provider in retry backoff state.
	m.retryStates["claude"] = retryState{
		statusCode:  429,
		secondsLeft: 120,
		generation:  1,
		attempt:     2,
	}

	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	model := updated.(Model)

	if _, retrying := model.retryStates["claude"]; retrying {
		t.Fatal("expected ctrl+r to clear retry state")
	}
	if model.pending != 1 {
		t.Fatalf("pending = %d, want 1", model.pending)
	}
	if cmd == nil {
		t.Fatal("expected ctrl+r to schedule fetch command")
	}
	runCmd(cmd)
	if p.resetCalls != 1 {
		t.Fatalf("resetCalls = %d, want 1 (ctrl+r should clear provider backoff)", p.resetCalls)
	}
	if p.fetchCalls != 1 {
		t.Fatalf("fetchCalls = %d, want 1 (ctrl+r should trigger immediate fetch)", p.fetchCalls)
	}
}

func TestView_singleProviderSpinnerUsesProviderColor(t *testing.T) {
	m := New([]provider.Provider{&stubProvider{name: "claude"}}, WithDarkBackground(true))

	v := m.View()
	if !strings.Contains(v.Content, "\x1b[38;2;222;115;86m") {
		t.Fatalf("View() = %q, want Claude-colored spinner", v.Content)
	}
}

func TestHeaderView_usesLightPaletteWhenConfigured(t *testing.T) {
	m := New(nil, WithDarkBackground(false))
	m.width = 120

	got := m.headerView()
	if !strings.Contains(got, "15;23;42") {
		t.Fatalf("headerView() = %q, want light-theme title color", got)
	}
}

func TestUpdate_scrollKeyMovesViewport(t *testing.T) {
	providers := []provider.Provider{
		&stubProvider{name: "claude"},
		&stubProvider{name: "gemini"},
	}
	m := New(providers)

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 10})
	m = updated.(Model)

	for _, name := range []string{"claude", "gemini"} {
		updated, _ = m.Update(fetchResultMsg{providerName: name, result: overflowingResult(name)})
		m = updated.(Model)
	}

	before := m.viewport.YOffset()
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'j'})
	m = updated.(Model)

	if m.viewport.YOffset() <= before {
		t.Fatalf("viewport yOffset = %d, want > %d", m.viewport.YOffset(), before)
	}
}

func TestView_showsScrollbarWhenContentOverflows(t *testing.T) {
	providers := []provider.Provider{
		&stubProvider{name: "claude"},
		&stubProvider{name: "gemini"},
	}
	m := New(providers)

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 10})
	m = updated.(Model)

	for _, name := range []string{"claude", "gemini"} {
		updated, _ = m.Update(fetchResultMsg{providerName: name, result: overflowingResult(name)})
		m = updated.(Model)
	}

	v := m.View()
	if !strings.Contains(v.Content, "█") {
		t.Fatalf("View() = %q, want scrollbar thumb", v.Content)
	}
}

func TestUpdate_tabTogglesQuickViewAndRendersSelectedMetrics(t *testing.T) {
	cached := map[string]provider.QuotaResult{
		"claude": {
			Provider: "claude",
			Status:   "ok",
			Windows: []provider.Window{{
				Name:        "five_hour",
				Utilization: 0.35,
				ResetsAt:    time.Now().Add(2 * time.Hour),
			}},
			FetchedAt: time.Now(),
		},
	}
	m := New(
		[]provider.Provider{&stubProvider{name: "claude"}},
		WithCachedResults(cached),
		WithSettings(config.Settings{QuickView: []string{"claude:five_hour"}}, func(config.Settings) error { return nil }),
	)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	m = updated.(Model)

	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m = updated.(Model)

	body := ansi.Strip(m.bodyContent())
	if !strings.Contains(body, "Claude • Session") {
		t.Fatalf("bodyContent() = %q, want quick view metric heading", body)
	}
	if !strings.Contains(body, "35% used") {
		t.Fatalf("bodyContent() = %q, want quick view used label", body)
	}
	if !strings.Contains(m.footerText(), "full view") {
		t.Fatalf("footerText() = %q, want quick view toggle hint", m.footerText())
	}
}

// TestTUI_ctrlR_doesNotRePersistBackoff verifies that after a ctrl+r manual
// refresh, if the provider returns a 429 error, the backoff is NOT re-persisted
// to disk. This tests the integration between the TUI manual refresh flow and
// the backoff persistence layer.
//
// The bug: triggerManualRefresh() calls ResetBackoff() then fetchProvidersCmd().
// But FetchQuota() inside claude.go re-saves backoff on 429. The user's
// explicit "I want to retry now" is completely defeated because the backoff
// file reappears immediately.
//
// The fix should ensure that FetchQuota knows it was called in "forced" mode
// (e.g. via a context key) and skips saving backoff.
func TestTUI_ctrlR_doesNotRePersistBackoff(t *testing.T) {
	rateLimitErr := apierrors.NewAPIError("rate limited", context.DeadlineExceeded)
	rateLimitErr.StatusCode = 429
	rateLimitErr.RetryAfter = 2 * time.Minute

	// contextAwareStub captures the context passed to FetchQuota so we can
	// inspect whether the TUI propagated a "forced" signal.
	var capturedCtx context.Context
	p := &contextCapturingStub{
		stubProvider: stubProvider{
			name: "claude",
			err:  rateLimitErr,
		},
		captureCtx: func(ctx context.Context) { capturedCtx = ctx },
	}

	m := New([]provider.Provider{p})
	m.pending = 0
	m.tick = func(d time.Duration, fn func(time.Time) tea.Msg) tea.Cmd {
		return func() tea.Msg { return fn(time.Unix(0, 0)) }
	}

	// Simulate a pre-existing retry/backoff state (a previous 429).
	m.retryStates["claude"] = retryState{
		statusCode:  429,
		secondsLeft: 600,
		generation:  1,
		attempt:     1,
	}

	// User presses ctrl+r to force a manual refresh.
	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	model := updated.(Model)

	// ctrl+r should have cleared the retry state and called ResetBackoff.
	if _, retrying := model.retryStates["claude"]; retrying {
		t.Fatal("expected ctrl+r to clear retry state")
	}
	if p.resetCalls != 1 {
		t.Fatalf("resetCalls = %d, want 1 (ctrl+r should clear provider backoff)", p.resetCalls)
	}

	// Execute the fetch command — it will call FetchQuota which returns 429.
	msgs := runCmd(cmd)
	if p.fetchCalls != 1 {
		t.Fatalf("fetchCalls = %d, want 1", p.fetchCalls)
	}

	// The critical assertion: the context passed to FetchQuota after a ctrl+r
	// should carry a "forced retry" signal so the real Claude provider knows
	// NOT to re-persist the backoff file on 429.
	//
	// Currently fetchCmd always uses a plain context.Background() with no
	// force signal, so this test FAILS — exposing the bug.
	if capturedCtx == nil {
		t.Fatal("expected FetchQuota to be called with a context")
	}
	if capturedCtx.Value(provider.ForceRetryKey{}) == nil {
		t.Fatal("BUG: after ctrl+r, the context passed to FetchQuota should contain " +
			"provider.ForceRetryKey so the provider skips re-persisting backoff on 429; " +
			"currently triggerManualRefresh uses a plain context with no forced signal")
	}

	// Also verify the fetch error message was produced.
	foundErr := false
	for _, msg := range msgs {
		if fe, ok := msg.(fetchErrorMsg); ok {
			if fe.providerName == "claude" {
				foundErr = true
			}
		}
	}
	if !foundErr {
		t.Fatal("expected fetchErrorMsg for claude in batch results after 429")
	}
}

func overflowingResult(name string) provider.QuotaResult {
	reset := time.Now().Add(2 * time.Hour)
	return provider.QuotaResult{
		Provider: name,
		Status:   "ok",
		Plan:     "max",
		Windows: []provider.Window{
			{Name: "5-hour", Utilization: 0.25, ResetsAt: reset},
			{Name: "7-day", Utilization: 0.5, ResetsAt: reset},
			{Name: "opus", Utilization: 0.75, ResetsAt: reset},
		},
	}
}
