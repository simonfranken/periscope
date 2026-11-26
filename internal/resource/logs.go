package resource

import (
	"cmp"
	"regexp"
	"time"
)

// LogRequest asks for the log of one container of a pod.
type LogRequest struct {
	Pod       *Object
	Container string
	Previous  bool // the container's last terminated run instead of the current one
	Tail      int  // how many lines from the end to start with, 0 for all
}

// LogLine is one line of a log; Time is when the container wrote it, zero
// if unknown.
type LogLine struct {
	Time time.Time
	Text string
}

// LogTarget is a log periscope can show: a container of a pod, its current
// or its previous run.
type LogTarget struct {
	Pod       *Object
	Container string
	Previous  bool
}

// Key identifies the target across reloads, which replace the objects.
func (t LogTarget) Key() string {
	if t.Pod == nil {
		return ""
	}
	k := t.Pod.Path + "\x00" + t.Container
	if t.Previous {
		k += "\x00previous"
	}
	return k
}

// ContainerInfo returns the state of the target's container, nil if it is
// unknown.
func (t LogTarget) ContainerInfo() *Container {
	if t.Pod == nil || t.Pod.Status == nil {
		return nil
	}
	for i := range t.Pod.Status.Containers {
		if t.Pod.Status.Containers[i].Name == t.Container {
			return &t.Pod.Status.Containers[i]
		}
	}
	return nil
}

// HasPrevious reports whether c has run before, so there is the log of a
// previous run.
func (c *Container) HasPrevious() bool { return c != nil && c.Restarts > 0 }

// PickLog chooses the log that matters for o, so it can be shown without
// asking (P19). For a pod: the container its most severe issue is about,
// else the default container, else the first; the previous run if the
// container isn't running and has run before (a crashloop's log is the
// crashed run's). For a workload: that of its pod with the most severe
// issue, else of its newest pod. Other kinds have no log; neither has an
// object whose state is still loading.
func PickLog(o *Object) (LogTarget, bool) {
	if o == nil {
		return LogTarget{}, false
	}
	switch o.Kind {
	case KindPod:
		return pickContainer(o)
	case KindDeployment, KindStatefulSet, KindDaemonSet, KindJob, KindCronJob:
		var best *Object
		Walk(o.Children, func(p *Object) {
			if p.Kind == KindPod && p.Status != nil && len(p.Status.Containers) > 0 && worse(p, best) {
				best = p
			}
		})
		if best == nil {
			return LogTarget{}, false
		}
		return pickContainer(best)
	}
	return LogTarget{}, false
}

// worse reports whether pod a needs attention before b: a more severe
// issue, then the newer one.
func worse(a, b *Object) bool {
	if b == nil {
		return true
	}
	ia, ib := a.Status.Issues(), b.Status.Issues()
	switch {
	case len(ia) > 0 && len(ib) == 0:
		return true
	case len(ia) == 0 && len(ib) > 0:
		return false
	case len(ia) > 0:
		if c := cmp.Compare(ia[0].Level, ib[0].Level); c != 0 {
			return c > 0
		}
		if c := cmp.Compare(ia[0].Kind, ib[0].Kind); c != 0 {
			return c < 0
		}
	}
	return a.Status.Created.After(b.Status.Created)
}

func pickContainer(p *Object) (LogTarget, bool) {
	st := p.Status
	if st == nil || len(st.Containers) == 0 {
		return LogTarget{}, false
	}
	name := st.Containers[0].Name
	if st.DefaultContainer != "" {
		for _, c := range st.Containers {
			if c.Name == st.DefaultContainer {
				name = c.Name
			}
		}
	}
	for _, is := range st.Issues() {
		if is.Container != "" {
			name = is.Container
			break
		}
	}
	return Target(p, name), true
}

// Target is the log of container name of pod p that is worth reading: the
// previous run if the container isn't running and has run before.
func Target(p *Object, name string) LogTarget {
	t := LogTarget{Pod: p, Container: name}
	c := t.ContainerInfo()
	t.Previous = c.HasPrevious() && c.State != ContainerRunning
	return t
}

// Log lines are classified by the words programs and logging libraries use
// for their levels: level=error, ERROR, [error], logrus' ERRO, zerolog's ERR,
// klog's E1125 prefix, Java's exceptions.
var (
	dangerLine = regexp.MustCompile(`(?i:\b(error|erro|fatal|panic|critical|crit|emerg)\b|exception\b)|\b(ERR|FTL|PNC)\b|^[EF]\d{4} `)
	warnLine   = regexp.MustCompile(`(?i:\b(warn|warning)\b)|\bWRN\b|^W\d{4} `)
)

// LogLevel is how much attention a log line asks for: LevelDanger for an
// error, LevelWarn for a warning, LevelQuiet otherwise.
func LogLevel(text string) Level {
	switch {
	case dangerLine.MatchString(text):
		return LevelDanger
	case warnLine.MatchString(text):
		return LevelWarn
	}
	return LevelQuiet
}
