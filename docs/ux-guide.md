# UX guide

The principles, colors and most patterns are those of submarine; this guide
is the reference for periscope.

## Principles

1. **Attention follows severity.** Healthy objects are quiet, problems are
   loud, dangers are loudest. Users should be able to scan a long list and
   only see what needs action.
2. **One word per row.** At most one state word in the tree (`+n` for more);
   the detail pane lists everything.
3. **Words over symbols.** States are short words that need no legend.
   Symbols are only used where they are conventional (`▸▾`, `n/m`).
4. **Meaning never depends on color alone** (every state has a word).
5. **Follow the user's terminal theme.** Use ANSI 16 colors for meaning.
6. **The screen explains itself.** Labels say what a key does, important
   keys are in the footer, `?` explains every word (submarine D32).

## Layout

```
 kind-dev  v1.34.1                              7 errors · 9 warnings · 3 notes · 7 namespaces · 22 pods
 ▸ default [1]                     healthy                 90d  │ shop · pod
 ▸ payments [2]                    ·3 ·4                   90d  │ api-7b8d9f6c5-zp6qx                 age 25m
 ▾ shop                            healthy                 90d  │
   deployments                                                  │ crashloop   container keeps crashing (app)
     ▾ api                         degraded +1        2/3  30d  │ restarting  restarted in the last hour (app)
         api-5f9c7d8b6-9vn2c       healthy            1/1   3d  │
         api-7b8d9f6c5-zp6qx       crashloop +1       0/1  25m  │ Phase       Running
   statefulsets                                                 │ Node        node-2
     ▾ postgres                    healthy            1/1  30d  │
         postgres-0                oom killed         1/1   3d  │ Containers
   legacy-feature                  terminating            400d  │ app         CrashLoopBackOff · 14 restarts, last 40s ago (Error)
                                                                │             registry.example.com/shop/api:1.8.0
                                                                │
                                                                │ Warnings
                                                                │ 40s  BackOff ×61 Back-off restarting failed container app
                                                                │ c copy name
 ↑↓/jk move • ←→/hl collapse/expand • ⇧←→/HL all • space toggle • ! problems only • / filter • r reload   ? help • q quit
```

| Area | Height / width |
|---|---|
| Header | 1 line |
| Tree | Remaining width |
| Detail pane | 40% of the width, at least 36 columns; hidden below 90 columns |
| Line above the footer | 1 line, only while filtering, showing problems only, or until the first key (hint) |
| Footer | 1 line (more with full help) |

### Key bar and palette

Keys come in four groups (`keyMap.Navigation`, `.Tools`, `.Resource`,
`.Global`):

- **Navigation** (move, collapse/expand, toggle): left in the footer.
- **Tools** (`!` problems only, `/` filter, `r` reload): in the footer after
  the navigation keys, most useful first.
- **Global** (`?`, `q`): right-aligned in the footer, always kept. If a newer
  release exists, `q quit and update to vX` follows them as the right-most
  item, on a highlighted background (`Theme.Highlight`: black on Primary),
  and plain quit is `Q`.
- **Resource** (act on the selected object, e.g. `c`): a palette at the
  bottom of the detail pane, wrapped onto more lines if the pane is too
  narrow. Left out if the pane is shorter than 12 lines; whenever the
  palette isn't shown, these keys move into the footer after the tools.

If the footer is too narrow, keys are dropped from the left group's end
(resource keys first, then the tools); the global keys stay. Keys work
whether or not they are shown.

### Full help

`?` steps through two pages, then back to the key bar; `esc` closes either
page at once (before it would close the filter):

1. **Keys**: every key in titled columns: `Move`, `Jump`, `Selected`,
   `Everything`, `App`. The `App` column says `? what words mean` and
   `esc close`.
2. **Legend**: the words and symbols in their colors: `Problems` (the
   danger words), `Status` (the other issue words, `healthy`, `completed`),
   `Symbols` (`n/m`, `+n`, `·k`, `[n]`, the age). Then `App` with
   `esc close`. The words come from `Theme.Legend()`, the levels from
   `IssueKind.Level()`, so they are the tree's.

Titles are muted and bold. Columns that don't fit are left out from the
right, marked with `…`; the `App` column is always kept.

### Start hint

Until the first key, the line above the footer reads muted `new here? ?
shows every key and what the words mean`. No state is kept between starts.

### Tree row

```
indent ▸ name [n]   status          ready  age
```

| Column | Width | Content |
|---|---|---|
| indent | 2 per depth | |
| toggle | 3 | `▸` collapsed, `▾` expanded, blank for leaves |
| name | fits the widest visible name | name, `[n]` = direct children (collapsed only) |
| status | the widest visible status, at least 16 | issue word, `+n`, `·k` hidden issues per level (red, yellow, blue); muted `healthy` / `completed` without issues; spinner while loading; `error` |
| ready | 5, right-aligned | `ready/desired` replicas of a workload, `ready/total` containers of a pod |
| age | 4, right-aligned, muted | like kubectl: `40s`, `25m`, `3h`, `12d` |

