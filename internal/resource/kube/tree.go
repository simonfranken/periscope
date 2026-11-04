package kube

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"sync"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"periscope/internal/resource"
)

// snapshot is what one Tree read: every object's status (without events),
// and which object an event is about. Status reads it; a new Tree replaces
// it.
type snapshot struct {
	statuses map[string]*resource.Status
	byUID    map[types.UID]string // involved object -> path; a ReplicaSet's -> its Deployment's
	byName   map[string]string    // "<kind>/<namespace>/<name>" -> path, for events without a UID
	nss      []string             // the namespaces, to list events per namespace if need be
	all      bool                 // whether namespaced kinds can be listed cluster-wide

	once   sync.Once
	events map[string][]resource.Event // by path, newest first
}

// lists is what Tree lists: one List call per kind, cluster-wide.
type lists struct {
	namespaces   []corev1.Namespace
	deployments  []appsv1.Deployment
	statefulsets []appsv1.StatefulSet
	daemonsets   []appsv1.DaemonSet
	replicasets  []appsv1.ReplicaSet
	jobs         []batchv1.Job
	cronjobs     []batchv1.CronJob
	pods         []corev1.Pod
}

// Tree lists the namespaces, workloads and pods. An exec plugin waiting for
// a login doesn't stop when its request is cancelled, so Tree gives up on
// it after treeTimeout and returns what the plugin said.
func (p *Provider) Tree(ctx context.Context) (*resource.Cluster, error) {
	ctx, cancel := context.WithTimeout(ctx, treeTimeout)
	defer cancel()
	mark := captured.mark()
	type result struct {
		c   *resource.Cluster
		err error
	}
	done := make(chan result, 1)
	go func() {
		c, err := p.tree(ctx)
		done <- result{c, err}
	}()
	var r result
	select {
	case r = <-done:
	case <-ctx.Done():
		r.err = ctx.Err()
	}
	if r.err == nil {
		return r.c, nil
	}
	return nil, p.explain(ctx, r.err, captured.since(mark))
}

// explain makes err fit the footer's line: the request's URL is dropped,
// and what an exec plugin wrote to stderr comes first, since it says what
// to do ("log in at …").
func (p *Provider) explain(ctx context.Context, err error, plugin string) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	if errors.Is(err, context.DeadlineExceeded) && context.Cause(ctx) == context.DeadlineExceeded {
		err = fmt.Errorf("no answer from %s within %s", p.server, treeTimeout)
	}
	if plugin != "" {
		err = fmt.Errorf("%s (%w)", plugin, err)
	}
	return err
}

