package tui

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

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
	if !strings.Contains(view, "Change order") {
		t.Fatalf("View() = %q, want settings menu item %q", view, "Change order")
	}
	if !strings.Contains(view, "Refresh rate") {
		t.Fatalf("View() = %q, want settings menu item %q", view, "Refresh rate")
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
	m.menuCursor = 2

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

func TestEscapeMenu_selectRefreshRateUpdatesIntervalAndSaves(t *testing.T) {
	var saved []config.Settings
	providers := []provider.Provider{
		&stubProvider{name: "claude"},
		&stubProvider{name: "gemini"},
		&stubProvider{name: "openai"},
	}
	m := New(
		providers,
		WithRefreshInterval(5*time.Minute),
		WithSettings(config.Settings{}, func(s config.Settings) error {
			saved = append(saved, s)
			return nil
		}),
	)
	m.tick = nil

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = updated.(Model)
	m.menuCursor = 1

	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(Model)
	if m.menuMode != menuModeRefresh {
		t.Fatalf("menuMode = %v, want %v", m.menuMode, menuModeRefresh)
	}

	m.menuCursor = 3 // 10 minutes
	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(Model)

	if m.refreshInterval != 10*time.Minute {
		t.Fatalf("refreshInterval = %v, want %v", m.refreshInterval, 10*time.Minute)
	}
	if m.settings.TUI.RefreshMinutes != 10 {
		t.Fatalf("settings.TUI.RefreshMinutes = %d, want 10", m.settings.TUI.RefreshMinutes)
	}
	if m.menuMode != menuModeMain {
		t.Fatalf("menuMode = %v, want %v", m.menuMode, menuModeMain)
	}

	m = applyCmd(t, m, cmd)
	if len(saved) != 1 {
		t.Fatalf("saved settings calls = %d, want 1", len(saved))
	}
	if saved[0].TUI.RefreshMinutes != 10 {
		t.Fatalf("saved RefreshMinutes = %d, want 10", saved[0].TUI.RefreshMinutes)
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
	if !strings.Contains(m.menuView(), "Claude → OpenAI → Gemini") {
		t.Fatalf("menuView() = %q, want live order preview", m.menuView())
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
	if !strings.Contains(m.menuView(), "Picked up: OpenAI") {
		t.Fatalf("menuView() = %q, want pickup hint", m.menuView())
	}
	if !strings.Contains(m.menuView(), "PICKED UP") {
		t.Fatalf("menuView() = %q, want picked-up badge", m.menuView())
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
	m.menuCursor = 2

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
