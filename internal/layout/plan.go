package layout

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/WilberC/herdr-suite/internal/herdrmock"
)

// Replay operations. One Action performs exactly one herdr mutation.
const (
	OpCreateWorkspace = "create-workspace"
	OpCreateTab       = "create-tab"
	OpCloseTab        = "close-tab"
	OpClosePane       = "close-pane"
	OpSplit           = "split"
	OpRenameTab       = "rename-tab"
	OpRenamePane      = "rename-pane"
	OpStartAgent      = "start-agent"
	OpFocusWorkspace  = "focus-workspace"
	OpFocusTab        = "focus-tab"
	OpFocusAgent      = "focus-agent"
)

// Target is the live destination a layout replays into.
type Target struct {
	// Workspace is where the replay builds. An empty workspace ID asks the
	// replay to create a new workspace from the stored one.
	Workspace herdrmock.Workspace
	// Replace closes the destination's other panes and tabs before the replay,
	// so the stored tree is rebuilt instead of laid over what was there.
	Replace bool
	// Keep is the pane the replay must not close, normally the pane hs runs in.
	// When it belongs to the destination workspace it becomes the base pane.
	Keep string
	// Agents are the live agent names the replay must not reuse.
	Agents []string
}

// Action is one mutation of a replay. Pane and From are stored pane ids used as
// handles: Pane is the leaf the action creates, renames or launches, and From is
// the leaf whose live pane the action splits. PaneID, TabID and WorkspaceID are
// live ids, known when the plan closes or focuses something that already exists.
type Action struct {
	Op string

	Pane string
	From string
	Tab  int

	TabLabel    string
	Label       string
	Cwd         string
	Direction   string
	Ratio       float64
	AgentKind   string
	AgentName   string
	Focus       bool
	PaneID      string
	TabID       string
	WorkspaceID string
}

// Base is the live starting point a replay reuses: the workspace it builds in
// and, when the destination is not empty, the tab and pane the first stored tab
// grows from.
type Base struct {
	WorkspaceID string
	TabID       string
	PaneID      string
	Leaf        string
}

// Replay is the ordered plan that recreates one layout.
type Replay struct {
	Layout    string
	Workspace string
	Tabs      int
	Panes     int
	Base      Base
	Actions   []Action
}

// Plan builds the ordered replay of document into target. The order keeps every
// split before the splits that subdivide its children and creates each pane
// exactly once, so a replay that stops halfway leaves a tree that can be read
// from the step report. Plan never touches herdr.
func Plan(document Layout, target Target) (Replay, error) {
	tabs := document.Workspace.Tabs
	if len(tabs) == 0 {
		return Replay{}, fmt.Errorf("layout '%s' holds no tabs", document.Name)
	}
	leaves, err := collectLeaves(document)
	if err != nil {
		return Replay{}, err
	}

	replay := Replay{
		Layout:    document.Name,
		Workspace: document.Workspace.Label,
		Tabs:      len(tabs),
		Panes:     len(leaves),
	}
	names := nameAgents(document, leaves, takenNames(target.Agents))
	if err := planWorkspace(&replay, document, target, leaves); err != nil {
		return Replay{}, err
	}
	if err := planTabs(&replay, document, leaves, names); err != nil {
		return Replay{}, err
	}
	planFocus(&replay, document, target, names)
	return replay, nil
}

