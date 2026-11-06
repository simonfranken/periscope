package kube

import (
	"context"
	"fmt"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"periscope/internal/resource"
)

func labeled(m metav1.ObjectMeta, labels map[string]string) metav1.ObjectMeta {
	m.Labels = labels
	return m
}

func template(app string) corev1.PodTemplateSpec {
	return corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": app}}}
}

func backend(svc string) networkingv1.IngressBackend {
	return networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{Name: svc}}
}

func paths1(bs ...networkingv1.IngressBackend) *networkingv1.HTTPIngressRuleValue {
	v := &networkingv1.HTTPIngressRuleValue{}
	for _, b := range bs {
		v.Paths = append(v.Paths, networkingv1.HTTPIngressPath{Path: "/", Backend: b})
	}
	return v
}

// networkFixture is a namespace with a Deployment web and its ready pod, a
// Deployment admin scaled to zero, their Services and one that selects
// nothing, two Ingresses, a ConfigMap and a Secret.
func networkFixture() []runtime.Object {
	web := map[string]string{"app": "web"}
	yes := true
	ready := discoveryv1.EndpointConditions{Ready: &yes}
	podRef := &corev1.ObjectReference{Kind: "Pod", Name: "web-1-x", UID: "p-web"}
	return []runtime.Object{
		&corev1.Namespace{ObjectMeta: meta("", "shop", "ns-shop")},
		&appsv1.Deployment{ObjectMeta: meta("shop", "web", "d-web"), Spec: appsv1.DeploymentSpec{Replicas: ptr(int32(1)), Template: template("web")},
			Status: appsv1.DeploymentStatus{ReadyReplicas: 1, UpdatedReplicas: 1}},
		&appsv1.Deployment{ObjectMeta: meta("shop", "admin", "d-admin"), Spec: appsv1.DeploymentSpec{Replicas: ptr(int32(0)), Template: template("admin")}},
		&appsv1.ReplicaSet{ObjectMeta: meta("shop", "web-1", "rs-web", controlledBy("Deployment", "web", "d-web"))},
		&corev1.Pod{ObjectMeta: labeled(meta("shop", "web-1-x", "p-web", controlledBy("ReplicaSet", "web-1", "rs-web")), web),
			Status: corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}}},
		&corev1.Service{ObjectMeta: meta("shop", "web", "s-web"), Spec: corev1.ServiceSpec{Selector: web}},
		&corev1.Service{ObjectMeta: meta("shop", "admin", "s-admin"), Spec: corev1.ServiceSpec{Selector: map[string]string{"app": "admin"}}},
		&corev1.Service{ObjectMeta: meta("shop", "typo", "s-typo"), Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeNodePort, Selector: map[string]string{"app": "wbe"}}},
		&corev1.Service{ObjectMeta: meta("shop", "db", "s-db"), Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeExternalName, ExternalName: "db.example.com"}},
		// dual-stack: the pod is in two slices
		&discoveryv1.EndpointSlice{ObjectMeta: labeled(meta("shop", "web-v4", "es-4"), map[string]string{discoveryv1.LabelServiceName: "web"}),
			Endpoints: []discoveryv1.Endpoint{{Addresses: []string{"10.0.0.1"}, Conditions: ready, TargetRef: podRef}}},
		&discoveryv1.EndpointSlice{ObjectMeta: labeled(meta("shop", "web-v6", "es-6"), map[string]string{discoveryv1.LabelServiceName: "web"}),
			Endpoints: []discoveryv1.Endpoint{{Addresses: []string{"fd00::1"}, Conditions: ready, TargetRef: podRef}}},
		&networkingv1.Ingress{ObjectMeta: meta("shop", "shop", "i-shop"), Spec: networkingv1.IngressSpec{
			DefaultBackend: &networkingv1.IngressBackend{Resource: &corev1.TypedLocalObjectReference{Kind: "Bucket", Name: "static"}},
			Rules: []networkingv1.IngressRule{
				{Host: "shop.example.com", IngressRuleValue: networkingv1.IngressRuleValue{HTTP: paths1(backend("web"), backend("web"))}},
				{IngressRuleValue: networkingv1.IngressRuleValue{HTTP: paths1(backend("gone"))}},
			}},
			Status: networkingv1.IngressStatus{LoadBalancer: networkingv1.IngressLoadBalancerStatus{Ingress: []networkingv1.IngressLoadBalancerIngress{{IP: "203.0.113.1"}}}}},
		&networkingv1.Ingress{ObjectMeta: meta("shop", "new", "i-new"), Spec: networkingv1.IngressSpec{DefaultBackend: ptr(backend("web"))}},
		&corev1.ConfigMap{ObjectMeta: meta("shop", "conf", "cm-conf"), Data: map[string]string{"b": "12345", "a": "1"}, BinaryData: map[string][]byte{"c": {1, 2}}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: "shop", Name: "tls", UID: "sec-tls", CreationTimestamp: created,
			Annotations: map[string]string{"kubectl.kubernetes.io/last-applied-configuration": `{"data":{"tls.key":"c2VjcmV0"}}`}},
			Type: corev1.SecretTypeTLS, Data: map[string][]byte{"tls.crt": []byte("cert"), "tls.key": []byte("secret")}},
		&corev1.Event{ObjectMeta: meta("shop", "e1", "e1"), Type: corev1.EventTypeWarning, Reason: "SyncLoadBalancerFailed",
			InvolvedObject: corev1.ObjectReference{Kind: "Service", Namespace: "shop", Name: "typo", UID: "s-typo"}},
	}
}

