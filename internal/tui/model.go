package tui

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"log/slog"
	"math"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/rudolfjs/agent-quota/internal/config"
	apierrors "github.com/rudolfjs/agent-quota/internal/errors"
	"github.com/rudolfjs/agent-quota/internal/provider"
)

type fetchResultMsg struct {
	providerName string
	result       provider.QuotaResult
}

type fetchErrorMsg struct {
	providerName string
	err          error
}

type refreshTickMsg struct {
	generation int
}

type saveSettingsResultMsg struct {
	err error
}

type saveQuotaCacheResultMsg struct {
	err error
}

type menuAnimationTickMsg struct{}

type retryTickMsg struct {
	providerName string
	generation   int
}

type retryState struct {
	statusCode  int
	secondsLeft int
	generation  int
	attempt     int
}

type tickFunc func(time.Duration, func(time.Time) tea.Msg) tea.Cmd

type retryBackoffFunc func(providerName string, attempt int, retryAfter time.Duration) time.Duration

type saveSettingsFunc func(config.Settings) error

type saveQuotaCacheFunc func(map[string]provider.QuotaResult) error

type menuMode int

const (
	menuModeClosed menuMode = iota
	menuModeMain
	menuModeProviders
	menuModeOrder
	menuModeQuickView
	menuModeRefresh
)

type menuItemKind int

const (
	menuItemProviders menuItemKind = iota
	menuItemChangeOrder
	menuItemQuickView
	menuItemRefreshRate
	menuItemToggleHeader
	menuItemToggleGuide
	menuItemToggleProvider
	menuItemToggleQuickMetric
	menuItemResetQuickViewOrder
	menuItemExit
)

type settingsItem struct {
	kind           menuItemKind
	title          string
	description    string
	providerName   string
	metricID       string
	refreshMinutes int
}

func (i settingsItem) FilterValue() string { return i.title }
func (i settingsItem) Title() string       { return i.title }
func (i settingsItem) Description() string { return i.description }

// Option configures a TUI model instance.
type Option func(*Model)

// WithRefreshInterval sets the dashboard auto-refresh interval.
func WithRefreshInterval(d time.Duration) Option {
	return func(m *Model) {
		m.refreshInterval = d
	}
}

// WithDarkBackground explicitly selects the light or dark palette.
func WithDarkBackground(isDark bool) Option {
	return func(m *Model) {
		m.palette = newPalette(isDark)
	}
}

// WithSettings injects persisted TUI settings and a save function used by the
// escape menu.
func WithSettings(settings config.Settings, save func(config.Settings) error) Option {
	return func(m *Model) {
		m.settings = settings
		m.saveSettings = save
	}
}

// WithCachedResults seeds the TUI with the last successful quota snapshots.
func WithCachedResults(results map[string]provider.QuotaResult) Option {
	return func(m *Model) {
		m.cachedResults = cloneQuotaResults(results)
	}
}

// WithQuotaCacheSave injects a persistence function for successful quota snapshots.
func WithQuotaCacheSave(save func(map[string]provider.QuotaResult) error) Option {
	return func(m *Model) {
		m.saveQuotaCache = save
	}
}

// WithQuickViewEnabled starts the dashboard in compact quick-view mode.
func WithQuickViewEnabled(enabled bool) Option {
	return func(m *Model) {
		m.quickViewEnabled = enabled
	}
}

// Model is the root Bubbletea v2 model for the agent-quota TUI.
type Model struct {
	allProviders       []provider.Provider
	providers          []provider.Provider
	results            map[string]provider.QuotaResult
	cachedResults      map[string]provider.QuotaResult
	errors             map[string]error
	retryStates        map[string]retryState
	spinner            spinner.Model
	viewport           viewport.Model
	menuViewport       viewport.Model
	palette            appPalette
	width              int
	height             int
	pending            int
	refreshInterval    time.Duration
	nextRefreshAt      time.Time
	refreshGeneration  int
	tick               tickFunc
	retryBackoff       retryBackoffFunc
	settings           config.Settings
	saveSettings       saveSettingsFunc
	saveQuotaCache     saveQuotaCacheFunc
	quickViewEnabled   bool
	menuMode           menuMode
	menuCursor         int
	menuMessage        string
	orderPickupIndex   int
	menuAnimationFrame int
}

