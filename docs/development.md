# Development

Back to the [README](../README.md). What the binary does is in
[Sessions](sessions.md) and [Spaces](spaces.md).

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
`sha256sum`, and creates the GitHub release with `gh`. Each binary is built with
`-ldflags "-X main.version=<tag>"`, so it self-reports its release; a local build
keeps `dev`, which is why `hs version --check` can tell a stale binary from the
manifest it sits beside. Both workflows read the toolchain from
`go-version-file: go.mod`, so the version lives in one place, and neither needs a
release tool: the pipeline is `go`, `sha256sum` and `gh` only.

A `smoke` job runs after `publish` and checks the release rather than the working
tree: it downloads `hs-linux-amd64` and `SHA256SUMS` back from the tag it just
created, verifies the binary against the checksum, and runs `./hs-linux-amd64
version --check` and `./hs-linux-amd64 --help`. A published asset that is broken,
or stamped with a version the manifest does not declare, fails the run instead of
an install.

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
| `internal/completions/` | the generated fish completion script and its install target |
| `internal/gsrestore/` | the GS session-restore reporter install, config gate and status |
| `internal/manifest/` | the plugin manifest fields the binary reports: the version and the herdr floor |
| `internal/doctor/` | the read-only installation report behind `hs doctor` |
| `extensions/` | `herdr-agent-state.ts`, the reporter this plugin ships |
| `herdr-plugin.toml` | the herdr plugin manifest |
| `.github/workflows/` | `ci.yml`, the checks every push runs, and `release.yml`, the tagged release |
| `odd/tasks/herdr-suite.md` | the working spec and task log |
