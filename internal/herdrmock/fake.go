package herdrmock

import (
	"context"
	"fmt"
	"sync"
)

// Fake is an in-memory Client for tests. It never opens a socket: every
// method returns scripted state and records the call it received. Mutations
// through Mutations also record the full request, so a test can assert the
// exact sequence a replay performs.
type Fake struct {
	Sessions   []Session
	Panes      map[string]Pane
	Workspaces map[string]Workspace
	Spaces     map[string]Space
	NextPaneID string

	ListSessionsErr    error
	StopSessionErr     error
	DeleteSessionErr   error
	SnapshotErr        error
	WorkspaceErr       error
	PaneErr            error
	CreateWorkspaceErr error
	CreateTabErr       error
	SplitPaneErr       error
	RenameTabErr       error
	RenamePaneErr      error
	FocusWorkspaceErr  error
	FocusTabErr        error
	FocusAgentErr      error
	StartAgentErr      error
	ClosePaneErr       error
	CloseTabErr        error

	// Fail, when set, receives every mutation before it runs and can fail it.
	Fail func(Op) error

	mu     sync.Mutex
	calls  []Call
	ops    []Op
	panes  int
	tabs   int
	spaces int
}

// Calls returns the calls recorded so far, oldest first.
func (f *Fake) Calls() []Call {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Call(nil), f.calls...)
}

// Ops returns the mutations recorded so far, oldest first.
func (f *Fake) Ops() []Op {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Op(nil), f.ops...)
}

func (f *Fake) record(method, target string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, Call{Method: method, Target: target})
}

// begin records one mutation and reports the scripted failure, if any.
func (f *Fake) begin(op Op) error {
	f.mu.Lock()
	f.ops = append(f.ops, op)
	f.calls = append(f.calls, Call{Method: op.Method, Target: callTarget(op)})
	f.mu.Unlock()
	if f.Fail != nil {
		return f.Fail(op)
	}
	return nil
}

// callTarget is the identifier a recorded mutation addressed.
func callTarget(op Op) string {
	switch op.Method {
	case "CreateWorkspace", "CreateTab", "FocusWorkspace", "CloseWorkspace":
		return op.WorkspaceID
	case "ClosePane", "RenamePane", "SplitPane", "StartAgent":
		return op.PaneID
	case "CloseTab", "RenameTab", "FocusTab":
		return op.TabID
	case "FocusAgent":
		return op.AgentName
	}
	return ""
}

// ListSessions returns the scripted sessions.
func (f *Fake) ListSessions(ctx context.Context) ([]Session, error) {
	f.record("ListSessions", "")
	if f.ListSessionsErr != nil {
		return nil, f.ListSessionsErr
	}
	return append([]Session(nil), f.Sessions...), nil
}

// StopSession records the stop and returns the scripted error.
func (f *Fake) StopSession(ctx context.Context, name string) error {
	f.record("StopSession", name)
	return f.StopSessionErr
}

// DeleteSession records the delete and returns the scripted error.
func (f *Fake) DeleteSession(ctx context.Context, name string) error {
	f.record("DeleteSession", name)
	return f.DeleteSessionErr
}

// Snapshot returns the scripted topology for the target session. A target name
// that was not scripted reports an error, so a test can exercise the fallbacks
// that depend on a session being unreadable.
func (f *Fake) Snapshot(ctx context.Context, target Target) (Space, error) {
	f.record("Snapshot", target.Name)
	if f.SnapshotErr != nil {
		return Space{}, f.SnapshotErr
	}
	space, ok := f.Spaces[target.Name]
	if !ok {
		return Space{}, fmt.Errorf("unknown session topology %q", target.Name)
	}
	return space, nil
}

// Workspace returns the scripted topology for workspaceID.
func (f *Fake) Workspace(ctx context.Context, workspaceID string) (Workspace, error) {
	f.record("Workspace", workspaceID)
	if f.WorkspaceErr != nil {
		return Workspace{}, f.WorkspaceErr
	}
	workspace, ok := f.Workspaces[workspaceID]
	if !ok {
		return Workspace{}, fmt.Errorf("unknown workspace %q", workspaceID)
	}
	return workspace, nil
}

// Pane returns the scripted pane for paneID.
func (f *Fake) Pane(ctx context.Context, paneID string) (Pane, error) {
	f.record("Pane", paneID)
	if f.PaneErr != nil {
		return Pane{}, f.PaneErr
	}
	pane, ok := f.Panes[paneID]
	if !ok {
		return Pane{}, fmt.Errorf("unknown pane %q", paneID)
	}
	return pane, nil
}

// Mutations returns the write surface bound to target. Every recorded mutation
// carries that target, so a test can assert which session was written to.
func (f *Fake) Mutations(target Target) Mutator {
	return &bound{fake: f, target: target}
}

// bound is the Fake's Mutator for one target session.
type bound struct {
	fake   *Fake
	target Target
}

