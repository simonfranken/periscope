package tree

import (
	"strings"

	"periscope/internal/resource"
)

type rowKind int

const (
	rowDir  rowKind = iota // non-selectable group header, e.g. "deployments"
	rowNode                // an object
)

type row struct {
	kind  rowKind
	depth int
	label string // group label for rowDir
	node  *resource.Object
}

// rows flattens the visible part of the tree.
func (m *Model) rows() []row {
	var out []row
	m.appendRows(&out, m.roots, 0)
	return out
}

// appendRows groups mods by their Group (e.g. "deployments"). Each group
// gets a header row; its objects are indented one level deeper. A Group with
// "/" nests headers ("custom resources/certificates").
func (m *Model) appendRows(out *[]row, mods []*resource.Object, depth int) {
	m.appendGroups(out, m.filtered(mods), depth, "")
}

// appendGroups lists mods below the headers in prefix.
func (m *Model) appendGroups(out *[]row, mods []*resource.Object, depth int, prefix string) {
	for _, g := range groupByDir(mods, prefix) {
		d := depth
		if g.dir != "" {
			*out = append(*out, row{kind: rowDir, depth: depth, label: g.dir})
			d++
		}
		if g.nested {
			m.appendGroups(out, g.mods, d, prefix+g.dir+"/")
			continue
		}
		for _, s := range g.mods {
			*out = append(*out, row{kind: rowNode, depth: d, node: s})
			if m.isExpanded(s) && len(s.Children) > 0 {
				m.appendRows(out, s.Children, d+1)
			}
		}
	}
}

type dirGroup struct {
	dir    string
	nested bool // the objects have further headers below dir
	mods   []*resource.Object
}

// groupByDir groups mods by the first header of their Group below prefix,
// in the order in which groups first appear.
func groupByDir(mods []*resource.Object, prefix string) []dirGroup {
	var groups []dirGroup
	idx := map[string]int{}
	for _, s := range mods {
		dir, rest, nested := strings.Cut(strings.TrimPrefix(s.Group, prefix), "/")
		i, ok := idx[dir]
		if !ok {
			i = len(groups)
			idx[dir] = i
			groups = append(groups, dirGroup{dir: dir})
		}
		groups[i].nested = groups[i].nested || (nested && rest != "")
		groups[i].mods = append(groups[i].mods, s)
	}
	return groups
}

// displayOrder is mods in the order the tree lists them below their
// headers.
func displayOrder(mods []*resource.Object, prefix string) []*resource.Object {
	var out []*resource.Object
	for _, g := range groupByDir(mods, prefix) {
		if g.nested {
			out = append(out, displayOrder(g.mods, prefix+g.dir+"/")...)
		} else {
			out = append(out, g.mods...)
		}
	}
	return out
}
