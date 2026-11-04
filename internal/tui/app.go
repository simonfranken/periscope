// Package tui is the Bubble Tea application. It only talks to cluster data
// through resource.Provider.
package tui

import (
	"context"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	zone "github.com/lrstanley/bubblezone"

	"periscope/internal/resource"
	"periscope/internal/tui/detail"
	"periscope/internal/tui/footer"
	"periscope/internal/tui/header"
	"periscope/internal/tui/theme"
	"periscope/internal/tui/tree"
)

const (
	headerHeight     = 1
	minDetailWidth   = 36
	detailHideBelow  = 90 // terminal width below which the detail pane is hidden
	detailWidthRatio = 0.4
)

type Model struct {
	ctx      context.Context
	provider resource.Provider
	theme    theme.Theme
	keys     keyMap

	cluster *resource.Cluster
	byPath  map[string]*resource.Object
	err     error

	loadingTree bool // a tree load is running; r does nothing meanwhile
	ticking     bool // a spinner tick is scheduled

	tree    tree.Model
	detail  detail.Model
	footer  footer.Model
	spinner spinner.Model
	filter  textinput.Model
	// filtering is true while the filter line is shown; the filter applies
	// exactly then. The input is focused while typing and blurred after enter.
	filtering bool
	hint      bool // the hint for newcomers above the footer, until the first key

	checkUpdate func(context.Context) string // looks for a newer release, nil if off
	update      string                       // the newer release found, "" if none
	updateTo    string                       // the release to quit and update to, "" if not chosen

	width, height int
	showDetail    bool
}

// Option configures New.
type Option func(*Model)

// WithUpdateCheck looks for a newer release at startup, in the background.
// check returns its version, or "" if there is none. With one, the footer
// offers to quit and update.
func WithUpdateCheck(check func(context.Context) string) Option {
	return func(m *Model) { m.checkUpdate = check }
}

// UpdateTo returns the release the user chose to quit and update to, or "".
func (m Model) UpdateTo() string { return m.updateTo }

func New(ctx context.Context, p resource.Provider, opts ...Option) Model {
	t := theme.Default()
	k := defaultKeys()
	sp := spinner.New(spinner.WithSpinner(spinner.MiniDot))
	fi := textinput.New()
	fi.Prompt = "/ "
	fi.PromptStyle = theme.Fg(t.Primary)
	fi.Cursor.Style = theme.Fg(t.Primary)
	fi.Placeholder = "filter by namespace, kind or name"
	fi.PlaceholderStyle = theme.Fg(t.Muted)
	m := Model{
		ctx:         ctx,
		provider:    p,
		theme:       t,
		keys:        k,
		loadingTree: true, // Init starts it
		ticking:     true, // Init schedules the first tick
		tree:        tree.New(t),
		detail:      detail.New(t, k.Resource()),
		footer:      footer.New(t, k),
		spinner:     sp,
		filter:      fi,
		hint:        true,
	}
	for _, o := range opts {
		o(&m)
	}
	return m
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.loadTree(), m.spinner.Tick, m.lookForUpdate())
}

func (m Model) lookForUpdate() tea.Cmd {
	if m.checkUpdate == nil {
		return nil
	}
	return func() tea.Msg { return updateMsg{m.checkUpdate(m.ctx)} }
}

func (m Model) loadTree() tea.Cmd {
	return func() tea.Msg {
		c, err := m.provider.Tree(m.ctx)
		return treeLoadedMsg{c, err}
	}
}

func (m Model) loadStatus(path string) tea.Cmd {
	return func() tea.Msg {
		st, err := m.provider.Status(m.ctx, path)
		return statusLoadedMsg{path, st, err}
	}
}

// busy reports whether something shows a spinner.
func (m Model) busy() bool {
	if m.cluster == nil {
		return true
	}
	loading := m.cluster.Root != nil && m.cluster.Root.Status == nil && m.cluster.Root.Err == nil
	resource.Walk(m.cluster.Objects, func(o *resource.Object) {
		loading = loading || (o.Status == nil && o.Err == nil)
	})
	return loading
}

