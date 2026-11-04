package resource

import (
	"slices"
	"time"
)

// Level is how much attention an issue needs.
type Level int

const (
	LevelQuiet Level = iota
	LevelInfo
	LevelWarn
	LevelDanger
)

// IssueKind is a problem of an object. The constants are ordered by
// severity, like Kinds.
type IssueKind int

const (
	IssueDown        IssueKind = iota // a workload with no ready replica
	IssueCrashLoop                    // a container restarts over and over
	IssueOOMKilled                    // a container ran out of memory
	IssuePullError                    // an image can't be pulled
	IssueFailed                       // a pod or job failed
	IssuePending                      // a pod is not scheduled or not started
	IssueNotReady                     // a running pod with a container not ready, or a custom resource whose Ready is False
	IssueDegraded                     // a workload with fewer ready replicas than desired
	IssueRestarting                   // a container restarted recently
	IssueRollingOut                   // a workload is updating its replicas
	IssueSuspended                    // a CronJob or Job is suspended
	IssueTerminating                  // a namespace is being deleted
)

// Kinds lists every issue kind, most severe first.
var Kinds = []IssueKind{
	IssueDown, IssueCrashLoop, IssueOOMKilled, IssuePullError, IssueFailed,
	IssuePending, IssueNotReady, IssueDegraded, IssueRestarting,
	IssueRollingOut, IssueSuspended, IssueTerminating,
}

// Level is the level of an issue kind.
func (k IssueKind) Level() Level {
	switch k {
	case IssueDown, IssueCrashLoop, IssueOOMKilled, IssuePullError, IssueFailed:
		return LevelDanger
	case IssuePending, IssueNotReady, IssueDegraded, IssueRestarting:
		return LevelWarn
	default:
		return LevelInfo
	}
}

// Issue is one problem of an object.
type Issue struct {
	Kind  IssueKind
	Level Level
	// Container names the container it is about, "" if it is about the object.
	Container string
}

// RecentRestart is how long a container's last exit counts as recent.
const RecentRestart = time.Hour

// now is replaced in tests.
var now = time.Now

// Issues summarizes the status into issues, most severe first. Each kind
// appears at most once, with the first container it applies to.
func (s *Status) Issues() []Issue {
	if s == nil {
		return nil
	}
	seen := map[IssueKind]bool{}
	var out []Issue
	add := func(k IssueKind, container string) {
		if !seen[k] {
			seen[k] = true
			out = append(out, Issue{Kind: k, Level: k.Level(), Container: container})
		}
	}

	if s.Terminating {
		add(IssueTerminating, "")
	}
	if s.Suspended {
		add(IssueSuspended, "")
	}
	if s.Desired > 0 {
		switch {
		case s.Ready == 0:
			add(IssueDown, "")
		case s.Ready < s.Desired:
			add(IssueDegraded, "")
		}
		if s.Updated < s.Desired {
			add(IssueRollingOut, "")
		}
	}

	for _, c := range s.Containers {
		switch c.Reason {
		case "CrashLoopBackOff":
			add(IssueCrashLoop, c.Name)
		case "ImagePullBackOff", "ErrImagePull":
			add(IssuePullError, c.Name)
		}
		recent := c.Restarts > 0 && now().Sub(c.LastExit) < RecentRestart
		switch {
		case recent && c.LastReason == "OOMKilled":
			add(IssueOOMKilled, c.Name)
		case recent:
			add(IssueRestarting, c.Name)
		}
	}
	switch s.Phase {
	case PhaseFailed:
		add(IssueFailed, "")
	case PhasePending:
		add(IssuePending, "")
	case PhaseRunning:
		for _, c := range s.Containers {
			if !c.Ready && c.State == ContainerRunning {
				add(IssueNotReady, c.Name)
				break
			}
		}
	}

	if r := s.ReadyCondition(); r != nil && r.Status == "False" {
		add(IssueNotReady, "")
	}

	slices.SortStableFunc(out, func(a, b Issue) int { return int(a.Kind) - int(b.Kind) })
	return out
}

// Hidden counts the issues below an object, one count per level. An object
// whose status failed to load counts as danger, one still loading not at all.
type Hidden struct{ Danger, Warn, Info int }

// Total is the number of hidden issues.
func (h Hidden) Total() int { return h.Danger + h.Warn + h.Info }

// HiddenIssues counts the issues of all descendants of o.
func (o *Object) HiddenIssues() Hidden {
	var h Hidden
	Walk(o.Children, func(c *Object) {
		if c.Err != nil {
			h.Danger++
			return
		}
		for _, is := range c.Status.Issues() {
			switch is.Level {
			case LevelDanger:
				h.Danger++
			case LevelWarn:
				h.Warn++
			case LevelInfo:
				h.Info++
			}
		}
	})
	return h
}
