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

	"github.com/schnetlerr/agent-quota/internal/config"
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

type refreshTickMsg struct {
	generation int
}

type saveSettingsResultMsg struct {
	err error
}

type menuAnimationTickMsg struct{}

type tickFunc func(time.Duration, func(time.Time) tea.Msg) tea.Cmd

type saveSettingsFunc func(config.Settings) error

type menuMode int

const (
	menuModeClosed menuMode = iota
	menuModeMain
	menuModeOrder
	menuModeRefresh
)

type menuItemKind int

const (
	menuItemChangeOrder menuItemKind = iota
	menuItemRefreshRate
	menuItemToggleHeader
	menuItemClose
)

type settingsItem struct {
	kind           menuItemKind
	title          string
	description    string
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

// Model is the root Bubbletea v2 model for the agent-quota TUI.
type Model struct {
	providers          []provider.Provider
	results            map[string]provider.QuotaResult
	errors             map[string]error
	spinner            spinner.Model
	viewport           viewport.Model
	palette            appPalette
	width              int
	height             int
	pending            int
	refreshInterval    time.Duration
	refreshGeneration  int
	tick               tickFunc
	settings           config.Settings
	saveSettings       saveSettingsFunc
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
	m := Model{
		providers:          providers,
		results:            make(map[string]provider.QuotaResult),
		errors:             make(map[string]error),
		spinner:            s,
		viewport:           vp,
		palette:            newPalette(detectDarkBackground()),
		width:              80,
		height:             24,
		pending:            len(providers),
		refreshInterval:    5 * time.Minute,
		tick:               tea.Tick,
		menuCursor:         0,
		orderPickupIndex:   -1,
		menuAnimationFrame: menuAnimationFrames,
	}
	for _, opt := range opts {
		opt(&m)
	}
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
		}
		if m.menuOpen() {
			return m.updateMenu(msg)
		}
		switch msg.String() {
		case "esc":
			m.openMainMenu()
			return m, m.menuAnimationCmd()
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
		if m.menuOpen() {
			m.rebuildMenu(m.menuCursor)
		}
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
		if msg.generation != m.refreshGeneration {
			return m, nil
		}
		if m.pending > 0 {
			return m, m.refreshTimerCmd()
		}
		m.pending += len(m.providers)
		return m, tea.Batch(m.fetchAllCmd(), m.refreshTimerCmd())

	case saveSettingsResultMsg:
		if msg.err != nil {
			m.menuMessage = safeSettingsMessage(msg.err)
			return m, nil
		}
		m.menuMessage = "Saved settings"
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
	case "enter":
		switch m.menuMode {
		case menuModeMain:
			return m.handleMainMenuSelection()
		case menuModeRefresh:
			return m.handleRefreshSelection()
		case menuModeOrder:
			return m.handleOrderPickupDrop()
		}
	case "J", "shift+j", "ctrl+j", "shift+down":
		if m.menuMode == menuModeOrder {
			return m, m.moveSelectedProvider(1)
		}
	case "K", "shift+k", "ctrl+k", "shift+up":
		if m.menuMode == menuModeOrder {
			return m, m.moveSelectedProvider(-1)
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
	case menuItemChangeOrder:
		m.openOrderMenu()
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
	case menuItemClose:
		m.closeMenu()
		return m, nil
	default:
		return m, nil
	}
}

func (m Model) handleRefreshSelection() (tea.Model, tea.Cmd) {
	item, ok := m.selectedSettingsItem()
	if !ok || item.refreshMinutes <= 0 {
		return m, nil
	}

	m.refreshInterval = time.Duration(item.refreshMinutes) * time.Minute
	m.settings.TUI.RefreshMinutes = item.refreshMinutes
	m.refreshGeneration++
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
		m.menuMessage = fmt.Sprintf("Picked up %s", providerDisplayName(m.providers[cursor].Name()))
		m.rebuildMenu(cursor)
		return m, nil
	}

	m.providers = moveProvider(m.providers, m.orderPickupIndex, cursor)
	m.settings.ProviderOrder = m.providerNames()
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
}

