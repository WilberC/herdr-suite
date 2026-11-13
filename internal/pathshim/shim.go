package pathshim

// FileName is the file the shim is installed as, so a shell finds it as "hs".
const FileName = "hs"

// Script is the shim, and the only content an install writes. It never names a
// checkout: it resolves the plugin root each time it runs, so a plugin update
// leaves it working. Only sh, grep, head and sed are assumed, because the
// target is any machine that runs herdr.
const Script = `#!/bin/sh
# hs - herdr-suite shim written by "hs path install" (remove: hs path remove).
# The plugin root is resolved at run time, so a plugin update never breaks this.
root=$(herdr plugin list --plugin herdr-suite --json 2>/dev/null \
  | grep -o '"plugin_root"[[:space:]]*:[[:space:]]*"[^"]*"' \
  | head -n 1 | sed 's/.*"\([^"]*\)"$/\1/')
if [ -z "$root" ]; then
  echo 'hs: the herdr-suite plugin is not installed; run: herdr plugin install WilberC/herdr-suite' >&2
  exit 1
fi
if [ ! -x "$root/bin/hs" ]; then
  echo "hs: $root/bin/hs is missing or not executable; run: herdr plugin update herdr-suite" >&2
  exit 1
fi
exec "$root/bin/hs" "$@"
`

// Refusal is a request this package will not carry out without a value the
// caller has to choose, such as where to write when HOME is unset.
type Refusal struct {
	Reason string
}

func (r Refusal) Error() string { return r.Reason }
