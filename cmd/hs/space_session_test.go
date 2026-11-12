package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/WilberC/herdr-suite/internal/herdrmock"
	"github.com/WilberC/herdr-suite/internal/layout"
)

func newSessionLoadRun(t *testing.T) *spaceRun {
	t.Helper()
	run := newForkRun(t,
		herdrmock.Session{Name: "default", Running: true, Default: true},
		herdrmock.Session{Name: "work", Running: true, Socket: "/tmp/work.sock"},
	)
	if code := run.run(t, "session", "fork", "work", "backup"); code != 0 {
		t.Fatalf("fork exit = %d\nstderr:\n%s", code, run.stderr)
	}
	run.reset()
	return run
}

func TestSpaceLoadReplaysEveryWorkspaceOfASessionLayout(t *testing.T) {
	run := newSessionLoadRun(t)

	code := run.run(t, "space", "load", "backup")

	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstdout:\n%s\nstderr:\n%s", code, run.stdout, run.stderr)
	}
	want := strings.Join([]string{
		"loaded layout 'backup' into 2 workspaces · 3 tabs · 6 panes · 18 steps",
		"workspace 'project' · 2 tabs · 4 panes · 11 steps",
		"workspace 'api' · 1 tab · 2 panes · 7 steps",
		"",
	}, "\n")
	if got := run.stdout.String(); got != want {
		t.Fatalf("stdout =\n%s\nwant:\n%s", got, want)
	}
	if run.stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", run.stderr.String())
	}

	var created []string
	var focused []string
	for _, op := range run.client.Ops() {
		switch op.Method {
		case "CreateWorkspace":
			created = append(created, op.Label)
		case "FocusWorkspace":
			focused = append(focused, op.WorkspaceID)
		}
	}
	if want := []string{"project", "api"}; strings.Join(created, ",") != strings.Join(want, ",") {
		t.Fatalf("created workspaces = %v, want %v", created, want)
	}
	if want := []string{"nw-1", "nw-2"}; strings.Join(focused, ",") != strings.Join(want, ",") {
		t.Fatalf("focused workspaces = %v, want %v", focused, want)
	}
}

func TestSpaceLoadPrintsEverySessionPlanStepOnADryRun(t *testing.T) {
	run := newSessionLoadRun(t)

	code := run.run(t, "space", "load", "backup", "--dry-run")

	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", code, run.stderr)
	}
	for _, want := range []string{
		"session plan for layout 'backup' · 2 workspaces · 3 tabs · 6 panes",
		"workspace 1 'project' · 2 tabs · 4 panes · 11 steps",
		"workspace 2 'api' · 1 tab · 2 panes · 7 steps",
	} {
		if !strings.Contains(run.stdout.String(), want) {
			t.Fatalf("stdout = %q, want it to contain %q", run.stdout.String(), want)
		}
	}
	if ops := run.client.Ops(); len(ops) != 0 {
		t.Fatalf("a dry run issued %d calls, want none: %#v", len(ops), ops)
	}
}

func TestSpaceLoadRefusesWorkspaceFlagsForASessionLayout(t *testing.T) {
	for _, flag := range []string{"--replace", "--new-workspace"} {
		t.Run(flag, func(t *testing.T) {
			run := newSessionLoadRun(t)

			code := run.run(t, "space", "load", "backup", flag)

			if code != 2 {
				t.Fatalf("exit = %d, want 2\nstdout:\n%s\nstderr:\n%s", code, run.stdout, run.stderr)
			}
			for _, want := range []string{"only apply to a single-workspace layout", "2 workspaces"} {
				if !strings.Contains(run.stderr.String(), want) {
					t.Fatalf("stderr = %q, want it to contain %q", run.stderr.String(), want)
				}
			}
			if ops := run.client.Ops(); len(ops) != 0 {
				t.Fatalf("a refused load issued calls: %#v", ops)
			}
		})
	}
}

func TestSpaceLoadReportsTheWorkspaceThatFailed(t *testing.T) {
	run := newSessionLoadRun(t)
	run.client.Fail = func(op herdrmock.Op) error {
		if op.Method == "CreateWorkspace" && op.Label == "api" {
			return errors.New("pane limit reached")
		}
		return nil
	}

	code := run.run(t, "space", "load", "backup")

	if code != 1 {
		t.Fatalf("exit = %d, want 1\nstdout:\n%s\nstderr:\n%s", code, run.stdout, run.stderr)
	}
	for _, want := range []string{"workspace 'api': step 1 of 7 failed", "pane limit reached", "no transaction"} {
		if !strings.Contains(run.stderr.String(), want) {
			t.Fatalf("stderr = %q, want it to contain %q", run.stderr.String(), want)
		}
	}
}

func TestSpaceLoadSelectsALayoutInteractively(t *testing.T) {
	run := newSessionLoadRun(t)
	run.d.interactive = true
	var offered []string
	run.d.pickLayout = func(entries []layout.Entry) (string, error) {
		for _, entry := range entries {
			offered = append(offered, entry.Name+"@"+entry.Scope)
		}
		return run.layoutPath("backup"), nil
	}

	code := run.run(t, "space", "load", "--select")

	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstdout:\n%s\nstderr:\n%s", code, run.stdout, run.stderr)
	}
	if want := []string{"backup@global"}; strings.Join(offered, ",") != strings.Join(want, ",") {
		t.Fatalf("offered layouts = %v, want %v", offered, want)
	}
	if !strings.Contains(run.stdout.String(), "loaded layout 'backup' into 2 workspaces") {
		t.Fatalf("stdout = %q, want the picked layout loaded", run.stdout.String())
	}
}

