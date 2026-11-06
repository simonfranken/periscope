package theme

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"periscope/internal/resource"
)

// Healthy is the word for an object without issues.
const Healthy = "healthy"

// Completed is the word for a pod that ran to completion.
const Completed = "completed"

// Unchecked is the word for a custom resource without a Ready condition:
// periscope can't tell whether it works, so it doesn't say healthy.
const Unchecked = "unchecked"

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
	case resource.IssueBackendMissing:
		return "backend missing"
	case resource.IssuePending:
		return "pending"
	case resource.IssueNotReady:
		return "not ready"
	case resource.IssueDegraded:
		return "degraded"
	case resource.IssueNoEndpoints:
		return "no endpoints"
	case resource.IssueNoAddress:
		return "no address"
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
	case resource.IssueBackendMissing:
		var missing []string
		for _, b := range s.Backends {
			if b.Missing {
				missing = append(missing, b.Service)
			}
		}
		return "no Service " + strings.Join(missing, ", ")
	case resource.IssuePending:
		return "not scheduled or not started yet"
	case resource.IssueNotReady:
		if r := s.ReadyCondition(); is.Container == "" && r != nil {
			return "Ready condition is False" + reason(r.Reason)
		}
		return "running, but not ready" + in
	case resource.IssueDegraded:
		return fmt.Sprintf("only %d of %d replicas ready", s.Ready, s.Desired)
	case resource.IssueRestarting:
		return "restarted in the last hour" + in
	case resource.IssueNoEndpoints:
		return "no ready endpoint for " + s.Selector
	case resource.IssueNoAddress:
		if s.Rejected != "" {
			return "rejected by the router" + reason(s.Rejected)
		}
		return "no address yet, no controller or router admitted it"
	case resource.IssueRollingOut:
		return fmt.Sprintf("%d of %d replicas updated", s.Updated, s.Desired)
	case resource.IssueSuspended:
		return "schedule suspended, no new runs"
	default:
		return "namespace is being deleted"
	}
}

func reason(r string) string {
	if r == "" {
		return ""
	}
	return " (" + r + ")"
}

// maxFact is the longest muted fact, so a long host doesn't widen the
// status column at the names' expense.
const maxFact = 24

// QuietWord is the muted word for an object without issues: healthy, or a
// fact about it (F62) for the kinds that show one, e.g. "3 keys".
func QuietWord(o *resource.Object) string {
	if f := Fact(o); f != "" {
		return ansi.Truncate(f, maxFact, "…")
	}
	s := o.Status
	switch {
	case o.Kind == resource.KindCustom && s.ReadyCondition() == nil:
		return Unchecked
	case s != nil && s.Phase == resource.PhaseSucceeded:
		return Completed
	}
	return Healthy
}

// Fact is what the tree shows for a Service, Ingress, Route, ConfigMap or
// Secret without issues: its endpoints, its first host, its keys; "" for
// other kinds.
func Fact(o *resource.Object) string {
	s := o.Status
	if s == nil {
		return ""
	}
	switch o.Kind {
	case resource.KindService:
		switch {
		case s.Type == "ExternalName":
			return "external name"
		case s.Selector == "" && s.Endpoints == 0:
			return "no selector"
		}
		return count(s.Endpoints, "endpoint", "endpoints")
	case resource.KindIngress, resource.KindRoute:
		if len(s.Hosts) == 0 {
			return ""
		}
		if len(s.Hosts) > 1 {
			return fmt.Sprintf("%s +%d", s.Hosts[0], len(s.Hosts)-1)
		}
		return s.Hosts[0]
	case resource.KindConfigMap, resource.KindSecret:
		return count(len(s.Keys), "key", "keys")
	}
	return ""
}

func count(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

// QuietText explains the quiet word in the detail pane; "" for a fact,
// which explains itself and claims no check.
func QuietText(o *resource.Object) string {
	switch {
	case Fact(o) != "":
		return ""
	case QuietWord(o) == Unchecked:
		return "no Ready condition to read"
	}
	return "nothing to do"
}

// StatusWord renders the state of o as one word: a spinner while loading,
// `error`, the most severe issue with "+n" for further ones, or a muted
// healthy word; nothing for a section. base is applied to every segment,
// e.g. to keep a row background.
func (t Theme) StatusWord(o *resource.Object, base lipgloss.Style, spinner string) string {
	seg := func(c lipgloss.TerminalColor, text string) string { return base.Foreground(c).Render(text) }
	switch {
	case o.Kind == resource.KindSection:
		return "" // no state of its own; collapsed, the tree adds the counts below
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
	return seg(t.Muted, QuietWord(o))
}

// LevelStyle returns base with the text color (and weight) for a level.
// PodMarker renders the pod glyph of o in the color of its state: the
// level of its most severe issue, Danger if it failed to load, muted while
// healthy or loading (the status cell has the spinner).
func (t Theme) PodMarker(o *resource.Object, base lipgloss.Style) string {
	switch {
	case o.Err != nil:
		return t.LevelStyle(resource.LevelDanger, base).Render(t.Glyphs.Pod)
	case o.Status != nil:
		if issues := o.Status.Issues(); len(issues) > 0 {
			return t.LevelStyle(issues[0].Level, base).Render(t.Glyphs.Pod)
		}
	}
	return base.Foreground(t.Muted).Render(t.Glyphs.Pod)
}

// LogStyle is the style of a log line of level l: its level color, not
// bold, so a log with many errors stays readable.
func (t Theme) LogStyle(l resource.Level) lipgloss.Style {
	switch l {
	case resource.LevelDanger:
		return Fg(t.Danger)
	case resource.LevelWarn:
		return Fg(t.Warn)
	}
	return Fg(t.Text)
}

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
