// Package layout owns stored space layouts: the snapshot schema, reading and
// writing it, the directory layouts live in, and the stable report a saved
// layout prints.
//
// # Format
//
// A layout is stored as JSON, not TOML. Go's standard library ships a JSON
// encoder and decoder; TOML would need a third-party dependency, and the stored
// layout schema is nested (a split tree), which is the shape JSON was made for.
// A single format keeps the file, the --json output and the in-memory snapshot
// identical, so a reader cannot disagree with the writer.
//
// # Schema
//
// A layout names itself, the session it was snapshotted from when that is
// known, and one workspace. A workspace holds tabs, and a tab holds a split
// tree:
//
//	Layout  { name, session?, workspace }
//	Workspace { label, tabs[] }
//	Tab     { label?, focused, focused_pane_id?, root }
//	Node    { type: "pane" }  pane_id?, label?, cwd?, agent?
//	Node    { type: "split" } direction, ratio, first, second
//	Agent   { kind, name?, state? }
//
// A split is binary and ordered: first is the left or top child of a "right" or
// "down" split, exactly as herdr reports it, and ratio is the share first takes.
// A pane records what herdr exposes about it, and nothing else: pane_id is the
// pane that was captured, label and cwd are optional, and agent holds the agent
// identification (kind, name when herdr reports one, and its reported state).
// herdr does not report the command a plain shell pane runs, so no command is
// recorded rather than a guessed one; restoring such a pane opens its shell in
// its cwd.
//
// # Storage
//
// Layouts live under one global directory, Dir, which follows XDG_CONFIG_HOME.
// The file name is the layout name plus ".json"; names are sanitized before
// they reach this package, and Save refuses a name that could escape the
// directory. Saving never replaces a stored layout unless the caller forces it.
package layout
