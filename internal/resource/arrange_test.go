package resource

import (
	"strings"
	"testing"
)

func TestArrangeOrdersCustomResourcesAfterWorkloads(t *testing.T) {
	cert := &Type{Kind: "Certificate", Plural: "certificates", Group: "cert-manager.io"}
	route := &Type{Kind: "Route", Plural: "routes", Group: "route.openshift.io"}
	ns := &Object{Kind: KindNamespace, Name: "shop", Children: []*Object{
		{Kind: KindCustom, Type: route, Name: "web"},
		{Kind: KindCustom, Type: cert, Name: "z-tls"},
		{Kind: KindCronJob, Name: "report", Group: "cronjobs"},
		{Kind: KindCustom, Type: cert, Name: "a-tls"},
		{Kind: KindDeployment, Name: "web", Group: "deployments"},
		{Kind: KindDeployment, Name: "api", Group: "deployments"},
	}}
	Arrange([]*Object{ns})
	var got []string
	for _, o := range ns.Children {
		got = append(got, o.Group+":"+o.Name)
	}
	want := []string{
		"deployments:web", "deployments:api", // provider order kept
		"cronjobs:report",
		"custom resources/certificates:a-tls", "custom resources/certificates:z-tls",
		"custom resources/routes:web",
	}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("order = %v, want %v", got, want)
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
	if g := a.Children[0].Group; g != "custom resources/applications argoproj.io" {
		t.Errorf("argo group = %q", g)
	}
	if g := b.Children[0].Group; g != "custom resources/applications app.k8s.io" {
		t.Errorf("sig group = %q", g)
	}
}

func TestArrangePutsClusterScopedAfterNamespaces(t *testing.T) {
	issuer := &Type{Kind: "ClusterIssuer", Plural: "clusterissuers", Group: "cert-manager.io"}
	top := []*Object{
		{Kind: KindCustom, Type: issuer, Name: "letsencrypt"},
		{Kind: KindNamespace, Name: "shop"},
	}
	Arrange(top)
	if top[0].Kind != KindNamespace || top[1].Group != "custom resources/clusterissuers" {
		t.Fatalf("top = %v %q, want the namespace first", top[0].Kind, top[1].Group)
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
