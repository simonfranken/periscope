// Package tree implements the collapsible object tree view.
package tree

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	zone "github.com/lrstanley/bubblezone"

	"periscope/internal/resource"
	"periscope/internal/tui/theme"
)

const (
	statusWidth = 16 // minimum: "rolling out +1"; wider if a visible row needs it
	readyWidth  = 5  // "10/10"
	ageWidth    = 4  // "120d"
)

type Model struct {
	id    string // zone prefix, unique to this instance
	theme theme.Theme
	root  *resource.Object // the cluster: selectable, above the first row; may be nil
	roots []*resource.Object
	// scope is the top-level object (a namespace or a section) the tree shows
	// the contents of; nil at the cluster level, which lists roots only.
	scope    *resource.Object
	lastIn   map[string]string // per scope path: the path selected when it was left
	expanded map[string]bool   // keyed by Object.Path; kept when ancestors collapse
	filter   string            // lower-case; empty if not filtering
	// problemsOnly hides objects without issues; it combines with filter.
	problemsOnly bool
	cursor       *resource.Object
	offset       int // index of the first visible row
	width        int
	height       int
	spinner      string
}

func New(t theme.Theme) Model {
	return Model{id: zone.NewPrefix(), theme: t, expanded: map[string]bool{}, lastIn: map[string]string{}}
}

// SetRoot sets the cluster, which can be selected like a row above the
// first one. Call it before SetRoots / ReplaceRoots.
func (m *Model) SetRoot(root *resource.Object) { m.root = root }

// SetRoots replaces the tree and shows the cluster level. All nodes start
// collapsed. The cluster is selected if there is one, else the first
// resource.
func (m *Model) SetRoots(roots []*resource.Object) {
	m.roots = roots
	m.scope = nil
	m.lastIn = map[string]string{}
	m.expanded = map[string]bool{}
	m.cursor = nil
	m.offset = 0
	if m.root != nil {
		m.cursor = m.root
		return
	}
	m.cursor = m.firstNode()
}

// ReplaceRoots swaps in a reloaded tree. Unlike SetRoots it keeps the
// level, the expansion state and the cursor (by path); if the selected
// object is gone, the cursor moves to the first one. If the scope is gone,
// the cluster level is shown.
func (m *Model) ReplaceRoots(roots []*resource.Object) {
	var path string
	if m.cursor != nil {
		path = m.cursor.Path
	}
	wasHead := m.onRoot()
	wasRoot := m.cursor.IsRoot() // by path: SetRoot may have installed a new node
	m.roots = roots
	if m.scope != nil {
		m.scope = m.topLevel(m.scope.Path)
	}
	m.cursor = nil
	switch {
	case wasRoot && m.root != nil:
		m.cursor = m.root
	case wasHead && m.scope != nil:
		m.cursor = m.scope
	}
	for _, r := range m.rows() {
		if m.cursor == nil && r.kind == rowNode && r.node.Path == path {
			m.cursor = r.node
			break
		}
	}
	if m.cursor == nil {
		m.reselect(nil)
	}
	m.clampOffset()
}

func (m *Model) SetSize(w, h int) {
	m.width, m.height = w, h
	m.clampOffset()
}

// SetSpinner sets the frame drawn for objects that are still loading.
func (m *Model) SetSpinner(frame string) { m.spinner = frame }

// Selected returns the object under the cursor, or nil. It is the
// cluster (empty Path), or the scope inside a namespace, while the header
// is selected.
func (m Model) Selected() *resource.Object { return m.cursor }

// RootSelected reports whether the header is selected.
func (m Model) RootSelected() bool { return m.onRoot() }

// Scope returns the namespace or section whose contents are shown, nil at
// the cluster level.
func (m Model) Scope() *resource.Object { return m.scope }

// head is the object the header stands for: the scope, or the cluster.
func (m Model) head() *resource.Object {
	if m.scope != nil {
		return m.scope
	}
	return m.root
}

func (m Model) onRoot() bool { return m.cursor != nil && m.cursor == m.head() }

// level is the objects listed at the top of the tree: the scope's children,
// or the top level.
func (m Model) level() []*resource.Object {
	if m.scope != nil {
		return m.scope.Children
	}
	return m.roots
}