// planWorkspace records how the replay joins its destination: the workspace it
// creates, the existing panes it closes, and the base it reuses.
func planWorkspace(replay *Replay, document Layout, target Target, leaves []leafInfo) error {
	tabs := document.Workspace.Tabs
	if target.Workspace.ID == "" {
		first := leaves[0]
		replay.Actions = append(replay.Actions, Action{
			Op:    OpCreateWorkspace,
			Tab:   1,
			Pane:  first.Pane,
			Label: document.Workspace.Label,
			Cwd:   first.Cwd,
		})
		if label := tabs[0].Label; label != "" {
			replay.Actions = append(replay.Actions, Action{Op: OpRenameTab, Tab: 1, Label: label})
		}
		return nil
	}

	tab, ok := baseTab(target.Workspace, target.Keep)
	if !ok {
		return fmt.Errorf("workspace '%s' holds no tabs", target.Workspace.Label)
	}
	pane, ok := basePane(tab, target.Keep)
	if !ok {
		return fmt.Errorf("workspace '%s' has a tab without panes", target.Workspace.Label)
	}
	replay.Base = Base{
		WorkspaceID: target.Workspace.ID,
		TabID:       tab.ID,
		PaneID:      pane,
		Leaf:        leftmost(tabs[0].Root),
	}

	if target.Replace {
		for _, other := range tab.Panes {
			if other.ID == pane {
				continue
			}
			replay.Actions = append(replay.Actions, Action{Op: OpClosePane, PaneID: other.ID})
		}
		for _, other := range target.Workspace.Tabs {
			if other.ID == tab.ID {
				continue
			}
			replay.Actions = append(replay.Actions, Action{Op: OpCloseTab, TabID: other.ID})
		}
	}
	if label := tabs[0].Label; label != "" && label != tab.Label {
		replay.Actions = append(replay.Actions, Action{Op: OpRenameTab, Tab: 1, Label: label})
	}
	return nil
}

// planTabs records the creation, splits, labels and agents of every stored tab,
// tab by tab and, inside a tab, parents before children.
func planTabs(replay *Replay, document Layout, leaves []leafInfo, names map[string]string) error {
	cwds := make(map[string]string, len(leaves))
	for _, leaf := range leaves {
		cwds[leaf.Pane] = leaf.Cwd
	}

	for index, tab := range document.Workspace.Tabs {
		tabIndex := index + 1
		area := leftmost(tab.Root)
		if tabIndex > 1 {
			replay.Actions = append(replay.Actions, Action{
				Op:       OpCreateTab,
				Tab:      tabIndex,
				TabLabel: tab.Label,
				Pane:     area,
				Cwd:      cwds[area],
			})
		}
		focused := ""
		if tab.Focused {
			focused = tab.FocusedPaneID
		}
		if err := planSplits(replay, tab.Root, area, tabIndex, cwds, focused); err != nil {
			return fmt.Errorf("tab %d: %w", tabIndex, err)
		}
		for _, leaf := range leaves {
			if leaf.Tab != tabIndex {
				continue
			}
			if leaf.Label != "" {
				replay.Actions = append(replay.Actions, Action{Op: OpRenamePane, Tab: tabIndex, Pane: leaf.Pane, Label: leaf.Label})
			}
			if leaf.Agent == nil {
				continue
			}
			replay.Actions = append(replay.Actions, Action{
				Op:        OpStartAgent,
				Tab:       tabIndex,
				Pane:      leaf.Pane,
				AgentKind: leaf.Agent.Kind,
				AgentName: names[leaf.Pane],
			})
		}
	}
	return nil
}

// planSplits records the split tree depth first: a split is replayed before the
// splits inside either of its children, and the pane it creates is the leftmost
// leaf of its second child.
func planSplits(replay *Replay, node Node, area string, tabIndex int, cwds map[string]string, focused string) error {
	if node.Type != TypeSplit {
		return nil
	}
	created := leftmost(*node.Second)
	replay.Actions = append(replay.Actions, Action{
		Op:        OpSplit,
		Tab:       tabIndex,
		From:      area,
		Pane:      created,
		Direction: node.Direction,
		Ratio:     node.Ratio,
		Cwd:       cwds[created],
		Focus:     created == focused,
	})
	if err := planSplits(replay, *node.First, area, tabIndex, cwds, focused); err != nil {
		return err
	}
	return planSplits(replay, *node.Second, created, tabIndex, cwds, focused)
}

// planFocus records the focus restoration last: the destination workspace, then
// the stored focused tab, then the agent the stored focused pane runs.
func planFocus(replay *Replay, document Layout, target Target, names map[string]string) {
	if target.Workspace.ID == "" || !target.Workspace.Focused {
		replay.Actions = append(replay.Actions, Action{
			Op:          OpFocusWorkspace,
			WorkspaceID: target.Workspace.ID,
			Label:       document.Workspace.Label,
		})
	}

	index := 1
	tab := document.Workspace.Tabs[0]
	for candidate, entry := range document.Workspace.Tabs {
		if entry.Focused {
			index, tab = candidate+1, entry
			break
		}
	}
	replay.Actions = append(replay.Actions, Action{Op: OpFocusTab, Tab: index, TabLabel: tab.Label})

	if name, ok := names[tab.FocusedPaneID]; ok && name != "" {
		replay.Actions = append(replay.Actions, Action{Op: OpFocusAgent, AgentName: name})
	}
}

