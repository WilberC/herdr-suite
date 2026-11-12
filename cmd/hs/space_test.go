package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/WilberC/herdr-suite/internal/herdrmock"
	"github.com/WilberC/herdr-suite/internal/layout"
)

func spaceFixture() herdrmock.Space {
	return herdrmock.Space{
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

type spaceRun struct {
	d              deps
	client         *herdrmock.Fake
	configDir      string
	stdout, stderr *bytes.Buffer
}

func newSpaceRun(t *testing.T, sessions ...herdrmock.Session) *spaceRun {
	t.Helper()
	configDir := t.TempDir()
	client := &herdrmock.Fake{
		Sessions: sessions,
		Spaces:   map[string]herdrmock.Space{"": spaceFixture()},
	}
	run := &spaceRun{
		client:    client,
		configDir: configDir,
		stdout:    &bytes.Buffer{},
		stderr:    &bytes.Buffer{},
	}
	run.d = deps{
		client: client,
		spaces: client,
		writer: client,
		exec:   &fakeExecer{},
		getenv: func(name string) string {
			if name == "XDG_CONFIG_HOME" {
				return configDir
			}
			return ""
		},
		out: run.stdout,
		err: run.stderr,
	}
	return run
}

func (r *spaceRun) layoutPath(name string) string {
	return filepath.Join(r.configDir, "herdr", "herdr-suite", "layouts", name+".json")
}

func (r *spaceRun) run(t *testing.T, args ...string) int {
	t.Helper()
	return runWith(r.d, args, r.stdout, r.stderr)
}

func TestSpaceSaveWritesTheActiveWorkspaceAndReportsIt(t *testing.T) {
	run := newSpaceRun(t)

	code := run.run(t, "space", "save", "my space")

	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstdout:\n%s\nstderr:\n%s", code, run.stdout, run.stderr)
	}
	want := strings.Join([]string{
		"saved layout 'my-space' at " + run.layoutPath("my-space"),
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
	if got := run.stdout.String(); got != want {
		t.Fatalf("stdout =\n%s\nwant:\n%s", got, want)
	}
	if run.stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", run.stderr.String())
	}
	if got, want := run.client.Calls(), []herdrmock.Call{{Method: "Snapshot"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("client calls = %v, want %v", got, want)
	}

	data, err := os.ReadFile(run.layoutPath("my-space"))
	if err != nil {
		t.Fatalf("read the stored layout: %v", err)
	}
	stored, err := layout.Unmarshal(data)
	if err != nil {
		t.Fatalf("Unmarshal the stored layout: %v", err)
	}
	if stored.Name != "my-space" {
		t.Fatalf("stored name = %q, want %q", stored.Name, "my-space")
	}
	if stored.Session != "" {
		t.Fatalf("stored session = %q, want it omitted for the ambient session", stored.Session)
	}
	if stored.Workspace.Label != "project" || len(stored.Workspace.Tabs) != 2 {
		t.Fatalf("stored workspace = %#v, want the project topology", stored.Workspace)
	}
}

func TestSpaceSaveTargetsAnExplicitSession(t *testing.T) {
	work := spaceFixture()
	work.Session = "work"
	run := newSpaceRun(t,
		herdrmock.Session{Name: "default", Running: true, Default: true},
		herdrmock.Session{Name: "work", Running: true, Socket: "/tmp/work.sock"},
	)
	run.client.Spaces["work"] = work

	code := run.run(t, "space", "save", "snap", "--session", "work")

	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", code, run.stderr)
	}
	want := []herdrmock.Call{{Method: "ListSessions"}, {Method: "Snapshot", Target: "work"}}
	if got := run.client.Calls(); !reflect.DeepEqual(got, want) {
		t.Fatalf("client calls = %v, want %v", got, want)
	}
	data, err := os.ReadFile(run.layoutPath("snap"))
	if err != nil {
		t.Fatalf("read the stored layout: %v", err)
	}
	stored, err := layout.Unmarshal(data)
	if err != nil {
		t.Fatalf("Unmarshal the stored layout: %v", err)
	}
	if stored.Session != "work" {
		t.Fatalf("stored session = %q, want %q", stored.Session, "work")
	}
}

func TestSpaceSaveRefusesAStoppedSession(t *testing.T) {
	run := newSpaceRun(t,
		herdrmock.Session{Name: "default", Running: true, Default: true},
		herdrmock.Session{Name: "scratch"},
	)

	code := run.run(t, "space", "save", "snap", "--session", "scratch")

	if code != 2 {
		t.Fatalf("exit = %d, want 2\nstdout:\n%s\nstderr:\n%s", code, run.stdout, run.stderr)
	}
	if want := "hs: session 'scratch' is not running; start it with hs attach scratch"; !strings.Contains(run.stderr.String(), want) {
		t.Fatalf("stderr = %q, want it to contain %q", run.stderr.String(), want)
	}
	if got, want := run.client.Calls(), []herdrmock.Call{{Method: "ListSessions"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("client calls = %v, want %v", got, want)
	}
}

func TestSpaceSaveRefusesAnUnknownSession(t *testing.T) {
	run := newSpaceRun(t, herdrmock.Session{Name: "default", Running: true, Default: true})

	code := run.run(t, "space", "save", "snap", "--session", "ghost")

	if code != 2 {
		t.Fatalf("exit = %d, want 2\nstderr:\n%s", code, run.stderr)
	}
	if want := "hs: unknown session 'ghost'"; !strings.Contains(run.stderr.String(), want) {
		t.Fatalf("stderr = %q, want it to contain %q", run.stderr.String(), want)
	}
}

func TestSpaceSaveRefusesToOverwriteAStoredLayout(t *testing.T) {
	run := newSpaceRun(t)
	if code := run.run(t, "space", "save", "snap"); code != 0 {
		t.Fatalf("first save exit = %d, want 0\nstderr:\n%s", code, run.stderr)
	}
	path := run.layoutPath("snap")
	if err := os.WriteFile(path, []byte("tamper"), 0o644); err != nil {
		t.Fatalf("tamper the stored layout: %v", err)
	}
	run.stdout.Reset()
	run.stderr.Reset()

	code := run.run(t, "space", "save", "snap")

	if code != 2 {
		t.Fatalf("exit = %d, want 2\nstdout:\n%s\nstderr:\n%s", code, run.stdout, run.stderr)
	}
	for _, want := range []string{"already exists", "pass --force to overwrite"} {
		if !strings.Contains(run.stderr.String(), want) {
			t.Fatalf("stderr = %q, want it to contain %q", run.stderr.String(), want)
		}
	}
	if run.stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", run.stdout.String())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the stored layout: %v", err)
	}
	if got, want := string(data), "tamper"; got != want {
		t.Fatalf("stored layout = %q, want the refused save to leave %q", got, want)
	}
}

func TestSpaceSaveReplacesAStoredLayoutWithForce(t *testing.T) {
	run := newSpaceRun(t)
	if code := run.run(t, "space", "save", "snap"); code != 0 {
		t.Fatalf("first save exit = %d, want 0\nstderr:\n%s", code, run.stderr)
	}
	path := run.layoutPath("snap")
	if err := os.WriteFile(path, []byte("tamper"), 0o644); err != nil {
		t.Fatalf("tamper the stored layout: %v", err)
	}
	run.stdout.Reset()

	code := run.run(t, "space", "save", "snap", "--force")

	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", code, run.stderr)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the stored layout: %v", err)
	}
	if _, err := layout.Unmarshal(data); err != nil {
		t.Fatalf("forwarded layout is not valid: %v\n%s", err, data)
	}
}

