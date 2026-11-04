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

// Context is a kubeconfig context a Switcher can switch to.
type Context struct {
	Name      string
	Cluster   string // the kubeconfig cluster it points at
	Namespace string // the context's namespace, "" if it sets none
	Current   bool   // the context the provider reads
}

// Switcher is an optional capability of a Provider: the kubeconfig contexts
// and a provider for another one. Switching affects periscope only; the
// kubeconfig (its current-context) is never written.
type Switcher interface {
	// Contexts lists the kubeconfig contexts, in kubeconfig order.
	Contexts(ctx context.Context) ([]Context, error)
	// Switch returns a provider for the context name. It must not talk to the
	// cluster: like at startup, the tree loads afterwards (N1).
	Switch(name string) (Provider, error)
}
