// Package tui provides the Bubbletea v2 TUI for displaying provider quota data.
package tui

import (
	"charm.land/lipgloss/v2"
)

var (
	titleStyle       = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7C3AED"))
	cardStyle        = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(1)
	windowStyle      = lipgloss.NewStyle().Faint(true)
	errorStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	subtleStyle      = lipgloss.NewStyle().Faint(true)
	scrollTrackStyle = subtleStyle
	scrollThumbStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#7C3AED"))
)