// RenderPlan prints the ordered replay as the stable text --dry-run shows.
func RenderPlan(replay Replay) string {
	var out strings.Builder
	fmt.Fprintf(&out, "plan for layout '%s' into workspace '%s' · %s · %s\n",
		replay.Layout, replay.Workspace, plural(replay.Tabs, "tab"), plural(replay.Panes, "pane"))
	for index, action := range replay.Actions {
		fmt.Fprintf(&out, "%d. %s\n", index+1, action.describe())
	}
	return out.String()
}

// describe renders one action as the one line the plan prints and the step
// report repeats when it fails.
func (a Action) describe() string {
	switch a.Op {
	case OpCreateWorkspace:
		return "create workspace " + quoted(a.Label) + cwdSuffix(a.Cwd)
	case OpCreateTab:
		return "create tab " + strconv.Itoa(a.Tab) + quotedPart(a.TabLabel) + cwdSuffix(a.Cwd)
	case OpClosePane:
		return "close pane " + a.PaneID
	case OpCloseTab:
		return "close tab " + a.TabID
	case OpRenameTab:
		return "rename tab " + strconv.Itoa(a.Tab) + quotedPart(a.Label)
	case OpRenamePane:
		return "rename " + a.Pane + quotedPart(a.Label)
	case OpSplit:
		return "split " + a.Direction + ratioPart(a.Ratio) + " in " + a.From + " · new pane " + a.Pane + cwdSuffix(a.Cwd)
	case OpStartAgent:
		return "start agent " + a.AgentKind + " in " + a.Pane + " as " + a.AgentName
	case OpFocusWorkspace:
		return "focus workspace " + quoted(a.Label)
	case OpFocusTab:
		return "focus tab " + strconv.Itoa(a.Tab) + quotedPart(a.TabLabel)
	case OpFocusAgent:
		return "focus agent " + a.AgentName
	}
	return a.Op
}

func ratioPart(ratio float64) string {
	if ratio <= 0 {
		return ""
	}
	return " " + strconv.FormatFloat(ratio, 'f', 2, 64)
}

func quotedPart(value string) string {
	if value == "" {
		return ""
	}
	return " " + quoted(value)
}

func cwdSuffix(cwd string) string {
	if cwd == "" {
		return ""
	}
	return " · cwd " + cwd
}

// leafInfo is one pane leaf of a stored layout, in document order.
type leafInfo struct {
	Pane  string
	Tab   int
	Cwd   string
	Label string
	Agent *Agent
}

// collectLeaves walks the stored tabs and returns every pane leaf, rejecting a
// document a replay could not reproduce.
func collectLeaves(document Layout) ([]leafInfo, error) {
	var leaves []leafInfo
	seen := make(map[string]bool)
	for index, tab := range document.Workspace.Tabs {
		tabIndex := index + 1
		if err := walkLeaves(tab.Root, tabIndex, &leaves, seen); err != nil {
			return nil, fmt.Errorf("tab %d: %w", tabIndex, err)
		}
	}
	if len(leaves) == 0 {
		return nil, fmt.Errorf("layout '%s' holds no panes", document.Name)
	}
	return leaves, nil
}

