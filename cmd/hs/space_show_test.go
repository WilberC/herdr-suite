package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/WilberC/herdr-suite/internal/layout"
)

func TestSpaceShowPrintsTheStoredLayoutWithoutTouchingTheSession(t *testing.T) {
	run := newLoadRun(t)
	run.saveWork(t)
	written := len(run.client.Calls())

	code := run.run(t, "space", "show", "work")

	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstdout:\n%s\nstderr:\n%s", code, run.stdout, run.stderr)
	}
	want := strings.Join([]string{
		"layout 'work' · global · " + run.layoutPath("work"),
		"schema_version 1",
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
	if calls := run.client.Calls(); len(calls) != written {
		t.Fatalf("show read the session: %v", calls[written:])
	}
}

func TestSpaceShowRendersTheSameTreeAsTheSnapshotReport(t *testing.T) {
	run := newLoadRun(t)
	run.saveWork(t)
	document := readLayout(t, run.layoutPath("work"))

	code := run.run(t, "space", "show", "work")

	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", code, run.stderr)
	}
	summary := layout.Summary(document, run.layoutPath("work"))
	show := run.stdout.String()
	index := strings.Index(show, "workspace ")
	if index < 0 {
		t.Fatalf("show printed no workspace:\n%s", show)
	}
	summaryIndex := strings.Index(summary, "workspace ")
	if summaryIndex < 0 {
		t.Fatalf("the snapshot report printed no workspace:\n%s", summary)
	}
	if got, want := show[index:], summary[summaryIndex:]; got != want {
		t.Fatalf("show tree =\n%s\nwant the snapshot renderer's:\n%s", got, want)
	}
}

func TestSpaceShowPrintsTheStoredDocumentAsJSON(t *testing.T) {
	run := newLoadRun(t)
	run.saveWork(t)
	stored, err := os.ReadFile(run.layoutPath("work"))
	if err != nil {
		t.Fatalf("read the stored layout: %v", err)
	}
	var want map[string]any
	if err := json.Unmarshal(stored, &want); err != nil {
		t.Fatalf("the stored layout is not JSON: %v", err)
	}

	code := run.run(t, "space", "show", "work", "--json")

	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", code, run.stderr)
	}
	var got map[string]any
	if err := json.Unmarshal(run.stdout.Bytes(), &got); err != nil {
		t.Fatalf("stdout is not a layout document: %v\n%s", err, run.stdout)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("show --json =\n%s\nwant the stored document:\n%s", run.stdout, stored)
	}
}

func TestSpaceShowResolvesTheProjectLayoutAndNamesItsScope(t *testing.T) {
	run := newSpaceRun(t)
	path := declareProject(t, run.configDir, projectLayoutNamed("api", "api"))

	code := run.run(t, "space", "show", "api")

	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", code, run.stderr)
	}
	header := "layout 'api' · project · " + path + "\n"
	if !strings.HasPrefix(run.stdout.String(), header) {
		t.Fatalf("stdout = %q, want it to start with %q", run.stdout.String(), header)
	}
}

func TestSpaceShowResolvesALayoutFileByPath(t *testing.T) {
	run := newSpaceRun(t)
	body, err := layout.Marshal(projectLayoutNamed("api", "api"))
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	path := filepath.Join(t.TempDir(), "hand", "made.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}

	code := run.run(t, "space", "show", path)

	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", code, run.stderr)
	}
	header := "layout 'api' · path · " + path + "\n"
	if !strings.HasPrefix(run.stdout.String(), header) {
		t.Fatalf("stdout = %q, want it to start with %q", run.stdout.String(), header)
	}
}

func TestSpaceShowDiscoversFromTheDirectoryItIsGiven(t *testing.T) {
	run := newSpaceRun(t)
	declareProject(t, run.configDir, projectLayoutNamed("api", "api"))

	code := run.run(t, "space", "show", "api", "--dir", run.configDir)

	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", code, run.stderr)
	}
	if want := "layout 'api' · project · "; !strings.HasPrefix(run.stdout.String(), want) {
		t.Fatalf("stdout = %q, want it to start with %q", run.stdout.String(), want)
	}
}

