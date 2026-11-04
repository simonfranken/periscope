package kube

import (
	"context"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"periscope/internal/resource"
)

var created = metav1.NewTime(time.Now().Add(-time.Hour))

func meta(ns, name, uid string, owner ...metav1.OwnerReference) metav1.ObjectMeta {
	return metav1.ObjectMeta{Namespace: ns, Name: name, UID: types.UID(uid), CreationTimestamp: created, OwnerReferences: owner}
}

func controlledBy(kind, name, uid string) metav1.OwnerReference {
	yes := true
	return metav1.OwnerReference{Kind: kind, Name: name, UID: types.UID(uid), Controller: &yes}
}

func ptr[T any](v T) *T { return &v }

// fixture is a namespace with a Deployment (through a ReplicaSet), a
// CronJob with a Job, a failed Job, a static pod and a pod of an orphaned
// ReplicaSet.
func fixture() []runtime.Object {
	return []runtime.Object{
		&corev1.Namespace{ObjectMeta: meta("", "kube-system", "ns-ks")},
		&corev1.Namespace{ObjectMeta: meta("", "shop", "ns-shop")},
		&appsv1.Deployment{ObjectMeta: meta("shop", "api", "d-api"), Spec: appsv1.DeploymentSpec{Replicas: ptr(int32(2))},
			Status: appsv1.DeploymentStatus{ReadyReplicas: 1, UpdatedReplicas: 2}},
		&appsv1.ReplicaSet{ObjectMeta: meta("shop", "api-7b8", "rs-api", controlledBy("Deployment", "api", "d-api"))},
		&appsv1.ReplicaSet{ObjectMeta: meta("shop", "orphan", "rs-orphan")},
		&batchv1.CronJob{ObjectMeta: meta("shop", "report", "cj-report"), Spec: batchv1.CronJobSpec{Suspend: ptr(true)}},
		&batchv1.Job{ObjectMeta: meta("shop", "report-1", "j-report", controlledBy("CronJob", "report", "cj-report")),
			Status: batchv1.JobStatus{Conditions: []batchv1.JobCondition{{Type: batchv1.JobComplete, Status: corev1.ConditionTrue}}}},
		&batchv1.Job{ObjectMeta: meta("shop", "migrate", "j-migrate"),
			Status: batchv1.JobStatus{Conditions: []batchv1.JobCondition{{Type: batchv1.JobFailed, Status: corev1.ConditionTrue}}}},
		&corev1.Pod{ObjectMeta: meta("shop", "api-7b8-x", "p-api", controlledBy("ReplicaSet", "api-7b8", "rs-api")),
			Spec: corev1.PodSpec{NodeName: "n1", Containers: []corev1.Container{{Name: "app", Image: "api:1"}}},
			Status: corev1.PodStatus{Phase: corev1.PodRunning, ContainerStatuses: []corev1.ContainerStatus{{
				Name: "app", Image: "docker.io/library/api@sha256:abc", RestartCount: 3,
				State:                corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}},
				LastTerminationState: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{Reason: "OOMKilled", FinishedAt: created}},
			}}}},
		&corev1.Pod{ObjectMeta: meta("shop", "report-1-y", "p-report", controlledBy("Job", "report-1", "j-report")),
			Status: corev1.PodStatus{Phase: corev1.PodSucceeded}},
		&corev1.Pod{ObjectMeta: meta("shop", "orphan-z", "p-orphan", controlledBy("ReplicaSet", "orphan", "rs-orphan"))},
		&corev1.Pod{ObjectMeta: meta("kube-system", "etcd-cp", "p-etcd", controlledBy("Node", "cp", "node-cp"))},
		&corev1.Event{ObjectMeta: meta("shop", "e1", "e1"), Type: corev1.EventTypeWarning, Reason: "BackOff", Count: 5,
			LastTimestamp:  metav1.NewTime(time.Now().Add(-time.Minute)),
			InvolvedObject: corev1.ObjectReference{Kind: "Pod", Namespace: "shop", Name: "api-7b8-x", UID: "p-api"}},
		&corev1.Event{ObjectMeta: meta("shop", "e2", "e2"), Type: corev1.EventTypeWarning, Reason: "FailedCreate",
			EventTime:      metav1.NewMicroTime(time.Now()),
			InvolvedObject: corev1.ObjectReference{Kind: "ReplicaSet", Namespace: "shop", Name: "api-7b8", UID: "rs-api"}},
		&corev1.Event{ObjectMeta: meta("shop", "e3", "e3"), Type: corev1.EventTypeWarning, Reason: "BackOff",
			InvolvedObject: corev1.ObjectReference{Kind: "Pod", Namespace: "shop", Name: "api-7b8-x", UID: "an-earlier-pod"}},
	}
}

