package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/WilberC/herdr-suite/internal/herdrmock"
	"github.com/WilberC/herdr-suite/internal/last"
)

type lastHarness struct {
	deps   deps
	client *herdrmock.Fake
	env    map[string]string
	out    *bytes.Buffer
	err    *bytes.Buffer
}

func newLastHarness(t *testing.T) *lastHarness {
	t.Helper()
	client := &herdrmock.Fake{}
	harness := &lastHarness{
		client: client,
		env: map[string]string{
			"HERDR_PLUGIN_STATE_DIR": t.TempDir(),
			"HERDR_SOCKET_PATH":      "/tmp/herdr/last-test.sock",
		},
		out: &bytes.Buffer{},
		err: &bytes.Buffer{},
	}
	harness.deps = deps{
		writer: client,
		getenv: func(key string) string { return harness.env[key] },
		out:    harness.out,
		err:    harness.err,
	}
	return harness
}

func (h *lastHarness) run(args ...string) int {
	return runWith(h.deps, args, h.out, h.err)
}

func (h *lastHarness) statePath(t *testing.T) string {
	t.Helper()
	path, err := last.Path(h.deps.getenv)
	if err != nil {
		t.Fatalf("last.Path: %v", err)
	}
	return path
}

func (h *lastHarness) seed(t *testing.T, state last.State) {
	t.Helper()
	if err := last.Save(h.statePath(t), state); err != nil {
		t.Fatalf("seed the state: %v", err)
	}
}

func (h *lastHarness) state(t *testing.T) last.State {
	t.Helper()
	state, err := last.Load(h.statePath(t))
	if err != nil {
		t.Fatalf("read the state: %v", err)
	}
	return state
}

func lastString(value string) *string { return &value }

func assertSilent(t *testing.T, harness *lastHarness) {
	t.Helper()
	if harness.out.Len() != 0 {
		t.Fatalf("stdout = %q, want nothing", harness.out.String())
	}
	if harness.err.Len() != 0 {
		t.Fatalf("stderr = %q, want nothing", harness.err.String())
	}
}

func TestLastObserveRecordsFocusEventsSilently(t *testing.T) {
	harness := newLastHarness(t)
	harness.env["HERDR_PLUGIN_EVENT"] = "workspace.focused"

	events := []string{
		`{"event":"workspace_focused","data":{"type":"workspace_focused","workspace_id":"w1"}}`,
		`{"event":"workspace_focused","data":{"type":"workspace_focused","workspace_id":"w2"}}`,
		`{"event":"workspace_focused","data":{"type":"workspace_focused","workspace_id":"w1"}}`,
	}
	want := []struct{ prev, cur string }{{"", "w1"}, {"w1", "w2"}, {"w2", "w1"}}
	for index, payload := range events {
		harness.env["HERDR_PLUGIN_EVENT_JSON"] = payload
		harness.out.Reset()
		harness.err.Reset()
		if exit := harness.run("last", "observe"); exit != 0 {
			t.Fatalf("event %d: exit = %d, want 0; stderr = %q", index, exit, harness.err.String())
		}
		assertSilent(t, harness)

		state := harness.state(t)
		if got := pointerValue(state.CurWorkspace); got != want[index].cur {
			t.Fatalf("event %d: cur workspace = %q, want %q", index, got, want[index].cur)
		}
		if got := pointerValue(state.PrevWorkspace); got != want[index].prev {
			t.Fatalf("event %d: prev workspace = %q, want %q", index, got, want[index].prev)
		}
		if state.CurTab != nil || state.PrevTab != nil {
			t.Fatalf("event %d: tab pair = %#v, want it untouched", index, state)
		}
	}
}

func pointerValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func TestLastObserveNeverFailsAFocusEvent(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
	}{
		{
			name: "an unknown kind",
			env:  map[string]string{"HERDR_PLUGIN_EVENT": "pane.focused", "HERDR_PLUGIN_EVENT_JSON": `{"data":{"pane_id":"p1"}}`},
		},
		{
			name: "a missing kind",
			env:  map[string]string{"HERDR_PLUGIN_EVENT_JSON": `{"data":{"workspace_id":"w1"}}`},
		},
		{
			name: "a missing payload",
			env:  map[string]string{"HERDR_PLUGIN_EVENT": "workspace.focused"},
		},
		{
			name: "a malformed payload",
			env:  map[string]string{"HERDR_PLUGIN_EVENT": "workspace.focused", "HERDR_PLUGIN_EVENT_JSON": "{not json"},
		},
		{
			name: "a payload without the id",
			env:  map[string]string{"HERDR_PLUGIN_EVENT": "tab.focused", "HERDR_PLUGIN_EVENT_JSON": `{"data":{}}`},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			harness := newLastHarness(t)
			for key, value := range test.env {
				harness.env[key] = value
			}

			if exit := harness.run("last", "observe"); exit != 0 {
				t.Fatalf("exit = %d, want 0; stderr = %q", exit, harness.err.String())
			}
			assertSilent(t, harness)
			if _, err := os.Stat(harness.statePath(t)); !os.IsNotExist(err) {
				t.Fatalf("state file exists after %s, want no write", test.name)
			}
		})
	}
}

