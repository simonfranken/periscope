package theme

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"periscope/internal/resource"
)

// Healthy is the word for an object without issues.
const Healthy = "healthy"

// Completed is the word for a pod that ran to completion.
const Completed = "completed"

// IssueWord is the short label for an issue kind shown in the tree.
func IssueWord(k resource.IssueKind) string {
	switch k {
	case resource.IssueDown:
		return "down"
	case resource.IssueCrashLoop:
		return "crashloop"
	case resource.IssueOOMKilled:
		return "oom killed"
	case resource.IssuePullError:
		return "pull error"
	case resource.IssueFailed:
		return "failed"
	case resource.IssuePending:
		return "pending"
	case resource.IssueNotReady:
		return "not ready"
	case resource.IssueDegraded:
		return "degraded"
	case resource.IssueRestarting:
		return "restarting"
	case resource.IssueRollingOut:
		return "rolling out"
	case resource.IssueSuspended:
		return "suspended"
	default:
		return "terminating"
	}
}

// IssueText explains an issue in the detail pane.
func IssueText(is resource.Issue, s *resource.Status) string {
	in := ""
	if is.Container != "" {
		in = " (" + is.Container + ")"
	}
	switch is.Kind {
	case resource.IssueDown:
		return fmt.Sprintf("no replica ready, 0/%d", s.Desired)
	case resource.IssueCrashLoop:
		return "container keeps crashing" + in
	case resource.IssueOOMKilled:
		return "container ran out of memory" + in
	case resource.IssuePullError:
		return "image can't be pulled" + in
	case resource.IssueFailed:
		return "ran and failed"
	case resource.IssuePending:
		return "not scheduled or not started yet"
	case resource.IssueNotReady:
		return "running, but not ready" + in
	case resource.IssueDegraded:
		return fmt.Sprintf("only %d of %d replicas ready", s.Ready, s.Desired)
	case resource.IssueRestarting:
		return "restarted in the last hour" + in
	case resource.IssueRollingOut:
		return fmt.Sprintf("%d of %d replicas updated", s.Updated, s.Desired)
	case resource.IssueSuspended:
		return "schedule suspended, no new runs"
	default:
		return "namespace is being deleted"
	}
}

// QuietWord is the muted word for an object without issues.
func QuietWord(s *resource.Status) string {
	if s != nil && s.Phase == resource.PhaseSucceeded {
		return Completed
	}
	return Healthy
}

// StatusWord renders the state of o as one word: a spinner while loading,
// `error`, the most severe issue with "+n" for further ones, or a muted
// healthy word. base is applied to every segment, e.g. to keep a row
// background.
func (t Theme) StatusWord(o *resource.Object, base lipgloss.Style, spinner string) string {
	seg := func(c lipgloss.TerminalColor, text string) string { return base.Foreground(c).Render(text) }
	switch {
	case o.Err != nil:
		return t.LevelStyle(resource.LevelDanger, base).Render("error")
	case o.Status == nil:
		return seg(t.Primary, spinner)
	}
	if issues := o.Status.Issues(); len(issues) > 0 {
		out := t.LevelStyle(issues[0].Level, base).Render(IssueWord(issues[0].Kind))
		if len(issues) > 1 {
			out += seg(t.Muted, fmt.Sprintf(" +%d", len(issues)-1))
		}
		return out
	}
	return seg(t.Muted, QuietWord(o.Status))
}

// LevelStyle returns base with the text color (and weight) for a level.
func (t Theme) LevelStyle(l resource.Level, base lipgloss.Style) lipgloss.Style {
	switch l {
	case resource.LevelDanger:
		return base.Foreground(t.Danger).Bold(true)
	case resource.LevelWarn:
		return base.Foreground(t.Warn)
	case resource.LevelInfo:
		return base.Foreground(t.Info)
	default:
		return base.Foreground(t.Muted)
	}
}

// Ready is the tree's count column: ready/desired replicas of a workload,
// ready/total containers of a pod; "" for other objects.
func Ready(o *resource.Object) string {
	s := o.Status
	if s == nil {
		return ""
	}
	switch o.Kind {
	case resource.KindDeployment, resource.KindStatefulSet, resource.KindDaemonSet:
		return fmt.Sprintf("%d/%d", s.Ready, s.Desired)
	case resource.KindPod:
		ready := 0
		for _, c := range s.Containers {
			if c.Ready {
				ready++
			}
		}
		return fmt.Sprintf("%d/%d", ready, len(s.Containers))
	}
	return ""
}

// Restarts is the sum of the restarts of a pod's containers.
func Restarts(s *resource.Status) int {
	n := 0
	if s != nil {
		for _, c := range s.Containers {
			n += c.Restarts
		}
	}
	return n
}

// Age formats how long ago t was, like kubectl: "40s", "25m", "3h", "12d";
// "" for the zero time.
func Age(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", max(int(d.Seconds()), 0))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

// LevelCounts renders the issues of h as one count per level, most severe
// first, each in its level color and joined by sep: "1 error · 3 warnings ·
// 2 notes". Zero counts are left out; "" if there are none. base is applied
// to every segment, e.g. to keep a bar background.
func (t Theme) LevelCounts(h resource.Hidden, sep string, base lipgloss.Style) string {
	var parts []string
	add := func(l resource.Level, n int, one, many string) {
		if n == 1 {
			parts = append(parts, t.LevelStyle(l, base).Render("1 "+one))
		} else if n > 1 {
			parts = append(parts, t.LevelStyle(l, base).Render(fmt.Sprintf("%d %s", n, many)))
		}
	}
	add(resource.LevelDanger, h.Danger, "error", "errors")
	add(resource.LevelWarn, h.Warn, "warning", "warnings")
	add(resource.LevelInfo, h.Info, "note", "notes")
	return strings.Join(parts, base.Foreground(t.Muted).Render(sep))
}
