package herdrcli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/WilberC/herdr-suite/internal/herdrmock"
)

// EnvSocketPath names the environment variable that points herdr at one
// session's API socket; herdr itself and the previous sessionizer use the same
// variable to read another session without attaching to it.
const EnvSocketPath = "HERDR_SOCKET_PATH"

// Snapshot reads a session's live topology with "herdr api snapshot", which
// answers with the workspaces, the tabs, the panes and one pane layout per tab
// in a single consistent read. target.Socket, when set, selects the session by
// pointing herdr at that session's socket.
func (c *Client) Snapshot(ctx context.Context, target herdrmock.Target) (herdrmock.Space, error) {
	out, err := c.run(ctx, target, "api", "snapshot")
	if err != nil {
		return herdrmock.Space{}, err
	}

	var document struct {
		Result *struct {
			Snapshot snapshotDocument `json:"snapshot"`
		} `json:"result"`
		Error *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(out, &document); err != nil {
		return herdrmock.Space{}, fmt.Errorf("parse herdr api snapshot: %w", err)
	}
	if document.Error != nil {
		return herdrmock.Space{}, fmt.Errorf("herdr api snapshot: %s: %s", document.Error.Code, document.Error.Message)
	}
	if document.Result == nil {
		return herdrmock.Space{}, errors.New("parse herdr api snapshot: the document has no result")
	}

	space, err := buildSpace(document.Result.Snapshot)
	if err != nil {
		return herdrmock.Space{}, fmt.Errorf("read herdr api snapshot: %w", err)
	}
	space.Session = target.Name
	return space, nil
}

// snapshotDocument mirrors the fields herdr reports; every optional field stays
// empty when herdr leaves it out.
type snapshotDocument struct {
	FocusedWorkspaceID string          `json:"focused_workspace_id"`
	FocusedTabID       string          `json:"focused_tab_id"`
	FocusedPaneID      string          `json:"focused_pane_id"`
	Workspaces         []workspaceInfo `json:"workspaces"`
	Tabs               []tabInfo       `json:"tabs"`
	Panes              []paneInfo      `json:"panes"`
	Agents             []agentInfo     `json:"agents"`
	Layouts            []tabLayout     `json:"layouts"`
}

type workspaceInfo struct {
	ID          string `json:"workspace_id"`
	Label       string `json:"label"`
	Focused     bool   `json:"focused"`
	ActiveTabID string `json:"active_tab_id"`
	AgentStatus string `json:"agent_status"`
	TabCount    int    `json:"tab_count"`
	PaneCount   int    `json:"pane_count"`
}

type tabInfo struct {
	ID          string `json:"tab_id"`
	WorkspaceID string `json:"workspace_id"`
	Label       string `json:"label"`
	Focused     bool   `json:"focused"`
}

type paneInfo struct {
	ID          string `json:"pane_id"`
	TabID       string `json:"tab_id"`
	WorkspaceID string `json:"workspace_id"`
	Label       string `json:"label"`
	Cwd         string `json:"cwd"`
	Agent       string `json:"agent"`
	AgentStatus string `json:"agent_status"`
}

type agentInfo struct {
	PaneID string `json:"pane_id"`
	Agent  string `json:"agent"`
	Name   string `json:"name"`
}

// tabLayout is one tab's pane layout: the pane rectangles and the splits that
// divide the tab area. herdr reports a split's rect as the whole area it
// divides, so the tree is assembled from those rects.
type tabLayout struct {
	WorkspaceID   string   `json:"workspace_id"`
	TabID         string   `json:"tab_id"`
	FocusedPaneID string   `json:"focused_pane_id"`
	Area          paneRect `json:"area"`
	Panes         []struct {
		PaneID  string   `json:"pane_id"`
		Focused bool     `json:"focused"`
		Rect    paneRect `json:"rect"`
	} `json:"panes"`
	Splits []splitInfo `json:"splits"`
}

type splitInfo struct {
	ID        string   `json:"id"`
	Direction string   `json:"direction"`
	Ratio     float64  `json:"ratio"`
	Rect      paneRect `json:"rect"`
}

// paneRect is a rectangle in the tab area.
type paneRect struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

func buildSpace(document snapshotDocument) (herdrmock.Space, error) {
	layouts := make(map[string]tabLayout, len(document.Layouts))
	for _, layout := range document.Layouts {
		layouts[layout.TabID] = layout
	}

	names := make(map[string]string, len(document.Agents))
	for _, agent := range document.Agents {
		if agent.Name != "" {
			names[agent.PaneID] = agent.Name
		}
	}

	panes := make(map[string]herdrmock.Pane, len(document.Panes))
	for _, pane := range document.Panes {
		panes[pane.ID] = herdrmock.Pane{
			ID:          pane.ID,
			WorkspaceID: pane.WorkspaceID,
			TabID:       pane.TabID,
			Label:       pane.Label,
			Cwd:         pane.Cwd,
			Agent:       pane.Agent,
			AgentName:   names[pane.ID],
			AgentState:  pane.AgentStatus,
		}
	}

	space := herdrmock.Space{
		FocusedWorkspaceID: document.FocusedWorkspaceID,
		FocusedTabID:       document.FocusedTabID,
		FocusedPaneID:      document.FocusedPaneID,
		Workspaces:         make([]herdrmock.Workspace, 0, len(document.Workspaces)),
	}

	for _, reported := range document.Workspaces {
		workspace := herdrmock.Workspace{
			ID:          reported.ID,
			Label:       reported.Label,
			Focused:     reported.Focused,
			ActiveTabID: reported.ActiveTabID,
			AgentState:  reported.AgentStatus,
			TabCount:    reported.TabCount,
			PaneCount:   reported.PaneCount,
		}
		for _, reportedTab := range document.Tabs {
			if reportedTab.WorkspaceID != reported.ID {
				continue
			}
			layout, ok := layouts[reportedTab.ID]
			if !ok {
				return herdrmock.Space{}, fmt.Errorf("tab %q has no layout", reportedTab.ID)
			}
			root, err := buildTree(layout)
			if err != nil {
				return herdrmock.Space{}, fmt.Errorf("tab %q: %w", reportedTab.ID, err)
			}
			workspace.Tabs = append(workspace.Tabs, herdrmock.Tab{
				ID:            reportedTab.ID,
				Label:         reportedTab.Label,
				Focused:       reportedTab.Focused,
				FocusedPaneID: layout.FocusedPaneID,
				Panes:         tabPanes(document.Panes, panes, reportedTab.ID),
				Root:          root,
			})
		}
		space.Workspaces = append(space.Workspaces, workspace)
	}
	return space, nil
}

// tabPanes returns the panes herdr reported for one tab, in document order.
func tabPanes(reported []paneInfo, panes map[string]herdrmock.Pane, tabID string) []herdrmock.Pane {
	var tabPanes []herdrmock.Pane
	for _, entry := range reported {
		if entry.TabID == tabID {
			tabPanes = append(tabPanes, panes[entry.ID])
		}
	}
	return tabPanes
}

// buildTree turns a tab's flat rect list into the split tree herdr drew. A
// split owns the area its rect covers, and its children are the closest entries
// inside that area, ordered left to right for a "right" split and top to bottom
// for a "down" one.
func buildTree(layout tabLayout) (herdrmock.LayoutNode, error) {
	var assemble func(area paneRect) (herdrmock.LayoutNode, error)
	assemble = func(area paneRect) (herdrmock.LayoutNode, error) {
		for _, split := range layout.Splits {
			if split.Rect != area {
				continue
			}
			children := childrenOf(layout, area, split.Direction)
			if len(children) != 2 {
				return herdrmock.LayoutNode{}, fmt.Errorf(
					"split %q covers %v and has %d children, want 2", split.ID, area, len(children))
			}
			first, err := assemble(children[0])
			if err != nil {
				return herdrmock.LayoutNode{}, err
			}
			second, err := assemble(children[1])
			if err != nil {
				return herdrmock.LayoutNode{}, err
			}
			return herdrmock.LayoutNode{
				Direction: split.Direction,
				Ratio:     split.Ratio,
				Children:  []herdrmock.LayoutNode{first, second},
			}, nil
		}

		for _, pane := range layout.Panes {
			if pane.Rect == area {
				return herdrmock.LayoutNode{PaneID: pane.PaneID}, nil
			}
		}
		return herdrmock.LayoutNode{}, fmt.Errorf("no pane or split covers %v", area)
	}

	return assemble(layout.Area)
}

// childrenOf returns the closest entries inside area, ordered along the split's
// axis: entries deeper in the tree are inside one of them, so the two that no
// other entry contains are this split's children.
func childrenOf(layout tabLayout, area paneRect, direction string) []paneRect {
	inside := make([]paneRect, 0, len(layout.Panes)+len(layout.Splits))
	for _, pane := range layout.Panes {
		if strictlyInside(pane.Rect, area) {
			inside = append(inside, pane.Rect)
		}
	}
	for _, split := range layout.Splits {
		if strictlyInside(split.Rect, area) {
			inside = append(inside, split.Rect)
		}
	}

	closest := make([]paneRect, 0, len(inside))
	for _, candidate := range inside {
		nested := false
		for _, other := range inside {
			if other != candidate && strictlyInside(candidate, other) {
				nested = true
				break
			}
		}
		if !nested {
			closest = append(closest, candidate)
		}
	}

	sort.SliceStable(closest, func(i, j int) bool {
		if direction == "down" {
			if closest[i].Y != closest[j].Y {
				return closest[i].Y < closest[j].Y
			}
			return closest[i].X < closest[j].X
		}
		if closest[i].X != closest[j].X {
			return closest[i].X < closest[j].X
		}
		return closest[i].Y < closest[j].Y
	})
	return closest
}

// strictlyInside reports whether inner sits within outer and is smaller than it.
func strictlyInside(inner, outer paneRect) bool {
	if inner == outer {
		return false
	}
	return inner.X >= outer.X && inner.Y >= outer.Y &&
		inner.X+inner.Width <= outer.X+outer.Width &&
		inner.Y+inner.Height <= outer.Y+outer.Height
}
