// Package logs holds the log of one container: the lines read so far, the
// full-screen view that scrolls, follows and filters them, and the preview
// at the bottom of the detail pane.
package logs

import (
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	zone "github.com/lrstanley/bubblezone"

	"periscope/internal/resource"
	"periscope/internal/tui/theme"
)

const (
	// Tail is how many lines a log starts with.
	Tail = 1000
	// maxLines is how many lines are kept; older ones are dropped.
	maxLines = 10000
	// timeWidth is the time column: "15:04:05 ".
	timeWidth = 9
)

// Model is the log of one target.
type Model struct {
	theme  theme.Theme
	target resource.LogTarget
	lines  []resource.LogLine
	levels []resource.Level

	loading bool   // before the first answer
	done    bool   // the log ended (a previous run, a terminated container)
	err     error  // the log couldn't be read
	note    string // why there is no log to read, e.g. the container is waiting to start

	width, height int
	offset        int  // the first visible row
	follow        bool // the view sticks to the end
	wrap          bool
	filter        string
	rows          []row // the wrapped rows of the lines that match the filter
	matching      int   // the lines that match the filter
	spinner       string
	showPod       bool // the preview names the pod: it was picked for a workload

	// selected is the range of selected lines, from anchor (where the drag
	// started) to end, indexes into lines; selecting is false without one.
	selecting   bool
	anchor, end int
}

// row is one screen row of the view: a part of line.
type row struct {
	line  int
	text  string
	first bool // the line's first row, which shows the time
}

func New(t theme.Theme) Model { return Model{theme: t, wrap: true, follow: true} }

// Target is the log shown.
func (m Model) Target() resource.LogTarget { return m.target }

// Loading reports whether the log is still being opened.
func (m Model) Loading() bool { return m.loading }

// Note explains why the target has no log to read: its container is
// waiting to start. "" if there is a log.
func Note(t resource.LogTarget) string {
	c := t.ContainerInfo()
	if c == nil || t.Previous || c.State != resource.ContainerWaiting {
		return ""
	}
	if c.Reason == "" {
		return c.Name + " hasn't started yet"
	}
	return fmt.Sprintf("%s is waiting to start (%s)", c.Name, c.Reason)
}

// Reset shows the log of t, empty until lines arrive. The filter stays.
func (m *Model) Reset(t resource.LogTarget) {
	m.target = t
	m.lines, m.levels, m.rows = nil, nil, nil
	m.err, m.done = nil, false
	m.note = Note(t)
	m.loading = m.note == "" && t.Pod != nil
	m.offset, m.follow, m.matching = 0, true, 0
	m.selecting = false
}

// SetTarget replaces the target with the same log of a newer object, e.g.
// after a reload; the lines stay.
func (m *Model) SetTarget(t resource.LogTarget) { m.target = t }

// ShowPod names the pod in the preview's title, when it was picked for a
// workload and is not the selection itself.
func (m *Model) ShowPod(on bool) { m.showPod = on }

// Clear shows no log.
func (m *Model) Clear() { m.Reset(resource.LogTarget{}) }

// Append adds lines read from the log.
func (m *Model) Append(lines []resource.LogLine) {
	m.loading = false
	for _, l := range lines {
		l.Text = clean(l.Text)
		m.lines = append(m.lines, l)
		m.levels = append(m.levels, resource.LogLevel(l.Text))
	}
	if n := len(m.lines) - maxLines; n > 0 {
		m.lines, m.levels = m.lines[n:], m.levels[n:]
		m.anchor, m.end = m.anchor-n, m.end-n
		if m.selecting && max(m.anchor, m.end) < 0 {
			m.selecting = false // its lines were dropped
		}
		m.anchor, m.end = max(m.anchor, 0), max(m.end, 0)
		m.rebuild()
		return
	}
	m.addRows(len(m.lines) - len(lines))
	m.settle()
}

// Finish marks the log as ended, with err if it couldn't be read.
func (m *Model) Finish(err error) {
	m.loading, m.done, m.err = false, true, err
}

// clean makes a line safe to draw: no escape sequences, no tabs, no
// control characters.
func clean(s string) string {
	s = strings.ReplaceAll(ansi.Strip(s), "\t", "    ")
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
}

// Open prepares the full-screen view: at the end, following, unfiltered.
func (m *Model) Open() {
	m.filter = ""
	m.follow = true
	m.rebuild()
}

func (m *Model) SetSize(w, h int) {
	resize := w != m.width
	m.width, m.height = w, h
	if resize {
		m.rebuild()
		return
	}
	m.settle()
}

