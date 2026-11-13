# Version, doctor and exit codes

Back to the [README](../README.md). Installation itself is in
[Install](install.md), and the store the doctor counts is in [Spaces § Layout
storage](spaces.md#layout-storage).

A published binary knows its own release: the release workflow stamps the tag
into it with `-ldflags "-X main.version=<tag>"`, so `hs version` reports the tag
it was built from. A build from a working tree embeds nothing and keeps the `dev`
default — `make hs` passes no ldflags on purpose.

```bash
hs version                      # hs version 0.2.0, or hs version dev in a tree
hs version --json               # {"version":"0.2.0"}
hs version --check              # hs version 0.2.0 · manifest 0.2.0 · ok, exit 1 when the binary is older than the manifest
```

`hs version --check` reads the manifest beside the binary — the plugin root above
`bin/hs`, then the working directory; `--manifest <path>` overrides both — and
exits 0 when the two versions match, 1 when the binary is stale, naming both
values and the fix. A dev build has no release version to compare, so it reports
`dev build; no release version embedded` and always exits 0: a working tree is
never failed by its own build. This is the check a script runs after installing
or updating the plugin.

```bash
hs doctor                       # PASS/WARN/FAIL/SKIP lines; exit 1 on a FAIL
hs doctor --json                # the whole report as one JSON document
```

`hs doctor` reports the installation one line per check, and writes nothing:

```text
PASS  herdr                herdr 0.9.5 at /usr/local/bin/herdr (min_herdr_version 0.7.5)
PASS  socket               reachable · 3 sessions
PASS  config               ~/.config/herdr/plugins/config/herdr-suite/config.json (missing) · gs_restore install off, auto_install off
PASS  binary               0.2.0 matches herdr-plugin.toml
PASS  layouts              ~/.config/herdr/herdr-suite/layouts · 2 layouts
PASS  project              project /home/wilber/dotfiles · 1 layout
SKIP  gs-restore reporter  not installed (feature default off)
SKIP  gs-restore version   no reporter installed
PASS  gs-restore backup    none
SKIP  gs-restore launcher  not checked; no reporter installed
PASS  manifest             ~/.local/share/herdr/plugins/herdr-suite/herdr-plugin.toml · Herd Suite 0.2.0 (min_herdr_version 0.7.5)
```

Every line prints `PASS`, `WARN`, `FAIL` or `SKIP`, and a failing line names the
fix in one clause — `stale binary: embedded 0.1.0, manifest 0.2.0; run make hs or
reinstall the plugin`. The exit status is 0 when nothing failed and 1 when a line
failed, so a warning keeps a script green. `--json` prints the whole report —
the build version, `ok` and every check with its id, status and detail — as one
document.

The lines cover: the herdr binary and its version against `min_herdr_version`,
the socket (a session list bounded by a three-second timeout, so a hung socket is
a `FAIL` line and never a hanging report), the plugin config and its `gs_restore`
toggles, this binary against the manifest version, the layout store and what the
current directory declares (counts only, never a layout's contents), the GS
session-restore files, and the plugin manifest itself.

## Exit codes

| Code | Meaning |
| --- | --- |
| `0` | the operation succeeded, the user declined a confirmation, `hs version --check` found a matching version, or `hs doctor` reported no failure |
| `1` | herdr failed, `hs version --check` found a stale binary, or `hs doctor` reported a `FAIL` line |
| `2` | bad arguments, an invalid name, a protected session, a missing confirmation, a missing layout, a layout name more than one place declares, a layout written by a newer `hs`, a `PI_CODING_AGENT_DIR` that is unset or is not a gentle-shell agent directory, a fish completion directory that neither `XDG_CONFIG_HOME` nor `HOME` names, or an `hs` in the bin directory that this tool did not write and `--force` did not authorize |

Inside herdr, the plugin exposes the same operations as the `save`, `load` and
`list` actions — see [Inside herdr](inside-herdr.md).
