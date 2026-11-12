package layout

import (
	"reflect"
	"strings"
	"testing"

	"github.com/WilberC/herdr-suite/internal/herdrmock"
)

func sessionSpace() herdrmock.Space {
	space := spaceFixture()
	space.Workspaces = append(space.Workspaces, herdrmock.Workspace{
		ID:          "w-2",
		Label:       "api",
		ActiveTabID: "t-3",
		TabCount:    1,
		PaneCount:   2,
		Tabs: []herdrmock.Tab{
			{
				ID:            "t-3",
				Label:         "main",
				Focused:       true,
				FocusedPaneID: "p-5",
				Panes: []herdrmock.Pane{
					{ID: "p-5", WorkspaceID: "w-2", TabID: "t-3", Cwd: "/srv/api", Agent: "pi", AgentName: "api", AgentState: "idle"},
					{ID: "p-6", WorkspaceID: "w-2", TabID: "t-3", Cwd: "/srv/api"},
				},
				Root: herdrmock.LayoutNode{
					Direction: "down",
					Ratio:     0.7,
					Children:  []herdrmock.LayoutNode{{PaneID: "p-5"}, {PaneID: "p-6"}},
				},
			},
		},
	})
	return space
}

func sessionLayout() Layout {
	return Layout{
		Name:    "session",
		Session: "work",
		Workspaces: []Workspace{
			wantLayout().Workspace,
			{
				Label: "api",
				Tabs: []Tab{
					{
						Label:         "main",
						Focused:       true,
						FocusedPaneID: "p-5",
						Root: Node{
							Type:      TypeSplit,
							Direction: "down",
							Ratio:     0.7,
							First: &Node{
								Type:   TypePane,
								PaneID: "p-5",
								Cwd:    "/srv/api",
								Agent:  &Agent{Kind: "pi", Name: "api", State: "idle"},
							},
							Second: &Node{
								Type:   TypePane,
								PaneID: "p-6",
								Cwd:    "/srv/api",
							},
						},
					},
				},
			},
		},
	}
}

func TestCaptureSessionStoresEveryWorkspace(t *testing.T) {
	space := sessionSpace()

	got, err := CaptureSession("session", space)
	if err != nil {
		t.Fatalf("CaptureSession error = %v, want nil", err)
	}

	if want := sessionLayout(); !reflect.DeepEqual(got, want) {
		t.Fatalf("CaptureSession = %#v, want %#v", got, want)
	}
	if !got.Multi() {
		t.Fatalf("Multi() = false, want true for a session document")
	}
	if spaces := got.Spaces(); len(spaces) != 2 {
		t.Fatalf("Spaces() holds %d workspaces, want 2", len(spaces))
	}
}

func TestCaptureSessionRefusesASessionWithoutWorkspaces(t *testing.T) {
	_, err := CaptureSession("session", herdrmock.Space{})

	if err == nil || !strings.Contains(err.Error(), "no workspaces") {
		t.Fatalf("CaptureSession error = %v, want a refusal naming the missing workspaces", err)
	}
}

