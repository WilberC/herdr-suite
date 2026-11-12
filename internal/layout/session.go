package layout

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/WilberC/herdr-suite/internal/herdrmock"
)

// SessionPlan is the ordered ladder that recreates a session layout: one replay
// per stored workspace, in the order the fork captured them. Every replay builds
// in a workspace of its own, because a session layout restores into a session
// rather than over an existing workspace.
type SessionPlan struct {
	Layout     string
	Workspaces []Replay
	Tabs       int
	Panes      int
	Steps      int
}

// PlanSession builds the ordered replay of a session layout. Agent names are
// chosen across the whole plan, because herdr keeps them unique per session
// rather than per workspace. A single-workspace layout is refused: it replays
// into a destination workspace, which is Plan's job.
func PlanSession(document Layout, target Target) (SessionPlan, error) {
	if !document.Multi() {
		return SessionPlan{}, fmt.Errorf(
			"layout '%s' holds one workspace; load it into a workspace instead of a session", document.Name)
	}

	plan := SessionPlan{Layout: document.Name, Workspaces: make([]Replay, 0, len(document.Workspaces))}
	agents := append([]string(nil), target.Agents...)
	for _, workspace := range document.Workspaces {
		single := Layout{Name: document.Name, Session: document.Session, Workspace: workspace}
		replay, err := Plan(single, Target{Keep: target.Keep, Agents: agents})
		if err != nil {
			return SessionPlan{}, fmt.Errorf("workspace '%s': %w", workspace.Label, err)
		}
		for _, action := range replay.Actions {
			if action.Op == OpStartAgent {
				agents = append(agents, action.AgentName)
			}
		}
		plan.Workspaces = append(plan.Workspaces, replay)
		plan.Tabs += replay.Tabs
		plan.Panes += replay.Panes
		plan.Steps += len(replay.Actions)
	}
	return plan, nil
}

// ApplySession replays every workspace of plan, one herdr call per step, and
// stops at the first failure with the workspace the failing step belongs to.
func ApplySession(ctx context.Context, mut herdrmock.Mutator, plan SessionPlan) error {
	for _, replay := range plan.Workspaces {
		if err := Apply(ctx, mut, replay); err != nil {
			var step StepError
			if errors.As(err, &step) {
				step.Workspace = replay.Workspace
				return step
			}
			return err
		}
	}
	return nil
}

// RenderSessionPlan prints the ordered session replay as the stable text
// --dry-run shows: one section per stored workspace, numbered from one.
func RenderSessionPlan(plan SessionPlan) string {
	var out strings.Builder
	fmt.Fprintf(&out, "session plan for layout '%s' · %s · %s · %s\n",
		plan.Layout, plural(len(plan.Workspaces), "workspace"), plural(plan.Tabs, "tab"), plural(plan.Panes, "pane"))
	for index, replay := range plan.Workspaces {
		fmt.Fprintf(&out, "workspace %d '%s' · %s · %s · %s\n", index+1, replay.Workspace,
			plural(replay.Tabs, "tab"), plural(replay.Panes, "pane"), plural(len(replay.Actions), "step"))
		for position, action := range replay.Actions {
			fmt.Fprintf(&out, "%d. %s\n", position+1, action.describe())
		}
	}
	return out.String()
}
