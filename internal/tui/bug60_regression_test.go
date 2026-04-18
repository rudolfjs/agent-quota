package tui

import (
	"math"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// TestRenderQuotaBar_bug60_snapWiredIn pins issue #60 at the full render path:
// with util=0.20 and guide=0.16 on a 12-cell bar, the marker must land on a
// cell that was part of the filled region — so the user sees the marker
// overlapping fill instead of sitting in the empty track past it.
func TestRenderQuotaBar_bug60_snapWiredIn(t *testing.T) {
	theme := themeForProvider("claude", newPalette(true))
	// width 22 → barWidth max(22-10, 12) = 12
	bar := renderQuotaBar(theme, 0.20, 22, 0.16)
	visible := ansi.Strip(bar)

	// Expected prefix is one filled cell then the marker. Before the fix the
	// prefix was "██│" (fill shorter than marker).
	if !strings.HasPrefix(visible, "█│") {
		t.Errorf("expected fill-then-marker overlap pattern, got: %q", visible)
	}
}

// TestGuidePosition_utilGreaterThanGuide_markerStrictlyInsideFill: core
// invariant — when util > guide the marker cell sits inside [0, fw-1].
func TestGuidePosition_utilGreaterThanGuide_markerStrictlyInsideFill(t *testing.T) {
	cases := []struct {
		util, guide float64
		bw          int
	}{
		{0.20, 0.16, 12},
		{0.30, 0.25, 20},
		{0.50, 0.40, 30},
	}
	for _, tc := range cases {
		fw := int(math.Round(tc.util * float64(tc.bw)))
		pos := guidePosition(tc.util, tc.guide, tc.bw)
		if pos >= fw {
			t.Errorf("guidePosition(util=%.2f, guide=%.2f, bw=%d) = %d, want < fw=%d",
				tc.util, tc.guide, tc.bw, pos, fw)
		}
	}
}

// TestGuidePosition_utilLessThanGuide_markerPastFill: complementary invariant —
// when util < guide the marker sits at or past the fill edge.
func TestGuidePosition_utilLessThanGuide_markerPastFill(t *testing.T) {
	cases := []struct {
		util, guide float64
		bw          int
	}{
		{0.10, 0.16, 12},
		{0.20, 0.30, 20},
		{0.40, 0.50, 30},
	}
	for _, tc := range cases {
		fw := int(math.Round(tc.util * float64(tc.bw)))
		pos := guidePosition(tc.util, tc.guide, tc.bw)
		if pos < fw {
			t.Errorf("guidePosition(util=%.2f, guide=%.2f, bw=%d) = %d, want >= fw=%d",
				tc.util, tc.guide, tc.bw, pos, fw)
		}
	}
}

// TestGuidePosition_saturatedFill_clampsToLastCell documents that when the
// fill fully saturates the bar (fw == barWidth) the ordering invariant
// physically cannot hold — the marker is clamped to the last cell.
func TestGuidePosition_saturatedFill_clampsToLastCell(t *testing.T) {
	cases := []struct {
		util, guide float64
		bw, wantPos int
	}{
		{0.96, 1.00, 12, 11},
		{1.00, 0.90, 12, 11},
	}
	for _, tc := range cases {
		got := guidePosition(tc.util, tc.guide, tc.bw)
		if got != tc.wantPos {
			t.Errorf("guidePosition(util=%.2f, guide=%.2f, bw=%d) = %d, want %d",
				tc.util, tc.guide, tc.bw, got, tc.wantPos)
		}
	}
}
