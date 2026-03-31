package tui

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/schnetlerr/agent-quota/internal/config"
	apierrors "github.com/schnetlerr/agent-quota/internal/errors"
	"github.com/schnetlerr/agent-quota/internal/provider"
)

func TestUpdate_escapeKeyOpensSettingsMenu(t *testing.T) {
	m := New(nil)

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = updated.(Model)

	if m.menuMode != menuModeMain {
		t.Fatalf("menuMode = %v, want %v", m.menuMode, menuModeMain)
	}

	view := m.View().Content
	if !strings.Contains(view, "Providers") {
		t.Fatalf("View() = %q, want settings menu item %q", view, "Providers")
	}
	if !strings.Contains(view, "Change order") {
		t.Fatalf("View() = %q, want settings menu item %q", view, "Change order")
	}
	if !strings.Contains(view, "Quick View") {
		t.Fatalf("View() = %q, want settings menu item %q", view, "Quick View")
	}
	if !strings.Contains(view, "Refresh rate") {
		t.Fatalf("View() = %q, want settings menu item %q", view, "Refresh rate")
	}
}

func TestUpdate_escapeMenuUsesDescriptionsForProvidersAndOrder(t *testing.T) {
	providers := []provider.Provider{
		&stubProvider{name: "claude"},
		&stubProvider{name: "openai"},
	}
	m := New(providers, WithSettings(config.Settings{}, func(config.Settings) error { return nil }))

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = updated.(Model)

	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "Choose shown providers") {
		t.Fatalf("View() = %q, want providers description", view)
	}
	if !strings.Contains(view, "Reorder provider cards") {
		t.Fatalf("View() = %q, want order description", view)
	}
	if !strings.Contains(view, "Choose compact metrics") {
		t.Fatalf("View() = %q, want quick view description", view)
	}
	if !strings.Contains(view, "Press Enter to open a section") {
		t.Fatalf("View() = %q, want main menu instruction", view)
	}
	if strings.Contains(view, "Claude, OpenAI") {
		t.Fatalf("View() = %q, want providers description instead of current selection", view)
	}
	if strings.Contains(view, "Claude → OpenAI") {
		t.Fatalf("View() = %q, want order description instead of current order", view)
	}
}

func TestEscapeMenu_selectProvidersOpensProvidersMenu(t *testing.T) {
	providers := []provider.Provider{
		&stubProvider{name: "claude"},
		&stubProvider{name: "gemini"},
		&stubProvider{name: "copilot"},
	}
	m := New(providers)

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = updated.(Model)
	m.menuCursor = 0

	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(Model)

	if m.menuMode != menuModeProviders {
		t.Fatalf("menuMode = %v, want %v", m.menuMode, menuModeProviders)
	}
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "Claude") || !strings.Contains(view, "Gemini") || !strings.Contains(view, "Copilot") {
		t.Fatalf("View() = %q, want provider entries in providers menu", view)
	}
	if !strings.Contains(view, "Press Enter or Space to toggle providers") {
		t.Fatalf("View() = %q, want provider selection hint", view)
	}
	if !strings.Contains(view, "● Claude") || !strings.Contains(view, "● Gemini") || !strings.Contains(view, "● Copilot") {
		t.Fatalf("View() = %q, want circular provider selectors", view)
	}
	if strings.Contains(view, "✓") || strings.Contains(view, "✦") || strings.Contains(view, "◆") || strings.Contains(view, "◉") || strings.Contains(view, "◎") {
		t.Fatalf("View() = %q, want checks and provider icons removed from provider selection", view)
	}
	if strings.Contains(view, "Current selection") {
		t.Fatalf("View() = %q, want current selection header removed", view)
	}
	if strings.Contains(view, "All available") {
		t.Fatalf("View() = %q, want all available summary removed", view)
	}
	if strings.Contains(view, "Visible in dashboard") || strings.Contains(view, "Hidden from dashboard") {
		t.Fatalf("View() = %q, want per-provider visibility descriptions removed", view)
	}
}

