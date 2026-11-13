package main

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/WilberC/herdr-suite/internal/herdrmock"
	"github.com/WilberC/herdr-suite/internal/layout"
)

func loadFixture() herdrmock.Space {
	return herdrmock.Space{
		FocusedWorkspaceID: "w1",
		FocusedTabID:       "w1:t1",
		FocusedPaneID:      "w1:p1",
		Workspaces: []herdrmock.Workspace{
			{
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
						Panes: []herdrmock.Pane{
							{ID: "w1:p1", WorkspaceID: "w1", TabID: "w1:t1", Cwd: "/home/me/project"},
						},
						Root: herdrmock.LayoutNode{PaneID: "w1:p1"},
					},
				},
			},
		},
	}
}

func busyFixture() herdrmock.Space {
	space := loadFixture()
	space.Workspaces[0].TabCount = 2
	space.Workspaces[0].PaneCount = 3
	space.Workspaces[0].Tabs[0].Panes = append(space.Workspaces[0].Tabs[0].Panes,
		herdrmock.Pane{ID: "w1:p2", WorkspaceID: "w1", TabID: "w1:t1", Cwd: "/tmp"})
	space.Workspaces[0].Tabs = append(space.Workspaces[0].Tabs, herdrmock.Tab{
		ID:            "w1:t2",
		Label:         "two",
		FocusedPaneID: "w1:p3",
		Panes: []herdrmock.Pane{
			{ID: "w1:p3", WorkspaceID: "w1", TabID: "w1:t2", Cwd: "/var/tmp"},
		},
		Root: herdrmock.LayoutNode{PaneID: "w1:p3"},
	})
	return space
}

func newLoadRun(t *testing.T, sessions ...herdrmock.Session) *spaceRun {
	t.Helper()
	if len(sessions) == 0 {
		sessions = []herdrmock.Session{{Name: "default", Running: true, Default: true}}
	}
	run := newSpaceRun(t, sessions...)
	run.client.Spaces[""] = loadFixture()
	return run
}

// saveWork stores the richer fixture as the layout "work", so a load has a real
// document saved by the save path.
func (r *spaceRun) saveWork(t *testing.T) {
	t.Helper()
	r.client.Spaces[""] = spaceFixture()
	if code := r.run(t, "space", "save", "work"); code != 0 {
		t.Fatalf("space save exit = %d\nstderr:\n%s", code, r.stderr)
	}
	r.client.Spaces[""] = loadFixture()
	r.reset()
}

func (r *spaceRun) reset() {
	r.stdout.Reset()
	r.stderr.Reset()
}

func TestSpaceLoadPrintsTheReplayPlanAndTouchesNothing(t *testing.T) {
	run := newLoadRun(t)
	run.saveWork(t)

	code := run.run(t, "space", "load", "work", "--dry-run")

	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", code, run.stderr)
	}
	want := strings.Join([]string{
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
	}, "\n")
	if got := run.stdout.String(); got != want {
		t.Fatalf("stdout =\n%s\nwant:\n%s", got, want)
	}
	if run.stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", run.stderr.String())
	}
	if ops := run.client.Ops(); len(ops) != 0 {
		t.Fatalf("dry run issued %d calls, want none: %#v", len(ops), ops)
	}
}

func TestSpaceLoadReplaysTheStoredTree(t *testing.T) {
	run := newLoadRun(t)
	run.saveWork(t)

	code := run.run(t, "space", "load", "work")

	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", code, run.stderr)
	}
	if want := "loaded layout 'work' into workspace 'project' · 2 tabs · 4 panes · 8 steps\n"; run.stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", run.stdout.String(), want)
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
	if got := run.client.Ops(); !reflect.DeepEqual(got, want) {
		t.Fatalf("ops =\n%#v\nwant:\n%#v", got, want)
	}
}

