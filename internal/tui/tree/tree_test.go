package tree

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	zone "github.com/lrstanley/bubblezone"

	"periscope/internal/resource"
	"periscope/internal/tui/theme"
)

func TestMain(m *testing.M) {
	zone.NewGlobal()
	os.Exit(m.Run())
}

// fixture:
//
//	a
//	src/
//	  b
//	    modules/
//	      c
//	        d
//	      e
//	  f
func fixture() []*resource.Object {
	mk := func(path, rel string, parent *resource.Object) *resource.Object {
		group := ""
		if i := strings.LastIndex(rel, "/"); i >= 0 {
			group = rel[:i]
		}
		s := &resource.Object{Name: path, Path: path, Group: group, Parent: parent}
		if parent != nil {
			parent.Children = append(parent.Children, s)
		}
		return s
	}
	a := mk("a", "a", nil)
	b := mk("b", "src/b", nil)
	c := mk("c", "modules/c", b)
	mk("d", "d", c)
	mk("e", "modules/e", b)
	f := mk("f", "src/f", nil)
	return []*resource.Object{a, b, f}
}

// inNamespace puts objs into a namespace "ns", the only top-level object.
func inNamespace(objs []*resource.Object) []*resource.Object {
	ns := &resource.Object{Kind: resource.KindNamespace, Name: "ns", Path: "ns", Children: objs}
	for _, o := range objs {
		o.Parent = ns
	}
	return []*resource.Object{ns}
}

// newTree shows the fixture inside the namespace "ns": the navigation
// tests run inside a namespace, where objects expand in place.
func newTree(t *testing.T) *Model {
	t.Helper()
	m := New(theme.Default())
	m.SetRoots(inNamespace(fixture()))
	m.SetSize(80, 20)
	m.EnterPath("ns")
	return &m
}

func assertCursor(t *testing.T, m *Model, want string) {
	t.Helper()
	if got := m.Selected().Path; got != want {
		t.Fatalf("cursor = %q, want %q", got, want)
	}
}

func visible(m *Model) []string {
	var out []string
	for _, r := range m.rows() {
		if r.kind == rowDir {
			out = append(out, r.label)
		} else {
			out = append(out, r.node.Path)
		}
	}
	return out
}

func TestReplaceRootsKeepsState(t *testing.T) {
	m := newTree(t)
	m.Down()  // b
	m.Right() // expand b, cursor on c
	m.Right() // expand c, cursor on d
	assertCursor(t, m, "d")

	m.ReplaceRoots(inNamespace(fixture())) // same paths, new nodes
	assertCursor(t, m, "d")
	if got := len(visible(m)); got != 8 {
		t.Errorf("visible rows = %v, want b and c still expanded", visible(m))
	}

	m.ReplaceRoots(inNamespace(fixture()[:1])) // only "a" left
	assertCursor(t, m, "a")
}

func TestInitiallyCollapsedWithHeaders(t *testing.T) {
	m := newTree(t)
	got := visible(m)
	want := []string{"a", "src", "b", "f"}
	if len(got) != len(want) {
		t.Fatalf("rows = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("rows = %v, want %v", got, want)
		}
	}
	assertCursor(t, m, "a")
}

func TestDownSkipsHeaders(t *testing.T) {
	m := newTree(t)
	m.Down()
	assertCursor(t, m, "b")
	m.Down()
	assertCursor(t, m, "f")
	m.Down() // stays at end
	assertCursor(t, m, "f")
	m.Up()
	m.Up()
	assertCursor(t, m, "a")
	m.Up() // the header, which stands for the namespace
	assertCursor(t, m, "ns")
	m.Up() // stays at start
	assertCursor(t, m, "ns")
}

func TestRightExpandsAndSelectsFirstChild(t *testing.T) {
	m := newTree(t)
	m.Down()
	m.Right()
	assertCursor(t, m, "c")
	if !m.expanded["b"] {
		t.Fatal("b should be expanded")
	}
	m.Right()
	assertCursor(t, m, "d")
	m.Right() // leaf: no-op
	assertCursor(t, m, "d")
}