func TestEscapeMenu_selectedRowsUseMinimalHighlighting(t *testing.T) {
	m := New([]provider.Provider{
		&stubProvider{name: "claude"},
		&stubProvider{name: "gemini"},
	})
	m.openProvidersMenu()

	view := ansi.Strip(m.menuView())
	if strings.Contains(view, "→") || strings.Contains(view, "▌") {
		t.Fatalf("menuView() = %q, want minimal selection styling without arrow markers", view)
	}
}

func TestEscapeMenu_modalsUseSpaciousHeadersAndCompactBodies(t *testing.T) {
	providers := []provider.Provider{
		&stubProvider{name: "claude"},
		&stubProvider{name: "gemini"},
	}
	cases := []struct {
		name  string
		open  func(*Model)
		title string
		hint  string
	}{
		{
			name:  "main",
			open:  func(m *Model) { m.openMainMenu() },
			title: "Settings",
			hint:  "Press Enter to open a section",
		},
		{
			name:  "providers",
			open:  func(m *Model) { m.openProvidersMenu() },
			title: "Providers",
			hint:  "Press Enter or Space to toggle providers",
		},
		{
			name:  "refresh",
			open:  func(m *Model) { m.openRefreshMenu() },
			title: "Refresh rate",
			hint:  "Press Enter to select refresh rate",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := New(providers)
			tc.open(&m)

			view := ansi.Strip(m.menuView())
			lines := strings.Split(view, "\n")
			titleLine := -1
			hintLine := -1
			for i, line := range lines {
				if strings.Contains(line, tc.title) {
					titleLine = i
				}
				if strings.Contains(line, tc.hint) {
					hintLine = i
				}
			}
			bodyLine := -1
			if hintLine != -1 {
				for i := hintLine + 1; i < len(lines); i++ {
					if isMenuBodyLine(lines[i]) {
						bodyLine = i
						break
					}
				}
			}
			if titleLine == -1 || hintLine == -1 || bodyLine == -1 {
				t.Fatalf("menuView() = %q, want title, hint, and body", view)
			}
			if !isBlankModalContentLine(lines[titleLine+1]) {
				t.Fatalf("menuView() = %q, want blank line after title", view)
			}
			if !isBlankModalContentLine(lines[hintLine+1]) {
				t.Fatalf("menuView() = %q, want blank line after hint", view)
			}
			if bodyLine-hintLine != 2 {
				t.Fatalf("menuView() = %q, want compact body immediately after hint spacing", view)
			}
		})
	}
}

func TestEscapeMenu_toggleProvidersUpdatesSettingsAndSaves(t *testing.T) {
	var saved []config.Settings
	providers := []provider.Provider{
		&stubProvider{name: "claude"},
		&stubProvider{name: "gemini"},
		&stubProvider{name: "openai"},
	}
	m := New(
		providers,
		WithSettings(config.Settings{}, func(s config.Settings) error {
			saved = append(saved, s)
			return nil
		}),
	)
	m.tick = nil

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = updated.(Model)
	m.menuCursor = 0

	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(Model)
	if m.menuMode != menuModeProviders {
		t.Fatalf("menuMode = %v, want %v", m.menuMode, menuModeProviders)
	}

	m.menuCursor = 1 // Gemini
	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(Model)

	wantActive := []string{"claude", "openai"}
	if got := providerNamesFromModel(m.providers); !reflect.DeepEqual(got, wantActive) {
		t.Fatalf("active providers = %v, want %v", got, wantActive)
	}
	if !reflect.DeepEqual(m.settings.Providers, wantActive) {
		t.Fatalf("settings.Providers = %v, want %v", m.settings.Providers, wantActive)
	}

	m = applyCmd(t, m, cmd)
	if len(saved) != 1 {
		t.Fatalf("saved settings calls = %d, want 1", len(saved))
	}
	if !reflect.DeepEqual(saved[0].Providers, wantActive) {
		t.Fatalf("saved Providers = %v, want %v", saved[0].Providers, wantActive)
	}
}

