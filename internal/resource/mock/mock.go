// Package mock is a provider with a fixed cluster that shows every issue
// word, with artificial latency so the loading path is exercised.
package mock

import (
	"context"
	"fmt"
	"math/rand/v2"
	"sync"
	"time"

	"periscope/internal/resource"
)

// Provider serves the fixture cluster. Every Tree builds it anew, with
// times relative to now.
type Provider struct {
	mu       sync.Mutex
	statuses map[string]*resource.Status
}

// New returns the mock provider.
func New() *Provider {
	p := &Provider{}
	p.Tree(context.Background())
	return p
}

func (p *Provider) Tree(ctx context.Context) (*resource.Cluster, error) {
	statuses := map[string]*resource.Status{}
	c := fixture(statuses, time.Now())
	p.mu.Lock()
	p.statuses = statuses
	p.mu.Unlock()
	return c, nil
}

func (p *Provider) Status(ctx context.Context, path string) (*resource.Status, error) {
	d := time.Duration(100+rand.IntN(700)) * time.Millisecond
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(d):
	}
	p.mu.Lock()
	s, ok := p.statuses[path]
	p.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("%s: not found", path)
	}
	cp := *s
	return &cp, nil
}

// builder links objects and records their statuses.
type builder struct {
	statuses map[string]*resource.Status
	now      time.Time
}

func (b builder) namespace(name string, st *resource.Status, objects ...*resource.Object) *resource.Object {
	ns := &resource.Object{Kind: resource.KindNamespace, Name: name, Path: name}
	b.statuses[ns.Path] = st
	for _, o := range objects {
		b.adopt(ns, o)
	}
	return ns
}

// adopt makes o a child of parent and derives the paths below it.
func (b builder) adopt(parent, o *resource.Object) {
	o.Parent = parent
	o.Namespace = parent.Namespace
	if parent.Kind == resource.KindNamespace {
		o.Namespace = parent.Name
		if o.Kind != resource.KindCustom { // resource.Arrange sets the custom resources' groups
			o.Group = o.Kind.Plural()
		}
	}
	o.Path = parent.Path + "/" + o.Ref()
	parent.Children = append(parent.Children, o)
	if st, ok := b.statuses[o.Ref()]; ok { // registered by ref before the path was known
		delete(b.statuses, o.Ref())
		b.statuses[o.Path] = st
	}
	children := o.Children
	o.Children = nil
	for _, c := range children {
		b.adopt(o, c)
	}
}

func (b builder) workload(k resource.Kind, name string, st *resource.Status, pods ...*resource.Object) *resource.Object {
	st.Created = b.now.Add(-30 * 24 * time.Hour)
	w := &resource.Object{Kind: k, Name: name, Children: pods}
	b.statuses[w.Ref()] = st
	return w
}

func (b builder) pod(name string, st *resource.Status) *resource.Object {
	if st.Created.IsZero() {
		st.Created = b.now.Add(-3 * 24 * time.Hour)
	}
	o := &resource.Object{Kind: resource.KindPod, Name: name}
	b.statuses[o.Ref()] = st
	return o
}

// custom is a custom resource of type t with the given conditions.
func (b builder) custom(t *resource.Type, name string, conds ...resource.Condition) *resource.Object {
	o := &resource.Object{Kind: resource.KindCustom, Type: t, Name: name}
	b.statuses[o.Ref()] = &resource.Status{Created: b.now.Add(-30 * 24 * time.Hour), Conditions: conds}
	return o
}

// clusterScoped puts a custom resource at the top level, where its path
// is its ref, the key custom registered its status under.
func (b builder) clusterScoped(o *resource.Object) *resource.Object {
	o.Path = o.Ref()
	return o
}

// Types of the custom resources; two of them share the plural
// "applications", so their headers name the API group.
var (
	certificate    = &resource.Type{Kind: "Certificate", Plural: "certificates", Group: "cert-manager.io"}
	clusterIssuer  = &resource.Type{Kind: "ClusterIssuer", Plural: "clusterissuers", Group: "cert-manager.io"}
	route          = &resource.Type{Kind: "Route", Plural: "routes", Group: "route.openshift.io"}
	serviceMonitor = &resource.Type{Kind: "ServiceMonitor", Plural: "servicemonitors", Group: "monitoring.coreos.com"}
	argoApp        = &resource.Type{Kind: "Application", Plural: "applications", Group: "argoproj.io"}
	sigApp         = &resource.Type{Kind: "Application", Plural: "applications", Group: "app.k8s.io"}
)

func ready(reason, message string) resource.Condition {
	return resource.Condition{Type: "Ready", Status: "True", Reason: reason, Message: message}
}

func running(node, image string) *resource.Status {
	return &resource.Status{Phase: resource.PhaseRunning, Node: node, Containers: []resource.Container{
		{Name: "app", Image: image, Ready: true, State: resource.ContainerRunning},
	}}
}

