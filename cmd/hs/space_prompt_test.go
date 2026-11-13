package main

import (
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/WilberC/herdr-suite/internal/layout"
	"github.com/WilberC/herdr-suite/internal/session"
	tea "github.com/charmbracelet/bubbletea"
)

func pickerFixture() []layout.Entry {
	return []layout.Entry{
		{Name: "work", Scope: layout.ScopeGlobal, Path: "/tmp/layouts/work.json", Workspace: "project", Tabs: 2, Panes: 4},
		{Name: "docs", Scope: layout.ScopePath, Path: "/tmp/docs.json", Workspace: "notes", Tabs: 1, Panes: 1},
	}
}

func restoreSteps() []layout.Step {
	return []layout.Step{
		{Index: 1, Total: 3, Action: layout.Action{Op: layout.OpSplit, From: "p-1", Pane: "p-3", Direction: "right", Ratio: 0.6, Cwd: "/tmp"}},
		{Index: 2, Total: 3, Action: layout.Action{Op: layout.OpRenamePane, Pane: "p-1", Label: "editor"}},
		{Index: 3, Total: 3, Action: layout.Action{Op: layout.OpFocusAgent, AgentName: "pi"}},
	}
}

// newTestPicker builds the picker a terminal would run, with one difference: its
// spinner ticks a thousand times a second, so draining it in a test is instant.
func newTestPicker(restore layoutRestore) *layoutPicker {
	model := newLayoutPicker(pickerFixture(), restore)
	model.tickDelay = time.Microsecond
	return model
}

// drainPicker runs the picker's own command chain until the restore leaves the
// restoring state. The done state's brief timer is deliberately not waited on.
func drainPicker(t *testing.T, model *layoutPicker, cmds ...tea.Cmd) {
	t.Helper()
	for len(cmds) > 0 && model.phase == pickerRestoring {
		cmd := cmds[0]
		cmds = cmds[1:]
		if cmd == nil {
			continue
		}
		msg := cmd()
		if msg == nil {
			continue
		}
		if batch, ok := msg.(tea.BatchMsg); ok {
			cmds = append(cmds, batch...)
			continue
		}
		_, next := model.Update(msg)
		if next != nil {
			cmds = append(cmds, next)
		}
	}
}

func keyRunes(value string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(value)}
}

// TestLayoutPickerRunsTheConfirmedRestoreInProgram pins the interaction change:
// enter no longer exits the picker, it enters a restoring state that reports each
// replay step, then a one-line done state.
func TestLayoutPickerRunsTheConfirmedRestoreInProgram(t *testing.T) {
	steps := restoreSteps()
	var offered []layout.Entry
	restore := func(entry layout.Entry, report func(layout.Step)) (string, error) {
		offered = append(offered, entry)
		for _, step := range steps {
			report(step)
		}
		return "loaded layout 'work' into workspace 'project' · 2 tabs · 4 panes · 3 steps\n", nil
	}
	model := newTestPicker(restore)

	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if model.phase != pickerRestoring {
		t.Fatalf("phase after enter = %d, want the restoring state", model.phase)
	}
	if cmd == nil {
		t.Fatalf("enter issued no command, so nothing would restore")
	}
	view := model.View()
	if !strings.Contains(view, "restoring 'work'") || !strings.Contains(view, "starting") {
		t.Fatalf("the restoring state does not name the layout:\n%s", view)
	}

	drainPicker(t, model, cmd)

	if want := []layout.Entry{pickerFixture()[0]}; len(offered) != 1 || offered[0].Path != want[0].Path {
		t.Fatalf("restore got %#v, want %#v", offered, want)
	}
	if model.phase != pickerDone {
		t.Fatalf("phase = %d, want the done state", model.phase)
	}
	if model.restoreErr != nil {
		t.Fatalf("restoreErr = %v, want nil", model.restoreErr)
	}
	var recorded []string
	for _, step := range model.steps {
		recorded = append(recorded, step.Describe())
	}
	want := []string{
		"split right 0.60 in p-1 · new pane p-3 · cwd /tmp",
		"rename p-1 'editor'",
		"focus agent pi",
	}
	if strings.Join(recorded, "\n") != strings.Join(want, "\n") {
		t.Fatalf("recorded steps =\n%s\nwant:\n%s", strings.Join(recorded, "\n"), strings.Join(want, "\n"))
	}
	view = model.View()
	if !strings.Contains(view, "restored 'work'") || !strings.Contains(view, "3 steps") {
		t.Fatalf("the done state does not report the replay:\n%s", view)
	}
	if strings.Count(strings.TrimSpace(view), "\n") != 0 {
		t.Fatalf("the done state is not one line:\n%s", view)
	}

	model.Update(pickerQuitMsg{})
	if !model.finished {
		t.Fatalf("the done state never left the picker")
	}
}

// TestLayoutPickerReportsTheFailingStepAndWaitsForAKey pins the error state: the
// step that stopped the replay is named, the picker stays up until a key, and the
// failure reaches the command so the exit status is the non-interactive one.
func TestLayoutPickerReportsTheFailingStepAndWaitsForAKey(t *testing.T) {
	failure := layout.StepError{
		Index:  2,
		Total:  3,
		Action: layout.Action{Op: layout.OpRenamePane, Pane: "p-1", Label: "editor"},
		Err:    errors.New("pane limit reached"),
	}
	restore := func(entry layout.Entry, report func(layout.Step)) (string, error) {
		report(restoreSteps()[0])
		return "", failure
	}
	model := newTestPicker(restore)

	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	drainPicker(t, model, cmd)

	if model.phase != pickerFailed {
		t.Fatalf("phase = %d, want the failed state", model.phase)
	}
	if !errors.Is(model.restoreErr, failure) {
		t.Fatalf("restoreErr = %v, want the failing step", model.restoreErr)
	}
	view := model.View()
	for _, want := range []string{"step 2/3", "rename p-1 'editor'", "pane limit reached", "press any key"} {
		if !strings.Contains(view, want) {
			t.Fatalf("the failed state does not contain %q:\n%s", want, view)
		}
	}
	if model.finished {
		t.Fatalf("the failed state left the picker before a key was pressed")
	}

	model.Update(keyRunes("x"))
	if !model.finished {
		t.Fatalf("the failed state ignored a key")
	}
}

