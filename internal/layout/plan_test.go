package layout

import (
	"reflect"
	"strings"
	"testing"

	"github.com/WilberC/herdr-suite/internal/herdrmock"
)

func emptyTarget() Target {
	return Target{Workspace: herdrmock.Workspace{
		ID:          "w1",
		Label:       "project",
		Focused:     true,
		ActiveTabID: "w1:t1",
		TabCount:    1,
		PaneCount:   1,
		Tabs: []herdrmock.Tab{
			{
				ID:            "w1:t1",
				Label:         "main",
				Focused:       true,
				FocusedPaneID: "w1:p1",
				Panes:         []herdrmock.Pane{{ID: "w1:p1", WorkspaceID: "w1", TabID: "w1:t1", Cwd: "/home/me/project"}},
				Root:          herdrmock.LayoutNode{PaneID: "w1:p1"},
			},
		},
	}}
}

func busyTarget(replace bool, keep string) Target {
	return Target{
		Replace: replace,
		Keep:    keep,
		Workspace: herdrmock.Workspace{
			ID:          "w1",
			Label:       "project",
			Focused:     false,
			ActiveTabID: "w1:t1",
			TabCount:    2,
			PaneCount:   3,
			Tabs: []herdrmock.Tab{
				{
					ID:            "w1:t1",
					Label:         "one",
					Focused:       true,
					FocusedPaneID: "w1:p1",
					Panes: []herdrmock.Pane{
						{ID: "w1:p1", WorkspaceID: "w1", TabID: "w1:t1", Cwd: "/home/me/project"},
						{ID: "w1:p2", WorkspaceID: "w1", TabID: "w1:t1", Cwd: "/tmp"},
					},
					Root: herdrmock.LayoutNode{
						Direction: "right",
						Ratio:     0.5,
						Children:  []herdrmock.LayoutNode{{PaneID: "w1:p1"}, {PaneID: "w1:p2"}},
					},
				},
				{
					ID:    "w1:t2",
					Label: "two",
					Panes: []herdrmock.Pane{{ID: "w1:p3", WorkspaceID: "w1", TabID: "w1:t2", Cwd: "/var/tmp"}},
					Root:  herdrmock.LayoutNode{PaneID: "w1:p3"},
				},
			},
		},
	}
}

func planLines(t *testing.T, replay Replay) []string {
	t.Helper()
	rendered := strings.Split(strings.TrimSuffix(RenderPlan(replay), "\n"), "\n")
	if len(rendered) < 1 {
		t.Fatalf("RenderPlan returned nothing")
	}
	return rendered[1:]
}

func TestPlanReplaysEveryParentBeforeItsChildren(t *testing.T) {
	replay, err := Plan(wantLayout(), emptyTarget())
	if err != nil {
		t.Fatalf("Plan error = %v, want nil", err)
	}

	want := []Action{
		{Op: OpSplit, Tab: 1, From: "p-1", Pane: "p-3", Direction: "right", Ratio: 0.6, Cwd: "/tmp"},
		{Op: OpSplit, Tab: 1, From: "p-1", Pane: "p-2", Direction: "right", Ratio: 0.5, Cwd: "/home/me/project"},
		{Op: OpRenamePane, Tab: 1, Pane: "p-1", Label: "editor"},
		{Op: OpStartAgent, Tab: 1, Pane: "p-1", AgentKind: "pi", AgentName: "pi"},
		{Op: OpCreateTab, Tab: 2, TabLabel: "side", Pane: "p-4", Cwd: "/tmp"},
		{Op: OpStartAgent, Tab: 2, Pane: "p-4", AgentKind: "claude", AgentName: "claude"},
		{Op: OpFocusTab, Tab: 1, TabLabel: "main"},
		{Op: OpFocusAgent, AgentName: "pi"},
	}
	if !reflect.DeepEqual(replay.Actions, want) {
		t.Fatalf("actions =\n%#v\nwant:\n%#v", replay.Actions, want)
	}

	wantBase := Base{WorkspaceID: "w1", TabID: "w1:t1", PaneID: "w1:p1", Leaf: "p-1"}
	if !reflect.DeepEqual(replay.Base, wantBase) {
		t.Fatalf("base = %#v, want %#v", replay.Base, wantBase)
	}
	if replay.Tabs != 2 || replay.Panes != 4 {
		t.Fatalf("counts = %d tabs, %d panes; want 2 tabs, 4 panes", replay.Tabs, replay.Panes)
	}
}

