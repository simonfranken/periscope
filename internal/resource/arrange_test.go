package resource

import (
	"strings"
	"testing"
)

var (
	cert  = &Type{Kind: "Certificate", Plural: "certificates", Group: "cert-manager.io"}
	route = &Type{Kind: "Route", Plural: "routes", Group: "route.openshift.io"}
)

func names(objs []*Object) string {
	var out []string
	for _, o := range objs {
		out = append(out, o.Group+":"+o.Name)
	}
	return strings.Join(out, " ")
}

func TestArrangeMovesCustomResourcesIntoSection(t *testing.T) {
	ns := &Object{Kind: KindNamespace, Name: "shop", Path: "shop", Children: []*Object{
		{Kind: KindCustom, Type: route, Name: "web"},
		{Kind: KindCustom, Type: cert, Name: "z-tls"},
		{Kind: KindCronJob, Name: "report", Group: "cronjobs"},
		{Kind: KindCustom, Type: cert, Name: "a-tls"},
		{Kind: KindDeployment, Name: "web", Group: "deployments"},
		{Kind: KindDeployment, Name: "api", Group: "deployments"},
	}}
	Arrange([]*Object{ns})
	// provider order kept within a built-in kind; the section last
	if got, want := names(ns.Children), "deployments:web deployments:api cronjobs:report :custom resources"; got != want {
		t.Fatalf("namespace = %v, want %v", got, want)
	}
	s := ns.Children[3]
	if s.Kind != KindSection || s.Path != "shop/custom resources" || s.Namespace != "shop" || s.Parent != ns || s.Status == nil {
		t.Fatalf("section = %+v", s)
	}
	if got, want := names(s.Children), "certificates:a-tls certificates:z-tls routes:web"; got != want {
		t.Fatalf("section = %v, want %v", got, want)
	}
	for _, c := range s.Children {
		if c.Parent != s {
			t.Errorf("%s: parent is not the section", c.Name)
		}
	}
}

func TestArrangeWithoutCustomResourcesAddsNoSection(t *testing.T) {
	ns := &Object{Kind: KindNamespace, Name: "shop", Children: []*Object{{Kind: KindDeployment, Name: "api"}}}
	top := Arrange([]*Object{ns})
	if len(top) != 1 || len(ns.Children) != 1 {
		t.Fatalf("top = %d objects, namespace = %d children, want 1 and 1", len(top), len(ns.Children))
	}
}

func TestArrangeAddsAPIGroupToSharedPlurals(t *testing.T) {
	argo := &Type{Kind: "Application", Plural: "applications", Group: "argoproj.io"}
	sig := &Type{Kind: "Application", Plural: "applications", Group: "app.k8s.io"}
	// the plurals collide across namespaces: both get their API group, also
	// in a namespace that has only one of them
	a := &Object{Kind: KindNamespace, Name: "argocd", Children: []*Object{{Kind: KindCustom, Type: argo, Name: "shop"}}}
	b := &Object{Kind: KindNamespace, Name: "shop", Children: []*Object{{Kind: KindCustom, Type: sig, Name: "store"}}}
	Arrange([]*Object{a, b})
	if g := a.Children[0].Children[0].Group; g != "applications argoproj.io" {
		t.Errorf("argo group = %q", g)
	}
	if g := b.Children[0].Children[0].Group; g != "applications app.k8s.io" {
		t.Errorf("sig group = %q", g)
	}
}

func TestArrangePutsClusterScopedSectionAfterNamespaces(t *testing.T) {
	issuer := &Type{Kind: "ClusterIssuer", Plural: "clusterissuers", Group: "cert-manager.io"}
	top := Arrange([]*Object{
		{Kind: KindCustom, Type: issuer, Name: "letsencrypt"},
		{Kind: KindNamespace, Name: "shop"},
	})
	if len(top) != 2 || top[0].Kind != KindNamespace || top[1].Kind != KindSection || top[1].Path != "custom resources" || top[1].Group != ClusterGroup || top[1].Parent != nil {
		t.Fatalf("top = %v", names(top))
	}
	if g := top[1].Children[0].Group; g != "clusterissuers" {
		t.Errorf("issuer group = %q", g)
	}
}

func TestReadyConditionFalseIsNotReady(t *testing.T) {
	cases := []struct {
		cond []Condition
		want bool
	}{
		{nil, false},
		{[]Condition{{Type: "Ready", Status: "True"}}, false},
		{[]Condition{{Type: "Ready", Status: "Unknown"}}, false},
		{[]Condition{{Type: "Synced", Status: "False"}}, false},
		{[]Condition{{Type: "Ready", Status: "False", Reason: "Failed"}}, true},
	}
	for _, c := range cases {
		is := (&Status{Conditions: c.cond}).Issues()
		got := len(is) == 1 && is[0].Kind == IssueNotReady
		if got != c.want {
			t.Errorf("conditions %v: not ready = %v, want %v", c.cond, got, c.want)
		}
	}
}
