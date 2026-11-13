package layout

import (
	"context"
	"errors"
	"fmt"

	"github.com/WilberC/herdr-suite/internal/herdrmock"
)

// StepError reports the replay step that failed. herdr keeps no transaction, so
// the steps before this one stay applied and the report names the step that
// stopped the replay. Workspace names the workspace of a session replay whose
// step failed, and is empty for a single-workspace replay.
type StepError struct {
	Index     int
	Total     int
	Action    Action
	Workspace string
	Err       error
}

func (e StepError) Error() string {
	workspace := ""
	if e.Workspace != "" {
		workspace = fmt.Sprintf("workspace '%s': ", e.Workspace)
	}
	return fmt.Sprintf("%sstep %d of %d failed: %s: %v", workspace, e.Index, e.Total, e.Action.describe(), e.Err)
}

// Unwrap returns the herdr failure the step reported.
func (e StepError) Unwrap() error { return e.Err }

// Step is one replay step as an observer reads it: its position in the replay,
// the action about to run, and the workspace a session replay builds it in.
// Workspace is empty for a single-workspace replay.
type Step struct {
	Index     int
	Total     int
	Action    Action
	Workspace string
}

// Describe is the one-line form a progress report shows for s: the action, and
// the workspace when the replay is a session replay.
func (s Step) Describe() string {
	if s.Workspace != "" {
		return fmt.Sprintf("workspace '%s': %s", s.Workspace, s.Action.describe())
	}
	return s.Action.describe()
}

// StepObserver is told which step is about to run, before its herdr call. It is
// optional: a caller with nothing to report passes nil, and a nil observer is
// never called. The observer runs in the replay's own goroutine and must not
// block, so a viewer hands the step to its own loop and returns.
type StepObserver func(Step)

// notify calls every observer that is not nil.
func notify(observers []StepObserver, step Step) {
	for _, observer := range observers {
		if observer != nil {
			observer(step)
		}
	}
}

// Apply replays replay against mut, one herdr call per step, and stops at the
// first failure. The caller reports the StepError: a replay that stopped
// halfway left a partial layout behind, and herdr cannot roll it back. An
// observer passed here is told each step before it runs, which is how a slow
// replay says what it is doing.
func Apply(ctx context.Context, mut herdrmock.Mutator, replay Replay, observers ...StepObserver) error {
	state := &liveReplay{
		workspace: replay.Base.WorkspaceID,
		tabs:      make(map[int]string, replay.Tabs),
		panes:     make(map[string]string, replay.Panes),
	}
	if replay.Base.TabID != "" && replay.Base.Leaf != "" {
		state.tabs[1] = replay.Base.TabID
		state.panes[replay.Base.Leaf] = replay.Base.PaneID
	}

	for index, action := range replay.Actions {
		notify(observers, Step{Index: index + 1, Total: len(replay.Actions), Action: action})
		if err := applyAction(ctx, mut, state, action); err != nil {
			return StepError{Index: index + 1, Total: len(replay.Actions), Action: action, Err: err}
		}
	}
	return nil
}

// liveReplay maps the stored handles a replay plans with to the live ids herdr
// answered with.
type liveReplay struct {
	workspace string
	tabs      map[int]string
	panes     map[string]string
}

func applyAction(ctx context.Context, mut herdrmock.Mutator, state *liveReplay, action Action) error {
	switch action.Op {
	case OpCreateWorkspace:
		created, err := mut.CreateWorkspace(ctx, herdrmock.CreateWorkspaceRequest{
			Cwd:   action.Cwd,
			Label: action.Label,
			Focus: action.Focus,
		})
		if err != nil {
			return err
		}
		state.workspace = created.WorkspaceID
		state.tabs[action.Tab] = created.TabID
		state.panes[action.Pane] = created.PaneID
		return nil

	case OpCreateTab:
		created, err := mut.CreateTab(ctx, herdrmock.CreateTabRequest{
			WorkspaceID: state.workspace,
			Cwd:         action.Cwd,
			Label:       action.TabLabel,
			Focus:       action.Focus,
		})
		if err != nil {
			return err
		}
		state.tabs[action.Tab] = created.TabID
		state.panes[action.Pane] = created.PaneID
		return nil

	case OpSplit:
		from, err := state.pane(action.From)
		if err != nil {
			return err
		}
		created, err := mut.SplitPane(ctx, herdrmock.SplitRequest{
			PaneID:    from,
			Direction: action.Direction,
			Ratio:     action.Ratio,
			Cwd:       action.Cwd,
			Focus:     action.Focus,
		})
		if err != nil {
			return err
		}
		state.panes[action.Pane] = created
		return nil

	case OpRenamePane:
		pane, err := state.pane(action.Pane)
		if err != nil {
			return err
		}
		return mut.RenamePane(ctx, pane, action.Label)

	case OpRenameTab:
		tab, err := state.tab(action.Tab)
		if err != nil {
			return err
		}
		return mut.RenameTab(ctx, tab, action.Label)

	case OpStartAgent:
		pane, err := state.pane(action.Pane)
		if err != nil {
			return err
		}
		return mut.StartAgent(ctx, herdrmock.StartAgentRequest{
			Name:   action.AgentName,
			Kind:   action.AgentKind,
			PaneID: pane,
		})

	case OpClosePane:
		return mut.ClosePane(ctx, action.PaneID)

	case OpCloseTab:
		return mut.CloseTab(ctx, action.TabID)

	case OpFocusWorkspace:
		workspace := action.WorkspaceID
		if workspace == "" {
			workspace = state.workspace
		}
		if workspace == "" {
			return errors.New("no workspace to focus")
		}
		return mut.FocusWorkspace(ctx, workspace)

	case OpFocusTab:
		tab, err := state.tab(action.Tab)
		if err != nil {
			return err
		}
		return mut.FocusTab(ctx, tab)

	case OpFocusAgent:
		return mut.FocusAgent(ctx, action.AgentName)
	}
	return fmt.Errorf("unknown replay step '%s'", action.Op)
}

func (s *liveReplay) pane(handle string) (string, error) {
	pane, ok := s.panes[handle]
	if !ok || pane == "" {
		return "", fmt.Errorf("pane '%s' is not part of the replay", handle)
	}
	return pane, nil
}

func (s *liveReplay) tab(index int) (string, error) {
	tab, ok := s.tabs[index]
	if !ok || tab == "" {
		return "", fmt.Errorf("tab %d is not part of the replay", index)
	}
	return tab, nil
}
