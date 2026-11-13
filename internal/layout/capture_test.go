package layout

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/WilberC/herdr-suite/internal/herdrmock"
)

func spaceFixture() herdrmock.Space {
	return herdrmock.Space{
		Session:            "work",
		FocusedWorkspaceID: "w-1",
		FocusedTabID:       "t-1",
		FocusedPaneID:      "p-1",
		Workspaces: []herdrmock.Workspace{
			{
				ID:          "w-1",
				Label:       "project",
				Focused:     true,
				ActiveTabID: "t-2",
				AgentState:  "working",
				TabCount:    2,
				PaneCount:   4,
				Tabs: []herdrmock.Tab{
					{
						ID:            "t-1",
						Label:         "main",
						Focused:       true,
						FocusedPaneID: "p-1",
						Panes: []herdrmock.Pane{
							{ID: "p-1", WorkspaceID: "w-1", TabID: "t-1", Label: "editor", Cwd: "/home/me/project", Agent: "pi", AgentState: "working"},
							{ID: "p-2", WorkspaceID: "w-1", TabID: "t-1", Cwd: "/home/me/project"},
							{ID: "p-3", WorkspaceID: "w-1", TabID: "t-1", Cwd: "/tmp"},
						},
						Root: herdrmock.LayoutNode{
							Direction: "right",
							Ratio:     0.6,
							Children: []herdrmock.LayoutNode{
								{
									Direction: "right",
									Ratio:     0.5,
									Children: []herdrmock.LayoutNode{
										{PaneID: "p-1"},
										{PaneID: "p-2"},
									},
								},
								{PaneID: "p-3"},
							},
						},
					},
					{
						ID:    "t-2",
						Label: "side",
						Panes: []herdrmock.Pane{
							{ID: "p-4", WorkspaceID: "w-1", TabID: "t-2", Cwd: "/tmp", Agent: "claude", AgentState: "idle"},
						},
						Root: herdrmock.LayoutNode{PaneID: "p-4"},
					},
				},
			},
		},
	}
}

func wantLayout() Layout {
	return Layout{
		Name:          "work",
		Session:       "work",
		SchemaVersion: CurrentSchema,
		Workspace: Workspace{
			Label: "project",
			Tabs: []Tab{
				{
					Label:         "main",
					Focused:       true,
					FocusedPaneID: "p-1",
					Root: Node{
						Type:      TypeSplit,
						Direction: "right",
						Ratio:     0.6,
						First: &Node{
							Type:      TypeSplit,
							Direction: "right",
							Ratio:     0.5,
							First: &Node{
								Type:   TypePane,
								PaneID: "p-1",
								Label:  "editor",
								Cwd:    "/home/me/project",
								Agent:  &Agent{Kind: "pi", State: "working"},
							},
							Second: &Node{
								Type:   TypePane,
								PaneID: "p-2",
								Cwd:    "/home/me/project",
							},
						},
						Second: &Node{
							Type:   TypePane,
							PaneID: "p-3",
							Cwd:    "/tmp",
						},
					},
				},
				{
					Label: "side",
					Root: Node{
						Type:   TypePane,
						PaneID: "p-4",
						Cwd:    "/tmp",
						Agent:  &Agent{Kind: "claude", State: "idle"},
					},
				},
			},
		},
	}
}

func TestCaptureKeepsTheSplitTreeAndEveryPaneField(t *testing.T) {
	space := spaceFixture()

	got, err := Capture("work", space, space.Workspaces[0])
	if err != nil {
		t.Fatalf("Capture error = %v, want nil", err)
	}

	if want := wantLayout(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Capture = %#v, want %#v", got, want)
	}
}

func TestCaptureRejectsASplitWithoutTwoChildren(t *testing.T) {
	space := spaceFixture()
	space.Workspaces[0].Tabs[0].Root.Children = space.Workspaces[0].Tabs[0].Root.Children[:1]

	_, err := Capture("work", space, space.Workspaces[0])

	if err == nil || !strings.Contains(err.Error(), "tab 'main'") {
		t.Fatalf("Capture error = %v, want it to name the malformed tab", err)
	}
}

func TestCaptureRejectsALeafThatNamesNoKnownPane(t *testing.T) {
	space := spaceFixture()
	space.Workspaces[0].Tabs[1].Panes = nil

	_, err := Capture("work", space, space.Workspaces[0])

	if err == nil || !strings.Contains(err.Error(), "p-4") {
		t.Fatalf("Capture error = %v, want it to name the missing pane", err)
	}
}

func TestLayoutRoundTripThroughTheFileFormat(t *testing.T) {
	want := wantLayout()

	data, err := Marshal(want)
	if err != nil {
		t.Fatalf("Marshal error = %v, want nil", err)
	}
	if !strings.HasSuffix(string(data), "\n") {
		t.Fatalf("Marshal output does not end with a newline:\n%s", data)
	}
	for _, want := range []string{`"name": "work"`, `"type": "split"`, `"first"`, `"second"`, `"direction": "right"`, `"ratio": 0.6`} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("Marshal output does not contain %q:\n%s", want, data)
		}
	}

	got, err := Unmarshal(data)
	if err != nil {
		t.Fatalf("Unmarshal error = %v, want nil", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip = %#v, want %#v", got, want)
	}
}

func TestWriteMatchesMarshal(t *testing.T) {
	var buf bytes.Buffer

	if err := Write(&buf, wantLayout()); err != nil {
		t.Fatalf("Write error = %v, want nil", err)
	}
	data, err := Marshal(wantLayout())
	if err != nil {
		t.Fatalf("Marshal error = %v, want nil", err)
	}
	if !bytes.Equal(buf.Bytes(), data) {
		t.Fatalf("Write = %q, want %q", buf.String(), data)
	}
}

func TestUnmarshalRejectsAnInvalidDocument(t *testing.T) {
	if _, err := Unmarshal([]byte("not json")); err == nil {
		t.Fatalf("Unmarshal error = nil, want a parse failure")
	}
}

func TestCaptureRecordsNoAgentForAPaneThatOnlyReportsAStatus(t *testing.T) {
	space := spaceFixture()
	plain := &space.Workspaces[0].Tabs[1].Panes[0]
	plain.Agent, plain.AgentState = "", "idle"

	got, err := Capture("work", space, space.Workspaces[0])
	if err != nil {
		t.Fatalf("Capture error = %v, want nil", err)
	}

	if agent := got.Workspace.Tabs[1].Root.Agent; agent != nil {
		t.Fatalf("captured agent = %#v, want none for a plain pane that reports a status", agent)
	}
}
