// Package pathshim writes the small shell script that puts hs on a plugin user's
// PATH.
//
// The plugin ships bin/hs inside a checkout whose path carries a hash and a
// generation number, so a path baked into a shell profile stops working at the
// first plugin update. The shim therefore resolves the plugin root when it runs,
// from "herdr plugin list --plugin herdr-suite --json", and execs
// <root>/bin/hs with the arguments it was given. Nothing about this machine is
// stored in it.
//
// The shell pipeline inside Script and ParsePluginRoot read the same field by
// the same rule: the first "plugin_root" value in that output. Script is the
// file a user keeps, so it is frozen by a golden test, and a test runs the shell
// pipe against ParsePluginRoot to prove the two cannot disagree.
//
// Install, Remove and Inspect are the write and read sides: an existing hs that
// is not the shim is refused unless --force keeps a backup of it, an identical
// shim is a no-op, and status reports what is in place without writing anything.
package pathshim
