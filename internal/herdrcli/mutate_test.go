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

// recordingStub returns a herdr binary that writes the arguments it received to
// a file and prints response, so a test can assert the exact command line.
func recordingStub(t *testing.T, response string) (string, func() []string) {
	t.Helper()
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + argsFile + "\nprintf '%s' '" + response + "'\n"
	return writeStub(t, script), func() []string {
		data, err := os.ReadFile(argsFile)
		if err != nil {
			t.Fatalf("read recorded arguments: %v", err)
		}
		return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	}
}

const (
	splitResponse  = `{"id":"cli:pane:split","result":{"type":"pane_info","pane":{"pane_id":"w1:p9"}}}`
	createResponse = `{"id":"cli:workspace:create","result":{"type":"workspace_created",` +
		`"workspace":{"workspace_id":"w7"},"tab":{"tab_id":"w7:t1"},"root_pane":{"pane_id":"w7:p1"}}}`
	tabCreateResponse = `{"id":"cli:tab:create","result":{"type":"tab_created",` +
		`"tab":{"tab_id":"w1:t5"},"root_pane":{"pane_id":"w1:p9"}}}`
)

func TestMutatorCommandsAndArguments(t *testing.T) {
	tests := []struct {
		name     string
		response string
		call     func(context.Context, herdrmock.Mutator) error
		want     []string
	}{
		{
			name:     "split pane",
			response: splitResponse,
			call: func(ctx context.Context, mut herdrmock.Mutator) error {
				got, err := mut.SplitPane(ctx, herdrmock.SplitRequest{
					PaneID:    "w1:p1",
					Direction: "right",
					Ratio:     0.6,
					Cwd:       "/tmp",
				})
				if err == nil && got != "w1:p9" {
					t.Errorf("SplitPane = %q, want w1:p9", got)
				}
				return err
			},
			want: []string{"pane", "split", "w1:p1", "--direction", "right", "--ratio", "0.6", "--cwd", "/tmp", "--no-focus"},
		},
		{
			name:     "split pane with focus and no ratio",
			response: splitResponse,
			call: func(ctx context.Context, mut herdrmock.Mutator) error {
				_, err := mut.SplitPane(ctx, herdrmock.SplitRequest{PaneID: "w1:p1", Direction: "down", Focus: true})
				return err
			},
			want: []string{"pane", "split", "w1:p1", "--direction", "down", "--focus"},
		},
		{
			name:     "create workspace",
			response: createResponse,
			call: func(ctx context.Context, mut herdrmock.Mutator) error {
				got, err := mut.CreateWorkspace(ctx, herdrmock.CreateWorkspaceRequest{Cwd: "/home/me/project", Label: "project"})
				want := herdrmock.CreatedPane{WorkspaceID: "w7", TabID: "w7:t1", PaneID: "w7:p1"}
				if err == nil && !reflect.DeepEqual(got, want) {
					t.Errorf("CreateWorkspace = %#v, want %#v", got, want)
				}
				return err
			},
			want: []string{"workspace", "create", "--cwd", "/home/me/project", "--label", "project", "--no-focus"},
		},
		{
			name:     "create tab",
			response: tabCreateResponse,
			call: func(ctx context.Context, mut herdrmock.Mutator) error {
				got, err := mut.CreateTab(ctx, herdrmock.CreateTabRequest{WorkspaceID: "w1", Cwd: "/tmp", Label: "side"})
				want := herdrmock.CreatedTab{TabID: "w1:t5", PaneID: "w1:p9"}
				if err == nil && !reflect.DeepEqual(got, want) {
					t.Errorf("CreateTab = %#v, want %#v", got, want)
				}
				return err
			},
			want: []string{"tab", "create", "--workspace", "w1", "--cwd", "/tmp", "--label", "side", "--no-focus"},
		},
		{
			name: "rename tab",
			call: func(ctx context.Context, mut herdrmock.Mutator) error {
				return mut.RenameTab(ctx, "w1:t1", "main")
			},
			want: []string{"tab", "rename", "w1:t1", "main"},
		},
		{
			name: "rename pane",
			call: func(ctx context.Context, mut herdrmock.Mutator) error {
				return mut.RenamePane(ctx, "w1:p1", "editor")
			},
			want: []string{"pane", "rename", "w1:p1", "editor"},
		},
		{
			name: "focus workspace",
			call: func(ctx context.Context, mut herdrmock.Mutator) error {
				return mut.FocusWorkspace(ctx, "w1")
			},
			want: []string{"workspace", "focus", "w1"},
		},
		{
			name: "focus tab",
			call: func(ctx context.Context, mut herdrmock.Mutator) error {
				return mut.FocusTab(ctx, "w1:t1")
			},
			want: []string{"tab", "focus", "w1:t1"},
		},
		{
			name: "focus agent",
			call: func(ctx context.Context, mut herdrmock.Mutator) error {
				return mut.FocusAgent(ctx, "pi")
			},
			want: []string{"agent", "focus", "pi"},
		},
		{
			name: "start agent",
			call: func(ctx context.Context, mut herdrmock.Mutator) error {
				return mut.StartAgent(ctx, herdrmock.StartAgentRequest{Name: "pi", Kind: "pi", PaneID: "w1:p1"})
			},
			want: []string{"agent", "start", "pi", "--kind", "pi", "--pane", "w1:p1"},
		},
		{
			name: "close pane",
			call: func(ctx context.Context, mut herdrmock.Mutator) error {
				return mut.ClosePane(ctx, "w1:p2")
			},
			want: []string{"pane", "close", "w1:p2"},
		},
		{
			name: "close tab",
			call: func(ctx context.Context, mut herdrmock.Mutator) error {
				return mut.CloseTab(ctx, "w1:t2")
			},
			want: []string{"tab", "close", "w1:t2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bin, args := recordingStub(t, tt.response)
			t.Setenv(EnvBinary, bin)

			if err := tt.call(context.Background(), New().Mutations(herdrmock.Target{})); err != nil {
				t.Fatalf("call error = %v, want nil", err)
			}
			if got := args(); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("command = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMutationsTargetTheSessionSocket(t *testing.T) {
	script := `#!/bin/sh
if [ "$HERDR_SOCKET_PATH" != "/tmp/work.sock" ]; then
  echo "socket=$HERDR_SOCKET_PATH" >&2
  exit 8
fi
printf '%s' '` + splitResponse + `'
`
	t.Setenv(EnvBinary, writeStub(t, script))
	t.Setenv(EnvSocketPath, "/stale.sock")

	mut := New().Mutations(herdrmock.Target{Name: "work", Socket: "/tmp/work.sock"})
	if _, err := mut.SplitPane(context.Background(), herdrmock.SplitRequest{PaneID: "w1:p1", Direction: "right"}); err != nil {
		t.Fatalf("SplitPane error = %v, want nil", err)
	}
}

func TestMutationsSurfaceHerdrErrors(t *testing.T) {
	t.Setenv(EnvBinary, writeStub(t, "#!/bin/sh\necho 'pane is gone' >&2\nexit 3\n"))

	err := New().Mutations(herdrmock.Target{}).RenamePane(context.Background(), "w1:p1", "editor")

	if err == nil {
		t.Fatalf("RenamePane error = nil, want the herdr failure")
	}
	for _, want := range []string{"herdr pane rename w1:p1 editor", "exit status 3", "pane is gone"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %q, want it to contain %q", err.Error(), want)
		}
	}
}

func TestSplitPaneRejectsAResponseWithoutAPane(t *testing.T) {
	t.Setenv(EnvBinary, writeStub(t, "#!/bin/sh\nprintf '%s' '{\"id\":\"cli:pane:split\",\"result\":{\"type\":\"pane_info\"}}'\n"))

	_, err := New().Mutations(herdrmock.Target{}).SplitPane(context.Background(), herdrmock.SplitRequest{PaneID: "w1:p1", Direction: "right"})

	if err == nil || !strings.Contains(err.Error(), "pane split") {
		t.Fatalf("SplitPane error = %v, want a parse failure naming the call", err)
	}
}
