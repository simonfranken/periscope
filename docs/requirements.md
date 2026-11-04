# Requirements

## Problem

`kubectl` answers exactly what it is asked, one resource type at a time.
`kubectl get pods -A` is a wall of rows in which a crashing pod looks like
any other, and the reason is spread over `describe`, the container states
and the events. It doesn't answer the questions users actually have:

- Is anything broken in this cluster, and where?
- Why is it broken?
- Which parts are fine, so I can ignore them?
- What is running, in what state, with which spec, and what happened to it
  lately? (The questions of a user who is not hunting a problem.)

Both kinds of question are equally important (see
[P11](decisions.md#p11-inspecting-the-cluster-is-as-important-as-finding-problems)):
periscope is a tool for browsing the cluster as much as for finding what is
broken.

## Goals

- Show the cluster as a **tree**: namespaces, workloads, pods. The cluster
  itself is shown by the header, which is selectable.
- Summarize each object's state as **one word**; let **problems stand out**
  and keep healthy objects quiet.
- Explain the problem where it is shown: issue, container states, events.
- Make the **current state of every object** easy to inspect: its fields,
  its YAML and its events, not only when something is wrong.
- Work comfortably with **hundreds of namespaces and thousands of pods**.

## Functional requirements

### Tree

- **F1** Two levels ([P15](decisions.md#p15-a-cluster-level-and-namespaces-you-open)).
  The **cluster level** lists the namespaces of the cluster and, under a
  `cluster` header, the cluster-scoped sections (F67); nothing expands in
  place there. Opening a namespace (`→`, `enter`) shows its contents
  without the namespace row: its workloads (Deployments, StatefulSets,
  DaemonSets, Jobs, CronJobs), below each workload its pods, then its
  `custom resources` section. `←` from the top of a namespace, or `⌫` from
  anywhere in it, goes back.
- **F2** Group the workloads of a namespace by kind under a non-selectable
  header (`deployments`, `statefulsets`, …), in that order.
- **F3** *(planned)* Pods without an owning workload are listed under a
  `pods` header of their namespace.
- **F4** On startup the cluster level is shown, or the namespace given by
  `--namespace`, else the one the kubeconfig context sets. Inside a
  namespace everything starts collapsed. The flow is the same for every
  user, also one who may list only their own namespace.
- **F8** Inside a namespace the header is a breadcrumb (`kind-dev › shop`),
  so the user sees where they are, and the key bar shows `⌫ back`. Its
  selection and expansion are kept when the user goes back and opens it
  again (for the session).
- **F5** A collapsed object shows the number of its **direct** children,
  e.g. `[2]`.
- **F6** A collapsed object shows the number of issues **hidden** in all
  its descendants (`·1 ·2 ·3`), one count per level: danger in red, then
  warnings in yellow, then information in blue. Without issues of its own
  it then shows no word, since `healthy` would read as if everything below
  were fine.
- **F7** Expansion state of descendants is remembered when a parent is
  collapsed and restored when it is expanded again (except after "collapse
  all below", which resets it for the collapsed objects).

### More resource kinds (planned, except F67)

See [P14](decisions.md#p14-resources-beyond-workloads) and the
[planned kinds](state-model.md#planned-kinds) of the state model.

- **F60** Below the workloads, a namespace lists its other resources,
  grouped by kind like the workloads (F2), in this order: `services`,
  `ingresses`, `persistentvolumeclaims`, `custom resources` (F67), then
  `configmaps`, `secrets`. Groups without objects are not shown.
- **F61** Configuration (`configmaps`, `secrets`) comes last: a namespace
  can hold hundreds of them (Helm keeps one Secret per release revision),
  and they are for browsing, not for finding problems.
- **F62** Kinds that can be broken get issues like workloads (Service
  without endpoints, Ingress without address or with a missing backend,
  PVC pending or lost). Kinds that can't (ConfigMap, Secret) show a muted
  fact instead of `healthy` (`3 keys`), which would claim a check that never
  ran. Services, Ingresses and PVCs show a muted fact when they are fine
  (type and endpoints, hosts, size and storage class).
- **F63** Relations between resources are shown in the detail pane, not as
  tree levels: a Service names the pods it selects and their workload, an
  Ingress the Services it routes to, a PVC the pods that mount it. The tree
  stays an ownership tree, so no object appears twice.
- **F64** Secret values are never shown unless the user reveals them on
  purpose, in the detail pane and in the YAML (F26) alike. Keys and sizes
  are shown.
- **F65** If listing a kind is forbidden (RBAC), its group shows `error`
  with the API server's message; the rest of the namespace loads normally.
- **F66** Cluster-scoped resources: a `nodes` group at the top level, next
  to the namespaces, with issues per node (not ready, memory, disk or PID
  pressure, cordoned). PersistentVolumes are reached from their PVC.
- **F67** *(UI implemented)* Custom resources (CRDs, e.g. OpenShift
  Routes, cert-manager Certificates, Argo CD Applications) are listed in a
  `custom resources` **section** of their namespace: a selectable row that
  is collapsed until it is expanded, like a namespace, with `[n]` and the
  issues below it when collapsed, but no state word of its own. Inside it,
  one header per kind (its plural, e.g. `certificates`), ordered
  alphabetically. Selected, the detail pane shows the number of objects,
  the objects per kind and the issues below. A kind is shown in a
  namespace only if it has objects there, so CRDs that are installed but
  unused cost no rows. If two kinds share a plural, their API group is
  added (`applications argoproj.io`); whether plurals collide is decided
  over the whole cluster, so a kind has the same header in every namespace.
  Cluster-scoped custom resources get the same section at the top level,
  after the namespaces (and `nodes`, F66), so a cluster with thousands of
  them still starts with one row for them. Their state comes from the
  `Ready` condition, which most operators set: `False` is `not ready`;
  without a Ready condition the muted word is `unchecked`, not `healthy`
  (F62). The detail pane lists all conditions. There is no configuration:
  which kinds appear follows from the cluster alone. *(Implemented on mock
  data; discovering the CRDs comes with the Kubernetes provider.)*

### State per object

- **F10** Each object shows at most **one** state word: the most severe
  issue, plus `+n` if there are more. Objects without issues show a muted
  `healthy` (`completed` for a pod that ran to completion). See
  [state-model.md](state-model.md) for the rules.
- **F11** Workloads show ready/desired replicas (`2/3`), pods ready/total
  containers (`0/1`). Every object shows its age, like kubectl (`25m`, `3d`).
- **F13** States load asynchronously per object; an object shows a spinner
  until its state is known. Loading errors are shown as `error`.

### Detail pane

- **F20** Shows the selected object: namespace and kind, name, age, every
  issue in words with an explanation (naming the container it is about).
- **F21** For workloads: ready and updated replicas. For pods: phase, node,
  and per container its state (`running`, `CrashLoopBackOff`, …), restarts
  with the last termination reason and when, and the image.
- **F22** The recent **warning events** of the object, newest first: age,
  reason, count and message; at most five, the rest summarized.
- **F23** Number of children and the issues below them. A namespace also
  counts the issues below it per issue kind (`Issues below`, one line per
  kind), like the cluster (F25); a workload doesn't, since its own word sums
  up its pods.
- **F25** The cluster (header selected): server, version, number of
  namespaces and the issues below, per level and per issue kind (one line
  per kind with the number of objects).
- **F26** *(planned)* The YAML of the selected object, as `kubectl get -o
  yaml` shows it, in a pager.
- **F27** *(planned)* All events of the selected object (normal and
  warning), newest first, in a pager.

### Header and footer

- **F30** The header shows the context, the server version, the issues
  below as one count per level (`7 errors · 9 warnings · 3 notes`; objects
  that failed to load count as errors), how many objects are still loading,
  and the number of namespaces and pods. If it is too narrow it is cut at
  the right end, so the most severe counts stay.
- **F31** The footer shows key hints; `?` steps through the full help and
  the legend. It shows short messages for a moment, on one line (e.g.
  `copied deployment/api`); errors stay longer.

### Navigation

- **F40** Keyboard navigation as specified in
  [ux-guide.md](ux-guide.md#navigation).
- **F41** Mouse: click a row to select it, click `▸`/`▾` to toggle (at the
  cluster level: open), click the context in the breadcrumb to go back,
  scroll wheel moves the selection.

### Tools

- **F53** `r` reloads everything. *(planned)* With a watch, the view follows
  the cluster live, and `r` is only needed after an error.
- **F54** `/` filters what the current level shows (case-insensitive
  substring): at the cluster level the names of namespaces and sections,
  inside a namespace the path below it (kind and name). Changing the level
  closes the filter. See [Filter](ux-guide.md#filter).
- **F55** `!` toggles "problems only": the tree shows only objects with an
  issue or an unknown state (loading, `error`), plus their ancestors,
  expanded; at the cluster level the namespaces and sections with an issue
  of their own or below. It combines with the filter and stays on across
  levels. A line above the footer says
  `problems only`, so a short list is not mistaken for a healthy cluster.
- **F56** `c` copies the name of the selection as kubectl writes it
  (`deployment/api`; a namespace's name; the context for the cluster).
- **F57** `C` opens the context picker: the kubeconfig contexts, filtered
  by typing; enter switches periscope to the selected one. The current
  cluster stays until the new one has loaded, so a context that can't be
  reached (a login that needs a browser, a cluster that is down) only costs
  a message. The kubeconfig is never written. See
  [Context picker](ux-guide.md#context-picker).

### Planned actions

See [ux-guide.md](ux-guide.md#key-reference) for their keys: logs, describe,
restart a rollout, scale, delete a pod.

## Usage

```sh
periscope --mock [--namespace NAME]   # built-in mock cluster; -n NAME for short
periscope update [--check] [--force]
periscope version
```

*(planned)* `periscope [--context NAME] [--namespace NAME]` reads the
cluster of the current (or given) kubeconfig context. `--namespace` starts
inside that namespace (F4).

- **F58** On every start the tool looks for a newer release in the
  background (never blocking, silent on failure; off for dev builds,
  `--mock` and `PERISCOPE_NO_UPDATE_CHECK=1`). If there is one, the footer
  shows `q quit and update to vX` right of help and quit, highlighted; `q`
  then quits and installs the release, and plain quit is `Q`.

## Non-functional requirements

- **N1** Startup must not block on the network. The tool shows its frame at
  once and fills in what the API server returns.
- **N2** Loading many objects must not freeze the UI (async, per object,
  bounded concurrency).
- **N3** Colors follow the user's terminal theme (ANSI 16 colors); the UI
  works on dark and light backgrounds.
- **N4** Meaning never depends on color alone: every state has a word.
- **N5** Works without a Nerd Font. `PERISCOPE_ASCII=1` replaces the few
  remaining Unicode symbols with ASCII.
- **N6** Works in narrow terminals (detail pane hides below 90 columns).
- **N7** The UI is independent of the data source: the mock and the
  Kubernetes provider are interchangeable without UI changes.
- **N8** The Kubernetes provider reads the kubeconfig like kubectl, so the
  user's contexts and credential plugins apply. It never prompts (a plugin
  that needs input fails with its message). It is read-only: it writes only
  when the user asks, with an action that says what it does.
