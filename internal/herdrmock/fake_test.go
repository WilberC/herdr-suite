package herdrmock_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/WilberC/herdr-suite/internal/herdrmock"
)

func fixture() *herdrmock.Fake {
	return &herdrmock.Fake{
		Sessions: []herdrmock.Session{
			{Name: "work", Running: true},
			{Name: "scratch"},
		},
		Panes: map[string]herdrmock.Pane{
			"p-1": {
				ID:          "p-1",
				WorkspaceID: "w-1",
				TabID:       "t-1",
				Label:       "editor",
				Cwd:         "/home/me/project",
				Agent:       "pi",
			},
		},
		Spaces: map[string]herdrmock.Space{
			"": {
				Session:            "work",
				FocusedWorkspaceID: "w-1",
				FocusedTabID:       "t-1",
				FocusedPaneID:      "p-1",
				Workspaces: []herdrmock.Workspace{
					{
						ID:         "w-1",
						Label:      "project",
						Focused:    true,
						TabCount:   1,
						PaneCount:  2,
						AgentState: "working",
					},
				},
			},
		},
		Workspaces: map[string]herdrmock.Workspace{
			"w-1": {
				ID:    "w-1",
				Label: "project",
				Tabs: []herdrmock.Tab{
					{
						ID:    "t-1",
						Label: "main",
						Root: herdrmock.LayoutNode{
							Direction: "right",
							Ratio:     0.6,
							Children: []herdrmock.LayoutNode{
								{PaneID: "p-1"},
								{PaneID: "p-2"},
							},
						},
					},
				},
			},
		},
		NextPaneID: "p-9",
	}
}

