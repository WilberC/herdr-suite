package layout

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestSummaryPrintsTheStableSnapshotReport(t *testing.T) {
	got := Summary(wantLayout(), "/tmp/layouts/work.json")

	want := strings.Join([]string{
		"saved layout 'work' at /tmp/layouts/work.json",
		"workspace 'project' · 2 tabs · 4 panes",
		"tab 1 'main' · 3 panes · focus p-1",
		"split right 0.60",
		"├─ split right 0.50",
		"│  ├─ pane p-1 · 'editor' · pi working · /home/me/project",
		"│  └─ pane p-2 · /home/me/project",
		"└─ pane p-3 · /tmp",
		"tab 2 'side' · 1 pane",
		"pane p-4 · claude idle · /tmp",
		"",
	}, "\n")

	if got != want {
		t.Fatalf("Summary =\n%s\nwant:\n%s", got, want)
	}
}

func TestSummaryCountsASinglePaneWorkspace(t *testing.T) {
	document := Layout{
		Name: "solo",
		Workspace: Workspace{
			Label: "dotfiles",
			Tabs: []Tab{
				{
					Label:         "main",
					Focused:       true,
					FocusedPaneID: "w35:p7",
					Root: Node{
						Type:   TypePane,
						PaneID: "w35:p7",
						Agent:  &Agent{Kind: "pi", Name: "review", State: "idle"},
						Cwd:    "/home/me/dotfiles",
					},
				},
			},
		},
	}

	got := Summary(document, "/layouts/solo.json")

	want := strings.Join([]string{
		"saved layout 'solo' at /layouts/solo.json",
		"workspace 'dotfiles' · 1 tab · 1 pane",
		"tab 1 'main' · 1 pane · focus w35:p7",
		"pane w35:p7 · pi 'review' idle · /home/me/dotfiles",
		"",
	}, "\n")

	if got != want {
		t.Fatalf("Summary =\n%s\nwant:\n%s", got, want)
	}
}

func TestWritePrintsTheLayoutDocumentForScripts(t *testing.T) {
	var buf bytes.Buffer

	if err := Write(&buf, wantLayout()); err != nil {
		t.Fatalf("Write error = %v, want nil", err)
	}

	var document map[string]any
	if err := json.Unmarshal(buf.Bytes(), &document); err != nil {
		t.Fatalf("Write output is not JSON: %v\n%s", err, buf.String())
	}
	workspace, ok := document["workspace"].(map[string]any)
	if !ok {
		t.Fatalf("document has no workspace object:\n%s", buf.String())
	}
	if got, want := workspace["label"], "project"; got != want {
		t.Fatalf("workspace label = %v, want %v", got, want)
	}
	tabs, ok := workspace["tabs"].([]any)
	if !ok || len(tabs) != 2 {
		t.Fatalf("document tabs = %v, want the two saved tabs", workspace["tabs"])
	}
}