// TestLayoutPickerWithoutARestoreOnlyChooses keeps the dry-run path: a picker
// with nothing to replay returns the chosen layout and leaves at once.
func TestLayoutPickerWithoutARestoreOnlyChooses(t *testing.T) {
	model := newTestPicker(nil)

	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if model.chosen != pickerFixture()[0].Path {
		t.Fatalf("chosen = %q, want the highlighted layout's file", model.chosen)
	}
	if model.phase != pickerChoosing {
		t.Fatalf("phase = %d, want the picker to stay in the choosing state", model.phase)
	}
	if got := model.View(); !strings.Contains(got, "load which layout?") {
		t.Fatalf("the picker did not leave the choosing state:\n%s", got)
	}
	if cmd == nil {
		t.Fatalf("enter issued no quit command")
	}
}

// TestLayoutPickerSpinsWhileRestoring pins the spinner: the frame advances on its
// own tick, so a slow step reads as work in progress instead of a hang.
func TestLayoutPickerSpinsWhileRestoring(t *testing.T) {
	restore := func(entry layout.Entry, report func(layout.Step)) (string, error) { return "ok\n", nil }
	model := newTestPicker(restore)

	model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if got := model.View(); !strings.Contains(got, pickerFrames[0]) {
		t.Fatalf("the restoring state does not show a spinner frame:\n%s", got)
	}

	_, cmd := model.Update(pickerTickMsg{})
	if model.frame != 1 {
		t.Fatalf("frame = %d, want the next one", model.frame)
	}
	if cmd == nil {
		t.Fatalf("the spinner stopped ticking")
	}
	if got := model.View(); !strings.Contains(got, pickerFrames[1]) {
		t.Fatalf("the view kept the previous frame:\n%s", got)
	}
}

// TestLayoutPickerReportsTheStepThatIsRunning pins the per-step update: every
// report replaces the step line, which is what makes a slow replay readable.
func TestLayoutPickerReportsTheStepThatIsRunning(t *testing.T) {
	model := newTestPicker(func(entry layout.Entry, report func(layout.Step)) (string, error) { return "ok\n", nil })

	model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	for _, step := range restoreSteps() {
		model.Update(restoreStepMsg{step: step})
		if model.phase != pickerRestoring {
			t.Fatalf("a step report left the restoring state")
		}
	}

	view := model.View()
	if !strings.Contains(view, "step 3/3") || !strings.Contains(view, "focus agent pi") {
		t.Fatalf("the view does not show the step that is running:\n%s", view)
	}
}

// TestLayoutPickerCancelsBeforeConfirming keeps esc where it always was.
func TestLayoutPickerCancelsBeforeConfirming(t *testing.T) {
	model := newTestPicker(func(entry layout.Entry, report func(layout.Step)) (string, error) { return "ok\n", nil })

	model.Update(tea.KeyMsg{Type: tea.KeyEsc})

	if !model.cancelled {
		t.Fatalf("esc did not cancel the picker")
	}
}

// TestLayoutPickerMovesTheHighlight keeps the list navigation the picker had.
func TestLayoutPickerMovesTheHighlight(t *testing.T) {
	model := newTestPicker(nil)

	model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if model.chosen != pickerFixture()[1].Path {
		t.Fatalf("chosen = %q, want the second layout", model.chosen)
	}
}

// TestPickLayoutSurfacesACancelledPicker pins what the command reads from the
// picker: a cancelled pick is the same exit status it was.
func TestPickLayoutSurfacesACancelledPicker(t *testing.T) {
	model := newLayoutPicker(pickerFixture(), nil)
	model.cancelled = true

	picked, err := pickResult(model)

	if !errors.Is(err, session.ErrCancelled) {
		t.Fatalf("err = %v, want the cancelled pick", err)
	}
	if picked.Restored {
		t.Fatalf("a cancelled pick reported a restore")
	}
}

// TestPickLayoutRunsTheRestoreInsideTheProgram drives the real picker program
// rather than the model: enter must run the restore, report its steps, and leave
// the same result the command reads.
func TestPickLayoutRunsTheRestoreInsideTheProgram(t *testing.T) {
	var ran bool
	restore := func(entry layout.Entry, report func(layout.Step)) (string, error) {
		ran = true
		for _, step := range restoreSteps() {
			report(step)
		}
		return "loaded layout 'work' into workspace 'project' · 3 steps\n", nil
	}

	picked, err := pickLayout(strings.NewReader("\r"), io.Discard, pickerFixture(), restore)

	if err != nil {
		t.Fatalf("pickLayout error = %v, want nil", err)
	}
	if !ran {
		t.Fatalf("the picker never ran the restore")
	}
	if !picked.Restored || picked.Entry.Name != "work" {
		t.Fatalf("picked = %#v, want the confirmed layout restored", picked)
	}
	if want := "loaded layout 'work' into workspace 'project' · 3 steps"; picked.Summary != want {
		t.Fatalf("summary = %q, want %q", picked.Summary, want)
	}
}
