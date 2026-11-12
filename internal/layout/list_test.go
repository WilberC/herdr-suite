package layout

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func projectTree(t *testing.T) (root, cwd, configHome string) {
	t.Helper()
	root = t.TempDir()
	cwd = filepath.Join(root, "src", "deep")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatalf("create the project tree: %v", err)
	}
	return root, cwd, t.TempDir()
}

func mustDiscover(t *testing.T, configHome, cwd string) Sources {
	t.Helper()
	sources, err := Discover(configEnv(configHome), cwd)
	if err != nil {
		t.Fatalf("Discover error = %v, want nil", err)
	}
	return sources
}

func stamp(t *testing.T, path string, at time.Time) {
	t.Helper()
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatalf("stamp %s: %v", path, err)
	}
}

func TestListReportsEveryVisibleLayoutWithItsScope(t *testing.T) {
	root, cwd, configHome := projectTree(t)
	projectPath := filepath.Join(root, ".herdr", "layouts", "api.json")
	project := wantLayout()
	project.Name = "api"
	project.Workspace.Label = "api"
	writeLayoutFile(t, projectPath, project)
	stamp(t, projectPath, time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))

	sources := mustDiscover(t, configHome, cwd)
	globalPath := filepath.Join(sources.Global, "work.json")
	work := wantLayout()
	work.Name = "work"
	writeLayoutFile(t, globalPath, work)
	stamp(t, globalPath, time.Date(2025, 12, 31, 23, 59, 0, 0, time.UTC))

	entries, err := List(sources)
	if err != nil {
		t.Fatalf("List error = %v, want nil", err)
	}

	if len(entries) != 2 {
		t.Fatalf("List returned %d entries, want 2: %#v", len(entries), entries)
	}
	first, second := entries[0], entries[1]
	if first.Name != "api" || first.Scope != ScopeProject || first.Path != projectPath {
		t.Fatalf("entry 1 = %#v, want the project layout", first)
	}
	if first.Workspace != "api" || first.Workspaces != 1 || first.Tabs != 2 || first.Panes != 4 {
		t.Fatalf("entry 1 = %#v, want the project layout's workspace and counts", first)
	}
	if !first.Modified.Equal(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)) {
		t.Fatalf("entry 1 modified = %v, want the stored file's mtime", first.Modified)
	}
	if second.Name != "work" || second.Scope != ScopeGlobal || second.Path != globalPath {
		t.Fatalf("entry 2 = %#v, want the global layout", second)
	}
}

func TestListCountsASessionLayoutAcrossItsWorkspaces(t *testing.T) {
	root, cwd, configHome := projectTree(t)
	document, err := CaptureSession("session", sessionSpace())
	if err != nil {
		t.Fatalf("CaptureSession error = %v", err)
	}
	writeLayoutFile(t, filepath.Join(root, ".herdr", "layouts", "session.json"), document)

	sources := mustDiscover(t, configHome, cwd)
	entries, err := List(sources)
	if err != nil {
		t.Fatalf("List error = %v, want nil", err)
	}

	if len(entries) != 1 {
		t.Fatalf("List returned %d entries, want 1: %#v", len(entries), entries)
	}
	entry := entries[0]
	if entry.Workspace != "project" || entry.Workspaces != 2 {
		t.Fatalf("entry = %#v, want the first workspace plus its count", entry)
	}
	if entry.Tabs != 3 || entry.Panes != 6 {
		t.Fatalf("entry = %#v, want the totals of every workspace", entry)
	}
	if cell := entry.workspaceCell(); cell != "project +1" {
		t.Fatalf("workspace cell = %q, want %q", cell, "project +1")
	}
}

func TestListReportsAnIndexEntryThatNamesNoFile(t *testing.T) {
	root, cwd, configHome := projectTree(t)
	writeIndex(t, filepath.Join(root, ".herdr"), "[layouts]\napi = \"missing.json\"\n")
	sources := mustDiscover(t, configHome, cwd)

	_, err := List(sources)

	if err == nil {
		t.Fatalf("List error = nil, want the missing declaration reported")
	}
	for _, want := range []string{"'api'", filepath.Join(root, ".herdr", "missing.json")} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("List error = %q, want it to contain %q", err.Error(), want)
		}
	}
}

