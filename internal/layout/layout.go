package layout

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/WilberC/herdr-suite/internal/herdrmock"
)

// Node types a split tree entry can hold.
const (
	TypePane  = "pane"
	TypeSplit = "split"
)

// Layout is one saved space: the workspace that was active when it was
// snapshotted, with every tab's split tree.
type Layout struct {
	Name      string    `json:"name"`
	Session   string    `json:"session,omitempty"`
	Workspace Workspace `json:"workspace"`
}

// Workspace is the snapshotted workspace.
type Workspace struct {
	Label string `json:"label"`
	Tabs  []Tab  `json:"tabs"`
}

// Tab is one tab of the workspace with its split tree.
type Tab struct {
	Label         string `json:"label,omitempty"`
	Focused       bool   `json:"focused"`
	FocusedPaneID string `json:"focused_pane_id,omitempty"`
	Root          Node   `json:"root"`
}

// Node is one node of a tab's split tree: a pane leaf, or a binary split with
// an ordered pair of children.
type Node struct {
	Type string `json:"type"`

	PaneID string `json:"pane_id,omitempty"`
	Label  string `json:"label,omitempty"`
	Cwd    string `json:"cwd,omitempty"`
	Agent  *Agent `json:"agent,omitempty"`

	Direction string  `json:"direction,omitempty"`
	Ratio     float64 `json:"ratio,omitempty"`
	First     *Node   `json:"first,omitempty"`
	Second    *Node   `json:"second,omitempty"`
}

// Agent is the agent identification herdr reports for a pane.
type Agent struct {
	Kind  string `json:"kind"`
	Name  string `json:"name,omitempty"`
	State string `json:"state,omitempty"`
}

// Capture turns a live workspace into a layout named name. Space supplies the
// session the topology was read from; it is empty when the read went to the
// ambient session and is then left out of the document.
func Capture(name string, space herdrmock.Space, workspace herdrmock.Workspace) (Layout, error) {
	document := Layout{
		Name:    name,
		Session: space.Session,
		Workspace: Workspace{
			Label: workspace.Label,
			Tabs:  make([]Tab, 0, len(workspace.Tabs)),
		},
	}

	for _, tab := range workspace.Tabs {
		root, err := captureNode(tab, tab.Root)
		if err != nil {
			return Layout{}, err
		}
		document.Workspace.Tabs = append(document.Workspace.Tabs, Tab{
			Label:         tab.Label,
			Focused:       tab.Focused,
			FocusedPaneID: tab.FocusedPaneID,
			Root:          root,
		})
	}
	return document, nil
}

func captureNode(tab herdrmock.Tab, node herdrmock.LayoutNode) (Node, error) {
	if len(node.Children) == 0 {
		pane, ok := paneByID(tab.Panes, node.PaneID)
		if !ok {
			return Node{}, fmt.Errorf("tab '%s': pane '%s' is missing from the topology", tabName(tab), node.PaneID)
		}
		captured := Node{
			Type:   TypePane,
			PaneID: pane.ID,
			Label:  pane.Label,
			Cwd:    pane.Cwd,
		}
		if pane.Agent != "" || pane.AgentName != "" || pane.AgentState != "" {
			captured.Agent = &Agent{Kind: pane.Agent, Name: pane.AgentName, State: pane.AgentState}
		}
		return captured, nil
	}

	if len(node.Children) != 2 {
		return Node{}, fmt.Errorf("tab '%s': split node has %d children, want 2", tabName(tab), len(node.Children))
	}
	first, err := captureNode(tab, node.Children[0])
	if err != nil {
		return Node{}, err
	}
	second, err := captureNode(tab, node.Children[1])
	if err != nil {
		return Node{}, err
	}
	return Node{
		Type:      TypeSplit,
		Direction: node.Direction,
		Ratio:     node.Ratio,
		First:     &first,
		Second:    &second,
	}, nil
}

func paneByID(panes []herdrmock.Pane, paneID string) (herdrmock.Pane, bool) {
	for _, pane := range panes {
		if pane.ID == paneID {
			return pane, true
		}
	}
	return herdrmock.Pane{}, false
}

func tabName(tab herdrmock.Tab) string {
	if tab.Label != "" {
		return tab.Label
	}
	return tab.ID
}

// Marshal encodes a layout as the document that is stored and printed.
func Marshal(document Layout) ([]byte, error) {
	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode layout: %w", err)
	}
	return append(data, '\n'), nil
}

// Unmarshal decodes a stored layout document.
func Unmarshal(data []byte) (Layout, error) {
	var document Layout
	if err := json.Unmarshal(data, &document); err != nil {
		return Layout{}, fmt.Errorf("decode layout: %w", err)
	}
	return document, nil
}

// Write prints a layout as the document scripts consume.
func Write(w io.Writer, document Layout) error {
	data, err := Marshal(document)
	if err != nil {
		return err
	}
	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("write layout: %w", err)
	}
	return nil
}

// countPanes counts the pane leaves of a split tree.
func countPanes(node Node) int {
	if node.Type == TypePane {
		return 1
	}
	if node.Type != TypeSplit {
		return 0
	}
	count := 0
	if node.First != nil {
		count += countPanes(*node.First)
	}
	if node.Second != nil {
		count += countPanes(*node.Second)
	}
	return count
}

// plural renders a count with its noun, singular at one.
func plural(count int, noun string) string {
	if count == 1 {
		return fmt.Sprintf("%d %s", count, noun)
	}
	return fmt.Sprintf("%d %ss", count, noun)
}

func joinParts(parts ...string) string {
	kept := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			kept = append(kept, part)
		}
	}
	return strings.Join(kept, " · ")
}

var (
	errNoConfigHome = errors.New("neither XDG_CONFIG_HOME nor HOME is set")
)
