package herdrmock

import "context"

// Session is a named herdr session as reported by "herdr session list".
type Session struct {
	Name    string
	Running bool
}

// Pane is one terminal pane inside a tab.
type Pane struct {
	ID          string
	WorkspaceID string
	TabID       string
	Label       string
	Cwd         string
	Agent       string
}

// LayoutNode is one node of a tab's split tree. A leaf names a pane; a split
// carries a direction, a ratio and exactly two children.
type LayoutNode struct {
	PaneID    string
	Direction string
	Ratio     float64
	Children  []LayoutNode
}

// Tab is one herdr tab with its pane split tree.
type Tab struct {
	ID    string
	Title string
	Root  LayoutNode
}

// Workspace is a herdr workspace with the tabs it holds.
type Workspace struct {
	ID   string
	Name string
	Tabs []Tab
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

// Client is the minimal herdr surface herdr-suite depends on. Production code
// talks to the real herdr CLI through this interface; tests use Fake.
type Client interface {
	ListSessions(ctx context.Context) ([]Session, error)
	StopSession(ctx context.Context, name string) error
	DeleteSession(ctx context.Context, name string) error
	Workspace(ctx context.Context, workspaceID string) (Workspace, error)
	Pane(ctx context.Context, paneID string) (Pane, error)
	SplitPane(ctx context.Context, req SplitRequest) (string, error)
	StartAgent(ctx context.Context, req StartAgentRequest) error
}
