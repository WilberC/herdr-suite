package layout

import (
	"fmt"
	"strconv"
	"strings"
)

// Summary renders the stable report a stored layout prints: where it was saved,
// what it holds, and one ASCII tree per tab with the split directions and
// ratios. Labels, agent identifications and cwds hang off each pane leaf.
func Summary(document Layout, path string) string {
	var report strings.Builder
	fmt.Fprintf(&report, "saved layout '%s' at %s\n", document.Name, path)

	tabs := document.Workspace.Tabs
	total := 0
	for _, tab := range tabs {
		total += countPanes(tab.Root)
	}
	fmt.Fprintf(&report, "workspace '%s' · %s · %s\n",
		document.Workspace.Label, plural(len(tabs), "tab"), plural(total, "pane"))

	for index, tab := range tabs {
		fmt.Fprintf(&report, "tab %d", index+1)
		if tab.Label != "" {
			fmt.Fprintf(&report, " '%s'", tab.Label)
		}
		line := []string{plural(countPanes(tab.Root), "pane")}
		if tab.FocusedPaneID != "" {
			line = append(line, "focus "+tab.FocusedPaneID)
		}
		fmt.Fprintf(&report, " · %s\n", strings.Join(line, " · "))
		writeNode(&report, tab.Root, "", "")
	}
	return report.String()
}

// ReplaySummary renders the stable report a completed replay prints: the layout
// it recreated, the workspace it built in, and what the workspace now holds.
func ReplaySummary(replay Replay) string {
	return fmt.Sprintf("loaded layout '%s' into workspace '%s' · %s · %s · %s\n",
		replay.Layout, replay.Workspace,
		plural(replay.Tabs, "tab"), plural(replay.Panes, "pane"), plural(len(replay.Actions), "step"))
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