// topLevel returns the top-level object with the given path, or nil.
func (m Model) topLevel(path string) *resource.Object {
	for _, o := range m.roots {
		if o.Path == path {
			return o
		}
	}
	return nil
}

// Enter shows the contents of the top-level object o (a namespace or a
// section) without it, selecting what was selected when it was last left,
// else its first object. The filter is cleared: it applies to one level.
func (m *Model) Enter(o *resource.Object) {
	if o == nil || len(o.Children) == 0 {
		return
	}
	m.scope = o
	m.filter = ""
	m.cursor = nil
	m.offset = 0
	if p, ok := m.lastIn[o.Path]; ok {
		m.Select(p)
		if p == o.Path {
			m.cursor = o
		}
	}
	if m.cursor == nil {
		m.cursor = m.firstNode()
	}
	if m.cursor == nil {
		m.cursor = o
	}
	m.clampOffset()
}

// EnterPath enters the top-level object with the given path and reports
// whether there is one.
func (m *Model) EnterPath(path string) bool {
	o := m.topLevel(path)
	if o == nil || len(o.Children) == 0 {
		return false
	}
	m.Enter(o)
	return true
}

// Back returns to the cluster level from inside a namespace or section,
// which becomes the selection. The filter is cleared.
func (m *Model) Back() {
	if m.scope == nil {
		return
	}
	if m.cursor != nil {
		m.lastIn[m.scope.Path] = m.cursor.Path
	}
	m.cursor = m.scope
	m.scope = nil
	m.filter = ""
	m.offset = 0
	m.clampOffset()
}

// RootZone is the zone mark of the header; a click on it selects what the
// header stands for. The caller wraps the header in it.
func (m Model) RootZone() string { return m.id + "root" }

// BackZone is the zone mark of the cluster's name in the header's
// breadcrumb; inside a namespace, a click on it goes back to the cluster
// level. The caller wraps the name in it.
func (m Model) BackZone() string { return m.id + "back" }

// --- navigation --------------------------------------------------------------

// Up selects the previous object row.
func (m *Model) Up() { m.moveBy(-1) }

// Down selects the next object row.
func (m *Model) Down() { m.moveBy(1) }

// PageUp and PageDown move by one screen height.
func (m *Model) PageUp()   { m.moveBy(-max(m.height-1, 1)) }
func (m *Model) PageDown() { m.moveBy(max(m.height-1, 1)) }

// Home and End select the first and last visible resource.
func (m *Model) Home() { m.moveBy(-len(m.rows())) }
func (m *Model) End()  { m.moveBy(len(m.rows())) }

// atClusterLevel reports whether the tree lists the top level, whose
// objects are entered, not expanded.
func (m Model) atClusterLevel() bool { return m.scope == nil }

// Toggle expands or collapses the selected resource; at the cluster level it
// enters it.
func (m *Model) Toggle() {
	switch {
	case m.cursor == nil || m.onRoot():
	case m.atClusterLevel():
		m.Enter(m.cursor)
	case len(m.cursor.Children) > 0:
		m.setExpanded(m.cursor, !m.expanded[m.cursor.Path])
	}
}

// Right expands the selected object and selects its first child; at the
// cluster level it enters it.
func (m *Model) Right() {
	switch {
	case m.onRoot():
		m.Down()
	case m.cursor == nil || len(m.cursor.Children) == 0:
	case m.atClusterLevel():
		m.Enter(m.cursor)
	default:
		m.setExpanded(m.cursor, true)
		m.Down() // children directly follow their parent
	}
}

// Left collapses the selected object if it is expanded; otherwise it
// collapses the parent and selects it. From the top of a namespace (or its
// header) it goes back to the cluster level; at the cluster level it selects
// the header.
func (m *Model) Left() {
	switch {
	case m.cursor == nil:
	case m.scope != nil && (m.onRoot() || m.cursor.Parent == m.scope && !m.isExpanded(m.cursor)):
		m.Back()
	case m.onRoot():
	case !m.atClusterLevel() && m.expanded[m.cursor.Path] && len(m.cursor.Children) > 0:
		m.setExpanded(m.cursor, false)
	case m.cursor.Parent != nil && m.cursor.Parent != m.scope:
		m.setExpanded(m.cursor.Parent, false)
	case m.head() != nil:
		m.cursor = m.head() // the header is the parent of the top level
		m.offset = 0
	}
}