func (m *Model) SetSpinner(frame string) { m.spinner = frame }

// Wrap reports whether long lines are wrapped.
func (m Model) Wrap() bool { return m.wrap }

// ToggleWrap wraps long lines, or cuts them; the end stays in view if
// followed, else the top line.
func (m *Model) ToggleWrap() {
	top := -1
	if m.offset < len(m.rows) {
		top = m.rows[m.offset].line
	}
	m.wrap = !m.wrap
	m.rebuild()
	if !m.follow && top >= 0 {
		for i, r := range m.rows {
			if r.line >= top {
				m.offset = i
				break
			}
		}
		m.settle()
	}
}

// Filter is the filter term, "" if none.
func (m Model) Filter() string { return m.filter }

// SetFilter shows only the lines that contain term, ignoring case.
func (m *Model) SetFilter(term string) {
	if term == m.filter {
		return
	}
	m.filter = term
	m.follow = true
	m.selecting = false
	m.rebuild()
}

// Following reports whether the view sticks to the end.
func (m Model) Following() bool { return m.follow }

// Scroll moves the view by n rows; at the end it follows again.
func (m *Model) Scroll(n int) {
	m.offset += n
	m.follow = false
	m.settle()
	if m.offset >= m.maxOffset() {
		m.follow = true
	}
}

func (m *Model) PageUp()   { m.Scroll(-max(m.height-1, 1)) }
func (m *Model) PageDown() { m.Scroll(max(m.height-1, 1)) }

// Home shows the first line.
func (m *Model) Home() {
	m.offset, m.follow = 0, false
	m.settle()
	m.follow = m.offset >= m.maxOffset()
}

// End shows the last line and follows the log.
func (m *Model) End() {
	m.follow = true
	m.settle()
}

func (m Model) maxOffset() int { return max(len(m.rows)-m.height, 0) }

// settle keeps the offset in range, at the end while following.
func (m *Model) settle() {
	if m.follow {
		m.offset = m.maxOffset()
	}
	m.offset = max(min(m.offset, m.maxOffset()), 0)
}

func (m *Model) rebuild() {
	m.rows, m.matching = nil, 0
	m.addRows(0)
	m.settle()
}

// addRows adds the rows of the lines from index from.
func (m *Model) addRows(from int) {
	w := m.textWidth()
	term := strings.ToLower(m.filter)
	for i := from; i < len(m.lines); i++ {
		text := m.lines[i].Text
		if term != "" && !strings.Contains(strings.ToLower(text), term) {
			continue
		}
		m.matching++
		parts := []string{text}
		if m.wrap && ansi.StringWidth(text) > w {
			parts = strings.Split(ansi.Wrap(text, w, ""), "\n")
		}
		for j, p := range parts {
			m.rows = append(m.rows, row{line: i, text: p, first: j == 0})
		}
	}
}

func (m Model) textWidth() int { return max(m.width-1-timeWidth, 1) }

// View renders the lines that fit, or why there are none.
func (m Model) View() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}
	out := make([]string, 0, m.height)
	if msg := m.empty(); msg != "" {
		out = append(out, " "+msg)
	}
	end := min(m.offset+m.height, len(m.rows))
	for _, r := range m.rows[min(m.offset, end):end] {
		out = append(out, m.renderRow(r, m.textWidth()))
	}
	for len(out) < m.height {
		out = append(out, "")
	}
	return lipgloss.NewStyle().Width(m.width).Render(strings.Join(out[:m.height], "\n"))
}

// empty says why there is nothing to show, "" if there is.
func (m Model) empty() string {
	t := m.theme
	muted := theme.Fg(t.Muted)
	switch {
	case m.note != "":
		return muted.Render(m.note)
	case m.err != nil:
		return t.LevelStyle(resource.LevelDanger, lipgloss.NewStyle()).Render("error") + " " + muted.Render(m.err.Error())
	case m.loading:
		return theme.Fg(t.Primary).Render(m.spinner) + muted.Render(" loading logs…")
	case len(m.lines) == 0 && m.done:
		return muted.Render("the log is empty")
	case len(m.lines) == 0:
		return muted.Render("no logs yet")
	case len(m.rows) == 0:
		return muted.Render("nothing matches")
	}
	return ""
}