func fixture(statuses map[string]*resource.Status, now time.Time) *resource.Cluster {
	b := builder{statuses: statuses, now: now}
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	ns := func() *resource.Status { return &resource.Status{Created: ago(90 * 24 * time.Hour)} }

	crash := running("node-2", "registry.example.com/shop/api:1.8.0")
	crash.Containers[0] = resource.Container{Name: "app", Image: "registry.example.com/shop/api:1.8.0",
		State: resource.ContainerWaiting, Reason: "CrashLoopBackOff", Restarts: 14, LastReason: "Error", LastExit: ago(40 * time.Second)}
	crash.Created = ago(25 * time.Minute)
	crash.Events = []resource.Event{{Reason: "BackOff", Message: "Back-off restarting failed container app", Count: 61, Last: ago(40 * time.Second)}}

	oom := running("node-3", "postgres:17.2")
	oom.Containers[0].Restarts, oom.Containers[0].LastReason, oom.Containers[0].LastExit = 1, "OOMKilled", ago(12*time.Minute)
	oom.Events = []resource.Event{{Reason: "OOMKilling", Message: "Memory cgroup out of memory: Killed process 4211 (postgres)", Count: 1, Last: ago(12 * time.Minute)}}

	pull := func() *resource.Status {
		return &resource.Status{Phase: resource.PhasePending, Node: "node-1", Created: ago(8 * time.Minute), Containers: []resource.Container{
			{Name: "app", Image: "registry.example.com/payments/checkout:2.4.0", State: resource.ContainerWaiting, Reason: "ImagePullBackOff"},
		}, Events: []resource.Event{{Reason: "Failed", Message: `Failed to pull image "registry.example.com/payments/checkout:2.4.0": not found`, Count: 9, Last: ago(time.Minute)}}}
	}

	notReady := running("node-1", "registry.example.com/payments/fraud:0.9.3")
	notReady.Containers = append(notReady.Containers, resource.Container{Name: "envoy", Image: "envoyproxy/envoy:v1.33", State: resource.ContainerRunning})
	notReady.Events = []resource.Event{{Reason: "Unhealthy", Message: "Readiness probe failed: HTTP probe failed with statuscode: 503", Count: 112, Last: ago(5 * time.Second)}}

	restarted := running("node-2", "grafana/grafana:12.1.0")
	restarted.Containers[0].Restarts, restarted.Containers[0].LastReason, restarted.Containers[0].LastExit = 2, "Error", ago(20*time.Minute)

	failed := &resource.Status{Phase: resource.PhaseFailed, Node: "node-3", Created: ago(2 * time.Hour), Containers: []resource.Container{
		{Name: "migrate", Image: "registry.example.com/shop/migrate:42", State: resource.ContainerTerminated, Reason: "Error"},
	}, Events: []resource.Event{{Reason: "BackoffLimitExceeded", Message: "Job has reached the specified backoff limit", Count: 1, Last: ago(110 * time.Minute)}}}

	unscheduled := &resource.Status{Phase: resource.PhasePending, Created: ago(3 * time.Minute), Containers: []resource.Container{
		{Name: "app", Image: "registry.example.com/search/indexer:3.0.0", State: resource.ContainerWaiting, Reason: "ContainerCreating"},
	}, Events: []resource.Event{{Reason: "FailedScheduling", Message: "0/3 nodes are available: 3 Insufficient memory.", Count: 4, Last: ago(30 * time.Second)}}}

	nodes := []string{"node-1", "node-2", "node-3"}
	var exporters []*resource.Object
	var proxies []*resource.Object
	for i, n := range nodes {
		exporters = append(exporters, b.pod(fmt.Sprintf("node-exporter-%c%c9x", 'a'+i, 'k'+i), running(n, "prom/node-exporter:v1.9.1")))
		proxies = append(proxies, b.pod(fmt.Sprintf("kube-proxy-%c2%cmq", 'p'+i, 'f'+i), running(n, "registry.k8s.io/kube-proxy:v1.34.1")))
	}

	objects := []*resource.Object{
		b.namespace("argocd", ns(),
			b.workload(resource.KindDeployment, "argocd-server", &resource.Status{Desired: 1, Ready: 1, Updated: 1},
				b.pod("argocd-server-7d9c6b5f4-wq8rt", running("node-1", "quay.io/argoproj/argocd:v3.1.0"))),
			b.custom(argoApp, "shop"), // Argo CD reports health in status.health, not as a condition
		),
		b.namespace("default", ns(),
			b.workload(resource.KindDeployment, "hello", &resource.Status{Desired: 1, Ready: 1, Updated: 1},
				b.pod("hello-6d8f9b7c4-x2k9p", running("node-1", "nginx:1.29"))),
		),
		b.namespace("kube-system", ns(),
			b.workload(resource.KindDeployment, "coredns", &resource.Status{Desired: 2, Ready: 2, Updated: 2},
				b.pod("coredns-7c65d6cfc9-4hzm2", running("node-1", "registry.k8s.io/coredns/coredns:v1.12.0")),
				b.pod("coredns-7c65d6cfc9-r8wq5", running("node-2", "registry.k8s.io/coredns/coredns:v1.12.0"))),
			b.workload(resource.KindDaemonSet, "kube-proxy", &resource.Status{Desired: 3, Ready: 3, Updated: 3}, proxies...),
		),
		b.namespace("monitoring", ns(),
			b.workload(resource.KindDeployment, "grafana", &resource.Status{Desired: 1, Ready: 1, Updated: 1},
				b.pod("grafana-5b9c8d7f6-l7n4t", restarted)),
			b.workload(resource.KindStatefulSet, "prometheus", &resource.Status{Desired: 1, Ready: 1, Updated: 1},
				b.pod("prometheus-0", running("node-3", "prom/prometheus:v3.5.0"))),
			b.workload(resource.KindDaemonSet, "node-exporter", &resource.Status{Desired: 3, Ready: 3, Updated: 3}, exporters...),
			b.custom(serviceMonitor, "grafana"),
			b.custom(serviceMonitor, "node-exporter"),
		),
		b.namespace("payments", ns(),
			b.workload(resource.KindDeployment, "checkout", &resource.Status{Desired: 2, Ready: 0, Updated: 2},
				b.pod("checkout-84c6f5d9b-7wq2z", pull()),
				b.pod("checkout-84c6f5d9b-mm4kd", pull())),
			b.workload(resource.KindDeployment, "fraud", &resource.Status{Desired: 2, Ready: 1, Updated: 2},
				b.pod("fraud-6f7d8c9b5-2kq8s", running("node-2", "registry.example.com/payments/fraud:0.9.3")),
				b.pod("fraud-6f7d8c9b5-vx9ld", notReady)),
		),
		b.namespace("search", ns(),
			b.workload(resource.KindDeployment, "indexer", &resource.Status{Desired: 2, Ready: 1, Updated: 2},
				b.pod("indexer-9d6b7c5f8-h3n2m", running("node-3", "registry.example.com/search/indexer:3.0.0")),
				b.pod("indexer-9d6b7c5f8-q7w4e", unscheduled)),
		),
		b.namespace("shop", ns(),
			b.workload(resource.KindDeployment, "api", &resource.Status{Desired: 3, Ready: 2, Updated: 1},
				b.pod("api-5f9c7d8b6-9vn2c", running("node-1", "registry.example.com/shop/api:1.7.4")),
				b.pod("api-5f9c7d8b6-tk4mr", running("node-3", "registry.example.com/shop/api:1.7.4")),
				b.pod("api-7b8d9f6c5-zp6qx", crash)),
			b.workload(resource.KindDeployment, "web", &resource.Status{Desired: 2, Ready: 2, Updated: 2},
				b.pod("web-6c5d8f7b9-b4m8n", running("node-1", "registry.example.com/shop/web:4.2.1")),
				b.pod("web-6c5d8f7b9-k2x7v", running("node-2", "registry.example.com/shop/web:4.2.1"))),
			b.workload(resource.KindStatefulSet, "postgres", &resource.Status{Desired: 1, Ready: 1, Updated: 1},
				b.pod("postgres-0", oom)),
			b.workload(resource.KindJob, "migrate-42", &resource.Status{Phase: resource.PhaseFailed},
				b.pod("migrate-42-x8f2k", failed)),
			b.workload(resource.KindCronJob, "nightly-report", &resource.Status{Suspended: true}),
			b.custom(certificate, "web-tls", resource.Condition{Type: "Ready", Status: "False", Reason: "Failed",
				Message: "The certificate request has failed to complete and will be retried: ACME challenge failed"}),
			b.custom(certificate, "api-tls", ready("Ready", "Certificate is up to date and has not expired")),
			b.custom(route, "web"),
			b.custom(sigApp, "storefront", ready("ComponentsReady", "all components ready")),
		),
		b.namespace("legacy-feature", &resource.Status{Created: ago(400 * 24 * time.Hour), Terminating: true}),
		b.clusterScoped(b.custom(clusterIssuer, "letsencrypt", ready("ACMEAccountRegistered", "The ACME account was registered with the ACME server"))),
	}
	resource.Arrange(objects)

	root := &resource.Object{Kind: resource.KindCluster, Name: "kind-dev"}
	statuses[""] = &resource.Status{Created: ago(120 * 24 * time.Hour)}
	return &resource.Cluster{
		Context: "kind-dev",
		Server:  "https://127.0.0.1:6443",
		Version: "v1.34.1",
		Root:    root,
		Objects: objects,
	}
}
