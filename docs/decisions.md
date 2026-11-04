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

**Decision.** As submarine D7, D8 and D9. Namespaces are the top level,
collapsed on start; workloads below them, grouped by kind under muted,
non-selectable headers (`deployments`, …); pods below their workload.
ReplicaSets are not shown. Navigation is branch-first (`→` enters, `←`
leaves, `H`/`L` all). Collapsed parents count the issues below them, one
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
