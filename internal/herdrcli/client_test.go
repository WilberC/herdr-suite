package herdrcli

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/WilberC/herdr-suite/internal/herdrmock"
)

func writeStub(t *testing.T, script string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "herdr")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	return path
}

const listStub = `#!/bin/sh
if [ "$1 $2" != "session list" ] || [ "$3" != "--json" ]; then
  echo "unexpected call: $*" >&2
  exit 9
fi
printf '%s' '{"sessions":[{"default":true,"name":"default","running":true,"socket_path":"/tmp/herdr.sock"},{"name":"work","running":false}]}'
`

func TestListSessionsRunsTheHerdrBinaryAndParsesIt(t *testing.T) {
	t.Setenv(EnvBinary, writeStub(t, listStub))

	sessions, err := New().ListSessions(context.Background())
	if err != nil {
		t.Fatalf("ListSessions error = %v, want nil", err)
	}

	want := []herdrmock.Session{
		{Name: "default", Running: true, Default: true, Socket: "/tmp/herdr.sock"},
		{Name: "work"},
	}
	if !reflect.DeepEqual(sessions, want) {
		t.Fatalf("ListSessions = %#v, want %#v", sessions, want)
	}
}

func TestListSessionsRejectsUnparsableOutput(t *testing.T) {
	t.Setenv(EnvBinary, writeStub(t, "#!/bin/sh\nprintf 'not json'\n"))

	_, err := New().ListSessions(context.Background())

	if err == nil || !strings.Contains(err.Error(), "parse herdr session list") {
		t.Fatalf("ListSessions error = %v, want a parse error", err)
	}
}

func TestStopSessionReportsTheHerdrError(t *testing.T) {
	t.Setenv(EnvBinary, writeStub(t, "#!/bin/sh\necho 'session is busy' >&2\nexit 3\n"))

	err := New().StopSession(context.Background(), "work")

	if err == nil {
		t.Fatalf("StopSession error = nil, want a failure")
	}
	for _, want := range []string{"herdr session stop work", "exit status 3", "session is busy"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("StopSession error = %q, want it to contain %q", err, want)
		}
	}
}

func TestDeleteSessionPassesTheName(t *testing.T) {
	t.Setenv(EnvBinary, writeStub(t, `#!/bin/sh
if [ "$1 $2 $3" != "session delete ghost" ]; then
  echo "unexpected call: $*" >&2
  exit 9
fi
`))

	if err := New().DeleteSession(context.Background(), "ghost"); err != nil {
		t.Fatalf("DeleteSession error = %v, want nil", err)
	}
}

const snapshotJSON = `{"id":"cli:api:snapshot","result":{"snapshot":{"version":"0.9.5","protocol":22,"focused_workspace_id":"w1","focused_tab_id":"w1:t1","focused_pane_id":"w1:p1",` +
	`"workspaces":[{"workspace_id":"w1","label":"probe","number":1,"focused":true,"active_tab_id":"w1:t2","agent_status":"working","tab_count":2,"pane_count":4},` +
	`{"workspace_id":"w2","label":"other","number":2,"focused":false,"active_tab_id":"w2:t1","agent_status":"idle","tab_count":1,"pane_count":1}],` +
	`"tabs":[{"tab_id":"w1:t1","workspace_id":"w1","number":1,"label":"main","focused":true,"pane_count":3,"agent_status":"working"},` +
	`{"tab_id":"w1:t2","workspace_id":"w1","number":2,"label":"side","focused":false,"pane_count":1,"agent_status":"idle"},` +
	`{"tab_id":"w2:t1","workspace_id":"w2","number":1,"label":"1","focused":false,"pane_count":1,"agent_status":"idle"}],` +
	`"panes":[{"pane_id":"w1:p1","tab_id":"w1:t1","workspace_id":"w1","label":"editor","cwd":"/home/me/project","agent":"pi","agent_status":"working","focused":true},` +
	`{"pane_id":"w1:p2","tab_id":"w1:t1","workspace_id":"w1","cwd":"/home/me/project","agent_status":"idle"},` +
	`{"pane_id":"w1:p3","tab_id":"w1:t1","workspace_id":"w1","cwd":"/tmp","agent_status":"unknown"},` +
	`{"pane_id":"w1:p4","tab_id":"w1:t2","workspace_id":"w1","cwd":"/tmp","agent":"claude","agent_status":"idle"},` +
	`{"pane_id":"w2:p1","tab_id":"w2:t1","workspace_id":"w2","cwd":"/var/tmp","agent_status":"unknown"}],` +
	`"layouts":[{"workspace_id":"w1","tab_id":"w1:t1","zoomed":false,"area":{"x":0,"y":0,"width":120,"height":40},"focused_pane_id":"w1:p1",` +
	`"panes":[{"pane_id":"w1:p1","focused":true,"rect":{"x":0,"y":0,"width":36,"height":40}},` +
	`{"pane_id":"w1:p2","focused":false,"rect":{"x":36,"y":0,"width":36,"height":40}},` +
	`{"pane_id":"w1:p3","focused":false,"rect":{"x":72,"y":0,"width":48,"height":40}}],` +
	`"splits":[{"id":"split_0_root","direction":"right","ratio":0.6,"rect":{"x":0,"y":0,"width":120,"height":40}},` +
	`{"id":"split_1_0","direction":"right","ratio":0.5,"rect":{"x":0,"y":0,"width":72,"height":40}}]},` +
	`{"workspace_id":"w1","tab_id":"w1:t2","zoomed":false,"area":{"x":0,"y":0,"width":120,"height":40},"focused_pane_id":"w1:p4",` +
	`"panes":[{"pane_id":"w1:p4","focused":true,"rect":{"x":0,"y":0,"width":120,"height":40}}],"splits":[]},` +
	`{"workspace_id":"w2","tab_id":"w2:t1","zoomed":false,"area":{"x":0,"y":0,"width":100,"height":30},"focused_pane_id":"w2:p1",` +
	`"panes":[{"pane_id":"w2:p1","focused":true,"rect":{"x":0,"y":0,"width":100,"height":30}}],"splits":[]}]}}}`

