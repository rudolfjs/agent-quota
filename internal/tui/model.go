package tui

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	apierrors "github.com/schnetlerr/agent-quota/internal/errors"
	"github.com/schnetlerr/agent-quota/internal/provider"
)

type fetchResultMsg struct {
	providerName string
	result       provider.QuotaResult
}

type fetchErrorMsg struct {
	providerName string
	err          error
}

type refreshTickMsg struct{}

type tickFunc func(time.Duration, func(time.Time) tea.Msg) tea.Cmd

// Option configures a TUI model instance.
type Option func(*Model)

// WithRefreshInterval sets the dashboard auto-refresh interval.
func WithRefreshInterval(d time.Duration) Option {
	return func(m *Model) {
		m.refreshInterval = d
	}
}

// Model is the root Bubbletea v2 model for the agent-quota TUI.
type Model struct {
	providers       []provider.Provider
	results         map[string]provider.QuotaResult
	errors          map[string]error
	spinner         spinner.Model
	viewport        viewport.Model
	width           int
	height          int
	pending         int
	refreshInterval time.Duration
	tick            tickFunc
}

// New creates a new TUI model with the given providers.
func New(providers []provider.Provider, opts ...Option) Model {
	s := spinner.New(spinner.WithSpinner(spinner.Dot))
	vp := viewport.New(viewport.WithWidth(78), viewport.WithHeight(18))
	vp.SoftWrap = true
	m := Model{
		providers:       providers,
		results:         make(map[string]provider.QuotaResult),
		errors:          make(map[string]error),
		spinner:         s,
		viewport:        vp,
		width:           80,
		height:          24,
		pending:         len(providers),
		refreshInterval: 5 * time.Minute,
		tick:            tea.Tick,
	}
	for _, opt := range opts {
		opt(&m)
	}
	m.syncViewport()
	return m
}

// Init starts the spinner, fires fetch commands for all providers, and schedules auto-refresh.
func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.spinner.Tick, m.fetchAllCmd(), m.refreshTimerCmd()}
	return tea.Batch(cmds...)
}

// Update handles incoming messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		}
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd

	case tea.MouseMsg:
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.syncViewport()
		return m, nil

	case fetchResultMsg:
		m.results[msg.providerName] = msg.result
		delete(m.errors, msg.providerName)
		if m.pending > 0 {
			m.pending--
		}
		m.syncViewport()
		return m, nil

	case fetchErrorMsg:
		m.errors[msg.providerName] = msg.err
		delete(m.results, msg.providerName)
		if m.pending > 0 {
			m.pending--
		}
		m.syncViewport()
		return m, nil

	case refreshTickMsg:
		if m.pending > 0 {
			return m, m.refreshTimerCmd()
		}
		m.pending += len(m.providers)
		return m, tea.Batch(m.fetchAllCmd(), m.refreshTimerCmd())

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}

	return m, nil
}

// View renders the TUI. Returns tea.View with AltScreen enabled.
func (m Model) View() tea.View {
	header := m.headerView()
	footer := subtleStyle.Render(m.footerText())
	body := m.viewport.View()

	if scrollbar := m.scrollbarView(); scrollbar != "" {
		body = lipgloss.JoinHorizontal(lipgloss.Top, body, scrollbar)
	}

	content := lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
	v := tea.NewView(content)
	v.AltScreen = true
	return v
}

func (m Model) pendingCount() int {
	if m.pending < 0 {
		return 0
	}
	return m.pending
}

func (m *Model) syncViewport() {
	m.updateViewportSize()
	m.viewport.SetContent(m.bodyContent())
}

func (m *Model) updateViewportSize() {
	m.viewport.SetWidth(max(m.width-scrollbarWidth, minViewportWidth))
	m.viewport.SetHeight(max(m.height-lipgloss.Height(m.headerView())-1, minViewportHeight))
}