func (p *Provider) tree(ctx context.Context) (*resource.Cluster, error) {
	var l lists
	all, err := p.listNamespaces(ctx, &l)
	if err != nil {
		return nil, err
	}
	// after the first answer, so an exec plugin that fails runs only once
	version := make(chan string, 1)
	go func() {
		v, err := p.client.Discovery().ServerVersion()
		if err != nil {
			version <- ""
			return
		}
		version <- v.GitVersion
	}()
	var names []string
	for _, n := range l.namespaces {
		names = append(names, n.Name)
	}
	c := p.client
	err = parallelly(
		func() (err error) {
			l.deployments, err = listKind(ctx, names, all, func(ns string, o metav1.ListOptions) ([]appsv1.Deployment, string, error) {
				r, err := c.AppsV1().Deployments(ns).List(ctx, o)
				return items(r, err, func(r *appsv1.DeploymentList) ([]appsv1.Deployment, string) { return r.Items, r.Continue })
			})
			return err
		},
		func() (err error) {
			l.statefulsets, err = listKind(ctx, names, all, func(ns string, o metav1.ListOptions) ([]appsv1.StatefulSet, string, error) {
				r, err := c.AppsV1().StatefulSets(ns).List(ctx, o)
				return items(r, err, func(r *appsv1.StatefulSetList) ([]appsv1.StatefulSet, string) { return r.Items, r.Continue })
			})
			return err
		},
		func() (err error) {
			l.daemonsets, err = listKind(ctx, names, all, func(ns string, o metav1.ListOptions) ([]appsv1.DaemonSet, string, error) {
				r, err := c.AppsV1().DaemonSets(ns).List(ctx, o)
				return items(r, err, func(r *appsv1.DaemonSetList) ([]appsv1.DaemonSet, string) { return r.Items, r.Continue })
			})
			return err
		},
		func() (err error) {
			l.replicasets, err = listKind(ctx, names, all, func(ns string, o metav1.ListOptions) ([]appsv1.ReplicaSet, string, error) {
				r, err := c.AppsV1().ReplicaSets(ns).List(ctx, o)
				return items(r, err, func(r *appsv1.ReplicaSetList) ([]appsv1.ReplicaSet, string) { return r.Items, r.Continue })
			})
			return err
		},
		func() (err error) {
			l.jobs, err = listKind(ctx, names, all, func(ns string, o metav1.ListOptions) ([]batchv1.Job, string, error) {
				r, err := c.BatchV1().Jobs(ns).List(ctx, o)
				return items(r, err, func(r *batchv1.JobList) ([]batchv1.Job, string) { return r.Items, r.Continue })
			})
			return err
		},
		func() (err error) {
			l.cronjobs, err = listKind(ctx, names, all, func(ns string, o metav1.ListOptions) ([]batchv1.CronJob, string, error) {
				r, err := c.BatchV1().CronJobs(ns).List(ctx, o)
				return items(r, err, func(r *batchv1.CronJobList) ([]batchv1.CronJob, string) { return r.Items, r.Continue })
			})
			return err
		},
		func() (err error) {
			l.pods, err = listKind(ctx, names, all, func(ns string, o metav1.ListOptions) ([]corev1.Pod, string, error) {
				r, err := c.CoreV1().Pods(ns).List(ctx, o)
				return items(r, err, func(r *corev1.PodList) ([]corev1.Pod, string) { return r.Items, r.Continue })
			})
			return err
		},
	)
	if err != nil {
		return nil, err
	}

	cl, s := build(&l)
	s.all = all
	cl.Context, cl.Server, cl.Namespace = p.context, p.server, p.ns
	cl.Root.Name = p.context
	select {
	case cl.Version = <-version:
	case <-time.After(time.Second): // the tree doesn't wait long for it
	}
	p.mu.Lock()
	p.snap = s
	p.mu.Unlock()
	return cl, nil
}

// listNamespaces lists the namespaces into l. If that is forbidden, the
// one of --namespace, else the context's, else "default" is the only one,
// so a user with
// rights in one namespace gets the same flow (F4), and the other kinds are
// listed in it only; all reports whether they may be listed cluster-wide.
func (p *Provider) listNamespaces(ctx context.Context, l *lists) (all bool, err error) {
	l.namespaces, err = page(ctx, func(o metav1.ListOptions) ([]corev1.Namespace, string, error) {
		r, err := p.client.CoreV1().Namespaces().List(ctx, o)
		return items(r, err, func(r *corev1.NamespaceList) ([]corev1.Namespace, string) { return r.Items, r.Continue })
	})
	if err == nil {
		return true, nil
	}
	if !apierrors.IsForbidden(err) {
		return false, err
	}
	name := cmp.Or(p.start, p.ns, "default")
	ns, err := p.client.CoreV1().Namespaces().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if !apierrors.IsForbidden(err) && !apierrors.IsNotFound(err) {
			return false, err
		}
		ns = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}
	}
	l.namespaces = []corev1.Namespace{*ns}
	return false, nil
}

// listFunc lists one page of a kind in namespace ns ("" for all).
type listFunc[T any] func(ns string, o metav1.ListOptions) (items []T, next string, err error)

