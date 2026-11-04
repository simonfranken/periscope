// Package detail renders the information pane for the selected object.
package detail

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"periscope/internal/resource"
	"periscope/internal/tui/theme"
)

const labelWidth = 12 // "rolling out" + 1

// maxEvents is how many warning events the pane lists.
const maxEvents = 5

type Model struct {
	theme   theme.Theme
	width   int
	height  int
	spinner string
	help    help.Model
	actions []key.Binding
}

// paletteMinHeight is the pane height below which the palette is left out:
// the information is worth more than the hints.
const paletteMinHeight = 12

func New(t theme.Theme, actions []key.Binding) Model {
	return Model{theme: t, help: theme.NewHelp(t), actions: actions}
}

// PaletteVisible reports whether the palette of resource actions fits.
func (m Model) PaletteVisible() bool { return m.height >= paletteMinHeight }

// SetActions replaces the bindings of the palette.
func (m *Model) SetActions(actions []key.Binding) { m.actions = actions }

func (m *Model) SetSize(w, h int)        { m.width, m.height = w, h }
func (m *Model) SetSpinner(frame string) { m.spinner = frame }

// View renders o; c is the loaded cluster (nil while loading), which the
// cluster's own view needs for its summary.
func (m Model) View(o *resource.Object, c *resource.Cluster) string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}
	var lines []string
	if o != nil {
		lines = m.lines(o, c)
	}
	for i, l := range lines {
		lines[i] = ansi.Truncate(l, m.width, "…")
	}
	height := m.height
	var palette []string
	if m.PaletteVisible() && (o == nil || o.Kind != resource.KindSection) { // nothing to act on
		palette = m.palette()
		height -= len(palette) // the palette is at the bottom
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	body := lipgloss.NewStyle().Width(m.width).Height(height).Render(strings.Join(lines, "\n"))
	if len(palette) == 0 {
		return body
	}
	return body + "\n" + strings.Join(palette, "\n")
}

// palette lays the actions out on as many lines as the width needs, so no
// key is cut off.
func (m Model) palette() []string {
	m.help.Width = 0
	var out []string
	var cur []key.Binding
	for _, b := range m.actions {
		if len(cur) > 0 && lipgloss.Width(m.help.ShortHelpView(append(cur, b))) > m.width {
			out = append(out, m.help.ShortHelpView(cur))
			cur = nil
		}
		cur = append(cur, b)
	}
	if len(cur) > 0 {
		out = append(out, m.help.ShortHelpView(cur))
	}
	for i, l := range out {
		out[i] = ansi.Truncate(l, m.width, "…")
	}
	return out
}

