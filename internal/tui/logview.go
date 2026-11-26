package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	zone "github.com/lrstanley/bubblezone"

	"periscope/internal/resource"
	"periscope/internal/tui/footer"
	"periscope/internal/tui/logs"
	"periscope/internal/tui/theme"
)

// logDelay is how long the selection has to rest before its log is opened
// for the preview, so moving through the tree doesn't open a stream per row.
const logDelay = 200 * time.Millisecond

// maxLogBatch is how many lines one message carries at most.
const maxLogBatch = 2000

// Mouse zones of the log view.
const (
	logZones    = "app:"
	logBackZone = "app:logback"
)

// logItem is a line read from a log, or the error that ended it (io.EOF at
// its end).
type logItem struct {
	line resource.LogLine
	err  error
}

// logLinesMsg carries the lines of stream id read since the last one; end
// is set when the log ended, with err if it failed.
type logLinesMsg struct {
	id    int
	lines []resource.LogLine
	end   bool
	err   error
}

// logDelayMsg opens the preview's log once the selection rested.
type logDelayMsg struct{ seq int }

// pump reads the log r asks for into ch until it ends or ctx is done.
func pump(ctx context.Context, lg resource.Logger, r resource.LogRequest, ch chan<- logItem) {
	defer close(ch)
	send := func(it logItem) bool {
		select {
		case ch <- it:
			return true
		case <-ctx.Done():
			return false
		}
	}
	s, err := lg.Logs(ctx, r)
	if err != nil {
		send(logItem{err: err})
		return
	}
	defer s.Close()
	for {
		l, err := s.Next()
		if err != nil {
			send(logItem{err: err})
			return
		}
		if !send(logItem{line: l}) {
			return
		}
	}
}

// waitLogs waits for the next lines of stream id and takes all that are
// ready, so a log's start arrives in one message.
func waitLogs(id int, ch <-chan logItem) tea.Cmd {
	return func() tea.Msg {
		msg := logLinesMsg{id: id}
		it, ok := <-ch
		for {
			switch {
			case !ok:
				msg.end = true
				return msg
			case it.err != nil:
				msg.end = true
				if !errors.Is(it.err, io.EOF) {
					msg.err = it.err
				}
				return msg
			}
			msg.lines = append(msg.lines, it.line)
			if len(msg.lines) >= maxLogBatch {
				return msg
			}
			select {
			case it, ok = <-ch:
			default:
				return msg
			}
		}
	}
}

// startLogs shows the log of t and opens it, unless there is none to read.
func (m *Model) startLogs(t resource.LogTarget) tea.Cmd {
	m.stopLogs()
	m.logs.Reset(t)
	m.logStarted = true
	m.lkeys.set(t, m.logs.Wrap())
	if m.logView {
		m.footer.SetKeys(m.lkeys)
	}
	if !m.logs.Loading() {
		return nil // the container is waiting: the note says so
	}
	lg, ok := m.provider.(resource.Logger)
	if !ok {
		m.logs.Finish(errors.New("this data source has no logs"))
		return nil
	}
	m.logSeq++
	m.logID = m.logSeq
	ctx, cancel := context.WithCancel(m.pctx)
	ch := make(chan logItem, 256)
	go pump(ctx, lg, resource.LogRequest{Pod: t.Pod, Container: t.Container, Previous: t.Previous, Tail: logs.Tail}, ch)
	m.logCancel, m.logCh = cancel, ch
	return tea.Batch(waitLogs(m.logID, ch), m.tick())
}

// stopLogs closes the open log, if any; its lines stay.
func (m *Model) stopLogs() {
	if m.logCancel != nil {
		m.logCancel()
	}
	m.logCancel, m.logCh, m.logID = nil, nil, 0
}

// logLines adds the lines of the open log and waits for more.
func (m Model) logLines(msg logLinesMsg) (tea.Model, tea.Cmd) {
	if msg.id != m.logID || m.logCh == nil {
		return m, nil // a log closed meanwhile
	}
	if len(msg.lines) > 0 {
		m.logs.Append(msg.lines)
	}
	if msg.end {
		m.logs.Finish(msg.err)
		m.stopLogs()
		return m, nil
	}
	return m, waitLogs(msg.id, m.logCh)
}

