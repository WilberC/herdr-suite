package last

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/WilberC/herdr-suite/internal/herdrmock"
)

func env(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func pointer(value string) *string { return &value }

func statePath(t *testing.T, values map[string]string) string {
	t.Helper()
	path, err := Path(env(values))
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	return path
}

func readState(t *testing.T, path string) State {
	t.Helper()
	state, err := Load(path)
	if err != nil {
		t.Fatalf("Load %s: %v", path, err)
	}
	return state
}

func TestPathKeysTheStateFileBySocket(t *testing.T) {
	dir := t.TempDir()
	first := statePath(t, map[string]string{"HERDR_PLUGIN_STATE_DIR": dir, "HERDR_SOCKET_PATH": "/tmp/herdr/one.sock"})
	second := statePath(t, map[string]string{"HERDR_PLUGIN_STATE_DIR": dir, "HERDR_SOCKET_PATH": "/tmp/herdr/two.sock"})

	if first == second {
		t.Fatalf("both sockets resolve to %s, want one file per session", first)
	}
	if got, want := filepath.Dir(first), dir; got != want {
		t.Fatalf("state directory = %q, want %q", got, want)
	}

	sum := sha256.Sum256([]byte("/tmp/herdr/one.sock"))
	want := filepath.Join(dir, "last-"+hex.EncodeToString(sum[:])[:12]+".json")
	if first != want {
		t.Fatalf("state path = %q, want %q", first, want)
	}
}

func TestPathFallsBackWhenThePluginProvidesNoStateDirectory(t *testing.T) {
	path := statePath(t, map[string]string{"XDG_STATE_HOME": "/state", "HERDR_SOCKET_PATH": "/tmp/a.sock"})
	if want := filepath.Join("/state", "herdr", "plugins", "herdr-suite"); filepath.Dir(path) != want {
		t.Fatalf("state directory = %q, want %q", filepath.Dir(path), want)
	}

	home := statePath(t, map[string]string{"HOME": "/home/person", "HERDR_SOCKET_PATH": ""})
	if want := filepath.Join("/home/person", ".local", "state", "herdr", "plugins", "herdr-suite", "last-default.json"); home != want {
		t.Fatalf("state path without a socket = %q, want %q", home, want)
	}

	if _, err := Path(env(map[string]string{})); err == nil {
		t.Fatal("Path with no state directory and no HOME = nil error, want a refusal")
	}
}

func TestRecordTransitions(t *testing.T) {
	values := map[string]string{"HERDR_PLUGIN_STATE_DIR": t.TempDir(), "HERDR_SOCKET_PATH": "/tmp/one.sock"}
	path := statePath(t, values)
	record := func(kind, id string) {
		t.Helper()
		if err := Record(env(values), kind, id); err != nil {
			t.Fatalf("Record(%q, %q): %v", kind, id, err)
		}
	}

	record(KindWorkspace, "w1")
	state := readState(t, path)
	if state.PrevWorkspace != nil || state.CurWorkspace == nil || *state.CurWorkspace != "w1" {
		t.Fatalf("fresh state = %#v, want prev null and cur w1", state)
	}

	record(KindWorkspace, "w2")
	state = readState(t, path)
	if state.PrevWorkspace == nil || *state.PrevWorkspace != "w1" || state.CurWorkspace == nil || *state.CurWorkspace != "w2" {
		t.Fatalf("second workspace = %#v, want prev w1 and cur w2", state)
	}

	record(KindWorkspace, "w1")
	state = readState(t, path)
	if state.PrevWorkspace == nil || *state.PrevWorkspace != "w2" || state.CurWorkspace == nil || *state.CurWorkspace != "w1" {
		t.Fatalf("alternating workspace = %#v, want prev w2 and cur w1", state)
	}
}

func TestRecordAlwaysWritesNullForMissingFields(t *testing.T) {
	values := map[string]string{"HERDR_PLUGIN_STATE_DIR": t.TempDir(), "HERDR_SOCKET_PATH": "/tmp/one.sock"}
	if err := Record(env(values), KindTab, "t1"); err != nil {
		t.Fatalf("Record: %v", err)
	}

	data, err := os.ReadFile(statePath(t, values))
	if err != nil {
		t.Fatalf("read the state file: %v", err)
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatalf("state file is not JSON: %v\n%s", err, data)
	}
	for _, key := range []string{"prev_workspace", "cur_workspace", "prev_tab", "cur_tab"} {
		value, ok := document[key]
		if !ok {
			t.Fatalf("state file %s omits %q, want an explicit null", data, key)
		}
		if key == "cur_tab" {
			if value != "t1" {
				t.Fatalf("state file %s has %s = %v, want t1", data, key, value)
			}
			continue
		}
		if value != nil {
			t.Fatalf("state file %s has %s = %v, want null", data, key, value)
		}
	}
}

func TestRecordSameIDIsANoOp(t *testing.T) {
	values := map[string]string{"HERDR_PLUGIN_STATE_DIR": t.TempDir(), "HERDR_SOCKET_PATH": "/tmp/one.sock"}
	path := statePath(t, values)
	seeded := State{PrevWorkspace: pointer("w9"), CurWorkspace: pointer("w1")}
	if err := Save(path, seeded); err != nil {
		t.Fatalf("Save: %v", err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the state file: %v", err)
	}

	if err := Record(env(values), KindWorkspace, "w1"); err != nil {
		t.Fatalf("Record: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the state file: %v", err)
	}
	if string(before) != string(after) {
		t.Fatalf("same-id event rewrote the state:\nbefore %s\nafter  %s", before, after)
	}
	state := readState(t, path)
	if state.PrevWorkspace == nil || *state.PrevWorkspace != "w9" {
		t.Fatalf("state = %#v, want prev w9 kept: a repeat focus must never collapse prev into cur", state)
	}
}

func TestRecordUnknownKindAndEmptyIDAreNoOps(t *testing.T) {
	values := map[string]string{"HERDR_PLUGIN_STATE_DIR": t.TempDir(), "HERDR_SOCKET_PATH": "/tmp/one.sock"}
	path := statePath(t, values)

	if err := Record(env(values), "pane.focused", "p1"); err != nil {
		t.Fatalf("Record with an unknown kind: %v", err)
	}
	if err := Record(env(values), KindWorkspace, ""); err != nil {
		t.Fatalf("Record with an empty id: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("unknown events created %s, want no state file", path)
	}
}

func TestLoadTreatsMissingAndCorruptFilesAsEmpty(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing.json")
	state, err := Load(missing)
	if err != nil {
		t.Fatalf("Load a missing file: %v", err)
	}
	if state.PrevWorkspace != nil || state.CurWorkspace != nil || state.PrevTab != nil || state.CurTab != nil {
		t.Fatalf("state from a missing file = %#v, want an empty state", state)
	}

	corrupt := filepath.Join(dir, "corrupt.json")
	if err := os.WriteFile(corrupt, []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write the corrupt file: %v", err)
	}
	state, err = Load(corrupt)
	if err != nil {
		t.Fatalf("Load a corrupt file: %v", err)
	}
	if state.PrevWorkspace != nil || state.CurWorkspace != nil || state.PrevTab != nil || state.CurTab != nil {
		t.Fatalf("state from a corrupt file = %#v, want an empty state", state)
	}
}

func TestRecordRecoversFromACorruptFile(t *testing.T) {
	values := map[string]string{"HERDR_PLUGIN_STATE_DIR": t.TempDir(), "HERDR_SOCKET_PATH": "/tmp/one.sock"}
	path := statePath(t, values)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create the state directory: %v", err)
	}
	if err := os.WriteFile(path, []byte("garbage"), 0o644); err != nil {
		t.Fatalf("write the corrupt file: %v", err)
	}

	if err := Record(env(values), KindWorkspace, "w1"); err != nil {
		t.Fatalf("Record: %v", err)
	}
	state := readState(t, path)
	if state.PrevWorkspace != nil || state.CurWorkspace == nil || *state.CurWorkspace != "w1" {
		t.Fatalf("state after recovery = %#v, want prev null and cur w1", state)
	}
}

func TestSaveIsAtomicAndLeavesNoTemporaryFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "last.json")
	if err := Save(path, State{CurWorkspace: pointer("w1")}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := Save(path, State{PrevWorkspace: pointer("w1"), CurWorkspace: pointer("w2")}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read the state directory: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "last.json" {
		t.Fatalf("state directory = %v, want only last.json and no leftover temporary file", entries)
	}
	state := readState(t, path)
	if state.PrevWorkspace == nil || *state.PrevWorkspace != "w1" || state.CurWorkspace == nil || *state.CurWorkspace != "w2" {
		t.Fatalf("state = %#v, want the latest write", state)
	}
}

func TestObserveReadsTheEventPayload(t *testing.T) {
	values := map[string]string{"HERDR_PLUGIN_STATE_DIR": t.TempDir(), "HERDR_SOCKET_PATH": "/tmp/one.sock"}
	path := statePath(t, values)

	workspace := `{"event":"workspace_focused","data":{"type":"workspace_focused","workspace_id":"w1"}}`
	if err := Observe(env(values), KindWorkspace, workspace); err != nil {
		t.Fatalf("Observe a workspace event: %v", err)
	}
	state := readState(t, path)
	if state.CurWorkspace == nil || *state.CurWorkspace != "w1" {
		t.Fatalf("state = %#v, want cur workspace w1", state)
	}

	tab := `{"event":"tab_focused","data":{"type":"tab_focused","tab_id":"t1","workspace_id":"w1"}}`
	if err := Observe(env(values), KindTab, tab); err != nil {
		t.Fatalf("Observe a tab event: %v", err)
	}
	state = readState(t, path)
	if state.CurTab == nil || *state.CurTab != "t1" {
		t.Fatalf("state = %#v, want cur tab t1", state)
	}

	for _, payload := range []string{"", "not json", `{"data":{}}`} {
		if err := Observe(env(values), KindWorkspace, payload); err != nil {
			t.Fatalf("Observe(%q): %v", payload, err)
		}
	}
}

func TestToggleSwapsThePairBackAndForth(t *testing.T) {
	values := map[string]string{"HERDR_PLUGIN_STATE_DIR": t.TempDir(), "HERDR_SOCKET_PATH": "/tmp/one.sock"}
	path := statePath(t, values)
	if err := Save(path, State{PrevWorkspace: pointer("w1"), CurWorkspace: pointer("w2"), PrevTab: pointer("t1"), CurTab: pointer("t2")}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	client := &herdrmock.Fake{}

	if err := ToggleWorkspace(context.Background(), client.Mutations(herdrmock.Target{}), env(values)); err != nil {
		t.Fatalf("ToggleWorkspace: %v", err)
	}
	state := readState(t, path)
	if state.PrevWorkspace == nil || *state.PrevWorkspace != "w2" || state.CurWorkspace == nil || *state.CurWorkspace != "w1" {
		t.Fatalf("state after the first toggle = %#v, want the pair swapped", state)
	}
	if state.PrevTab == nil || *state.PrevTab != "t1" || state.CurTab == nil || *state.CurTab != "t2" {
		t.Fatalf("state after a workspace toggle = %#v, want the tab pair untouched", state)
	}

	if err := ToggleTab(context.Background(), client.Mutations(herdrmock.Target{}), env(values)); err != nil {
		t.Fatalf("ToggleTab: %v", err)
	}
	state = readState(t, path)
	if state.PrevTab == nil || *state.PrevTab != "t2" || state.CurTab == nil || *state.CurTab != "t1" {
		t.Fatalf("state after the tab toggle = %#v, want the tab pair swapped", state)
	}

	ops := client.Ops()
	if len(ops) != 2 {
		t.Fatalf("mutations = %#v, want one workspace focus and one tab focus", ops)
	}
	if ops[0].Method != "FocusWorkspace" || ops[0].WorkspaceID != "w1" {
		t.Fatalf("first mutation = %#v, want FocusWorkspace w1", ops[0])
	}
	if ops[1].Method != "FocusTab" || ops[1].TabID != "t1" {
		t.Fatalf("second mutation = %#v, want FocusTab t1", ops[1])
	}
}

func TestToggleRefusesWhenHistoryIsEmpty(t *testing.T) {
	values := map[string]string{"HERDR_PLUGIN_STATE_DIR": t.TempDir(), "HERDR_SOCKET_PATH": "/tmp/one.sock"}
	client := &herdrmock.Fake{}

	err := ToggleWorkspace(context.Background(), client.Mutations(herdrmock.Target{}), env(values))
	var refusal Refusal
	if !errors.As(err, &refusal) {
		t.Fatalf("ToggleWorkspace error = %v, want a Refusal", err)
	}
	if refusal.Reason != NoPreviousWorkspace {
		t.Fatalf("refusal = %q, want %q", refusal.Reason, NoPreviousWorkspace)
	}
	if len(client.Ops()) != 0 {
		t.Fatalf("mutations = %#v, want none", client.Ops())
	}

	path := statePath(t, values)
	if err := Save(path, State{PrevWorkspace: pointer("w1"), CurWorkspace: pointer("w2")}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	err = ToggleTab(context.Background(), client.Mutations(herdrmock.Target{}), env(values))
	if !errors.As(err, &refusal) {
		t.Fatalf("ToggleTab error = %v, want a Refusal", err)
	}
	if refusal.Reason != NoPreviousTab {
		t.Fatalf("refusal = %q, want %q", refusal.Reason, NoPreviousTab)
	}
	if len(client.Ops()) != 0 {
		t.Fatalf("mutations = %#v, want none", client.Ops())
	}
	state := readState(t, path)
	if state.PrevWorkspace == nil || *state.PrevWorkspace != "w1" {
		t.Fatalf("state = %#v, a refusal must not change it", state)
	}
}

func TestToggleKeepsTheStateWhenFocusFails(t *testing.T) {
	values := map[string]string{"HERDR_PLUGIN_STATE_DIR": t.TempDir(), "HERDR_SOCKET_PATH": "/tmp/one.sock"}
	path := statePath(t, values)
	seeded := State{PrevWorkspace: pointer("w1"), CurWorkspace: pointer("w2")}
	if err := Save(path, seeded); err != nil {
		t.Fatalf("Save: %v", err)
	}
	client := &herdrmock.Fake{FocusWorkspaceErr: errors.New("herdr workspace focus w1: exit status 1")}

	if err := ToggleWorkspace(context.Background(), client.Mutations(herdrmock.Target{}), env(values)); err == nil {
		t.Fatal("ToggleWorkspace = nil error, want the focus failure")
	} else if !strings.Contains(err.Error(), "exit status 1") {
		t.Fatalf("error = %v, want the focus failure", err)
	}
	state := readState(t, path)
	if state.PrevWorkspace == nil || *state.PrevWorkspace != "w1" || state.CurWorkspace == nil || *state.CurWorkspace != "w2" {
		t.Fatalf("state = %#v, a failed focus must leave it unchanged", state)
	}
}