func (m Model) lines(o *resource.Object, c *resource.Cluster) []string {
	t := m.theme
	muted, text := theme.Fg(t.Muted), theme.Fg(t.Text)
	label := func(l string) string { return muted.Width(labelWidth).Render(l) }
	plain := lipgloss.NewStyle()

	root := o.IsRoot() && c != nil
	where := o.TypeName()
	switch {
	case o.Kind == resource.KindSection && o.Namespace == "":
		where = "cluster"
	case o.Kind == resource.KindSection:
		where = o.Namespace
	case o.Namespace != "":
		where = o.Namespace + " · " + where
	}
	name := o.Name
	if root {
		name = c.Context
	}
	out := []string{muted.Render(where), text.Bold(true).Render(name), ""}
	if o.Kind == resource.KindSection {
		return append(out, m.sectionLines(o)...)
	}

	switch {
	case o.Err != nil:
		return append(out, t.LevelStyle(resource.LevelDanger, plain).Render("error")+" "+muted.Render(o.Err.Error()))
	case o.Status == nil:
		return append(out, theme.Fg(t.Primary).Render(m.spinner)+muted.Render(" loading…"))
	}
	st := o.Status

	// the age is right-aligned in the name's line
	if age := theme.Age(st.Created); age != "" {
		age = muted.Render("age " + age)
		if gap := m.width - ansi.StringWidth(name) - lipgloss.Width(age); gap >= 2 {
			out[1] += strings.Repeat(" ", gap) + age
		}
	}

	// one line per issue: the word, then what it means
	issues := st.Issues()
	for _, is := range issues {
		word := theme.IssueWord(is.Kind)
		pad := strings.Repeat(" ", max(labelWidth-ansi.StringWidth(word), 1))
		out = append(out, t.LevelStyle(is.Level, plain).Render(word)+pad+text.Render(theme.IssueText(is, st)))
	}
	// "nothing to do" only if nothing below needs attention either
	below := o.HiddenIssues()
	if root {
		below = hiddenIn(c.Objects)
	}
	if len(issues) == 0 && below.Total() == 0 {
		out = append(out, label(theme.QuietWord(o))+muted.Render(theme.QuietText(o)))
	}
	if len(out) > 3 {
		out = append(out, "")
	}

	if root {
		out = append(out, label("Server")+text.Render(c.Server))
		if c.Version != "" {
			out = append(out, label("Version")+text.Render(c.Version))
		}
		namespaces := 0
		for _, o := range c.Objects {
			if o.Kind == resource.KindNamespace {
				namespaces++
			}
		}
		summary := fmt.Sprintf("%d", namespaces)
		if parts := m.hiddenParts(below); parts != "" {
			summary += muted.Render(" · ") + parts
		}
		out = append(out, label("Namespaces")+summary)
		return append(out, m.issuesBelow(c.Objects)...)
	}

	if r := theme.Ready(o); r != "" && o.Kind != resource.KindPod {
		out = append(out, label("Ready")+text.Render(r)+muted.Render(fmt.Sprintf(" · %d updated", st.Updated)))
	}
	if st.Phase != "" {
		out = append(out, label("Phase")+text.Render(string(st.Phase)))
	}
	if st.Node != "" {
		out = append(out, label("Node")+text.Render(st.Node))
	}
	if n := len(o.Children); n > 0 {
		children := fmt.Sprintf("%d", n)
		if parts := m.hiddenParts(below); parts != "" {
			children += muted.Render(" · ") + parts + muted.Render(" below")
		}
		out = append(out, label(childLabel(o))+children)
	}
	// the counts per kind only for a namespace: a workload's own word already
	// sums up its pods, which the tree lists right below it
	if o.Kind == resource.KindNamespace {
		out = append(out, m.issuesBelow(o.Children)...)
	}

	if len(st.Conditions) > 0 {
		out = append(out, "", muted.Bold(true).Render("Conditions"))
		out = append(out, m.conditionLines(st, issues)...)
	}
	if len(st.Containers) > 0 {
		out = append(out, "", muted.Bold(true).Render("Containers"))
		out = append(out, m.containerLines(st, issues)...)
	}
	if len(st.Events) > 0 {
		out = append(out, "", muted.Bold(true).Render("Warnings"))
		for i, e := range st.Events {
			if i == maxEvents {
				out = append(out, muted.Render(fmt.Sprintf("… %d more", len(st.Events)-maxEvents)))
				break
			}
			count := ""
			if e.Count > 1 {
				count = fmt.Sprintf(" ×%d", e.Count)
			}
			out = append(out, muted.Width(5).Render(theme.Age(e.Last))+theme.Fg(t.Warn).Render(e.Reason)+muted.Render(count)+" "+text.Render(e.Message))
		}
	}
	return out
}

// containerLines lists a pod's containers: name, state, restarts, image. A
// container an issue is about takes that issue's color.
func (m Model) containerLines(st *resource.Status, issues []resource.Issue) []string {
	t := m.theme
	muted, text := theme.Fg(t.Muted), theme.Fg(t.Text)
	level := map[string]resource.Level{}
	for _, is := range issues {
		if is.Container != "" && is.Level > level[is.Container] {
			level[is.Container] = is.Level
		}
	}
	var out []string
	for _, c := range st.Containers {
		state := "running"
		switch {
		case c.State != resource.ContainerRunning && c.Reason != "":
			state = c.Reason
		case c.State == resource.ContainerTerminated:
			state = "terminated"
		case c.State == resource.ContainerWaiting:
			state = "waiting"
		case !c.Ready:
			state = "not ready"
		}
		style := muted
		if l, ok := level[c.Name]; ok {
			style = t.LevelStyle(l, lipgloss.NewStyle())
		}
		line := text.Render(ansi.Truncate(c.Name, labelWidth-1, "…")) + strings.Repeat(" ", max(labelWidth-ansi.StringWidth(c.Name), 1)) + style.Render(state)
		if c.Restarts > 0 {
			restarts := fmt.Sprintf(" · %d restarts", c.Restarts)
			if c.LastReason != "" {
				restarts += fmt.Sprintf(", last %s ago (%s)", theme.Age(c.LastExit), c.LastReason)
			}
			line += muted.Render(restarts)
		}
		out = append(out, line, strings.Repeat(" ", labelWidth)+muted.Render(c.Image))
	}
	return out
}