func TestSpaceLoadRoundTripComposesTheSavedTree(t *testing.T) {
	run := newLoadRun(t)
	run.saveWork(t)
	stored := readLayout(t, run.layoutPath("work"))

	if code := run.run(t, "space", "load", "work"); code != 0 {
		t.Fatalf("load exit = %d\nstderr:\n%s", code, run.stderr)
	}

	space := composeSpace(t, run.client.Ops(), "w1", "w1:t1", "w1:p1", "project")
	loaded, err := layout.Capture("loaded", space, space.Workspaces[0])
	if err != nil {
		t.Fatalf("Capture the composed session: %v", err)
	}

	if len(loaded.Workspace.Tabs) != len(stored.Workspace.Tabs) {
		t.Fatalf("loaded %d tabs, want %d", len(loaded.Workspace.Tabs), len(stored.Workspace.Tabs))
	}
	for index := range stored.Workspace.Tabs {
		wantTab, gotTab := stored.Workspace.Tabs[index], loaded.Workspace.Tabs[index]
		if gotTab.Label != wantTab.Label {
			t.Fatalf("tab %d label = %q, want %q", index+1, gotTab.Label, wantTab.Label)
		}
		if got, want := nodeShape(gotTab.Root), nodeShape(wantTab.Root); got != want {
			t.Fatalf("tab %d tree =\n%s\nwant:\n%s", index+1, got, want)
		}
	}
}

func TestSpaceLoadRefusesANonEmptyWorkspaceWithoutReplace(t *testing.T) {
	run := newLoadRun(t)
	run.saveWork(t)
	run.client.Spaces[""] = busyFixture()

	code := run.run(t, "space", "load", "work")

	if code != 2 {
		t.Fatalf("exit = %d, want 2\nstdout:\n%s\nstderr:\n%s", code, run.stdout, run.stderr)
	}
	for _, want := range []string{"already has 3 panes", "--replace"} {
		if !strings.Contains(run.stderr.String(), want) {
			t.Fatalf("stderr = %q, want it to contain %q", run.stderr.String(), want)
		}
	}
	if ops := run.client.Ops(); len(ops) != 0 {
		t.Fatalf("a refused load issued calls: %#v", ops)
	}
}

func TestSpaceLoadReplacesTheDestinationWhenAsked(t *testing.T) {
	run := newLoadRun(t)
	run.saveWork(t)
	run.client.Spaces[""] = busyFixture()

	code := run.run(t, "space", "load", "work", "--replace")

	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", code, run.stderr)
	}
	wantNote := "replacing workspace 'project': closing 2 panes (w1:p2, w1:p3) and 1 tab (w1:t2)\n"
	if run.stderr.String() != wantNote {
		t.Fatalf("stderr = %q, want %q", run.stderr.String(), wantNote)
	}
	ops := run.client.Ops()
	want := []herdrmock.Op{
		{Method: "ClosePane", PaneID: "w1:p2"},
		{Method: "CloseTab", TabID: "w1:t2"},
		{Method: "SplitPane", PaneID: "w1:p1", Created: "np-1", Direction: "right", Ratio: 0.6, Cwd: "/tmp"},
	}
	if len(ops) < len(want) || !reflect.DeepEqual(ops[:len(want)], want) {
		t.Fatalf("leading ops =\n%#v\nwant:\n%#v", ops, want)
	}
}

func TestSpaceLoadCreatesANewWorkspaceWhenAsked(t *testing.T) {
	run := newLoadRun(t)
	run.saveWork(t)

	code := run.run(t, "space", "load", "work", "--new-workspace")

	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", code, run.stderr)
	}
	ops := run.client.Ops()
	want := herdrmock.Op{Method: "CreateWorkspace", WorkspaceID: "nw-1", TabID: "nt-1", Created: "np-1", Cwd: "/home/me/project", Label: "project"}
	if !reflect.DeepEqual(ops[0], want) {
		t.Fatalf("first op = %#v, want %#v", ops[0], want)
	}
	if last := ops[len(ops)-3]; last.Method != "FocusWorkspace" || last.WorkspaceID != "nw-1" {
		t.Fatalf("focus op = %#v, want the new workspace focused", last)
	}
}