func (m Model) headerView() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("agent-quota"))
	b.WriteString("\n")

	loading := m.pendingCount()
	if loading > 0 {
		b.WriteString("\n")
		b.WriteString(m.spinner.View())
		b.WriteString(subtleStyle.Render(" fetching quota data..."))
		b.WriteString("\n")
	}

	return b.String()
}

func (m Model) bodyContent() string {
	var b strings.Builder
	cardWidth := max(m.viewport.Width(), minViewportWidth)

	for _, p := range m.providers {
		name := p.Name()
		if r, ok := m.results[name]; ok {
			b.WriteString(RenderProviderCard(r, cardWidth))
			b.WriteString("\n\n")
			continue
		}
		if err, ok := m.errors[name]; ok {
			errResult := provider.QuotaResult{
				Provider: name,
				Status:   "error",
			}
			b.WriteString(RenderProviderCard(errResult, cardWidth))
			b.WriteString("\n")
			var domErr *apierrors.DomainError
			if errors.As(err, &domErr) {
				b.WriteString(errorStyle.Render("  " + domErr.Error()))
			} else {
				b.WriteString(errorStyle.Render("  an unexpected error occurred"))
			}
			b.WriteString("\n\n")
		}
	}

	return strings.TrimRight(b.String(), "\n")
}

func (m Model) footerText() string {
	footer := "j/k scroll • pgup/pgdn page • q quit"
	if m.refreshInterval > 0 {
		footer = fmt.Sprintf("Auto-refresh every %s • j/k scroll • pgup/pgdn page • q quit", formatRefreshInterval(m.refreshInterval))
	}
	return footer
}

func (m Model) scrollbarView() string {
	height := m.viewport.Height()
	if height <= 0 || m.viewport.TotalLineCount() <= height {
		return ""
	}

	thumbHeight := min(max(1, int(math.Round(float64(height*height)/float64(m.viewport.TotalLineCount())))), height)

	thumbTop := 0
	maxOffset := max(1, m.viewport.TotalLineCount()-height)
	if height > thumbHeight {
		thumbTop = int(math.Round(float64(m.viewport.YOffset()) / float64(maxOffset) * float64(height-thumbHeight)))
	}

	lines := make([]string, height)
	for i := range lines {
		if i >= thumbTop && i < thumbTop+thumbHeight {
			lines[i] = scrollThumbStyle.Render("█")
			continue
		}
		lines[i] = scrollTrackStyle.Render("│")
	}
	return strings.Join(lines, "\n")
}

func (m Model) fetchAllCmd() tea.Cmd {
	if len(m.providers) == 0 {
		return nil
	}
	cmds := make([]tea.Cmd, 0, len(m.providers))
	for _, p := range m.providers {
		cmds = append(cmds, fetchCmd(p))
	}
	return tea.Batch(cmds...)
}

func (m Model) refreshTimerCmd() tea.Cmd {
	if m.refreshInterval <= 0 || m.tick == nil {
		return nil
	}
	return m.tick(m.refreshInterval, func(time.Time) tea.Msg {
		return refreshTickMsg{}
	})
}

func fetchCmd(p provider.Provider) tea.Cmd {
	return func() tea.Msg {
		if !p.Available() {
			return fetchResultMsg{providerName: p.Name(), result: provider.QuotaResult{
				Provider:  p.Name(),
				Status:    "unavailable",
				FetchedAt: time.Now(),
			}}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		result, err := p.FetchQuota(ctx)
		if err != nil {
			return fetchErrorMsg{providerName: p.Name(), err: err}
		}
		return fetchResultMsg{providerName: p.Name(), result: result}
	}
}

const (
	minViewportWidth  = 40
	minViewportHeight = 3
	scrollbarWidth    = 2
)

func formatRefreshInterval(d time.Duration) string {
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d/time.Minute))
	}

	hours := int(d / time.Hour)
	minutes := int((d % time.Hour) / time.Minute)
	if minutes == 0 {
		return fmt.Sprintf("%dh", hours)
	}
	return fmt.Sprintf("%dh %dm", hours, minutes)
}