func TestSpaceLoadSelectBuildsASingleWorkspaceLayoutInANewWorkspace(t *testing.T) {
	run := newLoadRun(t)
	run.saveWork(t)
	run.d.interactive = true
	run.d.pickLayout = func([]layout.Entry) (string, error) { return run.layoutPath("work"), nil }

	code := run.run(t, "space", "load", "--select")

	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstdout:\n%s\nstderr:\n%s", code, run.stdout, run.stderr)
	}
	ops := run.client.Ops()
	if ops[0].Method != "CreateWorkspace" || ops[0].Label != "project" {
		t.Fatalf("first op = %#v, want the layout built in a new workspace", ops[0])
	}
}

func TestSpaceLoadSelectNeedsATerminal(t *testing.T) {
	run := newSessionLoadRun(t)

	code := run.run(t, "space", "load", "--select")

	if code != 2 {
		t.Fatalf("exit = %d, want 2\nstdout:\n%s\nstderr:\n%s", code, run.stdout, run.stderr)
	}
	if want := "the layout picker needs a terminal"; !strings.Contains(run.stderr.String(), want) {
		t.Fatalf("stderr = %q, want it to contain %q", run.stderr.String(), want)
	}
}

func TestSpaceLoadSelectRejectsANameAndReplace(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "a name",
			args: []string{"space", "load", "backup", "--select"},
			want: "--select picks the layout",
		},
		{
			name: "replace",
			args: []string{"space", "load", "--select", "--replace"},
			want: "--select builds in a new workspace",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			run := newSessionLoadRun(t)

			code := run.run(t, tt.args...)

			if code != 2 {
				t.Fatalf("exit = %d, want 2\nstderr:\n%s", code, run.stderr)
			}
			if !strings.Contains(run.stderr.String(), tt.want) {
				t.Fatalf("stderr = %q, want it to contain %q", run.stderr.String(), tt.want)
			}
			if ops := run.client.Ops(); len(ops) != 0 {
				t.Fatalf("bad arguments issued calls: %#v", ops)
			}
		})
	}
}

func TestSpaceLoadReportsAnAmbiguousNameWithItsListing(t *testing.T) {
	run := newLoadRun(t)
	run.saveWork(t)
	declared := declareProject(t, run.configDir, projectLayoutNamed("work", "project"))
	writeProjectIndex(t, run.configDir, "[layouts]\nwork = \"other/work.json\"\n")
	if err := os.MkdirAll(filepath.Join(run.configDir, ".herdr", "other"), 0o755); err != nil {
		t.Fatalf("create the second declaration: %v", err)
	}
	if err := os.WriteFile(filepath.Join(run.configDir, ".herdr", "other", "work.json"), mustRead(t, declared), 0o644); err != nil {
		t.Fatalf("write the second declaration: %v", err)
	}
	run.reset()

	code := run.run(t, "space", "load", "work")

	if code != 2 {
		t.Fatalf("exit = %d, want 2\nstdout:\n%s\nstderr:\n%s", code, run.stdout, run.stderr)
	}
	for _, want := range []string{"is declared in 2 places", "load the file by path"} {
		if !strings.Contains(run.stderr.String(), want) {
			t.Fatalf("stderr = %q, want it to contain %q", run.stderr.String(), want)
		}
	}
	if ops := run.client.Ops(); len(ops) != 0 {
		t.Fatalf("a refused load issued calls: %#v", ops)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}

func writeProjectIndex(t *testing.T, dir, body string) {
	t.Helper()
	path := filepath.Join(dir, ".herdr", "layouts.toml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write the project index: %v", err)
	}
}

func TestSpaceSaveCurrentNamesTheLayoutAfterTheWorkspace(t *testing.T) {
	run := newSpaceRun(t)

	code := run.run(t, "space", "save", "--current")

	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstdout:\n%s\nstderr:\n%s", code, run.stdout, run.stderr)
	}
	if !strings.Contains(run.stdout.String(), "saved layout 'project' at "+run.layoutPath("project")) {
		t.Fatalf("stdout = %q, want the workspace's label as the name", run.stdout.String())
	}
	run.reset()

	code = run.run(t, "space", "save", "--current")

	if code != 2 {
		t.Fatalf("second save exit = %d, want 2\nstderr:\n%s", code, run.stderr)
	}
	if !strings.Contains(run.stderr.String(), "already exists") {
		t.Fatalf("stderr = %q, want the stored layout reported", run.stderr.String())
	}
}

func TestSpaceSaveCurrentRejectsALayoutName(t *testing.T) {
	run := newSpaceRun(t)

	code := run.run(t, "space", "save", "work", "--current")

	if code != 2 {
		t.Fatalf("exit = %d, want 2\nstderr:\n%s", code, run.stderr)
	}
	if want := "--current names the layout after the workspace"; !strings.Contains(run.stderr.String(), want) {
		t.Fatalf("stderr = %q, want it to contain %q", run.stderr.String(), want)
	}
	if calls := run.client.Calls(); len(calls) != 0 {
		t.Fatalf("bad arguments issued calls: %v", calls)
	}
}