func TestSpaceLoadTargetsAnExplicitSession(t *testing.T) {
	work := loadFixture()
	work.Session = "work"
	run := newLoadRun(t,
		herdrmock.Session{Name: "default", Running: true, Default: true},
		herdrmock.Session{Name: "work", Running: true, Socket: "/tmp/work.sock"},
	)
	run.saveWork(t)
	run.client.Spaces["work"] = work

	code := run.run(t, "space", "load", "work", "--session", "work")

	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", code, run.stderr)
	}
	target := herdrmock.Target{Name: "work", Socket: "/tmp/work.sock"}
	for _, op := range run.client.Ops() {
		if op.Method == "Snapshot" {
			continue
		}
		if op.Target != target {
			t.Fatalf("op %#v targeted %#v, want %#v", op, op.Target, target)
		}
	}
}

func TestSpaceLoadAsksWhichWorkspaceWhenTheLabelsDiffer(t *testing.T) {
	run := newLoadRun(t)
	run.saveWork(t)
	space := loadFixture()
	space.Workspaces[0].Label = "default"
	space.Workspaces = append(space.Workspaces, herdrmock.Workspace{
		ID:          "w2",
		Label:       "api",
		ActiveTabID: "w2:t1",
		TabCount:    1,
		PaneCount:   1,
		Tabs: []herdrmock.Tab{{
			ID:            "w2:t1",
			Label:         "main",
			Panes:         []herdrmock.Pane{{ID: "w2:p1", WorkspaceID: "w2", TabID: "w2:t1", Cwd: "/srv/api"}},
			Root:          herdrmock.LayoutNode{PaneID: "w2:p1"},
			FocusedPaneID: "w2:p1",
		}},
	})
	run.client.Spaces[""] = space
	var offered []string
	run.d.interactive = true
	run.d.pick = func(workspaces []herdrmock.Workspace) (string, error) {
		for _, workspace := range workspaces {
			offered = append(offered, workspace.ID)
		}
		return "w1", nil
	}

	if code := run.run(t, "space", "load", "work"); code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", code, run.stderr)
	}
	if want := []string{"w1", "w2"}; !reflect.DeepEqual(offered, want) {
		t.Fatalf("offered workspaces = %v, want %v", offered, want)
	}
	if got := run.client.Ops()[0].PaneID; got != "w1:p1" {
		t.Fatalf("first split targeted %q, want the chosen workspace's pane", got)
	}
}

func TestSpaceLoadRefusesToGuessAWorkspaceNonInteractively(t *testing.T) {
	run := newLoadRun(t)
	run.saveWork(t)
	space := loadFixture()
	space.Workspaces[0].Label = "default"
	space.Workspaces = append(space.Workspaces, herdrmock.Workspace{ID: "w2", Label: "api"})
	run.client.Spaces[""] = space

	code := run.run(t, "space", "load", "work")

	if code != 2 {
		t.Fatalf("exit = %d, want 2\nstderr:\n%s", code, run.stderr)
	}
	for _, want := range []string{"'project'", "interactively", "--new-workspace"} {
		if !strings.Contains(run.stderr.String(), want) {
			t.Fatalf("stderr = %q, want it to contain %q", run.stderr.String(), want)
		}
	}
	if ops := run.client.Ops(); len(ops) != 0 {
		t.Fatalf("a refused load issued calls: %#v", ops)
	}
}

func TestSpaceLoadRefusesAnUnknownLayout(t *testing.T) {
	run := newLoadRun(t)

	code := run.run(t, "space", "load", "ghost")

	if code != 2 {
		t.Fatalf("exit = %d, want 2\nstdout:\n%s\nstderr:\n%s", code, run.stdout, run.stderr)
	}
	for _, want := range []string{"unknown layout 'ghost'", "hs space save ghost"} {
		if !strings.Contains(run.stderr.String(), want) {
			t.Fatalf("stderr = %q, want it to contain %q", run.stderr.String(), want)
		}
	}
	if ops := run.client.Ops(); len(ops) != 0 {
		t.Fatalf("a missing layout issued calls: %#v", ops)
	}
}

