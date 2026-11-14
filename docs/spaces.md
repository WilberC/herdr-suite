# Spaces

Back to the [README](../README.md). Sessions and the switcher are covered in
[Sessions](sessions.md); the reporter that brings gentle-shell sessions back is
[GS session restore](gs-restore.md).

```bash
hs space save work              # snapshot the active workspace as "work"
hs space save work --force      # replace a stored layout of the same name
hs space save --current         # name the layout after the active workspace
hs space save work --session api  # snapshot another session's active workspace
hs space save work --json       # print the snapshot document instead of the report
hs space load work              # recreate "work" in the active workspace
hs space load work --dry-run    # print the replay plan and change nothing
hs space load work --replace    # close the workspace's other panes and tabs first
hs space load work --new-workspace  # build it in a new workspace
hs space load work --session api    # replay into another session
hs space load work --verbose    # one [k/n] stderr line per replay step
hs space load --select          # pick a stored layout and watch it restore, in a new workspace
hs space load ./hand/made.json  # load a layout file by path
hs space list                   # NAME, SCOPE, WORKSPACE, TABS, PANES, MODIFIED
hs space list --json            # {"layouts":[{"name","scope","workspace",...}]}
hs space list --pane            # keep the table on screen until enter
hs space list --dir ~/project   # discover from that directory instead
hs space show work              # print a stored layout: header, tree, labels, cwds, agents
hs space show work --json       # print the stored document instead
hs space show ./hand/made.json  # resolve a layout file by path
```

## Saving a space

`hs space save <name>` reads the session this process already talks to — the
herdr session containing the pane `hs` runs in — and stores its active
workspace: every tab, and each tab's split tree with split directions, ratios,
pane labels, pane cwds and agent identifications (kind, name when herdr reports
one, and its reported state). `--session <name>` reads another running session
through that session's socket instead.

herdr reports no command for a plain shell pane, so no command is recorded
rather than a guessed one. Restoring such a pane opens its shell in its cwd.

A saved layout prints one stable report: where it was stored, the workspace
with its tab and pane counts, and one ASCII tree per tab:

```text
saved layout 'work' at ~/.config/herdr/herdr-suite/layouts/work.json
workspace 'dotfiles' · 1 tab · 1 pane
tab 1 '1' · 1 pane · focus w35:p7
pane w35:p7 · pi working · /home/wilber/dotfiles
```

`--json` prints the snapshot document instead, so scripts and the load command
share one machine-readable shape.

## Showing a space

`hs space show <name>` prints a stored layout without touching a session. The
header names the layout, the scope that resolved it, the file it lives in and the
schema version it was read at — plus the timestamp the document records, when it
carries one. Then every workspace follows, tab by tab, with the same split-tree
lines `hs space save` prints:

```text
layout 'work' · global · ~/.config/herdr/herdr-suite/layouts/work.json
schema_version 1
workspace 'dotfiles' · 1 tab · 2 panes
tab 1 '1' · 2 panes · focus w35:p7
split right 0.60
├─ pane w35:p7 · 'editor' · pi working · /home/wilber/dotfiles
└─ pane w35:p8 · /tmp
```

Resolution is the rule the whole CLI shares — explicit path, then the project
declaration, then the global store — and the refusals are a load's refusals: an
unknown layout, a name two places declare, and a layout a newer `hs` wrote print
the same message and exit 2, so checking a layout before replaying it asks the
question a replay would ask. `--json` prints the stored document instead, and
`--dir <path>` discovers from that directory.

The scope says which rule won: `global` for the store, `project` for a `.herdr/`
declaration, and `path` for a file named by path. A layout written before the
schema field existed reads at `schema_version 1` and prints it.

## Restoring a space

`hs space load <name>` reads the stored layout and recreates it in the session
`hs` runs against, or in the one `--session` names. The split tree replays from
its root, parents before children: herdr makes a split by dividing one pane, so
each replayed split consumes the pane that owns the area it divides and creates
the pane of its second child, before the splits inside that child follow. Each
stored leaf gets exactly one pane, in its stored cwd, with its stored label and,
when one was recorded, its agent started through `herdr agent start`.

herdr reports no command for a plain shell pane, so no command is restored:
those panes open a shell in their stored cwd and nothing else. Agent names must
be lowercase, start with a letter and stay unique among live agents, so a stored
name is folded into that shape, a leaf without one falls back to its agent kind,
and a collision gains a numeric suffix.