func TestSpaceSavePrintsTheSnapshotDocumentAsJSON(t *testing.T) {
	run := newSpaceRun(t)

	code := run.run(t, "space", "save", "snap", "--json")

	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", code, run.stderr)
	}
	var document layout.Layout
	if err := json.Unmarshal(run.stdout.Bytes(), &document); err != nil {
		t.Fatalf("stdout is not the layout document: %v\n%s", err, run.stdout.String())
	}
	if document.Name != "snap" || document.Workspace.Label != "project" {
		t.Fatalf("document = %#v, want the saved snapshot", document)
	}
	if strings.Contains(run.stdout.String(), "saved layout") {
		t.Fatalf("stdout mixed the summary into the JSON document:\n%s", run.stdout.String())
	}
	if _, err := os.Stat(run.layoutPath("snap")); err != nil {
		t.Fatalf("the layout was not stored: %v", err)
	}
}

func TestSpaceSaveRejectsAnInvalidLayoutName(t *testing.T) {
	run := newSpaceRun(t)

	code := run.run(t, "space", "save", "work!")

	if code != 2 {
		t.Fatalf("exit = %d, want 2\nstdout:\n%s\nstderr:\n%s", code, run.stdout, run.stderr)
	}
	for _, want := range []string{"hs: invalid layout name 'work!'", "usage: hs space save"} {
		if !strings.Contains(run.stderr.String(), want) {
			t.Fatalf("stderr = %q, want it to contain %q", run.stderr.String(), want)
		}
	}
	if len(run.client.Calls()) != 0 {
		t.Fatalf("client calls = %v, want none", run.client.Calls())
	}
}

