# herdr-suite

Session and space management for [herdr](https://herdr.dev): switch named
sessions, and snapshot a workspace layout so it can be recreated later.

The same Go binary serves both surfaces: a non-interactive CLI for scripts and
herdr plugin actions that run inside herdr. Destructive operations require
confirmation, and non-interactive commands keep stable exit codes and stdout.

## Status

Early scaffold. The repository currently ships:

- `hsd`, a CLI skeleton whose `session`, `space` and `layout` subcommands
  report `not implemented` and exit `2`.
- `internal/herdrmock`, an offline fake of the herdr CLI surface used by tests.
- `internal/session` and `internal/layout`, packages reserved for the session
  switcher and layout storage.

The session switcher lands next as the `hs` entry point, replacing
`herdr-sessionizer`. Layout save/restore and the plugin actions follow.

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
hsd            # print usage; exits 2 when invoked without a command
hsd --help     # print usage on stdout
hsd session    # scaffold: prints its usage and exits 2
hsd space      # scaffold: prints its usage and exits 2
hsd layout     # scaffold: prints its usage and exits 2
```

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

Tests never talk to a real herdr socket. `internal/herdrmock` provides a fake
`herdrmock.Client` that records calls and returns scripted state and errors, so
the suite is deterministic and runs offline.

## Repository layout

| Path | Holds |
| --- | --- |
| `cmd/hsd/` | the CLI entry point and its subcommand table |
| `internal/herdrmock/` | the herdr client interface and the offline fake |
| `internal/session/` | session management (switcher, lifecycle) |
| `internal/layout/` | layout discovery, storage, snapshot and restore |
| `herdr-plugin.toml` | the herdr plugin manifest |
| `odd/tasks/herdr-suite.md` | the working spec and task log |