// CreateWorkspace records the creation and returns scripted ids.
func (b *bound) CreateWorkspace(ctx context.Context, req CreateWorkspaceRequest) (CreatedPane, error) {
	b.fake.mu.Lock()
	b.fake.spaces++
	index := b.fake.spaces
	b.fake.mu.Unlock()

	created := CreatedPane{
		WorkspaceID: fmt.Sprintf("nw-%d", index),
		TabID:       fmt.Sprintf("nt-%d", index),
		PaneID:      b.fake.nextPane(),
	}
	op := Op{
		Method:      "CreateWorkspace",
		Target:      b.target,
		WorkspaceID: created.WorkspaceID,
		TabID:       created.TabID,
		Created:     created.PaneID,
		Cwd:         req.Cwd,
		Label:       req.Label,
		Focus:       req.Focus,
	}
	if err := b.fake.begin(op); err != nil {
		return CreatedPane{}, err
	}
	if b.fake.CreateWorkspaceErr != nil {
		return CreatedPane{}, b.fake.CreateWorkspaceErr
	}
	return created, nil
}

// CreateTab records the creation and returns scripted ids.
func (b *bound) CreateTab(ctx context.Context, req CreateTabRequest) (CreatedTab, error) {
	b.fake.mu.Lock()
	b.fake.tabs++
	index := b.fake.tabs
	b.fake.mu.Unlock()

	created := CreatedTab{TabID: fmt.Sprintf("nt-%d", index), PaneID: b.fake.nextPane()}
	op := Op{
		Method:      "CreateTab",
		Target:      b.target,
		WorkspaceID: req.WorkspaceID,
		TabID:       created.TabID,
		Created:     created.PaneID,
		Cwd:         req.Cwd,
		Label:       req.Label,
		Focus:       req.Focus,
	}
	if err := b.fake.begin(op); err != nil {
		return CreatedTab{}, err
	}
	if b.fake.CreateTabErr != nil {
		return CreatedTab{}, b.fake.CreateTabErr
	}
	return created, nil
}

// SplitPane records the split and returns the created pane id.
func (b *bound) SplitPane(ctx context.Context, req SplitRequest) (string, error) {
	created := b.fake.paneID()
	op := Op{
		Method:    "SplitPane",
		Target:    b.target,
		PaneID:    req.PaneID,
		Created:   created,
		Direction: req.Direction,
		Ratio:     req.Ratio,
		Cwd:       req.Cwd,
		Focus:     req.Focus,
	}
	if err := b.fake.begin(op); err != nil {
		return "", err
	}
	if b.fake.SplitPaneErr != nil {
		return "", b.fake.SplitPaneErr
	}
	return created, nil
}

// RenameTab records the rename.
func (b *bound) RenameTab(ctx context.Context, tabID, label string) error {
	if err := b.fake.begin(Op{Method: "RenameTab", Target: b.target, TabID: tabID, Label: label}); err != nil {
		return err
	}
	return b.fake.RenameTabErr
}

// RenamePane records the rename.
func (b *bound) RenamePane(ctx context.Context, paneID, label string) error {
	if err := b.fake.begin(Op{Method: "RenamePane", Target: b.target, PaneID: paneID, Label: label}); err != nil {
		return err
	}
	return b.fake.RenamePaneErr
}

// FocusWorkspace records the focus change.
func (b *bound) FocusWorkspace(ctx context.Context, workspaceID string) error {
	if err := b.fake.begin(Op{Method: "FocusWorkspace", Target: b.target, WorkspaceID: workspaceID}); err != nil {
		return err
	}
	return b.fake.FocusWorkspaceErr
}

// FocusTab records the focus change.
func (b *bound) FocusTab(ctx context.Context, tabID string) error {
	if err := b.fake.begin(Op{Method: "FocusTab", Target: b.target, TabID: tabID}); err != nil {
		return err
	}
	return b.fake.FocusTabErr
}

// FocusAgent records the focus change.
func (b *bound) FocusAgent(ctx context.Context, target string) error {
	if err := b.fake.begin(Op{Method: "FocusAgent", Target: b.target, AgentName: target}); err != nil {
		return err
	}
	return b.fake.FocusAgentErr
}

// StartAgent records the agent launch.
func (b *bound) StartAgent(ctx context.Context, req StartAgentRequest) error {
	op := Op{
		Method:    "StartAgent",
		Target:    b.target,
		PaneID:    req.PaneID,
		AgentKind: req.Kind,
		AgentName: req.Name,
	}
	if err := b.fake.begin(op); err != nil {
		return err
	}
	return b.fake.StartAgentErr
}

// ClosePane records the close.
func (b *bound) ClosePane(ctx context.Context, paneID string) error {
	if err := b.fake.begin(Op{Method: "ClosePane", Target: b.target, PaneID: paneID}); err != nil {
		return err
	}
	return b.fake.ClosePaneErr
}

// CloseTab records the close.
func (b *bound) CloseTab(ctx context.Context, tabID string) error {
	if err := b.fake.begin(Op{Method: "CloseTab", Target: b.target, TabID: tabID}); err != nil {
		return err
	}
	return b.fake.CloseTabErr
}

// nextPane returns the pane id the next creation hands out.
func (f *Fake) nextPane() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.panes++
	return fmt.Sprintf("np-%d", f.panes)
}

// paneID is nextPane for a split, which honours the scripted NextPaneID.
func (f *Fake) paneID() string {
	if f.NextPaneID != "" {
		return f.NextPaneID
	}
	return f.nextPane()
}

var (
	_ Client      = (*Fake)(nil)
	_ SpaceClient = (*Fake)(nil)
	_ Mutator     = (*bound)(nil)
)
