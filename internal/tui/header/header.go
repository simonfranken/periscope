// Package header renders the cluster summary line.
package header

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	zone "github.com/lrstanley/bubblezone"

	"periscope/internal/resource"
	"periscope/internal/tui/theme"
)

// View renders the context, the issue counts per level and what is still
// loading, on one line. The header stands for what the tree shows: the
// cluster, or scope (a namespace or section) with a breadcrumb
// "context › shop"; with selected it is drawn like a selected tree row.
// backZone marks the context's name inside a scope, a way back by mouse.
func View(t theme.Theme, c *resource.Cluster, scope *resource.Object, spinner string, width int, selected bool, backZone string) string {
	bar := lipgloss.NewStyle().Background(t.Bar)
	if selected {
		bar = lipgloss.NewStyle().Background(t.Selected).Bold(true)
	}
	seg := func(col lipgloss.TerminalColor, s string) string { return bar.Foreground(col).Render(s) }

	if c == nil {
		return bar.Width(width).Render(seg(t.Muted, " loading…"))
	}

	left := seg(t.Primary, " "+c.Context)
	head, objects := c.Root, c.Objects
	if scope != nil {
		head, objects = scope, scope.Children
		left = seg(t.Text, " ") + zone.Mark(backZone, seg(t.Primary, c.Context)) +
			seg(t.Muted, " "+t.Glyphs.Crumb+" ") + seg(t.Text, scope.Name)
	}
	if c.Version != "" {
		left += seg(t.Muted, "  "+c.Version)
	}
	// the own state word only if it says something: a quiet "healthy" next
	// to the counts of problems below would contradict them
	if r := head; r != nil && r.Kind != resource.KindSection && (r.Status == nil || r.Err != nil || len(r.Status.Issues()) > 0) {
		left += seg(t.Text, "  ") + t.StatusWord(r, bar, spinner)
	}

	// one count per level, like the tree's hidden counts; the counts per
	// issue kind are in the cluster's detail pane
	var namespaces, pods, loading int
	resource.Walk(objects, func(o *resource.Object) {
		switch o.Kind {
		case resource.KindNamespace:
			namespaces++
		case resource.KindPod:
			pods++
		}
		if o.Err == nil && o.Status == nil {
			loading++
		}
	})

	sep := seg(t.Muted, " · ")
	var parts []string
	if counts := t.LevelCounts((&resource.Object{Children: objects}).HiddenIssues(), " · ", bar); counts != "" {
		parts = append(parts, counts)
	}
	if loading > 0 {
		parts = append(parts, seg(t.Muted, fmt.Sprintf("%d loading", loading)))
	}
	if scope == nil {
		parts = append(parts, seg(t.Muted, fmt.Sprintf("%d namespaces · %d pods", namespaces, pods)))
	} else if pods > 0 {
		parts = append(parts, seg(t.Muted, fmt.Sprintf("%d pods", pods)))
	}
	right := strings.Join(parts, sep) + seg(t.Muted, " ")

	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 4 {
		// too narrow: the least important part, on the right, is cut
		return bar.Width(width).Render(ansi.Truncate(left+seg(t.Text, "   ")+right, width, "…"))
	}
	return left + bar.Render(fmt.Sprintf("%*s", gap, "")) + right
}
