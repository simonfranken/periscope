package tree

import "periscope/internal/resource"

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
// gets a header row; its objects are indented one level deeper.
func (m *Model) appendRows(out *[]row, mods []*resource.Object, depth int) {
	for _, g := range groupByDir(m.filtered(mods)) {
		d := depth
		if g.dir != "" {
			*out = append(*out, row{kind: rowDir, depth: depth, label: g.dir})
			d++
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
	dir  string
	mods []*resource.Object
}

// groupByDir keeps the order in which groups first appear.
func groupByDir(mods []*resource.Object) []dirGroup {
	var groups []dirGroup
	idx := map[string]int{}
	for _, s := range mods {
		dir := s.Group
		i, ok := idx[dir]
		if !ok {
			i = len(groups)
			idx[dir] = i
			groups = append(groups, dirGroup{dir: dir})
		}
		groups[i].mods = append(groups[i].mods, s)
	}
	return groups
}
