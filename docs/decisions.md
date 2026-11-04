# Decision log

Each entry: context, decision, consequences. Newest last. Entries that take
over a decision of [submarine](https://github.com/simonfranken/submarine/blob/main/docs/decisions.md)
name it (e.g. "submarine D3"); its rationale applies here too.

## P1: Raw dimensions in the domain, one word in the UI

**Context.** A pod can be pending, have a container that can't pull its
image, and have restarted, all at once. Showing every dimension makes rows
noisy; kubectl's `STATUS` column shows one reason with no ranking.
**Decision.** As submarine D3 and D5. `Status` stores independent facts
(phase, container states, replicas, events). `Status.Issues()` summarizes
them into issue kinds with levels. The tree shows the most severe issue as a
short word (+n); the detail pane shows all, with an explanation. Symbols only
where they are conventional (`▸▾`, `n/m`).
**Consequences.** One place defines what a problem is; the mock and the
Kubernetes provider get the same behavior.

## P2: Attention follows severity, four levels

**Context.** In a cluster almost everything is fine almost always; the
broken pod has to stand out among hundreds.
**Decision.** As submarine D4 and D26: quiet (muted, `healthy`), info
(blue: `rolling out`, `suspended`, `terminating`), warn (yellow: `pending`,
`not ready`, `degraded`, `restarting`), danger (red bold: `down`,
`crashloop`, `oom killed`, `pull error`, `failed`). Restarts count only
within the last hour; a workload scaled to zero is healthy.
**Consequences.** A long list reads as grey. A pod that restarted once,
weeks ago, stays quiet, which `kubectl get pods` (`RESTARTS 1 (21d ago)`)
does not do.

## P3: ANSI 16 colors

**Decision.** As submarine D10: ANSI 0–15 for all meaning, `AdaptiveColor`
only for backgrounds (bar, selection, backdrop), background detected before
Bubble Tea starts. The one difference is the accent (`Primary`): magenta
(ANSI 5), where submarine uses cyan, so the two tools are told apart at a
glance. Red, yellow and blue carry levels, green would read as healthy, so
magenta is the free hue; the grey backgrounds stay, since a tint would cost
contrast for the state words on the selected row.

## P4: Provider boundary and async loading

**Decision.** As submarine D11 and D16. The UI reads data only through
`resource.Provider`: `Tree()` for the fast structure, `Status(path)` per
object, loaded concurrently with a spinner per row; the provider bounds
concurrency. The skeleton ships a mock provider with artificial latency.
**Consequences.** The Kubernetes provider can be added without UI changes,
and the UI stays responsive on large clusters.

## P5: client-go, not the kubectl CLI

**Context.** submarine shells out to git (submarine D14), so the user's git
configuration applies. The same is possible with `kubectl get -o json`.
**Decision.** Use client-go. `clientcmd` loads the kubeconfig with kubectl's
own rules, so contexts, exec credential plugins (aws, gcloud, oidc) and
proxies behave as in kubectl, without requiring kubectl on `PATH`.
Informers give watches instead of polling or long-running `kubectl get -w`
processes, and typed objects need no JSON parsing.
**Alternatives.** The kubectl CLI: one process per query, `-w` streams to
parse, and behavior that depends on the installed kubectl version.
**Consequences.** A larger binary and the client-go dependency tree. kubectl
plugins (`kubectl foo`) are not available; actions are implemented with the
API. The network is the data source here, so submarine's "network only on
request" (N8) becomes: startup never waits for the API server, reads are
free, writes happen only on a user's action, and nothing prompts.

## P6: The cluster as a collapsed tree

*Partly replaced by [P15](#p15-a-cluster-level-and-namespaces-you-open).*

**Decision.** As submarine D7, D8 and D9. Namespaces are the top level,
collapsed on start; workloads below them, grouped by kind under muted,
non-selectable headers (`deployments`, …); pods below their workload.
ReplicaSets are not shown. Navigation is branch-first (`→` enters, `←`
leaves, `H`/`L` all below, see P13). Collapsed parents count the issues below them, one
count per level. The cluster itself is the header, selectable like
submarine's superproject.
**Amendment to submarine D7.** A collapsed parent without issues of its own
but with issues below shows only the counts (`·3 ·4`), not `healthy ·3 ·4`:
in Kubernetes, "healthy" on a namespace reads as "everything in it is
fine". The cluster's header and detail pane drop the word for the same
reason.

## P7: Distribution: local build, GitHub releases, curl | sh

**Decision.** As submarine D21 and D33 (without the Gitea history).
Releases are built locally with `make release` and published as GitHub
release assets (binaries, `checksums.txt`, `VERSION`), as a draft that is
published last. `install.sh` installs `periscope` and the alias `peri`;
`periscope update` replaces the binary atomically. Clients read
`releases/latest/download/VERSION` and `releases/download/<v>/<asset>`,
plain redirects with no rate limit. The `release` skill drives a release.
**Consequences.** Integrity is a sha256 from the same host (no signatures).
The release skill, Makefile, `install.sh` and `internal/update` are copies
of submarine's; a fix in one should be carried over.

## P8: Update check on every start, offered as "quit and update"

**Decision.** As submarine D27: a background check on every start (3 s
timeout, silent on failure); a newer release shows `q quit and update to
vX`, highlighted, right of help and quit; plain quit becomes `Q`. Off for
dev builds, `--mock` and `PERISCOPE_NO_UPDATE_CHECK=1`.

## P9: The screen explains itself

**Decision.** As submarine D32: labels say what a key does; the tools
(`!`, `/`, `r`) are in the footer; `?` has a keys page and a legend of every
word in its color, generated from the same functions as the tree; a start
hint until the first key; resource keys in a palette at the bottom of the
detail pane, or in the footer where the pane can't show them.

## P10: Name and keys

**Decision.** The tool is `periscope`, alias `peri`: the instrument a
submarine uses to look up and around, a sibling of `submarine` / `sub`.
Planned keys avoid two conflicts: logs are `v`, not `l` as in k9s (`l`
moves right, as in submarine); restarting a rollout is `t`, not `R`, which
sits next to `r` (reload; submarine D23 chose `x` over `R` for the same
reason).

## P11: Inspecting the cluster is as important as finding problems

**Context.** submarine's first priority is what needs attention; a clean
repository has little else to show. A cluster does: users open it to see
what is deployed, in which version, with which spec, and what happened to it
lately, without anything being broken.
**Decision.** periscope has two equal use cases: finding problems
(severity, one word, problems only) and inspecting the current state of the
cluster (objects, their fields, YAML and events). Attention still follows
severity (P2), but the inspecting side is not secondary: viewing an
object's YAML and all its events gets first-class keys and views.
**Consequences.** The detail pane and the planned pagers show the full
state of healthy objects too, not only the reasons for an issue. Features
are not judged only by whether they help find a problem.

## P12: The header counts issues per level, not per kind

**Context.** The header showed one count per issue kind, as submarine does
for its few states. A cluster has many kinds of issues at once (`down`,
`crashloop`, `pull error`, `pending`, `degraded`, …), and the line became a
wall of colored words that was cut on most terminals.
**Decision.** The header shows one count per level, `n errors · n warnings
· n notes`, the same summary as the tree's hidden counts and the detail
pane. Objects that failed to load count as errors. The counts per kind move
to the cluster's detail pane, as `Issues below`, one line per kind. A
namespace's detail pane has the same section: a namespace is where a
problem is narrowed down, and it can hold hundreds of pods. Workloads and
pods don't: a workload's own word sums up its pods, which the tree lists
right below it, and the pane needs the space for the workload's state.
**Consequences.** The header has a fixed, short shape however broken the
cluster is; the breakdown is one key (`g`, home) away.

## P13: Expand and collapse all below the selection

*Partly replaced by [P15](#p15-a-cluster-level-and-namespaces-you-open).*

**Context.** submarine's `L` / `H` expand and collapse the whole tree
(D8), which suits a repository with a handful of submodules. In a cluster
with hundreds of namespaces and thousands of pods, expanding everything is
a wall of rows; users are usually interested in one namespace.
**Decision.** `L` expands the selection and everything below it, `H`
collapses it and forgets what was expanded below; siblings stay as they
are, and `L` keeps the selection (submarine followed the first child down
to a leaf). With nothing to collapse (a leaf, a collapsed object) `H` acts
on the parent and selects it, like `h`, so repeating it climbs up. On the
header "everything below" is the cluster, so there both keys keep the old
global meaning, two keys away (`g`, then `H` or `L`).
**Consequences.** No separate global keys. The footer says `all below`.
`L` on a large namespace still opens a long list; `H` closes it again.

## P14: Resources beyond workloads

**Context.** The tree showed only namespaces, workloads and pods. Inspecting
the cluster is as important as finding problems (P11), and many real
problems are outside workloads: a Service without endpoints, an Ingress
whose backend is gone, a PVC that never binds, a node that is not ready.
**Decision.** periscope shows more kinds, as further groups of a namespace
after the workloads: Services, Ingresses, PVCs, then ConfigMaps and
Secrets; nodes as a group at the top level; custom resources generically
through their `Ready` condition, in a collapsible `custom resources` section with one
header per kind, alphabetically, only for kinds that have objects in the
namespace. periscope has no configuration file: what is shown follows from
the cluster, so it works the same on every machine and nothing needs
setting up. The tree stays
an ownership tree; relations (Service → pods, Ingress → Service, PVC →
pods) are shown in the detail pane. Kinds with a notion of broken get
issues; kinds without one show a muted fact (`3 keys`), never `healthy`
(an amendment to F10 for those kinds). Secret values are hidden unless
revealed on purpose. A forbidden list marks only its group as `error`.
**Consequences.** Namespaces get longer; configuration goes last and is
collapsed with the namespace, and empty groups are hidden. The provider
needs a list per kind and bounded concurrency across many more requests
(N2). Custom resources need discovery of the installed CRDs and one list
per kind across all namespaces (not one per namespace), filtered into the
namespaces; kinds with no objects add no rows.
**Custom resources, as implemented.** `custom resources` is a section, a
tree node of its own (`KindSection`) that `resource.Arrange` creates in each
namespace and at the top level, not a header: headers can't be selected or
collapsed, and a cluster with hundreds of cluster-scoped custom resources
(Crossplane, Kyverno, Karpenter) would otherwise push the namespaces off the
first screen. It starts collapsed like any object; it has no state word of
its own, since it isn't a resource. Whether two kinds share a plural is
decided over the whole cluster, so a kind's header doesn't change between
namespaces. Without a Ready condition the muted word is `unchecked`: we
can't tell, so we don't say `healthy`. Conditions are read only for custom
resources, so a pod's Ready condition never adds a second `not ready`.
`Cluster.Namespaces` became `Cluster.Objects`, since the top level now
holds cluster-scoped objects too.

## P15: A cluster level, and namespaces you open

**Context.** The tree had the namespaces as its top layer, expanded in
place (P6). In a large cluster one namespace is what users work in, but its
siblings stay on screen and everything is indented one level deeper. A
namespace scope like `kubectl -n` would focus, but loses the overview
("is anything broken anywhere?"), needs a picker and a mode, and users who
may not list namespaces need a way in either way.
**Decision.** Two levels, navigated with the keys the tree already has.
The **cluster level** is a list: the namespaces (with `[n]` and `·k`),
and under a `cluster` header the cluster-scoped sections. `→` / `enter` /
space opens one: the tree shows its contents without the namespace row,
the header becomes a breadcrumb (`kind-dev › shop`) with the namespace's
counts, and the key bar shows `⌫ back`. `←` at the top of a namespace (or on
its header), `⌫` anywhere, `H` climbing past the header, or a click on the
context in the breadcrumb goes back, with the namespace selected. `/` and
`!` apply to the current level only; changing the level closes the filter,
problems only stays. Start: `--namespace`, else the kubeconfig context's
namespace, else the cluster level. The flow is the same for everyone: a
user who may list only their own namespace sees a cluster level with one
entry. Replaces P6's "namespaces are the top level, expanded in place" and
P13's "on the header, everything".
**Consequences.** No namespace picker and no `n` key; the context picker
(`C`) is the only one. The overview is the cluster level, so "where are the
problems" is answered by `!` there and then by opening the namespace. A
search across namespaces at once is gone; it can come back as its own
feature if it is missed. Selection and expansion are kept per namespace for
the session, nothing is saved between runs.

## P16: Switching the context in periscope only

**Context.** Users work with several clusters. kubectx switches by writing
`current-context` into the kubeconfig, which changes every shell, and a
context that needs a fresh login or points at a cluster that is down
leaves the user with nothing on screen.
**Decision.** `C` opens a picker like submarine's branch picker (a popup
with a search line over the dimmed screen) with the kubeconfig contexts.
Enter switches periscope only; the kubeconfig is never written (N8). The
current cluster stays on screen until the new context's tree has loaded;
if it fails, the cluster stays and the footer says why, longer than other
messages. The new cluster starts like at startup: in the context's
namespace if it sets one, else at the cluster level; `--namespace` applies
to the first context only. Through the optional `resource.Switcher`, so a
provider without contexts simply doesn't offer it.
**Consequences.** The only picker; namespaces are opened from the cluster
level (P15). Answers from the old provider are dropped by identity. Nothing
about the switch is remembered between runs.

## P17: The Kubernetes provider reads a snapshot, never prompts, keeps stderr

**Context.** P5 chose client-go. A real cluster brings what the mock
didn't: thousands of objects, RBAC that forbids some lists, exec plugins
that write to the terminal or wait for a browser login, and client-go
logging to stderr, all under a full-screen UI.
**Decision.** `Tree` makes one paged List per kind, cluster-wide, and maps
every status into a snapshot; `Status` reads it, and the warning events
are listed once per `Tree` by the first `Status`. Pods are linked to
workloads by owner UID; pods without one go under `pods` (F3), Jobs of a
CronJob below it. Forbidden narrows what is shown (namespaces: the
context's only; a kind: per namespace, else left out); every other error
fails `Tree`. Exec plugins get no stdin, `Tree` gives up after 45 s, and
stderr is captured while the UI runs: klog and API warnings are dropped,
a plugin's own words lead the error. Contexts are sorted by name, as
`kubectl config get-contexts` does, since merged kubeconfigs have no order.
**Alternatives.** One `Get` per object in `Status`: thousands of requests
for what a few Lists return. Informers right away: the snapshot is the
same mapping, and watches come next (README step 2). Leaving stderr alone:
plugin output and log lines would paint over the UI and be lost.
**Consequences.** `r` re-reads the whole cluster; the tree is only as
fresh as the last `Tree` until watches come. A kind the user may not list
is missing silently until F65's `error` group exists. A user who may list
namespaces but no kind cluster-wide costs one List per kind and namespace;
with hundreds of namespaces that can exceed `Tree`'s 45 s.