func TestPlanIsDeterministic(t *testing.T) {
	first, err := Plan(wantLayout(), emptyTarget())
	if err != nil {
		t.Fatalf("Plan error = %v, want nil", err)
	}
	second, err := Plan(wantLayout(), emptyTarget())
	if err != nil {
		t.Fatalf("Plan error = %v, want nil", err)
	}

	if !reflect.DeepEqual(first, second) {
		t.Fatalf("two plans over the same input differ:\n%#v\n%#v", first.Actions, second.Actions)
	}
	if got, want := RenderPlan(first), RenderPlan(second); got != want {
		t.Fatalf("two rendered plans differ:\n%s\n%s", got, want)
	}
}

func TestPlanClosesTheDestinationBeforeReplaying(t *testing.T) {
	replay, err := Plan(wantLayout(), busyTarget(true, ""))
	if err != nil {
		t.Fatalf("Plan error = %v", err)
	}

	want := []Action{
		{Op: OpClosePane, PaneID: "w1:p2"},
		{Op: OpCloseTab, TabID: "w1:t2"},
		{Op: OpRenameTab, Tab: 1, Label: "main"},
		{Op: OpSplit, Tab: 1, From: "p-1", Pane: "p-3", Direction: "right", Ratio: 0.6, Cwd: "/tmp"},
	}
	if got := replay.Actions[:len(want)]; !reflect.DeepEqual(got, want) {
		t.Fatalf("leading actions =\n%#v\nwant:\n%#v", got, want)
	}
	if wantBase := (Base{WorkspaceID: "w1", TabID: "w1:t1", PaneID: "w1:p1", Leaf: "p-1"}); !reflect.DeepEqual(replay.Base, wantBase) {
		t.Fatalf("base = %#v, want %#v", replay.Base, wantBase)
	}
}

func TestPlanKeepsThePaneTheCallerRunsIn(t *testing.T) {
	target := busyTarget(true, "w1:p3")
	replay, err := Plan(wantLayout(), target)
	if err != nil {
		t.Fatalf("Plan error = %v", err)
	}

	want := []Action{
		{Op: OpCloseTab, TabID: "w1:t1"},
		{Op: OpRenameTab, Tab: 1, Label: "main"},
	}
	if got := replay.Actions[:len(want)]; !reflect.DeepEqual(got, want) {
		t.Fatalf("leading actions =\n%#v\nwant:\n%#v", got, want)
	}
	if wantBase := (Base{WorkspaceID: "w1", TabID: "w1:t2", PaneID: "w1:p3", Leaf: "p-1"}); !reflect.DeepEqual(replay.Base, wantBase) {
		t.Fatalf("base = %#v, want the caller's pane to be kept", replay.Base)
	}
}

func TestPlanCreatesANewWorkspaceWhenTheTargetHasNone(t *testing.T) {
	replay, err := Plan(wantLayout(), Target{})
	if err != nil {
		t.Fatalf("Plan error = %v", err)
	}

	want := []Action{
		{Op: OpCreateWorkspace, Tab: 1, Label: "project", Pane: "p-1", Cwd: "/home/me/project"},
		{Op: OpRenameTab, Tab: 1, Label: "main"},
		{Op: OpSplit, Tab: 1, From: "p-1", Pane: "p-3", Direction: "right", Ratio: 0.6, Cwd: "/tmp"},
	}
	if got := replay.Actions[:len(want)]; !reflect.DeepEqual(got, want) {
		t.Fatalf("leading actions =\n%#v\nwant:\n%#v", got, want)
	}
	if !reflect.DeepEqual(replay.Base, Base{}) {
		t.Fatalf("base = %#v, want nothing reused in a new workspace", replay.Base)
	}

	last := replay.Actions[len(replay.Actions)-3:]
	wantFocus := []Action{
		{Op: OpFocusWorkspace, WorkspaceID: "", Label: "project"},
		{Op: OpFocusTab, Tab: 1, TabLabel: "main"},
		{Op: OpFocusAgent, AgentName: "pi"},
	}
	if !reflect.DeepEqual(last, wantFocus) {
		t.Fatalf("trailing actions = %#v, want %#v", last, wantFocus)
	}
}

