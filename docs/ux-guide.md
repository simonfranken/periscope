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

Two levels (P15). The **cluster level** lists the namespaces and, under
`cluster`, the cluster-scoped sections; nothing expands in place there:

```
 kind-dev  v1.34.1                    7 errors · 10 warnings · 3 notes · 8 namespaces · 25 pods
 ▸ argocd [2]               healthy                 90d  │ cluster
 ▸ payments [2]             ·3 ·4                   90d  │ kind-dev                       age 120d
 ▸ shop [6]                 ·4 ·3 ·2                90d  │
   legacy-feature           terminating            400d  │ Server      https://127.0.0.1:6443
 cluster                                                 │ Namespaces  8 · 7 errors, 10 warnings, 3 notes
   ▸ custom resources [1]                                │
 ↑↓/jk move • →/enter open • ! problems only • / filter • r reload             ? help • q quit
```

`→` / `enter` on a namespace (or section) **opens** it: the tree shows its
contents without the namespace row, and the header becomes a breadcrumb:

```
 kind-dev › shop  v1.34.1                              4 errors · 3 warnings · 2 notes · 7 pods
 deployments                                                    │ shop · pod
   ▾ api                         degraded +1        2/3  30d    │ api-7b8d9f6c5-zp6qx                 age 25m
       api-5f9c7d8b6-9vn2c       healthy            1/1   3d    │
       api-7b8d9f6c5-zp6qx       crashloop +1       0/1  25m    │ crashloop   container keeps crashing (app)
 statefulsets                                                   │ restarting  restarted in the last hour (app)
   ▸ postgres [1]                ·1                 1/1  30d    │
 ▸ custom resources [4]          ·1                             │ Phase       Running
                                                                │ Node        node-2
                                                                │ c copy name
 ↑↓/jk move • ←→/hl collapse/expand • ⇧←→/HL all below • space toggle • ⌫ back • ! problems only • / filter • r reload
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
   danger words), `Status` (the other issue words, `healthy`, `completed`,
   `unchecked`),
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
| status | the widest visible status, at least 16 | issue word, `+n`, `·k` hidden issues per level (red, yellow, blue); muted `healthy` / `completed` / `unchecked` without issues; spinner while loading; `error` |
| ready | 5, right-aligned | `ready/desired` replicas of a workload, `ready/total` containers of a pod |
| age | 4, right-aligned, muted | like kubectl: `40s`, `25m`, `3h`, `12d` |

- The name column is only as wide as needed, so states stay close to names.
  The status column is never cut (`degraded +1 ·1 ·1`): if space is short,
  the names give way and are cut with `…`.
- Group headers (`deployments`, `statefulsets`) are muted and not
  selectable.
- A section (`custom resources`) is a selectable, collapsible row like a
  namespace, collapsed on start. It has no state of its own, so its status
  cell is empty unless it is collapsed with issues below (`·1`); `[n]`
  counts its objects.
- Rows without issues: name and counts muted (except the selected row),
  status `healthy` in muted grey. An empty cell would look like missing
  data; the muted word confirms the check ran.
- A collapsed row without issues of its own but with issues below shows
  only the counts (`·3 ·4`), not `healthy`.

### Detail pane

1. Namespace and kind (muted, `shop · pod`, `shop ·
   certificate.cert-manager.io`), then the name (bold) with the age
   right-aligned (`age 25m`).
2. One line per issue: word (colored, 12 columns) + explanation, naming the
   container (`container keeps crashing (app)`). `healthy  nothing to do`
   if there are none here and none below (`unchecked  no Ready condition
   to read` for a custom resource without one).
3. Facts: `Ready 2/3 · 1 updated` (workloads), `Phase`, `Node` (pods),
   `Objects` (namespaces) / `Pods` (workloads): count and the issues below.
   - `Issues below` (namespaces and the cluster only): one line per issue
     kind, the word in its color (12 columns) and the number of objects
     with it, most severe first; left out if there are none.
4. `Conditions` (custom resources): type, status (in the color of the
   issue about it, muted otherwise), reason; the message below, muted.
   `Containers` (pods): name, state (in the color of the issue about it, muted
   otherwise), restarts with the last reason and its age; the image below,
   muted.
5. `Warnings`: up to five events, newest first: age (muted), reason
   (yellow), `×count`, message; `… n more`.
6. The palette at the bottom.

For the cluster (header selected): `Server`, `Version`, `Namespaces`
(count and the issues below) and `Issues below`.

For a section: its namespace (or `cluster`) and name, `Objects` (count and
the issues below, or `no issues`), `Kinds` (objects per kind header) and
`Issues below`. No palette: a section has nothing to act on, and `c` copies
nothing.

### Header

The header is the row of what the tree shows: the cluster at the cluster
level, the namespace (or section) inside one. It is drawn with the selected
background (bold) while selected, and selected on startup at the cluster
level.

`context  version  [state]   n errors · n warnings · n notes · n loading · n namespaces · n pods`

Inside a namespace it is a breadcrumb, and the counts are the namespace's:

`context › shop  version  [state]   n errors · n warnings · n notes · n loading · n pods`

The breadcrumb shows where the user is and the way back: a click on the
context's name goes back to the cluster level, like `⌫`. The own state
word (the cluster's or the namespace's) is shown only if it says something
(an issue, loading, `error`; e.g. `terminating`). The counts form one group at the right edge. Issues are
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
| `not ready` | running, but not ready (`c`); Ready condition is False (`reason`) for a custom resource |
| `degraded` | only `r` of `n` replicas ready |
| `restarting` | restarted in the last hour (`c`) |
| `rolling out` | `u` of `n` replicas updated |
| `suspended` | schedule suspended, no new runs |
| `terminating` | namespace is being deleted |
| `healthy` | nothing to do |
| `unchecked` | no Ready condition to read (custom resources) |

## Navigation

| Key | Behavior |
|---|---|
| `↑` / `k`, `↓` / `j` | Previous / next object; group headers are skipped. `↑` from the first row selects the header |
| `space` | Toggle expand/collapse of the selection; at the cluster level: open it |
| `→` / `l`, `enter` | Header selected: first row. At the cluster level: open the namespace or section (empty ones can't be opened). Inside: collapsed with children: expand and select the first child; expanded: select the first child; leaf: nothing |
| `←` / `h` | Expanded: collapse it (stays selected). Otherwise: collapse the parent and select it. A collapsed top-level row or the header inside a namespace: back to the cluster level. At the cluster level: select the header |
| `⌫` (backspace) | Back to the cluster level from anywhere inside, with the namespace selected |
| `L` / `shift+→` / `ctrl+→` | Expand the selection and everything below it; the selection and its siblings stay. On the header inside a namespace: all of it. At the cluster level: open the selection with everything expanded |
| `H` / `shift+←` / `ctrl+←` | Collapse the selection and everything below it (resets remembered expansion there); siblings stay. With nothing to collapse (a leaf, a collapsed object) it collapses the parent and selects it, so repeating it climbs up: to the header (which collapses the namespace), then back to the cluster level. On the cluster: forget all expansion |
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

**Levels.** Opening a namespace selects what was selected when it was last
left (else its first row), and its expansion is kept, for the session.
Going back selects the namespace. Changing the level closes the filter (it
applies to one level); problems only stays on. If a reload no longer has
the namespace that is open, the cluster level is shown with `<name> is
gone`.

**Start.** `--namespace NAME` (`-n`) starts inside `NAME`; without it,
periscope starts inside the kubeconfig context's namespace if the context
sets one, else at the cluster level. An unknown name shows `no namespace
NAME` and the cluster level. The flow is the same for every user: someone
who may list only their own namespace sees a cluster level with that one
namespace.

### Filter

`/` shows a text input above the footer (`/ term`). It filters only what
the current level shows. At the cluster level: the namespaces and sections
whose name contains the term. Inside a namespace: the objects whose path
below the namespace (kind and name, e.g. `deployment/api`) contains the
term, ignoring case; their ancestors stay as context and are shown expanded. Group headers without a match
vanish. If nothing matches the tree says `nothing matches`.

- **Selection.** If the selection is filtered out, the next visible object
  (in display order) is selected, else the previous one.
- **Typing.** The input has the focus: letters edit the term, so `j`, `q`
  etc. are text. `↑` `↓` `pgup` `pgdn` still move the selection. `enter`
  hands the keys back to the tree (the filter stays); `/` edits again.
- **`esc`** closes the input and removes the filter. The selection stays;
  its ancestors are expanded so it is still visible.
- The header is never filtered out; its counts are not filtered.

### Problems only

`!` reduces the tree to what needs attention, with the filter's rules and
"match" meaning *has an issue* (`Status.Issues()`); at the cluster level,
a namespace or section with an issue of its own or below it. Loading and
failed objects count as problems, since their state is unknown. It combines with
the filter, stays until `!` is pressed again, and shows `problems only`
(yellow) and `! show all` above the footer (inside the filter line:
`problems only (!)`). With no problem the tree says `no problems`.

### Popups (planned)

The context picker opens in the middle of the screen as in
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
| `←` / `h`, `→` / `l` | Fold / unfold, parent / first child; back / open a namespace | Navigation | Bar, Help |
| `enter` | Open a namespace; expand | Navigation | Bar (cluster level), Help |
| `⌫` | Back to the cluster level | Navigation | Bar (inside a namespace), Help |
| `H` / `⇧←`, `L` / `⇧→` | Collapse / expand all below | Navigation | Bar, Help |
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
| `C` | Context picker (popup) | Tools |

Not `l` for logs (k9s' key): `l` moves right, as in submarine. Not `R` for
restart: it sits next to `r` (reload), the reason submarine's restore is `x`
(submarine D23).

### Mouse

- Click a row: select it. Click the header: select what it stands for.
- Click `▸` / `▾`: select and toggle (at the cluster level: open).
- Click the context's name in the breadcrumb: back to the cluster level.
- Scroll wheel: move the selection.

## Accessibility and terminals

- `PERISCOPE_ASCII=1`: ASCII symbols (`> v . * >`) instead of Unicode.
- Many setups capture `ctrl+←/→`, so expand/collapse all below also work as
  `shift+→/←` and as `L` / `H`.
