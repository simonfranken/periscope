package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"periscope/internal/resource"
	"periscope/internal/tui/picker"
)

// contextsMsg is the list of kubeconfig contexts for the picker.
type contextsMsg struct {
	contexts []resource.Context
	err      error
}

// switchedMsg is the first tree of the context name, read by p within
// pctx, which becomes the provider's context if the switch succeeds.
type switchedMsg struct {
	name    string
	p       resource.Provider
	pctx    context.Context
	pcancel context.CancelFunc
	cluster *resource.Cluster
	err     error
}

// openPicker opens the context picker and reads the contexts.
func (m Model) openPicker() (tea.Model, tea.Cmd) {
	sw, ok := m.provider.(resource.Switcher)
	var flash string
	switch {
	case !ok:
		flash = "this data source has no other contexts"
	case m.switching != "":
		flash = "wait for the switch to " + m.switching // its list would be the old context's
	}
	if flash != "" {
		cmd := m.footer.Flash(flash)
		m.layout()
		return m, cmd
	}
	m.picker = picker.New(m.theme, "Switch context", "switch", "contexts")
	m.picker.SetSpinner(m.spinner.View())
	ctx := m.ctx
	load := func() tea.Msg {
		cs, err := sw.Contexts(ctx)
		return contextsMsg{cs, err}
	}
	return m, tea.Batch(load, m.tick())
}

// contextsLoaded fills the picker, unless it was closed meanwhile.
func (m Model) contextsLoaded(msg contextsMsg) (tea.Model, tea.Cmd) {
	if m.picker == nil {
		return m, nil
	}
	if msg.err != nil {
		m.picker.SetErr(msg.err)
		return m, nil
	}
	items := make([]picker.Item, len(msg.contexts))
	for i, c := range msg.contexts {
		meta := c.Cluster
		if c.Namespace != "" {
			meta += " · " + c.Namespace
		}
		items[i] = picker.Item{Name: c.Name, Meta: meta, Current: c.Current}
	}
	m.picker.SetItems(items)
	return m, nil
}

// pickContext handles a key while the picker is open: arrows move, enter
// switches, esc closes, everything else edits the search.
func (m Model) pickContext(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyEsc:
		m.picker = nil
	case tea.KeyUp:
		m.picker.Up()
	case tea.KeyDown:
		m.picker.Down()
	case tea.KeyPgUp:
		m.picker.PageUp()
	case tea.KeyPgDown:
		m.picker.PageDown()
	case tea.KeyEnter:
		return m.confirmPick()
	default:
		return m, m.picker.Update(msg)
	}
	return m, nil
}

// confirmPick closes the picker and switches to the selected context. The
// current cluster stays on screen until the new one's tree has loaded, so a
// context that can't be reached costs nothing.
func (m Model) confirmPick() (tea.Model, tea.Cmd) {
	it := m.picker.Selected()
	if it == nil {
		return m, nil
	}
	m.picker = nil
	if it.Current {
		return m, nil
	}
	sw, ok := m.provider.(resource.Switcher)
	if !ok {
		return m, nil
	}
	p, err := sw.Switch(it.Name)
	if err != nil {
		cmd := m.footer.Alert("can't switch to " + it.Name + ": " + err.Error())
		m.layout()
		return m, cmd
	}
	m.switching, m.loadingTree = it.Name, true
	pctx, pcancel := context.WithCancel(m.ctx)
	name := it.Name
	load := func() tea.Msg {
		c, err := p.Tree(pctx)
		return switchedMsg{name, p, pctx, pcancel, c, err}
	}
	m.layout() // the line above the footer says it's switching
	return m, tea.Batch(load, m.tick())
}

// switched installs the new context's tree, or keeps the old one and says
// why the new one failed.
func (m Model) switched(msg switchedMsg) (tea.Model, tea.Cmd) {
	if msg.name != m.switching {
		return m, nil
	}
	m.switching, m.loadingTree = "", false
	if msg.err != nil {
		if msg.pcancel != nil {
			msg.pcancel()
		}
		cmd := m.footer.Alert("can't reach " + msg.name + ": " + msg.err.Error())
		m.layout()
		return m, cmd
	}
	m.pcancel() // the old provider's requests stop
	m.provider, m.pctx, m.pcancel = msg.p, msg.pctx, msg.pcancel
	m.cluster, m.byPath = nil, nil // a new cluster: treeLoaded starts afresh
	if m.filtering {
		m.closeFilter()
	}
	next, cmd := m.treeLoaded(treeLoadedMsg{msg.p, msg.cluster, nil})
	mm := next.(Model)
	flash := mm.footer.Flash("switched to " + msg.name)
	mm.layout()
	return mm, tea.Batch(cmd, flash)
}

// mousePicker handles the mouse while the picker is open: the wheel moves
// the selection, a click on a context switches to it, a click outside the
// popup closes it.
func (m Model) mousePicker(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		m.picker.Up()
	case tea.MouseButtonWheelDown:
		m.picker.Down()
	case tea.MouseButtonLeft:
		if msg.Action != tea.MouseActionPress {
			return m, nil
		}
		inside, hit := m.picker.Click(msg.X, msg.Y, m.width, m.height)
		switch {
		case hit:
			return m.confirmPick()
		case !inside:
			m.picker = nil
		}
	}
	return m, nil
}