// syncLogs follows the selection: the preview shows the log picked for it
// (P19), opened once the selection rested. In the log view the target
// stays, with the pod's latest state.
func (m *Model) syncLogs() tea.Cmd {
	cur := m.logs.Target()
	if m.logView {
		if cur.Pod != nil {
			if p := m.byPath[cur.Pod.Path]; p != nil && p != cur.Pod {
				cur.Pod = p
				m.logs.SetTarget(cur)
			}
		}
		return nil
	}
	t, ok := resource.PickLog(m.tree.Selected())
	m.setLogKey(ok)
	switch {
	case !ok:
		if cur.Pod != nil {
			m.stopLogs()
			m.logs.Clear()
			m.logStarted = false
		}
		return nil
	case t.Key() == cur.Key():
		m.logs.SetTarget(t) // the same log, maybe of a reloaded object
		return nil
	}
	m.logs.ShowPod(m.tree.Selected().Kind != resource.KindPod)
	m.stopLogs()
	m.logs.Reset(t)
	m.logStarted = false
	m.logDelay++
	seq := m.logDelay
	return tea.Batch(tea.Tick(logDelay, func(time.Time) tea.Msg { return logDelayMsg{seq} }), m.tick())
}

// setLogKey offers v if the selection has a log.
func (m *Model) setLogKey(on bool) {
	if m.keys.logs == on {
		return
	}
	m.keys.logs = on
	m.detail.SetActions(m.keys.Resource())
	m.footer.SetKeys(m.keys)
	m.layout()
}

// openLogs shows the log picked for the selection on the whole screen.
func (m Model) openLogs() (tea.Model, tea.Cmd) {
	t, ok := resource.PickLog(m.tree.Selected())
	if !ok {
		return m, nil
	}
	var cmd tea.Cmd
	if t.Key() != m.logs.Target().Key() || !m.logStarted {
		cmd = m.startLogs(t) // else the preview's log goes on
	}
	m.logView = true
	m.dragging = false
	m.logs.Open()
	m.logs.ClearSelection()
	m.syncCopyKey()
	m.lkeys.set(m.logs.Target(), m.logs.Wrap())
	m.footer.CloseHelp()
	m.footer.SetKeys(m.lkeys)
	m.layout()
	return m, cmd
}

// closeLogs goes back to the tree; the preview goes back to the picked log.
func (m Model) closeLogs() (tea.Model, tea.Cmd) {
	m.logView = false
	m.closeLogFilter()
	m.footer.CloseHelp()
	m.footer.SetKeys(m.keys)
	m.layout()
	return m, nil
}

// logKey handles a key in the log view.
func (m Model) logKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.logFilter.Focused() {
		return m.typeLogFilter(msg)
	}
	k := m.lkeys
	switch {
	case m.footer.HelpOpen() && msg.Type == tea.KeyEsc:
		m.footer.CloseHelp()
		m.layout()
	case m.logs.Selecting() && msg.Type == tea.KeyEsc:
		m.logs.ClearSelection()
		m.syncCopyKey()
	case m.logFiltering && msg.Type == tea.KeyEsc:
		m.closeLogFilter()
	case key.Matches(msg, k.Quit):
		return m, tea.Quit
	case key.Matches(msg, k.Back):
		return m.closeLogs()
	case key.Matches(msg, k.Help):
		m.footer.CycleHelp()
		m.layout()
	case key.Matches(msg, k.Up):
		m.logs.Scroll(-1)
	case key.Matches(msg, k.Down):
		m.logs.Scroll(1)
	case key.Matches(msg, k.PageUp):
		m.logs.PageUp()
	case key.Matches(msg, k.PageDown):
		m.logs.PageDown()
	case key.Matches(msg, k.Home):
		m.logs.Home()
	case key.Matches(msg, k.End):
		m.logs.End()
	case key.Matches(msg, k.Next):
		return m, m.cycleContainer(1)
	case key.Matches(msg, k.Prev):
		return m, m.cycleContainer(-1)
	case key.Matches(msg, k.Run):
		return m.toggleRun()
	case key.Matches(msg, k.Copy):
		return m.copyLog()
	case key.Matches(msg, k.Wrap):
		m.logs.ToggleWrap()
		m.lkeys.set(m.logs.Target(), m.logs.Wrap())
		m.footer.SetKeys(m.lkeys)
	case key.Matches(msg, k.Filter):
		m.logFiltering = true
		m.layout()
		return m, m.logFilter.Focus()
	}
	return m, nil
}

// cycleContainer shows the log of the pod's next (d=1) or previous (d=-1)
// container.
func (m *Model) cycleContainer(d int) tea.Cmd {
	t := m.logs.Target()
	if t.Pod == nil || t.Pod.Status == nil || len(t.Pod.Status.Containers) < 2 {
		return nil
	}
	cs := t.Pod.Status.Containers
	i := 0
	for j, c := range cs {
		if c.Name == t.Container {
			i = j
		}
	}
	return m.showContainer(cs[(i+d+len(cs))%len(cs)].Name)
}

