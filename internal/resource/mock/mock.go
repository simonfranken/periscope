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

// Provider serves the fixture cluster of one of its contexts. Every Tree
// builds it anew, with times relative to now.
type Provider struct {
	context  string
	mu       sync.Mutex
	statuses map[string]*resource.Status
}

// Contexts of the mock kubeconfig: kind-dev, the fixture showing every
// word; staging, a small healthy cluster whose context sets a namespace;
// prod-eu, whose login fails like an exec plugin that needs a browser.
var contexts = []resource.Context{
	{Name: "kind-dev", Cluster: "kind-dev", Current: true},
	{Name: "staging", Cluster: "staging-eu", Namespace: "shop"},
	{Name: "prod-eu", Cluster: "prod-eu-1", Namespace: "shop"},
}

// loginDelay is how long prod-eu's login takes to fail: longer than a
// flash, so the pending switch shows. Tests shorten it.
var loginDelay = 3 * time.Second

// New returns the mock provider for the current context, kind-dev.
func New() *Provider { return newFor("kind-dev") }

func newFor(name string) *Provider {
	p := &Provider{context: name}
	if name != "prod-eu" {
		p.Tree(context.Background())
	}
	return p
}

func (p *Provider) Contexts(ctx context.Context) ([]resource.Context, error) {
	out := make([]resource.Context, len(contexts))
	for i, c := range contexts {
		c.Current = c.Name == p.context
		out[i] = c
	}
	return out, nil
}

func (p *Provider) Switch(name string) (resource.Provider, error) {
	for _, c := range contexts {
		if c.Name == name {
			return newFor(name), nil
		}
	}
	return nil, fmt.Errorf("no context %q in the kubeconfig", name)
}

func (p *Provider) Tree(ctx context.Context) (*resource.Cluster, error) {
	statuses := map[string]*resource.Status{}
	var c *resource.Cluster
	switch p.context {
	case "staging":
		c = staging(statuses, time.Now())
	case "prod-eu":
		select { // the plugin takes a while before it gives up
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(loginDelay):
		}
		return nil, fmt.Errorf("getting credentials: exec: kubelogin: interactive login needs a browser, run `kubelogin` in a terminal first")
	default:
		c = fixture(statuses, time.Now())
	}
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

// object is a Service, Ingress, Route, ConfigMap or Secret of kind k.
func (b builder) object(k resource.Kind, name string, st *resource.Status) *resource.Object {
	if st.Created.IsZero() {
		st.Created = b.now.Add(-30 * 24 * time.Hour)
	}
	o := &resource.Object{Kind: k, Name: name}
	b.statuses[o.Ref()] = st
	return o
}

// service selects app=<app>; targets are the workloads that match.
func (b builder) service(name, app string, endpoints int, targets ...string) *resource.Object {
	return b.object(resource.KindService, name, &resource.Status{Type: "ClusterIP", Selector: "app=" + app, Endpoints: endpoints, Targets: targets})
}

func backends(services ...string) []resource.Backend {
	var out []resource.Backend
	for _, s := range services {
		missing := s[0] == '!'
		if missing {
			s = s[1:]
		}
		out = append(out, resource.Backend{Service: s, Missing: missing})
	}
	return out
}

// keys are the keys of a ConfigMap or Secret, all of size n.
func keys(n int, names ...string) []resource.Key {
	var out []resource.Key
	for _, k := range names {
		out = append(out, resource.Key{Name: k, Size: n})
	}
	return out
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

	var helm []*resource.Object // Helm keeps one Secret per release revision
	for i := 1; i <= 3; i++ {
		helm = append(helm, b.object(resource.KindSecret, fmt.Sprintf("sh.helm.release.v1.shop.v%d", i),
			&resource.Status{Type: "helm.sh/release.v1", Keys: keys(14_211, "release"), Created: ago(time.Duration(64-21*i) * 24 * time.Hour)}))
	}
	rootCA := func() *resource.Object {
		return b.object(resource.KindConfigMap, "kube-root-ca.crt", &resource.Status{Keys: keys(1107, "ca.crt")})
	}

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
			b.object(resource.KindService, "kubernetes", &resource.Status{Type: "ClusterIP", Endpoints: 1}),
			b.service("hello", "hello", 1, "deployment/hello"),
			rootCA(),
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
			b.service("grafana", "grafana", 1, "deployment/grafana"),
			b.service("prometheus", "prometheus", 1, "statefulset/prometheus"),
			b.object(resource.KindIngress, "grafana", &resource.Status{Hosts: []string{"grafana.example.com"}, Addresses: []string{"203.0.113.10"}, Backends: backends("grafana")}),
			b.custom(serviceMonitor, "grafana"),
			b.custom(serviceMonitor, "node-exporter"),
			b.object(resource.KindConfigMap, "grafana-dashboards", &resource.Status{Keys: keys(48_310, "cluster.json", "namespaces.json", "nodes.json", "pods.json", "workloads.json")}),
			rootCA(),
		),
		b.namespace("payments", ns(),
			b.workload(resource.KindDeployment, "checkout", &resource.Status{Desired: 2, Ready: 0, Updated: 2},
				b.pod("checkout-84c6f5d9b-7wq2z", pull()),
				b.pod("checkout-84c6f5d9b-mm4kd", pull())),
			b.workload(resource.KindDeployment, "fraud", &resource.Status{Desired: 2, Ready: 1, Updated: 2},
				b.pod("fraud-6f7d8c9b5-2kq8s", running("node-2", "registry.example.com/payments/fraud:0.9.3")),
				b.pod("fraud-6f7d8c9b5-vx9ld", notReady)),
			b.service("checkout", "checkout", 0, "deployment/checkout"),
			b.service("fraud", "fraud", 1, "deployment/fraud"),
			b.object(resource.KindIngress, "checkout", &resource.Status{Hosts: []string{"pay.example.com"}, Addresses: []string{"203.0.113.10"}, Backends: backends("checkout", "!checkout-v2")}),
			b.object(resource.KindRoute, "fraud-admin", &resource.Status{Hosts: []string{"fraud.apps.kind-dev.example.com"}, Rejected: "HostAlreadyClaimed", Backends: backends("fraud")}),
			b.object(resource.KindSecret, "stripe", &resource.Status{Type: "Opaque", Keys: keys(32, "api-key", "webhook-secret")}),
			rootCA(),
		),
		b.namespace("search", ns(),
			b.workload(resource.KindDeployment, "indexer", &resource.Status{Desired: 2, Ready: 1, Updated: 2},
				b.pod("indexer-9d6b7c5f8-h3n2m", running("node-3", "registry.example.com/search/indexer:3.0.0")),
				b.pod("indexer-9d6b7c5f8-q7w4e", unscheduled)),
			b.service("indexer", "indexer", 1, "deployment/indexer"),
			b.object(resource.KindIngress, "search", &resource.Status{Hosts: []string{"search.example.com", "search-admin.example.com"}, Backends: backends("indexer")}),
			rootCA(),
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
			b.workload(resource.KindDeployment, "admin", &resource.Status{}),
			b.service("api", "api", 2, "deployment/api"),
			b.service("web", "web", 2, "deployment/web"),
			b.service("postgres", "postgres", 1, "statefulset/postgres"),
			b.object(resource.KindService, "admin", &resource.Status{Type: "ClusterIP", Selector: "app=admin", Idle: true, Targets: []string{"deployment/admin"}}),
			b.object(resource.KindIngress, "shop", &resource.Status{Hosts: []string{"shop.example.com", "api.shop.example.com"}, Addresses: []string{"203.0.113.10"}, Backends: backends("web", "api")}),
			b.object(resource.KindRoute, "web", &resource.Status{Hosts: []string{"web-shop.apps.kind-dev.example.com"}, Addresses: []string{"router-default.apps.kind-dev.example.com"}, Backends: backends("web")}),
			b.object(resource.KindConfigMap, "api-config", &resource.Status{Keys: []resource.Key{{Name: "LOG_LEVEL", Size: 4}, {Name: "config.yaml", Size: 2_341}, {Name: "features.json", Size: 612}}}),
			rootCA(),
			b.object(resource.KindSecret, "api-tls", &resource.Status{Type: "kubernetes.io/tls", Keys: []resource.Key{{Name: "tls.crt", Size: 3_862}, {Name: "tls.key", Size: 1_704}}}),
			b.object(resource.KindSecret, "postgres", &resource.Status{Type: "Opaque", Keys: keys(24, "password", "username")}),
			b.custom(certificate, "web-tls", resource.Condition{Type: "Ready", Status: "False", Reason: "Failed",
				Message: "The certificate request has failed to complete and will be retried: ACME challenge failed"}),
			b.custom(certificate, "api-tls", ready("Ready", "Certificate is up to date and has not expired")),
			b.custom(sigApp, "storefront", ready("ComponentsReady", "all components ready")),
		),
		b.namespace("legacy-feature", &resource.Status{Created: ago(400 * 24 * time.Hour), Terminating: true}),
		b.clusterScoped(b.custom(clusterIssuer, "letsencrypt", ready("ACMEAccountRegistered", "The ACME account was registered with the ACME server"))),
	}
	for _, h := range helm {
		b.adopt(objects[6], h) // shop
	}
	objects = resource.Arrange(objects)

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