func TestEscapeMenu_cannotDisableLastProvider(t *testing.T) {
	providers := []provider.Provider{
		&stubProvider{name: "claude"},
	}
	m := New(providers)
	m.tick = nil

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = updated.(Model)
	m.menuCursor = 0
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(Model)

	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(Model)
	if cmd != nil {
		t.Fatal("disable last provider should not save")
	}
	if got := providerNamesFromModel(m.providers); !reflect.DeepEqual(got, []string{"claude"}) {
		t.Fatalf("active providers = %v, want [claude]", got)
	}
	if !strings.Contains(strings.ToLower(m.menuMessage), "at least one provider") {
		t.Fatalf("menuMessage = %q, want warning about last provider", m.menuMessage)
	}
}

func TestEscapeMenu_toggleHideHeaderUpdatesSettingsAndSaves(t *testing.T) {
	var saved []config.Settings
	m := New(
		nil,
		WithSettings(config.Settings{}, func(s config.Settings) error {
			saved = append(saved, s)
			return nil
		}),
	)
	m.tick = nil
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = updated.(Model)

	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = updated.(Model)
	m.menuCursor = 4

	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(Model)

	if !m.settings.TUI.HideHeader {
		t.Fatal("TUI.HideHeader = false, want true")
	}
	if got := m.headerView(); got != "" {
		t.Fatalf("headerView() = %q, want empty when header is hidden", got)
	}

	m = applyCmd(t, m, cmd)
	if len(saved) != 1 {
		t.Fatalf("saved settings calls = %d, want 1", len(saved))
	}
	if !saved[0].TUI.HideHeader {
		t.Fatalf("saved HideHeader = %v, want true", saved[0].TUI.HideHeader)
	}
}

func TestEscapeMenu_quickViewSelectionUpdatesSettingsAndSaves(t *testing.T) {
	var saved []config.Settings
	cached := provider.QuotaResult{
		Provider: "claude",
		Status:   "ok",
		Windows: []provider.Window{{
			Name:        "five_hour",
			Utilization: 0.35,
			ResetsAt:    time.Now().Add(2 * time.Hour),
		}},
		FetchedAt: time.Now(),
	}
	m := New(
		[]provider.Provider{&stubProvider{name: "claude"}},
		WithCachedResults(map[string]provider.QuotaResult{"claude": cached}),
		WithSettings(config.Settings{}, func(s config.Settings) error {
			saved = append(saved, s)
			return nil
		}),
	)
	m.tick = nil

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = updated.(Model)
	m.menuCursor = 2

	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(Model)
	if m.menuMode != menuModeQuickView {
		t.Fatalf("menuMode = %v, want %v", m.menuMode, menuModeQuickView)
	}
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "Claude: Session") {
		t.Fatalf("View() = %q, want quick view metric entry", view)
	}
	if !strings.Contains(view, "Press Enter or Space to toggle metrics") {
		t.Fatalf("View() = %q, want quick view selection hint", view)
	}
	if strings.Contains(view, "Quick view metrics") {
		t.Fatalf("View() = %q, want duplicate quick view header removed", view)
	}

	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(Model)
	m = applyCmd(t, m, cmd)

	want := []string{"claude:five_hour"}
	if !reflect.DeepEqual(m.settings.QuickView, want) {
		t.Fatalf("settings.QuickView = %v, want %v", m.settings.QuickView, want)
	}
	if len(saved) != 1 {
		t.Fatalf("saved settings calls = %d, want 1", len(saved))
	}
	if !reflect.DeepEqual(saved[0].QuickView, want) {
		t.Fatalf("saved QuickView = %v, want %v", saved[0].QuickView, want)
	}
}