// New creates a new TUI model with the given providers.
func New(providers []provider.Provider, opts ...Option) Model {
	s := spinner.New(spinner.WithSpinner(spinner.Dot))
	vp := viewport.New(viewport.WithWidth(78), viewport.WithHeight(18))
	vp.SoftWrap = true
	menuVP := viewport.New(viewport.WithWidth(42), viewport.WithHeight(8))
	menuVP.SoftWrap = true
	m := Model{
		allProviders:       append([]provider.Provider(nil), providers...),
		providers:          append([]provider.Provider(nil), providers...),
		results:            make(map[string]provider.QuotaResult),
		cachedResults:      make(map[string]provider.QuotaResult),
		errors:             make(map[string]error),
		retryStates:        make(map[string]retryState),
		spinner:            s,
		viewport:           vp,
		menuViewport:       menuVP,
		palette:            newPalette(detectDarkBackground()),
		width:              80,
		height:             24,
		pending:            len(providers),
		refreshInterval:    time.Duration(config.DefaultTUIRefreshMinutes) * time.Minute,
		tick:               tea.Tick,
		retryBackoff:       defaultRetryBackoff,
		menuCursor:         0,
		orderPickupIndex:   -1,
		menuAnimationFrame: menuAnimationFrames,
	}
	for _, opt := range opts {
		opt(&m)
	}
	if m.refreshInterval > 0 {
		m.nextRefreshAt = time.Now().Add(m.refreshInterval)
	}
	m.providers = config.ApplyProviderSelection(m.allProviders, m.settings.Providers)
	m.pending = len(m.providers)
	m.seedCachedResults()
	m.syncSpinnerStyle()
	m.syncViewport()
	m.openMainMenu()
	m.closeMenu()
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
		case "ctrl+r":
			return m.triggerManualRefresh()
		}
		if m.menuOpen() {
			return m.updateMenu(msg)
		}
		switch msg.String() {
		case "esc":
			m.openMainMenu()
			return m, m.menuAnimationCmd()
		case "tab":
			m.quickViewEnabled = !m.quickViewEnabled
			m.syncViewport()
			return m, nil
		}
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd

	case tea.MouseMsg:
		if m.menuOpen() {
			return m, nil
		}
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.syncViewport()
		m.syncMenuViewport()
		if m.menuOpen() {
			m.rebuildMenu(m.menuCursor)
		}
		return m, nil

	case fetchResultMsg:
		if m.pending > 0 {
			m.pending--
		}
		if m.isProviderActive(msg.providerName) {
			m.results[msg.providerName] = msg.result
			delete(m.errors, msg.providerName)
			delete(m.retryStates, msg.providerName)
			m.syncViewport()
			if msg.result.Status != "ok" {
				return m, nil
			}
			m.cachedResults[msg.providerName] = msg.result
			return m, m.saveQuotaCacheCmd()
		}
		return m, nil

	case fetchErrorMsg:
		if m.pending > 0 {
			m.pending--
		}
		if !m.isProviderActive(msg.providerName) {
			return m, nil
		}
		var domErr *apierrors.DomainError
		if errors.As(msg.err, &domErr) && isRetryableStatusCode(domErr.StatusCode) {
			existing := m.retryStates[msg.providerName]
			attempt := existing.attempt + 1
			gen := existing.generation + 1
			delay := m.retryBackoff(msg.providerName, attempt, domErr.RetryAfter)
			if domErr.RetryAfter > delay {
				delay = domErr.RetryAfter
			}
			m.retryStates[msg.providerName] = retryState{
				statusCode:  domErr.StatusCode,
				secondsLeft: retryDelaySeconds(delay),
				generation:  gen,
				attempt:     attempt,
			}
			// Keep m.results intact so the last good data remains visible.
			m.syncViewport()
			return m, m.retryTickCmd(msg.providerName, gen)
		}
		m.errors[msg.providerName] = msg.err
		delete(m.results, msg.providerName)
		delete(m.retryStates, msg.providerName)
		m.syncViewport()
		return m, nil

	case refreshTickMsg:
		if msg.generation != m.refreshGeneration {
			return m, nil
		}
		m.nextRefreshAt = time.Now().Add(m.refreshInterval)
		if m.pending > 0 {
			return m, m.refreshTimerCmd()
		}
		refreshable := m.refreshableProviders()
		if len(refreshable) == 0 {
			return m, m.refreshTimerCmd()
		}
		m.pending += len(refreshable)
		return m, tea.Batch(fetchProvidersCmd(refreshable), m.refreshTimerCmd())

	case retryTickMsg:
		rs, ok := m.retryStates[msg.providerName]
		if !ok || rs.generation != msg.generation {
			return m, nil
		}
		rs.secondsLeft--
		if rs.secondsLeft <= 0 {
			delete(m.retryStates, msg.providerName)
			for _, p := range m.providers {
				if strings.EqualFold(p.Name(), msg.providerName) {
					m.pending++
					m.syncViewport()
					return m, fetchCmd(p)
				}
			}
			m.syncViewport()
			return m, nil
		}
		m.retryStates[msg.providerName] = rs
		m.syncViewport()
		return m, m.retryTickCmd(msg.providerName, msg.generation)

	case saveSettingsResultMsg:
		if msg.err != nil {
			m.menuMessage = safeSettingsMessage(msg.err)
			return m, nil
		}
		m.menuMessage = "Saved settings"
		return m, nil

	case saveQuotaCacheResultMsg:
		if msg.err != nil {
			slog.Debug("failed to persist quota cache", "error", msg.err)
		}
		return m, nil

	case menuAnimationTickMsg:
		if !m.menuOpen() || m.menuAnimationFrame >= menuAnimationFrames {
			return m, nil
		}
		m.menuAnimationFrame++
		if m.menuAnimationFrame >= menuAnimationFrames {
			return m, nil
		}
		return m, m.menuAnimationCmd()

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m Model) updateMenu(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		if m.menuMode == menuModeMain {
			m.closeMenu()
			return m, nil
		}
		m.openMainMenu()
		return m, m.menuAnimationCmd()
	case "up", "k":
		m.moveMenuCursor(-1)
		return m, nil
	case "down", "j":
		m.moveMenuCursor(1)
		return m, nil
	case "enter", "space":
		switch m.menuMode {
		case menuModeMain:
			return m.handleMainMenuSelection()
		case menuModeProviders:
			return m.handleProviderSelection()
		case menuModeQuickView:
			return m.handleQuickViewSelection()
		case menuModeRefresh:
			return m.handleRefreshSelection()
		case menuModeOrder:
			return m.handleOrderPickupDrop()
		}
	case "J", "shift+j", "ctrl+j", "shift+down":
		if m.menuMode == menuModeOrder {
			return m, m.moveSelectedProvider(1)
		}
		if m.menuMode == menuModeQuickView {
			return m, m.moveSelectedQuickViewMetric(1)
		}
	case "K", "shift+k", "ctrl+k", "shift+up":
		if m.menuMode == menuModeOrder {
			return m, m.moveSelectedProvider(-1)
		}
		if m.menuMode == menuModeQuickView {
			return m, m.moveSelectedQuickViewMetric(-1)
		}
	}

	return m, nil
}

func (m Model) handleMainMenuSelection() (tea.Model, tea.Cmd) {
	item, ok := m.selectedSettingsItem()
	if !ok {
		return m, nil
	}

	switch item.kind {
	case menuItemProviders:
		m.openProvidersMenu()
		return m, m.menuAnimationCmd()
	case menuItemChangeOrder:
		m.openOrderMenu()
		return m, m.menuAnimationCmd()
	case menuItemQuickView:
		m.openQuickViewMenu()
		return m, m.menuAnimationCmd()
	case menuItemRefreshRate:
		m.openRefreshMenu()
		return m, m.menuAnimationCmd()
	case menuItemToggleHeader:
		cursor := m.menuCursor
		m.settings.TUI.HideHeader = !m.settings.TUI.HideHeader
		m.syncViewport()
		m.openMainMenu()
		m.menuCursor = cursor
		m.menuMessage = ""
		return m, m.saveSettingsCmd()
	case menuItemToggleGuide:
		cursor := m.menuCursor
		m.settings.TUI.HideGuide = !m.settings.TUI.HideGuide
		m.syncViewport()
		m.openMainMenu()
		m.menuCursor = cursor
		m.menuMessage = ""
		return m, m.saveSettingsCmd()
	case menuItemExit:
		return m, tea.Quit
	default:
		return m, nil
	}
}

func (m Model) handleProviderSelection() (tea.Model, tea.Cmd) {
	item, ok := m.selectedSettingsItem()
	if !ok || item.providerName == "" {
		return m, nil
	}

	selected := m.selectedProviderNames()
	name := strings.ToLower(item.providerName)
	if m.isProviderSelected(name) {
		if len(selected) == 1 {
			m.menuMessage = "At least one provider must remain enabled"
			return m, nil
		}
		filtered := make([]string, 0, len(selected)-1)
		for _, selectedName := range selected {
			if selectedName != name {
				filtered = append(filtered, selectedName)
			}
		}
		selected = filtered
	} else {
		selected = append(selected, name)
	}

	m.menuMessage = ""
	return m, m.applyProviderSelection(selected)
}