func TestSpaceLoadRejectsBadArguments(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "no layout name",
			args: []string{"space", "load"},
			want: []string{"expected one layout name", "usage: hs space load"},
		},
		{
			name: "unknown flag",
			args: []string{"space", "load", "work", "--bogus"},
			want: []string{`unknown flag "--bogus"`, "usage: hs space load"},
		},
		{
			name: "invalid name",
			args: []string{"space", "load", "work!"},
			want: []string{"invalid layout name 'work!'"},
		},
		{
			name: "missing session value",
			args: []string{"space", "load", "work", "--session"},
			want: []string{"--session needs a session name"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			run := newLoadRun(t)

			code := run.run(t, tt.args...)

			if code != 2 {
				t.Fatalf("exit = %d, want 2\nstderr:\n%s", code, run.stderr)
			}
			for _, want := range tt.want {
				if !strings.Contains(run.stderr.String(), want) {
					t.Fatalf("stderr = %q, want it to contain %q", run.stderr.String(), want)
				}
			}
			if ops := run.client.Ops(); len(ops) != 0 {
				t.Fatalf("bad arguments issued calls: %#v", ops)
			}
		})
	}
}

func TestSpaceLoadReportsTheStepThatFailed(t *testing.T) {
	run := newLoadRun(t)
	run.saveWork(t)
	run.client.Fail = func(op herdrmock.Op) error {
		if op.Method == "CreateTab" {
			return fmt.Errorf("pane limit reached")
		}
		return nil
	}

	code := run.run(t, "space", "load", "work")

	if code != 1 {
		t.Fatalf("exit = %d, want 1\nstdout:\n%s\nstderr:\n%s", code, run.stdout, run.stderr)
	}
	for _, want := range []string{
		"hs: step 5 of 8 failed",
		"create tab 2 'side'",
		"pane limit reached",
		"no transaction",
	} {
		if !strings.Contains(run.stderr.String(), want) {
			t.Fatalf("stderr = %q, want it to contain %q", run.stderr.String(), want)
		}
	}
}

func readLayout(t *testing.T, path string) layout.Layout {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	document, err := layout.Unmarshal(data)
	if err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return document
}

// shape is the split tree a replay composed, rebuilt from the calls it made.
type shape struct {
	Pane      string
	Direction string
	Ratio     float64
	First     *shape
	Second    *shape
}

type composedTab struct {
	id    string
	label string
	root  *shape
	panes []string
}

// composeSpace rebuilds the topology the recorded calls composed: every split
// expands the leaf that is its base pane and adds the created pane beside it.
func composeSpace(t *testing.T, ops []herdrmock.Op, workspaceID, baseTabID, basePane, label string) herdrmock.Space {
	t.Helper()
	tabs := map[string]*composedTab{
		baseTabID: {id: baseTabID, label: "main", root: &shape{Pane: basePane}, panes: []string{basePane}},
	}
	panes := map[string]herdrmock.Pane{
		basePane: {ID: basePane, WorkspaceID: workspaceID, TabID: baseTabID, Cwd: "/home/me/project"},
	}
	order := []string{baseTabID}

	for _, op := range ops {
		switch op.Method {
		case "CreateTab":
			tabs[op.TabID] = &composedTab{id: op.TabID, label: op.Label, root: &shape{Pane: op.Created}, panes: []string{op.Created}}
			panes[op.Created] = herdrmock.Pane{ID: op.Created, WorkspaceID: workspaceID, TabID: op.TabID, Cwd: op.Cwd}
			order = append(order, op.TabID)
		case "SplitPane":
			tab := tabHolding(tabs, op.PaneID)
			if tab == nil {
				t.Fatalf("split of %q does not belong to any tab", op.PaneID)
			}
			if !expand(tab.root, op.PaneID, op.Created, op.Direction, op.Ratio) {
				t.Fatalf("split of %q does not expand a leaf", op.PaneID)
			}
			tab.panes = append(tab.panes, op.Created)
			panes[op.Created] = herdrmock.Pane{ID: op.Created, WorkspaceID: workspaceID, TabID: tab.id, Cwd: op.Cwd}
		case "RenamePane":
			pane := panes[op.PaneID]
			pane.Label = op.Label
			panes[op.PaneID] = pane
		case "StartAgent":
			pane := panes[op.PaneID]
			pane.Agent = op.AgentKind
			panes[op.PaneID] = pane
		}
	}

	workspace := herdrmock.Workspace{ID: workspaceID, Label: label, ActiveTabID: order[0]}
	for _, id := range order {
		tab := tabs[id]
		reported := make([]herdrmock.Pane, 0, len(tab.panes))
		for _, paneID := range tab.panes {
			reported = append(reported, panes[paneID])
		}
		workspace.Tabs = append(workspace.Tabs, herdrmock.Tab{
			ID:    tab.id,
			Label: tab.label,
			Panes: reported,
			Root:  tab.root.node(),
		})
	}
	return herdrmock.Space{Workspaces: []herdrmock.Workspace{workspace}}
}

