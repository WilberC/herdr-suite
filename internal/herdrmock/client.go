package herdrmock

import "context"

// Session is a named herdr session as reported by "herdr session list".
type Session struct {
	Name    string
	Running bool
	Default bool
	Socket  string
}

// Pane is one terminal pane inside a tab. Agent and AgentState are what herdr
// reports about the agent running in the pane, so they are empty for a plain
// shell.
type Pane struct {
	ID          string
	WorkspaceID string
	TabID       string
	Label       string
	Cwd         string
	Agent       string
	AgentName   string
	AgentState  string
}

// LayoutNode is one node of a tab's split tree. A leaf names a pane; a split
// carries a direction, a ratio and exactly two children.
type LayoutNode struct {
	PaneID    string
	Direction string
	Ratio     float64
	Children  []LayoutNode
}

// Tab is one herdr tab with its panes and their split tree.
type Tab struct {
	ID            string
	Label         string
	Focused       bool
	FocusedPaneID string
	Panes         []Pane
	Root          LayoutNode
}

// Workspace is a herdr workspace with the tabs it holds.
type Workspace struct {
	ID          string
	Label       string
	Focused     bool
	ActiveTabID string
	AgentState  string
	TabCount    int
	PaneCount   int
	Tabs        []Tab
}

// Space is a session's live topology: its workspaces and, per workspace, the
// tabs with their pane split trees. Session names the session the topology was
// read from, and is empty when the read went to the ambient session.
type Space struct {
	Session            string
	FocusedWorkspaceID string
	FocusedTabID       string
	FocusedPaneID      string
	Workspaces         []Workspace
}

// Target selects the herdr session a topology read talks to: Socket when it is
// known, or the session this process already talks to when both fields are
// empty.
type Target struct {
	Name   string
	Socket string
}

// SplitRequest describes "herdr pane split" arguments.
type SplitRequest struct {
	PaneID    string
	Direction string
	Ratio     float64
	Cwd       string
	Label     string
}

// StartAgentRequest describes "herdr agent start" arguments.
type StartAgentRequest struct {
	Name   string
	Kind   string
	PaneID string
	Args   []string
}

// Call records one invoked Client method and the identifier it targeted.
type Call struct {
	Method string
	Target string
}

// SessionClient is the part of Client that session management needs: it lists
// sessions and changes their lifecycle. Production code talks to the real herdr
// CLI through this interface; tests use Fake.
type SessionClient interface {
	ListSessions(ctx context.Context) ([]Session, error)
	StopSession(ctx context.Context, name string) error
	DeleteSession(ctx context.Context, name string) error
}

// SpaceClient is the part of Client that reads a session's topology: the
// workspaces, their tabs, and every tab's pane split tree.
type SpaceClient interface {
	Snapshot(ctx context.Context, target Target) (Space, error)
}

// Client is the minimal herdr surface herdr-suite depends on.
type Client interface {
	SessionClient
	SpaceClient
	Workspace(ctx context.Context, workspaceID string) (Workspace, error)
	Pane(ctx context.Context, paneID string) (Pane, error)
	SplitPane(ctx context.Context, req SplitRequest) (string, error)
	StartAgent(ctx context.Context, req StartAgentRequest) error
}