// ExpandBelow expands the selection and everything below it; siblings and
// the selection stay. On the header inside a namespace it expands the whole
// namespace; at the cluster level it enters the selection with everything
// expanded.
func (m *Model) ExpandBelow() {
	s := m.cursor
	switch {
	case s == nil:
		return
	case m.atClusterLevel() && m.onRoot():
		return // the cluster level is a list: nothing to expand
	case m.atClusterLevel():
		if len(s.Children) == 0 {
			return
		}
		m.expandAll(s.Children)
		m.Enter(s)
		return
	}
	if !m.onRoot() && len(s.Children) > 0 {
		m.expanded[s.Path] = true
	}
	m.expandAll(s.Children)
	m.clampOffset()
}

func (m *Model) expandAll(objs []*resource.Object) {
	resource.Walk(objs, func(s *resource.Object) {
		if len(s.Children) > 0 {
			m.expanded[s.Path] = true
		}
	})
}

// CollapseBelow collapses the selection and everything below it, and
// forgets what was expanded there; siblings stay. With nothing to collapse
// (a leaf or a collapsed object) it acts on the parent and selects it, so
// repeating it climbs up: to the header, which collapses the namespace, and
// then back to the cluster level. On the cluster it forgets everything that
// was expanded anywhere.
func (m *Model) CollapseBelow() {
	s := m.cursor
	switch {
	case s == nil:
		return
	case m.scope != nil && m.onRoot():
		if !m.anyExpanded(m.scope.Children) {
			m.Back()
			return
		}
	case m.atClusterLevel() && !m.onRoot():
		if m.root == nil {
			m.expanded = map[string]bool{} // no cluster row: forget everything
			return
		}
		s = m.root // the cluster is the parent of the top level
		m.cursor = s
	case !m.atClusterLevel() && !m.expanded[s.Path]:
		s = s.Parent // the scope for a top-level object, so the header
		m.cursor = s
	}
	switch s {
	case m.root:
		m.expanded = map[string]bool{}
	default:
		if s != m.scope {
			delete(m.expanded, s.Path)
		}
		resource.Walk(s.Children, func(c *resource.Object) { delete(m.expanded, c.Path) })
	}
	m.clampOffset()
}

func (m *Model) anyExpanded(objs []*resource.Object) bool {
	any := false
	resource.Walk(objs, func(s *resource.Object) { any = any || m.expanded[s.Path] })
	return any
}

// Select selects the object with the given path if it is visible.
func (m *Model) Select(path string) {
	for _, r := range m.rows() {
		if r.kind == rowNode && r.node.Path == path {
			m.cursor = r.node
			m.clampOffset()
			return
		}
	}
}

// setExpanded changes the expansion of s. If collapsing hides the cursor,
// s becomes the selection.
func (m *Model) setExpanded(s *resource.Object, open bool) {
	m.expanded[s.Path] = open
	if !open && isAncestor(s, m.cursor) {
		m.cursor = s
	}
	m.clampOffset()
}

func isAncestor(a, s *resource.Object) bool {
	for s != nil {
		s = s.Parent
		if s == a {
			return true
		}
	}
	return false
}

// moveBy moves the cursor by n object rows, skipping directory headers
// and stopping at the ends. The cluster counts as a row before the
// first one.
func (m *Model) moveBy(n int) {
	rows := m.rows()
	i := m.cursorRow(rows)
	if m.onRoot() {
		i = -1
	} else if i < 0 {
		return
	}
	step := 1
	if n < 0 {
		step, n = -1, -n
	}
	for j := i + step; n > 0 && j >= -1 && j < len(rows); j += step {
		switch {
		case j == -1:
			if m.head() != nil {
				i = -1
				n--
			}
		case rows[j].kind == rowNode:
			i = j
			n--
		}
	}
	if i < 0 {
		m.cursor = m.head()
	} else {
		m.cursor = rows[i].node
	}
	m.clampOffset()
}

func (m Model) cursorRow(rows []row) int {
	for i, r := range rows {
		if r.kind == rowNode && r.node == m.cursor {
			return i
		}
	}
	return -1
}

