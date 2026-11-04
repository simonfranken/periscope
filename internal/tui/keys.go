package tui

import (
	"github.com/charmbracelet/bubbles/key"

	"periscope/internal/tui/footer"
)

type keyMap struct {
	Up, Down, Left, Right, Toggle key.Binding
	Enter, Back                   key.Binding
	CollapseBelow, ExpandBelow    key.Binding
	Home, End, PageUp, PageDown   key.Binding
	Reload, Filter, Issues        key.Binding
	Context                       key.Binding
	Copy                          key.Binding
	Help, Quit, QuitUpdate        key.Binding

	inside bool // the tree shows a namespace (or section), not the cluster level
}

func defaultKeys() keyMap {
	return keyMap{
		Up:            key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down:          key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		Left:          key.NewBinding(key.WithKeys("left", "h"), key.WithHelp("←/h", "collapse, parent, back")),
		Right:         key.NewBinding(key.WithKeys("right", "l"), key.WithHelp("→/l", "open, expand")),
		Toggle:        key.NewBinding(key.WithKeys(" "), key.WithHelp("space", "toggle")),
		Enter:         key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open, expand")),
		Back:          key.NewBinding(key.WithKeys("backspace"), key.WithHelp("⌫", "back to the cluster")),
		CollapseBelow: key.NewBinding(key.WithKeys("shift+left", "ctrl+left", "H"), key.WithHelp("H/⇧←", "collapse all below")),
		ExpandBelow:   key.NewBinding(key.WithKeys("shift+right", "ctrl+right", "L"), key.WithHelp("L/⇧→", "expand all below")),
		Home:          key.NewBinding(key.WithKeys("home", "g"), key.WithHelp("home/g", "first")),
		End:           key.NewBinding(key.WithKeys("end", "G"), key.WithHelp("end/G", "last")),
		PageUp:        key.NewBinding(key.WithKeys("pgup"), key.WithHelp("pgup", "page up")),
		PageDown:      key.NewBinding(key.WithKeys("pgdown"), key.WithHelp("pgdn", "page down")),
		Reload:        key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "reload")),
		Filter:        key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
		Issues:        key.NewBinding(key.WithKeys("!"), key.WithHelp("!", "problems only")),
		Context:       key.NewBinding(key.WithKeys("C"), key.WithHelp("C", "context")),
		Copy:          key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "copy name")),
		Help:          key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Quit:          key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
		QuitUpdate:    key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit and update"), key.WithDisabled()),
	}
}

// SetUpdate announces the newer release v: q quits and updates to it, and the
// plain quit moves to Q (ctrl+c always quits without updating).
func (k *keyMap) SetUpdate(v string) {
	k.Quit = key.NewBinding(key.WithKeys("Q", "ctrl+c"), key.WithHelp("Q", "quit"))
	k.QuitUpdate = key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit and update to "+v))
}

// Navigation returns the bindings that move the selection or fold the tree:
// at the cluster level how to open a namespace, inside one how to fold and
// how to get back.
func (k keyMap) Navigation() []key.Binding {
	move := key.NewBinding(key.WithKeys("up"), key.WithHelp("↑↓/jk", "move"))
	if !k.inside {
		open := key.NewBinding(key.WithKeys("right"), key.WithHelp("→/enter", "open"))
		return []key.Binding{move, open}
	}
	fold := key.NewBinding(key.WithKeys("left"), key.WithHelp("←→/hl", "collapse/expand"))
	all := key.NewBinding(key.WithKeys("H"), key.WithHelp("⇧←→/HL", "all below"))
	back := key.NewBinding(key.WithKeys("backspace"), key.WithHelp("⌫", "back"))
	return []key.Binding{move, fold, all, k.Toggle, back}
}

// Tools returns the bindings that act on the whole list, most useful first,
// since the footer drops them from the end.
func (k keyMap) Tools() []key.Binding {
	return []key.Binding{k.Issues, k.Filter, k.Context, k.Reload}
}

// Resource returns the bindings that act on the selected object. They are
// shown in the detail pane, or in the footer if the pane can't show them.
func (k keyMap) Resource() []key.Binding { return []key.Binding{k.Copy} }

// Global returns the bindings that don't depend on the selection.
func (k keyMap) Global() []key.Binding { return []key.Binding{k.Help, k.Quit} }

// fullUpdate is Highlight with a short description, for the full help.
func (k keyMap) fullUpdate() []key.Binding {
	if !k.QuitUpdate.Enabled() {
		return nil
	}
	return []key.Binding{key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit and update"))}
}

// Highlight returns the bindings that are shown after Global on a highlighted
// background: quit and update, if there is an update.
func (k keyMap) Highlight() []key.Binding {
	if !k.QuitUpdate.Enabled() {
		return nil
	}
	return []key.Binding{k.QuitUpdate}
}

// HelpGroups returns the columns of the full help, each with a title.
func (k keyMap) HelpGroups() []footer.Group {
	help := key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "what words mean"))
	esc := key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "close"))
	return []footer.Group{
		{Title: "Move", Keys: []key.Binding{k.Up, k.Down, k.Left, k.Right, k.Enter, k.Toggle, k.CollapseBelow, k.ExpandBelow, k.Back}},
		{Title: "Jump", Keys: []key.Binding{k.Home, k.End, k.PageUp, k.PageDown}},
		{Title: "Selected", Keys: k.Resource()},
		{Title: "Everything", Keys: []key.Binding{k.Issues, k.Filter, k.Context, k.Reload}},
		{Title: "App", Keys: append([]key.Binding{help, esc, k.Quit}, k.fullUpdate()...)},
	}
}

// LegendKeys returns the column next to the legend, the help's last page;
// ? closes it too, but esc is the key to tell.
func (k keyMap) LegendKeys() footer.Group {
	esc := key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "close"))
	return footer.Group{Title: "App", Keys: append([]key.Binding{esc, k.Quit}, k.fullUpdate()...)}
}
