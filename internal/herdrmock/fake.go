package herdrmock

import (
	"context"
	"fmt"
	"sync"
)

// Fake is an in-memory Client for tests. It never opens a socket: every
// method returns scripted state and records the call it received.
type Fake struct {
	Sessions   []Session
	Panes      map[string]Pane
	Workspaces map[string]Workspace
	NextPaneID string

	ListSessionsErr  error
	StopSessionErr   error
	DeleteSessionErr error
	WorkspaceErr     error
	PaneErr          error
	SplitPaneErr     error
	StartAgentErr    error

	mu    sync.Mutex
	calls []Call
}

// Calls returns the calls recorded so far, oldest first.
func (f *Fake) Calls() []Call {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Call(nil), f.calls...)
}

func (f *Fake) record(method, target string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, Call{Method: method, Target: target})
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

// SplitPane records the split and returns the scripted new pane id.
func (f *Fake) SplitPane(ctx context.Context, req SplitRequest) (string, error) {
	f.record("SplitPane", req.PaneID)
	if f.SplitPaneErr != nil {
		return "", f.SplitPaneErr
	}
	return f.NextPaneID, nil
}

// StartAgent records the agent launch and returns the scripted error.
func (f *Fake) StartAgent(ctx context.Context, req StartAgentRequest) error {
	f.record("StartAgent", req.PaneID)
	return f.StartAgentErr
}

var _ Client = (*Fake)(nil)
