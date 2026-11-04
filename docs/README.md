# periscope: documentation

periscope is a terminal UI for Kubernetes clusters. It shows the **objects
of a cluster** as a collapsible tree (namespaces, then workloads, then pods;
the cluster itself in the header) and summarizes each object's state as
**one word**, so problems stand out and healthy objects stay quiet.

Unlike submarine, finding problems is not the first priority but one of two
equal ones: periscope is just as much for **looking at the cluster as it
is**, its objects, their state, their YAML and their events, as kubectl
would show them, only faster to reach (see [P11](decisions.md#p11-inspecting-the-cluster-is-as-important-as-finding-problems)).

It is a sibling of [submarine](https://github.com/simonfranken/submarine)
(a TUI for git submodules) and shares its principles, stack, release,
install and update process. Decisions taken over from submarine name their
origin in [decisions.md](decisions.md).

## Documents

| Document | Contents |
|---|---|
| [requirements.md](requirements.md) | What the tool must do: functional and non-functional requirements |
| [state-model.md](state-model.md) | The objects, their raw state, and how it is summarized into issues and levels |
| [ux-guide.md](ux-guide.md) | Layout, attention hierarchy, colors, wording, navigation and key bindings |
| [architecture.md](architecture.md) | Package layout, the provider boundary, loading, distribution, testing |
| [decisions.md](decisions.md) | Decision log with rationale (ADR style) |

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/simonfranken/periscope/main/install.sh | sh
```

Installs `periscope` and the alias `peri` (linux/macOS, amd64/arm64) to
`~/.local/bin`. Update with `periscope update`. Maintainers publish with
`make release` (see [architecture](architecture.md#distribution)).

## Status

**Reads real clusters.** The Kubernetes provider
(`internal/resource/kube`, client-go, P5, P17) reads the kubeconfig like
kubectl (`--context`, `KUBECONFIG`, exec plugins without prompting), lists
namespaces, workloads and pods (pods without a workload under `pods`, F3;
Jobs of a CronJob below it), maps them to `resource.Status` and loads the
warning events. If listing namespaces is forbidden, the context's
namespace is the only one (F4). The context picker (`C`) switches between
the kubeconfig's contexts. The UI: the cluster level and opening
namespaces (`--namespace`), the tree (including custom resources, F67, so
far on mock data only), the detail pane, the header, problems only (`!`),
the filter (`/`), copy name (`c`), the full help and legend (`?`), and the
update check. `periscope --mock` runs on built-in mock data (three mock
contexts, one of which can't be reached).

Next steps, roughly in order:

1. **Crashloops on newer Kubernetes.** Between restarts kubelet now mostly
   reports a crashing container as terminated (`Error`) instead of waiting
   in `CrashLoopBackOff`, so a crashloop often shows as `restarting`.
   Decide whether a terminated, failed container that will restart counts
   as `crashloop` (state model).
2. **Watch** (`resource.Watcher`): informers keep the tree live, so a pod
   that starts crashing shows up without `r`.
3. **More resource kinds** (F60–F67, P14), in this order: Services,
   Ingresses and PVCs (they have real states); ConfigMaps and Secrets (for
   browsing, values hidden); nodes; discovering the custom resources (the
   UI for them is done). With them, a kind whose list is forbidden shows
   `error` (F65); for now it is left out.
4. **Read actions**: logs (`v`, the previous container's logs for a
   crashloop), describe (`d`), the YAML of an object and all its events
   (not only warnings), in a pager.
5. **Write actions**: restart a rollout (`t`), scale (`s`), delete a pod
   (`x`). Each asks first if something would be lost (submarine D23).
6. A fixture for integration tests: a `kind` cluster script, like
   submarine's `scripts/make-fixture.sh`.
7. Init containers: one that crashloops shows only as `pending` of its pod.