func snapshotStub() string {
	return "#!/bin/sh\n" +
		`if [ "$1 $2" != "api snapshot" ]; then` + "\n" +
		`  echo "unexpected call: $*" >&2` + "\n" +
		"  exit 9\n" +
		"fi\n" +
		`printf '%s' '` + snapshotJSON + "'\n"
}

func wantSpace() herdrmock.Space {
	return herdrmock.Space{
		FocusedWorkspaceID: "w1",
		FocusedTabID:       "w1:t1",
		FocusedPaneID:      "w1:p1",
		Workspaces: []herdrmock.Workspace{
			{
				ID:          "w1",
				Label:       "probe",
				Focused:     true,
				ActiveTabID: "w1:t2",
				AgentState:  "working",
				TabCount:    2,
				PaneCount:   4,
				Tabs: []herdrmock.Tab{
					{
						ID:            "w1:t1",
						Label:         "main",
						Focused:       true,
						FocusedPaneID: "w1:p1",
						Panes: []herdrmock.Pane{
							{ID: "w1:p1", WorkspaceID: "w1", TabID: "w1:t1", Label: "editor", Cwd: "/home/me/project", Agent: "pi", AgentState: "working"},
							{ID: "w1:p2", WorkspaceID: "w1", TabID: "w1:t1", Cwd: "/home/me/project", AgentState: "idle"},
							{ID: "w1:p3", WorkspaceID: "w1", TabID: "w1:t1", Cwd: "/tmp", AgentState: "unknown"},
						},
						Root: herdrmock.LayoutNode{
							Direction: "right",
							Ratio:     0.6,
							Children: []herdrmock.LayoutNode{
								{
									Direction: "right",
									Ratio:     0.5,
									Children: []herdrmock.LayoutNode{
										{PaneID: "w1:p1"},
										{PaneID: "w1:p2"},
									},
								},
								{PaneID: "w1:p3"},
							},
						},
					},
					{
						ID:            "w1:t2",
						Label:         "side",
						FocusedPaneID: "w1:p4",
						Panes: []herdrmock.Pane{
							{ID: "w1:p4", WorkspaceID: "w1", TabID: "w1:t2", Cwd: "/tmp", Agent: "claude", AgentState: "idle"},
						},
						Root: herdrmock.LayoutNode{PaneID: "w1:p4"},
					},
				},
			},
			{
				ID:          "w2",
				Label:       "other",
				ActiveTabID: "w2:t1",
				AgentState:  "idle",
				TabCount:    1,
				PaneCount:   1,
				Tabs: []herdrmock.Tab{
					{
						ID:            "w2:t1",
						Label:         "1",
						FocusedPaneID: "w2:p1",
						Panes: []herdrmock.Pane{
							{ID: "w2:p1", WorkspaceID: "w2", TabID: "w2:t1", Cwd: "/var/tmp", AgentState: "unknown"},
						},
						Root: herdrmock.LayoutNode{PaneID: "w2:p1"},
					},
				},
			},
		},
	}
}

func TestSnapshotParsesTheWholeTopology(t *testing.T) {
	t.Setenv(EnvBinary, writeStub(t, snapshotStub()))

	got, err := New().Snapshot(context.Background(), herdrmock.Target{})
	if err != nil {
		t.Fatalf("Snapshot error = %v, want nil", err)
	}

	if want := wantSpace(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Snapshot = %#v, want %#v", got, want)
	}
}

