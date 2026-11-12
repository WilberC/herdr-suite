package main

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/WilberC/herdr-suite/internal/herdrmock"
	"github.com/WilberC/herdr-suite/internal/layout"
)

func multiSpaceFixture() herdrmock.Space {
	space := spaceFixture()
	space.Session = "work"
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

func newForkRun(t *testing.T, sessions ...herdrmock.Session) *spaceRun {
	t.Helper()
	run := newSpaceRun(t, sessions...)
	run.client.Spaces["work"] = multiSpaceFixture()
	return run
}

func TestSessionForkStoresEveryWorkspaceOfASession(t *testing.T) {
	run := newForkRun(t,
		herdrmock.Session{Name: "default", Running: true, Default: true},
		herdrmock.Session{Name: "work", Running: true, Socket: "/tmp/work.sock"},
	)

	code := run.run(t, "session", "fork", "work", "backup")

	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstdout:\n%s\nstderr:\n%s", code, run.stdout, run.stderr)
	}
	want := strings.Join([]string{
		"saved layout 'backup' at " + run.layoutPath("backup"),
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
	if got := run.stdout.String(); got != want {
		t.Fatalf("stdout =\n%s\nwant:\n%s", got, want)
	}
	if run.stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", run.stderr.String())
	}
	wantCalls := []herdrmock.Call{{Method: "ListSessions"}, {Method: "Snapshot", Target: "work"}}
	if got := run.client.Calls(); !reflect.DeepEqual(got, wantCalls) {
		t.Fatalf("client calls = %v, want %v", got, wantCalls)
	}

	data, err := os.ReadFile(run.layoutPath("backup"))
	if err != nil {
		t.Fatalf("read the stored layout: %v", err)
	}
	document, err := layout.Unmarshal(data)
	if err != nil {
		t.Fatalf("Unmarshal the stored layout: %v", err)
	}
	if !document.Multi() {
		t.Fatalf("stored document = %#v, want the session form", document)
	}
	if document.Session != "work" || len(document.Workspaces) != 2 {
		t.Fatalf("stored document = %#v, want two workspaces of session 'work'", document)
	}
	if document.Workspaces[0].Label != "project" || document.Workspaces[1].Label != "api" {
		t.Fatalf("stored workspaces = %#v, want project then api", document.Workspaces)
	}
}

func TestSessionForkDefaultsTheLayoutNameToTheSession(t *testing.T) {
	run := newForkRun(t,
		herdrmock.Session{Name: "default", Running: true, Default: true},
		herdrmock.Session{Name: "work", Running: true, Socket: "/tmp/work.sock"},
	)

	code := run.run(t, "session", "fork", "work")

	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", code, run.stderr)
	}
	if _, err := os.Stat(run.layoutPath("work")); err != nil {
		t.Fatalf("the layout was not stored under the session name: %v", err)
	}
}

func TestSessionForkOfTheCurrentSessionReadsItsNameFromTheSocket(t *testing.T) {
	run := newForkRun(t,
		herdrmock.Session{Name: "default", Running: true, Default: true},
		herdrmock.Session{Name: "work", Running: true, Socket: "/tmp/work.sock"},
	)
	run.d.getenv = func(name string) string {
		switch name {
		case "XDG_CONFIG_HOME":
			return run.configDir
		case "HERDR_SOCKET_PATH":
			return "/tmp/work.sock"
		}
		return ""
	}

	code := run.run(t, "session", "fork")

	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstdout:\n%s\nstderr:\n%s", code, run.stdout, run.stderr)
	}
	if !strings.Contains(run.stdout.String(), "saved layout 'work' at "+run.layoutPath("work")) {
		t.Fatalf("stdout = %q, want the fork stored as 'work'", run.stdout.String())
	}
	wantCalls := []herdrmock.Call{{Method: "ListSessions"}, {Method: "Snapshot", Target: "work"}}
	if got := run.client.Calls(); !reflect.DeepEqual(got, wantCalls) {
		t.Fatalf("client calls = %v, want %v", got, wantCalls)
	}
}

func TestSessionForkRefusesACurrentSessionItCannotName(t *testing.T) {
	run := newForkRun(t, herdrmock.Session{Name: "default", Running: true, Default: true, Socket: "/tmp/default.sock"})
	run.d.getenv = func(name string) string {
		if name == "XDG_CONFIG_HOME" {
			return run.configDir
		}
		return ""
	}

	code := run.run(t, "session", "fork")

	if code != 2 {
		t.Fatalf("exit = %d, want 2\nstdout:\n%s\nstderr:\n%s", code, run.stdout, run.stderr)
	}
	for _, want := range []string{"cannot name the current session", "pass the session name"} {
		if !strings.Contains(run.stderr.String(), want) {
			t.Fatalf("stderr = %q, want it to contain %q", run.stderr.String(), want)
		}
	}
	if ops := run.client.Ops(); len(ops) != 0 {
		t.Fatalf("a refused fork issued calls: %#v", ops)
	}
}

