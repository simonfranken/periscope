package tui

import "periscope/internal/resource"

// treeLoadedMsg and statusLoadedMsg name the provider they come from, so
// answers for a context that was switched away from are dropped.
type treeLoadedMsg struct {
	from    resource.Provider
	cluster *resource.Cluster
	err     error
}

type statusLoadedMsg struct {
	from   resource.Provider
	path   string
	status *resource.Status
	err    error
}

// updateMsg carries the result of the startup update check; version is ""
// if there is no newer release.
type updateMsg struct{ version string }