func (m Model) renderRow(r row, w int) string {
	t := m.theme
	base := lipgloss.NewStyle()
	selected := m.isSelected(r.line)
	if selected {
		base = base.Background(t.Selected)
	}
	ts := base.Render(strings.Repeat(" ", timeWidth))
	if l := m.lines[r.line]; r.first && !l.Time.IsZero() {
		ts = base.Foreground(t.Muted).Render(l.Time.Local().Format("15:04:05")) + base.Render(" ")
	}
	text := r.text
	if !m.wrap {
		text = ansi.Truncate(text, w, "…")
	}
	style := t.LogStyle(m.levels[r.line])
	if selected {
		style = style.Background(t.Selected)
	}
	out := base.Render(" ") + ts + m.highlight(text, style)
	if selected { // the whole row, so the selection reads as lines
		out += base.Render(strings.Repeat(" ", max(m.width-lipgloss.Width(out), 0)))
	}
	return out
}

// highlight renders text in style, the parts matching the filter in the
// primary color.
func (m Model) highlight(text string, style lipgloss.Style) string {
	if m.filter == "" {
		return style.Render(text)
	}
	match := style.Foreground(m.theme.Primary).Bold(true)
	lower, term := strings.ToLower(text), strings.ToLower(m.filter)
	var b strings.Builder
	for {
		i := strings.Index(lower, term)
		if i < 0 || len(lower) != len(text) { // ToLower changed the length: no offsets to trust
			b.WriteString(style.Render(text))
			return b.String()
		}
		b.WriteString(style.Render(text[:i]) + match.Render(text[i:i+len(term)]))
		text, lower = text[i+len(term):], lower[i+len(term):]
	}
}

// TabZone is the mouse zone of the tab of container name, with the given
// prefix.
func TabZone(prefix, name string) string { return prefix + "logtab:" + name }

// Header is the top line of the full-screen view: the breadcrumb to the
// pod, the pod's containers as tabs, and the state of the log. backZone
// marks the context's name, a way back to the tree.
func (m Model) Header(context, zonePrefix, backZone string) string {
	t := m.theme
	bar := lipgloss.NewStyle().Background(t.Bar)
	seg := func(c lipgloss.TerminalColor, s string) string { return bar.Foreground(c).Render(s) }
	crumb := seg(t.Muted, " "+t.Glyphs.Crumb+" ")
	p := m.target.Pod
	if p == nil {
		return bar.Width(m.width).Render("")
	}
	left := seg(t.Text, " ") + zone.Mark(backZone, seg(t.Primary, context)) + crumb + seg(t.Text, p.Namespace) + crumb + seg(t.Text, p.Name) + seg(t.Text, "  ")
	if p.Status != nil {
		for _, c := range p.Status.Containers {
			tab := seg(t.Muted, " "+c.Name+" ")
			if c.Name == m.target.Container {
				tab = lipgloss.NewStyle().Background(t.Selected).Foreground(t.Text).Bold(true).Render(" " + c.Name + " ")
			}
			left += zone.Mark(TabZone(zonePrefix, c.Name), tab)
		}
	}
	right := m.state(bar) + seg(t.Text, " ")
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 2 {
		return bar.Width(m.width).Render(ansi.Truncate(left+seg(t.Text, "  ")+right, m.width, "…"))
	}
	return left + bar.Render(strings.Repeat(" ", gap)) + right
}

// state is the right end of the header: which run, whether it follows.
func (m Model) state(bar lipgloss.Style) string {
	t := m.theme
	seg := func(c lipgloss.TerminalColor, s string) string { return bar.Foreground(c).Render(s) }
	var parts []string
	if _, n, _ := m.SelectedText(); m.selecting {
		sel := fmt.Sprintf("%d lines selected", n)
		if n == 1 {
			sel = "1 line selected"
		}
		parts = append(parts, seg(t.Text, sel)+seg(t.Muted, " · c copies"))
	}
	if m.filter != "" && len(m.lines) > 0 {
		parts = append(parts, seg(t.Muted, fmt.Sprintf("%d of %d lines", m.matching, len(m.lines))))
	}
	c := m.target.ContainerInfo()
	if m.target.Previous && m.note == "" {
		s := bar.Foreground(t.Text).Bold(true).Render("previous run")
		if c != nil && !c.LastExit.IsZero() {
			s += seg(t.Muted, " · ended "+theme.Age(c.LastExit)+" ago")
			if c.LastReason != "" {
				s += seg(t.Muted, " ("+c.LastReason+")")
			}
		}
		parts = append(parts, s)
	}
	switch {
	case m.note != "":
	case m.err != nil:
		parts = append(parts, t.LevelStyle(resource.LevelDanger, bar).Render("error"))
	case m.loading:
		parts = append(parts, seg(t.Primary, m.spinner)+seg(t.Muted, " loading"))
	case m.target.Previous:
	case m.done:
		parts = append(parts, seg(t.Muted, "ended"))
	case m.follow:
		parts = append(parts, seg(t.Muted, "following"))
	default:
		parts = append(parts, seg(t.Text, "paused")+seg(t.Muted, " · G follows"))
	}
	return strings.Join(parts, seg(t.Muted, " · "))
}

