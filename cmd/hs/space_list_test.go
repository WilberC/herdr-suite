package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/WilberC/herdr-suite/internal/layout"
)

// stampLayout gives a stored layout a fixed mtime, so the list output is exact.
func stampLayout(t *testing.T, path string, at time.Time) {
	t.Helper()
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatalf("stamp %s: %v", path, err)
	}
}

// declareProject writes the project layout declaration under dir.
func declareProject(t *testing.T, dir string, document layout.Layout) string {
	t.Helper()
	body, err := layout.Marshal(document)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	path := filepath.Join(dir, ".herdr", "layouts", document.Name+".json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create the project layouts: %v", err)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatalf("write the project layout: %v", err)
	}
	return path
}

func projectLayoutNamed(name, workspace string) layout.Layout {
	document := layout.Layout{
		Name: name,
		Workspace: layout.Workspace{
			Label: workspace,
			Tabs: []layout.Tab{{
				Label:         "main",
				Focused:       true,
				FocusedPaneID: "w1:p1",
				Root:          layout.Node{Type: layout.TypePane, PaneID: "w1:p1", Cwd: "/srv/" + workspace},
			}},
		},
	}
	return document
}

func TestSpaceListPrintsTheVisibleLayouts(t *testing.T) {
	run := newSpaceRun(t)
	if code := run.run(t, "space", "save", "work"); code != 0 {
		t.Fatalf("space save exit = %d\nstderr:\n%s", code, run.stderr)
	}
	workPath := run.layoutPath("work")
	stampLayout(t, workPath, time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
	projectPath := declareProject(t, run.configDir, projectLayoutNamed("api", "api"))
	stampLayout(t, projectPath, time.Date(2025, 12, 31, 23, 59, 0, 0, time.UTC))
	run.reset()

	code := run.run(t, "space", "list")

	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", code, run.stderr)
	}
	want := strings.Join([]string{
		"NAME  SCOPE    WORKSPACE  TABS  PANES  MODIFIED",
		"api   project  api        1     1      2025-12-31 23:59",
		"work  global   project    2     4      2026-01-02 03:04",
		"",
	}, "\n")
	if got := run.stdout.String(); got != want {
		t.Fatalf("stdout =\n%s\nwant:\n%s", got, want)
	}
	if run.stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", run.stderr.String())
	}
}

func TestSpaceListPrintsTheLayoutsAsJSON(t *testing.T) {
	run := newSpaceRun(t)
	if code := run.run(t, "space", "save", "work"); code != 0 {
		t.Fatalf("space save exit = %d\nstderr:\n%s", code, run.stderr)
	}
	run.reset()

	code := run.run(t, "space", "list", "--json")

	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", code, run.stderr)
	}
	var document layout.ListDocument
	if err := json.Unmarshal(run.stdout.Bytes(), &document); err != nil {
		t.Fatalf("stdout is not the layout list: %v\n%s", err, run.stdout.String())
	}
	if len(document.Layouts) != 1 {
		t.Fatalf("document holds %d layouts, want 1:\n%s", len(document.Layouts), run.stdout.String())
	}
	row := document.Layouts[0]
	if row.Name != "work" || row.Scope != "global" || row.Workspace != "project" {
		t.Fatalf("row = %#v, want the stored layout", row)
	}
	if row.Tabs != 2 || row.Panes != 4 {
		t.Fatalf("row = %#v, want the stored counts", row)
	}
}

func TestSpaceListKeepsTheTableOnScreenInAPane(t *testing.T) {
	run := newSpaceRun(t)
	if code := run.run(t, "space", "save", "work"); code != 0 {
		t.Fatalf("space save exit = %d\nstderr:\n%s", code, run.stderr)
	}
	run.reset()
	run.d.in = strings.NewReader("\n")

	code := run.run(t, "space", "list", "--pane")

	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", code, run.stderr)
	}
	for _, want := range []string{"NAME  SCOPE", "work  global", "press enter to close"} {
		if !strings.Contains(run.stdout.String(), want) {
			t.Fatalf("stdout = %q, want it to contain %q", run.stdout.String(), want)
		}
	}
}

func TestSpaceListPrintsOnlyTheHeaderWithoutStoredLayouts(t *testing.T) {
	run := newSpaceRun(t)

	code := run.run(t, "space", "list")

	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", code, run.stderr)
	}
	if want := "NAME  SCOPE  WORKSPACE  TABS  PANES  MODIFIED\n"; run.stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", run.stdout.String(), want)
	}
}