func TestSessionDocumentRoundTripsThroughTheFileForm(t *testing.T) {
	want := sessionLayout()

	data, err := Marshal(want)
	if err != nil {
		t.Fatalf("Marshal error = %v, want nil", err)
	}
	for _, fragment := range []string{`"workspaces"`, `"name": "session"`, `"session": "work"`} {
		if !strings.Contains(string(data), fragment) {
			t.Fatalf("stored document does not contain %q:\n%s", fragment, data)
		}
	}
	if strings.Contains(string(data), `"workspace":`) {
		t.Fatalf("a session document stored a single workspace field:\n%s", data)
	}

	got, err := Unmarshal(data)
	if err != nil {
		t.Fatalf("Unmarshal error = %v, want nil", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip = %#v, want %#v", got, want)
	}
}

func TestSingleWorkspaceDocumentKeepsItsStoredForm(t *testing.T) {
	stored := `{
  "name": "work",
  "session": "work",
  "workspace": {
    "label": "project",
    "tabs": [
      {
        "label": "main",
        "focused": true,
        "focused_pane_id": "p-1",
        "root": {"type": "pane", "pane_id": "p-1", "cwd": "/home/me/project"}
      }
    ]
  }
}
`
	document, err := Unmarshal([]byte(stored))
	if err != nil {
		t.Fatalf("Unmarshal error = %v, want nil", err)
	}
	if document.Multi() {
		t.Fatalf("Multi() = true, want false for a single-workspace document")
	}
	if document.Workspace.Label != "project" || len(document.Workspace.Tabs) != 1 {
		t.Fatalf("document = %#v, want the stored workspace", document)
	}

	data, err := Marshal(document)
	if err != nil {
		t.Fatalf("Marshal error = %v, want nil", err)
	}
	if !strings.Contains(string(data), `"workspace":`) {
		t.Fatalf("a single-workspace document lost its stored form:\n%s", data)
	}
	if strings.Contains(string(data), `"workspaces"`) {
		t.Fatalf("a single-workspace document gained a session list:\n%s", data)
	}
	again, err := Unmarshal(data)
	if err != nil {
		t.Fatalf("Unmarshal of the re-encoded document error = %v, want nil", err)
	}
	if !reflect.DeepEqual(again, document) {
		t.Fatalf("re-encoded document = %#v, want %#v", again, document)
	}
}

func TestUnmarshalRefusesADocumentWithBothForms(t *testing.T) {
	stored := `{"name": "mixed", "workspace": {"label": "a", "tabs": []}, "workspaces": [{"label": "b", "tabs": []}]}`

	_, err := Unmarshal([]byte(stored))

	if err == nil || !strings.Contains(err.Error(), "both") {
		t.Fatalf("Unmarshal error = %v, want a refusal naming both forms", err)
	}
}

func TestUnmarshalRefusesADocumentWithoutAWorkspace(t *testing.T) {
	_, err := Unmarshal([]byte(`{"name": "empty"}`))

	if err == nil || !strings.Contains(err.Error(), "no workspace") {
		t.Fatalf("Unmarshal error = %v, want a refusal naming the missing workspace", err)
	}
}

func TestSummaryPrintsEveryWorkspaceOfASessionLayout(t *testing.T) {
	got := Summary(sessionLayout(), "/layouts/session.json")

	want := strings.Join([]string{
		"saved layout 'session' at /layouts/session.json",
		"session 'work' · 2 workspaces · 3 tabs · 6 panes",
		"workspace 'project' · 2 tabs · 4 panes",
		"tab 1 'main' · 3 panes · focus p-1",
		"split right 0.60",
		"├─ split right 0.50",
		"│  ├─ pane p-1 · 'editor' · pi working · /home/me/project",
		"│  └─ pane p-2 · /home/me/project",
		"└─ pane p-3 · /tmp",
		"tab 2 'side' · 1 pane",
		"pane p-4 · claude idle · /tmp",
		"workspace 'api' · 1 tab · 2 panes",
		"tab 1 'main' · 2 panes · focus p-5",
		"split down 0.70",
		"├─ pane p-5 · pi 'api' idle · /srv/api",
		"└─ pane p-6 · /srv/api",
		"",
	}, "\n")

	if got != want {
		t.Fatalf("Summary =\n%s\nwant:\n%s", got, want)
	}
}

func TestSessionReplaySummaryPrintsEachWorkspace(t *testing.T) {
	plan, err := PlanSession(sessionLayout(), Target{})
	if err != nil {
		t.Fatalf("PlanSession error = %v, want nil", err)
	}

	got := SessionReplaySummary(plan)

	want := strings.Join([]string{
		"loaded layout 'session' into 2 workspaces · 3 tabs · 6 panes · 18 steps",
		"workspace 'project' · 2 tabs · 4 panes · 11 steps",
		"workspace 'api' · 1 tab · 2 panes · 7 steps",
		"",
	}, "\n")
	if got != want {
		t.Fatalf("SessionReplaySummary =\n%s\nwant:\n%s", got, want)
	}
}

func TestRenderSessionPlanPrintsOneSectionPerWorkspace(t *testing.T) {
	plan, err := PlanSession(sessionLayout(), Target{})
	if err != nil {
		t.Fatalf("PlanSession error = %v, want nil", err)
	}

	rendered := RenderSessionPlan(plan)
	lines := strings.Split(rendered, "\n")
	want := []string{
		"session plan for layout 'session' · 2 workspaces · 3 tabs · 6 panes",
		"workspace 1 'project' · 2 tabs · 4 panes · 11 steps",
		"1. create workspace 'project' · cwd /home/me/project",
	}
	if len(lines) < len(want) {
		t.Fatalf("RenderSessionPlan printed %d lines, want at least %d:\n%s", len(lines), len(want), rendered)
	}
	for index, wantLine := range want {
		if lines[index] != wantLine {
			t.Fatalf("line %d = %q, want %q\n%s", index+1, lines[index], wantLine, rendered)
		}
	}
	for _, fragment := range []string{
		"workspace 2 'api' · 1 tab · 2 panes · 7 steps",
		"1. create workspace 'api' · cwd /srv/api",
		"9. focus workspace 'project'",
		"7. focus agent api",
	} {
		if !strings.Contains(rendered, fragment) {
			t.Fatalf("RenderSessionPlan does not contain %q:\n%s", fragment, rendered)
		}
	}
}