// staging is a small cluster without problems; its context sets the
// namespace shop, so periscope starts inside it.
func staging(statuses map[string]*resource.Status, now time.Time) *resource.Cluster {
	b := builder{statuses: statuses, now: now}
	ns := func() *resource.Status { return &resource.Status{Created: now.Add(-200 * 24 * time.Hour)} }
	objects := resource.Arrange([]*resource.Object{
		b.namespace("default", ns()),
		b.namespace("shop", ns(),
			b.workload(resource.KindDeployment, "api", &resource.Status{Desired: 2, Ready: 2, Updated: 2},
				b.pod("api-6c9d8b7f5-4kq2m", running("node-a", "registry.example.com/shop/api:1.7.4")),
				b.pod("api-6c9d8b7f5-t8w3x", running("node-b", "registry.example.com/shop/api:1.7.4"))),
			b.workload(resource.KindDeployment, "web", &resource.Status{Desired: 1, Ready: 1, Updated: 1},
				b.pod("web-5b8c7d6f4-p2r9z", running("node-a", "registry.example.com/shop/web:4.2.1"))),
			b.service("api", "api", 2, "deployment/api"),
			b.service("web", "web", 1, "deployment/web"),
			b.object(resource.KindIngress, "shop", &resource.Status{Hosts: []string{"staging.shop.example.com"}, Addresses: []string{"198.51.100.7"}, Backends: backends("web", "api")}),
			b.object(resource.KindConfigMap, "api-config", &resource.Status{Keys: keys(120, "LOG_LEVEL", "config.yaml")}),
		),
	})
	statuses[""] = &resource.Status{Created: now.Add(-300 * 24 * time.Hour)}
	return &resource.Cluster{
		Context:   "staging",
		Server:    "https://staging.example.com:6443",
		Version:   "v1.33.4",
		Namespace: "shop",
		Root:      &resource.Object{Kind: resource.KindCluster, Name: "staging"},
		Objects:   objects,
	}
}