// conditionLines are a custom resource's conditions: type, status (in the
// color of the issue about it, muted otherwise) and reason; the message
// below, muted.
func (m Model) conditionLines(st *resource.Status, issues []resource.Issue) []string {
	t := m.theme
	muted, plain := theme.Fg(t.Muted), lipgloss.NewStyle()
	var out []string
	for _, c := range st.Conditions {
		style := muted
		if c.Type == "Ready" && c.Status == "False" {
			for _, is := range issues {
				if is.Kind == resource.IssueNotReady {
					style = t.LevelStyle(is.Level, plain)
				}
			}
		}
		line := muted.Width(labelWidth).Render(c.Type) + style.Render(c.Status)
		if c.Reason != "" {
			line += muted.Render(" · " + c.Reason)
		}
		out = append(out, line)
		if c.Message != "" {
			out = append(out, strings.Repeat(" ", labelWidth)+muted.Render(c.Message))
		}
	}
	return out
}

// sectionLines summarize a section: how many objects, the issues below
// them, and the objects per kind header.
func (m Model) sectionLines(o *resource.Object) []string {
	t := m.theme
	muted, text := theme.Fg(t.Muted), theme.Fg(t.Text)
	count := fmt.Sprintf("%d", len(o.Children))
	below := o.HiddenIssues()
	if parts := m.hiddenParts(below); parts != "" {
		count += muted.Render(" · ") + parts + muted.Render(" below")
	} else {
		count += muted.Render(" · no issues")
	}
	out := []string{muted.Width(labelWidth).Render("Objects") + count, "", muted.Bold(true).Render("Kinds")}
	var kinds []string
	perKind := map[string]int{}
	for _, c := range o.Children {
		if perKind[c.Group] == 0 {
			kinds = append(kinds, c.Group)
		}
		perKind[c.Group]++
	}
	for _, k := range kinds {
		out = append(out, text.Render(fmt.Sprintf("%3d ", perKind[k]))+muted.Render(k))
	}
	return append(out, m.issuesBelow(o.Children)...)
}

func childLabel(o *resource.Object) string {
	if o.Kind == resource.KindNamespace {
		return "Objects"
	}
	return "Pods"
}

func hiddenIn(roots []*resource.Object) resource.Hidden {
	return (&resource.Object{Children: roots}).HiddenIssues()
}

func (m Model) hiddenParts(h resource.Hidden) string {
	return m.theme.LevelCounts(h, ", ", lipgloss.NewStyle())
}

// issuesBelow is the "Issues below" section: the objects in roots and their
// descendants counted per issue kind, most severe first, one line each;
// nothing if there are no issues.
func (m Model) issuesBelow(roots []*resource.Object) []string {
	t := m.theme
	plain := lipgloss.NewStyle()
	errored := 0
	perKind := map[resource.IssueKind]int{}
	resource.Walk(roots, func(o *resource.Object) {
		if o.Err != nil {
			errored++
		}
		for _, is := range o.Status.Issues() {
			perKind[is.Kind]++
		}
	})
	line := func(l resource.Level, word string, n int) string {
		return t.LevelStyle(l, plain).Width(labelWidth).Render(word) + theme.Fg(t.Text).Render(fmt.Sprintf("%d", n))
	}
	out := []string{"", theme.Fg(t.Muted).Bold(true).Render("Issues below")}
	if errored > 0 {
		out = append(out, line(resource.LevelDanger, "error", errored))
	}
	for _, k := range resource.Kinds {
		if n := perKind[k]; n > 0 {
			out = append(out, line(k.Level(), theme.IssueWord(k), n))
		}
	}
	if len(out) == 2 {
		return nil
	}
	return out
}
