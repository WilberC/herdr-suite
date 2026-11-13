# Inside herdr

Back to the [README](../README.md). The commands these actions run are in
[Spaces](spaces.md), the install path in [Install](install.md), and the reporter
toggle in [GS session restore](gs-restore.md).

`herdr-plugin.toml` declares seven actions and the two panes they open, anchored
to the `hs` binary in the plugin's own `bin/` directory — the layout herdr-bar
uses for its release binary:

| Action | Runs |
| --- | --- |
| `save` | `bin/hs space save --current`, naming the layout after the workspace it runs in |
| `load` | the `load` pane: `bin/hs space load --select`, picking a stored layout and rebuilding it in a new workspace |
| `list` | the `layouts` pane: `bin/hs space list --pane`, keeping the table until enter is pressed |
| `gs-install` | `bin/hs gs-restore install`, installing the GS session-restore reporter (default off) |
| `hs-path` | `bin/hs path install`, writing the `hs` shim into `~/.local/bin` so `hs` works from a shell ([Install § PATH](install.md#path)) |
| `last-workspace` | `bin/hs last workspace`, focusing the previous workspace ([Last](last.md)) |
| `last-tab` | `bin/hs last tab`, focusing the previous tab ([Last](last.md)) |

Bind them like any plugin action:

```toml
[[keys.command]]
key = "prefix+l"
type = "plugin_action"
command = "herdr-suite.list"
```

Action output lands in `herdr plugin log herdr-suite`.

The same manifest declares two event hooks, `workspace.focused` and
`tab.focused`, each running `bin/hs last observe`. Herdr runs them once per focus
event; the command records the id in a per-session state file and exits, so no
daemon stays resident. Nothing is bound by default: bind
`herdr-suite.last-workspace` and `herdr-suite.last-tab` when you want the two
toggles, as [Last](last.md) shows.

The plugin's config file lives in the plugin config directory —
`$HERDR_PLUGIN_CONFIG_DIR/config.json`, otherwise
`$XDG_CONFIG_HOME/herdr/plugins/config/herdr-suite/config.json`, then
`~/.config/herdr/plugins/config/herdr-suite/config.json`. [GS session
restore](gs-restore.md) owns the two toggles it holds.

## bin/hs

Two facts are documented rather than worked around. On `herdr plugin
install` (and on update) the `[[build]]` hook runs `bin/ensure-binary.sh
--in-tree`, which builds `bin/hs` with a local Go toolchain and falls back to
the prebuilt `hs-<os>-<arch>` release asset (published on every `v*` tag,
with `SHA256SUMS`); a machine without Go still installs the prebuilt asset,
and `herdr plugin link` runs no build at all, so a working tree uses
`make hs` — every action and pane above expects exactly that path.
The manual asset path is in [Install](install.md).
And inside herdr, runtime commands run with the plugin directory as their working
directory, so project discovery there needs `--dir <path>`; herdr reports no
project directory to a plugin command, so guessing one would be wrong.
