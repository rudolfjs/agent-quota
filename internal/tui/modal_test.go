package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func TestMenuView_appliesBackdropWashAndModalStyling(t *testing.T) {
	m := New(nil, WithDarkBackground(true))
	m.width = 100
	m.height = 30
	m.openMainMenu()

	base := "dashboard body"
	got := m.overlayModal(base, m.menuView())

	if !strings.Contains(got, "Settings") {
		t.Fatalf("overlayModal() = %q, want modal content", got)
	}
	if !strings.Contains(got, "\x1b[2;") {
		t.Fatalf("overlayModal() = %q, want dimmed backdrop styling", got)
	}
}

func TestOverlayModal_preservesBackdropContentOutsideCenteredModal(t *testing.T) {
	m := New(nil, WithDarkBackground(true))
	m.width = 20
	m.height = 5

	base := strings.Join([]string{
		"abcdefghijklmnopqrst",
		"abcdefghijklmnopqrst",
		"abcdefghijklmnopqrst",
		"abcdefghijklmnopqrst",
		"abcdefghijklmnopqrst",
	}, "\n")

	got := m.overlayModal(base, "XX")
	lines := strings.Split(got, "\n")
	if len(lines) < 3 {
		t.Fatalf("overlayModal() returned %d lines, want at least 3", len(lines))
	}

	centerLine := lines[2]
	if !strings.Contains(centerLine, "abcdefghi") {
		t.Fatalf("overlayModal() center line = %q, want left backdrop content preserved", centerLine)
	}
	if !strings.Contains(centerLine, "lmnopqrst") {
		t.Fatalf("overlayModal() center line = %q, want right backdrop content preserved", centerLine)
	}
	if !strings.Contains(centerLine, "XX") {
		t.Fatalf("overlayModal() center line = %q, want modal content overlaid", centerLine)
	}
}

func TestUpdate_escapeStartsMenuAnimation(t *testing.T) {
	m := New(nil)
	m.tick = func(d time.Duration, fn func(time.Time) tea.Msg) tea.Cmd {
		if d != menuAnimationStep {
			t.Fatalf("tick duration = %v, want %v", d, menuAnimationStep)
		}
		return func() tea.Msg { return fn(time.Unix(0, 0)) }
	}

	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = updated.(Model)

	if m.menuMode != menuModeMain {
		t.Fatalf("menuMode = %v, want %v", m.menuMode, menuModeMain)
	}
	if m.menuAnimationFrame != 0 {
		t.Fatalf("menuAnimationFrame = %d, want 0", m.menuAnimationFrame)
	}
	if cmd == nil {
		t.Fatal("expected menu animation cmd")
	}

	updated, next := m.Update(cmd())
	m = updated.(Model)
	if m.menuAnimationFrame != 1 {
		t.Fatalf("menuAnimationFrame = %d, want 1", m.menuAnimationFrame)
	}
	if next == nil {
		t.Fatal("expected follow-up menu animation cmd")
	}
}