// Preview renders the log in at most n lines of width w for the detail
// pane: a title, then the last lines, cut instead of wrapped. Filter and
// scrolling are the full-screen view's.
func (m Model) Preview(w, n int) []string {
	t := m.theme
	muted := theme.Fg(t.Muted)
	if m.target.Pod == nil || n < 2 {
		return nil
	}
	from := m.target.Container
	if m.showPod {
		from = m.target.Pod.Name + " · " + from
	}
	title := muted.Bold(true).Render("Logs") + muted.Render("  "+from)
	if m.target.Previous {
		title += muted.Render(" · previous run")
	}
	out := []string{ansi.Truncate(title, w, "…")}
	pm := m
	pm.filter, pm.wrap = "", false
	if msg := pm.empty(); msg != "" {
		return append(out, ansi.Truncate(msg, w, "…"))
	}
	for i := max(len(m.lines)-(n-1), 0); i < len(m.lines); i++ {
		r := pm.renderRow(row{line: i, text: m.lines[i].Text, first: true}, max(w-timeWidth, 1))
		out = append(out, strings.TrimPrefix(r, " "))
	}
	return out
}

// --- selection ---------------------------------------------------------------

// maxCopy is how much text a copy takes at most: terminals and tmux limit
// what OSC 52 may carry. Beyond it the last lines that fit are copied.
const maxCopy = 1 << 20

// lineAt is the line shown at row y of the view (0 is its first row). Below
// the last row it is the last line.
func (m Model) lineAt(y int) (int, bool) {
	if len(m.rows) == 0 {
		return 0, false
	}
	i := min(max(m.offset+y, 0), len(m.rows)-1)
	return m.rows[i].line, true
}

// Press starts a selection at row y, or extends the one there is to it.
func (m *Model) Press(y int, extend bool) {
	l, ok := m.lineAt(y)
	if !ok {
		return
	}
	m.follow = false // the lines stay put while they are selected
	if extend && m.selecting {
		m.end = l
		return
	}
	m.selecting, m.anchor, m.end = true, l, l
}

// Drag extends the selection to row y; above or below the view it scrolls.
func (m *Model) Drag(y int) {
	if !m.selecting {
		return
	}
	switch {
	case y < 0:
		m.Scroll(-1)
		y = 0
	case y >= m.height:
		m.Scroll(1)
		y = m.height - 1
	}
	m.follow = false
	if l, ok := m.lineAt(y); ok {
		m.end = l
	}
}

// Selecting reports whether lines are selected.
func (m Model) Selecting() bool { return m.selecting }

// ClearSelection removes the selection; it reports whether there was one.
func (m *Model) ClearSelection() bool {
	had := m.selecting
	m.selecting = false
	return had
}

func (m Model) isSelected(line int) bool {
	return m.selecting && line >= min(m.anchor, m.end) && line <= max(m.anchor, m.end)
}

// SelectedText is the selected lines that match the filter, as text to
// copy, and how many lines it has.
func (m Model) SelectedText() (string, int, bool) {
	if !m.selecting {
		return "", 0, false
	}
	return m.text(min(m.anchor, m.end), max(m.anchor, m.end))
}

// ShownText is every line the view shows (those matching the filter), as
// text to copy, and how many lines it has.
func (m Model) ShownText() (string, int, bool) { return m.text(0, len(m.lines)-1) }

// text is the lines from..to that match the filter, each with its time as
// shown, unwrapped; the last ones up to maxCopy. cut reports whether lines
// were left out for the limit.
func (m Model) text(from, to int) (s string, n int, cut bool) {
	term := strings.ToLower(m.filter)
	var out []string
	size := 0
	for i := min(to, len(m.lines)-1); i >= max(from, 0); i-- {
		l := m.lines[i]
		if term != "" && !strings.Contains(strings.ToLower(l.Text), term) {
			continue
		}
		line := l.Text
		if !l.Time.IsZero() {
			line = l.Time.Local().Format("15:04:05") + " " + line
		}
		if size+len(line)+1 > maxCopy {
			cut = true
			break
		}
		size += len(line) + 1
		out = append(out, line)
	}
	slices.Reverse(out)
	return strings.Join(out, "\n"), len(out), cut
}
