package resource

import (
	"cmp"
	"slices"
	"strings"
)

// CustomGroup is the header above the custom resources of a namespace (or
// of the cluster), which has one header per kind below it.
const CustomGroup = "custom resources"

// Arrange puts the objects of a tree into the order the tree shows them, and
// is what every provider calls on the result of Tree. Within top and within
// each object's children: by kind (namespaces, the workload kinds in the
// order of F2, pods, then custom resources), keeping the provider's order
// within a built-in kind; custom resources alphabetically by their kind's
// header, then by name.
//
// It sets the Group of each custom resource to "custom resources/<plural>".
// If two types that have objects in the cluster share a plural, their API
// group is added ("applications argoproj.io"), the same in every namespace.
func Arrange(top []*Object) {
	groups := map[string]map[string]bool{} // plural -> API groups
	Walk(top, func(o *Object) {
		if o.Kind == KindCustom && o.Type != nil {
			if groups[o.Type.Plural] == nil {
				groups[o.Type.Plural] = map[string]bool{}
			}
			groups[o.Type.Plural][o.Type.Group] = true
		}
	})
	Walk(top, func(o *Object) {
		if o.Kind == KindCustom && o.Type != nil {
			header := o.Type.Plural
			if len(groups[o.Type.Plural]) > 1 {
				header += " " + o.Type.Group
			}
			o.Group = CustomGroup + "/" + header
		}
	})
	sortObjects(top)
	Walk(top, func(o *Object) { sortObjects(o.Children) })
}

func sortObjects(objs []*Object) {
	slices.SortStableFunc(objs, func(a, b *Object) int {
		if c := cmp.Compare(a.Kind, b.Kind); c != 0 || a.Kind != KindCustom {
			return c
		}
		return cmp.Or(strings.Compare(a.Group, b.Group), strings.Compare(a.Name, b.Name))
	})
}
