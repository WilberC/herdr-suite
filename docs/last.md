# Previous workspace and previous tab

Back to the [README](../README.md). The sessions these toggles move through are
in [Sessions](sessions.md), the plugin surface in
[Inside herdr](inside-herdr.md), and the exit codes in
[Health § Exit codes](health.md#exit-codes).

Two commands answer the question "where was I before this?":

```bash
hs last workspace    # focus the workspace you were on before this one
hs last tab          # focus the tab you were on before this one
```

Each one focuses the recorded previous id and swaps the pair, so the same
command goes back. This is the ergonomics of
[herdr-last](https://github.com/lmilojevicc/herdr-last), reimplemented here
without its daemon: no resident process, no socket client, no background
watcher.

## What it tracks, and where

One history per herdr session holds two pairs:

```json
{
  "prev_workspace": "w1",
  "cur_workspace": "w2",
  "prev_tab": "t1",
  "cur_tab": "t2"
}
```

The file is
`$HERDR_PLUGIN_STATE_DIR/herdr-suite/last-<socket-hash>.json` inside herdr, or
`$XDG_STATE_HOME/herdr-suite/last-<socket-hash>.json` (then
`~/.local/state/herdr-suite/…`) outside a plugin command. `<socket-hash>` is the
first twelve hex digits of the SHA-256 of `$HERDR_SOCKET_PATH`, so two sessions
never share one history; with no socket path the name is `last-default.json`.

Writes are atomic: the command writes a temporary file beside the target and
renames it into place, so a reader never sees a half-written document and no
temporary file survives a successful write. A missing or corrupt file is an
empty history, which is what a fresh session has. An event that repeats the
current id writes nothing, so the pair never collapses into itself.

## No daemon

The plugin declares two event hooks, one per focus kind:

```toml
events = [
  { on = "workspace.focused", command = ["bin/hs", "last", "observe"] },
  { on = "tab.focused", command = ["bin/hs", "last", "observe"] },
]
```

Herdr runs the command once per focus event. It reads the event from
`$HERDR_PLUGIN_EVENT` (the kind) and `$HERDR_PLUGIN_EVENT_JSON` (the payload,
whose `data` object carries `workspace_id` or `tab_id`), updates the state file,
and exits 0 without printing anything. Nothing stays running between events, so
the feature costs one short-lived process per focus change — a median of about
8 ms over fifty runs on the development machine — and zero resident processes.
An unknown kind, a missing or malformed payload, or a state file that cannot be
written also exits 0 silently: a recorder must never fail the focus event it is
observing.

The toggle's own focus event is harmless. By the time its hook reads the state,
the id is either already current — the repeat rule makes it a no-op — or the hook
records the very swap the toggle is writing, which is the same pair. That is why
the repeat case must never collapse the pair into itself.

This is why there is no `hs last status` and no `hs last remove`. There is
nothing installed and nothing resident to inspect or stop; the only artifact is
one small JSON file per session, safe to delete at any time, and it is rewritten
on the next focus event. Removing the two keybindings below makes the feature
inert, and uninstalling the plugin removes the hooks themselves.

## Suggested keybindings

Nothing is bound by default — the plugin declares the actions, you choose the
keys. Two free keys that read well for "go back":

```toml
[[keys.command]]
key = "prefix+,"
type = "plugin_action"
command = "herdr-suite.last-workspace"

[[keys.command]]
key = "prefix+."
type = "plugin_action"
command = "herdr-suite.last-tab"
```

## Refusals

`hs last workspace` refuses with exit status 2 and
`hs: no previous workspace recorded yet; focus another workspace first` when the
session has no earlier workspace — a fresh session or one that never left its
first workspace. `hs last tab` refuses the same way with
`hs: no previous tab recorded in this workspace yet`. A refusal never changes
the recorded history, and a focus that herdr rejects leaves it unchanged too, so
a failed toggle can simply be retried.

The state is one pair per kind: moving to a third workspace replaces the older
one, exactly like the two-slot history the toggle needs. It records ids that
herdr may since have closed; focusing a closed workspace or tab fails with
herdr's error and keeps the history, which the next focus event then replaces.