func (m Model) handleQuickViewSelection() (tea.Model, tea.Cmd) {
	item, ok := m.selectedSettingsItem()
	if !ok {
		return m, nil
	}
	if item.kind == menuItemResetQuickViewOrder {
		m.settings.QuickView = m.defaultOrderedQuickViewMetricIDs(m.settings.QuickView)
		m.menuMessage = ""
		m.syncViewport()
		return m, m.saveSettingsCmd()
	}
	if item.metricID == "" {
		return m, nil
	}

	usesDefaultOrder := m.quickViewUsesDefaultOrder()
	if m.isQuickViewMetricSelected(item.metricID) {
		filtered := make([]string, 0, len(m.settings.QuickView))
		for _, id := range m.settings.QuickView {
			if id != item.metricID {
				filtered = append(filtered, id)
			}
		}
		m.settings.QuickView = filtered
	} else {
		m.settings.QuickView = append(m.settings.QuickView, item.metricID)
	}
	if usesDefaultOrder {
		m.settings.QuickView = m.defaultOrderedQuickViewMetricIDs(m.settings.QuickView)
	}
	m.menuMessage = ""
	m.syncViewport()
	return m, m.saveSettingsCmd()
}

func (m Model) handleRefreshSelection() (tea.Model, tea.Cmd) {
	item, ok := m.selectedSettingsItem()
	if !ok || item.refreshMinutes <= 0 {
		return m, nil
	}

	m.refreshInterval = time.Duration(item.refreshMinutes) * time.Minute
	m.settings.TUI.RefreshMinutes = item.refreshMinutes
	m.refreshGeneration++
	m.nextRefreshAt = time.Now().Add(m.refreshInterval)
	m.menuMessage = ""
	m.openMainMenu()
	return m, tea.Batch(m.saveSettingsCmd(), m.refreshTimerCmd(), m.menuAnimationCmd())
}

func (m Model) handleOrderPickupDrop() (tea.Model, tea.Cmd) {
	cursor := m.menuCursor
	if cursor < 0 || cursor >= len(m.providers) {
		return m, nil
	}

	if m.orderPickupIndex == -1 {
		m.orderPickupIndex = cursor
		m.menuMessage = ""
		m.rebuildMenu(cursor)
		return m, nil
	}

	usesDefaultQuickViewOrder := m.quickViewUsesDefaultOrder()
	m.providers = moveProvider(m.providers, m.orderPickupIndex, cursor)
	m.settings.ProviderOrder = m.providerNames()
	m.allProviders = config.ApplyProviderOrder(m.allProviders, m.settings.ProviderOrder)
	if usesDefaultQuickViewOrder {
		m.settings.QuickView = m.defaultOrderedQuickViewMetricIDs(m.settings.QuickView)
	}
	m.syncViewport()
	m.orderPickupIndex = -1
	m.menuMessage = ""
	m.rebuildMenu(cursor)
	return m, m.saveSettingsCmd()
}

