package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/WilberC/herdr-suite/internal/herdrmock"
	"github.com/WilberC/herdr-suite/internal/layout"
	"github.com/WilberC/herdr-suite/internal/session"
)

const spaceLoadUsage = `usage: hs space load <name> [options]

Recreate the stored layout <name> in the destination workspace of the session
hs runs against. The split tree replays from its root: every split is made
before the splits inside its children, each pane is created once, pane labels
are restored, the agents the layout recorded are started, and the recorded tab
and pane are focused last.

A workspace holding a single idle pane is the empty case: that pane becomes the
first pane of the layout. A workspace with more panes or tabs needs --replace.

A session layout holds every workspace of a forked session: loading one creates
each workspace it stores instead of building over the current one, so --replace
and --new-workspace do not apply to it.

The layout name is resolved the same way the list resolves it: by path when it
holds a separator, then in the nearest project declaration under .herdr/, then in
the global store.

Options:
  --session <name>  Replay into that session instead of the current one
  --replace         Close the workspace's other panes and tabs before the replay
  --new-workspace   Build the layout in a new workspace instead of this one
  --select          Pick the layout and watch it replay in a new workspace
  --verbose         Report every replay step on stderr
  --dir <path>      Discover layouts from that directory instead of this one
  --dry-run         Print the replay plan and change nothing
`

// loadOptions is the parsed "hs space load" command line.
type loadOptions struct {
	name         string
	session      string
	dir          string
	replace      bool
	newWorkspace bool
	selecting    bool
	dryRun       bool
	verbose      bool
}

func runSpaceLoad(d deps, args []string) int {
	options, err := parseLoadArgs(args)
	if err != nil {
		return report(err, d.err)
	}
	if options.selecting {
		return runSpaceLoadSelect(d, options)
	}
	resolved, err := resolveStoredLayout(d, options.name, options.dir, spaceLoadUsage)
	if err != nil {
		return report(err, d.err)
	}
	document := resolved.Document

	ctx := context.Background()
	target, err := sessionTarget(ctx, d, options.session, spaceLoadUsage)
	if err != nil {
		return report(err, d.err)
	}

	space, err := d.spaces.Snapshot(ctx, target)
	if err != nil {
		return report(fmt.Errorf("snapshot %s: %w", describeTarget(target), err), d.err)
	}
	if document.Multi() {
		return loadSessionLayout(d, options, target, space, document)
	}

	destination, err := loadDestination(d, target, space, document, options)
	if err != nil {
		return report(err, d.err)
	}

	replay, err := layout.Plan(document, destination)
	if err != nil {
		return report(err, d.err)
	}
	if options.dryRun {
		fmt.Fprint(d.out, layout.RenderPlan(replay))
		return 0
	}

	if destination.Replace {
		fmt.Fprintln(d.err, replaceNote(destination, replay))
	}
	progress := newStepProgress(d.err, options.verbose, progressBudget, d.clock())
	err = layout.Apply(ctx, d.writer.Mutations(target), replay, progress.report)
	progress.finish()
	if err != nil {
		return reportReplayFailure(d, err)
	}
	fmt.Fprint(d.out, layout.ReplaySummary(replay))
	return 0
}

// loadSessionLayout replays a session layout: every workspace it stores is
// created again, and the last one ends focused.
func loadSessionLayout(d deps, options loadOptions, target herdrmock.Target, space herdrmock.Space, document layout.Layout) int {
	if options.replace || options.newWorkspace {
		return report(usageError{
			reason: fmt.Sprintf(
				"--replace and --new-workspace only apply to a single-workspace layout; '%s' holds %s, and a session load creates every one of them",
				document.Name, countLabel(len(document.Workspaces), "workspace")),
			usage: spaceLoadUsage,
		}, d.err)
	}

	plan, err := layout.PlanSession(document, layout.Target{
		Keep:   d.getenv("HERDR_PANE_ID"),
		Agents: liveAgents(space),
	})
	if err != nil {
		return report(err, d.err)
	}
	if options.dryRun {
		fmt.Fprint(d.out, layout.RenderSessionPlan(plan))
		return 0
	}

	progress := newStepProgress(d.err, options.verbose, progressBudget, d.clock())
	err = layout.ApplySession(context.Background(), d.writer.Mutations(target), plan, progress.report)
	progress.finish()
	if err != nil {
		return reportReplayFailure(d, err)
	}
	fmt.Fprint(d.out, layout.SessionReplaySummary(plan))
	return 0
}