func TestSnapshotTargetsTheSessionSocket(t *testing.T) {
	t.Setenv("HERDR_SOCKET_PATH", "/stale.sock")
	t.Setenv(EnvBinary, writeStub(t, `#!/bin/sh
if [ "$1 $2" != "api snapshot" ]; then
  echo "unexpected call: $*" >&2
  exit 9
fi
if [ "$HERDR_SOCKET_PATH" != "/tmp/work.sock" ]; then
  echo "socket=$HERDR_SOCKET_PATH" >&2
  exit 8
fi
printf '%s' '`+snapshotJSON+`'
`))

	space, err := New().Snapshot(context.Background(), herdrmock.Target{Name: "work", Socket: "/tmp/work.sock"})
	if err != nil {
		t.Fatalf("Snapshot error = %v, want nil", err)
	}

	if got, want := space.Session, "work"; got != want {
		t.Fatalf("Snapshot session = %q, want %q", got, want)
	}
}

func TestSnapshotReportsTheEnvelopeError(t *testing.T) {
	t.Setenv(EnvBinary, writeStub(t, `#!/bin/sh
printf '%s' '{"id":"cli:api:snapshot","error":{"code":"server_not_running","message":"no herdr server is running at /tmp/nope.sock"}}'
`))

	_, err := New().Snapshot(context.Background(), herdrmock.Target{})

	if err == nil {
		t.Fatalf("Snapshot error = nil, want the herdr failure")
	}
	for _, want := range []string{"server_not_running", "no herdr server is running"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("Snapshot error = %q, want it to contain %q", err, want)
		}
	}
}

func TestSnapshotRejectsUnparsableOutput(t *testing.T) {
	t.Setenv(EnvBinary, writeStub(t, "#!/bin/sh\nprintf 'not json'\n"))

	_, err := New().Snapshot(context.Background(), herdrmock.Target{})

	if err == nil || !strings.Contains(err.Error(), "parse herdr api snapshot") {
		t.Fatalf("Snapshot error = %v, want a parse error", err)
	}
}

func TestSnapshotRejectsATabWithoutALayout(t *testing.T) {
	without := strings.Replace(snapshotJSON, `{"workspace_id":"w1","tab_id":"w1:t2"`, `{"workspace_id":"w1","tab_id":"w1:t9"`, 1)
	t.Setenv(EnvBinary, writeStub(t, "#!/bin/sh\n"+`printf '%s' '`+without+`'`+"\n"))

	_, err := New().Snapshot(context.Background(), herdrmock.Target{})

	if err == nil || !strings.Contains(err.Error(), "w1:t2") {
		t.Fatalf("Snapshot error = %v, want it to name the tab without a layout", err)
	}
}

func TestBinaryResolutionExplainsHowToFindHerdr(t *testing.T) {
	t.Setenv(EnvBinary, "")
	t.Setenv("PATH", "")

	_, err := New().ListSessions(context.Background())

	if err == nil || !strings.Contains(err.Error(), EnvBinary) {
		t.Fatalf("ListSessions error = %v, want it to name %s", err, EnvBinary)
	}
}

func TestBinaryPrefersTheClientPathOverTheEnvironment(t *testing.T) {
	explicit := writeStub(t, listStub)
	other := writeStub(t, "#!/bin/sh\nexit 7\n")
	t.Setenv(EnvBinary, other)

	client := &Client{Bin: explicit}
	if _, err := client.ListSessions(context.Background()); err != nil {
		t.Fatalf("ListSessions error = %v, want nil", err)
	}
}

func TestExecerReplacesTheProcessWithTheHerdrClient(t *testing.T) {
	original := execReplace
	t.Cleanup(func() { execReplace = original })

	var gotName string
	var gotArgv, gotEnv []string
	execReplace = func(name string, argv, env []string) error {
		gotName, gotArgv, gotEnv = name, argv, env
		return nil
	}
	t.Setenv(EnvBinary, "/opt/herdr")

	if err := (Execer{}).Attach("work"); err != nil {
		t.Fatalf("Attach error = %v, want nil", err)
	}

	if got, want := gotName, "/opt/herdr"; got != want {
		t.Fatalf("exec path = %q, want %q", got, want)
	}
	wantArgv := []string{"/opt/herdr", "--session", "work"}
	if !reflect.DeepEqual(gotArgv, wantArgv) {
		t.Fatalf("exec argv = %v, want %v", gotArgv, wantArgv)
	}
	if !reflect.DeepEqual(gotEnv, os.Environ()) {
		t.Fatalf("exec environment was not inherited")
	}
}

func TestExecerSurfacesTheReplacementFailure(t *testing.T) {
	original := execReplace
	t.Cleanup(func() { execReplace = original })
	execReplace = func(string, []string, []string) error { return os.ErrPermission }
	t.Setenv(EnvBinary, "/opt/herdr")

	err := (Execer{}).Attach("work")

	if err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("Attach error = %v, want the exec failure", err)
	}
}
