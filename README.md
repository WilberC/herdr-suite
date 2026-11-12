# herdr-suite

Session and space management for [herdr](https://herdr.dev): switch named
sessions, and snapshot a workspace layout so it can be recreated later.

The same Go binary serves both surfaces: a non-interactive CLI for scripts and
herdr plugin actions that run inside herdr. Destructive operations require
confirmation, and non-interactive commands keep stable exit codes and stdout.

## Status

`hs` replaces `herdr-sessionizer`: the interactive switcher and the session
lifecycle commands are implemented. What still renders a scaffold is layout
work:

- `hs` and `hs session ...` manage sessions.
- `hs space` and `hs layout` report `not implemented` and exit `2`.
- `internal/herdrmock` is the offline fake of the herdr surface used by tests.

Layout save/restore, `session fork` and the plugin actions follow.

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
| `internal/herdrcli/` | the herdr client: the CLI calls and the process replacement |
| `internal/herdrmock/` | the herdr client interface and the offline fake |
| `internal/layout/` | layout discovery, storage, snapshot and restore |
| `herdr-plugin.toml` | the herdr plugin manifest |
| `odd/tasks/herdr-suite.md` | the working spec and task log |