// items unpacks a List result.
func items[L, T any](r L, err error, get func(L) ([]T, string)) ([]T, string, error) {
	if err != nil {
		return nil, "", err
	}
	its, next := get(r)
	return its, next, nil
}

// pageSize is how many objects a List returns at once.
const pageSize = 500

// page lists all pages.
func page[T any](ctx context.Context, list func(metav1.ListOptions) ([]T, string, error)) ([]T, error) {
	var out []T
	o := metav1.ListOptions{Limit: pageSize}
	for {
		its, next, err := list(o)
		if err != nil {
			return nil, err
		}
		out = append(out, its...)
		if next == "" || ctx.Err() != nil {
			return out, ctx.Err()
		}
		o.Continue = next
	}
}

// listKind lists a namespaced kind in the whole cluster if all, else (or if
// that is forbidden) in each namespace of nss, a few at a time. Namespaces
// and kinds the user may not list are left out: they see what they may see.
// A kind the server doesn't serve is empty.
func listKind[T any](ctx context.Context, nss []string, all bool, list listFunc[T]) ([]T, error) {
	if all {
		its, err := page(ctx, func(o metav1.ListOptions) ([]T, string, error) { return list("", o) })
		switch {
		case err == nil:
			return its, nil
		case apierrors.IsNotFound(err):
			return nil, nil
		case !apierrors.IsForbidden(err):
			return nil, err
		}
	}
	var (
		mu    sync.Mutex
		out   []T
		first error
		wg    sync.WaitGroup
		sem   = make(chan struct{}, parallel)
	)
	for _, ns := range nss {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			its, err := page(ctx, func(o metav1.ListOptions) ([]T, string, error) { return list(ns, o) })
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				out = append(out, its...)
			case apierrors.IsForbidden(err), apierrors.IsNotFound(err):
			case first == nil:
				first = err
			}
		})
	}
	wg.Wait()
	return out, first
}