func (m *Model) showContainer(name string) tea.Cmd {
	t := m.logs.Target()
	if t.Pod == nil || name == t.Container {
		return nil
	}
	return m.startLogs(resource.Target(t.Pod, name))
}

// toggleRun switches between the container's current and previous run.
func (m Model) toggleRun() (tea.Model, tea.Cmd) {
	t := m.logs.Target()
	c := t.ContainerInfo()
	if !c.HasPrevious() {
		if c == nil {
			return m, nil
		}
		cmd := m.footer.Flash(c.Name + " has no previous run")
		return m, cmd
	}
	t.Previous = !t.Previous
	return m, m.startLogs(t)
}

// typeLogFilter handles a key while the log filter has the focus.
func (m Model) typeLogFilter(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyEsc:
		m.closeLogFilter()
	case tea.KeyEnter:
		m.logFilter.Blur()
	case tea.KeyUp:
		m.logs.Scroll(-1)
	case tea.KeyDown:
		m.logs.Scroll(1)
	case tea.KeyPgUp:
		m.logs.PageUp()
	case tea.KeyPgDown:
		m.logs.PageDown()
	default:
		var cmd tea.Cmd
		m.logFilter, cmd = m.logFilter.Update(msg)
		m.logs.SetFilter(m.logFilter.Value())
		m.syncCopyKey()
		return m, cmd
	}
	return m, nil
}

func (m *Model) closeLogFilter() {
	m.logFiltering = false
	m.logFilter.Blur()
	m.logFilter.Reset()
	m.logs.SetFilter("")
	m.syncCopyKey()
	m.layout()
}

// logFilterView is the line above the footer while the log is filtered.
func (m Model) logFilterView() string {
	hint := "enter scroll • esc close"
	if !m.logFilter.Focused() {
		hint = "/ edit • esc close"
	}
	hint = theme.Fg(m.theme.Muted).Render(hint)
	m.logFilter.Width = max(m.width-len("enter scroll • esc close")-6, 1)
	return m.line(m.logFilter.View(), hint)
}

// logMouse: the wheel scrolls; dragging over lines selects them (a click:
// one line, shift+click extends), which c then copies: the clipboard is
// only written on a key (P20). A tab shows its container, the context's
// name goes back to the tree.
func (m Model) logMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		m.logs.Scroll(-3)
		return m, nil
	case tea.MouseButtonWheelDown:
		m.logs.Scroll(3)
		return m, nil
	}
	y := msg.Y - headerHeight // the row in the log's body
	switch {
	case m.dragging && msg.Action == tea.MouseActionMotion:
		m.logs.Drag(y)
		return m, nil
	case m.dragging && msg.Action == tea.MouseActionRelease:
		m.dragging = false
		m.logs.Drag(y)
		return m, nil
	}
	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return m, nil
	}
	if y >= 0 && y < m.logBodyHeight() {
		m.logs.Press(y, msg.Shift)
		m.dragging = m.logs.Selecting()
		m.syncCopyKey()
		return m, nil
	}
	if zone.Get(logBackZone).InBounds(msg) {
		return m.closeLogs()
	}
	if p := m.logs.Target().Pod; p != nil && p.Status != nil {
		for _, c := range p.Status.Containers {
			if zone.Get(logs.TabZone(logZones, c.Name)).InBounds(msg) {
				return m, m.showContainer(c.Name)
			}
		}
	}
	return m, nil
}

// logBodyHeight is the number of rows the log's lines have.
func (m Model) logBodyHeight() int {
	h := m.height - headerHeight - lipgloss.Height(m.footer.View())
	if m.logFiltering {
		h--
	}
	return h
}

// copyLog copies the selected lines, else every line shown (with a filter:
// those matching it).
func (m Model) copyLog() (tea.Model, tea.Cmd) {
	text, n, cut := m.logs.SelectedText()
	if !m.logs.Selecting() {
		text, n, cut = m.logs.ShownText()
	}
	m.syncCopyKey()
	if n == 0 {
		cmd := m.footer.Flash("nothing to copy")
		return m, cmd
	}
	what := fmt.Sprintf("copied %d lines", n)
	switch {
	case cut:
		what = fmt.Sprintf("copied the last %d lines, the most the clipboard takes", n)
	case n == 1:
		what = "copied 1 line"
	}
	return m, tea.Batch(m.copyToClipboard(text), m.footer.Flash(what))
}

