# GS session restore

Back to the [README](../README.md). The layout commands it complements are in
[Spaces](spaces.md), and the progress budget that its install line obeys is in
[Spaces § Long operations](spaces.md#long-operations).

herdr restores a pane's agent by running the command the reporter reported, and
the vanilla pi integration reports pi. Inside gentle-shell that brings back
vanilla pi rather than gentle-shell, so the session loses its launcher. This
feature ships a reporter that reports `gentle-shell -- --session <path>`, keeps
re-reporting the session identity every 5 seconds for 2 minutes while herdr
anchors it, and keeps the pane `working` while live subagents are still running.

**Default off.** Two toggles in
`$HERDR_PLUGIN_CONFIG_DIR/config.json` (otherwise
`$XDG_CONFIG_HOME/herdr/plugins/config/herdr-suite/config.json`, then
`~/.config/herdr/plugins/config/herdr-suite/config.json`) gate it:

```json
{
  "gs_restore": {
    "install": false,
    "auto_install": false
  }
}
```

`install` is the master gate: with it false the startup hook does nothing at
all, even when `auto_install` is true. With `install` true, `auto_install`
chooses the behaviour — true installs the reporter idempotently at server start
and prints one line only when it changes something; false never writes the agent
directory and prints one hint per server socket instead. Unknown keys survive a
rewrite, and a missing or invalid file keeps both toggles off.

The reporter is `extensions/herdr-agent-state.ts` in this repository. It
installs into the gentle-shell agent directory named by `PI_CODING_AGENT_DIR`,
and only when that directory exists and holds an `extensions/` directory; a
vanilla pi installation has no such variable, so the feature simply does not
apply there.

**herdr's integration marker.** herdr decides whether an integration file is
current by reading a `// HERDR_INTEGRATION_VERSION=<n>` line from it, where
`<n>` is the pi integration version that herdr ships; `herdr integration status`
prints one line per agent, as `pi: current (v9)`, `pi: outdated (v9 < v10)`,
`pi: outdated (legacy < v10)` or `pi: not installed`. The shipped reporter
carries `// HERDR_INTEGRATION_ID=pi` and the marker with the version it was
written for — `9`, the pi integration of herdr 0.9.5 — and every install
stamps the version the installed herdr expects: the right-hand number of
`< vN`, or the only number of `current (vN)`. herdr never rewrites the file on
its own, which is why hs writes it, and why the `[[startup]]` hook refreshes it
on every server start: a reporter whose marker differs from what herdr now
expects is rewritten (its previous copy becomes the single backup) and prints
the install line; a reporter whose marker already matches is left alone and
prints nothing. When herdr cannot be run, prints no pi version (`not installed`,
`needs repair (vN)`) or prints something unparseable, the install keeps the
shipped `9` and says nothing — a missing or broken herdr never fails an install
or a server start. Official herdr then reports `pi: current` with no local
patch.

```bash
hs gs-restore install          # install or refresh the reporter
hs gs-restore install --force  # replace a file that is not a herdr reporter
hs gs-restore remove           # restore the latest backup, or delete the file
hs gs-restore status           # reporter, version and herdr markers, backup, launcher, toggles
hs gs-restore status --json    # the same report as one JSON document
```

`status` prints one `integration:` line for the herdr marker — the installed
version and the one herdr expects, as `v9 (herdr expects v9)`, and
`v10 (herdr expects v10; written for v9)` when the installed herdr expects a
version other than the shipped `9`. That difference is information: the marker
is what herdr reads, so the same reporter is current there and neither the line
nor the exit status reports a failure.

`status --json` prints one document carrying the same values as the human lines —
`config_path` and `config_present`, `install` and `auto_install`, `agent_dir`,
`reporter_path`, `reporter_present`, `reporter_installed`, `reporter_version`,
`version_matches`, `shipped_version`, `backup` and `gentle_shell`, plus
`integration_version`, `integration_expected` (numbers, omitted when unknown),
`integration_shipped` and `integration_differs` — plus `config_error` and
`agent_dir_error` when the environment does not provide the value, so a script
reads the report without parsing its prose.

An existing herdr pi integration is backed up next to the file before it is
replaced, as `<name>.bak-<timestamp>`; repeated installs keep that single backup
instead of collecting more, and `remove` puts the latest backup back. A file
that is not a herdr reporter is refused with a clear error unless `--force` is
passed, and even then it is backed up first. The directory has to be created by
whoever owns the gentle-shell installation: `install` refuses, with the
`mkdir -p` command to run, instead of inventing a path.

**Single reporter.** Keep exactly one copy of the reporter in the agent
directory's `extensions/`. Two copies report the same pane twice and race for
the session identity, which is why install, status and remove all name the one
file they manage.

**Compatibility.** The reporter is a TypeScript extension for pi. The
mechanism was verified on Linux with gentle-shell; validating this generic
reporter on Windows and WSL is still pending, which is why the plugin manifest
declares only `linux` and `macos`. On vanilla pi the extension reports no
`resume_argv` and skips the gentle-pi event bus, so it degrades silently rather
than failing: there is no launcher to detect and nothing user-specific to guess.