// parallelly runs fns at once and returns the first error.
func parallelly(fns ...func() error) error {
	errs := make([]error, len(fns))
	var wg sync.WaitGroup
	for i, fn := range fns {
		wg.Go(func() { errs[i] = fn() })
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// build links the listed objects into a tree, through owner references:
// Pod → ReplicaSet → Deployment, Pod → StatefulSet or DaemonSet,
// Pod → Job → CronJob. ReplicaSets are not shown. A pod without a workload
// (a static pod, a bare pod, one of an operator) is listed in its namespace
// under "pods" (F3), so no pod is left out.
func build(l *lists) (*resource.Cluster, *snapshot) {
	s := &snapshot{
		statuses: map[string]*resource.Status{},
		byUID:    map[types.UID]string{},
		byName:   map[string]string{},
	}
	root := &resource.Object{Kind: resource.KindCluster}
	s.statuses[""] = &resource.Status{}

	nss := map[string]*resource.Object{}
	var top []*resource.Object
	for _, n := range l.namespaces {
		o := &resource.Object{Kind: resource.KindNamespace, Name: n.Name, Path: n.Name}
		s.add(o, "Namespace", &n.ObjectMeta, namespaceStatus(&n))
		nss[n.Name] = o
		top = append(top, o)
		s.nss = append(s.nss, n.Name)
		if n.Name == "kube-system" { // as old as the cluster
			s.statuses[""].Created = n.CreationTimestamp.Time
		}
	}

	owners := map[types.UID]*resource.Object{} // workloads, by UID
	workload := func(parent *resource.Object, k resource.Kind, kind string, m *metav1.ObjectMeta, st *resource.Status) {
		if parent == nil {
			return // its namespace is not listed
		}
		o := &resource.Object{Kind: k, Name: m.Name}
		adopt(parent, o)
		s.add(o, kind, m, st)
		owners[m.UID] = o
	}
	for _, d := range l.deployments {
		workload(nss[d.Namespace], resource.KindDeployment, "Deployment", &d.ObjectMeta, deploymentStatus(&d))
	}
	for _, d := range l.statefulsets {
		workload(nss[d.Namespace], resource.KindStatefulSet, "StatefulSet", &d.ObjectMeta, statefulSetStatus(&d))
	}
	for _, d := range l.daemonsets {
		workload(nss[d.Namespace], resource.KindDaemonSet, "DaemonSet", &d.ObjectMeta, daemonSetStatus(&d))
	}
	for _, c := range l.cronjobs {
		workload(nss[c.Namespace], resource.KindCronJob, "CronJob", &c.ObjectMeta, cronJobStatus(&c))
	}
	for _, j := range l.jobs {
		parent := nss[j.Namespace]
		if parent != nil {
			if o := owner(&j.ObjectMeta, owners, "CronJob"); o != nil {
				parent = o
			}
		}
		workload(parent, resource.KindJob, "Job", &j.ObjectMeta, jobStatus(&j))
	}
	for _, r := range l.replicasets {
		if d := owner(&r.ObjectMeta, owners, "Deployment"); d != nil {
			owners[r.UID] = d // its pods belong to the Deployment
			s.byUID[r.UID] = d.Path
		}
	}
	for _, p := range l.pods {
		parent := nss[p.Namespace]
		if parent == nil {
			continue
		}
		if o := owner(&p.ObjectMeta, owners, "ReplicaSet", "StatefulSet", "DaemonSet", "Job"); o != nil {
			parent = o
		}
		o := &resource.Object{Kind: resource.KindPod, Name: p.Name}
		adopt(parent, o)
		s.add(o, "Pod", &p.ObjectMeta, podStatus(&p))
	}

	byName := func(objs []*resource.Object) {
		slices.SortStableFunc(objs, func(a, b *resource.Object) int { return cmp.Compare(a.Name, b.Name) })
	}
	byName(top)
	resource.Walk(top, func(o *resource.Object) { byName(o.Children) })
	return &resource.Cluster{Root: root, Objects: resource.Arrange(top)}, s
}

// owner returns the workload that controls m, if it is of one of kinds.
func owner(m *metav1.ObjectMeta, owners map[types.UID]*resource.Object, kinds ...string) *resource.Object {
	ref := metav1.GetControllerOfNoCopy(m)
	if ref == nil || !slices.Contains(kinds, ref.Kind) {
		return nil
	}
	return owners[ref.UID]
}

// adopt makes o a child of parent and sets its namespace, path and group.
func adopt(parent, o *resource.Object) {
	o.Parent = parent
	o.Namespace = parent.Namespace
	if parent.Kind == resource.KindNamespace {
		o.Namespace = parent.Name
		o.Group = o.Kind.Plural()
	}
	o.Path = parent.Path + "/" + o.Ref()
	parent.Children = append(parent.Children, o)
}

// add records the status of o and where events about it go.
func (s *snapshot) add(o *resource.Object, kind string, m *metav1.ObjectMeta, st *resource.Status) {
	st.Created = m.CreationTimestamp.Time
	s.statuses[o.Path] = st
	if m.UID != "" {
		s.byUID[m.UID] = o.Path
	}
	s.byName[kind+"/"+m.Namespace+"/"+m.Name] = o.Path
}

// Status returns the state of the object at path from the last Tree, with
// its warning events. The events of the whole cluster are listed once per
// Tree, by the first Status call.
func (p *Provider) Status(ctx context.Context, path string) (*resource.Status, error) {
	p.mu.Lock()
	s := p.snap
	p.mu.Unlock()
	if s == nil {
		return nil, fmt.Errorf("%s: not loaded", path)
	}
	st, ok := s.statuses[path]
	if !ok {
		return nil, fmt.Errorf("%s: not found", path)
	}
	s.once.Do(func() { s.events = p.listEvents(ctx, s) })
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cp := *st
	cp.Events = s.events[path]
	return &cp, nil
}
