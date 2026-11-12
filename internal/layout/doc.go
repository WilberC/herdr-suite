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
//
// # Replay
//
// Plan is the inverse of Capture: it turns a stored layout into the ordered
// ladder of herdr mutations that recreates it, and Apply walks that ladder. herdr
// has no transaction, so a replay is a sequence of explicit calls and a failure
// names the step that stopped it.
//
// The order is depth first, parents before children. A binary split in herdr is
// made by splitting one pane, so a replayed split consumes the pane that owns
// the area it divides and creates the pane of its second child's leftmost leaf.
// The split therefore comes first, then the splits inside its first child, then
// the splits inside its second: every split is replayed before the splits that
// subdivide it, and each stored leaf receives exactly one pane. Replaying the
// other way round would need a pane the split has not created yet.
//
// A split is replayed with the stored direction and ratio. herdr keeps the split
// pane and puts the new one beside it, so the stored ratio is the share the
// original pane keeps and needs no conversion. A ratio of zero is not a ratio
// herdr reports, so the replay leaves it out and herdr divides the area evenly.
//
// Leaves carry what the snapshot recorded. A stored label is applied with a
// rename, and a stored agent is started in its pane with "herdr agent start".
// herdr requires lowercase agent names that start with a letter and are unique
// among live agents, so a stored name is folded into that shape, a leaf without
// one falls back to its agent kind and then to the layout name, and a collision
// gains a numeric suffix. A leaf with no agent stays a plain pane: herdr reports
// no command for a plain shell, so none was recorded and none is restored. The
// pane opens in its stored cwd, which is the cwd passed to the call that created
// the pane.
//
// The destination workspace decides what a replay starts from. An existing
// workspace gives its active tab and that tab's first pane as the base, which the
// first stored tab grows from; with Replace the workspace's other panes and tabs
// are closed first, and the pane the caller runs in is never one of them. A
// workspace that does not exist yet is created from the stored label and cwd.
//
// Focus is restored last: the destination workspace, then the stored focused
// tab, then, when the stored focused pane runs an agent, that agent by name. The
// focused pane itself receives focus by being the only pane the replay creates
// with focus; herdr's CLI has no command that focuses a pane by id, so a stored
// focused pane that is not created by a split keeps whatever focus its tab had.
package layout