func (m *Model) updateViewportSize() {
	headerHeight := 0
	if !m.settings.TUI.HideHeader {
		headerHeight = lipgloss.Height(m.headerView())
	}
	m.viewport.SetWidth(max(m.width-scrollbarWidth, minViewportWidth))
	m.viewport.SetHeight(max(m.height-headerHeight-1, minViewportHeight))
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
		providerNames = []string{"claude", "openai", "gemini"}
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
		if m.refreshInterval > 0 {
			return lipgloss.JoinHorizontal(lipgloss.Center, m.spinner.View(), " ", subtleStyle(m.palette).Render(fmt.Sprintf("%s • refresh %s", providerCount, formatRefreshInterval(m.refreshInterval))))
		}
		return lipgloss.JoinHorizontal(lipgloss.Center, m.spinner.View(), " ", subtleStyle(m.palette).Render(fmt.Sprintf("%s • manual refresh", providerCount)))
	}

	if m.refreshInterval > 0 {
		return lipgloss.JoinHorizontal(lipgloss.Center, m.spinner.View(), " ", subtleStyle(m.palette).Render(fmt.Sprintf("live • %s • Auto-refresh every %s", providerCount, formatRefreshInterval(m.refreshInterval))))
	}
	return lipgloss.JoinHorizontal(lipgloss.Center, m.spinner.View(), " ", subtleStyle(m.palette).Render(fmt.Sprintf("live • %s • manual refresh", providerCount)))
}

func (m Model) loadingView() string {
	loading := m.pendingCount()
	if loading == 0 {
		return ""
	}
	return lipgloss.JoinHorizontal(lipgloss.Center, m.spinner.View(), " ", subtleStyle(m.palette).Render(fmt.Sprintf("fetching quota data (%d remaining)", loading)))
}

func (m Model) bodyContent() string {
	var b strings.Builder
	cardWidth := max(m.viewport.Width(), minViewportWidth)

	for _, p := range m.providers {
		name := p.Name()
		if r, ok := m.results[name]; ok {
			b.WriteString(renderProviderCardWithPalette(r, cardWidth, m.palette))
			b.WriteString("\n\n")
			continue
		}
		if err, ok := m.errors[name]; ok {
			errResult := provider.QuotaResult{
				Provider: name,
				Status:   "error",
			}
			b.WriteString(renderProviderCardWithPalette(errResult, cardWidth, m.palette))
			b.WriteString("\n")
			var domErr *apierrors.DomainError
			if errors.As(err, &domErr) {
				b.WriteString(errorStyle(m.palette).Render("  " + domErr.Error()))
			} else {
				b.WriteString(errorStyle(m.palette).Render("  an unexpected error occurred"))
			}
			b.WriteString("\n\n")
		}
	}

	return strings.TrimRight(b.String(), "\n")
}