// View renders the TUI. Returns tea.View with AltScreen enabled.
func (m Model) View() tea.View {
	sections := make([]string, 0, 3)
	if header := m.headerView(); header != "" {
		sections = append(sections, header)
	}

	body := m.viewport.View()
	if scrollbar := m.scrollbarView(); scrollbar != "" {
		body = lipgloss.JoinHorizontal(lipgloss.Top, body, scrollbar)
	}
	sections = append(sections, body)
	sections = append(sections, subtleStyle(m.palette).Render(m.footerText()))

	content := lipgloss.JoinVertical(lipgloss.Left, sections...)
	if m.menuOpen() {
		content = m.overlayModal(content, m.menuView())
	}

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

func (m *Model) syncSpinnerStyle() {
	m.spinner.Style = lipgloss.NewStyle().Foreground(lipgloss.Color(m.spinnerColorHex()))
}

func (m Model) spinnerColorHex() string {
	if len(m.providers) == 1 {
		return themeForProvider(m.providers[0].Name(), m.palette).BarHex
	}
	return m.palette.LogoBorderHex
}

func (m *Model) syncViewport() {
	m.updateViewportSize()
	m.viewport.SetContent(m.bodyContent())
	m.syncMenuViewport()
}

func (m *Model) syncMenuViewport() {
	m.updateMenuViewportSize()
	if m.menuMode != menuModeQuickView {
		m.menuViewport.SetContent("")
		return
	}
	items, _ := m.menuItems()
	m.menuViewport.SetContent(m.renderQuickViewMenuItems(items))
	m.ensureMenuCursorVisible()
}

func (m *Model) updateViewportSize() {
	headerHeight := 0
	if !m.settings.TUI.HideHeader {
		headerHeight = lipgloss.Height(m.headerView())
	}
	m.viewport.SetWidth(max(m.width-scrollbarWidth, minViewportWidth))
	m.viewport.SetHeight(max(m.height-headerHeight-1, minViewportHeight))
}

func (m *Model) updateMenuViewportSize() {
	width := max(m.menuInnerWidth(), 10)
	height := min(max(m.height/3, 6), 12)
	m.menuViewport.SetWidth(width)
	m.menuViewport.SetHeight(height)
}

func (m *Model) ensureMenuCursorVisible() {
	if m.menuMode != menuModeQuickView {
		return
	}
	items, _ := m.menuItems()
	if len(items) == 0 {
		return
	}
	selectedTop := 0
	for i := 0; i < m.menuCursor && i < len(items); i++ {
		selectedTop += m.menuItemLineSpan(items[i])
	}
	selectedBottom := selectedTop + m.menuItemLineSpan(items[min(m.menuCursor, len(items)-1)])
	if selectedTop < m.menuViewport.YOffset() {
		m.menuViewport.SetYOffset(selectedTop)
		return
	}
	if selectedBottom > m.menuViewport.YOffset()+m.menuViewport.Height() {
		m.menuViewport.SetYOffset(selectedBottom - m.menuViewport.Height())
	}
}

func (m Model) menuItemLineSpan(item settingsItem) int {
	lines := 1
	if item.description != "" {
		lines++
	}
	return lines
}

func (m Model) headerView() string {
	if m.settings.TUI.HideHeader {
		return ""
	}
	sections := []string{m.bannerView(), m.statusView()}
	if loading := m.loadingView(); loading != "" {
		sections = append(sections, loading)
	}
	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

func (m Model) bannerView() string {
	if m.width < 72 {
		return lipgloss.JoinVertical(
			lipgloss.Left,
			lipgloss.JoinHorizontal(
				lipgloss.Center,
				compactLogoStyle(m.palette).Render("AQ"),
				" ",
				titleStyle(m.palette).Render("agent-quota"),
			),
			subtitleStyle(m.palette).Render("AI provider quota dashboard"),
		)
	}

	return renderBrandBanner(m.width, m.palette, m.providerNames())
}

func renderBrandBanner(width int, palette appPalette, providerNames []string) string {
	badge := logoBadgeStyle(palette).Render(lipgloss.JoinVertical(
		lipgloss.Center,
		logoBarsView(palette),
		logoTextStyle(palette).Render("AQ"),
	))

	copy := []string{
		titleStyle(palette).Render("agent-quota"),
		subtitleStyle(palette).Render("AI provider quota dashboard"),
	}
	if width >= 100 {
		copy = append(copy, providerChipsView(palette, providerNames))
	}

	return lipgloss.JoinHorizontal(
		lipgloss.Top,
		badge,
		"  ",
		lipgloss.JoinVertical(lipgloss.Left, copy...),
	)
}

func providerChipsView(palette appPalette, providerNames []string) string {
	if len(providerNames) == 0 {
		providerNames = []string{"claude", "openai", "gemini", "copilot"}
	}
	chips := make([]string, 0, len(providerNames))
	for _, name := range providerNames {
		chips = append(chips, providerChipStyle(themeForProvider(name, palette)).Render(providerDisplayName(name)))
	}
	return strings.Join(chips, " ")
}

func (m Model) statusView() string {
	providerCount := fmt.Sprintf("%d provider", len(m.providers))
	if len(m.providers) != 1 {
		providerCount += "s"
	}

	if m.width < 72 {
		return lipgloss.JoinHorizontal(lipgloss.Center, m.spinner.View(), " ", subtleStyle(m.palette).Render(fmt.Sprintf("%s • %s", providerCount, m.refreshStatusLabel())))
	}

	return lipgloss.JoinHorizontal(lipgloss.Center, m.spinner.View(), " ", subtleStyle(m.palette).Render(fmt.Sprintf("live • %s • %s", providerCount, m.refreshStatusLabel())))
}

func (m Model) refreshStatusLabel() string {
	if m.refreshInterval > 0 {
		return m.refreshCountdownLabel() + "  (ctrl+r)"
	}
	return "manual refresh  (ctrl+r)"
}

// refreshCountdownLabel returns a countdown string like "refresh in 1m 45s" that
// counts down to the next scheduled auto-refresh.
func (m Model) refreshCountdownLabel() string {
	remaining := time.Until(m.nextRefreshAt).Round(time.Second)
	if remaining <= 0 {
		return "refreshing..."
	}
	mins := int(remaining.Minutes())
	secs := int(remaining.Seconds()) % 60
	if mins > 0 {
		return fmt.Sprintf("refresh in %dm %ds", mins, secs)
	}
	return fmt.Sprintf("refresh in %ds", secs)
}

func (m Model) loadingView() string {
	loading := m.pendingCount()
	if loading == 0 {
		return ""
	}
	return lipgloss.JoinHorizontal(lipgloss.Center, m.spinner.View(), " ", subtleStyle(m.palette).Render(fmt.Sprintf("fetching quota data (%d remaining)", loading)))
}

func (m Model) bodyContent() string {
	if m.quickViewEnabled {
		return m.quickViewContent()
	}

	var b strings.Builder
	cardWidth := max(m.viewport.Width(), minViewportWidth)

	for _, p := range m.providers {
		name := p.Name()
		rs, retrying := m.retryStates[name]

		showGuide := !m.settings.TUI.HideGuide
		if r, ok := m.results[name]; ok {
			b.WriteString(renderProviderCardWithPalette(r, cardWidth, m.palette, showGuide))
			if retrying {
				b.WriteString("\n")
				b.WriteString(renderRetryFootnote(rs, m.palette, true))
			}
			b.WriteString("\n\n")
			continue
		}

		if retrying {
			errResult := provider.QuotaResult{Provider: name, Status: "error"}
			b.WriteString(renderProviderCardWithPalette(errResult, cardWidth, m.palette, showGuide))
			b.WriteString("\n")
			b.WriteString(renderRetryFootnote(rs, m.palette, false))
			b.WriteString("\n\n")
			continue
		}

		if err, ok := m.errors[name]; ok {
			errResult := provider.QuotaResult{Provider: name, Status: "error"}
			b.WriteString(renderProviderCardWithPalette(errResult, cardWidth, m.palette, showGuide))
			b.WriteString("\n")
			b.WriteString(errorStyle(m.palette).Render("  " + compactProviderError(err)))
			b.WriteString("\n\n")
		}
	}

	return strings.TrimRight(b.String(), "\n")
}

func renderRetryFootnote(rs retryState, palette appPalette, showingLastKnownData bool) string {
	return errorStyle(palette).Render("  " + retryStatusMessage(rs, showingLastKnownData))
}

func retryStatusMessage(rs retryState, showingLastKnownData bool) string {
	if rs.statusCode == 429 {
		message := fmt.Sprintf("HTTP 429 · cooldown %s", formatRetryCountdown(rs.secondsLeft))
		if showingLastKnownData {
			message += " · stale"
		}
		return message + " · ctrl+r retry now"
	}

	message := fmt.Sprintf("HTTP %d · Retrying in %s", rs.statusCode, formatRetryCountdown(rs.secondsLeft))
	if showingLastKnownData {
		message += " · stale"
	}
	return message
}

func retryStatusCompactMessage(rs retryState) string {
	if rs.statusCode == 429 {
		return fmt.Sprintf("stale • cooldown %s • ctrl+r retry now", formatRetryCountdown(rs.secondsLeft))
	}
	return fmt.Sprintf("stale • retry in %s", formatRetryCountdown(rs.secondsLeft))
}

func compactProviderError(err error) string {
	var domErr *apierrors.DomainError
	if errors.As(err, &domErr) {
		return domErr.Error()
	}
	return "unexpected error"
}

func (m Model) footerText() string {
	switch m.menuMode {
	case menuModeMain:
		return "↑/↓ move • enter select • esc close • q quit"
	case menuModeProviders:
		return "↑/↓ choose • enter/space toggle • esc back • q quit"
	case menuModeQuickView:
		return "↑/↓ choose • enter/space toggle/reset • shift+↑/shift+↓ reorder • esc back • q quit"
	case menuModeRefresh:
		return "↑/↓ choose • enter apply • esc back • q quit"
	case menuModeOrder:
		return "j/k move cursor • enter pick up/drop • shift+↑/shift+↓ reorder • esc back • q quit"
	default:
		if m.quickViewEnabled {
			return "tab full view • j/k scroll • esc menu • q quit"
		}
		return "tab quick view • j/k scroll • pgup/pgdn page • esc menu • q quit"
	}
}

func (m Model) scrollbarView() string {
	return scrollbarViewForViewport(m.viewport, m.palette)
}

func scrollbarViewForViewport(v viewport.Model, palette appPalette) string {
	height := v.Height()
	if height <= 0 || v.TotalLineCount() <= height {
		return ""
	}

	thumbHeight := min(max(1, int(math.Round(float64(height*height)/float64(v.TotalLineCount())))), height)

	thumbTop := 0
	maxOffset := max(1, v.TotalLineCount()-height)
	if height > thumbHeight {
		thumbTop = int(math.Round(float64(v.YOffset()) / float64(maxOffset) * float64(height-thumbHeight)))
	}

	lines := make([]string, height)
	for i := range lines {
		if i >= thumbTop && i < thumbTop+thumbHeight {
			lines[i] = scrollThumbStyle(palette).Render("█")
			continue
		}
		lines[i] = scrollTrackStyle(palette).Render("│")
	}
	return strings.Join(lines, "\n")
}

func (m Model) fetchAllCmd() tea.Cmd {
	return fetchProvidersCmd(m.providers)
}

func fetchProvidersCmd(providers []provider.Provider) tea.Cmd {
	return fetchProvidersCmdForced(providers, false)
}

func fetchProvidersCmdForced(providers []provider.Provider, forced bool) tea.Cmd {
	if len(providers) == 0 {
		return nil
	}
	cmds := make([]tea.Cmd, 0, len(providers))
	for _, p := range providers {
		cmds = append(cmds, fetchCmdForced(p, forced))
	}
	return tea.Batch(cmds...)
}

func (m Model) refreshableProviders() []provider.Provider {
	providers := make([]provider.Provider, 0, len(m.providers))
	for _, p := range m.providers {
		if _, retrying := m.retryStates[strings.ToLower(p.Name())]; retrying {
			continue
		}
		providers = append(providers, p)
	}
	return providers
}

func (m Model) refreshTimerCmd() tea.Cmd {
	if m.refreshInterval <= 0 || m.tick == nil {
		return nil
	}
	generation := m.refreshGeneration
	return m.tick(m.refreshInterval, func(time.Time) tea.Msg {
		return refreshTickMsg{generation: generation}
	})
}

func (m Model) triggerManualRefresh() (tea.Model, tea.Cmd) {
	if m.pending > 0 {
		return m, nil
	}
	// Clear retry states so retrying providers are included in the refresh.
	// Stale retryTickMsgs are harmlessly ignored because the generation check
	// in Update fails when the key no longer exists.
	clear(m.retryStates)
	refreshable := m.refreshableProviders()
	if len(refreshable) == 0 {
		return m, nil
	}
	for _, p := range refreshable {
		if resetter, ok := p.(provider.BackoffResetter); ok {
			if err := resetter.ResetBackoff(); err != nil {
				slog.Debug("failed to clear provider backoff state", slog.String("provider", p.Name()), "error", err)
			}
		}
	}
	m.pending += len(refreshable)
	if m.refreshInterval > 0 {
		m.refreshGeneration++
		m.nextRefreshAt = time.Now().Add(m.refreshInterval)
		return m, tea.Batch(fetchProvidersCmdForced(refreshable, true), m.refreshTimerCmd())
	}
	return m, fetchProvidersCmdForced(refreshable, true)
}

func fetchCmd(p provider.Provider) tea.Cmd {
	return fetchCmdForced(p, false)
}

func fetchCmdForced(p provider.Provider, forced bool) tea.Cmd {
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
		if forced {
			ctx = context.WithValue(ctx, provider.ForceRetryKey{}, true)
		}
		result, err := p.FetchQuota(ctx)
		if err != nil {
			return fetchErrorMsg{providerName: p.Name(), err: err}
		}
		return fetchResultMsg{providerName: p.Name(), result: result}
	}
}

func (m *Model) openMainMenu() {
	m.menuMode = menuModeMain
	m.orderPickupIndex = -1
	m.menuAnimationFrame = 0
	m.rebuildMenu(0)
}

func (m *Model) openProvidersMenu() {
	m.menuMode = menuModeProviders
	m.orderPickupIndex = -1
	m.menuAnimationFrame = 0
	m.rebuildMenu(0)
}

func (m *Model) openQuickViewMenu() {
	m.menuMode = menuModeQuickView
	m.orderPickupIndex = -1
	m.menuAnimationFrame = 0
	cursor := 0
	if len(m.settings.QuickView) > 1 {
		cursor = 1
	}
	m.rebuildMenu(cursor)
}

func (m *Model) openRefreshMenu() {
	m.menuMode = menuModeRefresh
	m.orderPickupIndex = -1
	m.menuAnimationFrame = 0
	m.rebuildMenu(0)
}

func (m *Model) openOrderMenu() {
	m.menuMode = menuModeOrder
	m.orderPickupIndex = -1
	m.menuAnimationFrame = 0
	m.rebuildMenu(0)
}

func (m *Model) closeMenu() {
	m.menuMode = menuModeClosed
	m.menuMessage = ""
	m.orderPickupIndex = -1
	m.menuAnimationFrame = menuAnimationFrames
}

func (m Model) menuOpen() bool {
	return m.menuMode != menuModeClosed
}

func (m *Model) rebuildMenu(cursor int) {
	items, _ := m.menuItems()
	if len(items) == 0 {
		m.menuCursor = 0
		m.syncMenuViewport()
		return
	}
	m.menuCursor = min(max(cursor, 0), len(items)-1)
	m.syncMenuViewport()
}

func (m *Model) moveMenuCursor(delta int) {
	items, _ := m.menuItems()
	if len(items) == 0 {
		m.menuCursor = 0
		m.syncMenuViewport()
		return
	}
	m.menuCursor = min(max(m.menuCursor+delta, 0), len(items)-1)
	m.syncMenuViewport()
}

func (m Model) menuItems() ([]settingsItem, string) {
	switch m.menuMode {
	case menuModeProviders:
		return m.providersMenuItems(), "Providers"
	case menuModeOrder:
		return m.orderMenuItems(), "Change order"
	case menuModeQuickView:
		return m.quickViewMenuItems(), "Quick View"
	case menuModeRefresh:
		return m.refreshMenuItems(), "Refresh rate"
	default:
		return m.mainMenuItems(), "Settings"
	}
}

func (m Model) menuTitle() string {
	_, title := m.menuItems()
	return title
}

func (m Model) mainMenuItems() []settingsItem {
	headerTitle := "Hide header"
	headerDescription := "Currently visible"
	if m.settings.TUI.HideHeader {
		headerTitle = "Show header"
		headerDescription = "Currently hidden"
	}

	items := []settingsItem{
		{
			kind:        menuItemProviders,
			title:       "Providers",
			description: "Choose shown providers",
		},
		{
			kind:        menuItemChangeOrder,
			title:       "Change order",
			description: "Reorder provider cards",
		},
		{
			kind:        menuItemQuickView,
			title:       "Quick View",
			description: m.quickViewDescription(),
		},
		{
			kind:        menuItemRefreshRate,
			title:       "Refresh rate",
			description: formatRefreshInterval(m.refreshInterval),
		},
	}
	guideTitle := "Hide guide"
	guideDescription := "Budget guide visible"
	if m.settings.TUI.HideGuide {
		guideTitle = "Show guide"
		guideDescription = "Budget guide hidden"
	}

	items = append(items,
		settingsItem{
			kind:        menuItemToggleHeader,
			title:       headerTitle,
			description: headerDescription,
		},
		settingsItem{
			kind:        menuItemToggleGuide,
			title:       guideTitle,
			description: guideDescription,
		},
		settingsItem{
			kind:        menuItemExit,
			title:       "Quit",
			description: "Quit application",
		},
	)
	return items
}

func (m Model) providersMenuItems() []settingsItem {
	items := make([]settingsItem, 0, len(m.allProviders))
	for _, p := range m.allProviders {
		name := p.Name()
		checked := "○"
		if m.isProviderSelected(name) {
			checked = "●"
		}
		items = append(items, settingsItem{
			kind:         menuItemToggleProvider,
			title:        fmt.Sprintf("%s %s", checked, providerDisplayName(name)),
			providerName: name,
		})
	}
	return items
}

func (m Model) orderMenuItems() []settingsItem {
	names := m.providerNames()
	items := make([]settingsItem, 0, len(names))
	carrying := m.orderPickupIndex >= 0
	for i, name := range names {
		items = append(items, settingsItem{
			title: orderMenuTitle(i+1, name, providerTheme{}, m.palette, i == m.orderPickupIndex, carrying),
		})
	}
	return items
}

func (m Model) refreshMenuItems() []settingsItem {
	options := []int{5, 10, 15, 30, 60}
	items := make([]settingsItem, 0, len(options))
	current := int(m.refreshInterval / time.Minute)
	for _, minutes := range options {
		marker := "○"
		if minutes == current {
			marker = "●"
		}
		items = append(items, settingsItem{
			kind:           menuItemRefreshRate,
			title:          fmt.Sprintf("%s %s", marker, formatRefreshInterval(time.Duration(minutes)*time.Minute)),
			refreshMinutes: minutes,
		})
	}
	return items
}

func (m Model) quickViewMenuItems() []settingsItem {
	metrics := m.availableQuickViewMetrics()

	availableMetricsMap := make(map[string]quickViewMetric)
	for _, metric := range metrics {
		availableMetricsMap[metric.ID] = metric
	}

	items := make([]settingsItem, 0, len(metrics)+1)
	if len(m.settings.QuickView) > 1 {
		items = append(items, settingsItem{
			kind:  menuItemResetQuickViewOrder,
			title: "Reset order to Change order",
		})
	}

	addedMap := make(map[string]struct{})
	for _, id := range m.settings.QuickView {
		if metric, ok := availableMetricsMap[id]; ok {
			items = append(items, settingsItem{
				kind:     menuItemToggleQuickMetric,
				title:    fmt.Sprintf("● %s", metric.menuLabel(m.palette)),
				metricID: metric.ID,
			})
			addedMap[id] = struct{}{}
		}
	}

	for _, metric := range metrics {
		if _, ok := addedMap[metric.ID]; ok {
			continue
		}
		items = append(items, settingsItem{
			kind:     menuItemToggleQuickMetric,
			title:    fmt.Sprintf("○ %s", metric.menuLabel(m.palette)),
			metricID: metric.ID,
		})
	}

	return items
}

func (m Model) quickViewDescription() string {
	count := len(m.settings.QuickView)
	if count == 0 {
		return "Choose compact metrics"
	}
	if count == 1 {
		return "1 metric selected"
	}
	return fmt.Sprintf("%d metrics selected", count)
}

func (m Model) defaultOrderedQuickViewMetricIDs(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}

	selected := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		selected[id] = struct{}{}
	}

	ordered := make([]string, 0, len(ids))
	used := make(map[string]struct{}, len(ids))
	for _, metric := range m.availableQuickViewMetrics() {
		if _, ok := selected[metric.ID]; !ok {
			continue
		}
		ordered = append(ordered, metric.ID)
		used[metric.ID] = struct{}{}
	}

	for _, id := range ids {
		if _, ok := used[id]; ok {
			continue
		}
		ordered = append(ordered, id)
	}
	return ordered
}

