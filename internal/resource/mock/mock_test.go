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
