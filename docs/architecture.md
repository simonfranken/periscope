# Architecture

Go, [Bubble Tea](https://github.com/charmbracelet/bubbletea) (v1),
[Lip Gloss](https://github.com/charmbracelet/lipgloss),
[Bubbles](https://github.com/charmbracelet/bubbles) (key, help, spinner,
textinput), [bubblezone](https://github.com/lrstanley/bubblezone) (mouse
hit areas), [go-osc52](https://github.com/aymanbagabas/go-osc52) (clipboard).
The same stack and versions as submarine. The Kubernetes provider will add
[client-go](https://github.com/kubernetes/client-go) (P5).

## Package layout

```
cmd/periscope/main.go          wiring: choose the provider (--mock), update check, start the program; `update`, `version`
internal/resource/             domain model; NO UI imports
  model.go                     Cluster, Object, Kind, Status, Container, Event, Walk
  status.go                    Issues(), levels, HiddenIssues()
  provider.go                  Provider interface
internal/resource/mock/        fixture cluster with every issue word, artificial latency
internal/resource/kube/        (planned) provider using client-go
internal/update/               self-update from GitHub releases; NO UI imports
internal/tui/                  Bubble Tea application
  app.go                       root model: layout, message routing, loading, filter, problems only
  keys.go                      key bindings (also feed the help view)
  msgs.go                      tree/status/update messages
  clipboard.go                 OSC 52
  theme/                       colors, glyphs, issue words and texts, level styles, legend
  tree/                        tree component: rows, group headers, navigation, filter, mouse, view
  detail/                      detail pane and its palette
  header/                      header line
  footer/                      key bar (navigation and tools left, global right), full help pages, flash messages
```

Much of `internal/tui` (tree navigation and filter, footer, help, theme
helpers, update) started as a copy of submarine's and keeps its structure,
so a fix in one can be carried over to the other. There is no shared module
(yet): the two tools release independently.

### Layering rules

- `internal/resource` knows nothing about the UI. Everything that decides
  **what** a state means (issue kinds, levels, hidden issues) lives here.
- `internal/tui/theme` decides **how** it looks: words, colors, symbols.
- Components (`tree`, `detail`, `header`, `footer`) use the theme; they never
  define colors or classify states themselves.
- The UI only accesses data through `resource.Provider`.

## Provider boundary

```go
type Provider interface {
    Tree(ctx context.Context) (*Cluster, error)               // structure only, fast
    Status(ctx context.Context, path string) (*Status, error) // per object, may be slow
}
```

- `Tree` returns the cluster and all objects with `Parent` and `Children`
  linked and `Group` set, passed through `resource.Arrange` (display order;
  custom resources moved into `custom resources` sections). `Cluster.Objects`
  is the top level: namespaces, then the section of cluster-scoped custom
  resources. `Cluster.Namespace` is the kubeconfig context's namespace, where
  the UI starts (F4); "" if the context sets none. `Status` fields are nil,
  except the sections' (empty).
- `Status` is called once per object except sections, concurrently. It must be safe to call
  concurrently and must respect `ctx`.
- `Object.Path` is the unique ID (see
  [state-model.md](state-model.md#objects)).

Planned optional capabilities, type-asserted by the app like submarine's
(`Fetcher`, `Watcher`, …):

```go
type Watcher interface {
    Watch(ctx context.Context) <-chan Change // {Path, Structure}: reload one status, or the tree
}
type Logger interface {
    Logs(ctx context.Context, path, container string, previous bool) (io.ReadCloser, error)
}
```

The mock provider (`internal/resource/mock`) covers every issue word and
sleeps 100–800 ms per `Status` call to exercise the loading path.

### Kubernetes provider (planned)

- `clientcmd` loads the kubeconfig with kubectl's rules (`KUBECONFIG`,
  `--context`, `--namespace`), so contexts, exec credential plugins and
  proxies work as in kubectl. Exec plugins run without a TTY: one that
  needs interaction fails with its message instead of prompting.
- `Tree` lists namespaces, workloads and pods with a few `List` calls (not
  one per object) and links pods to workloads by owner references
  (Pod → ReplicaSet → Deployment, Pod → Job → CronJob). The list results are
  cached, so `Status` maps from the cache and is cheap; events are listed
  once per namespace with `type=Warning`.
- Concurrency is bounded (as submarine's D16), and every call has a
  timeout, so an unreachable API server shows `error` instead of hanging.
- `Watch` uses shared informers (watches instead of polling, P5) and
  reports changes as `Change`, so the app reloads what changed, like
  submarine's watcher (D20).

## Loading flow

1. `Init` runs `Tree()` → `treeLoadedMsg`, and in parallel the update check.
2. The app indexes objects by path and starts one `tea.Cmd` per object
   calling `Status()` → `statusLoadedMsg`.
3. Each result is stored on the object (`Status` or `Err`); the view
   updates incrementally, including parents' hidden-issue counts.
4. `r` loads the tree again (ignored while a load runs). The new tree
   carries the old `Status` and `Err` over by path, and
   `tree.ReplaceRoots` keeps expansion and the cursor, so a reload doesn't
   flash spinners or move the selection.
5. The spinner ticks only while something loads.

## Tree component

- State: roots, `expanded map[path]bool`, cursor (an `*Object`), scroll
  offset, size, filter, problems only.
- `rows()` flattens the visible tree into group-header rows and object
  rows. Grouping uses `Object.Group` and keeps the order of first
  appearance.
- Navigation methods (`Up`, `Down`, `Left`, `Right`, `Toggle`,
  `CollapseBelow`, `ExpandBelow`, `Home`, `End`, `PageUp`, `PageDown`) are plain
  methods without Bubble Tea dependencies, so they are unit-tested directly.
- Key mapping happens in `app.go`; the tree only handles mouse messages.
- Mouse zones: `toggle:<path>` and `row:<path>`, prefixed per instance.

## Rendering notes

- Each segment of a row is rendered with the row's `base` style
  (background), because an inner style's reset would otherwise clear the
  outer background.
- Widths are measured with `lipgloss.Width`; truncation uses
  `ansi.Truncate`, which keeps escape sequences and bubblezone markers intact.
- `zone.Scan` runs once, on the root view.

## Distribution

The same process as submarine (P7). No CI runner: releases are built and
published from a developer machine.

- `make dist` cross-compiles `CGO_ENABLED=0` binaries for linux/darwin ×
  amd64/arm64 into `dist/` plus `checksums.txt`; the version comes from
  `git describe` and is baked in with `-X main.version`.
- `make release` (needs `GH_TOKEN` in `.env`, see `.env.template`; uses the
  GitHub API with `curl`; HEAD must be a clean `vX.Y.Z` tag pushed to
  GitHub) runs tests, builds and creates the GitHub release `vX.Y.Z` with
  the assets `periscope_<os>_<arch>`, `checksums.txt` and `VERSION`. It is
  created without notes as a **draft** and published last, so
  `releases/latest` never points at a release with missing assets. Clients
  read, without the API (plain redirects, no rate limit), below
  `https://github.com/simonfranken/periscope/releases`:
  - `latest/download/VERSION`
  - `download/<version>/periscope_<os>_<arch>` and `checksums.txt`
- The `release` skill (`.claude/skills/release/SKILL.md`) drives a release:
  latest tag → next semver from the commits since → check (`vet`, `test`,
  `gofmt`, `make dist`) → tag and push → the developer runs `make release` →
  the skill verifies that `latest/download/VERSION` serves the new version,
  then writes the release notes.
- `install.sh` (`curl -fsSL
  https://raw.githubusercontent.com/simonfranken/periscope/main/install.sh |
  sh`) detects OS/arch, verifies the sha256, installs `periscope` and a
  `peri` symlink to `~/.local/bin` (`PERISCOPE_INSTALL_DIR`,
  `PERISCOPE_VERSION`, `PERISCOPE_BASE_URL` override).
- `periscope update [--check] [--force]` (package `internal/update`) reads
  `VERSION`, downloads the asset for the running platform, verifies the
  checksum and atomically replaces the real executable (symlinks resolved,
  so `peri` works too). `PERISCOPE_UPDATE_URL` overrides the releases root.
- On every start the TUI looks for a newer release in the background (3 s
  timeout, no cache, no blocking). `main` passes `tui.WithUpdateCheck` and
  the TUI gets the result as a message; the footer then offers `q quit and
  update to vX`. That key quits, and `main` installs the release once the
  terminal is restored (the same code as `periscope update`). The check is
  off for dev builds, `--mock` and `PERISCOPE_NO_UPDATE_CHECK=1`.

## Testing

```sh
go vet ./...
go test ./...
```

- `internal/resource`: issue classification and hidden-issue aggregation.
- `internal/resource/mock`: every issue word occurs, paths are unique.
- `internal/tui/tree`: navigation, filter and problems-only rules. Tests
  need `zone.NewGlobal()` (see `TestMain`).
- `internal/tui/footer`: key bar dropping order, help pages.
- `internal/update`: version comparison, download and checksum against a
  local server that redirects like GitHub.

Manual check in a headless terminal:

```sh
go build -o /tmp/periscope ./cmd/periscope
tmux new-session -d -s peri -x 130 -y 30 "/tmp/periscope --mock"
tmux send-keys -t peri L          # expand all below the selection
tmux send-keys -t peri -l '/'     # one key at a time: a burst of keys
tmux send-keys -t peri -l 'shop'  # arrives as one message
tmux capture-pane -pt peri        # add -e to see colors
tmux kill-session -t peri
```

*(planned)* Integration tests against a `kind` cluster built by a script,
skipped if `kind` is not installed.
