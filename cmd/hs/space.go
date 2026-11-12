package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/WilberC/herdr-suite/internal/herdrmock"
	"github.com/WilberC/herdr-suite/internal/layout"
	"github.com/WilberC/herdr-suite/internal/session"
)

const spaceUsage = `usage: hs space <command>

Commands:
  save <name>             Snapshot the active workspace as a layout
  load <name>             Recreate a stored layout in a workspace

Run "hs space save --help" or "hs space load --help" for their options.

A layout holds the workspace's tabs and each tab's split tree: split
directions, ratios, pane labels, cwds and agent identifications. Layouts are
stored as JSON under $XDG_CONFIG_HOME/herdr/herdr-suite/layouts.
`

const spaceSaveUsage = `usage: hs space save <name> [options]

Snapshot the active workspace of the session hs runs against and store it as
the layout <name>. Names are sanitized like session names: whitespace runs
become '-', and only letters, digits, '.', '_' and '-' are kept.

Options:
  --session <name>  Read that session instead of the current one
  --force           Replace a stored layout with the same name
  --json            Print the snapshot document instead of the report

Without --session the snapshot reads the session this process already talks
to, which is the herdr session containing the pane hs runs in.
`

func runSpace(d deps, args []string) int {
	if len(args) == 0 {
		fmt.Fprint(d.err, spaceUsage)
		return 2
	}

	switch args[0] {
	case "save":
		return runSpaceSave(d, args[1:])
	case "load":
		return runSpaceLoad(d, args[1:])
	}

	fmt.Fprintf(d.err, "unknown space command %q\n\n%s", args[0], spaceUsage)
	return 2
}

func runSpaceSave(d deps, args []string) int {
	options, err := parseSaveArgs(args)
	if err != nil {
		return report(err, d.err)
	}
	name, err := session.SanitizeName(options.name, "layout")
	if err != nil {
		return report(usageError{reason: err.Error(), usage: spaceSaveUsage}, d.err)
	}

	ctx := context.Background()
	target, err := sessionTarget(ctx, d, options.session, spaceSaveUsage)
	if err != nil {
		return report(err, d.err)
	}

	space, err := d.spaces.Snapshot(ctx, target)
	if err != nil {
		return report(fmt.Errorf("snapshot %s: %w", describeTarget(target), err), d.err)
	}
	workspace, ok := activeWorkspace(space)
	if !ok {
		return report(fmt.Errorf("herdr reported no focused workspace in %s", describeTarget(target)), d.err)
	}

	document, err := layout.Capture(name, space, workspace)
	if err != nil {
		return report(err, d.err)
	}
	dir, err := layout.Dir(d.getenv)
	if err != nil {
		return report(err, d.err)
	}
	path, err := layout.Save(dir, document, options.force)
	if err != nil {
		return report(err, d.err)
	}

	if options.json {
		return report(layout.Write(d.out, document), d.err)
	}
	fmt.Fprint(d.out, layout.Summary(document, path))
	return 0
}

// saveOptions is the parsed "hs space save" command line.
type saveOptions struct {
	name    string
	session string
	force   bool
	json    bool
}

// parseSaveArgs reads "<name> [--session <name>] [--force] [--json]" in any
// order and rejects anything else before a client call happens.
func parseSaveArgs(args []string) (saveOptions, error) {
	var options saveOptions
	var names []string
	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch {
		case arg == "--force":
			options.force = true
		case arg == "--json":
			options.json = true
		case arg == "--session":
			if index+1 >= len(args) {
				return saveOptions{}, usageError{reason: "--session needs a session name", usage: spaceSaveUsage}
			}
			index++
			options.session = args[index]
		case strings.HasPrefix(arg, "-"):
			return saveOptions{}, usageError{reason: fmt.Sprintf("unknown flag %q", arg), usage: spaceSaveUsage}
		default:
			names = append(names, arg)
		}
	}
	if len(names) != 1 {
		return saveOptions{}, usageError{reason: "expected one layout name", usage: spaceSaveUsage}
	}
	options.name = names[0]
	return options, nil
}

// sessionTarget resolves which session a space command reads or writes.
// Without --session it is the ambient session, the one this process already
// talks to.
func sessionTarget(ctx context.Context, d deps, raw, usage string) (herdrmock.Target, error) {
	if raw == "" {
		return herdrmock.Target{}, nil
	}
	name, err := session.Sanitize(raw)
	if err != nil {
		return herdrmock.Target{}, usageError{reason: err.Error(), usage: usage}
	}

	sessions, err := d.manager(false).List(ctx)
	if err != nil {
		return herdrmock.Target{}, err
	}
	for _, candidate := range sessions {
		if candidate.Name != name {
			continue
		}
		if !candidate.Running {
			return herdrmock.Target{}, session.Refusal{Reason: fmt.Sprintf(
				"session '%s' is not running; start it with hs attach %s", name, name)}
		}
		return herdrmock.Target{Name: candidate.Name, Socket: candidate.Socket}, nil
	}
	return herdrmock.Target{}, session.Refusal{Reason: fmt.Sprintf("unknown session '%s'", name)}
}

// activeWorkspace is the workspace herdr reports as focused: the one the user is
// looking at. A session that reports no focused workspace at all falls back to
// its only workspace.
func activeWorkspace(space herdrmock.Space) (herdrmock.Workspace, bool) {
	for _, workspace := range space.Workspaces {
		if workspace.ID == space.FocusedWorkspaceID {
			return workspace, true
		}
	}
	if space.FocusedWorkspaceID == "" && len(space.Workspaces) == 1 {
		return space.Workspaces[0], true
	}
	return herdrmock.Workspace{}, false
}

func describeTarget(target herdrmock.Target) string {
	if target.Name == "" {
		return "the current session"
	}
	return fmt.Sprintf("session '%s'", target.Name)
}
