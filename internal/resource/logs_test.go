package resource

import (
	"testing"
	"time"
)

func pod(name string, created time.Time, cs ...Container) *Object {
	return &Object{Kind: KindPod, Name: name, Path: "shop/pod/" + name, Status: &Status{Phase: PhaseRunning, Created: created, Containers: cs}}
}

func TestPickLogPod(t *testing.T) {
	t0 := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	now = func() time.Time { return t0 }
	defer func() { now = time.Now }()
	run := func(name string) Container { return Container{Name: name, Ready: true, State: ContainerRunning} }

	cases := []struct {
		name      string
		pod       *Object
		container string
		previous  bool
	}{
		{"first container", pod("a", t0, run("app"), run("envoy")), "app", false},
		{"the issue's container", pod("b", t0, run("app"), Container{Name: "envoy", State: ContainerRunning}), "envoy", false},
		{"crashloop: the crashed run", pod("c", t0, Container{Name: "app", State: ContainerWaiting, Reason: "CrashLoopBackOff", Restarts: 3}), "app", true},
		{"restarted but running: the current run", pod("d", t0, Container{Name: "app", Ready: true, State: ContainerRunning, Restarts: 2, LastExit: t0.Add(-time.Minute)}), "app", false},
		{"never started: the current run", pod("e", t0, Container{Name: "app", State: ContainerWaiting, Reason: "ImagePullBackOff"}), "app", false},
	}
	for _, c := range cases {
		got, ok := PickLog(c.pod)
		if !ok || got.Pod != c.pod || got.Container != c.container || got.Previous != c.previous {
			t.Errorf("%s: got %+v %v, want %s previous=%v", c.name, got, ok, c.container, c.previous)
		}
	}

	d := pod("f", t0, run("istio-proxy"), run("app"))
	d.Status.DefaultContainer = "app"
	if got, _ := PickLog(d); got.Container != "app" {
		t.Errorf("default container: got %q", got.Container)
	}
	if _, ok := PickLog(&Object{Kind: KindPod, Name: "loading"}); ok {
		t.Error("a pod still loading has no log yet")
	}
	if _, ok := PickLog(&Object{Kind: KindNamespace, Status: &Status{}}); ok {
		t.Error("a namespace has no log")
	}
}

func TestPickLogWorkload(t *testing.T) {
	t0 := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	now = func() time.Time { return t0 }
	defer func() { now = time.Now }()
	ok := func(name string, created time.Time) *Object {
		return pod(name, created, Container{Name: "app", Ready: true, State: ContainerRunning})
	}
	dep := &Object{Kind: KindDeployment, Children: []*Object{ok("old", t0.Add(-time.Hour)), ok("new", t0), {Kind: KindPod, Name: "loading"}}}
	if got, _ := PickLog(dep); got.Pod.Name != "new" {
		t.Errorf("all healthy: the newest pod, got %s", got.Pod.Name)
	}
	warn := pod("unready", t0.Add(-time.Hour), Container{Name: "app", State: ContainerRunning})
	crash := pod("crash", t0.Add(-2*time.Hour), Container{Name: "app", State: ContainerWaiting, Reason: "CrashLoopBackOff", Restarts: 5})
	dep.Children = append(dep.Children, warn, crash)
	if got, _ := PickLog(dep); got.Pod.Name != "crash" || !got.Previous {
		t.Errorf("the most severe pod's crashed run, got %s previous=%v", got.Pod.Name, got.Previous)
	}
	// a CronJob's pods are below its Jobs
	cj := &Object{Kind: KindCronJob, Children: []*Object{{Kind: KindJob, Children: []*Object{ok("run-1", t0)}}}}
	if got, found := PickLog(cj); !found || got.Pod.Name != "run-1" {
		t.Errorf("cronjob: got %+v", got)
	}
	if _, found := PickLog(&Object{Kind: KindCronJob}); found {
		t.Error("a cronjob without runs has no log")
	}
}

func TestLogLevel(t *testing.T) {
	cases := map[string]Level{
		`level=error msg="dial tcp: connection refused"`:  LevelDanger,
		`{"level":"error","msg":"boom"}`:                  LevelDanger,
		`ERROR: relation "orders" already exists`:         LevelDanger,
		`panic: runtime error: index out of range`:        LevelDanger,
		`ERRO[0003] failed to pull`:                       LevelDanger,
		`12:00:01 ERR request failed`:                     LevelDanger,
		`E1125 10:41:02.123456       1 reflector.go:1] x`: LevelDanger,
		`java.lang.NullPointerException`:                  LevelDanger,
		`level=warn msg="slow request"`:                   LevelWarn,
		`WARNING: no TLS configured`:                      LevelWarn,
		`W1125 10:41:02.123456       1 warnings.go:70] x`: LevelWarn,
		`level=info msg="request" status=200`:             LevelQuiet,
		`processed 3 items, 0 errors`:                     LevelQuiet,
		`error_count=0`:                                   LevelQuiet,
		`Everything fine, no warnings`:                    LevelQuiet,
	}
	for line, want := range cases {
		if got := LogLevel(line); got != want {
			t.Errorf("%q: level %d, want %d", line, got, want)
		}
	}
}
