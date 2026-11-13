# Contributing

Read [`AGENTS.md`](AGENTS.md) first — it is the whole engineering contract:
the layout, the exit-code discipline, the offline-test rule and what each
document owns. [`docs/development.md`](docs/development.md) covers the actual
day-to-day: make targets, the workflows and the release flow.

The short version:

1. **Fork or branch; never commit to `main` directly.** Pushes, merges, tags
   and releases are the owner's decisions.
2. **Test-first, offline.** Every behavior lands with a failing test first and
   passes with the fake herdr surface — a test that talks to a socket will not
   be accepted.
3. **One reviewable unit per commit** (Conventional Commit), tests and docs
   beside the behavior. If your change touches code an existing command shares,
   pin the old command's behavior with a test in the same commit.
4. **Docs travel with behavior.** A change that contradicts a `docs/*.md`
   section updates that section in the same commit, and `README.md` stays
   under 160 lines — depth belongs in `docs/`.
5. **No user-specific paths, ever.** Detection is capability-based.

## Reporting bugs and asking features

Use the issue templates; `hs version`, `hs doctor` output and the platform
answer most questions before the first reply.