func TestSpaceListFindsTheProjectFromASubdirectory(t *testing.T) {
	run := newSpaceRun(t)
	declareProject(t, run.configDir, projectLayoutNamed("api", "api"))
	deep := filepath.Join(run.configDir, "src", "deep")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatalf("create the project tree: %v", err)
	}
	run.d.cwd = func() (string, error) { return deep, nil }

	code := run.run(t, "space", "list", "--json")

	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", code, run.stderr)
	}
	var document layout.ListDocument
	if err := json.Unmarshal(run.stdout.Bytes(), &document); err != nil {
		t.Fatalf("stdout is not the layout list: %v\n%s", err, run.stdout.String())
	}
	if len(document.Layouts) != 1 || document.Layouts[0].Scope != "project" {
		t.Fatalf("document = %#v, want the project layout", document.Layouts)
	}
}

func TestSpaceListDiscoversFromTheDirectoryItIsGiven(t *testing.T) {
	run := newSpaceRun(t)
	declareProject(t, run.configDir, projectLayoutNamed("api", "api"))

	code := run.run(t, "space", "list", "--json", "--dir", run.configDir)

	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", code, run.stderr)
	}
	var document layout.ListDocument
	if err := json.Unmarshal(run.stdout.Bytes(), &document); err != nil {
		t.Fatalf("stdout is not the layout list: %v\n%s", err, run.stdout.String())
	}
	if len(document.Layouts) != 1 || document.Layouts[0].Name != "api" {
		t.Fatalf("document = %#v, want the project layout", document.Layouts)
	}
}

func TestSpaceListRejectsBadArguments(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "an unknown flag",
			args: []string{"space", "list", "--bogus"},
			want: `unknown flag "--bogus"`,
		},
		{
			name: "a positional argument",
			args: []string{"space", "list", "work"},
			want: "list takes no arguments",
		},
		{
			name: "a missing directory",
			args: []string{"space", "list", "--dir"},
			want: "--dir needs a directory",
		},
		{
			name: "json and pane together",
			args: []string{"space", "list", "--json", "--pane"},
			want: "--json and --pane cannot be combined",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			run := newSpaceRun(t)

			code := run.run(t, tt.args...)

			if code != 2 {
				t.Fatalf("exit = %d, want 2\nstdout:\n%s\nstderr:\n%s", code, run.stdout, run.stderr)
			}
			if !strings.Contains(run.stderr.String(), tt.want) {
				t.Fatalf("stderr = %q, want it to contain %q", run.stderr.String(), tt.want)
			}
			if run.stdout.Len() != 0 {
				t.Fatalf("stdout = %q, want empty", run.stdout.String())
			}
		})
	}
}

func TestSpaceHelpListsEverySubcommand(t *testing.T) {
	run := newSpaceRun(t)

	code := run.run(t, "space", "--help")

	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", code, run.stderr)
	}
	for _, want := range []string{"list", "save <name>", "load <name>"} {
		if !strings.Contains(run.stdout.String(), want) {
			t.Fatalf("stdout = %q, want it to contain %q", run.stdout.String(), want)
		}
	}
	if !bytes.Contains(run.stdout.Bytes(), []byte("space list --help")) {
		t.Fatalf("stdout = %q, want it to point at the list help", run.stdout.String())
	}
}

func TestSubcommandsPrintTheirOwnHelp(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "space list",
			args: []string{"space", "list", "--help"},
			want: []string{"usage: hs space list", "--json", "--pane", "--dir"},
		},
		{
			name: "space save",
			args: []string{"space", "save", "--help"},
			want: []string{"usage: hs space save", "--current", "--force"},
		},
		{
			name: "session fork",
			args: []string{"session", "fork", "--help"},
			want: []string{"usage: hs session fork", "--force", "layout-name"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			run := newSpaceRun(t)

			code := run.run(t, tt.args...)

			if code != 0 {
				t.Fatalf("exit = %d, want 0\nstderr:\n%s", code, run.stderr)
			}
			for _, want := range tt.want {
				if !strings.Contains(run.stdout.String(), want) {
					t.Fatalf("stdout = %q, want it to contain %q", run.stdout.String(), want)
				}
			}
			if calls := run.client.Calls(); len(calls) != 0 {
				t.Fatalf("a help request issued calls: %v", calls)
			}
		})
	}
}
