package layout

import (
	"fmt"
	"strconv"
	"strings"
)

// Summary renders the stable report a stored layout prints: where it was saved,
// what it holds, and one ASCII tree per tab with the split directions and
// ratios. Labels, agent identifications and cwds hang off each pane leaf. A
// session layout prints a line for the session before its workspaces.
func Summary(document Layout, path string) string {
	var report strings.Builder
	fmt.Fprintf(&report, "saved layout '%s' at %s\n", document.Name, path)

	spaces := document.Spaces()
	if document.Multi() {
		fmt.Fprintf(&report, "%s\n", sessionLine(document, spaces))
	}
	for _, workspace := range spaces {
		writeWorkspace(&report, workspace)
	}
	return report.String()
}

// sessionLine is the header of a session layout: the session it was forked from
// when that is known, then the totals of every stored workspace.
func sessionLine(document Layout, spaces []Workspace) string {
	tabs, panes := 0, 0
	for _, workspace := range spaces {
		tabs += len(workspace.Tabs)
		for _, tab := range workspace.Tabs {
			panes += countPanes(tab.Root)
		}
	}
	parts := []string{plural(len(spaces), "workspace"), plural(tabs, "tab"), plural(panes, "pane")}
	if document.Session != "" {
		parts = append([]string{fmt.Sprintf("session '%s'", document.Session)}, parts...)
	}
	return strings.Join(parts, " · ")
}

// writeWorkspace prints one workspace and the tree of each of its tabs.
func writeWorkspace(report *strings.Builder, workspace Workspace) {
	tabs := workspace.Tabs
	total := 0
	for _, tab := range tabs {
		total += countPanes(tab.Root)
	}
	fmt.Fprintf(report, "workspace '%s' · %s · %s\n",
		workspace.Label, plural(len(tabs), "tab"), plural(total, "pane"))

	for index, tab := range tabs {
		fmt.Fprintf(report, "tab %d", index+1)
		if tab.Label != "" {
			fmt.Fprintf(report, " '%s'", tab.Label)
		}
		line := []string{plural(countPanes(tab.Root), "pane")}
		if tab.FocusedPaneID != "" {
			line = append(line, "focus "+tab.FocusedPaneID)
		}
		fmt.Fprintf(report, " · %s\n", strings.Join(line, " · "))
		writeNode(report, tab.Root, "", "")
	}
}

// ReplaySummary renders the stable report a completed replay prints: the layout
// it recreated, the workspace it built in, and what the workspace now holds.
func ReplaySummary(replay Replay) string {
	return fmt.Sprintf("loaded layout '%s' into workspace '%s' · %s · %s · %s\n",
		replay.Layout, replay.Workspace,
		plural(replay.Tabs, "tab"), plural(replay.Panes, "pane"), plural(len(replay.Actions), "step"))
}

// SessionReplaySummary renders the stable report a completed session replay
// prints: a total line, then what every recreated workspace holds.
func SessionReplaySummary(plan SessionPlan) string {
	var report strings.Builder
	fmt.Fprintf(&report, "loaded layout '%s' into %s · %s · %s · %s\n",
		plan.Layout, plural(len(plan.Workspaces), "workspace"),
		plural(plan.Tabs, "tab"), plural(plan.Panes, "pane"), plural(plan.Steps, "step"))
	for _, replay := range plan.Workspaces {
		fmt.Fprintf(&report, "workspace '%s' · %s · %s · %s\n", replay.Workspace,
			plural(replay.Tabs, "tab"), plural(replay.Panes, "pane"), plural(len(replay.Actions), "step"))
	}
	return report.String()
}

// writeNode renders one tree node and its subtree. branch is the connector the
// caller already decided on, and prefix carries the indentation of the levels
// above it.
func writeNode(report *strings.Builder, node Node, prefix, branch string) {
	report.WriteString(prefix + branch + nodeLine(node))
	report.WriteString("\n")
	if node.Type != TypeSplit {
		return
	}

	childPrefix := prefix
	switch branch {
	case "├─ ":
		childPrefix += "│  "
	case "└─ ":
		childPrefix += "   "
	}
	if node.First != nil {
		writeNode(report, *node.First, childPrefix, "├─ ")
	}
	if node.Second != nil {
		writeNode(report, *node.Second, childPrefix, "└─ ")
	}
}

func nodeLine(node Node) string {
	if node.Type == TypeSplit {
		return "split " + node.Direction + " " + strconv.FormatFloat(node.Ratio, 'f', 2, 64)
	}
	return joinParts(
		"pane "+node.PaneID,
		quoted(node.Label),
		agentLine(node.Agent),
		node.Cwd,
	)
}

func quoted(label string) string {
	if label == "" {
		return ""
	}
	return "'" + label + "'"
}

func agentLine(agent *Agent) string {
	if agent == nil {
		return ""
	}
	return joinSpace(agent.Kind, quoted(agent.Name), agent.State)
}

func joinSpace(parts ...string) string {
	kept := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			kept = append(kept, part)
		}
	}
	return strings.Join(kept, " ")
}
