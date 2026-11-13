package layout

import (
	"strings"
	"testing"
)

func TestShowPrintsTheHeaderThenTheSameTreeAsSummary(t *testing.T) {
	document := wantLayout()
	document.CapturedAt = "2026-01-02T03:04:05Z"
	path := "/tmp/layouts/work.json"

	got := Show(document, path, ScopeGlobal)
	summary := Summary(document, path)

	wantHeader := strings.Join([]string{
		"layout 'work' · global · /tmp/layouts/work.json",
		"schema_version 1",
		"captured 2026-01-02T03:04:05Z",
		"",
	}, "\n")
	if !strings.HasPrefix(got, wantHeader) {
		t.Fatalf("Show =\n%s\nwant it to start with:\n%s", got, wantHeader)
	}

	tree := got[strings.Index(got, "workspace "):]
	wantTree := summary[strings.Index(summary, "workspace "):]
	if tree != wantTree {
		t.Fatalf("Show tree =\n%s\nwant:\n%s", tree, wantTree)
	}
}

func TestShowOmitsACapturedTimestampTheDocumentDoesNotHold(t *testing.T) {
	got := Show(wantLayout(), "/tmp/layouts/work.json", ScopeProject)

	if strings.Contains(got, "captured") {
		t.Fatalf("Show = %q, want no captured line", got)
	}
	if !strings.HasPrefix(got, "layout 'work' · project · /tmp/layouts/work.json\nschema_version 1\n") {
		t.Fatalf("Show = %q, want the name, scope and schema first", got)
	}
}

func TestShowNamesTheSessionOfASessionLayout(t *testing.T) {
	document := Layout{
		Name:          "fork",
		Session:       "work",
		SchemaVersion: CurrentSchema,
		Workspaces: []Workspace{
			{Label: "project", Tabs: []Tab{{Root: Node{Type: TypePane, PaneID: "w1:p1"}}}},
			{Label: "api", Tabs: []Tab{{Root: Node{Type: TypePane, PaneID: "w2:p1"}}}},
		},
	}

	got := Show(document, "/layouts/fork.json", ScopeGlobal)

	want := strings.Join([]string{
		"layout 'fork' · global · /layouts/fork.json",
		"schema_version 1",
		"session 'work' · 2 workspaces · 2 tabs · 2 panes",
		"workspace 'project' · 1 tab · 1 pane",
		"tab 1 · 1 pane",
		"pane w1:p1",
		"workspace 'api' · 1 tab · 1 pane",
		"tab 1 · 1 pane",
		"pane w2:p1",
		"",
	}, "\n")
	if got != want {
		t.Fatalf("Show =\n%s\nwant:\n%s", got, want)
	}
}

func TestScopeOfNamesWhereAResolvedLayoutLives(t *testing.T) {
	sources := Sources{
		Global:  "/home/me/.config/herdr/herdr-suite/layouts",
		Project: "/repo/.herdr/layouts",
		Root:    "/repo",
		Index:   "/repo/.herdr/layouts.toml",
	}

	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "the global store", path: "/home/me/.config/herdr/herdr-suite/layouts/work.json", want: ScopeGlobal},
		{name: "the project layouts directory", path: "/repo/.herdr/layouts/api.json", want: ScopeProject},
		{name: "a file the project index names", path: "/repo/.herdr/other/api.json", want: ScopeProject},
		{name: "a file outside both", path: "/tmp/hand/made.json", want: ScopePath},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ScopeOf(sources, tt.path); got != tt.want {
				t.Fatalf("ScopeOf(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestScopeOfNamesAPathWhenNoProjectIsDeclared(t *testing.T) {
	sources := Sources{Global: "/home/me/.config/herdr/herdr-suite/layouts"}

	if got := ScopeOf(sources, "/tmp/hand/made.json"); got != ScopePath {
		t.Fatalf("ScopeOf = %q, want %q", got, ScopePath)
	}
}

func TestCapturedAtSurvivesAReadAndWrite(t *testing.T) {
	data := []byte(`{
  "schema_version": 1,
  "name": "old",
  "captured_at": "2026-01-02T03:04:05Z",
  "workspace": {"label": "project", "tabs": []}
}`)

	document, err := Unmarshal(data)
	if err != nil {
		t.Fatalf("Unmarshal error = %v", err)
	}
	if document.CapturedAt != "2026-01-02T03:04:05Z" {
		t.Fatalf("CapturedAt = %q, want the stored timestamp", document.CapturedAt)
	}
	encoded, err := Marshal(document)
	if err != nil {
		t.Fatalf("Marshal error = %v", err)
	}
	if !strings.Contains(string(encoded), `"captured_at": "2026-01-02T03:04:05Z"`) {
		t.Fatalf("Marshal = %s, want the timestamp kept", encoded)
	}
}