func TestSessionForkRefusesAStoppedSession(t *testing.T) {
	run := newForkRun(t,
		herdrmock.Session{Name: "default", Running: true, Default: true},
		herdrmock.Session{Name: "work"},
	)

	code := run.run(t, "session", "fork", "work", "backup")

	if code != 2 {
		t.Fatalf("exit = %d, want 2\nstderr:\n%s", code, run.stderr)
	}
	if want := "session 'work' is not running"; !strings.Contains(run.stderr.String(), want) {
		t.Fatalf("stderr = %q, want it to contain %q", run.stderr.String(), want)
	}
	if _, err := os.Stat(run.layoutPath("backup")); err == nil {
		t.Fatalf("a refused fork stored a layout")
	}
}

func TestSessionForkRefusesToReplaceAStoredLayout(t *testing.T) {
	run := newForkRun(t,
		herdrmock.Session{Name: "default", Running: true, Default: true},
		herdrmock.Session{Name: "work", Running: true, Socket: "/tmp/work.sock"},
	)
	if code := run.run(t, "session", "fork", "work", "backup"); code != 0 {
		t.Fatalf("first fork exit = %d\nstderr:\n%s", code, run.stderr)
	}
	run.reset()
	if err := os.WriteFile(run.layoutPath("backup"), []byte("tamper"), 0o644); err != nil {
		t.Fatalf("tamper the stored layout: %v", err)
	}

	code := run.run(t, "session", "fork", "work", "backup")

	if code != 2 {
		t.Fatalf("exit = %d, want 2\nstderr:\n%s", code, run.stderr)
	}
	if !strings.Contains(run.stderr.String(), "already exists") {
		t.Fatalf("stderr = %q, want the stored layout reported", run.stderr.String())
	}
	data, err := os.ReadFile(run.layoutPath("backup"))
	if err != nil {
		t.Fatalf("read the stored layout: %v", err)
	}
	if string(data) != "tamper" {
		t.Fatalf("a refused fork rewrote the stored layout")
	}
}

func TestSessionForkPrintsTheDocumentAsJSON(t *testing.T) {
	run := newForkRun(t,
		herdrmock.Session{Name: "default", Running: true, Default: true},
		herdrmock.Session{Name: "work", Running: true, Socket: "/tmp/work.sock"},
	)

	code := run.run(t, "session", "fork", "work", "backup", "--json")

	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", code, run.stderr)
	}
	var document layout.Layout
	if err := json.Unmarshal(run.stdout.Bytes(), &document); err != nil {
		t.Fatalf("stdout is not the layout document: %v\n%s", err, run.stdout.String())
	}
	if !document.Multi() || len(document.Workspaces) != 2 {
		t.Fatalf("document = %#v, want both workspaces", document)
	}
}

func TestSessionForkRejectsBadArguments(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "too many names",
			args: []string{"session", "fork", "work", "backup", "extra"},
			want: "expected at most a session and a layout name",
		},
		{
			name: "an unknown flag",
			args: []string{"session", "fork", "work", "--bogus"},
			want: `unknown flag "--bogus"`,
		},
		{
			name: "an invalid layout name",
			args: []string{"session", "fork", "work", "backup!"},
			want: "invalid layout name 'backup!'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			run := newForkRun(t)

			code := run.run(t, tt.args...)

			if code != 2 {
				t.Fatalf("exit = %d, want 2\nstdout:\n%s\nstderr:\n%s", code, run.stdout, run.stderr)
			}
			if !strings.Contains(run.stderr.String(), tt.want) {
				t.Fatalf("stderr = %q, want it to contain %q", run.stderr.String(), tt.want)
			}
			if ops := run.client.Ops(); len(ops) != 0 {
				t.Fatalf("bad arguments issued calls: %#v", ops)
			}
			entries, err := os.ReadDir(run.configDir)
			if err != nil {
				t.Fatalf("read the config directory: %v", err)
			}
			if len(entries) != 0 {
				t.Fatalf("bad arguments wrote %d entries, want none", len(entries))
			}
		})
	}
}