func routeObject(name, to string, admitted string, reason string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "route.openshift.io/v1",
		"kind":       "Route",
		"metadata":   map[string]any{"namespace": "shop", "name": name, "uid": "r-" + name},
		"spec":       map[string]any{"host": name + ".apps.example.com", "to": map[string]any{"kind": "Service", "name": to}},
		"status": map[string]any{"ingress": []any{map[string]any{
			"routerName": "default", "routerCanonicalHostname": "router-default.apps.example.com",
			"conditions": []any{map[string]any{"type": "Admitted", "status": admitted, "reason": reason}},
		}}},
	}}
	return u
}

func dynamicClient(objs ...runtime.Object) *dynamicfake.FakeDynamicClient {
	return dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(),
		map[schema.GroupVersionResource]string{routeGVR: "RouteList"}, objs...)
}

func networkProvider(c *fake.Clientset) *Provider {
	p := provider(c, "")
	p.dyn = dynamicClient(routeObject("web", "web", "True", ""), routeObject("taken", "gone", "False", "HostAlreadyClaimed"))
	return p
}

func statuses(t *testing.T, p *Provider) (map[string]*resource.Object, func(string) *resource.Status) {
	t.Helper()
	c, err := p.Tree(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return paths(c), func(path string) *resource.Status {
		t.Helper()
		st, err := p.Status(context.Background(), path)
		if err != nil {
			t.Fatal(err)
		}
		return st
	}
}

func issueKinds(st *resource.Status) []resource.IssueKind {
	var out []resource.IssueKind
	for _, is := range st.Issues() {
		out = append(out, is.Kind)
	}
	return out
}

func TestNetworkAndConfig(t *testing.T) {
	got, get := statuses(t, networkProvider(fake.NewClientset(networkFixture()...)))
	for path, group := range map[string]string{
		"shop/service/web":    "services",
		"shop/ingress/shop":   "ingresses",
		"shop/route/web":      "routes",
		"shop/configmap/conf": "configmaps",
		"shop/secret/tls":     "secrets",
	} {
		if o, ok := got[path]; !ok || o.Group != group {
			t.Errorf("%s: %+v, want it in group %s", path, o, group)
		}
	}
	var order []string
	for _, o := range got["shop"].Children {
		order = append(order, o.Ref())
	}
	want := "[deployment/admin deployment/web service/admin service/db service/typo service/web ingress/new ingress/shop route/taken route/web configmap/conf secret/tls]"
	if g := fmtList(order); g != want {
		t.Errorf("shop's children = %s, want %s", g, want)
	}

	web := get("shop/service/web")
	if web.Type != "ClusterIP" || web.Selector != "app=web" || web.Endpoints != 1 || fmtList(web.Targets) != "[deployment/web]" || len(web.Issues()) != 0 {
		t.Errorf("service web = %+v", web)
	}
	if st := get("shop/service/admin"); !st.Idle || len(st.Issues()) != 0 || fmtList(st.Targets) != "[deployment/admin]" {
		t.Errorf("service of a workload scaled to zero = %+v", st)
	}
	typo := get("shop/service/typo")
	if typo.Type != "NodePort" || fmtList(issueKinds(typo)) != fmtList([]resource.IssueKind{resource.IssueNoEndpoints}) || len(typo.Events) != 1 {
		t.Errorf("service selecting nothing = %+v", typo)
	}
	if st := get("shop/service/db"); st.Type != "ExternalName" || st.Selector != "" || len(st.Issues()) != 0 {
		t.Errorf("external name = %+v", st)
	}

	in := get("shop/ingress/shop")
	if fmtList(in.Hosts) != "[shop.example.com *]" || fmtList(in.Addresses) != "[203.0.113.1]" ||
		fmtList(in.Backends) != "[{web false} {gone true}]" {
		t.Errorf("ingress = %+v", in)
	}
	if st := get("shop/ingress/new"); fmtList(issueKinds(st)) != fmtList([]resource.IssueKind{resource.IssueNoAddress}) || fmtList(st.Hosts) != "[*]" {
		t.Errorf("ingress without address = %+v", st)
	}
	if st := get("shop/route/web"); fmtList(st.Addresses) != "[router-default.apps.example.com]" || len(st.Issues()) != 0 {
		t.Errorf("route = %+v", st)
	}
	taken := get("shop/route/taken")
	if taken.Rejected != "HostAlreadyClaimed" || fmtList(issueKinds(taken)) != fmtList([]resource.IssueKind{resource.IssueBackendMissing, resource.IssueNoAddress}) {
		t.Errorf("rejected route = %+v", taken)
	}

	if st := get("shop/configmap/conf"); fmtList(st.Keys) != "[{a 1} {b 5} {c 2}]" {
		t.Errorf("configmap keys = %+v", st.Keys)
	}
	if st := get("shop/secret/tls"); st.Type != "kubernetes.io/tls" || fmtList(st.Keys) != "[{tls.crt 4} {tls.key 6}]" {
		t.Errorf("secret = %+v", st)
	}
}

// Where Services can't be listed, no backend is missing; where
// EndpointSlices can't, the ready pods a Service selects count instead.
func TestNetworkForbidden(t *testing.T) {
	c := fake.NewClientset(networkFixture()...)
	forbid(c, "endpointslices", false)
	p := networkProvider(c)
	_, get := statuses(t, p)
	if st := get("shop/service/web"); st.Endpoints != 1 || len(st.Issues()) != 0 {
		t.Errorf("service web from its pods = %+v", st)
	}
	if st := get("shop/service/typo"); len(st.Issues()) != 1 {
		t.Errorf("service selecting nothing, from pods = %+v", st)
	}

	forbid(c, "services", false)
	got, get := statuses(t, p)
	if _, ok := got["shop/service/web"]; ok {
		t.Error("listed a Service that may not be listed")
	}
	if st := get("shop/ingress/shop"); len(st.Issues()) != 0 {
		t.Errorf("ingress whose Services can't be listed = %+v, want no issue", st)
	}
}

// A cluster without OpenShift doesn't serve Routes.
func TestRoutesNotServed(t *testing.T) {
	p := provider(fake.NewClientset(networkFixture()...), "")
	dyn := dynamicClient()
	dyn.PrependReactor("list", "routes", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewNotFound(routeGVR.GroupResource(), "")
	})
	p.dyn = dyn
	got, _ := statuses(t, p)
	if _, ok := got["shop/service/web"]; !ok {
		t.Errorf("tree without Routes = %v", got)
	}
}

func fmtList[T any](xs []T) string { return fmt.Sprint(xs) }