func (m Model) quickViewUsesDefaultOrder() bool {
	return slices.Equal(m.settings.QuickView, m.defaultOrderedQuickViewMetricIDs(m.settings.QuickView))
}

func (m Model) quickViewOrderStatusText() string {
	if m.quickViewUsesDefaultOrder() {
		return "Order: following Change order"
	}
	return "Order: custom"
}

func (m Model) isQuickViewMetricSelected(id string) bool {
	for _, selected := range m.settings.QuickView {
		if selected == id {
			return true
		}
	}
	return false
}

func (m Model) providerNames() []string {
	names := make([]string, len(m.providers))
	for i, p := range m.providers {
		names[i] = p.Name()
	}
	return names
}

func (m Model) selectedProviderNames() []string {
	return m.providerNames()
}

func (m Model) isProviderSelected(name string) bool {
	name = strings.ToLower(name)
	for _, p := range m.providers {
		if strings.ToLower(p.Name()) == name {
			return true
		}
	}
	return false
}

func (m Model) isProviderActive(name string) bool {
	name = strings.ToLower(name)
	for _, p := range m.providers {
		if strings.ToLower(p.Name()) == name {
			return true
		}
	}
	return false
}

func (m Model) selectedSettingsItem() (settingsItem, bool) {
	items, _ := m.menuItems()
	if m.menuCursor < 0 || m.menuCursor >= len(items) {
		return settingsItem{}, false
	}
	return items[m.menuCursor], true
}