func TestQuickViewSelection_defaultsToProviderOrder(t *testing.T) {
	cached := map[string]provider.QuotaResult{
		"openai": {
			Provider: "openai",
			Status:   "ok",
			Windows: []provider.Window{{
				Name:        "five_hour",
				Utilization: 0.25,
				ResetsAt:    time.Now().Add(2 * time.Hour),
			}},
			FetchedAt: time.Now(),
		},
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
		[]provider.Provider{&stubProvider{name: "openai"}, &stubProvider{name: "claude"}},
		WithCachedResults(cached),
		WithSettings(config.Settings{}, func(config.Settings) error { return nil }),
	)
	m.openQuickViewMenu()

	m.menuCursor = 1 // Claude first, even though OpenAI is the default provider order.
	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(Model)
	m = applyCmd(t, m, cmd)

	m.menuCursor = 1 // OpenAI remains the first unselected metric.
	updated, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(Model)
	m = applyCmd(t, m, cmd)

	want := []string{"openai:five_hour", "claude:five_hour"}
	if !reflect.DeepEqual(m.settings.QuickView, want) {
		t.Fatalf("settings.QuickView = %v, want %v", m.settings.QuickView, want)
	}
}

func TestQuickViewMenu_resetOrderMatchesChangeOrder(t *testing.T) {
	var saved []config.Settings
	cached := map[string]provider.QuotaResult{
		"openai": {
			Provider: "openai",
			Status:   "ok",
			Windows: []provider.Window{{
				Name:        "five_hour",
				Utilization: 0.25,
				ResetsAt:    time.Now().Add(2 * time.Hour),
			}},
			FetchedAt: time.Now(),
		},
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
		[]provider.Provider{&stubProvider{name: "openai"}, &stubProvider{name: "claude"}},
		WithCachedResults(cached),
		WithSettings(config.Settings{QuickView: []string{"claude:five_hour", "openai:five_hour"}}, func(s config.Settings) error {
			saved = append(saved, s)
			return nil
		}),
	)
	m.openQuickViewMenu()

	view := ansi.Strip(m.menuView())
	if !strings.Contains(view, "Reset order to Change order") {
		t.Fatalf("menuView() = %q, want reset action", view)
	}

	m.menuCursor = 0
	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(Model)
	m = applyCmd(t, m, cmd)

	want := []string{"openai:five_hour", "claude:five_hour"}
	if !reflect.DeepEqual(m.settings.QuickView, want) {
		t.Fatalf("settings.QuickView = %v, want %v", m.settings.QuickView, want)
	}
	if len(saved) != 1 {
		t.Fatalf("saved settings calls = %d, want 1", len(saved))
	}
	if !reflect.DeepEqual(saved[0].QuickView, want) {
		t.Fatalf("saved QuickView = %v, want %v", saved[0].QuickView, want)
	}
}

func TestChangeOrder_reordersQuickViewWhenQuickViewStillUsesDefaultOrder(t *testing.T) {
	var saved []config.Settings
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
		"openai": {
			Provider: "openai",
			Status:   "ok",
			Windows: []provider.Window{{
				Name:        "five_hour",
				Utilization: 0.25,
				ResetsAt:    time.Now().Add(2 * time.Hour),
			}},
			FetchedAt: time.Now(),
		},
	}
	m := New(
		[]provider.Provider{&stubProvider{name: "claude"}, &stubProvider{name: "openai"}},
		WithCachedResults(cached),
		WithSettings(config.Settings{QuickView: []string{"claude:five_hour", "openai:five_hour"}}, func(s config.Settings) error {
			saved = append(saved, s)
			return nil
		}),
	)

	m.menuCursor = 0
	m.openOrderMenu()
	m.menuCursor = 0
	m = applyCmd(t, m, m.moveSelectedProvider(1))

	want := []string{"openai:five_hour", "claude:five_hour"}
	if !reflect.DeepEqual(m.settings.QuickView, want) {
		t.Fatalf("settings.QuickView = %v, want %v", m.settings.QuickView, want)
	}
	if len(saved) != 1 {
		t.Fatalf("saved settings calls = %d, want 1", len(saved))
	}
	if !reflect.DeepEqual(saved[0].QuickView, want) {
		t.Fatalf("saved QuickView = %v, want %v", saved[0].QuickView, want)
	}
}

