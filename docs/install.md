# Install

Back to the [README](../README.md). Also read: [Inside herdr](inside-herdr.md)
for how the plugin runs the binary, and [Health](health.md) for the post-install
check.

The plugin requires herdr 0.7.5 or newer (`herdr api snapshot`, which every
space command reads through, first shipped in 0.7.2) and installs like any other
herdr plugin:

```bash
herdr plugin install WilberC/herdr-suite
```

Each `v*` tag publishes four static binaries — `hs-linux-amd64`,
`hs-linux-arm64`, `hs-darwin-amd64` and `hs-darwin-arm64` — next to a
`SHA256SUMS` file on [the releases
page](https://github.com/WilberC/herdr-suite/releases). `herdr plugin install`
leaves a checkout whose path `herdr plugin list --plugin herdr-suite --json`
reports as `plugin_root`; run this inside it, picking the asset for this machine:

```bash
ASSET=hs-linux-amd64   # or hs-darwin-arm64, hs-darwin-amd64, hs-linux-arm64
curl -fsSLO "https://github.com/WilberC/herdr-suite/releases/latest/download/$ASSET"
curl -fsSLO https://github.com/WilberC/herdr-suite/releases/latest/download/SHA256SUMS
grep -F "$ASSET" SHA256SUMS | sha256sum -c -   # macOS: | shasum -a 256 -c -
mkdir -p bin && install -m 755 "$ASSET" bin/hs
```

`SHA256SUMS` covers every asset, so the check reads only the line of the binary
this machine downloaded. From this point herdr's `[[build]]` hook takes over:
on `plugin install` (and update) `bin/ensure-binary.sh --in-tree` provisions
`bin/hs` — local Go build first, prebuilt release binary as fallback — while
`herdr plugin link` runs no build, so a working tree builds it with
`make hs`; every action and pane expects exactly that path. See
[Inside herdr](inside-herdr.md).

A published binary self-reports its release, and `hs version --check` compares
it against the manifest beside it — that is the check to run after installing or
updating. See [Health](health.md).

## PATH

`bin/hs` lives inside the plugin checkout, whose path carries a hash and a
generation number, so pointing a shell profile at it breaks at the first plugin
update. `hs path install` writes a small `sh` shim instead — `hs` in
`$HOME/.local/bin` by default, or in `--dir <path>` — that resolves the plugin
root when it runs:

```bash
bin/hs path install    # run once from the plugin root; afterwards plain `hs` works
hs path status         # installed? still our script? on PATH? plugin resolvable?
hs path remove         # delete the shim again
```

The shim is fifteen lines and asks nothing of the machine beyond `sh`, `grep`,
`head` and `sed`: it reads `plugin_root` out of `herdr plugin list --plugin
herdr-suite --json`, checks that `<root>/bin/hs` is executable, and `exec`s it
with the arguments it was given. It never stores the checkout path, so a plugin
update, a new generation directory or a moved config all leave it working; a
plugin that is not installed makes it exit 1 with the `herdr plugin install`
command that fixes it. The global "Install hs on PATH" plugin action runs the
same install — see [Inside herdr](inside-herdr.md) — and nothing is ever
installed behind your back.

Install reports the file it wrote and whether its directory is in `PATH`; when
it is not, the report carries the line to copy —
`export PATH="$HOME/.local/bin:$PATH"` — because a shim a shell never searches
is not an `hs` command. Two options change what the commands do: `--dir <path>`
chooses the directory, and `--force` replaces an existing `hs` this tool did not
write, after copying it to `<name>.bak-<timestamp>`. An `hs` that is already the
current shim is a no-op, and a removal that finds nothing exits 0. With `HOME`
unset every one of the three commands refuses and names `--dir` rather than
guessing a home directory.

## Shell completions

`hs completions fish` prints a fish completion script. It completes the command
tree statically and asks `hs` itself for the live names — `hs session list
--json` for session names, `hs space list --json` for layout names — so the
completion offers exactly what the CLI resolves, project declarations included,
instead of keeping a second copy of the rules.

```bash
hs completions fish             # print the fish completion script
hs completions fish --install   # write it to $XDG_CONFIG_HOME/fish/completions/hs.fish
```

`--install` writes `hs.fish` to `$XDG_CONFIG_HOME/fish/completions`, or
`~/.config/fish/completions` when `XDG_CONFIG_HOME` is unset, and creates that
directory when it does not exist, so the install is one command. With neither
variable set it refuses and names the one to export rather than guessing a home
directory. A new shell reads the file automatically; `complete -C 'hs space
load '` reloads it in the current one.

The script is generated from the CLI's own command table, never edited by hand:
every command and subcommand the binary dispatches appears in it, a test reads
that table to prove it, and regenerating after an upgrade is another `--install`.
Session names complete for `hs attach` and `hs session
create|stop|restart|delete|fork`; layout names complete for `hs space load` and
`hs space show`; `hs space save` names a new layout, so it completes none. A
herdr call that fails prints nothing, because both reads drop stderr: a
completion must never write on the command line. A layout reference may be a
path, so `hs space load` and `hs space show` keep fish's file completion beside
the stored names.
