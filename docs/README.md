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

**Skeleton.** The UI runs on built-in mock data (`periscope --mock`):
the tree, the detail pane, the header, problems only (`!`), the filter (`/`),
copy name (`c`), the full help and legend (`?`), and the update check.
Without `--mock` periscope exits with a hint, because there is no cluster
provider yet.

Next steps, roughly in order:

1. **Kubernetes provider** (`internal/resource/kube`, client-go, P5): read
   the kubeconfig like kubectl (`--context`, `--namespace`, `KUBECONFIG`),
   list namespaces, workloads and pods, map them to `resource.Status`, and
   load warning events. Bounded concurrency, never block startup.
2. **Watch** (`resource.Watcher`): informers keep the tree live, so a pod
   that starts crashing shows up without `r`.
3. **Context and namespace pickers** (`C`, `n`): popups like submarine's
   branch picker, with a dimmed backdrop.
4. **Read actions**: logs (`v`, the previous container's logs for a
   crashloop), describe (`d`), the YAML of an object and all its events
   (not only warnings), in a pager.
5. **Write actions**: restart a rollout (`t`), scale (`s`), delete a pod
   (`x`). Each asks first if something would be lost (submarine D23).
6. A fixture for integration tests: a `kind` cluster script, like
   submarine's `scripts/make-fixture.sh`.
