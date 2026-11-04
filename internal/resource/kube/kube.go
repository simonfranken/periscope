// Package kube is the provider that reads a Kubernetes cluster with
// client-go. It loads the kubeconfig like kubectl, never prompts and never
// writes; see docs/architecture.md#kubernetes-provider.
package kube

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth" // legacy auth providers (oidc, azure, gcp), as kubectl
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"periscope/internal/resource"
)

// Timeouts: a request, the whole Tree (an exec plugin that waits for a
// browser login has no timeout of its own) and loading the events.
const (
	requestTimeout = 30 * time.Second
	treeTimeout    = 45 * time.Second
	eventsTimeout  = 30 * time.Second
)

// parallel bounds the lists that run at once when they have to be made per
// namespace.
const parallel = 8

// Provider reads the cluster of one kubeconfig context.
type Provider struct {
	rules   clientcmd.ClientConfigLoadingRules
	raw     clientcmdapi.Config
	context string // the context it reads
	ns      string // the context's namespace, "" if it sets none
	start   string // --namespace, the one to list if namespaces can't be
	server  string
	client  kubernetes.Interface

	mu   sync.Mutex
	snap *snapshot // of the last Tree
}

// New returns the provider for the kubeconfig context name, the current
// context if name is "". namespace is --namespace: if namespaces can't be
// listed, it is the one listed instead of the context's. New reads the
// kubeconfig (KUBECONFIG or ~/.kube/config) but doesn't talk to the
// cluster.
func New(name, namespace string) (*Provider, error) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	raw, err := rules.Load()
	if err != nil {
		return nil, err
	}
	p, err := newFor(*rules, *raw, name)
	if err != nil {
		return nil, err
	}
	p.start = namespace
	return p, nil
}

func newFor(rules clientcmd.ClientConfigLoadingRules, raw clientcmdapi.Config, name string) (*Provider, error) {
	if name == "" {
		name = raw.CurrentContext
	}
	if name == "" {
		return nil, fmt.Errorf("the kubeconfig sets no current context; choose one with --context")
	}
	kctx, ok := raw.Contexts[name]
	if !ok {
		return nil, fmt.Errorf("no context %q in the kubeconfig", name)
	}
	cc := clientcmd.NewNonInteractiveClientConfig(raw, name, &clientcmd.ConfigOverrides{}, &rules)
	cfg, err := cc.ClientConfig()
	if err != nil {
		return nil, err
	}
	cfg.QPS, cfg.Burst = 50, 100
	cfg.Timeout = requestTimeout
	cfg.WarningHandler = rest.NoWarnings{}
	if cfg.ExecProvider != nil {
		// The terminal belongs to the UI: a plugin that needs it fails with
		// its message instead of prompting (N8).
		cfg.ExecProvider.StdinUnavailable = true
		cfg.ExecProvider.StdinUnavailableMessage = "periscope can't prompt; log in in a terminal first, e.g. with kubectl"
	}
	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	return &Provider{rules: rules, raw: raw, context: name, ns: kctx.Namespace, server: cfg.Host, client: client}, nil
}

// Contexts lists the kubeconfig's contexts sorted by name, as kubectl
// config get-contexts does: the kubeconfig keeps no order of its own once
// several files are merged.
func (p *Provider) Contexts(ctx context.Context) ([]resource.Context, error) {
	var out []resource.Context
	for name, c := range p.raw.Contexts {
		out = append(out, resource.Context{Name: name, Cluster: c.Cluster, Namespace: c.Namespace, Current: name == p.context})
	}
	slices.SortFunc(out, func(a, b resource.Context) int { return cmp.Compare(a.Name, b.Name) })
	return out, nil
}

// Switch returns the provider for another context of the same kubeconfig.
// --namespace is for the first context only (P16).
func (p *Provider) Switch(name string) (resource.Provider, error) {
	return newFor(p.rules, p.raw, name)
}
