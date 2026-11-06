package kube

import (
	"cmp"
	"maps"
	"slices"
	"strings"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"periscope/internal/resource"
)

// The status of Services, Ingresses, Routes, ConfigMaps and Secrets, as
// docs/state-model.md#raw-dimensions lists it.

// routeGVR is OpenShift's Route. Clusters without OpenShift don't serve it,
// so its list is empty there.
var routeGVR = schema.GroupVersionResource{Group: "route.openshift.io", Version: "v1", Resource: "routes"}

// route is the part of a Route periscope reads; it is listed with the
// dynamic client, so periscope needs no OpenShift API module.
type route struct {
	Metadata metav1.ObjectMeta `json:"metadata"`
	Spec     struct {
		Host              string        `json:"host"`
		To                routeTarget   `json:"to"`
		AlternateBackends []routeTarget `json:"alternateBackends"`
	} `json:"spec"`
	Status struct {
		Ingress []struct {
			Host                    string `json:"host"`
			RouterName              string `json:"routerName"`
			RouterCanonicalHostname string `json:"routerCanonicalHostname"`
			Conditions              []struct {
				Type    string `json:"type"`
				Status  string `json:"status"`
				Reason  string `json:"reason"`
				Message string `json:"message"`
			} `json:"conditions"`
		} `json:"ingress"`
	} `json:"status"`
}

type routeTarget struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

// routes converts a page of Routes; one that doesn't parse is left out.
func routes(r *unstructured.UnstructuredList) ([]route, string) {
	var out []route
	for _, u := range r.Items {
		var rt route
		if runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &rt) == nil {
			out = append(out, rt)
		}
	}
	return out, r.GetContinue()
}

// keyed is a ConfigMap or Secret as far as periscope reads it: who it is,
// its type and its keys with the sizes of their values. It is made when a
// page is listed, so the values (and annotations such as
// last-applied-configuration, which hold them too) are never kept (F64).
type keyed struct {
	meta metav1.ObjectMeta
	typ  string
	keys []resource.Key
}

// identity keeps of m only what snapshot.add reads.
func identity(m *metav1.ObjectMeta) metav1.ObjectMeta {
	return metav1.ObjectMeta{Namespace: m.Namespace, Name: m.Name, UID: m.UID, CreationTimestamp: m.CreationTimestamp}
}

func configMaps(r *corev1.ConfigMapList) ([]keyed, string) {
	out := make([]keyed, 0, len(r.Items))
	for i := range r.Items {
		c := &r.Items[i]
		k := keyed{meta: identity(&c.ObjectMeta)}
		for name, v := range c.Data {
			k.keys = append(k.keys, resource.Key{Name: name, Size: len(v)})
		}
		for name, v := range c.BinaryData {
			k.keys = append(k.keys, resource.Key{Name: name, Size: len(v)})
		}
		out = append(out, k)
	}
	return out, r.Continue
}

func secrets(r *corev1.SecretList) ([]keyed, string) {
	out := make([]keyed, 0, len(r.Items))
	for i := range r.Items {
		s := &r.Items[i]
		k := keyed{meta: identity(&s.ObjectMeta), typ: string(s.Type)}
		for name, v := range s.Data {
			k.keys = append(k.keys, resource.Key{Name: name, Size: len(v)})
		}
		out = append(out, k)
	}
	return out, r.Continue
}

func keyedStatus(k *keyed) *resource.Status {
	keys := slices.Clone(k.keys)
	slices.SortFunc(keys, func(a, b resource.Key) int { return cmp.Compare(a.Name, b.Name) })
	return &resource.Status{Type: k.typ, Keys: keys}
}

// selects is what a Service's selector may match in a namespace: its pods
// and the workloads by their pod template, with the top-level object of
// each pod in the tree (its workload, or the pod itself).
type selects struct {
	pods      []selectedPod
	workloads []selectedWorkload
}

type selectedPod struct {
	labels map[string]string
	ready  bool   // the pod's Ready condition is True
	target string // the ref of its workload, or its own
}

type selectedWorkload struct {
	labels  map[string]string // of its pod template
	ref     string
	desired int
}

// endpoints counts the ready endpoints of each Service, by namespace and
// name. A pod is counted once, though dual-stack lists it in two slices.
func endpoints(sls []discoveryv1.EndpointSlice) map[string]int {
	seen := map[string]bool{}
	out := map[string]int{}
	for _, sl := range sls {
		svc := sl.Labels[discoveryv1.LabelServiceName]
		if svc == "" {
			continue
		}
		key := sl.Namespace + "/" + svc
		for _, e := range sl.Endpoints {
			if e.Conditions.Ready != nil && !*e.Conditions.Ready {
				continue
			}
			id := ""
			switch {
			case e.TargetRef != nil:
				id = string(e.TargetRef.UID) + e.TargetRef.Name
			case len(e.Addresses) > 0:
				id = e.Addresses[0]
			}
			if id != "" && seen[key+"/"+id] {
				continue
			}
			seen[key+"/"+id] = true
			out[key]++
		}
	}
	return out
}