func TestRightOnLeafIsNoop(t *testing.T) {
	m := newTree(t)
	m.Right()
	assertCursor(t, m, "a")
}

func TestLeftCollapsesParentAndSelectsIt(t *testing.T) {
	m := newTree(t)
	m.Down()
	m.Right() // b expanded, cursor c
	m.Down()  // e
	assertCursor(t, m, "e")
	m.Left()
	assertCursor(t, m, "b")
	if m.expanded["b"] {
		t.Fatal("b should be collapsed")
	}
}

func TestLeftOnExpandedNodeCollapsesIt(t *testing.T) {
	m := newTree(t)
	m.Down()
	m.Toggle() // expand b, cursor stays
	assertCursor(t, m, "b")
	m.Left()
	assertCursor(t, m, "b")
	if m.expanded["b"] {
		t.Fatal("b should be collapsed")
	}
	m.Left() // top level and collapsed: back to the cluster level
	assertCursor(t, m, "ns")
	if m.Scope() != nil {
		t.Fatal("Left on a collapsed top-level object should leave the namespace")
	}
}

func TestExpansionStateKept(t *testing.T) {
	m := newTree(t)
	m.Down()
	m.Right()  // c
	m.Right()  // d, c expanded
	m.Left()   // collapse c, select c
	m.Right()  // d again
	m.Left()   // c
	m.Toggle() // re-expand c
	m.Left()   // c is expanded -> collapse c
	m.Toggle() // expand c
	m.Up()     // b
	m.Toggle() // collapse b
	m.Toggle() // expand b: c should still be expanded
	got := visible(m)
	want := []string{"a", "src", "b", "modules", "c", "d", "e", "f"}
	if len(got) != len(want) {
		t.Fatalf("rows = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("rows = %v, want %v", got, want)
		}
	}
}

func TestCollapsingAncestorMovesCursor(t *testing.T) {
	m := newTree(t)
	m.Down()
	m.Right()
	m.Right() // cursor d
	m.setExpanded(m.scope.Children[1], false)
	assertCursor(t, m, "b")
}

func TestScrollKeepsCursorVisible(t *testing.T) {
	m := newTree(t)
	m.SetSize(80, 2)
	m.Down()
	m.Right() // rows: a src/ b modules/ c e f ; cursor c (index 4)
	if i := m.cursorRow(m.rows()); i < m.offset || i >= m.offset+m.height {
		t.Fatalf("cursor row %d not in [%d,%d)", i, m.offset, m.offset+m.height)
	}
	m.Home()
	if m.offset != 0 {
		t.Fatalf("offset = %d after Home, want 0", m.offset)
	}
}

func TestExpandBelowKeepsSelectionAndSiblings(t *testing.T) {
	m := newTree(t)
	m.Down() // b
	m.ExpandBelow()
	assertCursor(t, m, "b")
	assertRows(t, m, "a", "src", "b", "modules", "c", "d", "e", "f")
	if m.expanded["a"] || m.expanded["f"] {
		t.Fatal("siblings must stay collapsed")
	}
}

func TestExpandBelowOnLeafIsNoop(t *testing.T) {
	m := newTree(t) // cursor a, a leaf
	m.ExpandBelow()
	assertCursor(t, m, "a")
	if m.expanded["b"] || m.expanded["c"] {
		t.Fatal("a leaf has nothing below to expand")
	}
}

func TestCollapseBelowForgetsExpansion(t *testing.T) {
	m := newTree(t)
	m.Down() // b
	m.ExpandBelow()
	m.CollapseBelow()
	assertCursor(t, m, "b")
	assertRows(t, m, "a", "src", "b", "f")
	m.Toggle() // re-expanding b must not restore c's expansion
	if m.expanded["c"] {
		t.Fatal("c should be collapsed after CollapseBelow on b")
	}
}

