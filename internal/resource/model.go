// Package resource is the domain model of periscope: the objects of a
// cluster as a tree, their raw state, and what that state means. It has no
// UI imports; see docs/state-model.md.
package resource

import "time"

// Kind is the kind of an object in the tree.
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
	default:
		return "pod"
	}
}

// Plural is the name of a group of objects of this kind, e.g. "deployments".
func (k Kind) Plural() string { return k.Name() + "s" }

// Cluster is the result of Provider.Tree: the context and its objects.
type Cluster struct {
	Context    string // kubeconfig context
	Server     string // API server URL
	Version    string // server version, "" if unknown
	Root       *Object
	Namespaces []*Object
}

// Object is a node of the tree: a namespace, a workload or a pod.
type Object struct {
	Kind      Kind
	Namespace string // "" for the cluster and namespaces
	Name      string
	// Path is the unique ID: "<namespace>", "<namespace>/<kind>/<name>",
	// "<namespace>/<kind>/<name>/pod/<pod>"; "" for the cluster.
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

// Ref is how kubectl names the object, e.g. "deployment/api".
func (o *Object) Ref() string { return o.Kind.Name() + "/" + o.Name }

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

	// Warning events about the object, newest first.
	Events []Event
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