func (m *Model) applyProviderSelection(selected []string) tea.Cmd {
	previous := make(map[string]struct{}, len(m.providers))
	for _, p := range m.providers {
		previous[strings.ToLower(p.Name())] = struct{}{}
	}
	usesDefaultQuickViewOrder := m.quickViewUsesDefaultOrder()

	m.providers = config.ApplyProviderSelection(m.allProviders, selected)
	if len(m.providers) == len(m.allProviders) {
		m.settings.Providers = nil
	} else {
		m.settings.Providers = m.providerNames()
	}

	// Purge stale results/errors/retries/bars for providers that are no longer active.
	current := make(map[string]struct{}, len(m.providers))
	for _, p := range m.providers {
		current[strings.ToLower(p.Name())] = struct{}{}
	}

	// Remove disabled providers from QuickView
	filteredQuickView := make([]string, 0, len(m.settings.QuickView))
	for _, qv := range m.settings.QuickView {
		metric, ok := parseQuickViewMetricID(qv)
		if ok {
			if _, isActive := current[strings.ToLower(metric.ProviderName)]; isActive {
				filteredQuickView = append(filteredQuickView, qv)
			}
		} else {
			filteredQuickView = append(filteredQuickView, qv) // keep invalid ones or handle them elsewhere
		}
	}
	m.settings.QuickView = filteredQuickView
	if usesDefaultQuickViewOrder {
		m.settings.QuickView = m.defaultOrderedQuickViewMetricIDs(m.settings.QuickView)
	}

	for name := range previous {
		if _, ok := current[name]; !ok {
			delete(m.results, name)
			delete(m.errors, name)
			delete(m.retryStates, name)
		}
	}

	m.syncSpinnerStyle()
	m.syncViewport()

	cmds := []tea.Cmd{m.saveSettingsCmd()}
	newlyEnabled := 0
	for _, p := range m.providers {
		name := strings.ToLower(p.Name())
		if _, ok := previous[name]; ok {
			continue
		}
		if cached, ok := m.cachedResults[name]; ok {
			m.results[name] = cached
		}
		newlyEnabled++
		cmds = append(cmds, fetchCmd(p))
	}
	m.pending += newlyEnabled
	return tea.Batch(cmds...)
}