The destination workspace decides what the replay starts from. A workspace
holding a single idle pane is the empty case: that pane becomes the first pane of
the layout. Anything else is refused, because the replay would lay its tree over
existing work:

```text
hs: workspace 'dotfiles' already has 3 panes; pass --replace to close them before loading
```

`--replace` closes the workspace's other panes and tabs first, and says on stderr
what it is about to close, naming the ids it read:

```text
replacing workspace 'dotfiles': closing 2 panes (w1:p2, w1:p3) and 1 tab (w1:t2)
```

The pane `hs` itself runs in is never closed. `--new-workspace` builds the layout
in a new workspace named after the stored one instead of touching this one, and
`--new-workspace` is the only way a load creates a workspace.

When the stored workspace is not the active one and the session has several, the
load asks which workspace to use. It refuses to guess without a terminal, so
scripts either run it where the stored workspace is active or pass
`--new-workspace`.

`--select` picks the layout from the same list `hs space list` prints and
replays it in a new workspace. Enter is no longer the end of the picker: it
becomes a restoring view that names the layout and the step that is starting,
then a one-line done state before the command prints the summary a named load
prints.

```text
⠹ restoring 'work' — step 3/8 split right 0.60 in p-1 · new pane p-3 · cwd /tmp
```

A replay that fails shows the step that stopped it and waits for a key; the
command then prints the failure and exits `1`, exactly as a named load does.
Ctrl-C while restoring stops watching and leaves the picker. `--select
--dry-run` only chooses: it prints the plan and changes nothing.

The picker and the workspace prompt below it share the switcher's footer: the
bindings in one bar under the selected item's facts, and `tab` pages that bar
when the window is too narrow for every binding. The bar ends with
`tab more 1/2` while there is more to see, and `tab` wraps back to the first
page. Both prompts keep a compact frame on purpose -- 60 to 120 columns and 16
to 24 rows -- because a store of layouts is a short fixed list; past 24 rows the
body windows on the highlight instead of growing into an empty band.

## Mouse