// parseLoadArgs reads "<name> [options]" in any order and rejects anything else
// before a client call happens.
func parseLoadArgs(args []string) (loadOptions, error) {
	var options loadOptions
	var names []string
	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch {
		case arg == "--replace":
			options.replace = true
		case arg == "--new-workspace":
			options.newWorkspace = true
		case arg == "--dry-run":
			options.dryRun = true
		case arg == "--select":
			options.selecting = true
		case arg == "--verbose":
			options.verbose = true
		case arg == "--dir":
			if index+1 >= len(args) {
				return loadOptions{}, usageError{reason: "--dir needs a directory", usage: spaceLoadUsage}
			}
			index++
			options.dir = args[index]
		case arg == "--session":
			if index+1 >= len(args) {
				return loadOptions{}, usageError{reason: "--session needs a session name", usage: spaceLoadUsage}
			}
			index++
			options.session = args[index]
		case strings.HasPrefix(arg, "-"):
			return loadOptions{}, usageError{reason: fmt.Sprintf("unknown flag %q", arg), usage: spaceLoadUsage}
		default:
			names = append(names, arg)
		}
	}
	if options.selecting {
		if len(names) != 0 {
			return loadOptions{}, usageError{reason: "--select picks the layout, so pass no layout name", usage: spaceLoadUsage}
		}
		if options.replace {
			return loadOptions{}, usageError{reason: "--select builds in a new workspace, so --replace does not apply", usage: spaceLoadUsage}
		}
		return options, nil
	}
	if len(names) != 1 {
		return loadOptions{}, usageError{reason: "expected one layout name", usage: spaceLoadUsage}
	}
	options.name = names[0]
	return options, nil
}

// loadDestination is the live workspace the layout replays into: the active one
// when it matches the stored workspace, the user's choice when it does not, and
// nothing but what --new-workspace asks for when that is set.
func loadDestination(d deps, target herdrmock.Target, space herdrmock.Space, document layout.Layout, options loadOptions) (layout.Target, error) {
	destination := layout.Target{
		Replace: options.replace,
		Keep:    d.getenv("HERDR_PANE_ID"),
		Agents:  liveAgents(space),
	}
	if options.newWorkspace {
		return destination, nil
	}

	active, ok := activeWorkspace(space)
	if !ok {
		return layout.Target{}, fmt.Errorf("herdr reported no focused workspace in %s", describeTarget(target))
	}

	chosen := active
	if active.Label != document.Workspace.Label && len(space.Workspaces) > 1 {
		if !d.interactive || d.pick == nil {
			return layout.Target{}, session.Refusal{Reason: fmt.Sprintf(
				"layout '%s' holds workspace '%s' but the active workspace is '%s'; run it interactively to choose a workspace, or pass --new-workspace",
				document.Name, document.Workspace.Label, active.Label)}
		}
		id, err := d.pick(space.Workspaces)
		if err != nil {
			return layout.Target{}, err
		}
		chosen, ok = workspaceByID(space.Workspaces, id)
		if !ok {
			return layout.Target{}, fmt.Errorf("workspace '%s' is gone; run hs space load again", id)
		}
	}

	if err := checkDestination(chosen, options.replace); err != nil {
		return layout.Target{}, err
	}
	destination.Workspace = chosen
	return destination, nil
}