func (m *Model) moveSelectedProvider(delta int) tea.Cmd {
	cursor := m.menuCursor
	target := cursor + delta
	if cursor < 0 || cursor >= len(m.providers) || target < 0 || target >= len(m.providers) {
		return nil
	}

	usesDefaultQuickViewOrder := m.quickViewUsesDefaultOrder()
	m.providers[cursor], m.providers[target] = m.providers[target], m.providers[cursor]
	m.settings.ProviderOrder = m.providerNames()
	m.allProviders = config.ApplyProviderOrder(m.allProviders, m.settings.ProviderOrder)
	if usesDefaultQuickViewOrder {
		m.settings.QuickView = m.defaultOrderedQuickViewMetricIDs(m.settings.QuickView)
	}
	m.syncViewport()
	m.menuMessage = ""
	m.orderPickupIndex = -1
	m.openOrderMenu()
	m.menuCursor = target
	return m.saveSettingsCmd()
}

func (m *Model) moveSelectedQuickViewMetric(delta int) tea.Cmd {
	items, _ := m.menuItems()
	cursor := m.menuCursor
	targetRow := cursor + delta
	if cursor < 0 || cursor >= len(items) || targetRow < 0 || targetRow >= len(items) {
		return nil
	}

	currentItem := items[cursor]
	targetItem := items[targetRow]
	if currentItem.metricID == "" || targetItem.metricID == "" {
		return nil
	}
	if !m.isQuickViewMetricSelected(currentItem.metricID) || !m.isQuickViewMetricSelected(targetItem.metricID) {
		return nil
	}

	from := slices.Index(m.settings.QuickView, currentItem.metricID)
	to := slices.Index(m.settings.QuickView, targetItem.metricID)
	if from < 0 || to < 0 {
		return nil
	}

	m.settings.QuickView[from], m.settings.QuickView[to] = m.settings.QuickView[to], m.settings.QuickView[from]
	m.syncViewport()
	m.menuMessage = ""
	m.openQuickViewMenu()
	m.menuCursor = targetRow
	return m.saveSettingsCmd()
}

func (m Model) saveSettingsCmd() tea.Cmd {
	if m.saveSettings == nil {
		return nil
	}
	settings := m.settings
	return func() tea.Msg {
		return saveSettingsResultMsg{err: m.saveSettings(settings)}
	}
}

func (m Model) menuView() string {
	sections := []string{m.centerMenuText(windowStyle(m.palette).Render(m.menuTitle()))}
	if m.menuMode == menuModeMain {
		sections = append(sections, m.mainMenuPreviewView())
	}
	if m.menuMode == menuModeProviders {
		sections = append(sections, m.providersPreviewView())
	}
	if m.menuMode == menuModeOrder {
		sections = append(sections, m.orderPreviewView())
	}
	if m.menuMode == menuModeQuickView {
		sections = append(sections, m.quickViewPreviewView())
	}
	if m.menuMode == menuModeRefresh {
		sections = append(sections, m.refreshPreviewView())
	}
	sections = append(sections, m.renderMenuItems())
	if m.menuMessage != "" {
		msgStyle := subtleStyle(m.palette)
		if strings.Contains(strings.ToLower(m.menuMessage), "failed") {
			msgStyle = errorStyle(m.palette)
		}
		sections = append(sections, m.centerMenuText(msgStyle.Render(m.menuMessage)))
	}
	sections = append(sections, subtleStyle(m.palette).Render("esc: close menu"))

	content := strings.Join(sections, "\n\n")
	return menuBoxStyle(m.palette, m.menuAnimationProgress()).
		Width(m.menuWidth()).
		MarginTop(m.menuEntranceOffset()).
		Render(content)
}

func (m Model) mainMenuPreviewView() string {
	return m.centerMenuText(subtleStyle(m.palette).Render("Press Enter to open a section"))
}

func (m Model) providersPreviewView() string {
	return m.centerMenuText(subtleStyle(m.palette).Render("Press Enter or Space to toggle providers"))
}

func (m Model) quickViewPreviewView() string {
	text := strings.Join([]string{
		"Press Enter or Space to toggle metrics • reset order at the top",
		m.quickViewOrderStatusText(),
	}, "\n")
	return m.centerMenuText(subtleStyle(m.palette).Render(text))
}

func (m Model) refreshPreviewView() string {
	return m.centerMenuText(subtleStyle(m.palette).Render("Press Enter to select refresh rate"))
}

func (m Model) orderPreviewView() string {
	hint := "Press Enter to move provider • Shift+↑ / Shift+↓ also reorders"
	if m.orderPickupIndex >= 0 && m.orderPickupIndex < len(m.providers) {
		hint = "Press Enter to drop provider • j/k move cursor • Shift+↑ / Shift+↓ reorder"
	}
	return m.centerMenuText(subtleStyle(m.palette).Render(hint))
}

func orderMenuTitle(position int, name string, _ providerTheme, _ appPalette, picked, _ bool) string {
	label := fmt.Sprintf("%d. %s", position, providerDisplayName(name))
	if picked {
		return label + "  PICKED"
	}
	return label
}

func (m Model) renderMenuItems() string {
	items, _ := m.menuItems()
	if len(items) == 0 {
		return m.centerMenuText(subtleStyle(m.palette).Render("No options"))
	}
	if m.menuMode == menuModeQuickView {
		return m.menuViewport.View()
	}

	lines := make([]string, 0, len(items)*2)
	for i, item := range items {
		lines = append(lines, m.renderMenuItemTitle(item, i == m.menuCursor))
		if item.description != "" {
			lines = append(lines, m.renderMenuItemDescription(item, i == m.menuCursor))
		}
	}
	return strings.Join(lines, "\n")
}

func (m Model) renderQuickViewMenuItems(items []settingsItem) string {
	lines := make([]string, 0, len(items))
	for i, item := range items {
		lines = append(lines, m.renderQuickViewMenuItem(item, i == m.menuCursor))
	}
	return strings.Join(lines, "\n")
}

func (m Model) renderQuickViewMenuItem(item settingsItem, selected bool) string {
	style := quickViewMenuItemStyle(m.palette)
	if metric, ok := parseQuickViewMetricID(item.metricID); ok && selected {
		style = quickViewMenuSelectedItemStyle(themeForProvider(metric.ProviderName, m.palette), m.palette)
	}
	return menuSectionStyle(m.palette, m.menuItemWidth()).Render(style.Width(m.menuItemWidth()).Render(item.title))
}

func (m Model) renderMenuItemTitle(item settingsItem, selected bool) string {
	style := menuItemTitleStyle(m.palette)
	if selected {
		style = menuSelectedTitleStyle(m.palette)
	}
	return m.menuBlock(style.Width(m.menuItemWidth()).Render(item.title))
}