func TestQuickViewMenu_showsDefaultOrderStatus(t *testing.T) {
	cached := map[string]provider.QuotaResult{
		"openai": {
			Provider: "openai",
			Status:   "ok",
			Windows: []provider.Window{{
				Name:        "five_hour",
				Utilization: 0.25,
				ResetsAt:    time.Now().Add(2 * time.Hour),
			}},
			FetchedAt: time.Now(),
		},
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
		[]provider.Provider{&stubProvider{name: "openai"}, &stubProvider{name: "claude"}},
		WithCachedResults(cached),
		WithSettings(config.Settings{QuickView: []string{"openai:five_hour", "claude:five_hour"}}, func(config.Settings) error { return nil }),
	)
	m.openQuickViewMenu()

	view := ansi.Strip(m.menuView())
	if !strings.Contains(view, "Order: following Change order") {
		t.Fatalf("menuView() = %q, want default-order status", view)
	}
}

func TestQuickViewMenu_showsCustomOrderStatus(t *testing.T) {
	cached := map[string]provider.QuotaResult{
		"openai": {
			Provider: "openai",
			Status:   "ok",
			Windows: []provider.Window{{
				Name:        "five_hour",
				Utilization: 0.25,
				ResetsAt:    time.Now().Add(2 * time.Hour),
			}},
			FetchedAt: time.Now(),
		},
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
		[]provider.Provider{&stubProvider{name: "openai"}, &stubProvider{name: "claude"}},
		WithCachedResults(cached),
		WithSettings(config.Settings{QuickView: []string{"claude:five_hour", "openai:five_hour"}}, func(config.Settings) error { return nil }),
	)
	m.openQuickViewMenu()

	view := ansi.Strip(m.menuView())
	if !strings.Contains(view, "Order: custom") {
		t.Fatalf("menuView() = %q, want custom-order status", view)
	}
}

func TestQuickViewMenu_filtersStaleClaudeMetricsAndRenamesOpenAICodexWindows(t *testing.T) {
	cached := map[string]provider.QuotaResult{
		"claude": {
			Provider: "claude",
			Status:   "ok",
			Windows: []provider.Window{
				{Name: "five_hour", Utilization: 0.25, ResetsAt: time.Now().Add(2 * time.Hour)},
				{Name: "seven_day_oauth_apps", Utilization: 0.25, ResetsAt: time.Now().Add(2 * time.Hour)},
				{Name: "seven_day_opus", Utilization: 0.25, ResetsAt: time.Now().Add(2 * time.Hour)},
			},
			FetchedAt: time.Now(),
		},
		"openai": {
			Provider: "openai",
			Status:   "ok",
			Windows: []provider.Window{
				{Name: "codex_bengalfox_five_hour", Utilization: 0.25, ResetsAt: time.Now().Add(2 * time.Hour)},
			},
			FetchedAt: time.Now(),
		},
	}
	m := New(
		[]provider.Provider{&stubProvider{name: "claude"}, &stubProvider{name: "openai"}},
		WithCachedResults(cached),
	)
	m.openQuickViewMenu()

	view := ansi.Strip(m.menuView())
	if !strings.Contains(view, "Claude: Session") {
		t.Fatalf("menuView() = %q, want supported Claude metric", view)
	}
	if !strings.Contains(view, "OpenAI: Session") {
		t.Fatalf("menuView() = %q, want renamed OpenAI Codex metric as pretty name", view)
	}
	if strings.Contains(view, "seven_day_oauth_apps") || strings.Contains(view, "seven_day_opus") {
		t.Fatalf("menuView() = %q, want stale Claude metrics removed", view)
	}
}