// serviceStatus reads a Service's type and selector; ready is its count of
// ready endpoints, or nil if the EndpointSlices of its namespace can't be
// listed, when the ready pods it selects are counted instead.
func serviceStatus(svc *corev1.Service, sel *selects, ready *int) *resource.Status {
	st := &resource.Status{Type: cmp.Or(string(svc.Spec.Type), string(corev1.ServiceTypeClusterIP))}
	if ready != nil {
		st.Endpoints = *ready
	}
	if len(svc.Spec.Selector) == 0 || svc.Spec.Type == corev1.ServiceTypeExternalName {
		return st
	}
	var parts []string
	for _, k := range slices.Sorted(maps.Keys(svc.Spec.Selector)) {
		parts = append(parts, k+"="+svc.Spec.Selector[k])
	}
	st.Selector = strings.Join(parts, ",")
	selector := labels.SelectorFromSet(svc.Spec.Selector)
	readyPods := 0
	for _, p := range sel.pods {
		if selector.Matches(labels.Set(p.labels)) {
			if p.ready {
				readyPods++
			}
			if !slices.Contains(st.Targets, p.target) {
				st.Targets = append(st.Targets, p.target)
			}
		}
	}
	if ready == nil {
		st.Endpoints = readyPods
	}
	matched, idle := false, true
	for _, w := range sel.workloads {
		if selector.Matches(labels.Set(w.labels)) {
			matched = true
			idle = idle && w.desired == 0
			if !slices.Contains(st.Targets, w.ref) {
				st.Targets = append(st.Targets, w.ref)
			}
		}
	}
	st.Idle = st.Endpoints == 0 && matched && idle
	return st
}

// ingressStatus reads an Ingress's hosts ("*" for a rule without one),
// load balancer addresses and the Services of its rules and default
// backend; exists reports whether a Service exists, nil if the Services of
// the namespace can't be listed, when none counts as missing.
func ingressStatus(in *networkingv1.Ingress, exists func(string) bool) *resource.Status {
	st := &resource.Status{}
	for _, r := range in.Spec.Rules {
		if h := cmp.Or(r.Host, "*"); !slices.Contains(st.Hosts, h) {
			st.Hosts = append(st.Hosts, h)
		}
	}
	if len(st.Hosts) == 0 {
		st.Hosts = []string{"*"} // a default backend only
	}
	for _, lb := range in.Status.LoadBalancer.Ingress {
		if a := cmp.Or(lb.IP, lb.Hostname); a != "" {
			st.Addresses = append(st.Addresses, a)
		}
	}
	backend := func(b *networkingv1.IngressBackend) {
		if b == nil || b.Service == nil { // a resource backend is no Service
			return
		}
		st.Backends = addBackend(st.Backends, b.Service.Name, exists)
	}
	backend(in.Spec.DefaultBackend)
	for _, r := range in.Spec.Rules {
		if r.HTTP != nil {
			for _, p := range r.HTTP.Paths {
				backend(&p.Backend)
			}
		}
	}
	return st
}

// routeStatus reads a Route's host, the routers that admitted it (their
// canonical host names, else their names), why one rejected it if none
// did, and the Services it routes to.
func routeStatus(r *route, exists func(string) bool) *resource.Status {
	st := &resource.Status{}
	if r.Spec.Host != "" {
		st.Hosts = []string{r.Spec.Host}
	}
	for _, in := range r.Status.Ingress {
		if len(st.Hosts) == 0 && in.Host != "" {
			st.Hosts = []string{in.Host}
		}
		for _, c := range in.Conditions {
			if c.Type != "Admitted" {
				continue
			}
			switch c.Status {
			case "True":
				st.Addresses = append(st.Addresses, cmp.Or(in.RouterCanonicalHostname, in.RouterName))
			case "False":
				st.Rejected = cmp.Or(st.Rejected, c.Reason)
			}
		}
	}
	if len(st.Hosts) == 0 {
		st.Hosts = []string{"*"}
	}
	if len(st.Addresses) > 0 {
		st.Rejected = ""
	}
	for _, t := range append([]routeTarget{r.Spec.To}, r.Spec.AlternateBackends...) {
		if t.Name != "" && cmp.Or(t.Kind, "Service") == "Service" {
			st.Backends = addBackend(st.Backends, t.Name, exists)
		}
	}
	return st
}

func addBackend(bs []resource.Backend, name string, exists func(string) bool) []resource.Backend {
	if slices.ContainsFunc(bs, func(b resource.Backend) bool { return b.Service == name }) {
		return bs
	}
	return append(bs, resource.Backend{Service: name, Missing: exists != nil && !exists(name)})
}

// podReady reports whether a pod's Ready condition is True, as the
// endpoints controller decides.
func podReady(p *corev1.Pod) bool {
	for _, c := range p.Status.Conditions {
		if c.Type == corev1.PodReady {
			return c.Status == corev1.ConditionTrue
		}
	}
	return false
}
