// Package resource is the domain model of periscope: the objects of a
// cluster as a tree, their raw state, and what that state means. It has no
// UI imports; see docs/state-model.md.
package resource

import (
	"strings"
	"time"
)

// Kind is the kind of an object in the tree. The constants are in display
// order (Arrange sorts by them): new kinds go where F60 lists them, e.g.
// Services before KindSection, ConfigMaps and Secrets after it.
type Kind int

const (
	KindCluster Kind = iota // the root, shown by the header
	KindNamespace
	KindDeployment
	KindStatefulSet
	KindDaemonSet
	KindJob
	KindCronJob
	KindPod
	KindSection // a collapsible section of its parent, e.g. "custom resources"; it has no state of its own
	KindCustom  // a custom resource; Object.Type says which
)

// Name is the lower-case singular name kubectl uses, e.g. "deployment".
func (k Kind) Name() string {
	switch k {
	case KindCluster:
		return "cluster"
	case KindNamespace:
		return "namespace"
	case KindDeployment:
		return "deployment"
	case KindStatefulSet:
		return "statefulset"
	case KindDaemonSet:
		return "daemonset"
	case KindJob:
		return "job"
	case KindCronJob:
		return "cronjob"
	case KindSection:
		return "section"
	case KindCustom:
		return "custom resource"
	default:
		return "pod"
	}
}

// Plural is the name of a group of objects of this kind, e.g. "deployments".
func (k Kind) Plural() string { return k.Name() + "s" }

// Cluster is the result of Provider.Tree: the context and its objects.
type Cluster struct {
	Context string // kubeconfig context
	Server  string // API server URL
	Version string // server version, "" if unknown
	// Namespace is the kubeconfig context's namespace, where the tree
	// starts; "" if the context sets none, which starts at the cluster level.
	Namespace string
	Root      *Object
	// Objects is the top level: the namespaces, then cluster-scoped
	// objects (custom resources), in the order of Arrange.
	Objects []*Object
}

// Type describes the kind of a custom resource, as its CRD names it.
type Type struct {
	Kind   string // e.g. "Certificate"
	Plural string // e.g. "certificates"
	Group  string // API group, e.g. "cert-manager.io"
}

// Name is the kubectl name of the type, e.g. "certificate.cert-manager.io".
func (t Type) Name() string { return strings.ToLower(t.Kind) + "." + t.Group }

// Object is a node of the tree: a namespace, a workload, a pod or a
// custom resource.
type Object struct {
	Kind      Kind
	Type      *Type  // the custom resource's type, nil for other kinds
	Namespace string // "" for the cluster, namespaces and cluster-scoped objects
	Name      string
	// Path is the unique ID: "<namespace>", "<namespace>/<ref>",
	// "<namespace>/<ref>/pod/<pod>", "<ref>" for a cluster-scoped object
	// (see Ref); a section's is "<parent>/<name>" ("custom resources" at the
	// top level); "" for the cluster.
	Path string
	// Group is the header the object is listed under within its parent
	// (e.g. "deployments"); "" lists it without one.
	Group    string
	Parent   *Object
	Children []*Object

	Status *Status // nil while loading
	Err    error   // set if the status could not be loaded
}

// IsRoot reports whether o is the cluster.
func (o *Object) IsRoot() bool { return o != nil && o.Path == "" }

// TypeName is how kubectl names the object's kind, e.g. "deployment" or
// "certificate.cert-manager.io".
func (o *Object) TypeName() string {
	if o.Kind == KindCustom && o.Type != nil {
		return o.Type.Name()
	}
	return o.Kind.Name()
}

// Ref is how kubectl names the object, e.g. "deployment/api" or
// "certificate.cert-manager.io/api-tls".
func (o *Object) Ref() string { return o.TypeName() + "/" + o.Name }

// Status holds the raw dimensions of an object's state. Which of them mean a
// problem is decided by Issues.
type Status struct {
	Created time.Time

	// Namespaces
	Terminating bool

	// Workloads: replicas (DaemonSet: scheduled pods, Job: completions)
	Desired, Ready, Updated int
	Suspended               bool // CronJob or Job

	// Pods
	Phase      Phase
	Node       string
	Containers []Container

	// Custom resources only: status.conditions. Pods and workloads carry
	// their state in the fields above, so their conditions are not read.
	Conditions []Condition

	// Warning events about the object, newest first.
	Events []Event
}

// Condition is one entry of a custom resource's status.conditions.
type Condition struct {
	Type    string // e.g. "Ready"
	Status  string // "True", "False" or "Unknown"
	Reason  string
	Message string
}

// ReadyCondition returns the condition of type Ready, or nil.
func (s *Status) ReadyCondition() *Condition {
	if s == nil {
		return nil
	}
	for i := range s.Conditions {
		if s.Conditions[i].Type == "Ready" {
			return &s.Conditions[i]
		}
	}
	return nil
}

// Phase is a pod's phase.
type Phase string

const (
	PhasePending   Phase = "Pending"
	PhaseRunning   Phase = "Running"
	PhaseSucceeded Phase = "Succeeded"
	PhaseFailed    Phase = "Failed"
)

// Container is the state of one container of a pod.
type Container struct {
	Name     string
	Image    string
	Ready    bool
	State    ContainerState
	Reason   string // waiting or terminated reason, e.g. "CrashLoopBackOff"
	Restarts int
	// LastReason is why the container last terminated, e.g. "OOMKilled";
	// LastExit is when.
	LastReason string
	LastExit   time.Time
}

// ContainerState is a container's current state.
type ContainerState int

const (
	ContainerWaiting ContainerState = iota
	ContainerRunning
	ContainerTerminated
)

// Event is a warning event about an object.
type Event struct {
	Reason  string
	Message string
	Count   int
	Last    time.Time
}

// Walk calls fn for every object below roots, depth first, parents first.
func Walk(roots []*Object, fn func(*Object)) {
	for _, o := range roots {
		fn(o)
		Walk(o.Children, fn)
	}
}