// syncCopyKey says what c copies: the selection, or everything shown.
func (m *Model) syncCopyKey() {
	help := "copy all"
	if m.logs.Filter() != "" {
		help = "copy matches"
	}
	if m.logs.Selecting() {
		help = "copy selection"
	}
	if m.lkeys.Copy.Help().Desc != help {
		m.lkeys.Copy.SetHelp("c", help)
		if m.logView {
			m.footer.SetKeys(m.lkeys)
		}
	}
}

// logKeys are the keys of the log view.
type logKeys struct {
	Up, Down, PageUp, PageDown, Home, End key.Binding
	Next, Prev, Run, Wrap, Filter, Copy   key.Binding
	Back, Help, Quit                      key.Binding

	follow key.Binding // G in the bar: follow, or the last line of a log that ended
}

func defaultLogKeys() logKeys {
	return logKeys{
		Up:       key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down:     key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		PageUp:   key.NewBinding(key.WithKeys("pgup"), key.WithHelp("pgup", "page up")),
		PageDown: key.NewBinding(key.WithKeys("pgdown", " "), key.WithHelp("pgdn", "page down")),
		Home:     key.NewBinding(key.WithKeys("home", "g"), key.WithHelp("home/g", "first line")),
		End:      key.NewBinding(key.WithKeys("end", "G"), key.WithHelp("end/G", "last line, follow")),
		Next:     key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next container")),
		Prev:     key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("⇧tab", "previous container")),
		Run:      key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "previous run")),
		Wrap:     key.NewBinding(key.WithKeys("w"), key.WithHelp("w", "cut lines")),
		Filter:   key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
		Copy:     key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "copy all")),
		Back:     key.NewBinding(key.WithKeys("esc", "q", "backspace", "left", "h"), key.WithHelp("q/esc", "back")),
		Help:     key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Quit:     key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "quit")),
	}
}

// set adapts the keys to the log of t: tab only with more than one
// container, p only if the container has run before, w says what it does.
func (k *logKeys) set(t resource.LogTarget, wrap bool) {
	many := t.Pod != nil && t.Pod.Status != nil && len(t.Pod.Status.Containers) > 1
	k.Next.SetEnabled(many)
	k.Prev.SetEnabled(many)
	c := t.ContainerInfo()
	k.Run.SetEnabled(c.HasPrevious())
	run := "previous run"
	if t.Previous {
		run = "current run"
	}
	k.Run.SetHelp("p", run)
	k.follow = key.NewBinding(key.WithKeys("G"), key.WithHelp("G", "follow"))
	if t.Previous || c != nil && c.State == resource.ContainerTerminated {
		k.follow.SetHelp("G", "last line")
	}
	wrapHelp := "wrap lines"
	if wrap {
		wrapHelp = "cut lines"
	}
	k.Wrap.SetHelp("w", wrapHelp)
}

func (k logKeys) Navigation() []key.Binding {
	scroll := key.NewBinding(key.WithKeys("up"), key.WithHelp("↑↓/jk", "scroll"))
	return []key.Binding{scroll, k.follow, k.Next, k.Run}
}

func (k logKeys) Tools() []key.Binding     { return []key.Binding{k.Copy, k.Filter, k.Wrap} }
func (k logKeys) Resource() []key.Binding  { return nil }
func (k logKeys) Global() []key.Binding    { return []key.Binding{k.Help, k.Back} }
func (k logKeys) Highlight() []key.Binding { return nil }

func (k logKeys) HelpGroups() []footer.Group {
	help := key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "what words mean"))
	esc := key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "close"))
	back := key.NewBinding(key.WithKeys("q"), key.WithHelp("q/esc/⌫/←", "back to the tree"))
	drag := key.NewBinding(key.WithKeys("mouse"), key.WithHelp("drag", "select lines, copy"))
	extend := key.NewBinding(key.WithKeys("mouse"), key.WithHelp("⇧click", "extend the selection"))
	cp := key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "copy selection, else all shown"))
	return []footer.Group{
		{Title: "Scroll", Keys: []key.Binding{k.Up, k.Down, k.PageUp, k.PageDown, k.Home, k.End}},
		{Title: "Log", Keys: []key.Binding{k.Next, k.Prev, k.Run, k.Wrap, k.Filter}},
		{Title: "Copy", Keys: []key.Binding{drag, extend, cp}},
		{Title: "App", Keys: []key.Binding{help, esc, back, k.Quit}},
	}
}

func (k logKeys) LegendKeys() footer.Group {
	esc := key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "close"))
	return footer.Group{Title: "App", Keys: []key.Binding{esc, k.Quit}}
}
