package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/WilberC/herdr-suite/internal/herdrmock"
	"github.com/WilberC/herdr-suite/internal/last"
)

const lastUsage = `usage: hs last <command>

Commands:
  workspace  Toggle back to the previous workspace
  tab        Toggle back to the previous tab
  observe    Record a focus event, run by the plugin's event hooks

The plugin hooks record one history per herdr session, so both toggles answer
from what this session saw. Neither is bound by default: bind
herdr-suite.last-workspace and herdr-suite.last-tab when you want them.
`

const lastWorkspaceUsage = `usage: hs last workspace

Focus the workspace this session was on before the current one, then swap the
recorded pair so the same command leads back. Exit status 2 with a refusal when
this session has no earlier workspace recorded.
`

const lastTabUsage = `usage: hs last tab

Focus the tab this session was on before the current one, then swap the recorded
pair so the same command leads back. Exit status 2 with a refusal when this
workspace has no earlier tab recorded.
`

const lastObserveUsage = `usage: hs last observe

Record the focus event herdr hands a plugin hook: $HERDR_PLUGIN_EVENT names the
kind (workspace.focused or tab.focused) and $HERDR_PLUGIN_EVENT_JSON carries the
focused id in its data object. The history is one file per herdr socket, written
atomically under the plugin state directory.

This runs once per focus event from the plugin's event hooks, so it always exits
0 and prints nothing: an unknown kind, a missing or malformed payload, or a
state file that cannot be written never fails the event.
`

func runLast(d deps, args []string) int {
	if len(args) == 0 {
		fmt.Fprint(d.err, lastUsage)
		return 2
	}

	switch args[0] {
	case "workspace":
		return runLastToggle(d, args[1:], lastWorkspace)
	case "tab":
		return runLastToggle(d, args[1:], lastTab)
	case "observe":
		return runLastObserve(d, args[1:])
	}

	fmt.Fprintf(d.err, "unknown last command %q\n\n%s", args[0], lastUsage)
	return 2
}

type lastTarget int

const (
	lastWorkspace lastTarget = iota + 1
	lastTab
)

func runLastToggle(d deps, args []string, target lastTarget) int {
	name, usage := "workspace", lastWorkspaceUsage
	if target == lastTab {
		name, usage = "tab", lastTabUsage
	}
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			return report(usageError{reason: fmt.Sprintf("unknown flag %q", arg), usage: usage}, d.err)
		}
		return report(usageError{reason: fmt.Sprintf("%s takes no arguments", name), usage: usage}, d.err)
	}

	ctx := context.Background()
	mutator := d.writer.Mutations(herdrmock.Target{})
	if target == lastTab {
		return report(last.ToggleTab(ctx, mutator, d.getenv), d.err)
	}
	return report(last.ToggleWorkspace(ctx, mutator, d.getenv), d.err)
}

func runLastObserve(d deps, args []string) int {
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			return report(usageError{reason: fmt.Sprintf("unknown flag %q", arg), usage: lastObserveUsage}, d.err)
		}
		return report(usageError{reason: "observe takes no arguments", usage: lastObserveUsage}, d.err)
	}

	event := d.getenv("HERDR_PLUGIN_EVENT")
	payload := d.getenv("HERDR_PLUGIN_EVENT_JSON")
	last.Observe(d.getenv, event, payload)
	return 0
}
