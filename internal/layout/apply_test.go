package layout

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/WilberC/herdr-suite/internal/herdrmock"
)

func applyFake() *herdrmock.Fake {
	return &herdrmock.Fake{}
}

func TestApplyIssuesTheExactMutationSequence(t *testing.T) {
	replay, err := Plan(wantLayout(), emptyTarget())
	if err != nil {
		t.Fatalf("Plan error = %v", err)
	}
	client := applyFake()

	if err := Apply(context.Background(), client.Mutations(herdrmock.Target{}), replay); err != nil {
		t.Fatalf("Apply error = %v, want nil", err)
	}

	want := []herdrmock.Op{
		{Method: "SplitPane", PaneID: "w1:p1", Created: "np-1", Direction: "right", Ratio: 0.6, Cwd: "/tmp"},
		{Method: "SplitPane", PaneID: "w1:p1", Created: "np-2", Direction: "right", Ratio: 0.5, Cwd: "/home/me/project"},
		{Method: "RenamePane", PaneID: "w1:p1", Label: "editor"},
		{Method: "StartAgent", PaneID: "w1:p1", AgentKind: "pi", AgentName: "pi"},
		{Method: "CreateTab", WorkspaceID: "w1", TabID: "nt-1", Created: "np-3", Cwd: "/tmp", Label: "side"},
		{Method: "StartAgent", PaneID: "np-3", AgentKind: "claude", AgentName: "claude"},
		{Method: "FocusTab", TabID: "w1:t1"},
		{Method: "FocusAgent", AgentName: "pi"},
	}
	if got := client.Ops(); !reflect.DeepEqual(got, want) {
		t.Fatalf("ops =\n%#v\nwant:\n%#v", got, want)
	}
}

func TestApplyReusesTheBasePaneAndTab(t *testing.T) {
	replay := Replay{
		Layout:    "work",
		Workspace: "project",
		Tabs:      1,
		Panes:     1,
		Base:      Base{WorkspaceID: "w1", TabID: "w1:t1", PaneID: "w1:p1", Leaf: "p-1"},
		Actions: []Action{
			{Op: OpRenamePane, Tab: 1, Pane: "p-1", Label: "editor"},
			{Op: OpFocusTab, Tab: 1, TabLabel: "main"},
		},
	}
	client := applyFake()

	if err := Apply(context.Background(), client.Mutations(herdrmock.Target{Name: "work", Socket: "/tmp/work.sock"}), replay); err != nil {
		t.Fatalf("Apply error = %v, want nil", err)
	}

	want := []herdrmock.Op{
		{Method: "RenamePane", Target: herdrmock.Target{Name: "work", Socket: "/tmp/work.sock"}, PaneID: "w1:p1", Label: "editor"},
		{Method: "FocusTab", Target: herdrmock.Target{Name: "work", Socket: "/tmp/work.sock"}, TabID: "w1:t1"},
	}
	if got := client.Ops(); !reflect.DeepEqual(got, want) {
		t.Fatalf("ops =\n%#v\nwant:\n%#v", got, want)
	}
}

func TestApplyReportsTheFailingStepAndStops(t *testing.T) {
	replay, err := Plan(wantLayout(), emptyTarget())
	if err != nil {
		t.Fatalf("Plan error = %v", err)
	}
	client := applyFake()
	client.Fail = func(op herdrmock.Op) error {
		if op.Method == "CreateTab" {
			return errors.New("pane limit reached")
		}
		return nil
	}

	err = Apply(context.Background(), client.Mutations(herdrmock.Target{}), replay)

	if err == nil {
		t.Fatalf("Apply error = nil, want the failing step")
	}
	var step StepError
	if !errors.As(err, &step) {
		t.Fatalf("Apply error = %T, want a StepError", err)
	}
	if step.Index != 5 || step.Total != 8 {
		t.Fatalf("step = %d of %d, want 5 of 8", step.Index, step.Total)
	}
	for _, want := range []string{"step 5 of 8", "create tab 2 'side'", "pane limit reached"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("Apply error = %q, want it to contain %q", err.Error(), want)
		}
	}
	if ops := client.Ops(); len(ops) != 5 {
		t.Fatalf("ops after the failure = %d, want nothing after step 5: %#v", len(ops), ops)
	}
}

