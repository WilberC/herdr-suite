package layout

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/WilberC/herdr-suite/internal/herdrmock"
)

func TestPlanSessionCreatesOneWorkspacePerStoredEntry(t *testing.T) {
	plan, err := PlanSession(sessionLayout(), Target{})
	if err != nil {
		t.Fatalf("PlanSession error = %v, want nil", err)
	}

	if plan.Layout != "session" {
		t.Fatalf("plan layout = %q, want %q", plan.Layout, "session")
	}
	if len(plan.Workspaces) != 2 {
		t.Fatalf("plan holds %d workspaces, want 2", len(plan.Workspaces))
	}
	wantLabels := []string{"project", "api"}
	for index, replay := range plan.Workspaces {
		if replay.Workspace != wantLabels[index] {
			t.Fatalf("workspace %d = %q, want %q", index+1, replay.Workspace, wantLabels[index])
		}
		if first := replay.Actions[0]; first.Op != OpCreateWorkspace || first.Label != wantLabels[index] {
			t.Fatalf("workspace %d starts with %#v, want a creation of %q", index+1, first, wantLabels[index])
		}
		focus := -1
		for position, action := range replay.Actions {
			if action.Op == OpFocusWorkspace {
				focus = position
			}
		}
		if focus < 0 {
			t.Fatalf("workspace %d never focuses the workspace it creates", index+1)
		}
		if id := replay.Actions[focus].WorkspaceID; id != "" {
			t.Fatalf("workspace %d focused the live workspace %q, want the one the replay creates", index+1, id)
		}
		if focus != len(replay.Actions)-2 && focus != len(replay.Actions)-3 {
			t.Fatalf("workspace %d focuses the workspace at step %d of %d, want the tab and agent focus after it",
				index+1, focus+1, len(replay.Actions))
		}
		if next := replay.Actions[focus+1]; next.Op != OpFocusTab {
			t.Fatalf("workspace %d follows its workspace focus with %#v, want the tab focus", index+1, next)
		}
	}
}

func TestPlanSessionNamesAgentsPerWorkspaceUniquely(t *testing.T) {
	space := sessionSpace()
	space.Workspaces[1].Tabs[0].Panes[0].AgentName = "pi"

	plan, err := PlanSession(mustCaptureSession(t, "session", space), Target{Agents: []string{"pi"}})
	if err != nil {
		t.Fatalf("PlanSession error = %v, want nil", err)
	}

	var names []string
	for _, replay := range plan.Workspaces {
		for _, action := range replay.Actions {
			if action.Op == OpStartAgent {
				names = append(names, action.AgentName)
			}
		}
	}
	if want := []string{"pi-2", "claude", "pi-3"}; strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("agent names = %v, want %v", names, want)
	}
}

func TestPlanSessionRefusesASingleWorkspaceDocument(t *testing.T) {
	_, err := PlanSession(wantLayout(), Target{})

	if err == nil || !strings.Contains(err.Error(), "one workspace") {
		t.Fatalf("PlanSession error = %v, want a refusal naming the single-workspace document", err)
	}
}

func TestApplySessionReplaysEveryWorkspaceAndFocusesTheLast(t *testing.T) {
	plan, err := PlanSession(sessionLayout(), Target{})
	if err != nil {
		t.Fatalf("PlanSession error = %v, want nil", err)
	}
	client := applyFake()

	if err := ApplySession(context.Background(), client.Mutations(herdrmock.Target{}), plan); err != nil {
		t.Fatalf("ApplySession error = %v, want nil", err)
	}

	var created []string
	var focused []string
	for _, op := range client.Ops() {
		switch op.Method {
		case "CreateWorkspace":
			created = append(created, op.Label+"@"+op.WorkspaceID)
		case "FocusWorkspace":
			focused = append(focused, op.WorkspaceID)
		}
	}
	if want := []string{"project@nw-1", "api@nw-2"}; strings.Join(created, ",") != strings.Join(want, ",") {
		t.Fatalf("created workspaces = %v, want %v", created, want)
	}
	if want := []string{"nw-1", "nw-2"}; strings.Join(focused, ",") != strings.Join(want, ",") {
		t.Fatalf("focused workspaces = %v, want %v", focused, want)
	}
	ops := client.Ops()
	tail := ops[len(ops)-3:]
	if tail[0].Method != "FocusWorkspace" || tail[0].WorkspaceID != "nw-2" {
		t.Fatalf("tail = %#v, want the last workspace focused", tail)
	}
	if tail[1].Method != "FocusTab" || tail[1].TabID != "nt-2" {
		t.Fatalf("tail = %#v, want the last workspace's tab focused", tail)
	}
	if tail[2].Method != "FocusAgent" || tail[2].AgentName != "api" {
		t.Fatalf("tail = %#v, want the last workspace's agent focused", tail)
	}
}

func TestApplySessionNamesTheFailingWorkspace(t *testing.T) {
	plan, err := PlanSession(sessionLayout(), Target{})
	if err != nil {
		t.Fatalf("PlanSession error = %v, want nil", err)
	}
	client := applyFake()
	client.Fail = func(op herdrmock.Op) error {
		if op.Method == "CreateTab" && op.Label == "side" {
			return errors.New("pane limit reached")
		}
		return nil
	}

	err = ApplySession(context.Background(), client.Mutations(herdrmock.Target{}), plan)

	if err == nil {
		t.Fatalf("ApplySession error = nil, want the failing step")
	}
	for _, want := range []string{"workspace 'project'", "step 7 of 11", "pane limit reached"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("ApplySession error = %q, want it to contain %q", err.Error(), want)
		}
	}
}

func mustCaptureSession(t *testing.T, name string, space herdrmock.Space) Layout {
	t.Helper()
	document, err := CaptureSession(name, space)
	if err != nil {
		t.Fatalf("CaptureSession error = %v", err)
	}
	return document
}

// TestApplySessionNumbersItsStepsAcrossTheWholePlan pins what a session loader
// shows: one count over every workspace, and the workspace each step builds in.
func TestApplySessionNumbersItsStepsAcrossTheWholePlan(t *testing.T) {
	plan, err := PlanSession(sessionLayout(), Target{})
	if err != nil {
		t.Fatalf("PlanSession error = %v, want nil", err)
	}
	client := applyFake()
	var steps []Step

	err = ApplySession(context.Background(), client.Mutations(herdrmock.Target{}), plan, func(step Step) {
		steps = append(steps, step)
	})
	if err != nil {
		t.Fatalf("ApplySession error = %v, want nil", err)
	}

	if len(steps) != plan.Steps {
		t.Fatalf("observer saw %d steps, want %d", len(steps), plan.Steps)
	}
	var workspaces []string
	for index, step := range steps {
		if step.Index != index+1 || step.Total != plan.Steps {
			t.Fatalf("step %d = %d of %d, want %d of %d", index, step.Index, step.Total, index+1, plan.Steps)
		}
		if step.Workspace == "" {
			t.Fatalf("step %d names no workspace", step.Index)
		}
		if len(workspaces) == 0 || workspaces[len(workspaces)-1] != step.Workspace {
			workspaces = append(workspaces, step.Workspace)
		}
		if !strings.HasPrefix(step.Describe(), "workspace '"+step.Workspace+"': ") {
			t.Fatalf("step %d describes %q, want it to name workspace %q", step.Index, step.Describe(), step.Workspace)
		}
	}
	if want := []string{"project", "api"}; strings.Join(workspaces, ",") != strings.Join(want, ",") {
		t.Fatalf("step workspaces = %v, want %v", workspaces, want)
	}
}