// checkDestination refuses a destination a replay would lay over existing work,
// unless --replace says the existing panes may be closed.
func checkDestination(workspace herdrmock.Workspace, replace bool) error {
	if replace {
		return nil
	}
	panes := workspacePaneCount(workspace)
	if panes > 1 || len(workspace.Tabs) > 1 {
		return session.Refusal{Reason: fmt.Sprintf(
			"workspace '%s' already has %s; pass --replace to close them before loading",
			workspace.Label, countLabel(panes, "pane"))}
	}
	return nil
}

// replaceNote is the stderr record of a replacing load: what it closes, taken
// from the destination as it was read.
func replaceNote(destination layout.Target, replay layout.Replay) string {
	var panes, tabs []string
	for _, action := range replay.Actions {
		switch action.Op {
		case layout.OpClosePane:
			panes = append(panes, action.PaneID)
		case layout.OpCloseTab:
			tabs = append(tabs, action.TabID)
		}
	}
	for _, tab := range destination.Workspace.Tabs {
		if tab.ID == replay.Base.TabID {
			continue
		}
		for _, pane := range tab.Panes {
			panes = append(panes, pane.ID)
		}
	}
	if len(panes) == 0 && len(tabs) == 0 {
		return fmt.Sprintf("replacing workspace '%s': nothing to close", destination.Workspace.Label)
	}

	var parts []string
	if len(panes) > 0 {
		parts = append(parts, fmt.Sprintf("%s (%s)", countLabel(len(panes), "pane"), strings.Join(panes, ", ")))
	}
	if len(tabs) > 0 {
		parts = append(parts, fmt.Sprintf("%s (%s)", countLabel(len(tabs), "tab"), strings.Join(tabs, ", ")))
	}
	return fmt.Sprintf("replacing workspace '%s': closing %s", destination.Workspace.Label, strings.Join(parts, " and "))
}

// liveAgents is every agent name herdr reports, so a replay does not try to
// reuse one that is already running.
func liveAgents(space herdrmock.Space) []string {
	var names []string
	for _, workspace := range space.Workspaces {
		for _, tab := range workspace.Tabs {
			for _, pane := range tab.Panes {
				if pane.AgentName != "" {
					names = append(names, pane.AgentName)
				}
			}
		}
	}
	return names
}

func workspaceByID(workspaces []herdrmock.Workspace, id string) (herdrmock.Workspace, bool) {
	for _, workspace := range workspaces {
		if workspace.ID == id {
			return workspace, true
		}
	}
	return herdrmock.Workspace{}, false
}

// workspacePaneCount counts the panes herdr reports for a workspace, falling
// back to its reported total when the tabs were not read in full.
func workspacePaneCount(workspace herdrmock.Workspace) int {
	count := 0
	for _, tab := range workspace.Tabs {
		count += len(tab.Panes)
	}
	if workspace.PaneCount > count {
		return workspace.PaneCount
	}
	return count
}

func countLabel(count int, noun string) string {
	if count == 1 {
		return fmt.Sprintf("%d %s", count, noun)
	}
	return fmt.Sprintf("%d %ss", count, noun)
}

// runSpaceLoadSelect is "hs space load --select": the picker runs the replay
// itself, so its spinner and per-step report cover the whole restore.
func runSpaceLoadSelect(d deps, options loadOptions) int {
	if !d.interactive || d.pickLayout == nil {
		return report(session.Refusal{Reason: "the layout picker needs a terminal; pass a layout name"}, d.err)
	}

	sources, err := layoutSources(d, options.dir)
	if err != nil {
		return report(err, d.err)
	}
	entries, err := layout.List(sources)
	if err != nil {
		return report(err, d.err)
	}
	if len(entries) == 0 {
		return report(session.Refusal{Reason: "no layout is stored here; create one with hs space save"}, d.err)
	}

	ctx := context.Background()
	target, err := sessionTarget(ctx, d, options.session, spaceLoadUsage)
	if err != nil {
		return report(err, d.err)
	}

	var restore layoutRestore
	if !options.dryRun {
		restore = func(entry layout.Entry, report func(layout.Step)) (string, error) {
			return restorePickedLayout(d, ctx, target, entry, report)
		}
	}
	picked, err := d.pickLayout(entries, restore)
	if err != nil {
		return reportReplayFailure(d, err)
	}
	if options.dryRun {
		return printPickedPlan(d, ctx, target, picked.Entry)
	}
	if picked.Summary != "" {
		fmt.Fprintln(d.out, picked.Summary)
	}
	return 0
}

