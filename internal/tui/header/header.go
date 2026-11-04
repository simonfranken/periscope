// Package header renders the cluster summary line.
package header

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"periscope/internal/resource"
	"periscope/internal/tui/theme"
)

// View renders the context, the counts per issue kind and what is still
// loading, on one line. The header stands for the cluster: with selected it
// is drawn like a selected tree row.
func View(t theme.Theme, c *resource.Cluster, spinner string, width int, selected bool) string {
	bar := lipgloss.NewStyle().Background(t.Bar)
	if selected {
		bar = lipgloss.NewStyle().Background(t.Selected).Bold(true)
	}
	seg := func(col lipgloss.TerminalColor, s string) string { return bar.Foreground(col).Render(s) }

	if c == nil {
		return bar.Width(width).Render(seg(t.Muted, " loading…"))
	}

	left := seg(t.Primary, " "+c.Context)
	if c.Version != "" {
		left += seg(t.Muted, "  "+c.Version)
	}
	// the cluster's own state word only if it says something: a quiet
	// "healthy" next to the counts of problems below would contradict them
	if r := c.Root; r != nil && (r.Status == nil || r.Err != nil || len(r.Status.Issues()) > 0) {
		left += seg(t.Text, "  ") + t.StatusWord(r, bar, spinner)
	}

	// count objects per issue kind (most severe first)
	var namespaces, pods, loading, errored int
	perKind := map[resource.IssueKind]int{}
	resource.Walk(c.Namespaces, func(o *resource.Object) {
		switch o.Kind {
		case resource.KindNamespace:
			namespaces++
		case resource.KindPod:
			pods++
		}
		switch {
		case o.Err != nil:
			errored++
		case o.Status == nil:
			loading++
		}
		for _, is := range o.Status.Issues() {
			perKind[is.Kind]++
		}
	})

	sep := seg(t.Muted, " · ")
	var parts []string
	if errored > 0 {
		parts = append(parts, t.LevelStyle(resource.LevelDanger, bar).Render(fmt.Sprintf("%d error", errored)))
	}
	for _, k := range resource.Kinds {
		if n := perKind[k]; n > 0 {
			parts = append(parts, t.LevelStyle(k.Level(), bar).Render(fmt.Sprintf("%d %s", n, theme.IssueWord(k))))
		}
	}
	if loading > 0 {
		parts = append(parts, seg(t.Muted, fmt.Sprintf("%d loading", loading)))
	}
	parts = append(parts, seg(t.Muted, fmt.Sprintf("%d namespaces · %d pods", namespaces, pods)))
	right := strings.Join(parts, sep) + seg(t.Muted, " ")

	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 4 {
		// too narrow: the least important part, on the right, is cut
		return bar.Width(width).Render(ansi.Truncate(left+seg(t.Text, "   ")+right, width, "…"))
	}
	return left + bar.Render(fmt.Sprintf("%*s", gap, "")) + right
}
