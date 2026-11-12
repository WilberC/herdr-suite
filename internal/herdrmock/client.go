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

// SplitRequest describes "herdr pane split" arguments. herdr keeps the split
// pane and places the new one beside it, so Ratio is the share the split pane
// keeps and the new pane takes the rest.
type SplitRequest struct {
	PaneID    string
	Direction string
	Ratio     float64
	Cwd       string
	Focus     bool
}

// StartAgentRequest describes "herdr agent start" arguments.
type StartAgentRequest struct {
	Name   string
	Kind   string
	PaneID string
	Args   []string
}

// CreateWorkspaceRequest describes "herdr workspace create" arguments.
type CreateWorkspaceRequest struct {
	Cwd   string
	Label string
	Focus bool
}

// CreatedPane is the workspace, tab and root pane a creation call returned.
type CreatedPane struct {
	WorkspaceID string
	TabID       string
	PaneID      string
}

// CreateTabRequest describes "herdr tab create" arguments.
type CreateTabRequest struct {
	WorkspaceID string
	Cwd         string
	Label       string
	Focus       bool
}

// CreatedTab is the tab and its root pane a creation call returned.
type CreatedTab struct {
	TabID  string
	PaneID string
}

// Call records one invoked Client method and the identifier it targeted.
type Call struct {
	Method string
	Target string
}

// Op records one client request in full, so a test can assert the exact call
// sequence a replay performs and rebuild the topology it composed.
type Op struct {
	Method      string
	Target      Target
	PaneID      string
	Created     string
	TabID       string
	WorkspaceID string
	Direction   string
	Ratio       float64
	Cwd         string
	Label       string
	AgentKind   string
	AgentName   string
	Focus       bool
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

// Writer binds one herdr session's write surface: a layout replay performs
// every mutation through the Mutator it returns.
type Writer interface {
	Mutations(target Target) Mutator
}

// Client is the minimal herdr surface herdr-suite depends on.
type Client interface {
	SessionClient
	SpaceClient
	Writer
	Workspace(ctx context.Context, workspaceID string) (Workspace, error)
	Pane(ctx context.Context, paneID string) (Pane, error)
}

// Mutator is the write surface of one herdr session: every call mutates the
// layout of the session target selects.
type Mutator interface {
	CreateWorkspace(ctx context.Context, req CreateWorkspaceRequest) (CreatedPane, error)
	CreateTab(ctx context.Context, req CreateTabRequest) (CreatedTab, error)
	SplitPane(ctx context.Context, req SplitRequest) (string, error)
	RenameTab(ctx context.Context, tabID, label string) error
	RenamePane(ctx context.Context, paneID, label string) error
	FocusWorkspace(ctx context.Context, workspaceID string) error
	FocusTab(ctx context.Context, tabID string) error
	FocusAgent(ctx context.Context, target string) error
	StartAgent(ctx context.Context, req StartAgentRequest) error
	ClosePane(ctx context.Context, paneID string) error
	CloseTab(ctx context.Context, tabID string) error
}
