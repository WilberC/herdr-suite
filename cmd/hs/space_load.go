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

Options:
  --session <name>  Replay into that session instead of the current one
  --replace         Close the workspace's other panes and tabs before the replay
  --new-workspace   Build the layout in a new workspace instead of this one
  --dry-run         Print the replay plan and change nothing
`

// loadOptions is the parsed "hs space load" command line.
type loadOptions struct {
	name         string
	session      string
	replace      bool
	newWorkspace bool
	dryRun       bool
}

func runSpaceLoad(d deps, args []string) int {
	options, err := parseLoadArgs(args)
	if err != nil {
		return report(err, d.err)
	}
	name, err := session.SanitizeName(options.name, "layout")
	if err != nil {
		return report(usageError{reason: err.Error(), usage: spaceLoadUsage}, d.err)
	}

	dir, err := layout.Dir(d.getenv)
	if err != nil {
		return report(err, d.err)
	}
	document, _, err := layout.Load(dir, name)
	if err != nil {
		var missing layout.NotFoundError
		if errors.As(err, &missing) {
			return report(session.Refusal{Reason: fmt.Sprintf(
				"unknown layout '%s'; create it with hs space save %s", name, name)}, d.err)
		}
		return report(err, d.err)
	}

	ctx := context.Background()
	target, err := sessionTarget(ctx, d, options.session, spaceLoadUsage)
	if err != nil {
		return report(err, d.err)
	}

	space, err := d.spaces.Snapshot(ctx, target)
	if err != nil {
		return report(fmt.Errorf("snapshot %s: %w", describeTarget(target), err), d.err)
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
	if err := layout.Apply(ctx, d.writer.Mutations(target), replay); err != nil {
		var step layout.StepError
		if !errors.As(err, &step) {
			return report(err, d.err)
		}
		fmt.Fprintf(d.err, "hs: %v\n", err)
		fmt.Fprintf(d.err, "hs: herdr has no transaction, so the workspace may hold a partial layout. Remove the panes or tabs it created and load again.\n")
		return 1
	}
	fmt.Fprint(d.out, layout.ReplaySummary(replay))
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