func provider(client *fake.Clientset, ns string) *Provider {
	return &Provider{context: "kind-dev", ns: ns, server: "https://127.0.0.1:6443", client: client}
}

func paths(c *resource.Cluster) map[string]*resource.Object {
	out := map[string]*resource.Object{}
	resource.Walk(c.Objects, func(o *resource.Object) { out[o.Path] = o })
	return out
}

func TestTree(t *testing.T) {
	p := provider(fake.NewClientset(fixture()...), "")
	c, err := p.Tree(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if c.Context != "kind-dev" || c.Namespace != "" || c.Root.Name != "kind-dev" {
		t.Errorf("cluster = %+v", c)
	}
	got := paths(c)
	for path, group := range map[string]string{
		"shop":                                            "",
		"shop/deployment/api":                             "deployments",
		"shop/deployment/api/pod/api-7b8-x":               "",
		"shop/cronjob/report":                             "cronjobs",
		"shop/cronjob/report/job/report-1":                "",
		"shop/cronjob/report/job/report-1/pod/report-1-y": "",
		"shop/job/migrate":                                "jobs",
		"shop/pod/orphan-z":                               "pods",
		"kube-system/pod/etcd-cp":                         "pods",
	} {
		o, ok := got[path]
		if !ok {
			t.Errorf("missing %s", path)
			continue
		}
		if o.Group != group {
			t.Errorf("%s: group %q, want %q", path, o.Group, group)
		}
		if o.Parent == nil && o.Kind != resource.KindNamespace {
			t.Errorf("%s: no parent", path)
		}
	}
	if len(got) != 10 {
		t.Errorf("%d objects, want 10: %v", len(got), got)
	}
	shop := got["shop"]
	var kinds []resource.Kind
	for _, o := range shop.Children {
		kinds = append(kinds, o.Kind)
	}
	want := []resource.Kind{resource.KindDeployment, resource.KindJob, resource.KindCronJob, resource.KindPod}
	if len(kinds) != len(want) {
		t.Fatalf("shop's children: %v, want %v", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Errorf("shop's children: %v, want %v", kinds, want)
		}
	}
}

func TestStatus(t *testing.T) {
	p := provider(fake.NewClientset(fixture()...), "")
	if _, err := p.Tree(context.Background()); err != nil {
		t.Fatal(err)
	}
	get := func(path string) *resource.Status {
		t.Helper()
		st, err := p.Status(context.Background(), path)
		if err != nil {
			t.Fatal(err)
		}
		return st
	}
	if st := get(""); !st.Created.Equal(created.Time) {
		t.Errorf("cluster created %v, want kube-system's", st.Created)
	}
	d := get("shop/deployment/api")
	if d.Desired != 2 || d.Ready != 1 || d.Updated != 2 {
		t.Errorf("deployment = %+v", d)
	}
	if len(d.Events) != 1 || d.Events[0].Reason != "FailedCreate" || d.Events[0].Count != 1 || d.Events[0].Last.IsZero() {
		t.Errorf("deployment events (its ReplicaSet's) = %+v", d.Events)
	}
	pod := get("shop/deployment/api/pod/api-7b8-x")
	c := pod.Containers[0]
	if pod.Node != "n1" || c.Image != "api:1" || c.Reason != "CrashLoopBackOff" || c.Restarts != 3 || c.LastReason != "OOMKilled" {
		t.Errorf("pod = %+v", pod)
	}
	if len(pod.Events) != 1 || pod.Events[0].Count != 5 {
		t.Errorf("pod events = %+v, want only those of this pod", pod.Events)
	}
	if st := get("shop/job/migrate"); st.Phase != resource.PhaseFailed || st.Issues()[0].Kind != resource.IssueFailed {
		t.Errorf("failed job = %+v", st)
	}
	if st := get("shop/cronjob/report/job/report-1"); st.Phase != resource.PhaseSucceeded || len(st.Issues()) != 0 {
		t.Errorf("complete job = %+v", st)
	}
	if st := get("shop/cronjob/report"); !st.Suspended {
		t.Errorf("cronjob = %+v", st)
	}
	if _, err := p.Status(context.Background(), "shop/deployment/gone"); err == nil {
		t.Error("no error for an unknown path")
	}
}

func forbid(c *fake.Clientset, resource string, clusterWide bool) {
	c.PrependReactor("list", resource, func(a k8stesting.Action) (bool, runtime.Object, error) {
		if clusterWide && a.GetNamespace() != "" {
			return false, nil, nil
		}
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: resource}, "", nil)
	})
}

