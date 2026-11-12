# herdr-suite

Session and space management for [herdr](https://herdr.dev): switch named
sessions, and snapshot a workspace layout so it can be recreated later.

The same Go binary serves both surfaces: a non-interactive CLI for scripts and
herdr plugin actions that run inside herdr. Destructive operations require
confirmation, and non-interactive commands keep stable exit codes and stdout.

## Status

`hs` replaces `herdr-sessionizer`: the interactive switcher, the session
lifecycle commands, and saving and restoring spaces are implemented.

- `hs` and `hs session ...` manage sessions.
- `hs space save` snapshots the active workspace as a stored layout.
- `hs space load` recreates a stored layout in a workspace.
- `hs layout` still reports `not implemented` and exits `2`.
- `internal/herdrmock` is the offline fake of the herdr surface used by tests.

Layout discovery, `session fork` and the plugin actions follow.

## Install

Not published yet. The plugin requires herdr 0.7.5 or newer and will be
installed like other herdr plugins:

```bash
herdr plugin install WilberC/herdr-suite
```

Distribution follows the herdr-bar pattern: release CI publishes `linux` and
`macOS` binaries for `amd64` and `arm64` with a `SHA256SUMS` file, and the
plugin build step downloads the verified binary, falling back to a local Go
build when no release artifact matches.

## Usage

```bash
hs                              # interactive switcher; needs a terminal
hs attach work                  # replace this shell with the attached client
hs session list                 # stable table: NAME and STATE columns
hs session list --json          # {"sessions":[{"name","running","default"}]}
hs session create "my project"  # creates my-project by attaching it
hs session stop work --yes      # stop work
hs session restart work --yes   # stop it so the next attach starts fresh
hs session delete scratch --yes # stop it first, then delete it
hs space save work              # snapshot the active workspace as "work"
hs space save work --force      # replace a stored layout of the same name
hs space save work --session api  # snapshot another session's active workspace
hs space save work --json       # print the snapshot document instead of the report
hs space load work              # recreate "work" in the active workspace
hs space load work --dry-run    # print the replay plan and change nothing
hs space load work --replace    # close the workspace's other panes and tabs first
hs space load work --new-workspace  # build it in a new workspace
hs space load work --session api    # replay into another session
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

### Spaces

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

### Restoring a space

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

### Layout storage

Layouts live in one global directory, `$XDG_CONFIG_HOME/herdr/herdr-suite/
layouts` (or `~/.config/herdr/herdr-suite/layouts` when `XDG_CONFIG_HOME` is
unset). The file name is the layout name plus `.json`, and names follow the
session rules: whitespace runs become one `-`, and only letters, digits, `.`,
`_` and `-` are kept. Saving refuses to replace a stored layout unless `--force`
is passed.

Layouts are JSON, not TOML: Go's standard library ships a JSON encoder and
decoder, while TOML would need a third-party dependency, and the schema is a
nested split tree, which JSON represents directly. One format also means the
stored file, the `--json` output and the in-memory snapshot cannot disagree.
The schema is documented in `internal/layout/doc.go`.

### Switcher

`hs` with no command opens the Bubbletea switcher. Typing filters session
names only, never the state column:

| Key | Action |
| --- | --- |
| `enter` | attach the highlighted session; when nothing matches, create the typed name |
| `alt-enter` | create a session with the typed name, even when a match exists |
| `ctrl-r` | restart the highlighted session after confirmation: stop it, then attach when the switcher closes |
| `ctrl-s` | stop the highlighted session after confirmation |
| `ctrl-x` | stop and delete the highlighted session after confirmation |
| `esc` | quit |

Herdr forbids nested launches, so `hs` and `hs attach` refuse to run inside a
herdr pane (`HERDR_ENV`), and attaching replaces the process with
`herdr --session <name>`. Before the replacement it prints an OSC 2 window
title (`herdr:<name>`), which names the session for the life of the terminal
window. Restarting is a stop plus an attach: the stop happens in the switcher,
the name is left in a temp file, and the attach happens once the switcher
exits.

The switcher shows the highlighted session's workspaces beside the list —
labels, tab and pane counts, and herdr's agent state — read through the same
access layer that space snapshots use. A session that cannot be read shows
`unavailable`, and a stopped one shows that it is stopped without a read.

The herdr binary is found through `HERDR_BIN_PATH`, then `PATH`.

### Exit codes

| Code | Meaning |
| --- | --- |
| `0` | the operation succeeded, or the user declined a confirmation |
| `1` | herdr failed |
| `2` | bad arguments, an invalid name, a protected session, or a missing confirmation |

Inside herdr, the plugin exposes the same operations as actions; the manifest
declares the action list as it is filled in.

## Development

Requirements: Go 1.27 or newer. The toolchain is not pinned to a vendor; use
whatever `go` your environment provides.

```bash
make build   # go build ./...
make test    # go test ./...
make vet     # go vet ./...
make fmt     # gofmt -w .
```

Tests never talk to a real herdr socket or binary. `internal/herdrmock`
provides a fake `herdrmock.Client` that records calls and returns scripted
state and errors, and the `internal/herdrcli` tests point `HERDR_BIN_PATH` at a
stub script, so the suite is deterministic and runs offline.

## Repository layout

| Path | Holds |
| --- | --- |
| `cmd/hs/` | the `hs` entry point and its subcommand table |
| `internal/session/` | session lifecycle, the switcher, and the table/JSON output |
| `internal/herdrcli/` | the herdr client: the CLI calls, the topology snapshot, the layout mutations and the process replacement |
| `internal/herdrmock/` | the herdr client interface and the offline fake |
| `internal/layout/` | the layout schema, its storage directory, the snapshot report and the replay plan |
| `herdr-plugin.toml` | the herdr plugin manifest |
| `odd/tasks/herdr-suite.md` | the working spec and task log |