- The name column is only as wide as needed, so states stay close to names.
  The status column is never cut (`degraded +1 ·1 ·1`): if space is short,
  the names give way and are cut with `…`.
- Group headers (`deployments`, `statefulsets`) are muted and not
  selectable.
- Rows without issues: name and counts muted (except the selected row),
  status `healthy` in muted grey. An empty cell would look like missing
  data; the muted word confirms the check ran.
- A collapsed row without issues of its own but with issues below shows
  only the counts (`·3 ·4`), not `healthy`.

### Detail pane

1. Namespace and kind (muted, `shop · pod`), then the name (bold) with the
   age right-aligned (`age 25m`).
2. One line per issue: word (colored, 12 columns) + explanation, naming the
   container (`container keeps crashing (app)`). `healthy  nothing to do`
   if there are none here and none below.
3. Facts: `Ready 2/3 · 1 updated` (workloads), `Phase`, `Node` (pods),
   `Workloads` / `Pods` (count and the issues below).
   - `Issues below` (namespaces and the cluster only): one line per issue
     kind, the word in its color (12 columns) and the number of objects
     with it, most severe first; left out if there are none.
4. `Containers`: name, state (in the color of the issue about it, muted
   otherwise), restarts with the last reason and its age; the image below,
   muted.
5. `Warnings`: up to five events, newest first: age (muted), reason
   (yellow), `×count`, message; `… n more`.
6. The palette at the bottom.

For the cluster (header selected): `Server`, `Version`, `Namespaces`
(count and the issues below) and `Issues below`.

### Header

The header is the cluster's row: selected on startup and drawn with the
selected background (bold) while selected.

`context  version  [state]   n errors · n warnings · n notes · n loading · n namespaces · n pods`

The cluster's own state word is shown only if it says something (an issue,
loading, `error`). The counts form one group at the right edge. Issues are
counted **per level**, as in the tree's hidden counts (`·1 ·2 ·3`): danger
(including objects that failed to load), warn and info, each in its level
color; zero counts are omitted. A real cluster has many kinds of issues at
once, and one count per kind overloaded the line; the counts per kind are
in the cluster's detail pane (`Issues below`). If the line is too narrow it
is cut with `…` at the right end, so the most severe counts stay.

## Colors

All colors are defined in `internal/tui/theme/theme.go`. Never hard-code
colors in components.

| Role | Color | Used for |
|---|---|---|
| Danger | ANSI `1` red, bold | `down`, `crashloop`, `oom killed`, `pull error`, `failed`, `error` |
| Warn | ANSI `3` yellow | `pending`, `not ready`, `degraded`, `restarting`, event reasons, `problems only` |
| Info | ANSI `4` blue | `rolling out`, `suspended`, `terminating` |
| Primary | ANSI `5` magenta | the context, spinner, filter prompt |
| Muted | ANSI `8` grey | quiet rows, group headers, labels, ages, images |
| Text | terminal default | normal text |
| Border | ANSI `8` grey | pane and help separators |
| Bar | adaptive 254 (light) / 235 (dark) | header background |
| Selected | adaptive 253 (light) / 237 (dark) | selected row background |
| Backdrop | adaptive 252 (light) / 238 (dark) | the screen behind a popup (planned pickers) |
| Highlight | ANSI `0` black, bold, on Primary | the `quit and update` hint in the footer |

- Background shades have no ANSI-16 equivalent, so they use `AdaptiveColor`.
  The background is detected once in `main` before Bubble Tea starts.
- Styles that render on a background (header, selected row) take a `base`
  style (`LevelStyle(level, base)`), so inner segments keep the background.

## Wording

| Word | Detail pane text |
|---|---|
| `down` | no replica ready, 0/`n` |
| `crashloop` | container keeps crashing (`c`) |
| `oom killed` | container ran out of memory (`c`) |
| `pull error` | image can't be pulled (`c`) |
| `failed` | ran and failed |
| `pending` | not scheduled or not started yet |
| `not ready` | running, but not ready (`c`) |
| `degraded` | only `r` of `n` replicas ready |
| `restarting` | restarted in the last hour (`c`) |
| `rolling out` | `u` of `n` replicas updated |
| `suspended` | schedule suspended, no new runs |
| `terminating` | namespace is being deleted |
| `healthy` | nothing to do |

## Navigation

