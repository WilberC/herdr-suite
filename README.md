# herdr-suite

Session and space management for [herdr](https://herdr.dev): switch named
sessions, and snapshot a workspace layout so it can be recreated later.

<!-- Badges go live with the first v* tag; uncomment them then.
[![CI](https://github.com/WilberC/herdr-suite/actions/workflows/ci.yml/badge.svg)](https://github.com/WilberC/herdr-suite/actions/workflows/ci.yml)
[![release](https://github.com/WilberC/herdr-suite/actions/workflows/release.yml/badge.svg)](https://github.com/WilberC/herdr-suite/actions/workflows/release.yml)
-->

The same Go binary serves both surfaces: a non-interactive CLI for scripts and
herdr plugin actions that run inside herdr. Destructive operations require
confirmation, and non-interactive commands keep stable exit codes and stdout.

## Status

`hs` replaces `herdr-sessionizer`: the interactive switcher, the session
lifecycle commands, saving and restoring spaces, project layout discovery,
session forks and the plugin actions are implemented.

- `hs` and `hs session ...` manage sessions, and `hs session fork` snapshots a
  whole session as one layout.
- `hs space save` snapshots the active workspace as a stored layout.
- `hs space load` recreates a stored layout, in a workspace or as every
  workspace a fork captured.
- `hs space list` lists the layouts visible from the current directory.
- `herdr-plugin.toml` declares the `save`, `load` and `list` actions and the two
  panes they open.
- `internal/herdrmock` is the offline fake of the herdr surface used by tests.
- `.github/workflows/release.yml` publishes the release binaries and their
  checksums on a `v*` tag, and `.github/workflows/ci.yml` gates every push.

No release is tagged yet, and herdr runs no build step for this manifest, so
`herdr-plugin.toml` names a `bin/hs` that only a release download or a local
build puts there. See [Install](#install) and [Inside herdr](#inside-herdr).

## Install

The plugin requires herdr 0.7.5 or newer (`herdr api snapshot`, which every
space command reads through, first shipped in 0.7.2) and installs like any other
herdr plugin:

```bash
herdr plugin install WilberC/herdr-suite
```

Each `v*` tag publishes four static binaries — `hs-linux-amd64`,
`hs-linux-arm64`, `hs-darwin-amd64` and `hs-darwin-arm64` — next to a
`SHA256SUMS` file on [the releases
page](https://github.com/WilberC/herdr-suite/releases). `herdr plugin install`
leaves a checkout whose path `herdr plugin list --plugin herdr-suite --json`
reports as `plugin_root`; run this inside it, picking the asset for this machine:

```bash
ASSET=hs-linux-amd64   # or hs-darwin-arm64, hs-darwin-amd64, hs-linux-arm64
curl -fsSLO "https://github.com/WilberC/herdr-suite/releases/latest/download/$ASSET"
curl -fsSLO https://github.com/WilberC/herdr-suite/releases/latest/download/SHA256SUMS
grep -F "$ASSET" SHA256SUMS | sha256sum -c -   # macOS: | shasum -a 256 -c -
mkdir -p bin && install -m 755 "$ASSET" bin/hs
```

`SHA256SUMS` covers every asset, so the check reads only the line of the binary
this machine downloaded. Nothing compiles and nothing is downloaded
automatically yet: herdr's install runs no build step for this manifest, so the
`bin/hs` the plugin actions run is the one placed above, or `make hs` in a
working tree — see [Inside herdr](#inside-herdr).

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
hs session fork                 # snapshot the current session under its name
hs session fork work backup     # snapshot session work as the layout backup
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
hs space load --select          # pick a stored layout in a new workspace
hs space load ./hand/made.json  # load a layout file by path
hs space list                   # NAME, SCOPE, WORKSPACE, TABS, PANES, MODIFIED
hs space list --json            # {"layouts":[{"name","scope","workspace",...}]}
hs space list --pane            # keep the table on screen until enter
hs space list --dir ~/project   # discover from that directory instead
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

### Project layouts

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

### Forking a session

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

The stored document gains a session form: `workspaces` instead of the single
`workspace`, so a layout saved before forks existed still reads and re-encodes
unchanged. `hs space load <name>` of a forked layout creates one workspace per
stored entry, replays every one of them, and restores each workspace's focus
last, which leaves the last stored workspace focused:

```text
loaded layout 'work' into 2 workspaces · 3 tabs · 6 panes · 18 steps
workspace 'project' · 2 tabs · 4 panes · 11 steps
workspace 'api' · 1 tab · 2 panes · 7 steps
```

`--replace` and `--new-workspace` describe a single destination workspace, so a
forked layout refuses them rather than ignoring them.

### Inside herdr

`herdr-plugin.toml` declares three actions and the two panes they open, anchored
to the `hs` binary in the plugin's own `bin/` directory — the layout herdr-bar
uses for its release binary:

| Action | Runs |
| --- | --- |
| `save` | `bin/hs space save --current`, naming the layout after the workspace it runs in |
| `load` | the `load` pane: `bin/hs space load --select`, picking a stored layout and rebuilding it in a new workspace |
| `list` | the `layouts` pane: `bin/hs space list --pane`, keeping the table until enter is pressed |

Bind them like any plugin action:

```toml
[[keys.command]]
key = "prefix+l"
type = "plugin_action"
command = "herdr-suite.list"
```

Action output lands in `herdr plugin log herdr-suite`.

Two facts are documented rather than worked around. On `herdr plugin
install` (and on update) the `[[build]]` hook runs `bin/ensure-binary.sh
--in-tree`, which builds `bin/hs` with a local Go toolchain and falls back to
the prebuilt `hs-<os>-<arch>` release asset once tag v0.1.0 publishes; until
then a machine without Go builds nothing, and `herdr plugin link` runs no
build at all, so a working tree uses `make hs` — every action and pane above
expects exactly that path.
And inside herdr, runtime commands run with the plugin directory as their working
directory, so project discovery there needs `--dir <path>`; herdr reports no
project directory to a plugin command, so guessing one would be wrong.

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
| `2` | bad arguments, an invalid name, a protected session, a missing confirmation, a missing layout, or a layout name more than one place declares |

Inside herdr, the plugin exposes the same operations as the `save`, `load` and
`list` actions.

## Development

Requirements: Go 1.27 or newer. The toolchain is not pinned to a vendor; use
whatever `go` your environment provides.

```bash
make build   # go build ./...
make hs      # go build -o bin/hs ./cmd/hs, the binary the plugin runs
make test    # go test ./...
make vet     # go vet ./...
make fmt     # gofmt -w .
```

`.github/workflows/ci.yml` runs the same checks on `ubuntu-latest` and
`macos-latest` for every push and pull request: build, `go vet`, a `gofmt -l cmd
internal` that fails the job on any unformatted file, and `go test -count=1
./...`. Locally:

```bash
go build ./... && go vet ./... && test -z "$(gofmt -l cmd internal)" && go test -count=1 ./...
```

`.github/workflows/release.yml` repeats the checks once more on a `v*` tag, then
cross-compiles the four targets with `CGO_ENABLED=0`, writes `SHA256SUMS` with
`sha256sum`, and creates the GitHub release with `gh`. Both workflows read the
toolchain from `go-version-file: go.mod`, so the version lives in one place, and
neither needs a release tool: the pipeline is `go`, `sha256sum` and `gh` only.

The matrix stops at linux and darwin, the two platforms `herdr-plugin.toml`
declares. A Windows binary is deliberately not shipped: `syscall.Exec` returns
`EWINDOWS` on Windows, so `hs attach` and the switcher could not replace the
process there, and the code does compile for `windows/amd64`. WSL runs the linux
asset, so a windows target would only add an asset nothing should run.

Cutting a release is `git tag v0.1.0 && git push origin v0.1.0`, and the tag is
the owner's decision, like a merge or a push. `cmd/hs/release_workflow_test.go`
reads both workflows: it fails when the release matrix and the platforms in
`herdr-plugin.toml` disagree, so a target cannot ship from one without the other.

Tests never talk to a real herdr socket or binary. `internal/herdrmock`
provides a fake `herdrmock.Client` that records calls and returns scripted
state and errors, and the `internal/herdrcli` tests point `HERDR_BIN_PATH` at a
stub script, so the suite is deterministic and runs offline. The plugin manifest
test reads `herdr-plugin.toml` with python's `tomllib` (3.11+) and skips when no
such interpreter is installed.

## Repository layout

| Path | Holds |
| --- | --- |
| `cmd/hs/` | the `hs` entry point and its subcommand table |
| `internal/session/` | session lifecycle, the switcher, and the table/JSON output |
| `internal/herdrcli/` | the herdr client: the CLI calls, the topology snapshot, the layout mutations and the process replacement |
| `internal/herdrmock/` | the herdr client interface and the offline fake |
| `internal/layout/` | the layout schema, its storage directory, the snapshot report and the replay plan |
| `herdr-plugin.toml` | the herdr plugin manifest |
| `.github/workflows/` | `ci.yml`, the checks every push runs, and `release.yml`, the tagged release |
| `odd/tasks/herdr-suite.md` | the working spec and task log |