func (m *Model) firstNode() *resource.Object {
	for _, r := range m.rows() {
		if r.kind == rowNode {
			return r.node
		}
	}
	return nil
}

func (m Model) firstNodeRow() int {
	for i, r := range m.rows() {
		if r.kind == rowNode {
			return i
		}
	}
	return 0
}

// clampOffset scrolls so the cursor is visible. When the cursor is the first
// object, headers above it are shown too.
func (m *Model) clampOffset() {
	if m.height <= 0 {
		return
	}
	if m.onRoot() {
		m.offset = 0
		return
	}
	rows := m.rows()
	i := m.cursorRow(rows)
	if i < 0 {
		return
	}
	if i == m.firstNodeRow() {
		i = 0
	}
	if i < m.offset {
		m.offset = i
	}
	if i >= m.offset+m.height {
		m.offset = i - m.height + 1
	}
	m.offset = max(0, min(m.offset, len(rows)-m.height))
}

// --- mouse -------------------------------------------------------------------

// Update handles mouse input. Keyboard input is mapped by the parent model.
func (m *Model) Update(msg tea.Msg) {
	mm, ok := msg.(tea.MouseMsg)
	if !ok {
		return
	}
	switch mm.Button {
	case tea.MouseButtonWheelUp:
		m.Up()
		return
	case tea.MouseButtonWheelDown:
		m.Down()
		return
	}
	if mm.Action != tea.MouseActionPress || mm.Button != tea.MouseButtonLeft {
		return
	}
	if m.scope != nil && zone.Get(m.BackZone()).InBounds(mm) {
		m.Back()
		return
	}
	if m.head() != nil && zone.Get(m.RootZone()).InBounds(mm) {
		m.cursor = m.head()
		m.offset = 0
		return
	}
	for _, r := range m.visibleRows() {
		if r.kind != rowNode {
			continue
		}
		if zone.Get(m.id + "toggle:" + r.node.Path).InBounds(mm) {
			m.cursor = r.node
			m.Toggle()
			return
		}
		if zone.Get(m.id + "row:" + r.node.Path).InBounds(mm) {
			m.cursor = r.node
			m.clampOffset()
			return
		}
	}
}

func (m Model) visibleRows() []row {
	rows := m.rows()
	end := min(len(rows), m.offset+m.height)
	if m.offset >= end {
		return nil
	}
	return rows[m.offset:end]
}

// --- view --------------------------------------------------------------------

func (m Model) View() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}
	rows := m.visibleRows()
	statusColW := m.statusColumnWidth(rows)
	nameColW := m.nameColumnWidth(rows, statusColW)
	lines := make([]string, 0, m.height)
	for _, r := range rows {
		if r.kind == rowDir {
			lines = append(lines, m.dirLine(r))
		} else {
			lines = append(lines, m.nodeLine(r, nameColW, statusColW))
		}
	}
	if len(rows) == 0 && m.narrowed() {
		none := " nothing matches"
		if m.filter == "" {
			none = " no problems"
		}
		lines = append(lines, theme.Fg(m.theme.Muted).Width(m.width).Render(none))
	}
	for len(lines) < m.height {
		lines = append(lines, strings.Repeat(" ", m.width))
	}
	return strings.Join(lines, "\n")
}

// nameColumnWidth is the width of indent + toggle + name for the widest
// visible row, so the status column sits close to the names. It shrinks to
// fit the available width.
func (m Model) nameColumnWidth(rows []row, statusColW int) int {
	w := 0
	for _, r := range rows {
		if r.kind == rowNode {
			w = max(w, 2*r.depth+3+lipgloss.Width(r.node.Name)+len(m.childCount(r.node)))
		}
	}
	return min(w+2, m.width-statusColW-readyWidth-ageWidth-3)
}

// statusColumnWidth is the width of the widest status of the visible rows,
// at least statusWidth, so a word with "+n" and the hidden counts is never
// cut. The name column gives way instead.
func (m Model) statusColumnWidth(rows []row) int {
	w := statusWidth
	for _, r := range rows {
		if r.kind == rowNode {
			w = max(w, lipgloss.Width(m.status(r.node, lipgloss.NewStyle())))
		}
	}
	return w
}