func TestQuickViewMenu_usesPlainListWithoutScrollbar(t *testing.T) {
	windows := make([]provider.Window, 0, 12)
	for i := 0; i < 12; i++ {
		windows = append(windows, provider.Window{
			Name:        fmt.Sprintf("metric_%02d", i),
			Utilization: 0.25,
			ResetsAt:    time.Now().Add(2 * time.Hour),
		})
	}
	cached := provider.QuotaResult{
		Provider:  "claude",
		Status:    "ok",
		Windows:   windows,
		FetchedAt: time.Now(),
	}
	m := New(
		[]provider.Provider{&stubProvider{name: "claude"}},
		WithCachedResults(map[string]provider.QuotaResult{"claude": cached}),
	)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 20})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = updated.(Model)
	m.menuCursor = 2
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(Model)

	if m.menuMode != menuModeQuickView {
		t.Fatalf("menuMode = %v, want %v", m.menuMode, menuModeQuickView)
	}
	view := ansi.Strip(m.menuView())
	if !strings.Contains(view, "Metric 00") {
		t.Fatalf("menuView() = %q, want quick view metric list", view)
	}
	if strings.Contains(view, "█") {
		t.Fatalf("menuView() = %q, want quick view scrollbar removed", view)
	}
	for i := 0; i < 10; i++ {
		updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
		m = updated.(Model)
	}
	if m.menuViewport.YOffset() == 0 {
		t.Fatalf("menu viewport yOffset = %d, want > 0 after moving down", m.menuViewport.YOffset())
	}
}

func TestEscapeMenu_selectRefreshRateUpdatesIntervalAndSaves(t *testing.T) {
	var saved []config.Settings
	providers := []provider.Provider{
		&stubProvider{name: "claude"},
		&stubProvider{name: "gemini"},
		&stubProvider{name: "openai"},
	}
	m := New(
		providers,
		WithRefreshInterval(15*time.Minute),
		WithSettings(config.Settings{}, func(s config.Settings) error {
			saved = append(saved, s)
			return nil
		}),
	)
	m.tick = nil

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = updated.(Model)
	m.menuCursor = 3

	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(Model)
	if m.menuMode != menuModeRefresh {
		t.Fatalf("menuMode = %v, want %v", m.menuMode, menuModeRefresh)
	}

	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "Press Enter to select refresh rate") {
		t.Fatalf("View() = %q, want refresh instruction", view)
	}
	if !strings.Contains(view, "○ 5m") || !strings.Contains(view, "○ 10m") || !strings.Contains(view, "● 15m") {
		t.Fatalf("View() = %q, want circular refresh selector with 5m option", view)
	}
	if strings.Contains(view, "Current") || strings.Contains(view, "Every 15 minutes") {
		t.Fatalf("View() = %q, want redundant refresh descriptions removed", view)
	}

	m.menuCursor = 0 // 5 minutes
	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(Model)

	if m.refreshInterval != 5*time.Minute {
		t.Fatalf("refreshInterval = %v, want %v", m.refreshInterval, 5*time.Minute)
	}
	if m.settings.TUI.RefreshMinutes != 5 {
		t.Fatalf("settings.TUI.RefreshMinutes = %d, want 5", m.settings.TUI.RefreshMinutes)
	}
	if m.menuMode != menuModeMain {
		t.Fatalf("menuMode = %v, want %v", m.menuMode, menuModeMain)
	}

	m = applyCmd(t, m, cmd)
	if len(saved) != 1 {
		t.Fatalf("saved settings calls = %d, want 1", len(saved))
	}
	if saved[0].TUI.RefreshMinutes != 5 {
		t.Fatalf("saved RefreshMinutes = %d, want 5", saved[0].TUI.RefreshMinutes)
	}
}