func TestWriteListPrintsTheStableTable(t *testing.T) {
	entries := []Entry{
		{
			Name:       "api",
			Scope:      ScopeProject,
			Path:       "/proj/.herdr/layouts/api.json",
			Workspace:  "api",
			Workspaces: 1,
			Tabs:       1,
			Panes:      1,
			Modified:   time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		},
		{
			Name:       "work",
			Scope:      ScopeGlobal,
			Path:       "/layouts/work.json",
			Workspace:  "project",
			Workspaces: 1,
			Tabs:       2,
			Panes:      4,
			Modified:   time.Date(2025, 12, 31, 23, 59, 0, 0, time.UTC),
		},
	}

	var out bytes.Buffer
	if err := WriteList(&out, entries); err != nil {
		t.Fatalf("WriteList error = %v, want nil", err)
	}

	want := strings.Join([]string{
		"NAME  SCOPE    WORKSPACE  TABS  PANES  MODIFIED",
		"api   project  api        1     1      2026-01-02 03:04",
		"work  global   project    2     4      2025-12-31 23:59",
		"",
	}, "\n")
	if got := out.String(); got != want {
		t.Fatalf("WriteList =\n%s\nwant:\n%s", got, want)
	}
}

func TestWriteListJSONPrintsTheStableDocument(t *testing.T) {
	entries := []Entry{{
		Name:       "work",
		Scope:      ScopeGlobal,
		Path:       "/layouts/work.json",
		Workspace:  "project",
		Workspaces: 2,
		Tabs:       3,
		Panes:      6,
		Modified:   time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
	}}

	var out bytes.Buffer
	if err := WriteListJSON(&out, entries); err != nil {
		t.Fatalf("WriteListJSON error = %v, want nil", err)
	}

	var document struct {
		Layouts []map[string]any `json:"layouts"`
	}
	if err := json.Unmarshal(out.Bytes(), &document); err != nil {
		t.Fatalf("stdout is not the layout list: %v\n%s", err, out.String())
	}
	if len(document.Layouts) != 1 {
		t.Fatalf("document holds %d layouts, want 1:\n%s", len(document.Layouts), out.String())
	}
	want := map[string]any{
		"name":       "work",
		"scope":      "global",
		"path":       "/layouts/work.json",
		"workspace":  "project",
		"workspaces": float64(2),
		"tabs":       float64(3),
		"panes":      float64(6),
		"modified":   "2026-01-02T03:04:05Z",
	}
	for key, value := range want {
		if got := document.Layouts[0][key]; got != value {
			t.Fatalf("field %q = %#v, want %#v", key, got, value)
		}
	}
}

func TestResolvePrefersTheProjectLayoutOverTheGlobalStore(t *testing.T) {
	root, cwd, configHome := projectTree(t)
	project := wantLayout()
	project.Name = "api"
	project.Workspace.Label = "project-copy"
	writeLayoutFile(t, filepath.Join(root, ".herdr", "layouts", "api.json"), project)

	sources := mustDiscover(t, configHome, cwd)
	global := wantLayout()
	global.Name = "api"
	global.Workspace.Label = "global-copy"
	writeLayoutFile(t, filepath.Join(sources.Global, "api.json"), global)

	document, path, err := Resolve(sources, "api")
	if err != nil {
		t.Fatalf("Resolve error = %v, want nil", err)
	}

	if document.Workspace.Label != "project-copy" {
		t.Fatalf("resolved workspace = %q, want the project copy", document.Workspace.Label)
	}
	if want := filepath.Join(root, ".herdr", "layouts", "api.json"); path != want {
		t.Fatalf("resolved path = %q, want %q", path, want)
	}
}

func TestResolveFallsThroughToTheGlobalStore(t *testing.T) {
	root, cwd, configHome := projectTree(t)
	writeLayoutFile(t, filepath.Join(root, ".herdr", "layouts", "api.json"), wantLayout())
	sources := mustDiscover(t, configHome, cwd)
	global := wantLayout()
	global.Name = "work"
	path := filepath.Join(sources.Global, "work.json")
	writeLayoutFile(t, path, global)

	document, gotPath, err := Resolve(sources, "work")
	if err != nil {
		t.Fatalf("Resolve error = %v, want nil", err)
	}

	if document.Name != "work" || gotPath != path {
		t.Fatalf("resolved = %q at %q, want the global copy at %q", document.Name, gotPath, path)
	}
}

