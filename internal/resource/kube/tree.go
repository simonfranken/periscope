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
	discoveryv1 "k8s.io/api/discovery/v1"
	networkingv1 "k8s.io/api/networking/v1"
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
	services     []corev1.Service
	slices       []discoveryv1.EndpointSlice
	ingresses    []networkingv1.Ingress
	routes       []route
	configmaps   []keyed
	secrets      []keyed

	// the namespaces where Services or EndpointSlices can't be listed
	servicesDenied, slicesDenied map[string]bool
}

// Tree lists the namespaces, workloads, pods, Services, Ingresses, Routes,
// ConfigMaps and Secrets. An exec plugin waiting for
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
		func() (err error) {
			l.services, l.servicesDenied, err = listKindDenied(ctx, names, all, func(ns string, o metav1.ListOptions) ([]corev1.Service, string, error) {
				r, err := c.CoreV1().Services(ns).List(ctx, o)
				return items(r, err, func(r *corev1.ServiceList) ([]corev1.Service, string) { return r.Items, r.Continue })
			})
			return err
		},
		func() (err error) {
			l.slices, l.slicesDenied, err = listKindDenied(ctx, names, all, func(ns string, o metav1.ListOptions) ([]discoveryv1.EndpointSlice, string, error) {
				r, err := c.DiscoveryV1().EndpointSlices(ns).List(ctx, o)
				return items(r, err, func(r *discoveryv1.EndpointSliceList) ([]discoveryv1.EndpointSlice, string) {
					return r.Items, r.Continue
				})
			})
			return err
		},
		func() (err error) {
			l.ingresses, err = listKind(ctx, names, all, func(ns string, o metav1.ListOptions) ([]networkingv1.Ingress, string, error) {
				r, err := c.NetworkingV1().Ingresses(ns).List(ctx, o)
				return items(r, err, func(r *networkingv1.IngressList) ([]networkingv1.Ingress, string) { return r.Items, r.Continue })
			})
			return err
		},
		func() (err error) {
			if p.dyn == nil {
				return nil
			}
			l.routes, err = listKind(ctx, names, all, func(ns string, o metav1.ListOptions) ([]route, string, error) {
				r, err := p.dyn.Resource(routeGVR).Namespace(ns).List(ctx, o)
				return items(r, err, routes)
			})
			return err
		},
		func() (err error) {
			l.configmaps, err = listKind(ctx, names, all, func(ns string, o metav1.ListOptions) ([]keyed, string, error) {
				r, err := c.CoreV1().ConfigMaps(ns).List(ctx, o)
				return items(r, err, configMaps)
			})
			return err
		},
		func() (err error) {
			l.secrets, err = listKind(ctx, names, all, func(ns string, o metav1.ListOptions) ([]keyed, string, error) {
				o.Limit = secretPageSize
				r, err := c.CoreV1().Secrets(ns).List(ctx, o)
				return items(r, err, secrets)
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

// pageSize is how many objects a List returns at once; secretPageSize for
// Secrets, of which Helm's can be up to 1 MiB each and are only reduced to
// their keys once a page is decoded.
const (
	pageSize       = 500
	secretPageSize = 50
)

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
	its, _, err := listKindDenied(ctx, nss, all, list)
	return its, err
}

// listKindDenied is listKind, and also returns the namespaces left out
// because listing the kind there is forbidden, for what is derived from the
// kind: a Service that isn't listed is not missing.
func listKindDenied[T any](ctx context.Context, nss []string, all bool, list listFunc[T]) ([]T, map[string]bool, error) {
	if all {
		its, err := page(ctx, func(o metav1.ListOptions) ([]T, string, error) { return list("", o) })
		switch {
		case err == nil:
			return its, nil, nil
		case apierrors.IsNotFound(err):
			return nil, nil, nil
		case !apierrors.IsForbidden(err):
			return nil, nil, err
		}
	}
	var (
		mu     sync.Mutex
		out    []T
		denied = map[string]bool{}
		first  error
		wg     sync.WaitGroup
		sem    = make(chan struct{}, parallel)
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
			case apierrors.IsForbidden(err):
				denied[ns] = true
			case apierrors.IsNotFound(err):
			case first == nil:
				first = err
			}
		})
	}
	wg.Wait()
	return out, denied, first
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
// under "pods" (F3), so no pod is left out. Services, Ingresses, Routes,
// ConfigMaps and Secrets are children of their namespace; what a Service
// selects and an Ingress routes to is in their status (F63).
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
	sel := map[string]*selects{}               // by namespace
	for name := range nss {
		sel[name] = &selects{}
	}
	workload := func(parent *resource.Object, k resource.Kind, kind string, m *metav1.ObjectMeta, st *resource.Status) *resource.Object {
		if parent == nil {
			return nil // its namespace is not listed
		}
		o := &resource.Object{Kind: k, Name: m.Name}
		adopt(parent, o)
		s.add(o, kind, m, st)
		owners[m.UID] = o
		return o
	}
	template := func(o *resource.Object, t *corev1.PodTemplateSpec, st *resource.Status) {
		if o != nil {
			sel[o.Namespace].workloads = append(sel[o.Namespace].workloads, selectedWorkload{labels: t.Labels, ref: o.Ref(), desired: st.Desired})
		}
	}
	for _, d := range l.deployments {
		st := deploymentStatus(&d)
		template(workload(nss[d.Namespace], resource.KindDeployment, "Deployment", &d.ObjectMeta, st), &d.Spec.Template, st)
	}
	for _, d := range l.statefulsets {
		st := statefulSetStatus(&d)
		template(workload(nss[d.Namespace], resource.KindStatefulSet, "StatefulSet", &d.ObjectMeta, st), &d.Spec.Template, st)
	}
	for _, d := range l.daemonsets {
		st := daemonSetStatus(&d)
		template(workload(nss[d.Namespace], resource.KindDaemonSet, "DaemonSet", &d.ObjectMeta, st), &d.Spec.Template, st)
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
		w := o // what the pod is listed as: its workload, or itself
		for w.Parent.Kind != resource.KindNamespace {
			w = w.Parent
		}
		sel[p.Namespace].pods = append(sel[p.Namespace].pods, selectedPod{labels: p.Labels, ready: podReady(&p), target: w.Ref()})
	}

	child := func(k resource.Kind, kind string, m *metav1.ObjectMeta, st *resource.Status) {
		if parent := nss[m.Namespace]; parent != nil {
			o := &resource.Object{Kind: k, Name: m.Name}
			adopt(parent, o)
			s.add(o, kind, m, st)
		}
	}
	ready := endpoints(l.slices)
	services := map[string]bool{} // "<namespace>/<name>"
	for _, svc := range l.services {
		services[svc.Namespace+"/"+svc.Name] = true
		if sel[svc.Namespace] == nil {
			continue
		}
		var n *int
		if !l.slicesDenied[svc.Namespace] {
			c := ready[svc.Namespace+"/"+svc.Name]
			n = &c
		}
		child(resource.KindService, "Service", &svc.ObjectMeta, serviceStatus(&svc, sel[svc.Namespace], n))
	}
	exists := func(ns string) func(string) bool {
		if l.servicesDenied[ns] {
			return nil
		}
		return func(name string) bool { return services[ns+"/"+name] }
	}
	for _, in := range l.ingresses {
		child(resource.KindIngress, "Ingress", &in.ObjectMeta, ingressStatus(&in, exists(in.Namespace)))
	}
	for _, r := range l.routes {
		child(resource.KindRoute, "Route", &r.Metadata, routeStatus(&r, exists(r.Metadata.Namespace)))
	}
	for _, c := range l.configmaps {
		child(resource.KindConfigMap, "ConfigMap", &c.meta, keyedStatus(&c))
	}
	for _, c := range l.secrets {
		child(resource.KindSecret, "Secret", &c.meta, keyedStatus(&c))
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
