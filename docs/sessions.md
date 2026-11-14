# Sessions

Back to the [README](../README.md). The layouts these commands write are covered
in [Spaces](spaces.md), and the exit codes in [Health](health.md).

```bash
hs                              # interactive switcher; needs a terminal
hs attach work                  # replace this shell with the attached client
hs session list                 # stable table: NAME and STATE columns
hs session list --json          # {"sessions":[{"name","running","default"}]}
hs session create "my project"  # creates my-project by attaching it
hs session stop work --yes      # stop work
hs session restart work --yes   # stop it so the next attach starts fresh
hs session delete scratch --yes # stop it first, then delete it
hs session fork                 # snapshot the current session under its name
hs session fork work backup     # snapshot session work as the layout backup
```

Session names are sanitized before they reach herdr: whitespace runs become
one `-`, and anything left outside `A-Z a-z 0-9 . _ -` is refused. Herdr
creates a session on attach, so `create` and `attach` are the same call.

`hs session list` prints running sessions first, then each group by name, so
the output is deterministic. `--json` mirrors the field names of
`herdr session list --json`.

Destructive commands ask for a confirmation when stdin is a terminal, and
require `--yes` otherwise. Deleting stops the session first, because herdr only
deletes stopped sessions, and the default session cannot be deleted at all.

## Switcher

`hs` with no command opens the Bubbletea switcher. Typing filters session
names only, never the state column:

| Key | Action |
| --- | --- |
| `enter` | attach the highlighted session; when nothing matches, create the typed name |
| `alt-enter` | create a session with the typed name, even when a match exists |
| `ctrl-r` | restart the highlighted session after confirmation: stop it, then attach when the switcher closes |
| `ctrl-s` | stop the highlighted session after confirmation |
| `ctrl-x` | stop and delete the highlighted session after confirmation |
| `tab` | page the hint bar when the window is too narrow for every binding |
| `esc` | quit |

The bindings sit in one bar under the detail line of the footer. When the window
is too narrow to show them all, the bar shows the first page and ends with
`tab more 1/2`: `tab` moves to the next page and wraps back to the first.
Nothing is ever hidden, and `tab` never reaches the filter or answers a
confirmation.

Herdr forbids nested launches, so `hs` and `hs attach` refuse to run inside a
herdr pane (`HERDR_ENV`), and attaching replaces the process with
`herdr --session <name>`. Before the replacement it prints an OSC 2 window
title (`herdr:<name>`), which names the session for the life of the terminal
window. Restarting is a stop plus an attach: the stop happens in the switcher,
the name is left in a temp file, and the attach happens once the switcher
exits.

The switcher shows the highlighted session's workspaces beside the list —
labels, tab and pane counts, and herdr's agent state — read through the same
access layer that space snapshots use. The read is asynchronous, so the preview
header shows `⟳ loading` while it is outstanding and a slow session reads as work
in progress instead of an empty list. A session that cannot be read shows
`unavailable`, and a stopped one shows that it is stopped without a read. See
[Spaces § Long operations](spaces.md#long-operations) for the progress contract
behind that header.

The switcher and the space prompts draw their frame at the size of the pane with
the footer pinned to the bottom of the frame. The bounds differ because the
content does: the switcher clamps to 60..160 columns and 16..40 rows, since it
nests the highlighted session's workspaces and grows with them, while the space
prompts clamp to 60..120 columns and 16..24 rows, because a layout store or a
session's workspaces are a short fixed set and a taller frame would be mostly
empty under the last row. A pane larger than the bounds shows the frame centred,
and the two herdr popups in `herdr-plugin.toml` are fixed at 120x24 cells so the
prompts fill them instead of sitting in a band of unused pane.

The herdr binary is found through `HERDR_BIN_PATH`, then `PATH`.

## Forking a session

`hs session fork [session] [layout-name]` snapshots every workspace of a session
into one layout, instead of the single active workspace `hs space save` stores.
Without a session it forks the current one — resolved by matching the socket
herdr exports against the sessions herdr lists — and the layout name defaults to
the session name.

```text
saved layout 'work' at ~/.config/herdr/herdr-suite/layouts/work.json
session 'work' · 2 workspaces · 3 tabs · 6 panes
workspace 'project' · 2 tabs · 4 panes
tab 1 'main' · 3 panes · focus w1:p1
...
workspace 'api' · 1 tab · 2 panes
...
```

The layout it writes stores every workspace in one document, and loading it
replays them all. [Spaces § Multi-workspace
layouts](spaces.md#multi-workspace-layouts) covers the document form, the replay,
and why `--replace` and `--new-workspace` are refused there.
