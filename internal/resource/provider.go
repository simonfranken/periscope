package resource

import "context"

// Provider is the only way the UI reads cluster data. The mock provider
// (package mock) and the Kubernetes provider (planned, client-go) are
// interchangeable; see docs/architecture.md#provider-boundary.
type Provider interface {
	// Tree returns the cluster and its objects with Parent and Children
	// linked and Status nil. It should be fast.
	Tree(ctx context.Context) (*Cluster, error)
	// Status returns the state of the object at path ("" is the cluster). It
	// is called once per object, concurrently, and must respect ctx.
	Status(ctx context.Context, path string) (*Status, error)
}
