package mock

import (
	"context"
	"testing"

	"periscope/internal/resource"
)

// The mock is the place to see every word: each issue kind must occur.
func TestMockShowsEveryIssue(t *testing.T) {
	p := New()
	c, err := p.Tree(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	seen := map[resource.IssueKind]bool{}
	paths := map[string]bool{}
	resource.Walk(c.Objects, func(o *resource.Object) {
		if paths[o.Path] {
			t.Errorf("duplicate path %q", o.Path)
		}
		paths[o.Path] = true
		if o.Kind == resource.KindSection {
			return // no state of its own, never loaded
		}
		st, ok := p.statuses[o.Path]
		if !ok {
			t.Errorf("no status for %q", o.Path)
			return
		}
		for _, is := range st.Issues() {
			seen[is.Kind] = true
		}
	})
	for _, k := range resource.Kinds {
		if !seen[k] {
			t.Errorf("no object with issue %d", k)
		}
	}
	if _, ok := p.statuses[""]; !ok {
		t.Error("no status for the cluster")
	}
}

func TestContextsAndSwitch(t *testing.T) {
	p := New()
	cs, err := p.Contexts(context.Background())
	if err != nil || len(cs) != 3 || !cs[0].Current || cs[1].Current {
		t.Fatalf("contexts = %+v, %v; want kind-dev current", cs, err)
	}
	s, err := p.Switch("staging")
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.Tree(context.Background())
	if err != nil || c.Context != "staging" || c.Namespace != "shop" {
		t.Fatalf("staging tree = %+v, %v", c, err)
	}
	cs, _ = s.(resource.Switcher).Contexts(context.Background())
	if !cs[1].Current || cs[0].Current {
		t.Errorf("after the switch staging should be current: %+v", cs)
	}
	if _, err := p.Switch("nope"); err == nil {
		t.Error("an unknown context should fail")
	}
}

func TestUnreachableContextFails(t *testing.T) {
	s, err := New().Switch("prod-eu")
	if err != nil {
		t.Fatal(err) // switching itself doesn't talk to the cluster
	}
	if _, err := s.Tree(context.Background()); err == nil {
		t.Error("prod-eu's tree should fail like a login that needs a browser")
	}
}