func TestMoveSelectedProvider_reordersProvidersAndUpdatesSettings(t *testing.T) {
	providers := []provider.Provider{
		&stubProvider{name: "claude"},
		&stubProvider{name: "gemini"},
		&stubProvider{name: "openai"},
	}
	m := New(providers, WithSettings(config.Settings{}, func(config.Settings) error { return nil }))
	m.openOrderMenu()
	m.menuCursor = 2

	cmd := m.moveSelectedProvider(-1)

	want := []string{"claude", "openai", "gemini"}
	if got := providerNamesFromModel(m.providers); !reflect.DeepEqual(got, want) {
		t.Fatalf("provider order = %v, want %v", got, want)
	}
	if !reflect.DeepEqual(m.settings.ProviderOrder, want) {
		t.Fatalf("settings.ProviderOrder = %v, want %v", m.settings.ProviderOrder, want)
	}
	if m.menuCursor != 1 {
		t.Fatalf("menu cursor = %d, want 1", m.menuCursor)
	}
	if cmd == nil {
		t.Fatal("moveSelectedProvider() cmd = nil, want save cmd")
	}
	view := ansi.Strip(m.menuView())
	if !strings.Contains(view, "Press Enter to move provider") {
		t.Fatalf("menuView() = %q, want order instruction", view)
	}
	if !strings.Contains(view, "1. Claude") || !strings.Contains(view, "2. OpenAI") || !strings.Contains(view, "3. Gemini") {
		t.Fatalf("menuView() = %q, want numbered order list without icons", view)
	}
	if strings.Contains(view, "✦") || strings.Contains(view, "◆") || strings.Contains(view, "◎") || strings.Contains(view, "◉") {
		t.Fatalf("menuView() = %q, want provider icons removed from order menu", view)
	}
	if strings.Contains(view, "Current order") || strings.Contains(view, "Claude → OpenAI → Gemini") {
		t.Fatalf("menuView() = %q, want order preview removed", view)
	}
}

func TestUpdate_orderMenuShiftUpAndDownReordersProviders(t *testing.T) {
	providers := []provider.Provider{
		&stubProvider{name: "claude"},
		&stubProvider{name: "gemini"},
		&stubProvider{name: "openai"},
	}
	m := New(providers, WithSettings(config.Settings{}, func(config.Settings) error { return nil }))
	m.openOrderMenu()
	m.menuCursor = 1

	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyUp, Mod: tea.ModShift})
	m = updated.(Model)
	m = applyCmd(t, m, cmd)

	wantUp := []string{"gemini", "claude", "openai"}
	if got := providerNamesFromModel(m.providers); !reflect.DeepEqual(got, wantUp) {
		t.Fatalf("after shift+up provider order = %v, want %v", got, wantUp)
	}
	if m.menuCursor != 0 {
		t.Fatalf("after shift+up menu cursor = %d, want 0", m.menuCursor)
	}

	updated, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyDown, Mod: tea.ModShift})
	m = updated.(Model)
	m = applyCmd(t, m, cmd)

	wantDown := []string{"claude", "gemini", "openai"}
	if got := providerNamesFromModel(m.providers); !reflect.DeepEqual(got, wantDown) {
		t.Fatalf("after shift+down provider order = %v, want %v", got, wantDown)
	}
	if m.menuCursor != 1 {
		t.Fatalf("after shift+down menu cursor = %d, want 1", m.menuCursor)
	}
}