func (m Model) footerText() string {
	switch m.menuMode {
	case menuModeMain:
		return "↑/↓ move • enter select • esc close • q quit"
	case menuModeRefresh:
		return "↑/↓ choose • enter apply • esc back • q quit"
	case menuModeOrder:
		return "j/k move cursor • enter pick up/drop • shift+↑/shift+↓ reorder • esc back • q quit"
	default:
		return "j/k scroll • pgup/pgdn page • esc menu • q quit"
	}
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
			lines[i] = scrollThumbStyle(m.palette).Render("█")
			continue
		}
		lines[i] = scrollTrackStyle(m.palette).Render("│")
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
	generation := m.refreshGeneration
	return m.tick(m.refreshInterval, func(time.Time) tea.Msg {
		return refreshTickMsg{generation: generation}
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

func (m *Model) openMainMenu() {
	m.menuMode = menuModeMain
	m.orderPickupIndex = -1
	m.menuAnimationFrame = 0
	m.rebuildMenu(0)
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
		return
	}
	m.menuCursor = min(max(cursor, 0), len(items)-1)
}

func (m *Model) moveMenuCursor(delta int) {
	items, _ := m.menuItems()
	if len(items) == 0 {
		m.menuCursor = 0
		return
	}
	m.menuCursor = min(max(m.menuCursor+delta, 0), len(items)-1)
}

func (m Model) menuItems() ([]settingsItem, string) {
	switch m.menuMode {
	case menuModeOrder:
		return m.orderMenuItems(), "Change order"
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

	return []settingsItem{
		settingsItem{
			kind:        menuItemChangeOrder,
			title:       "Change order",
			description: m.providerOrderDescription(),
		},
		settingsItem{
			kind:        menuItemRefreshRate,
			title:       "Refresh rate",
			description: formatRefreshInterval(m.refreshInterval),
		},
		settingsItem{
			kind:        menuItemToggleHeader,
			title:       headerTitle,
			description: headerDescription,
		},
		settingsItem{
			kind:        menuItemClose,
			title:       "Close",
			description: "Return to the dashboard",
		},
	}
}

func (m Model) orderMenuItems() []settingsItem {
	names := m.providerNames()
	items := make([]settingsItem, 0, len(names))
	carrying := m.orderPickupIndex >= 0
	for i, name := range names {
		theme := themeForProvider(name, m.palette)
		title := orderMenuTitle(name, theme, m.palette, i == m.orderPickupIndex, carrying)
		description := fmt.Sprintf("Position %d", i+1)
		if i == 0 {
			description = "Shown first"
		}
		if i == m.orderPickupIndex {
			description = orderPickedBadgeStyle(theme).Render("MOVE WITH J/K OR SHIFT+↑/↓")
		} else if carrying {
			description = orderDimmedStyle(m.palette).Render(description)
		}
		items = append(items, settingsItem{
			title:       title,
			description: description,
		})
	}
	return items
}

func (m Model) refreshMenuItems() []settingsItem {
	options := []int{1, 2, 5, 10, 15, 30}
	items := make([]settingsItem, 0, len(options))
	current := int(m.refreshInterval / time.Minute)
	for _, minutes := range options {
		description := fmt.Sprintf("Every %d minute", minutes)
		if minutes != 1 {
			description += "s"
		}
		if minutes == current {
			description = "Current"
		}
		items = append(items, settingsItem{
			kind:           menuItemRefreshRate,
			title:          formatRefreshInterval(time.Duration(minutes) * time.Minute),
			description:    description,
			refreshMinutes: minutes,
		})
	}
	return items
}

func (m Model) providerOrderDescription() string {
	names := m.providerNames()
	if len(names) == 0 {
		return "No providers selected"
	}
	labels := make([]string, 0, len(names))
	for _, name := range names {
		labels = append(labels, providerDisplayName(name))
	}
	return strings.Join(labels, " → ")
}

func (m Model) providerNames() []string {
	names := make([]string, len(m.providers))
	for i, p := range m.providers {
		names[i] = p.Name()
	}
	return names
}

func (m Model) selectedSettingsItem() (settingsItem, bool) {
	items, _ := m.menuItems()
	if m.menuCursor < 0 || m.menuCursor >= len(items) {
		return settingsItem{}, false
	}
	return items[m.menuCursor], true
}

func (m *Model) moveSelectedProvider(delta int) tea.Cmd {
	cursor := m.menuCursor
	target := cursor + delta
	if cursor < 0 || cursor >= len(m.providers) || target < 0 || target >= len(m.providers) {
		return nil
	}

	m.providers[cursor], m.providers[target] = m.providers[target], m.providers[cursor]
	m.settings.ProviderOrder = m.providerNames()
	m.syncViewport()
	m.menuMessage = ""
	m.orderPickupIndex = -1
	m.openOrderMenu()
	m.menuCursor = target
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
	sections := make([]string, 0, 4)
	sections = append(sections, m.centerMenuText(windowStyle(m.palette).Render(m.menuTitle())))
	if m.menuMode == menuModeOrder {
		sections = append(sections, m.orderPreviewView())
	}
	sections = append(sections, m.renderMenuItems())
	if m.menuMessage != "" {
		msgStyle := subtleStyle(m.palette)
		if strings.Contains(strings.ToLower(m.menuMessage), "failed") {
			msgStyle = errorStyle(m.palette)
		}
		sections = append(sections, m.centerMenuText(msgStyle.Render(m.menuMessage)))
	}

	content := lipgloss.JoinVertical(lipgloss.Left, sections...)
	return menuBoxStyle(m.palette, m.menuAnimationProgress()).
		Width(m.menuWidth()).
		MarginTop(m.menuEntranceOffset()).
		Render(content)
}

func (m Model) orderPreviewView() string {
	selected := m.selectedOrderIndex()
	hint := "Press Enter to pick up a provider, then move and press Enter again to drop"
	if m.orderPickupIndex >= 0 && m.orderPickupIndex < len(m.providers) {
		hint = fmt.Sprintf("Picked up: %s • move with j/k, then Enter to drop", providerDisplayName(m.providers[m.orderPickupIndex].Name()))
	} else if selected >= 0 && selected < len(m.providers) {
		hint = fmt.Sprintf("Selected: %s • Enter to pick up • Shift+↑ / Shift+↓ also reorders", providerDisplayName(m.providers[selected].Name()))
	}

	return lipgloss.JoinVertical(
		lipgloss.Left,
		m.centerMenuText(windowStyle(m.palette).Render("Current order")),
		m.centerMenuText(subtleStyle(m.palette).Render(m.providerOrderDescription())),
		m.centerMenuText(renderProviderOrderPreview(m.providerNames(), selected, m.palette)),
		m.centerMenuText(subtleStyle(m.palette).Render(hint)),
	)
}

func renderProviderOrderPreview(names []string, selected int, palette appPalette) string {
	if len(names) == 0 {
		return subtleStyle(palette).Render("No providers selected")
	}

	parts := make([]string, 0, len(names)*2-1)
	for i, name := range names {
		theme := themeForProvider(name, palette)
		chip := providerChipStyle(theme).Render(providerDisplayName(name))
		if i == selected {
			chip = providerBadgeStyle(theme).Render(providerDisplayName(name))
		}
		parts = append(parts, chip)
		if i < len(names)-1 {
			parts = append(parts, subtleStyle(palette).Render("→"))
		}
	}
	return lipgloss.JoinHorizontal(lipgloss.Center, parts...)
}

func orderMenuTitle(name string, theme providerTheme, palette appPalette, picked, carrying bool) string {
	label := providerLabel(name)
	if picked {
		return lipgloss.JoinHorizontal(
			lipgloss.Center,
			orderPickedAccentStyle(theme).Render("▌"),
			" ",
			orderPickedTitleStyle(theme).Render(label),
			" ",
			orderPickedBadgeStyle(theme).Render("PICKED UP"),
		)
	}
	if carrying {
		return orderDimmedStyle(palette).Render(label)
	}
	return orderIdleTitleStyle(theme).Render(label)
}

func (m Model) selectedOrderIndex() int {
	if len(m.providers) == 0 {
		return -1
	}
	if m.orderPickupIndex >= 0 && m.orderPickupIndex < len(m.providers) {
		return m.orderPickupIndex
	}
	return min(max(m.menuCursor, 0), len(m.providers)-1)
}

func (m Model) renderMenuItems() string {
	items, _ := m.menuItems()
	if len(items) == 0 {
		return m.centerMenuText(subtleStyle(m.palette).Render("No options"))
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

func (m Model) renderMenuItemTitle(item settingsItem, selected bool) string {
	style := menuItemTitleStyle(m.palette)
	if selected {
		style = menuSelectedTitleStyle(m.palette)
	}
	return m.menuBlock(style.Width(m.menuInnerWidth()).Render(item.title))
}

func (m Model) renderMenuItemDescription(item settingsItem, selected bool) string {
	style := menuItemDescStyle(m.palette)
	if selected {
		style = menuSelectedDescStyle(m.palette)
	}
	return m.menuBlock(style.Width(m.menuInnerWidth()).Render(item.description))
}

func (m Model) centerMenuText(text string) string {
	return menuSectionStyle(m.palette, m.menuInnerWidth()).PaddingLeft(1).Align(lipgloss.Left).Render(text)
}

func (m Model) menuBlock(text string) string {
	return menuSectionStyle(m.palette, m.menuInnerWidth()).Render(text)
}

func (m Model) menuInnerWidth() int {
	return max(m.menuWidth()-6, 0)
}

func moveProvider(providers []provider.Provider, from, to int) []provider.Provider {
	if from < 0 || from >= len(providers) || to < 0 || to >= len(providers) || from == to {
		return providers
	}

	moved := providers[from]
	copy(providers[from:], providers[from+1:])
	providers = providers[:len(providers)-1]

	providers = append(providers, nil)
	copy(providers[to+1:], providers[to:])
	providers[to] = moved
	return providers
}

func (m Model) overlayModal(base, modal string) string {
	if m.width <= 0 || m.height <= 0 {
		return modal
	}

	basePlaced := menuBackdropStyle(m.palette, m.menuAnimationProgress()).Render(lipgloss.Place(m.width, m.height, lipgloss.Left, lipgloss.Top, base))
	modalPlaced := lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, modal)

	baseLines := strings.Split(basePlaced, "\n")
	modalLines := strings.Split(modalPlaced, "\n")
	lineCount := max(len(baseLines), len(modalLines))
	merged := make([]string, 0, lineCount)
	for i := 0; i < lineCount; i++ {
		var baseLine, modalLine string
		if i < len(baseLines) {
			baseLine = baseLines[i]
		}
		if i < len(modalLines) {
			modalLine = modalLines[i]
		}
		if strings.TrimSpace(modalLine) == "" {
			merged = append(merged, baseLine)
			continue
		}
		merged = append(merged, modalLine)
	}
	return strings.Join(merged, "\n")
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