// tick schedules a spinner tick if none is scheduled.
func (m *Model) tick() tea.Cmd {
	if m.ticking {
		return nil
	}
	m.ticking = true
	return m.spinner.Tick
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		return m, nil

	case updateMsg:
		if msg.version != "" {
			m.update = msg.version
			m.keys.SetUpdate(msg.version)
			m.footer.SetKeys(m.keys)
			m.layout()
		}
		return m, nil

	case treeLoadedMsg:
		return m.treeLoaded(msg)

	case statusLoadedMsg:
		if o, ok := m.byPath[msg.path]; ok {
			if msg.err != nil {
				o.Err = msg.err
			} else {
				o.Status, o.Err = msg.status, nil
			}
			m.tree.Refit()
		}
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		m.tree.SetSpinner(m.spinner.View())
		m.detail.SetSpinner(m.spinner.View())
		if !m.busy() {
			m.ticking = false // stop ticking until there is work again
			return m, nil
		}
		return m, cmd

	case tea.MouseMsg:
		m.tree.Update(msg)
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	m.footer.Update(msg)
	return m, nil
}

// treeLoaded installs a (re)loaded tree and loads every status. On a reload
// the known statuses are carried over by path, so rows don't fall back to a
// spinner while the new ones load.
func (m Model) treeLoaded(msg treeLoadedMsg) (tea.Model, tea.Cmd) {
	m.loadingTree = false
	initial := m.cluster == nil
	if msg.err != nil {
		if initial {
			m.err = msg.err
			return m, nil
		}
		cmd := m.footer.Flash("reload failed: " + msg.err.Error())
		return m, cmd
	}

	old := m.byPath
	m.cluster = msg.cluster
	m.byPath = map[string]*resource.Object{}
	var cmds []tea.Cmd
	track := func(o *resource.Object) {
		m.byPath[o.Path] = o
		if p, ok := old[o.Path]; ok {
			o.Status, o.Err = p.Status, p.Err
		}
		cmds = append(cmds, m.loadStatus(o.Path))
	}
	if m.cluster.Root != nil {
		track(m.cluster.Root)
	}
	resource.Walk(m.cluster.Objects, track)
	m.tree.SetRoot(m.cluster.Root)
	if initial {
		m.tree.SetRoots(m.cluster.Objects)
	} else {
		m.tree.ReplaceRoots(m.cluster.Objects)
	}
	cmds = append(cmds, m.tick())
	return m, tea.Batch(cmds...)
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := m.keys
	if m.hint {
		m.hint = false
		m.layout()
	}
	if m.filter.Focused() {
		return m.typeFilter(msg)
	}
	switch {
	case m.footer.HelpOpen() && msg.Type == tea.KeyEsc: // before the filter's esc
		m.footer.CloseHelp()
		m.layout()
	case m.filtering && msg.Type == tea.KeyEsc:
		m.closeFilter()
	case key.Matches(msg, k.Quit):
		return m, tea.Quit
	case key.Matches(msg, k.QuitUpdate):
		m.updateTo = m.update
		return m, tea.Quit
	case key.Matches(msg, k.Help):
		m.footer.CycleHelp()
		m.layout()
	case key.Matches(msg, k.Up):
		m.tree.Up()
	case key.Matches(msg, k.Down):
		m.tree.Down()
	case key.Matches(msg, k.Left):
		m.tree.Left()
	case key.Matches(msg, k.Right):
		m.tree.Right()
	case key.Matches(msg, k.Toggle):
		m.tree.Toggle()
	case key.Matches(msg, k.CollapseBelow):
		m.tree.CollapseBelow()
	case key.Matches(msg, k.ExpandBelow):
		m.tree.ExpandBelow()
	case key.Matches(msg, k.Home):
		m.tree.Home()
	case key.Matches(msg, k.End):
		m.tree.End()
	case key.Matches(msg, k.PageUp):
		m.tree.PageUp()
	case key.Matches(msg, k.PageDown):
		m.tree.PageDown()
	case key.Matches(msg, k.Reload):
		if m.loadingTree {
			return m, nil
		}
		m.loadingTree = true
		return m, m.loadTree()
	case key.Matches(msg, k.Copy):
		return m.copyName()
	case key.Matches(msg, k.Filter):
		m.filtering = true
		m.layout()
		return m, m.filter.Focus()
	case key.Matches(msg, k.Issues):
		m.tree.SetProblemsOnly(!m.tree.ProblemsOnly())
		m.layout()
	}
	return m, nil
}

// typeFilter handles a key while the filter input has the focus: text edits
// the filter, enter hands the keys back to the tree (the filter stays), esc
// closes the filter. Arrows still move the selection.
func (m Model) typeFilter(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyEsc:
		m.closeFilter()
	case tea.KeyEnter:
		m.filter.Blur()
	case tea.KeyUp:
		m.tree.Up()
	case tea.KeyDown:
		m.tree.Down()
	case tea.KeyPgUp:
		m.tree.PageUp()
	case tea.KeyPgDown:
		m.tree.PageDown()
	default:
		var cmd tea.Cmd
		m.filter, cmd = m.filter.Update(msg)
		m.tree.SetFilter(m.filter.Value())
		return m, cmd
	}
	return m, nil
}

