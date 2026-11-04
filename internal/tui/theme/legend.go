package theme

import "periscope/internal/resource"

// LegendRow is one word or symbol of the UI, rendered as it appears, and
// what it means.
type LegendRow struct{ Word, Meaning string }

// LegendGroup is a titled column of the legend.
type LegendGroup struct {
	Title string
	Rows  []LegendRow
}

// issueMeaning explains an issue word in a few plain words.
func issueMeaning(k resource.IssueKind) string {
	switch k {
	case resource.IssueDown:
		return "no replica ready"
	case resource.IssueCrashLoop:
		return "keeps crashing"
	case resource.IssueOOMKilled:
		return "out of memory"
	case resource.IssuePullError:
		return "image not pullable"
	case resource.IssueFailed:
		return "pod or job failed"
	case resource.IssuePending:
		return "not started yet"
	case resource.IssueNotReady:
		return "pod or resource not ready"
	case resource.IssueDegraded:
		return "some replicas ready"
	case resource.IssueRestarting:
		return "restarted in last hour"
	case resource.IssueRollingOut:
		return "replicas updating"
	case resource.IssueSuspended:
		return "schedule suspended"
	default:
		return "namespace being deleted"
	}
}

// Legend explains the words and symbols of the tree and the detail pane, in
// their colors: what the full help's second page shows. The status words are
// split into two columns, problems and information, so the page stays short.
func (t Theme) Legend() []LegendGroup {
	g := t.Glyphs
	muted := Fg(t.Muted)
	problems := LegendGroup{Title: "Problems"}
	other := LegendGroup{Title: "Status"}
	for _, k := range resource.Kinds {
		row := LegendRow{t.LevelStyle(k.Level(), Fg(t.Text)).Render(IssueWord(k)), issueMeaning(k)}
		if k.Level() == resource.LevelDanger {
			problems.Rows = append(problems.Rows, row)
		} else {
			other.Rows = append(other.Rows, row)
		}
	}
	other.Rows = append(other.Rows,
		LegendRow{muted.Render(Healthy), "nothing to do"},
		LegendRow{muted.Render(Completed), "pod ran to completion"},
		LegendRow{muted.Render(Unchecked), "no Ready condition to read"},
	)
	symbols := LegendGroup{Title: "Symbols", Rows: []LegendRow{
		{muted.Render("n/m"), "ready / desired"},
		{muted.Render("+n"), "n more issues"},
		{Fg(t.Warn).Render(g.Problems + "k"), "issues below, collapsed"},
		{muted.Render("[n]"), "children, collapsed"},
		{muted.Render("3d"), "age"},
	}}
	return []LegendGroup{problems, other, symbols}
}