// restorePickedLayout is the replay the picker runs in-program. --select always
// builds in a new workspace, so the destination carries no existing workspace
// and the replay creates the stored one.
func restorePickedLayout(d deps, ctx context.Context, target herdrmock.Target, entry layout.Entry, report func(layout.Step)) (string, error) {
	document, _, err := layout.LoadFile(entry.Path)
	if err != nil {
		return "", layoutRefusal(err, entry.Path)
	}
	space, err := d.spaces.Snapshot(ctx, target)
	if err != nil {
		return "", fmt.Errorf("snapshot %s: %w", describeTarget(target), err)
	}
	destination := layout.Target{Keep: d.getenv("HERDR_PANE_ID"), Agents: liveAgents(space)}

	if document.Multi() {
		plan, err := layout.PlanSession(document, destination)
		if err != nil {
			return "", err
		}
		if err := layout.ApplySession(ctx, d.writer.Mutations(target), plan, layout.StepObserver(report)); err != nil {
			return "", err
		}
		return layout.SessionReplaySummary(plan), nil
	}

	replay, err := layout.Plan(document, destination)
	if err != nil {
		return "", err
	}
	if err := layout.Apply(ctx, d.writer.Mutations(target), replay, layout.StepObserver(report)); err != nil {
		return "", err
	}
	return layout.ReplaySummary(replay), nil
}

// printPickedPlan is the dry run of an interactive load: the picker only chose,
// and the plan prints exactly as a named dry run prints it.
func printPickedPlan(d deps, ctx context.Context, target herdrmock.Target, entry layout.Entry) int {
	document, _, err := layout.LoadFile(entry.Path)
	if err != nil {
		return report(layoutRefusal(err, entry.Path), d.err)
	}
	space, err := d.spaces.Snapshot(ctx, target)
	if err != nil {
		return report(fmt.Errorf("snapshot %s: %w", describeTarget(target), err), d.err)
	}
	destination := layout.Target{Keep: d.getenv("HERDR_PANE_ID"), Agents: liveAgents(space)}

	if document.Multi() {
		plan, err := layout.PlanSession(document, destination)
		if err != nil {
			return report(err, d.err)
		}
		fmt.Fprint(d.out, layout.RenderSessionPlan(plan))
		return 0
	}
	replay, err := layout.Plan(document, destination)
	if err != nil {
		return report(err, d.err)
	}
	fmt.Fprint(d.out, layout.RenderPlan(replay))
	return 0
}

// reportReplayFailure is the stderr contract of a replay that stopped halfway:
// the failing step, then the warning that herdr cannot undo it. The warning
// names the unit the failed replay left behind.
func reportReplayFailure(d deps, err error) int {
	var step layout.StepError
	if !errors.As(err, &step) {
		return report(err, d.err)
	}
	fmt.Fprintf(d.err, "hs: %v\n", err)
	if step.Workspace == "" {
		fmt.Fprintf(d.err, "hs: herdr has no transaction, so the workspace may hold a partial layout. Remove the panes or tabs it created and load again.\n")
	} else {
		fmt.Fprintf(d.err, "hs: herdr has no transaction, so the session may hold a partial layout. Remove the workspaces, tabs or panes it created and load again.\n")
	}
	return 1
}