| Key | Behavior |
|---|---|
| `↑` / `k`, `↓` / `j` | Previous / next object; group headers are skipped. `↑` from the first namespace selects the header (cluster) |
| `space` | Toggle expand/collapse of the selection |
| `→` / `l` | Header selected: first namespace. Collapsed with children: expand and select the first child. Expanded: select the first child. Leaf: nothing |
| `←` / `h` | Expanded: collapse it (stays selected). Otherwise: collapse the parent and select it. Top level: select the header |
| `L` / `shift+→` / `ctrl+→` | Expand all; then follow the first child from the selection down to a leaf |
| `H` / `shift+←` / `ctrl+←` | Collapse all (resets remembered expansion); select the top-level ancestor |
| `home` / `g`, `end` / `G` | Header / last object |
| `pgup`, `pgdn` | Move by one screen |
| `?` | Full help: keys, then the legend, then closed |
| `esc` | Close the full help; else close the filter |
| `q`, `ctrl+c` | Quit. With a newer release: `q` quits and updates, `Q` and `ctrl+c` quit |
| `r` | Reload everything |
| `c` | Copy the name of the selection (`deployment/api`, a namespace's name, the context for the cluster) to the clipboard (OSC 52, works over SSH; in tmux `set -g set-clipboard on`). The footer confirms with `copied <name>` |
| `/` | Open the filter, see [Filter](#filter) |
| `!` | Toggle problems only, see [Problems only](#problems-only) |

"First child" always means the first child **as displayed** (children are
grouped by kind). Collapsing an ancestor of the selection moves the
selection to that ancestor. The view scrolls so the selection is always
visible.

### Filter

`/` shows a text input above the footer (`/ term`). While it is open the
tree shows only the objects whose path (namespace, kind and name, e.g.
`shop/deployment/api`) contains the term, ignoring case; their ancestors
stay as context and are shown expanded. Group headers without a match
vanish. If nothing matches the tree says `nothing matches`.

- **Selection.** If the selection is filtered out, the next visible object
  (in display order) is selected, else the previous one.
- **Typing.** The input has the focus: letters edit the term, so `j`, `q`
  etc. are text. `↑` `↓` `pgup` `pgdn` still move the selection. `enter`
  hands the keys back to the tree (the filter stays); `/` edits again.
- **`esc`** closes the input and removes the filter. The selection stays;
  its ancestors are expanded so it is still visible.
- The cluster is never filtered out; the header counts are not filtered.

### Problems only

`!` reduces the tree to what needs attention, with the filter's rules and
"match" meaning *has an issue* (`Status.Issues()`). Loading and failed
objects count as problems, since their state is unknown. It combines with
the filter, stays until `!` is pressed again, and shows `problems only`
(yellow) and `! show all` above the footer (inside the filter line:
`problems only (!)`). With no problem the tree says `no problems`.

### Popups (planned)

Pickers (context, namespace) open in the middle of the screen as in
submarine's branch picker: a search line, a list, `↑↓ select · enter … ·
esc cancel`; the screen behind is dimmed (`Backdrop`). Actions that can
lose something ask on the line above the footer: `Delete pod
shop/api-7b8d9f6c5-zp6qx?   y delete · n cancel`.

### Key reference

Every key, where it is shown. Update this table when a key is added or
moved. "Bar" is the footer bar, "Palette" the detail pane's palette, "Help"
the full help.

| Key | Action | Group | Shown in |
|---|---|---|---|
| `↑` / `k`, `↓` / `j` | Move selection | Navigation | Bar, Help |
| `←` / `h`, `→` / `l` | Fold / unfold, parent / first child | Navigation | Bar, Help |
| `H` / `⇧←`, `L` / `⇧→` | Collapse / expand all | Navigation | Bar, Help |
| `space` | Toggle expand/collapse | Navigation | Bar, Help |
| `home` / `g`, `end` / `G` | Header / last object | Navigation | Help |
| `pgup`, `pgdn` | Move by one screen | Navigation | Help |
| `c` | Copy name | Resource | Palette, Help |
| `!` | Problems only (toggle) | Tools | Bar, Help |
| `/` | Filter | Tools | Bar, Help |
| `r` | Reload everything | Tools | Bar, Help |
| `?` | Full help: keys, legend, close | Global | Bar, Help |
| `esc` | Close the full help (and the filter) | Global | Help |
| `q`, `ctrl+c` | Quit (`Q`, `ctrl+c` while an update is offered) | Global | Bar, Help |
| `q` | Quit and update to the newer release (only if there is one) | Global | Bar (highlighted), Help |

Planned, not implemented (keys reserved):

| Key | Action | Group |
|---|---|---|
| `v` | View the logs of a pod (the previous container's for a crashloop), in a pager | Resource |
| `d` | Describe, in a pager | Resource |
| `e` | Edit in `$EDITOR` | Resource |
| `s` | Scale a workload | Resource |
| `t` | Restart a rollout | Resource |
| `x` | Delete a pod (asks first) | Resource |
| `n` | Namespace picker (popup) | Tools |
| `C` | Context picker (popup) | Tools |

Not `l` for logs (k9s' key): `l` moves right, as in submarine. Not `R` for
restart: it sits next to `r` (reload), the reason submarine's restore is `x`
(submarine D23).

### Mouse

- Click a row: select it. Click the header: select the cluster.
- Click `▸` / `▾`: select and toggle.
- Scroll wheel: move the selection.

## Accessibility and terminals

- `PERISCOPE_ASCII=1`: ASCII symbols (`> v . *`) instead of Unicode.
- Many setups capture `ctrl+←/→`, so expand/collapse all also work as
  `shift+→/←` and as `L` / `H`.