func TestClientContract(t *testing.T) {
	splitRequest := herdrmock.SplitRequest{
		PaneID:    "p-1",
		Direction: "down",
		Ratio:     0.5,
		Cwd:       "/home/me/project",
		Label:     "logs",
	}
	agentRequest := herdrmock.StartAgentRequest{
		Name:   "reviewer",
		Kind:   "pi",
		PaneID: "p-1",
		Args:   []string{"--model", "fast"},
	}
	topology := fixture().Workspaces["w-1"]

	tests := []struct {
		name      string
		fake      func() *herdrmock.Fake
		call      func(context.Context, herdrmock.Client) (any, error)
		want      any
		wantErr   error
		wantCalls []herdrmock.Call
	}{
		{
			name: "list sessions returns the scripted sessions",
			fake: fixture,
			call: func(ctx context.Context, client herdrmock.Client) (any, error) {
				return client.ListSessions(ctx)
			},
			want:      fixture().Sessions,
			wantCalls: []herdrmock.Call{{Method: "ListSessions"}},
		},
		{
			name: "list sessions surfaces scripted errors",
			fake: func() *herdrmock.Fake {
				f := fixture()
				f.ListSessionsErr = errors.New("socket unavailable")
				return f
			},
			call: func(ctx context.Context, client herdrmock.Client) (any, error) {
				return client.ListSessions(ctx)
			},
			wantErr:   errors.New("socket unavailable"),
			wantCalls: []herdrmock.Call{{Method: "ListSessions"}},
		},
		{
			name: "stop session records the target name",
			fake: fixture,
			call: func(ctx context.Context, client herdrmock.Client) (any, error) {
				return nil, client.StopSession(ctx, "work")
			},
			wantCalls: []herdrmock.Call{{Method: "StopSession", Target: "work"}},
		},
		{
			name: "delete session records the target name",
			fake: fixture,
			call: func(ctx context.Context, client herdrmock.Client) (any, error) {
				return nil, client.DeleteSession(ctx, "scratch")
			},
			wantCalls: []herdrmock.Call{{Method: "DeleteSession", Target: "scratch"}},
		},
		{
			name: "workspace returns the scripted topology",
			fake: fixture,
			call: func(ctx context.Context, client herdrmock.Client) (any, error) {
				return client.Workspace(ctx, "w-1")
			},
			want:      topology,
			wantCalls: []herdrmock.Call{{Method: "Workspace", Target: "w-1"}},
		},
		{
			name: "workspace rejects an unknown id",
			fake: fixture,
			call: func(ctx context.Context, client herdrmock.Client) (any, error) {
				return client.Workspace(ctx, "w-missing")
			},
			wantErr:   errors.New(`unknown workspace "w-missing"`),
			wantCalls: []herdrmock.Call{{Method: "Workspace", Target: "w-missing"}},
		},
		{
			name: "snapshot returns the scripted space",
			fake: fixture,
			call: func(ctx context.Context, client herdrmock.Client) (any, error) {
				return herdrmock.SpaceClient(client).Snapshot(ctx, herdrmock.Target{})
			},
			want:      fixture().Spaces[""],
			wantCalls: []herdrmock.Call{{Method: "Snapshot"}},
		},
		{
			name: "snapshot records an explicit session",
			fake: fixture,
			call: func(ctx context.Context, client herdrmock.Client) (any, error) {
				return herdrmock.SpaceClient(client).Snapshot(ctx, herdrmock.Target{Name: "work", Socket: "/tmp/work.sock"})
			},
			wantErr:   errors.New(`unknown session topology "work"`),
			wantCalls: []herdrmock.Call{{Method: "Snapshot", Target: "work"}},
		},
		{
			name: "pane returns the scripted pane",
			fake: fixture,
			call: func(ctx context.Context, client herdrmock.Client) (any, error) {
				return client.Pane(ctx, "p-1")
			},
			want:      fixture().Panes["p-1"],
			wantCalls: []herdrmock.Call{{Method: "Pane", Target: "p-1"}},
		},
		{
			name: "pane rejects an unknown id",
			fake: fixture,
			call: func(ctx context.Context, client herdrmock.Client) (any, error) {
				return client.Pane(ctx, "p-missing")
			},
			wantErr:   errors.New(`unknown pane "p-missing"`),
			wantCalls: []herdrmock.Call{{Method: "Pane", Target: "p-missing"}},
		},
		{
			name: "split pane returns the next pane id",
			fake: fixture,
			call: func(ctx context.Context, client herdrmock.Client) (any, error) {
				return client.SplitPane(ctx, splitRequest)
			},
			want:      "p-9",
			wantCalls: []herdrmock.Call{{Method: "SplitPane", Target: "p-1"}},
		},
		{
			name: "split pane surfaces scripted errors",
			fake: func() *herdrmock.Fake {
				f := fixture()
				f.SplitPaneErr = errors.New("pane is gone")
				return f
			},
			call: func(ctx context.Context, client herdrmock.Client) (any, error) {
				return client.SplitPane(ctx, splitRequest)
			},
			wantErr:   errors.New("pane is gone"),
			wantCalls: []herdrmock.Call{{Method: "SplitPane", Target: "p-1"}},
		},
		{
			name: "start agent records the request",
			fake: fixture,
			call: func(ctx context.Context, client herdrmock.Client) (any, error) {
				return nil, client.StartAgent(ctx, agentRequest)
			},
			wantCalls: []herdrmock.Call{{Method: "StartAgent", Target: "p-1"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var client herdrmock.Client = tt.fake()
			ctx := context.Background()

			got, gotErr := tt.call(ctx, client)

			if tt.wantErr != nil {
				if gotErr == nil || gotErr.Error() != tt.wantErr.Error() {
					t.Fatalf("error = %v, want %v", gotErr, tt.wantErr)
				}
			} else if gotErr != nil {
				t.Fatalf("unexpected error: %v", gotErr)
			}
			if tt.wantErr == nil && !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("result = %#v, want %#v", got, tt.want)
			}
			fake, ok := client.(*herdrmock.Fake)
			if !ok {
				t.Fatalf("client is %T, want *herdrmock.Fake", client)
			}
			if !reflect.DeepEqual(fake.Calls(), tt.wantCalls) {
				t.Fatalf("calls = %#v, want %#v", fake.Calls(), tt.wantCalls)
			}
		})
	}
}