func tabHolding(tabs map[string]*composedTab, paneID string) *composedTab {
	for _, tab := range tabs {
		for _, pane := range tab.panes {
			if pane == paneID {
				return tab
			}
		}
	}
	return nil
}

func expand(node *shape, from, created, direction string, ratio float64) bool {
	if node.First == nil {
		if node.Pane != from {
			return false
		}
		*node = shape{
			Direction: direction,
			Ratio:     ratio,
			First:     &shape{Pane: from},
			Second:    &shape{Pane: created},
		}
		return true
	}
	return expand(node.First, from, created, direction, ratio) || expand(node.Second, from, created, direction, ratio)
}

func (s *shape) node() herdrmock.LayoutNode {
	if s.First == nil {
		return herdrmock.LayoutNode{PaneID: s.Pane}
	}
	return herdrmock.LayoutNode{
		Direction: s.Direction,
		Ratio:     s.Ratio,
		Children:  []herdrmock.LayoutNode{s.First.node(), s.Second.node()},
	}
}

// nodeShape is a tree rendered without pane ids, so two captures of the same
// layout compare equal even when herdr gave the panes new ids.
func nodeShape(node layout.Node) string {
	if node.Type == layout.TypePane {
		parts := []string{"pane"}
		if node.Label != "" {
			parts = append(parts, "'"+node.Label+"'")
		}
		if node.Cwd != "" {
			parts = append(parts, node.Cwd)
		}
		if node.Agent != nil {
			parts = append(parts, "agent="+node.Agent.Kind)
		}
		return "(" + strings.Join(parts, " ") + ")"
	}
	return fmt.Sprintf("split %s %.2f %s %s", node.Direction, node.Ratio, nodeShape(*node.First), nodeShape(*node.Second))
}

// TestSpaceLoadRefusesALayoutANewerBuildWrote pins the exit status of a stored
// layout this build cannot read: it is a refusal, so nothing is replayed and the
// message says which release to install.
func TestSpaceLoadRefusesALayoutANewerBuildWrote(t *testing.T) {
	run := newLoadRun(t)
	path := run.layoutPath("future")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "{\n  \"schema_version\": 2,\n  \"name\": \"future\",\n  \"workspace\": {\"label\": \"project\", \"tabs\": []}\n}\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	code := run.run(t, "space", "load", "future")

	if code != 2 {
		t.Fatalf("exit = %d, want 2\nstdout:\n%s\nstderr:\n%s", code, run.stdout, run.stderr)
	}
	for _, want := range []string{"newer herdr-suite", "update hs", path} {
		if !strings.Contains(run.stderr.String(), want) {
			t.Errorf("stderr does not contain %q; got:\n%s", want, run.stderr)
		}
	}
	for _, call := range run.client.Calls() {
		if call.Method != "ListSessions" && call.Method != "Snapshot" {
			t.Errorf("a refused load performed %v, want no replay", call)
		}
	}
}