func TestUpdate_orderMenuEnterPickupAndDropReordersProviders(t *testing.T) {
	providers := []provider.Provider{
		&stubProvider{name: "claude"},
		&stubProvider{name: "gemini"},
		&stubProvider{name: "openai"},
	}
	m := New(providers, WithSettings(config.Settings{}, func(config.Settings) error { return nil }))
	m.openOrderMenu()
	m.menuCursor = 2

	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(Model)
	if cmd != nil {
		t.Fatal("pickup should not save immediately")
	}
	if m.orderPickupIndex != 2 {
		t.Fatalf("orderPickupIndex = %d, want 2", m.orderPickupIndex)
	}
	view := ansi.Strip(m.menuView())
	if !strings.Contains(view, "Press Enter to drop provider") {
		t.Fatalf("menuView() = %q, want drop instruction", view)
	}
	if strings.Contains(view, "Selected:") || strings.Contains(view, "Position 2") || strings.Contains(view, "Shown first") {
		t.Fatalf("menuView() = %q, want order subtext removed", view)
	}
	if !strings.Contains(view, "1. Claude") || !strings.Contains(view, "2. Gemini") || !strings.Contains(view, "3. OpenAI  PICKED") {
		t.Fatalf("menuView() = %q, want numbered providers while carrying item", view)
	}
	if strings.Contains(view, "✦") || strings.Contains(view, "◆") || strings.Contains(view, "◎") || strings.Contains(view, "◉") {
		t.Fatalf("menuView() = %q, want provider icons removed from order menu", view)
	}

	m.menuCursor = 1
	updated, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(Model)
	m = applyCmd(t, m, cmd)

	want := []string{"claude", "openai", "gemini"}
	if got := providerNamesFromModel(m.providers); !reflect.DeepEqual(got, want) {
		t.Fatalf("after pickup/drop provider order = %v, want %v", got, want)
	}
	if m.orderPickupIndex != -1 {
		t.Fatalf("orderPickupIndex = %d, want -1 after drop", m.orderPickupIndex)
	}
	if m.menuCursor != 1 {
		t.Fatalf("after drop menu cursor = %d, want 1", m.menuCursor)
	}
}

func TestUpdate_saveSettingsErrorRendersSafeMessage(t *testing.T) {
	m := New(nil, WithSettings(config.Settings{}, func(config.Settings) error {
		return apierrors.NewConfigError("failed to persist agent-quota settings", errors.New("permission denied: /secret/path"))
	}))
	m.tick = nil

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = updated.(Model)
	m.menuCursor = 4

	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(Model)
	m = applyCmd(t, m, cmd)

	if !strings.Contains(m.View().Content, "failed to persist agent-quota settings") {
		t.Fatalf("View() = %q, want safe settings error message", m.View().Content)
	}
	if strings.Contains(m.View().Content, "/secret/path") {
		t.Fatalf("View() leaked raw save error detail: %q", m.View().Content)
	}
}

func applyCmd(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	for _, msg := range runCmd(cmd) {
		updated, next := m.Update(msg)
		m = updated.(Model)
		for _, nested := range runCmd(next) {
			updated, next = m.Update(nested)
			m = updated.(Model)
			if next != nil {
				t.Fatalf("nested cmd returned unexpectedly from %T", nested)
			}
		}
	}
	return m
}

func runCmd(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var msgs []tea.Msg
		for _, nested := range batch {
			msgs = append(msgs, runCmd(nested)...)
		}
		return msgs
	}
	return []tea.Msg{msg}
}

func providerNamesFromModel(providers []provider.Provider) []string {
	names := make([]string, len(providers))
	for i, p := range providers {
		names[i] = p.Name()
	}
	return names
}

func isBlankModalContentLine(line string) bool {
	trimmed := strings.TrimSpace(strings.Trim(line, "│╭╮╰╯"))
	return trimmed == ""
}

func isMenuBodyLine(line string) bool {
	trimmed := strings.TrimSpace(strings.Trim(line, "│╭╮╰╯→"))
	return strings.Contains(trimmed, "Choose shown providers") ||
		strings.Contains(trimmed, "Providers") ||
		strings.Contains(trimmed, "● Claude") ||
		strings.Contains(trimmed, "○ Claude") ||
		strings.Contains(trimmed, "● Gemini") ||
		strings.Contains(trimmed, "○ Gemini") ||
		strings.Contains(trimmed, "○ 5m") ||
		strings.Contains(trimmed, "○ 10m") ||
		strings.Contains(trimmed, "● 15m")
}
