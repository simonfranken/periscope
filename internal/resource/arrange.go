package resource

import (
	"cmp"
	"slices"
	"strings"
)

// CustomSection is the name of the section that holds the custom resources
// of a namespace, or the cluster-scoped ones at the top level.
const CustomSection = "custom resources"

// Arrange puts the objects of a tree into the order the tree shows them, and
// is what every provider calls on the result of Tree. It returns the new top
// level.
//
// The custom resources of each namespace, and those at the top level, move
// into a section object (KindSection, CustomSection) that takes their place,
// collapsible like a namespace and with an empty Status, since it has no
// state of its own. Within the section they are listed under one header per
// kind, its plural; if two types that have objects in the cluster share a
// plural, their API group is added ("applications argoproj.io"), the same
// in every namespace.
//
// Order, within top and within each object's children: by kind (namespaces,
// the workload kinds in the order of F2, pods, then the section), keeping
// the provider's order within a built-in kind; custom resources
// alphabetically by their header, then by name.
func Arrange(top []*Object) []*Object {
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
			o.Group = o.Type.Plural
			if len(groups[o.Type.Plural]) > 1 {
				o.Group += " " + o.Type.Group
			}
		}
	})

	top = section(nil, top)
	Walk(top, func(o *Object) {
		if o.Kind != KindSection {
			o.Children = section(o, o.Children)
		}
	})
	sortObjects(top)
	Walk(top, func(o *Object) { sortObjects(o.Children) })
	return top
}

// section moves the custom resources among objs, the children of parent
// (nil for the top level), into a section that takes their place.
func section(parent *Object, objs []*Object) []*Object {
	var rest, custom []*Object
	for _, o := range objs {
		if o.Kind == KindCustom {
			custom = append(custom, o)
		} else {
			rest = append(rest, o)
		}
	}
	if len(custom) == 0 {
		return objs
	}
	s := &Object{Kind: KindSection, Name: CustomSection, Path: CustomSection, Parent: parent, Status: &Status{}}
	if parent != nil {
		s.Namespace, s.Path = parent.Namespace, parent.Path+"/"+CustomSection
		if parent.Kind == KindNamespace {
			s.Namespace = parent.Name
		}
	}
	for _, o := range custom {
		o.Parent = s
	}
	s.Children = custom
	return append(rest, s)
}

func sortObjects(objs []*Object) {
	slices.SortStableFunc(objs, func(a, b *Object) int {
		if c := cmp.Compare(a.Kind, b.Kind); c != 0 || a.Kind != KindCustom {
			return c
		}
		return cmp.Or(strings.Compare(a.Group, b.Group), strings.Compare(a.Name, b.Name))
	})
}