func TestApplyCreatesAWorkspaceThenBuildsInIt(t *testing.T) {
	replay, err := Plan(wantLayout(), Target{})
	if err != nil {
		t.Fatalf("Plan error = %v", err)
	}
	client := applyFake()

	if err := Apply(context.Background(), client.Mutations(herdrmock.Target{}), replay); err != nil {
		t.Fatalf("Apply error = %v, want nil", err)
	}

	ops := client.Ops()
	if len(ops) == 0 {
		t.Fatalf("Apply issued no calls")
	}
	want := herdrmock.Op{Method: "CreateWorkspace", WorkspaceID: "nw-1", TabID: "nt-1", Created: "np-1", Cwd: "/home/me/project", Label: "project"}
	if !reflect.DeepEqual(ops[0], want) {
		t.Fatalf("first op = %#v, want %#v", ops[0], want)
	}
	for _, op := range ops {
		if op.Method == "SplitPane" && op.PaneID != "np-1" {
			t.Fatalf("split targeted %q, want the created workspace's pane np-1", op.PaneID)
		}
	}
	if last := ops[len(ops)-3]; last.Method != "FocusWorkspace" || last.WorkspaceID != "nw-1" {
		t.Fatalf("focus op = %#v, want the created workspace focused", last)
	}
}

// TestApplyReportsEveryStepBeforeItRuns pins the observer contract the loaders
// read: one report per step, numbered from one over the whole replay, delivered
// before the step reaches herdr so a slow step is named while it runs.
func TestApplyReportsEveryStepBeforeItRuns(t *testing.T) {
	replay, err := Plan(wantLayout(), emptyTarget())
	if err != nil {
		t.Fatalf("Plan error = %v", err)
	}
	client := applyFake()
	var steps []Step
	var opsAtReport []int

	err = Apply(context.Background(), client.Mutations(herdrmock.Target{}), replay, func(step Step) {
		steps = append(steps, step)
		opsAtReport = append(opsAtReport, len(client.Ops()))
	})
	if err != nil {
		t.Fatalf("Apply error = %v, want nil", err)
	}

	if len(steps) != len(replay.Actions) {
		t.Fatalf("observer saw %d steps, want %d", len(steps), len(replay.Actions))
	}
	for index, step := range steps {
		if step.Index != index+1 || step.Total != len(replay.Actions) {
			t.Fatalf("step %d = %d of %d, want %d of %d", index, step.Index, step.Total, index+1, len(replay.Actions))
		}
		if step.Describe() != replay.Actions[index].describe() {
			t.Fatalf("step %d describes %q, want %q", step.Index, step.Describe(), replay.Actions[index].describe())
		}
		if opsAtReport[index] != index {
			t.Fatalf("step %d was reported after %d calls, want it reported before the step runs", step.Index, opsAtReport[index])
		}
	}
}

// TestApplyAcceptsANilObserver keeps the callback optional: every caller that
// has nothing to report passes nil and the replay behaves as before.
func TestApplyAcceptsANilObserver(t *testing.T) {
	replay, err := Plan(wantLayout(), emptyTarget())
	if err != nil {
		t.Fatalf("Plan error = %v", err)
	}
	client := applyFake()

	if err := Apply(context.Background(), client.Mutations(herdrmock.Target{}), replay, nil); err != nil {
		t.Fatalf("Apply error = %v, want nil", err)
	}
	if len(client.Ops()) != len(replay.Actions) {
		t.Fatalf("replayed %d calls, want %d", len(client.Ops()), len(replay.Actions))
	}
}