func TestCollapseBelowClimbsFromLeaf(t *testing.T) {
	m := newTree(t)
	m.Down()
	m.Right() // c
	m.Right() // d, a leaf
	m.CollapseBelow()
	assertCursor(t, m, "c")
	if m.expanded["c"] || !m.expanded["b"] {
		t.Fatal("CollapseBelow on a leaf collapses only its parent")
	}
	m.CollapseBelow() // c is collapsed now: climbs to b
	assertCursor(t, m, "b")
	assertRows(t, m, "a", "src", "b", "f")
}

func TestCollapseBelowKeepsSiblings(t *testing.T) {
	m := newTree(t)
	m.Down() // b
	m.ExpandBelow()
	m.Down() // c
	m.CollapseBelow()
	assertCursor(t, m, "c")
	assertRows(t, m, "a", "src", "b", "modules", "c", "e", "f")
}

func assertRows(t *testing.T, m *Model, want ...string) {
	t.Helper()
	if got := visible(m); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("rows = %v, want %v", got, want)
	}
}

func TestFilterShowsMatchesWithAncestors(t *testing.T) {
	m := newTree(t)
	m.SetFilter("D")
	want := []string{"src", "b", "modules", "c", "d"}
	if got := visible(m); !equal(got, want) {
		t.Fatalf("rows = %v, want %v", got, want)
	}
	assertCursor(t, m, "b") // a was selected and is gone: next available (an ancestor of the match)
}

func TestFilterSelectionMovesToNextThenPrevious(t *testing.T) {
	m := newTree(t)
	m.Select("a")
	m.SetFilter("f")
	assertCursor(t, m, "f")
	m.SetFilter("")
	m.Select("f")
	m.SetFilter("a") // only a matches, it lies before f
	assertCursor(t, m, "a")
}

func TestFilterWithoutMatchSelectsNothing(t *testing.T) {
	m := newTree(t)
	m.SetFilter("zzz")
	if !m.RootSelected() || len(m.rows()) != 0 { // the header, for ns, remains
		t.Fatalf("selected %v, rows %v", m.Selected(), visible(m))
	}
	m.Down() // must not panic
	m.SetFilter("f")
	m.Down() // the header stays selected, like the cluster's
	assertCursor(t, m, "f")
}