func TestPlanFocusesTheRecordedPaneOnly(t *testing.T) {
	document := wantLayout()
	document.Workspace.Tabs[0].FocusedPaneID = "p-2"

	replay, err := Plan(document, emptyTarget())
	if err != nil {
		t.Fatalf("Plan error = %v", err)
	}

	var focused []string
	for _, action := range replay.Actions {
		if action.Op == OpSplit && action.Focus {
			focused = append(focused, action.Pane)
		}
	}
	if want := []string{"p-2"}; !reflect.DeepEqual(focused, want) {
		t.Fatalf("focused panes = %v, want only %v", focused, want)
	}

	last := replay.Actions[len(replay.Actions)-1]
	if last.Op != OpFocusTab || last.Tab != 1 {
		t.Fatalf("last action = %#v, want the recorded tab focused last", last)
	}
}

func TestPlanNamesReplayedAgentsDeterministically(t *testing.T) {
	document := wantLayout()
	document.Workspace.Tabs[0].Root.First.First.Agent = &Agent{Kind: "pi", Name: "Reviewer One"}

	replay, err := Plan(document, Target{Agents: []string{"reviewer-one"}})
	if err != nil {
		t.Fatalf("Plan error = %v", err)
	}

	names := map[string]string{}
	for _, action := range replay.Actions {
		if action.Op == OpStartAgent {
			names[action.Pane] = action.AgentName
		}
	}
	if want := map[string]string{"p-1": "reviewer-one-2", "p-4": "claude"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("agent names = %v, want %v", names, want)
	}
}

func TestPlanRejectsAMalformedLayout(t *testing.T) {
	tests := []struct {
		name   string
		change func(*Layout)
		want   string
	}{
		{
			name:   "a leaf without a pane id",
			change: func(document *Layout) { document.Workspace.Tabs[1].Root.PaneID = "" },
			want:   "no pane id",
		},
		{
			name:   "a split without two children",
			change: func(document *Layout) { document.Workspace.Tabs[0].Root.Second = nil },
			want:   "two children",
		},
		{
			name:   "a pane id used twice",
			change: func(document *Layout) { document.Workspace.Tabs[1].Root.PaneID = "p-1" },
			want:   "twice",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			document := wantLayout()
			tt.change(&document)

			_, err := Plan(document, emptyTarget())

			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Plan error = %v, want it to mention %q", err, tt.want)
			}
		})
	}
}

func TestPlanTreatsAnAgentWithoutAKindAsAPlainPane(t *testing.T) {
	document := wantLayout()
	document.Workspace.Tabs[1].Root.Agent = &Agent{State: "idle"}

	replay, err := Plan(document, emptyTarget())
	if err != nil {
		t.Fatalf("Plan error = %v, want nil", err)
	}

	for _, action := range replay.Actions {
		if action.Op == OpStartAgent && action.Pane == "p-4" {
			t.Fatalf("a pane whose agent has no kind produced %#v", action)
		}
	}
}

func TestRenderPlanPrintsTheOrderedSteps(t *testing.T) {
	replay, err := Plan(wantLayout(), emptyTarget())
	if err != nil {
		t.Fatalf("Plan error = %v", err)
	}

	want := []string{
		"plan for layout 'work' into workspace 'project' · 2 tabs · 4 panes",
		"1. split right 0.60 in p-1 · new pane p-3 · cwd /tmp",
		"2. split right 0.50 in p-1 · new pane p-2 · cwd /home/me/project",
		"3. rename p-1 'editor'",
		"4. start agent pi in p-1 as pi",
		"5. create tab 2 'side' · cwd /tmp",
		"6. start agent claude in p-4 as claude",
		"7. focus tab 1 'main'",
		"8. focus agent pi",
		"",
	}
	if got, wantText := RenderPlan(replay), strings.Join(want, "\n"); got != wantText {
		t.Fatalf("RenderPlan =\n%s\nwant:\n%s", got, wantText)
	}
}
