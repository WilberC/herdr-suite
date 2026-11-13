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
// known, and either one workspace or, when a fork captured a session, every
// workspace of that session. A workspace holds tabs, and a tab holds a split
// tree:
//
//	Layout  { schema_version: 1, name, session?, workspace }            single-workspace form
//	Layout  { schema_version: 1, name, session?, workspaces[] }         session form
//	Workspace { label, tabs[] }
//	Tab     { label?, focused, focused_pane_id?, root }
//	Node    { type: "pane" }  pane_id?, label?, cwd?, agent?
//	Node    { type: "split" } direction, ratio, first, second
//	Agent   { kind, name?, state? }
//
// The two forms never mix in one file: a stored layout holds either the workspace
// that was active when it was saved or the workspace list a fork captured, so a
// file written before the session form existed still reads and re-encodes
// unchanged.
//
// # Schema version
//
// Every saved document names its schema at the top level, and CurrentSchema is
// the only version this build writes. A document that carries no schema_version
// predates the field — v0.1.0 wrote single-workspace files without it — and is
// read as CurrentSchema, so a stored layout keeps working across an upgrade.
// A document that names a version this build cannot read, or a value that is not
// an integer, is refused instead of read: the fields this build knows may not be
// all the file holds, and silently dropping the rest would lose a layout. The
// refusal names the file, the version and the fix ("written by a newer
// herdr-suite; update hs").
//
// Reading never rewrites a document, so a load-only flow leaves an older file
// byte-for-byte as it was; the next save writes the field, and that field is the
// only difference a saved legacy document gains. Saving a document this build
// read and saving it again therefore produces identical bytes.
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
// Saving always writes the global directory: a project declares the layouts it
// commits itself, and nothing in this package writes into one.
//
// # Discovery
//
// Sources is what one working directory can see: the global directory, and the
// declaration of the nearest project at or above it, which Discover finds the
// way git finds a repository root. A project declares layouts in .herdr: a
// layouts directory whose file names are the layout names, a layouts.toml index
// that names the files it commits, or both. The index is a narrow TOML subset —
// one [layouts] table of name = "path" entries relative to .herdr, with '#'
// comments, and no path leaving .herdr — and anything else is refused rather
// than guessed at.
//
// Resolve reads one layout, and its order is the rule the commands share:
//
//  1. an explicit path, which is the reference itself and loads that file,
//  2. the project declaration, searched by name,
//  3. the global directory.
//
// A name the project does not declare falls through to the global directory. A
// name the project declares twice — in its layouts directory and again in its
// index, for example — is refused with AmbiguousError, which lists every
// candidate and its scope, because picking one silently would load a layout the
// caller did not name. List prints the same sources in the same order, so what a
// listing showed is what a load resolves.
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
//
// # Session replay
//
// A session layout replays into the session instead of into one workspace:
// PlanSession builds one replay per stored workspace, and every one of them
// starts by creating its workspace, so nothing in the session is replaced.
// ApplySession walks the replays in stored order and a failure names both the
// workspace and the step. Each replay restores its own focus last, which leaves
// the last stored workspace focused. Agent names are chosen across the whole
// plan, because herdr keeps them unique per session rather than per workspace.
package layout
