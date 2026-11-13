# Changelog

All notable changes to herdr-suite are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the versions
follow [Semantic Versioning](https://semver.org/).

## [0.2.0] - 2026-11-12

### Added

- `hs version` with the release version embedded at build time; `--check`
  compares the binary against the manifest so a stale install is caught.
- `hs doctor`: eleven read-only checks over the herdr binary, socket, plugin
  config, binary/manifest skew, layout store, project discovery and the
  GS-restore feature, with a fix clause per failure and `--json` output.
- Layout files carry `schema_version`: older v0.1.0 layouts load unchanged,
  newer schemas refuse with a typed error instead of guessing.
- `hs completions fish [--install]`: fish completions generated from the CLI
  command table, with dynamic session and layout names.
- `hs space show <name>`: inspect a stored layout without touching a session.
- Loaders for every slow operation: the `--select` restore runs in-program with
  a full-screen per-step spinner, replays print `[k/n]` progress to stderr past
  a two-second budget (`--verbose` forces it immediately), the switcher preview
  shows a loading glyph, and slow installs report their elapsed time.
- GS session restore (default off): the plugin can restore gentle-shell sessions
  through its own launcher. Ships `hs gs-restore install|remove|status`, a
  `[[startup]]` gate behind two config toggles, and a generalized reporter.
- The release workflow smoke-tests its own artifact: it downloads the published
  binary, verifies `SHA256SUMS` and runs it before the release is done.

### Changed

- The installed plugin provisions `bin/hs` through a `[[build]]` hook on install
  and update; the hook sanity-checks a download by asking it for its version.

## [0.1.0] - 2026-11-12

### Added

- Replaces `herdr-sessionizer`: the interactive session switcher and
  `hs session list|create|stop|restart|delete` with sanitization, confirmation
  gates and default-session protection.
- `hs space save|load|list`: workspace layout snapshots and restore, faithful
  to split directions, ratios, pane labels, working directories and agents,
  with `--dry-run` plans and step-numbered failure reports.
- Project layouts discovered from `.herdr/` plus the global store, and
  `hs session fork` for whole-session templates.
- Plugin actions (`save`, `load`, `list`) and popup panes declared in
  `herdr-plugin.toml`.
- Release pipeline: cross-built static binaries with `SHA256SUMS`, no third
  party release tooling.