func TestResolveReadsTheProjectIndex(t *testing.T) {
	root, cwd, configHome := projectTree(t)
	declared := wantLayout()
	declared.Name = "api"
	declared.Workspace.Label = "from-the-index"
	writeLayoutFile(t, filepath.Join(root, ".herdr", "committed", "api.json"), declared)
	writeIndex(t, filepath.Join(root, ".herdr"), "[layouts]\napi = \"committed/api.json\"\n")

	document, path, err := Resolve(mustDiscover(t, configHome, cwd), "api")
	if err != nil {
		t.Fatalf("Resolve error = %v, want nil", err)
	}

	if document.Workspace.Label != "from-the-index" {
		t.Fatalf("resolved workspace = %q, want the declared layout", document.Workspace.Label)
	}
	if want := filepath.Join(root, ".herdr", "committed", "api.json"); path != want {
		t.Fatalf("resolved path = %q, want %q", path, want)
	}
}

func TestResolveRefusesAnAmbiguousNameWithAListing(t *testing.T) {
	root, cwd, configHome := projectTree(t)
	first := filepath.Join(root, ".herdr", "layouts", "api.json")
	second := filepath.Join(root, ".herdr", "other", "api.json")
	writeLayoutFile(t, first, wantLayout())
	writeLayoutFile(t, second, wantLayout())
	writeIndex(t, filepath.Join(root, ".herdr"), "[layouts]\napi = \"other/api.json\"\n")

	_, _, err := Resolve(mustDiscover(t, configHome, cwd), "api")

	var ambiguous AmbiguousError
	if !errors.As(err, &ambiguous) {
		t.Fatalf("Resolve error = %v, want an AmbiguousError", err)
	}
	if len(ambiguous.Candidates) != 2 {
		t.Fatalf("candidates = %#v, want two", ambiguous.Candidates)
	}
	for _, fragment := range []string{
		"layout 'api' is declared in 2 places",
		"project " + first,
		"project " + second,
		"load the file by path",
	} {
		if !strings.Contains(err.Error(), fragment) {
			t.Fatalf("error = %q, want it to contain %q", err.Error(), fragment)
		}
	}
}

func TestResolveLoadsAnExplicitPathFirst(t *testing.T) {
	root, cwd, configHome := projectTree(t)
	explicit := wantLayout()
	explicit.Name = "somewhere-else"
	explicit.Workspace.Label = "explicit"
	path := filepath.Join(root, "hand", "made.json")
	writeLayoutFile(t, path, explicit)

	sources := mustDiscover(t, configHome, cwd)
	global := wantLayout()
	global.Name = "somewhere-else"
	global.Workspace.Label = "from-the-store"
	writeLayoutFile(t, filepath.Join(sources.Global, "somewhere-else.json"), global)

	document, gotPath, err := Resolve(sources, path)
	if err != nil {
		t.Fatalf("Resolve error = %v, want nil", err)
	}

	if document.Workspace.Label != "explicit" || gotPath != path {
		t.Fatalf("resolved = %q at %q, want the file the path names", document.Workspace.Label, gotPath)
	}
}

func TestResolveReportsAMissingLayoutWithThePathItLookedFor(t *testing.T) {
	_, cwd, configHome := projectTree(t)
	sources := mustDiscover(t, configHome, cwd)

	_, _, err := Resolve(sources, "ghost")

	var missing NotFoundError
	if !errors.As(err, &missing) {
		t.Fatalf("Resolve error = %v, want a NotFoundError", err)
	}
	if want := filepath.Join(sources.Global, "ghost.json"); missing.Path != want {
		t.Fatalf("NotFoundError path = %q, want %q", missing.Path, want)
	}
}

func TestResolveRefusesANameThatCouldEscapeTheStore(t *testing.T) {
	_, cwd, configHome := projectTree(t)
	sources := mustDiscover(t, configHome, cwd)

	for _, name := range []string{"with space", "..", ""} {
		if _, _, err := Resolve(sources, name); err == nil {
			t.Fatalf("Resolve(%q) error = nil, want a refusal", name)
		}
	}
}