Both prompts take the mouse the same way the [switcher does](sessions.md#mouse):
a click selects the row it landed on, a double click runs the row's default
action — load the layout, pick the workspace — and the wheel moves the highlight
inside the body. A single click only selects: loading a layout stays a deliberate
act, and the paging chip answers like `tab`. The restoring, done and failed views
take no mouse action, because their only answer is any key. Neither prompt
implements text selection, so use the terminal's own.

`--dry-run` prints the replay plan and changes nothing: the ordered list of
closes, creations, splits, renames, agent starts and focus steps, with the stored
pane ids, the directions and the ratios the replay would use.

```text
plan for layout 'dotfiles' into workspace 'dotfiles' · 1 tab · 1 pane
1. split right 0.60 in w35:p7 · new pane w35:p8 · cwd /home/wilber/dotfiles
2. start agent pi in w35:p8 as pi
3. focus tab 1 '1'
4. focus agent pi
```

herdr has no transaction. A replay that fails halfway keeps the steps it already
made, so the failure names the step and says the workspace may hold a partial
layout:

```text
hs: step 3 of 5 failed: create tab 2 'side' · cwd /tmp: herdr tab create: exit status 1: pane limit reached
hs: herdr has no transaction, so the workspace may hold a partial layout. Remove the panes or tabs it created and load again.
```

Focus is restored last: the destination workspace when it was not the active one,
then the stored focused tab, then the agent of the stored focused pane when it has
one. herdr's CLI cannot focus a pane by id, so the replayed pane receives focus by
being the only one created with `--focus`; a stored focused pane that no split
creates keeps its tab's focus.

## Long operations

Progress is a stderr contract, so stdout stays exactly what a script reads:

| Operation | Silent | Speaks |
| --- | --- | --- |
| `hs space load <name>` | the replay finishes under two seconds | over the budget, one `[k/n] <action>` line per step, the steps that already ran included; `--verbose` prints them even when the replay is fast |
| `hs space load --select` | never: the picker owns the terminal | the restoring view names the layout and the step that is starting, and Ctrl-C stops watching |
| the switcher preview | the session answers at once | the preview header shows `⟳ loading` until the workspaces arrive |
| `hs gs-restore install` | the install finishes under two seconds | the `installed:` line gains `(took 2.4s)` |
| `hs gs-restore startup` | nothing changed, or the install was fast | the changed line gains the same suffix |

The budget is two seconds. Under it nothing changes, which keeps fast runs and
scripts byte-for-byte what they were; over it the operation says what it is doing
instead of looking hung, and a failure still prints the step that stopped it:

```text
[5/8] create tab 2 'side' · cwd /tmp
hs: step 5 of 8 failed: create tab 2 'side' · cwd /tmp: herdr tab create: exit status 1: pane limit reached
hs: herdr has no transaction, so the workspace may hold a partial layout. ...
```

## Project layouts

`hs space list` and `hs space load` look for layouts in two places: the global
store, and the nearest project that declares its own layouts under `.herdr/`.
The project is found from the working directory upward, like a git repository
root, and the nearest declaration wins.

A project declares layouts in either form, or both:

```text
.herdr/layouts/api.json   # the file name is the layout name
.herdr/layouts.toml       # an index of names and files
```

```toml
# .herdr/layouts.toml
[layouts]
api = "api.json"
web = "layouts/web.json"
```

The index is a narrow TOML subset on purpose: one `[layouts]` table, one
`name = "path"` entry per layout with the path relative to `.herdr/`, and `#`
comments. Anything else is refused with the line number instead of guessed at,
which is why the package needs no TOML dependency.

Resolution is one rule for the whole CLI, and `internal/layout/doc.go` owns it:

1. an explicit path (`hs space load ./api.json`),
2. the project declaration, by name,
3. the global store.

A name the project does not declare falls through to the global store. A name the
project declares twice — in its `layouts/` directory and again in its index — is
refused with every candidate listed, because picking one silently would load a
layout the caller did not name:

```text
hs: layout 'api' is declared in 2 places: project /repo/.herdr/layouts/api.json, project /repo/.herdr/other/api.json; remove one or load the file by path
```

`hs space list` prints the scope of every row, so two layouts with the same name
are visible before a load resolves one:

```text
NAME  SCOPE    WORKSPACE  TABS  PANES  MODIFIED
api   project  api        1     1      2026-01-02 03:04
work  global   project    2     4      2026-01-02 03:04
```

The workspace column prints the first stored workspace, plus how many further
workspaces a forked layout holds (`project +1`). The modified time is UTC, so the
table is stable across time zones.

Saving always writes the global store; a project's `.herdr/` layouts are
committed by hand.

Inside herdr, runtime commands run with the plugin directory as their working
directory, so project discovery there needs `--dir <path>`. See [Inside
herdr](inside-herdr.md).

## Multi-workspace layouts

`hs session fork` writes one document holding every workspace of a session, and
`hs space load <name>` loads it like any other layout. The stored document gains
a session form: `workspaces` instead of the single `workspace`, so a layout saved
before forks existed still reads and re-encodes unchanged. A load creates one
workspace per stored entry, replays every one of them, and restores each
workspace's focus last, which leaves the last stored workspace focused:

```text
loaded layout 'work' into 2 workspaces · 3 tabs · 6 panes · 18 steps
workspace 'project' · 2 tabs · 4 panes · 11 steps
workspace 'api' · 1 tab · 2 panes · 7 steps
```

`--replace` and `--new-workspace` describe a single destination workspace, so a
forked layout refuses them rather than ignoring them. The fork command itself is
in [Sessions § Forking a session](sessions.md#forking-a-session).

## Layout storage

Layouts live in one global directory, `$XDG_CONFIG_HOME/herdr/herdr-suite/
layouts` (or `~/.config/herdr/herdr-suite/layouts` when `XDG_CONFIG_HOME` is
unset). The file name is the layout name plus `.json`, and names follow the
session rules: whitespace runs become one `-`, and only letters, digits, `.`,
`_` and `-` are kept. Saving refuses to replace a stored layout unless `--force`
is passed.

Every saved layout names its schema at the top level, `"schema_version": 1`. A
file written before the field existed (v0.1.0 wrote single-workspace files
without it) reads as version 1, so a stored layout keeps working across an
upgrade; a version above 1 was written by a newer `hs` and is refused with the
fix rather than read, because this build cannot know what the extra fields mean.
Reading never rewrites a file, so loading an older layout leaves it byte-for-byte
as it was.

Layouts are JSON, not TOML: Go's standard library ships a JSON encoder and
decoder, while TOML would need a third-party dependency, and the schema is a
nested split tree, which JSON represents directly. One format also means the
stored file, the `--json` output and the in-memory snapshot cannot disagree.
The schema is documented in `internal/layout/doc.go`.