func (m Model) childCount(s *resource.Object) string {
	if len(s.Children) == 0 || m.isExpanded(s) {
		return ""
	}
	return fmt.Sprintf(" [%d]", len(s.Children))
}

func (m Model) dirLine(r row) string {
	text := strings.Repeat("  ", r.depth) + " " + r.label
	return theme.Fg(m.theme.Muted).Width(m.width).Render(ansi.Truncate(text, m.width, "…"))
}

// nodeLine renders: indent ▸ name [n] │ status ·k │ ready │ age
func (m Model) nodeLine(r row, nameColW, statusColW int) string {
	t, s := m.theme, r.node
	base := lipgloss.NewStyle()
	if s == m.cursor {
		base = base.Background(t.Selected).Bold(true)
	}
	seg := func(c lipgloss.TerminalColor, text string) string {
		return base.Foreground(c).Render(text)
	}
	pad := func(n int) string { return base.Render(strings.Repeat(" ", max(n, 0))) }

	collapsed := len(s.Children) > 0 && !m.isExpanded(s)

	// toggle marker
	marker := "  "
	if len(s.Children) > 0 {
		marker = t.Glyphs.Expanded + " "
		if collapsed {
			marker = t.Glyphs.Collapsed + " "
		}
	}
	indent := pad(len("  ") * r.depth)
	toggle := zone.Mark(m.id+"toggle:"+s.Path, seg(t.Muted, " "+marker))

	// Rows without issues are drawn muted so problems stand out.
	issues := s.Status.Issues()
	quiet := s.Err == nil && s.Status != nil && len(issues) == 0
	textColor := t.Text
	if quiet && s != m.cursor {
		textColor = t.Muted
	}

	// name + child count
	nameW := nameColW - 2*r.depth - 3
	count := m.childCount(s)
	name := ansi.Truncate(s.Name, max(nameW-len(count), 1), "…")
	nameCell := seg(textColor, name) + seg(t.Muted, count) + pad(nameW-lipgloss.Width(name+count))

	status := m.status(s, base)
	status = ansi.Truncate(status, statusColW, "")
	status += pad(statusColW - lipgloss.Width(status))

	// ready count and age
	ready := theme.Ready(s)
	readyCell := pad(readyWidth-lipgloss.Width(ready)) + seg(textColor, ready)
	age := ""
	if s.Status != nil {
		age = theme.Age(s.Status.Created)
	}
	ageCell := pad(ageWidth-lipgloss.Width(age)) + seg(t.Muted, age)

	content := nameCell + pad(1) + status + pad(1) + readyCell + pad(1) + ageCell
	line := indent + toggle + zone.Mark(m.id+"row:"+s.Path, content)
	if w := lipgloss.Width(line); w < m.width {
		line += pad(m.width - w)
	}
	return ansi.Truncate(line, m.width, "")
}

// status renders the status cell of s on base: the most severe issue as a
// word, "+n" for further issues, a muted "healthy" without issues, so an
// empty cell never looks broken; a collapsed row adds the issues hidden
// below it, per level, and then drops "healthy".
func (m Model) status(s *resource.Object, base lipgloss.Style) string {
	t := m.theme
	pad := func(n int) string { return base.Render(strings.Repeat(" ", max(n, 0))) }
	collapsed := len(s.Children) > 0 && !m.isExpanded(s)
	quiet := s.Err == nil && s.Status != nil && len(s.Status.Issues()) == 0
	status := t.StatusWord(s, base, m.spinner)
	h := s.HiddenIssues()
	if quiet && collapsed && h.Total() > 0 {
		status = "" // "healthy ·3" would read as if everything below were fine
	}
	if collapsed && h.Total() > 0 {
		for _, c := range []struct {
			level resource.Level
			n     int
		}{{resource.LevelDanger, h.Danger}, {resource.LevelWarn, h.Warn}, {resource.LevelInfo, h.Info}} {
			if c.n == 0 {
				continue
			}
			if status != "" {
				status += pad(1)
			}
			status += t.LevelStyle(c.level, base).Render(fmt.Sprintf("%s%d", t.Glyphs.Problems, c.n))
		}
	}
	return status
}