func (m Model) renderMenuItemDescription(item settingsItem, selected bool) string {
	style := menuItemDescStyle(m.palette)
	if selected {
		style = menuSelectedDescStyle(m.palette)
	}
	return m.menuBlock(style.Width(m.menuItemWidth()).Render(item.description))
}

func (m Model) centerMenuText(text string) string {
	return menuSectionStyle(m.palette, m.menuInnerWidth()).PaddingLeft(1).Align(lipgloss.Left).Render(text)
}

func (m Model) menuBlock(text string) string {
	return menuSectionStyle(m.palette, m.menuItemWidth()).Render(text)
}

func (m Model) menuItemWidth() int {
	if m.menuMode == menuModeQuickView && m.menuViewport.Width() > 0 {
		return m.menuViewport.Width()
	}
	return m.menuInnerWidth()
}

func (m Model) menuInnerWidth() int {
	return max(m.menuWidth()-6, 0)
}

func moveProvider(providers []provider.Provider, from, to int) []provider.Provider {
	if from < 0 || from >= len(providers) || to < 0 || to >= len(providers) || from == to {
		return providers
	}

	result := make([]provider.Provider, len(providers))
	copy(result, providers)

	moved := result[from]
	copy(result[from:], result[from+1:])
	result = result[:len(result)-1]

	result = append(result, nil)
	copy(result[to+1:], result[to:])
	result[to] = moved
	return result
}

func (m Model) overlayModal(base, modal string) string {
	if m.width <= 0 || m.height <= 0 {
		return modal
	}

	basePlaced := menuBackdropStyle(m.palette, m.menuAnimationProgress()).Render(lipgloss.Place(m.width, m.height, lipgloss.Left, lipgloss.Top, base))
	baseLines := strings.Split(basePlaced, "\n")
	modalLines := strings.Split(modal, "\n")

	modalWidth := lipgloss.Width(modal)
	modalHeight := lipgloss.Height(modal)
	offsetX := max((m.width-modalWidth)/2, 0)
	offsetY := max((m.height-modalHeight)/2, 0)

	for i, modalLine := range modalLines {
		if strings.TrimSpace(modalLine) == "" {
			continue
		}
		lineIndex := offsetY + i
		if lineIndex < 0 || lineIndex >= len(baseLines) {
			continue
		}

		baseLine := baseLines[lineIndex]
		modalLineWidth := lipgloss.Width(modalLine)
		prefix := ansi.Cut(baseLine, 0, min(offsetX, m.width))
		suffixStart := min(offsetX+modalLineWidth, m.width)
		suffix := ansi.Cut(baseLine, suffixStart, m.width)
		baseLines[lineIndex] = prefix + modalLine + suffix
	}

	return strings.Join(baseLines, "\n")
}

func (m Model) menuWidth() int {
	return max(36, min(m.width-6, 52))
}

func safeSettingsMessage(err error) string {
	var domErr *apierrors.DomainError
	if errors.As(err, &domErr) {
		return domErr.Error()
	}
	return "failed to persist agent-quota settings"
}

func (m Model) menuAnimationCmd() tea.Cmd {
	if !m.menuOpen() || m.tick == nil || m.menuAnimationFrame >= menuAnimationFrames {
		return nil
	}
	return m.tick(menuAnimationStep, func(time.Time) tea.Msg {
		return menuAnimationTickMsg{}
	})
}

func (m Model) menuAnimationProgress() float64 {
	return float64(min(max(m.menuAnimationFrame, 0), menuAnimationFrames)) / float64(menuAnimationFrames)
}

func (m Model) menuEntranceOffset() int {
	if m.menuAnimationFrame >= menuAnimationFrames {
		return 0
	}
	return menuAnimationFrames - m.menuAnimationFrame
}

const (
	minViewportWidth    = 40
	minViewportHeight   = 3
	scrollbarWidth      = 2
	menuAnimationFrames = 2
)

const menuAnimationStep = 45 * time.Millisecond

func isRetryableStatusCode(code int) bool {
	switch code {
	case 429, 500, 502, 503, 504:
		return true
	}
	return false
}

func (m Model) retryTickCmd(providerName string, generation int) tea.Cmd {
	if m.tick == nil {
		return nil
	}
	return m.tick(time.Second, func(time.Time) tea.Msg {
		return retryTickMsg{providerName: providerName, generation: generation}
	})
}

func (m *Model) seedCachedResults() {
	for _, p := range m.providers {
		name := strings.ToLower(p.Name())
		result, ok := m.cachedResults[name]
		if !ok || result.Status != "ok" {
			continue
		}
		m.results[name] = result
	}
}

func (m Model) saveQuotaCacheCmd() tea.Cmd {
	if m.saveQuotaCache == nil {
		return nil
	}
	results := cloneQuotaResults(m.cachedResults)
	return func() tea.Msg {
		return saveQuotaCacheResultMsg{err: m.saveQuotaCache(results)}
	}
}

func cloneQuotaResults(results map[string]provider.QuotaResult) map[string]provider.QuotaResult {
	if len(results) == 0 {
		return map[string]provider.QuotaResult{}
	}
	cloned := make(map[string]provider.QuotaResult, len(results))
	for name, result := range results {
		result.Windows = append([]provider.Window(nil), result.Windows...)
		if result.ExtraUsage != nil {
			extra := *result.ExtraUsage
			result.ExtraUsage = &extra
		}
		cloned[name] = result
	}
	return cloned
}

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

func retryDelaySeconds(delay time.Duration) int {
	if delay <= 0 {
		return 1
	}
	return max(1, int(math.Ceil(delay.Seconds())))
}

func formatRetryCountdown(seconds int) string {
	if seconds < 60 {
		return fmt.Sprintf("%ds", seconds)
	}
	if seconds < 3600 {
		minutes := seconds / 60
		remainingSeconds := seconds % 60
		if remainingSeconds == 0 {
			return fmt.Sprintf("%dm", minutes)
		}
		return fmt.Sprintf("%dm %ds", minutes, remainingSeconds)
	}

	hours := seconds / 3600
	remainingMinutes := (seconds % 3600) / 60
	if remainingMinutes == 0 {
		return fmt.Sprintf("%dh", hours)
	}
	return fmt.Sprintf("%dh %dm", hours, remainingMinutes)
}

func defaultRetryBackoff(providerName string, attempt int, retryAfter time.Duration) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	delay := time.Minute << min(attempt-1, 4)
	if delay > 15*time.Minute {
		delay = 15 * time.Minute
	}
	jitter := retryJitter(providerName, attempt, delay)
	delay += jitter
	if retryAfter > delay {
		return retryAfter
	}
	return delay
}

func retryJitter(providerName string, attempt int, delay time.Duration) time.Duration {
	if delay <= 0 {
		return 0
	}
	hasher := fnv.New32a()
	_, _ = fmt.Fprintf(hasher, "%s:%d", providerName, attempt)
	percentage := time.Duration(hasher.Sum32() % 21)
	return delay * percentage / 100
}
