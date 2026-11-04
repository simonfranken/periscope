package resource

import (
	"errors"
	"testing"
	"time"
)

func kinds(is []Issue) []IssueKind {
	var out []IssueKind
	for _, i := range is {
		out = append(out, i.Kind)
	}
	return out
}

func TestIssues(t *testing.T) {
	t0 := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	now = func() time.Time { return t0 }
	defer func() { now = time.Now }()

	cases := []struct {
		name string
		s    *Status
		want []IssueKind
	}{
		{"nil", nil, nil},
		{"healthy deployment", &Status{Desired: 3, Ready: 3, Updated: 3}, nil},
		{"scaled to zero", &Status{}, nil},
		{"down", &Status{Desired: 2, Updated: 2}, []IssueKind{IssueDown}},
		{"degraded and rolling out", &Status{Desired: 3, Ready: 2, Updated: 1}, []IssueKind{IssueDegraded, IssueRollingOut}},
		{"suspended", &Status{Suspended: true}, []IssueKind{IssueSuspended}},
		{"terminating", &Status{Terminating: true}, []IssueKind{IssueTerminating}},
		{"running pod", &Status{Phase: PhaseRunning, Containers: []Container{{Name: "app", Ready: true, State: ContainerRunning}}}, nil},
		{"crashloop with restarts", &Status{Phase: PhaseRunning, Containers: []Container{
			{Name: "app", State: ContainerWaiting, Reason: "CrashLoopBackOff", Restarts: 12, LastExit: t0.Add(-time.Minute)},
		}}, []IssueKind{IssueCrashLoop, IssueRestarting}},
		{"oom killed", &Status{Phase: PhaseRunning, Containers: []Container{
			{Name: "app", Ready: true, State: ContainerRunning, Restarts: 1, LastReason: "OOMKilled", LastExit: t0.Add(-10 * time.Minute)},
		}}, []IssueKind{IssueOOMKilled}},
		{"old restart is quiet", &Status{Phase: PhaseRunning, Containers: []Container{
			{Name: "app", Ready: true, State: ContainerRunning, Restarts: 3, LastExit: t0.Add(-2 * time.Hour)},
		}}, nil},
		{"pull error while pending", &Status{Phase: PhasePending, Containers: []Container{
			{Name: "app", State: ContainerWaiting, Reason: "ImagePullBackOff"},
		}}, []IssueKind{IssuePullError, IssuePending}},
		{"not ready", &Status{Phase: PhaseRunning, Containers: []Container{
			{Name: "app", Ready: true, State: ContainerRunning},
			{Name: "sidecar", State: ContainerRunning},
		}}, []IssueKind{IssueNotReady}},
		{"failed", &Status{Phase: PhaseFailed}, []IssueKind{IssueFailed}},
		{"succeeded", &Status{Phase: PhaseSucceeded}, nil},
	}
	for _, c := range cases {
		got := kinds(c.s.Issues())
		if len(got) != len(c.want) {
			t.Errorf("%s: issues = %v, want %v", c.name, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%s: issues = %v, want %v", c.name, got, c.want)
				break
			}
		}
	}
}

func TestIssueNamesContainer(t *testing.T) {
	s := &Status{Phase: PhaseRunning, Containers: []Container{
		{Name: "app", Ready: true, State: ContainerRunning},
		{Name: "proxy", State: ContainerRunning},
	}}
	if is := s.Issues(); len(is) != 1 || is[0].Container != "proxy" || is[0].Level != LevelWarn {
		t.Fatalf("issues = %+v, want not ready on proxy", is)
	}
}

func TestKindsOrderedBySeverity(t *testing.T) {
	for i := 1; i < len(Kinds); i++ {
		if Kinds[i] <= Kinds[i-1] || Kinds[i].Level() > Kinds[i-1].Level() {
			t.Fatalf("Kinds not ordered by severity at %d", i)
		}
	}
}

func TestHiddenIssues(t *testing.T) {
	ns := &Object{Kind: KindNamespace, Path: "a"}
	dep := &Object{Kind: KindDeployment, Path: "a/deployment/x", Parent: ns,
		Status: &Status{Desired: 2, Ready: 1, Updated: 2}}
	pod := &Object{Kind: KindPod, Path: "a/deployment/x/pod/p", Parent: dep,
		Status: &Status{Phase: PhaseFailed}}
	broken := &Object{Kind: KindPod, Path: "a/deployment/x/pod/q", Parent: dep, Err: errors.New("forbidden")}
	loading := &Object{Kind: KindPod, Path: "a/deployment/x/pod/r", Parent: dep}
	ns.Children = []*Object{dep}
	dep.Children = []*Object{pod, broken, loading}

	if h := ns.HiddenIssues(); h != (Hidden{Danger: 2, Warn: 1}) {
		t.Fatalf("hidden = %+v", h)
	}
	if h := dep.HiddenIssues(); h.Total() != 2 {
		t.Fatalf("hidden below deployment = %+v", h)
	}
}
