# Repository instructions

herdr-suite: session switching and workspace layout snapshot/restore for
[herdr](https://herdr.dev) — one Go binary (`hs`) serving a CLI surface and a
herdr plugin manifest. Public product; users install it with
`herdr plugin install WilberC/herdr-suite`.

Short by design. Each rule names what enforces it. Detail lives in the file
that owns it; this file links, never restates.

## The layout

| Directory | Holds | Never holds |
| --- | --- | --- |
| `cmd/hs/` | the entry point: dispatch, exit-code contract, CLI texts | domain logic |
| `internal/<name>/` | every package the binary is made of | CLI surfaces |
| `internal/herdrmock/` | the offline fake of the herdr surface | production reads |
| `extensions/` | agent-extension files the plugin ships (the GS reporter) | Go code |
| `docs/` | every document, one topic per file (`README` only links) | everything long-form in README |
| `docs/media/` | README images | — |
| `.github/workflows/` | CI and the release pipeline | tests that live outside Go |
| `odd/tasks/` | feature specifications and the decision log | code |
| `herdr-plugin.toml` | the plugin manifest: actions, panes, build hook | logic |

Four sentences that make the rest follow.

1. **One binary (`hs`), one exit-code contract.** Every command runs through
   `run(args, stdout, stderr) int`. Exit 0 = success, 1 = operation failure,
   2 = refusal/usage. Refusals name the fix. Pinned by `cmd/hs/main_test.go`.
2. **Tests never touch a socket, a terminal or the network.** The herdr surface
   is an interface (`internal/herdrmock`); production code implements it in
   `internal/herdrcli`. Offline is the only test mode this repo knows.
3. **README is the landing, docs/ is the depth.** `README.md` ≤ 160 lines:
   what it solves, quick start, cheatsheet, links. Every topic lives in one
   `docs/*.md`, and every fact lives in exactly one place.
4. **Nothing user-specific enters the code.** No home paths, no user names, no
   `~/.gentle-shell` assumptions. Detection is capability-based (is the needed
   command resolvable? is the env var present?). Review rejects paths.

## What an agent must do before writing

1. **Read the spec first.** Feature work lives in `odd/tasks/<feature>.md`,
   in this order: header → `## Specs` (S#: user's words, verbatim) →
   `## Tasks` → `## Log` (L#: every user correction and decision, verbatim).
   Write nothing until you can name the S#s you are covering. Report them.
2. **Consult [docs/](docs/) before changing behavior** — the docs are the
   audited truth of shipped behavior; a change that contradicts one must
   update the doc in the same commit.
3. **Check the herdr floor.** A herdr CLI surface newer than
   `min_herdr_version` in `herdr-plugin.toml` must raise it, with the version
   that introduced the API named in the commit message.

## The rules, and what enforces each

| Rule | Enforced by |
| --- | --- |
| **Test-first**: observe RED, then GREEN, then refactor; assert exit codes, stdout AND stderr of every command | Review; `TestRunContract`-style tables pin the contract |
| **New command ⇒ new contract cases + docs + completions row**. The command table, usage text and fish completion read the same data — never fork them | Review; completions tests guard the drift both directions |
| **stdout is scriptable ground.** Progress goes to stderr; nothing outcome-relevant moves off stdout once shipped | Review; differential checks vs base before finishing |
| **Plugin manifest stays parseable** by herdr's own parser; parents validate via `herdr plugin link --disabled` + unlink and clean the entry afterward | Review; `plugin_manifest_test.go` pins structure |
| **Destructive actions confirm.** Interactive y/N; non-interactive requires `--yes` | Review; tests pin the refusal wording |
| **Layout format changes bump `schema_version`** — legacy files must still load, future files must refuse typewise | `internal/layout/schema_test.go` |
| **New dependency only with a stated reason** in the commit; stdlib and in-package implementations win ties (see the spinner decision) | Review |
| **No comments explaining what goes in `docs/`** — code states mechanics, docs state contracts and reasons, naming the test that pins them | Review |
| **Commits are Conventional, one reviewable unit, tests and docs beside the behavior** (`feat(...)`, `fix(...)`, `docs(...)`); the decision log gains an L# per worker report and per user correction | Review (the log is parent-owned: agents report, parent appends) |
| **A push, a tag and a release are the owner's decision.** Agents never commit unless the workflow authorizes the work-unit commit, never push tags | Review |

## Dev commands

```sh
mise exec go -- go build ./... && mise exec go -- go vet ./...
mise exec go -- gofmt -l cmd internal      # must print nothing
mise exec go -- go test -count=1 ./...
make hs                                    # build bin/hs (dev binary)
```

## Where the truth lives

| Question | Read |
| --- | --- |
| Install, binary download, build hook | [`docs/install.md`](docs/install.md) |
| Session switching and `hs session` | [`docs/sessions.md`](docs/sessions.md) |
| Layouts: save, load, show, list, forks, storage | [`docs/spaces.md`](docs/spaces.md) |
| The GS session-restore reporter | [`docs/gs-restore.md`](docs/gs-restore.md) |
| Plugin actions, panes, inside-herdr keys | [`docs/inside-herdr.md`](docs/inside-herdr.md) |
| `hs version`, `hs doctor`, exit codes | [`docs/health.md`](docs/health.md) |
| Tests, workflows, release flow, repo layout | [`docs/development.md`](docs/development.md) |
| What was decided and why, for every feature | `odd/tasks/herdr-suite.md` § Log |
| How to contribute and what is reviewed | [`CONTRIBUTING.md`](CONTRIBUTING.md) |
| What changed in each release | [`CHANGELOG.md`](CHANGELOG.md) |

## Adding a command

1. Name the spec (`odd/tasks/...`, S#) it belongs to — no spec, no command.
2. Write the contract test first: usage text, exit path, stdout/stderr.
3. Add the command row to **the shared table** in `cmd/hs/main.go` (the
   completions generator reads it — that is the point).
4. Update the topic doc in `docs/`, the README cheatsheet row (one line), and
   the exit-code table if the command introduces a new refusal class.

## Releasing

1. Bump `version` in `herdr-plugin.toml` (the manifest is the source of truth).
2. Tag `v<version>` — the release workflow verifies, cross-builds (the
   embedded version comes from the tag), publishes binaries + `SHA256SUMS`,
   and runs the smoke job that downloads its own artifact.
3. A tag without the manifest bump fails the smoke job on purpose.

## Compatibility posture

- Every user-facing addition stays **default-off** until the user asks for it
  (see the GS-restore toggles).
- Behavior that other tools consume (stdout shapes, JSON keys, exit codes,
  plugin action ids, config file names) is a contract: deprecate loudly, never
  break silently.