func walkLeaves(node Node, tabIndex int, leaves *[]leafInfo, seen map[string]bool) error {
	switch node.Type {
	case TypePane:
		if node.PaneID == "" {
			return fmt.Errorf("a pane leaf has no pane id")
		}
		if seen[node.PaneID] {
			return fmt.Errorf("pane '%s' appears twice", node.PaneID)
		}
		agent := node.Agent
		if agent != nil && agent.Kind == "" {
			agent = nil
		}
		seen[node.PaneID] = true
		*leaves = append(*leaves, leafInfo{
			Pane:  node.PaneID,
			Tab:   tabIndex,
			Cwd:   node.Cwd,
			Label: node.Label,
			Agent: agent,
		})
		return nil
	case TypeSplit:
		if node.First == nil || node.Second == nil || node.First.Type == "" || node.Second.Type == "" {
			return fmt.Errorf("split %s needs two children", node.Direction)
		}
		if node.Direction != "right" && node.Direction != "down" {
			return fmt.Errorf("split direction '%s' is neither right nor down", node.Direction)
		}
		if node.Ratio < 0 || node.Ratio >= 1 {
			return fmt.Errorf("split ratio %v is outside [0,1)", node.Ratio)
		}
		if err := walkLeaves(*node.First, tabIndex, leaves, seen); err != nil {
			return err
		}
		return walkLeaves(*node.Second, tabIndex, leaves, seen)
	}
	return fmt.Errorf("node type '%s' is neither pane nor split", node.Type)
}

// leftmost is the stored pane id whose area a split tree occupies: the leaf a
// split's area starts from, and the pane a creation call opens.
func leftmost(node Node) string {
	for node.Type == TypeSplit && node.First != nil {
		node = *node.First
	}
	return node.PaneID
}

// baseTab is the destination tab a replay grows from: the tab holding the pane
// the caller runs in, otherwise the workspace's active tab.
func baseTab(workspace herdrmock.Workspace, keep string) (herdrmock.Tab, bool) {
	if keep != "" {
		for _, tab := range workspace.Tabs {
			for _, pane := range tab.Panes {
				if pane.ID == keep {
					return tab, true
				}
			}
		}
	}
	for _, tab := range workspace.Tabs {
		if tab.ID == workspace.ActiveTabID {
			return tab, true
		}
	}
	if len(workspace.Tabs) == 0 {
		return herdrmock.Tab{}, false
	}
	return workspace.Tabs[0], true
}

// basePane is the pane a replay keeps and splits in its base tab: the caller's
// pane when it lives there, otherwise the tab's first pane.
func basePane(tab herdrmock.Tab, keep string) (string, bool) {
	for _, pane := range tab.Panes {
		if pane.ID == keep {
			return pane.ID, true
		}
	}
	if len(tab.Panes) == 0 {
		return "", false
	}
	return tab.Panes[0].ID, true
}

// takenNames is the set of live agent names a replay must not reuse.
func takenNames(names []string) map[string]bool {
	taken := make(map[string]bool, len(names))
	for _, name := range names {
		taken[strings.ToLower(name)] = true
	}
	return taken
}

// nameAgents picks the agent name every agent leaf starts under. herdr requires
// lowercase names that start with a letter, so a stored name is sanitized, a
// leaf without one falls back to its kind and then to the layout name, and a
// collision with a live agent or an earlier leaf gains a numeric suffix.
func nameAgents(document Layout, leaves []leafInfo, taken map[string]bool) map[string]string {
	names := make(map[string]string, len(leaves))
	for index, leaf := range leaves {
		if leaf.Agent == nil {
			continue
		}
		want := leaf.Agent.Name
		if want == "" {
			want = leaf.Agent.Kind
		}
		if folded := agentSlug(want); folded == "" {
			want = "hs-" + document.Name + "-" + strconv.Itoa(index+1)
		}
		names[leaf.Pane] = uniqueName(agentSlug(want), taken)
	}
	return names
}

// uniqueName fits base into herdr's agent name rule and makes it unique.
func uniqueName(base string, taken map[string]bool) string {
	const limit = 32
	candidate := truncateName(base, limit)
	for suffix := 2; taken[candidate]; suffix++ {
		tail := "-" + strconv.Itoa(suffix)
		candidate = truncateName(base, limit-len(tail)) + tail
	}
	taken[candidate] = true
	return candidate
}

// agentSlug folds a label into the characters herdr accepts in an agent name.
func agentSlug(value string) string {
	var slug strings.Builder
	for _, r := range strings.ToLower(value) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
			slug.WriteRune(r)
		default:
			slug.WriteRune('-')
		}
	}
	trimmed := strings.Trim(slug.String(), "-_")
	if trimmed == "" {
		return ""
	}
	if trimmed[0] < 'a' || trimmed[0] > 'z' {
		return "hs-" + trimmed
	}
	return trimmed
}

func truncateName(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return strings.Trim(value[:limit], "-_")
}
