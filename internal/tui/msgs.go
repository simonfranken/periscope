package tui

import "periscope/internal/resource"

type treeLoadedMsg struct {
	cluster *resource.Cluster
	err     error
}

type statusLoadedMsg struct {
	path   string
	status *resource.Status
	err    error
}

// updateMsg carries the result of the startup update check; version is ""
// if there is no newer release.
type updateMsg struct{ version string }