func TestClearingFilterKeepsSelectionVisible(t *testing.T) {
	m := newTree(t)
	m.SetFilter("d")
	m.Select("d")
	m.SetFilter("")
	assertCursor(t, m, "d")
	want := []string{"a", "src", "b", "modules", "c", "d", "e", "f"}
	if got := visible(m); !equal(got, want) {
		t.Fatalf("rows = %v, want %v", got, want)
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// newTreeWithRoot is newTree with a selectable cluster, and the header
// selected; inside ns, the header stands for ns.
func newTreeWithRoot(t *testing.T) *Model {
	t.Helper()
	m := New(theme.Default())
	m.SetRoot(&resource.Object{Name: "root"})
	m.SetRoots(inNamespace(fixture()))
	m.SetSize(80, 20)
	m.EnterPath("ns")
	m.Home() // the header: inside ns, it stands for ns
	return &m
}

func TestRootIsSelectedInitially(t *testing.T) {
	m := newTreeWithRoot(t)
	if !m.RootSelected() {
		t.Fatal("header not selected")
	}
	assertCursor(t, m, "ns")
}

func TestRootIsAboveFirstRow(t *testing.T) {
	m := newTreeWithRoot(t)
	m.Down()
	assertCursor(t, m, "a")
	if m.RootSelected() {
		t.Error("root still selected after Down")
	}
	m.Up()
	if !m.RootSelected() {
		t.Error("Up from the first object should select the root")
	}
	m.Up() // stays
	if !m.RootSelected() {
		t.Error("Up on the root should stay on the root")
	}

	m.End()
	assertCursor(t, m, "f")
	m.Home()
	if !m.RootSelected() {
		t.Error("Home should select the root")
	}
	m.PageDown()
	assertCursor(t, m, "f")
	m.PageUp()
	if !m.RootSelected() {
		t.Error("PageUp should reach the root")
	}
}

func TestRootNavigationKeys(t *testing.T) {
	m := newTreeWithRoot(t)
	m.Right() // like Down
	assertCursor(t, m, "a")
	m.Up()
	m.Toggle()
	m.ExpandBelow() // on the header: all of ns
	if !m.RootSelected() {
		t.Error("fold keys must not move the header selection")
	}
	assertRows(t, m, "a", "src", "b", "modules", "c", "d", "e", "f")
	m.CollapseBelow() // on the header: all of ns
	if !m.RootSelected() || m.Scope() == nil {
		t.Error("collapsing ns keeps the header selected, inside ns")
	}
	assertRows(t, m, "a", "src", "b", "f")

	m.Down()
	m.Down() // b
	m.Right()
	m.Right() // d
	m.CollapseBelow()
	m.CollapseBelow()
	assertCursor(t, m, "b")
	m.CollapseBelow() // a collapsed top-level object climbs to the header
	if !m.RootSelected() {
		t.Error("CollapseBelow on a collapsed top-level object should select the header")
	}
	m.CollapseBelow() // nothing expanded: back to the cluster level
	if m.Scope() != nil {
		t.Error("CollapseBelow on a collapsed namespace's header should leave it")
	}
	assertCursor(t, m, "ns")
}

func TestReplaceRootsKeepsRootSelected(t *testing.T) {
	m := newTreeWithRoot(t)
	m.SetRoot(&resource.Object{Name: "root"})
	m.ReplaceRoots(inNamespace(fixture()))
	if !m.RootSelected() || m.Selected() != m.Scope() || m.Scope() != m.roots[0] {
		t.Error("the header selection should move to the reloaded namespace")
	}

	m.Down()
	m.SetRoot(&resource.Object{Name: "root"})
	m.ReplaceRoots(inNamespace(fixture()))
	assertCursor(t, m, "a")
}

func TestFilterKeepsRoot(t *testing.T) {
	m := newTreeWithRoot(t)
	m.SetFilter("f")
	if !m.RootSelected() {
		t.Error("the root stays selected while filtering")
	}
	m.Down()
	assertCursor(t, m, "f")

	m.SetFilter("zzz") // nothing matches: the root remains, not nil
	if !m.RootSelected() {
		t.Error("no match should fall back to the root")
	}
	m.SetFilter("")
	if !m.RootSelected() {
		t.Error("closing the filter should keep the root")
	}
}

// healthy gives every object of the fixture an issue-free status, then
// marks the given paths as changed.
func healthy(t *testing.T, m *Model, changed ...string) {
	t.Helper()
	resource.Walk(m.roots, func(s *resource.Object) {
		s.Status = &resource.Status{}
		for _, p := range changed {
			if s.Path == p {
				s.Status.Phase = resource.PhaseFailed
			}
		}
	})
}

func TestProblemsOnlyShowsIssuesWithAncestors(t *testing.T) {
	m := newTree(t)
	healthy(t, m, "d")
	m.SetProblemsOnly(true)
	want := []string{"src", "b", "modules", "c", "d"}
	if got := visible(m); !equal(got, want) {
		t.Fatalf("rows = %v, want %v", got, want)
	}
	assertCursor(t, m, "b") // a was selected and is healthy: next available
	m.Select("d")
	m.SetProblemsOnly(false)
	assertCursor(t, m, "d")
	if got := visible(m); len(got) != 8 {
		t.Fatalf("rows = %v, want all expanded to d", got)
	}
}

func TestProblemsOnlyKeepsUnknownStates(t *testing.T) {
	m := newTree(t)
	healthy(t, m)
	f := m.scope.Children[2]
	f.Status, f.Err = nil, os.ErrNotExist // failed to load
	m.scope.Children[0].Status = nil      // still loading
	m.SetProblemsOnly(true)
	if got, want := visible(m), []string{"a", "src", "f"}; !equal(got, want) {
		t.Fatalf("rows = %v, want %v", got, want)
	}
}

func TestProblemsOnlyWithoutProblemsKeepsRoot(t *testing.T) {
	m := New(theme.Default())
	m.SetRoot(&resource.Object{Kind: resource.KindCluster, Status: &resource.Status{}})
	m.SetRoots(fixture())
	m.SetSize(80, 20)
	healthy(t, &m)
	m.SetProblemsOnly(true)
	if len(m.rows()) != 0 || !m.RootSelected() {
		t.Fatalf("rows %v, root selected %v", visible(&m), m.RootSelected())
	}
	m.Down() // must not panic
}

func TestProblemsOnlyCombinesWithFilter(t *testing.T) {
	m := newTree(t)
	healthy(t, m, "d", "f")
	m.SetProblemsOnly(true)
	m.SetFilter("f")
	if got, want := visible(m), []string{"src", "f"}; !equal(got, want) {
		t.Fatalf("rows = %v, want %v", got, want)
	}
	m.SetFilter("")
	if !m.ProblemsOnly() || len(visible(m)) != 6 {
		t.Fatalf("closing the filter keeps problems only: %v", visible(m))
	}
}

func TestRefitMovesSelectionWhenProblemDisappears(t *testing.T) {
	m := newTree(t)
	healthy(t, m, "a", "f")
	m.SetProblemsOnly(true)
	assertCursor(t, m, "a")
	m.scope.Children[0].Status.Phase = ""
	m.Refit()
	assertCursor(t, m, "f")
}

func TestStatusColumnFitsWidestStatus(t *testing.T) {
	m := newTree(t)
	b := m.scope.Children[1]
	// "rolling out +1 ·1 ·1 ·1" is wider than the minimum column
	b.Status = &resource.Status{Desired: 3, Ready: 3, Updated: 1, Suspended: true}
	b.Children[0].Err = errors.New("forbidden")
	b.Children[1].Status = &resource.Status{Desired: 2, Ready: 1, Updated: 2}
	b.Children[0].Children[0].Status = &resource.Status{Terminating: true}
	want := ansi.Strip(m.status(b, lipgloss.NewStyle()))
	if lipgloss.Width(want) <= statusWidth {
		t.Fatalf("fixture status %q fits the minimum width, test proves nothing", want)
	}
	if !strings.Contains(ansi.Strip(m.View()), want) {
		t.Fatalf("view cuts the status %q:\n%s", want, ansi.Strip(m.View()))
	}
}

// customTree is a namespace with a workload and custom resources of two
// kinds, which Arrange moves into a section.
func customTree(t *testing.T) *Model {
	t.Helper()
	cert := &resource.Type{Kind: "Certificate", Plural: "certificates", Group: "cert-manager.io"}
	route := &resource.Type{Kind: "Route", Plural: "routes", Group: "route.openshift.io"}
	ns := &resource.Object{Kind: resource.KindNamespace, Name: "shop", Path: "shop", Status: &resource.Status{}}
	for _, o := range []*resource.Object{
		{Kind: resource.KindDeployment, Name: "api", Group: "deployments"},
		{Kind: resource.KindCustom, Type: route, Name: "web"},
		{Kind: resource.KindCustom, Type: cert, Name: "api-tls"},
	} {
		o.Parent, o.Namespace, o.Path = ns, "shop", "shop/"+o.Ref()
		o.Status = &resource.Status{}
		ns.Children = append(ns.Children, o)
	}
	m := New(theme.Default())
	m.SetRoots(resource.Arrange([]*resource.Object{ns}))
	m.SetSize(80, 20)
	m.Right() // enter shop, select api
	return &m
}

const section = "shop/custom resources"

func rowsWithDepth(m *Model) string {
	var got []string
	for _, r := range m.rows() {
		label := r.label
		if r.kind == rowNode {
			label = r.node.Name
		}
		got = append(got, fmt.Sprintf("%d:%s", r.depth, label))
	}
	return strings.Join(got, " ")
}

func TestCustomResourcesSectionStartsCollapsed(t *testing.T) {
	m := customTree(t)
	if got, want := rowsWithDepth(m), "0:deployments 1:api 0:custom resources"; got != want {
		t.Fatalf("rows = %v, want %v", got, want)
	}
}

func TestCustomResourcesSectionExpands(t *testing.T) {
	m := customTree(t)
	m.Down()
	assertCursor(t, m, section)
	m.Right()
	assertCursor(t, m, "shop/certificate.cert-manager.io/api-tls")
	want := "0:deployments 1:api 0:custom resources 1:certificates 2:api-tls 1:routes 2:web"
	if got := rowsWithDepth(m); got != want {
		t.Fatalf("rows = %v, want %v", got, want)
	}
	m.Down()
	assertCursor(t, m, "shop/route.route.openshift.io/web") // skips the routes header
	m.Left()                                                // a leaf: collapses the section
	assertCursor(t, m, section)
	m.Left() // a collapsed top-level object: back to the cluster level
	assertCursor(t, m, "shop")
	if m.Scope() != nil {
		t.Fatal("Left on a collapsed top-level object should leave the namespace")
	}
}

func TestFilterReachesCustomResources(t *testing.T) {
	m := customTree(t)
	m.SetFilter("cert-manager")
	assertRows(t, m, section, "certificates", "shop/certificate.cert-manager.io/api-tls")
	assertCursor(t, m, section) // api is filtered out: the next visible row
}

func TestProblemsOnlyShowsNotReadyCustomResource(t *testing.T) {
	m := customTree(t)
	web := m.roots[0].Children[1].Children[1]
	web.Status = &resource.Status{Conditions: []resource.Condition{{Type: "Ready", Status: "False"}}}
	m.SetProblemsOnly(true)
	assertRows(t, m, section, "routes", "shop/route.route.openshift.io/web")
}

// clusterTree is the cluster level: three namespaces (one empty) and a
// top-level section, with a selectable cluster.
func clusterTree(t *testing.T) *Model {
	t.Helper()
	issuer := &resource.Type{Kind: "ClusterIssuer", Plural: "clusterissuers", Group: "cert-manager.io"}
	mk := func(name string, children ...*resource.Object) *resource.Object {
		ns := &resource.Object{Kind: resource.KindNamespace, Name: name, Path: name, Status: &resource.Status{}, Children: children}
		for _, c := range children {
			c.Parent, c.Namespace, c.Path = ns, name, name+"/"+c.Ref()
			c.Status = &resource.Status{}
		}
		return ns
	}
	top := resource.Arrange([]*resource.Object{
		mk("payments", &resource.Object{Kind: resource.KindDeployment, Name: "checkout", Group: "deployments"}),
		mk("shop",
			&resource.Object{Kind: resource.KindDeployment, Name: "api", Group: "deployments"},
			&resource.Object{Kind: resource.KindDeployment, Name: "web", Group: "deployments"}),
		mk("empty"),
		{Kind: resource.KindCustom, Type: issuer, Name: "letsencrypt", Path: "clusterissuer.cert-manager.io/letsencrypt", Status: &resource.Status{}},
	})
	m := New(theme.Default())
	m.SetRoot(&resource.Object{Kind: resource.KindCluster, Status: &resource.Status{}})
	m.SetRoots(top)
	m.SetSize(80, 20)
	return &m
}

func TestClusterLevelListsTopLevelOnly(t *testing.T) {
	m := clusterTree(t)
	assertRows(t, m, "payments", "shop", "empty", "cluster", "custom resources")
	if !m.RootSelected() || m.Scope() != nil {
		t.Fatal("start: the cluster selected, at the cluster level")
	}
	m.Down()
	m.ExpandBelow() // enters payments with everything expanded
	if m.Scope() == nil || m.Scope().Path != "payments" {
		t.Fatalf("scope = %v, want payments", m.Scope())
	}
}

func TestEnterAndBack(t *testing.T) {
	m := clusterTree(t)
	m.Down()
	m.Down() // shop
	m.Right()
	assertRows(t, m, "deployments", "shop/deployment/api", "shop/deployment/web")
	assertCursor(t, m, "shop/deployment/api")
	m.Down() // web
	m.Back()
	assertCursor(t, m, "shop")
	assertRows(t, m, "payments", "shop", "empty", "cluster", "custom resources")

	m.Toggle() // space enters too, and restores the selection
	assertCursor(t, m, "shop/deployment/web")
	m.Up()
	m.Up() // the header, for shop
	assertCursor(t, m, "shop")
	m.Left() // on the header: back
	if m.Scope() != nil {
		t.Fatal("Left on the header should leave the namespace")
	}
}

func TestEnterEmptyNamespaceIsNoop(t *testing.T) {
	m := clusterTree(t)
	m.Select("empty")
	m.Right()
	if m.Scope() != nil {
		t.Fatal("a namespace without objects has nothing to enter")
	}
}

func TestEnterTopLevelSection(t *testing.T) {
	m := clusterTree(t)
	if !m.EnterPath("custom resources") {
		t.Fatal("the top-level section can be entered")
	}
	assertRows(t, m, "clusterissuers", "clusterissuer.cert-manager.io/letsencrypt")
	if m.EnterPath("nope") {
		t.Fatal("EnterPath of an unknown path reports false")
	}
}

func TestFilterAppliesToCurrentLevel(t *testing.T) {
	m := clusterTree(t)
	m.SetFilter("api") // a workload in shop, not a namespace name
	assertRows(t, m)
	m.SetFilter("sho")
	assertRows(t, m, "shop")

	m.SetFilter("")
	m.EnterPath("shop")
	m.SetFilter("shop") // inside shop, its own name matches nothing
	assertRows(t, m)
	m.SetFilter("web")
	assertRows(t, m, "deployments", "shop/deployment/web")
	m.Back()
	if m.Filter() != "" {
		t.Fatal("changing the level clears the filter")
	}
}

func TestProblemsOnlyAtClusterLevelKeepsProblemsBelow(t *testing.T) {
	m := clusterTree(t)
	m.topLevel("shop").Children[1].Status.Phase = resource.PhaseFailed
	m.SetProblemsOnly(true)
	assertRows(t, m, "shop")
	m.EnterPath("shop")
	assertRows(t, m, "deployments", "shop/deployment/web") // problems only stays on
}

func TestReloadInsideNamespace(t *testing.T) {
	m := clusterTree(t)
	m.EnterPath("shop")
	m.Down() // web
	fresh := clusterTree(t)
	m.ReplaceRoots(fresh.roots)
	if m.Scope() != fresh.topLevel("shop") {
		t.Fatal("the scope should move to the reloaded namespace")
	}
	assertCursor(t, m, "shop/deployment/web")
	if m.Selected() != fresh.topLevel("shop").Children[1] {
		t.Fatal("the selection should be the reloaded object")
	}

	m.ReplaceRoots(fresh.roots[:1]) // shop is gone
	if m.Scope() != nil {
		t.Fatal("a vanished namespace leads back to the cluster level")
	}
}

func TestBackWithProblemsOnlyKeepsSelectionVisible(t *testing.T) {
	m := clusterTree(t)
	m.topLevel("shop").Children[1].Status.Phase = resource.PhaseFailed
	m.EnterPath("payments") // no problems
	m.SetProblemsOnly(true)
	m.Back() // payments is not listed at the cluster level now
	if m.cursorRow(m.rows()) < 0 && !m.RootSelected() {
		t.Fatalf("selection %q is not visible", m.Selected().Path)
	}
	m.Down()
	assertCursor(t, m, "shop")
}