func TestSpaceSaveRequiresALayoutName(t *testing.T) {
	run := newSpaceRun(t)

	code := run.run(t, "space", "save")

	if code != 2 {
		t.Fatalf("exit = %d, want 2\nstderr:\n%s", code, run.stderr)
	}
	for _, want := range []string{"expected one layout name", "usage: hs space save"} {
		if !strings.Contains(run.stderr.String(), want) {
			t.Fatalf("stderr = %q, want it to contain %q", run.stderr.String(), want)
		}
	}
}

func TestSpaceSaveRejectsAnUnknownFlag(t *testing.T) {
	run := newSpaceRun(t)

	code := run.run(t, "space", "save", "snap", "--bogus")

	if code != 2 {
		t.Fatalf("exit = %d, want 2\nstderr:\n%s", code, run.stderr)
	}
	if want := `unknown flag "--bogus"`; !strings.Contains(run.stderr.String(), want) {
		t.Fatalf("stderr = %q, want it to contain %q", run.stderr.String(), want)
	}
}

func TestSpaceSaveSurfacesASnapshotFailure(t *testing.T) {
	run := newSpaceRun(t)
	run.client.SnapshotErr = errors.New("socket unavailable")

	code := run.run(t, "space", "save", "snap")

	if code != 1 {
		t.Fatalf("exit = %d, want 1\nstdout:\n%s\nstderr:\n%s", code, run.stdout, run.stderr)
	}
	if want := "hs: snapshot the current session: socket unavailable"; !strings.Contains(run.stderr.String(), want) {
		t.Fatalf("stderr = %q, want it to contain %q", run.stderr.String(), want)
	}
	if _, err := os.Stat(run.layoutPath("snap")); err == nil {
		t.Fatalf("a failed snapshot stored a layout")
	}
}

func TestSpaceSaveReportsAMissingFocusedWorkspace(t *testing.T) {
	run := newSpaceRun(t)
	space := spaceFixture()
	space.FocusedWorkspaceID = "w-missing"
	run.client.Spaces[""] = space

	code := run.run(t, "space", "save", "snap")

	if code != 1 {
		t.Fatalf("exit = %d, want 1\nstdout:\n%s\nstderr:\n%s", code, run.stdout, run.stderr)
	}
	if want := "focused workspace"; !strings.Contains(run.stderr.String(), want) {
		t.Fatalf("stderr = %q, want it to contain %q", run.stderr.String(), want)
	}
}

func TestSpaceHelpPrintsItsUsage(t *testing.T) {
	run := newSpaceRun(t)

	code := run.run(t, "space", "--help")

	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", code, run.stderr)
	}
	for _, want := range []string{"usage: hs space", "save <name>"} {
		if !strings.Contains(run.stdout.String(), want) {
			t.Fatalf("stdout = %q, want it to contain %q", run.stdout.String(), want)
		}
	}
}

func TestSpaceRejectsAnUnknownSubcommand(t *testing.T) {
	run := newSpaceRun(t)

	code := run.run(t, "space", "bogus")

	if code != 2 {
		t.Fatalf("exit = %d, want 2\nstdout:\n%s\nstderr:\n%s", code, run.stdout, run.stderr)
	}
	if want := `unknown space command "bogus"`; !strings.Contains(run.stderr.String(), want) {
		t.Fatalf("stderr = %q, want it to contain %q", run.stderr.String(), want)
	}
}