// A user who may not list namespaces gets the context's, and the other
// kinds listed in it.
func TestNamespacesForbidden(t *testing.T) {
	c := fake.NewClientset(fixture()...)
	forbid(c, "namespaces", false)
	forbid(c, "pods", true)      // only in a namespace
	forbid(c, "cronjobs", false) // not at all
	p := provider(c, "shop")
	cl, err := p.Tree(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if cl.Namespace != "shop" || len(cl.Objects) != 1 || cl.Objects[0].Name != "shop" {
		t.Fatalf("top level = %v, namespace %q", cl.Objects, cl.Namespace)
	}
	got := paths(cl)
	if _, ok := got["shop/deployment/api/pod/api-7b8-x"]; !ok {
		t.Errorf("pods weren't listed in the namespace: %v", got)
	}
	if _, ok := got["shop/job/report-1"]; !ok {
		t.Errorf("without its CronJob, a Job is in the namespace: %v", got)
	}
	if _, err := p.Status(context.Background(), "shop"); err != nil {
		t.Error(err)
	}

	// --namespace names the one to list, over the context's
	p = provider(c, "default")
	p.start = "shop"
	if cl, err = p.Tree(context.Background()); err != nil {
		t.Fatal(err)
	}
	if cl.Namespace != "default" || len(cl.Objects) != 1 || cl.Objects[0].Name != "shop" {
		t.Errorf("with --namespace: top level = %v, namespace %q", cl.Objects, cl.Namespace)
	}
}

func TestTreeError(t *testing.T) {
	c := fake.NewClientset()
	c.PrependReactor("list", "namespaces", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewUnauthorized("token expired")
	})
	if _, err := provider(c, "").Tree(context.Background()); err == nil || !strings.Contains(err.Error(), "token expired") {
		t.Errorf("err = %v", err)
	}
}

func TestContexts(t *testing.T) {
	raw := clientcmdapi.Config{
		CurrentContext: "b",
		Clusters:       map[string]*clientcmdapi.Cluster{"c": {Server: "https://c.example.com"}},
		AuthInfos:      map[string]*clientcmdapi.AuthInfo{"u": {Exec: &clientcmdapi.ExecConfig{Command: "login", APIVersion: "client.authentication.k8s.io/v1", InteractiveMode: clientcmdapi.IfAvailableExecInteractiveMode}}},
		Contexts: map[string]*clientcmdapi.Context{
			"b": {Cluster: "c", AuthInfo: "u", Namespace: "shop"},
			"a": {Cluster: "c", AuthInfo: "u"},
		},
	}
	p, err := newFor(clientcmd.ClientConfigLoadingRules{}, raw, "")
	if err != nil {
		t.Fatal(err)
	}
	if p.context != "b" || p.ns != "shop" || p.server != "https://c.example.com" {
		t.Errorf("provider = %+v", p)
	}
	cs, _ := p.Contexts(context.Background())
	if len(cs) != 2 || cs[0].Name != "a" || cs[0].Current || cs[1].Name != "b" || !cs[1].Current || cs[1].Namespace != "shop" {
		t.Errorf("contexts = %+v", cs)
	}
	if _, err := p.Switch("a"); err != nil {
		t.Error(err)
	}
	if _, err := p.Switch("x"); err == nil {
		t.Error("switched to a context that doesn't exist")
	}
}
