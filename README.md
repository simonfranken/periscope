# periscope

**A terminal UI for Kubernetes clusters.**
See what needs your attention. Ignore the rest.

`kubectl get pods -A` gives you a wall of rows, and the reason a pod is
broken hides in `describe` and the events. periscope answers the questions
you actually have:

- Is anything broken in this cluster, and where?
- Why is it broken: crashing, out of memory, an image that can't be pulled?
- Which namespaces and workloads are fine, so I can ignore them?

## Why you might like it

- **The cluster as a tree.** Namespaces, then their workloads grouped by
  kind (deployments, statefulsets, daemonsets, jobs, cronjobs), then their
  pods. Collapsed until you open them.
- **Problems are loud, healthy is quiet.** Objects without issues are a
  muted `healthy`. Real problems are words in red (`crashloop`,
  `oom killed`, `pull error`, `down`, `failed`) or yellow (`pending`,
  `not ready`, `degraded`, `restarting`). A collapsed namespace counts the
  problems hidden below it (`·3 ·4`).
- **Words, not symbols.** Every state is a short word, so nothing depends on
  color alone. `?` shows every key and what each word means.
- **The reason, not just the state.** The detail pane explains each issue,
  lists the containers with their state and restarts, and shows the recent
  warning events.
- **Problems only.** `!` cuts the tree down to what's broken, with the
  namespace and workload around it.

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/simonfranken/periscope/main/install.sh | sh
```

Installs `peri` and `periscope` (linux/macOS, amd64/arm64) to
`~/.local/bin`. Update later with `peri update`.

## Try it

```sh
peri --mock          # built-in sample cluster
```

Reading a real cluster from your kubeconfig is the next step; see
[docs/README.md](docs/README.md#status).

## Keys

| Key | Action |
|---|---|
| `↑` `↓` / `j` `k` | Move |
| `←` `→` / `h` `l` | Fold / unfold, parent / first child |
| `H` `L` / `⇧←` `⇧→` | Collapse / expand all below the selection (everything on the header) |
| `space` | Toggle the selected object |
| `home` `end` / `g` `G`, `pgup` `pgdn` | Jump, page |
| `!` | Problems only |
| `/` | Filter by namespace, kind or name |
| `c` | Copy the name (`deployment/api`) |
| `r` | Reload |
| `?` | Full help: every key, then what the words mean (`esc` closes it) |
| `q` | Quit. If a newer release exists, the footer offers `q quit and update to vX` (highlighted), and plain quit moves to `Q` |

The mouse works too: click to select, click `▸` to toggle, wheel to move.

## Documentation

Requirements, state rules, UX guide, architecture and the decision log are in
[docs/](docs/README.md).

## Development

```sh
go run ./cmd/periscope --mock
go vet ./... && go test ./...
```