// closeFilter hides the filter line and removes the filter; the selection stays.
func (m *Model) closeFilter() {
	m.filtering = false
	m.filter.Blur()
	m.filter.Reset()
	m.tree.SetFilter("")
	m.layout()
}

// filterView is the line above the footer: the input and, right-aligned, the
// keys that apply.
func (m Model) filterView() string {
	hint := "enter navigate • esc close"
	if !m.filter.Focused() {
		hint = "/ edit • esc close"
	}
	if m.tree.ProblemsOnly() {
		hint = "problems only (!) • " + hint
	}
	hint = theme.Fg(m.theme.Muted).Render(hint)
	m.filter.Width = max(m.width-lipgloss.Width(hint)-6, 1)
	in := m.filter.View()
	return m.line(in, hint)
}

// extraLine reports whether a line above the footer is shown: the filter,
// the notice that only problems are shown, or the hint for newcomers.
func (m Model) extraLine() bool {
	return m.filtering || m.tree.ProblemsOnly() || m.hint
}

// line lays out the line above the footer: left, and right right-aligned.
func (m Model) line(left, right string) string {
	gap := max(m.width-2-lipgloss.Width(left)-lipgloss.Width(right), 1)
	return lipgloss.NewStyle().Width(m.width).Padding(0, 1).Render(left + strings.Repeat(" ", gap) + right)
}

// hintView is the line above the footer until the first key: where help is.
func (m Model) hintView() string {
	muted := theme.Fg(m.theme.Muted)
	return m.line(muted.Render("new here? ")+theme.Fg(m.theme.Text).Bold(true).Render("?")+muted.Render(" shows every key and what the words mean"), "")
}

// problemsView is the line above the footer while only problems are shown, so
// a short list is not mistaken for a healthy cluster.
func (m Model) problemsView() string {
	return m.line(theme.Fg(m.theme.Warn).Render("problems only"), theme.Fg(m.theme.Muted).Render("! show all"))
}

// copyName copies how kubectl names the selection (e.g. "deployment/api"),
// or the context for the cluster.
func (m Model) copyName() (tea.Model, tea.Cmd) {
	o := m.tree.Selected()
	if o == nil {
		return m, nil
	}
	name := o.Ref()
	if o.IsRoot() && m.cluster != nil {
		name = m.cluster.Context
	} else if o.Kind == resource.KindNamespace {
		name = o.Name
	}
	cmd := tea.Batch(copyToClipboard(name), m.footer.Flash("copied "+name))
	m.layout()
	return m, cmd
}

// layout distributes the terminal size among the components.
func (m *Model) layout() {
	m.footer.SetWidth(m.width)
	bodyHeight := m.height - headerHeight - lipgloss.Height(m.footer.View())
	if m.extraLine() {
		bodyHeight--
	}
	bodyHeight = max(bodyHeight, 0)

	m.showDetail = m.width >= detailHideBelow
	treeWidth := m.width
	if m.showDetail {
		detailWidth := max(int(float64(m.width)*detailWidthRatio), minDetailWidth)
		treeWidth = m.width - detailWidth
		m.detail.SetSize(detailWidth-2, bodyHeight) // border + padding
	}
	m.footer.SetActions(!m.showDetail || !m.detail.PaletteVisible())
	m.tree.SetSize(treeWidth, bodyHeight)
}

func (m Model) View() string {
	if m.width == 0 {
		return ""
	}
	if m.err != nil {
		return theme.Fg(m.theme.Danger).Render("error: " + m.err.Error())
	}

	body := m.tree.View()
	if m.showDetail {
		pane := lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), false, false, false, true).
			BorderForeground(m.theme.Border).
			PaddingLeft(1).
			Render(m.detail.View(m.tree.Selected(), m.cluster))
		body = lipgloss.JoinHorizontal(lipgloss.Top, body, pane)
	}

	head := header.View(m.theme, m.cluster, m.spinner.View(), m.width, m.tree.RootSelected())
	parts := []string{zone.Mark(m.tree.RootZone(), head), body}
	switch {
	case m.filtering:
		parts = append(parts, m.filterView())
	case m.tree.ProblemsOnly():
		parts = append(parts, m.problemsView())
	case m.hint:
		parts = append(parts, m.hintView())
	}
	return zone.Scan(lipgloss.JoinVertical(lipgloss.Left, append(parts, m.footer.View())...))
}
