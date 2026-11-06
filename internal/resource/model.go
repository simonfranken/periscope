// Package resource is the domain model of periscope: the objects of a
// cluster as a tree, their raw state, and what that state means. It has no
// UI imports; see docs/state-model.md.
package resource

import (
	"strings"
	"time"
)

// Kind is the kind of an object in the tree. The constants are in display
// order (Arrange sorts by them), as F60 lists them: the network kinds before
// KindSection, ConfigMaps and Secrets after it.
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
	KindService
	KindIngress
	KindRoute   // an OpenShift Route (route.openshift.io), the Ingress of OpenShift
	KindSection // a collapsible section of its parent, e.g. "custom resources"; it has no state of its own
	KindConfigMap
	KindSecret
	KindCustom // a custom resource; Object.Type says which
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
	case KindPod:
		return "pod"
	case KindService:
		return "service"
	case KindIngress:
		return "ingress"
	case KindRoute:
		return "route"
	case KindConfigMap:
		return "configmap"
	case KindSecret:
		return "secret"
	case KindSection:
		return "section"
	default:
		return "custom resource"
	}
}

// Plural is the name of a group of objects of this kind, e.g. "deployments".
func (k Kind) Plural() string {
	if k == KindIngress {
		return "ingresses"
	}
	return k.Name() + "s"
}

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

// Object is a node of the tree: a namespace, a workload, a pod, a Service,
// Ingress, Route, ConfigMap or Secret, or a custom resource.
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
	// DefaultContainer is the container kubectl picks for logs and exec
	// (the kubectl.kubernetes.io/default-container annotation), "" if unset.
	DefaultContainer string

	// Services: the type (ClusterIP, NodePort, LoadBalancer, ExternalName)
	// and selector ("app=web", "" for none). Endpoints counts the ready
	// endpoints of its EndpointSlices; Idle is set if it has none because
	// every workload it selects is scaled to zero. Targets are the workloads
	// (and bare pods) it selects, as refs ("deployment/web").
	// Secrets: Type is the secret's type, e.g. "kubernetes.io/tls".
	Type      string
	Selector  string
	Endpoints int
	Idle      bool
	Targets   []string

	// Ingresses and Routes: the hosts they serve, the addresses they are
	// reachable at (a load balancer's, the routers' that admitted a Route),
	// the Services they route to. Rejected is why a router rejected a Route.
	Hosts     []string
	Addresses []string
	Backends  []Backend
	Rejected  string

	// ConfigMaps and Secrets: their keys and the size of each value; the
	// values themselves are never read into a Status (F64).
	Keys []Key

	// Custom resources only: status.conditions. Pods and workloads carry
	// their state in the fields above, so their conditions are not read.
	Conditions []Condition

	// Warning events about the object, newest first.
	Events []Event
}

// Backend is a Service an Ingress or Route routes to.
type Backend struct {
	Service string
	Missing bool // the Service doesn't exist in the namespace
}

// Key is a key of a ConfigMap or Secret and the size of its value in bytes.
type Key struct {
	Name string
	Size int
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