func TestLastObserveRecoversFromACorruptStateFile(t *testing.T) {
	harness := newLastHarness(t)
	harness.env["HERDR_PLUGIN_EVENT"] = "tab.focused"
	harness.env["HERDR_PLUGIN_EVENT_JSON"] = `{"data":{"tab_id":"t1","workspace_id":"w1"}}`
	path := harness.statePath(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create the state directory: %v", err)
	}
	if err := os.WriteFile(path, []byte("garbage"), 0o644); err != nil {
		t.Fatalf("write the corrupt state: %v", err)
	}

	if exit := harness.run("last", "observe"); exit != 0 {
		t.Fatalf("exit = %d, want 0; stderr = %q", exit, harness.err.String())
	}
	assertSilent(t, harness)
	state := harness.state(t)
	if got := pointerValue(state.CurTab); got != "t1" {
		t.Fatalf("cur tab = %q, want t1", got)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatalf("read the state directory: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != filepath.Base(path) {
		t.Fatalf("state directory = %v, want only %s", entries, filepath.Base(path))
	}
}

func TestLastWorkspaceTogglesAndSwaps(t *testing.T) {
	harness := newLastHarness(t)
	harness.seed(t, last.State{PrevWorkspace: lastString("w1"), CurWorkspace: lastString("w2")})

	if exit := harness.run("last", "workspace"); exit != 0 {
		t.Fatalf("first toggle: exit = %d, want 0; stderr = %q", exit, harness.err.String())
	}
	assertSilent(t, harness)
	state := harness.state(t)
	if pointerValue(state.PrevWorkspace) != "w2" || pointerValue(state.CurWorkspace) != "w1" {
		t.Fatalf("state after the first toggle = %#v, want prev w2 and cur w1", state)
	}

	if exit := harness.run("last", "workspace"); exit != 0 {
		t.Fatalf("second toggle: exit = %d, want 0; stderr = %q", exit, harness.err.String())
	}
	state = harness.state(t)
	if pointerValue(state.PrevWorkspace) != "w1" || pointerValue(state.CurWorkspace) != "w2" {
		t.Fatalf("state after the second toggle = %#v, want the pair swapped back", state)
	}

	ops := harness.client.Ops()
	if len(ops) != 2 {
		t.Fatalf("mutations = %#v, want two focus calls", ops)
	}
	if ops[0].Method != "FocusWorkspace" || ops[0].WorkspaceID != "w1" {
		t.Fatalf("first mutation = %#v, want FocusWorkspace w1", ops[0])
	}
	if ops[1].Method != "FocusWorkspace" || ops[1].WorkspaceID != "w2" {
		t.Fatalf("second mutation = %#v, want FocusWorkspace w2", ops[1])
	}
}

func TestLastTabTogglesAndSwaps(t *testing.T) {
	harness := newLastHarness(t)
	harness.seed(t, last.State{PrevTab: lastString("t1"), CurTab: lastString("t2")})

	if exit := harness.run("last", "tab"); exit != 0 {
		t.Fatalf("toggle: exit = %d, want 0; stderr = %q", exit, harness.err.String())
	}
	assertSilent(t, harness)
	state := harness.state(t)
	if pointerValue(state.PrevTab) != "t2" || pointerValue(state.CurTab) != "t1" {
		t.Fatalf("state after the toggle = %#v, want prev t2 and cur t1", state)
	}
	ops := harness.client.Ops()
	if len(ops) != 1 || ops[0].Method != "FocusTab" || ops[0].TabID != "t1" {
		t.Fatalf("mutations = %#v, want FocusTab t1", ops)
	}
}

func TestLastRefusals(t *testing.T) {
	t.Run("workspace with no history", func(t *testing.T) {
		harness := newLastHarness(t)

		if exit := harness.run("last", "workspace"); exit != 2 {
			t.Fatalf("exit = %d, want 2; stderr = %q", exit, harness.err.String())
		}
		want := "hs: " + last.NoPreviousWorkspace + "\n"
		if harness.err.String() != want {
			t.Fatalf("stderr = %q, want %q", harness.err.String(), want)
		}
		if harness.out.Len() != 0 {
			t.Fatalf("stdout = %q, want nothing", harness.out.String())
		}
		if len(harness.client.Ops()) != 0 {
			t.Fatalf("mutations = %#v, want none", harness.client.Ops())
		}
		if _, err := os.Stat(harness.statePath(t)); !os.IsNotExist(err) {
			t.Fatalf("a refusal wrote a state file")
		}
	})

	t.Run("tab with no history in this workspace", func(t *testing.T) {
		harness := newLastHarness(t)
		harness.seed(t, last.State{PrevWorkspace: lastString("w1"), CurWorkspace: lastString("w2")})

		if exit := harness.run("last", "tab"); exit != 2 {
			t.Fatalf("exit = %d, want 2; stderr = %q", exit, harness.err.String())
		}
		want := "hs: " + last.NoPreviousTab + "\n"
		if harness.err.String() != want {
			t.Fatalf("stderr = %q, want %q", harness.err.String(), want)
		}
		if len(harness.client.Ops()) != 0 {
			t.Fatalf("mutations = %#v, want none", harness.client.Ops())
		}
	})
}

func TestLastReportsAFailedFocus(t *testing.T) {
	harness := newLastHarness(t)
	harness.seed(t, last.State{PrevWorkspace: lastString("w1"), CurWorkspace: lastString("w2")})
	harness.client.FocusWorkspaceErr = errors.New("herdr workspace focus w1: exit status 1")

	if exit := harness.run("last", "workspace"); exit != 1 {
		t.Fatalf("exit = %d, want 1; stderr = %q", exit, harness.err.String())
	}
	want := "hs: herdr workspace focus w1: exit status 1\n"
	if harness.err.String() != want {
		t.Fatalf("stderr = %q, want %q", harness.err.String(), want)
	}
	state := harness.state(t)
	if pointerValue(state.PrevWorkspace) != "w1" || pointerValue(state.CurWorkspace) != "w2" {
		t.Fatalf("state after a failed focus = %#v, want it unchanged", state)
	}
}

func TestLastUsageAndHelp(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantExit   int
		wantStdout []string
		wantStderr []string
	}{
		{
			name:       "no subcommand prints the group usage on stderr",
			args:       []string{"last"},
			wantExit:   2,
			wantStderr: []string{"usage: hs last", "workspace", "tab", "observe"},
		},
		{
			name:       "help prints the group usage on stdout",
			args:       []string{"last", "--help"},
			wantExit:   0,
			wantStdout: []string{"usage: hs last", "workspace", "tab", "observe"},
		},
		{
			name:       "the workspace usage is documented",
			args:       []string{"last", "workspace", "--help"},
			wantExit:   0,
			wantStdout: []string{"usage: hs last workspace"},
		},
		{
			name:       "the tab usage is documented",
			args:       []string{"last", "tab", "--help"},
			wantExit:   0,
			wantStdout: []string{"usage: hs last tab"},
		},
		{
			name:       "the observe usage is documented",
			args:       []string{"last", "observe", "--help"},
			wantExit:   0,
			wantStdout: []string{"usage: hs last observe", "HERDR_PLUGIN_EVENT"},
		},
		{
			name:       "an unknown subcommand is refused",
			args:       []string{"last", "bogus"},
			wantExit:   2,
			wantStderr: []string{`unknown last command "bogus"`, "usage: hs last"},
		},
		{
			name:       "the toggles take no arguments",
			args:       []string{"last", "workspace", "extra"},
			wantExit:   2,
			wantStderr: []string{"hs: workspace takes no arguments", "usage: hs last workspace"},
		},
		{
			name:       "observe takes no arguments",
			args:       []string{"last", "observe", "--verbose"},
			wantExit:   2,
			wantStderr: []string{"hs: unknown flag \"--verbose\"", "usage: hs last observe"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			harness := newLastHarness(t)
			exit := harness.run(test.args...)
			if exit != test.wantExit {
				t.Fatalf("exit = %d, want %d", exit, test.wantExit)
			}
			for _, want := range test.wantStdout {
				if !strings.Contains(harness.out.String(), want) {
					t.Fatalf("stdout = %q, want it to name %q", harness.out.String(), want)
				}
			}
			for _, want := range test.wantStderr {
				if !strings.Contains(harness.err.String(), want) {
					t.Fatalf("stderr = %q, want it to name %q", harness.err.String(), want)
				}
			}
		})
	}
}