func TestSpaceShowPrintsTheCapturedTimestampWhenTheDocumentHoldsOne(t *testing.T) {
	run := newSpaceRun(t)
	body := `{
  "schema_version": 1,
  "name": "old",
  "captured_at": "2026-01-02T03:04:05Z",
  "workspace": {"label": "project", "tabs": [{"label": "main", "focused": true, "focused_pane_id": "w1:p1", "root": {"type": "pane", "pane_id": "w1:p1", "cwd": "/srv"}}]}
}
`
	if err := os.MkdirAll(filepath.Dir(run.layoutPath("old")), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(run.layoutPath("old"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	code := run.run(t, "space", "show", "old")

	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", code, run.stderr)
	}
	if want := "captured 2026-01-02T03:04:05Z\n"; !strings.Contains(run.stdout.String(), want) {
		t.Fatalf("stdout = %q, want it to contain %q", run.stdout.String(), want)
	}
}

func TestSpaceShowOmitsTheTimestampTheDocumentDoesNotHold(t *testing.T) {
	run := newLoadRun(t)
	run.saveWork(t)

	code := run.run(t, "space", "show", "work")

	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", code, run.stderr)
	}
	if strings.Contains(run.stdout.String(), "captured ") {
		t.Fatalf("stdout = %q, want no captured line", run.stdout.String())
	}
}

func TestSpaceShowReadsALegacyDocumentAtTheCurrentSchema(t *testing.T) {
	run := newSpaceRun(t)
	body := `{
  "name": "legacy",
  "workspace": {"label": "project", "tabs": [{"label": "main", "focused": true, "root": {"type": "pane", "pane_id": "w1:p1", "cwd": "/srv"}}]}
}
`
	if err := os.MkdirAll(filepath.Dir(run.layoutPath("legacy")), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(run.layoutPath("legacy"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	code := run.run(t, "space", "show", "legacy")

	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", code, run.stderr)
	}
	if want := "schema_version 1\n"; !strings.Contains(run.stdout.String(), want) {
		t.Fatalf("stdout = %q, want it to contain %q", run.stdout.String(), want)
	}
}

func TestSpaceShowRefusesWhatLoadRefuses(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, run *spaceRun)
		args  []string
		want  []string
	}{
		{
			name: "a missing layout",
			args: []string{"space", "show", "ghost"},
			want: []string{"unknown layout 'ghost'", "hs space save ghost"},
		},
		{
			name: "a name two project declarations answer to",
			setup: func(t *testing.T, run *spaceRun) {
				declareProject(t, run.configDir, projectLayoutNamed("api", "api"))
				index := filepath.Join(run.configDir, ".herdr", "layouts.toml")
				other := filepath.Join(run.configDir, ".herdr", "other", "api.json")
				if err := os.MkdirAll(filepath.Dir(other), 0o755); err != nil {
					t.Fatal(err)
				}
				body, err := layout.Marshal(projectLayoutNamed("api", "api"))
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(other, body, 0o644); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(index, []byte("[layouts]\napi = \"other/api.json\"\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			},
			args: []string{"space", "show", "api"},
			want: []string{"layout 'api' is declared in 2 places", "remove one or load the file by path"},
		},
		{
			name: "a layout a newer build wrote",
			setup: func(t *testing.T, run *spaceRun) {
				path := run.layoutPath("future")
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				body := "{\n  \"schema_version\": 2,\n  \"name\": \"future\",\n  \"workspace\": {\"label\": \"project\", \"tabs\": []}\n}\n"
				if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
					t.Fatal(err)
				}
			},
			args: []string{"space", "show", "future"},
			want: []string{"newer herdr-suite", "update hs"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			run := newSpaceRun(t)
			if tt.setup != nil {
				tt.setup(t, run)
			}

			code := run.run(t, tt.args...)

			if code != 2 {
				t.Fatalf("exit = %d, want 2\nstdout:\n%s\nstderr:\n%s", code, run.stdout, run.stderr)
			}
			for _, want := range tt.want {
				if !strings.Contains(run.stderr.String(), want) {
					t.Fatalf("stderr = %q, want it to contain %q", run.stderr.String(), want)
				}
			}
			if run.stdout.Len() != 0 {
				t.Fatalf("a refused show printed %q", run.stdout.String())
			}
			if calls := run.client.Calls(); len(calls) != 0 {
				t.Fatalf("a refused show issued calls: %v", calls)
			}
		})
	}
}

func TestSpaceShowRejectsBadArguments(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "no layout name",
			args: []string{"space", "show"},
			want: "expected one layout name",
		},
		{
			name: "two layout names",
			args: []string{"space", "show", "api", "web"},
			want: "expected one layout name",
		},
		{
			name: "an unknown flag",
			args: []string{"space", "show", "api", "--bogus"},
			want: `unknown flag "--bogus"`,
		},
		{
			name: "a missing directory",
			args: []string{"space", "show", "api", "--dir"},
			want: "--dir needs a directory",
		},
		{
			name: "an invalid name",
			args: []string{"space", "show", "work!"},
			want: "invalid layout name 'work!'",
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
			if !strings.Contains(run.stderr.String(), "usage: hs space show") {
				t.Fatalf("stderr = %q, want the show usage", run.stderr.String())
			}
			if run.stdout.Len() != 0 {
				t.Fatalf("stdout = %q, want empty", run.stdout.String())
			}
		})
	}
}
