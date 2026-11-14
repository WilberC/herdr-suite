package main

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/WilberC/herdr-suite/internal/herdrmock"
	"github.com/WilberC/herdr-suite/internal/layout"
	"github.com/WilberC/herdr-suite/internal/session"
	"github.com/WilberC/tui-kit/frame"
	"github.com/WilberC/tui-kit/list"
	"github.com/WilberC/tui-kit/text"
	"github.com/WilberC/tui-kit/theme"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
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
	if !strings.Contains(view, "hs ❯ space load") {
		t.Fatalf("the done state left the shared surface:\n%s", view)
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

// TestLayoutPickerViewFitsTheWindow pins the responsive contract for the picker:
// the same frame as the switcher, every line inside the window, no wrapping.
func TestLayoutPickerViewFitsTheWindow(t *testing.T) {
	for _, size := range []struct {
		name string
		w, h int
	}{
		{"180x50", 180, 50},
		{"140x40", 140, 40},
		{"80x24", 80, 24},
	} {
		entries := append(pickerFixture(), layout.Entry{
			Name:      "a-layout-with-a-name-that-needs-clipping-to-fit",
			Scope:     layout.ScopePath,
			Path:      "/home/ana/src/very/deeply/nested/project/.herdr/layouts/a-layout-with-a-name-that-needs-clipping-to-fit.json",
			Workspace: "a-workspace-with-a-rather-long-name", Tabs: 12, Panes: 34,
			Modified: time.Now().Add(-3 * time.Hour),
		})
		model := newLayoutPicker(entries, nil)
		model.width, model.height = size.w, size.h
		lines := strings.Split(model.View(), "\n")

		if len(lines) > size.h {
			t.Fatalf("%s: view has %d lines, want at most %d", size.name, len(lines), size.h)
		}
		for i, line := range lines {
			if got := text.Width(line); got > size.w {
				t.Fatalf("%s: line %d is %d cells wide, want at most %d: %q", size.name, i, got, size.w, line)
			}
		}
		frame := strings.TrimSpace(strings.Join(lines, "\n"))
		if !strings.Contains(frame, "╭") || !strings.Contains(frame, "╯") {
			t.Fatalf("%s: the frame is not closed:\n%s", size.name, frame)
		}
	}
}

// TestLayoutPickerMarksExactlyOneSelectedRow pins the selection affordance and
// the scope grouping: one marked row, and a header per scope the store lists.
func TestLayoutPickerMarksExactlyOneSelectedRow(t *testing.T) {
	model := newLayoutPicker(pickerFixture(), nil)
	model.width, model.height = 140, 40

	for range pickerFixture() {
		entry := pickerFixture()[model.cursor]
		var marked []string
		for _, line := range strings.Split(model.View(), "\n") {
			if strings.Contains(line, text.SelectMark) {
				marked = append(marked, line)
			}
		}
		if len(marked) != 1 {
			t.Fatalf("view marked %d rows, want exactly one:\n%s", len(marked), strings.Join(marked, "\n"))
		}
		if !strings.Contains(marked[0], entry.Name) {
			t.Fatalf("the marked row is %q, want the highlighted layout %q", marked[0], entry.Name)
		}
		model.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
}

// TestLayoutPickerFooterFollowsTheSelection pins the footer: the highlighted
// layout's facts, and the named bindings under them.
func TestLayoutPickerFooterFollowsTheSelection(t *testing.T) {
	model := newLayoutPicker(pickerFixture(), nil)
	model.width, model.height = 140, 40

	footer := lastFooterLine(model.View())
	for _, want := range []string{"work", "global", "workspace", "project", "2 tabs", "4 panes"} {
		if !strings.Contains(footer, want) {
			t.Fatalf("the footer does not contain %q:\n%s", want, footer)
		}
	}

	model.Update(tea.KeyMsg{Type: tea.KeyDown})
	footer = lastFooterLine(model.View())
	if !strings.Contains(footer, "docs") {
		t.Fatalf("the footer did not follow the highlight:\n%s", footer)
	}
	if strings.Contains(footer, "2 tabs") {
		t.Fatalf("the footer kept the previous layout's facts:\n%s", footer)
	}
}

// TestLayoutPickerGroupsByScope pins the hierarchy the picker renders: the
// project's layouts under a project header, the global ones under theirs.
func TestLayoutPickerGroupsByScope(t *testing.T) {
	model := newLayoutPicker([]layout.Entry{
		{Name: "api", Scope: layout.ScopeProject, Path: "/src/api/.herdr/api.json", Workspace: "api", Tabs: 1, Panes: 2},
		{Name: "work", Scope: layout.ScopeGlobal, Path: "/tmp/work.json", Workspace: "project", Tabs: 2, Panes: 4},
	}, nil)
	model.width, model.height = 100, 30

	view := model.View()
	projectAt := strings.Index(view, "● PROJECT")
	globalAt := strings.Index(view, "● GLOBAL")
	apiAt := strings.Index(view, "api")
	if projectAt < 0 || globalAt < 0 || apiAt < 0 {
		t.Fatalf("the picker did not group the list by scope:\n%s", view)
	}
	if !(projectAt < apiAt && apiAt < globalAt) {
		t.Fatalf("the groups are not in store order (project, then global):\n%s", view)
	}
}

// TestLayoutPickerEmptyStateIsCalm pins the empty list: a centered message
// inside the frame and the hint that says how to fill it, never a bare screen.
func TestLayoutPickerEmptyStateIsCalm(t *testing.T) {
	model := newLayoutPicker(nil, nil)
	model.width, model.height = 100, 30

	view := model.View()
	for _, want := range []string{"no layouts found", "hs space save", "╰"} {
		if !strings.Contains(view, want) {
			t.Fatalf("the empty picker does not contain %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "● PROJECT") || strings.Contains(view, "● GLOBAL") {
		t.Fatalf("the empty picker rendered a group header:\n%s", view)
	}
}

// lastFooterLine is the detail line of the footer: the interior line above the
// binding bar.
func lastFooterLine(view string) string {
	lines := strings.Split(view, "\n")
	if len(lines) < 4 {
		return view
	}
	return lines[len(lines)-3]
}

// TestLayoutPickerGroupRhythm pins the vertical rhythm: the rule that closes the
// header is followed by the body's one blank padding row and then the first
// group, and every following group gets exactly one blank row -- never two,
// which is what a stray leading row produced.
func TestLayoutPickerGroupRhythm(t *testing.T) {
	model := newLayoutPicker([]layout.Entry{
		{Name: "api", Scope: layout.ScopeProject, Path: "/src/api/.herdr/api.json", Workspace: "api", Tabs: 1, Panes: 2},
		{Name: "work", Scope: layout.ScopeGlobal, Path: "/tmp/work.json", Workspace: "project", Tabs: 2, Panes: 4},
		{Name: "docs", Scope: layout.ScopePath, Path: "/tmp/docs.json", Workspace: "notes", Tabs: 1, Panes: 1},
	}, nil)
	model.width, model.height = 100, 30

	lines := strings.Split(model.View(), "\n")
	rule := -1
	for i, line := range lines {
		if strings.Contains(line, "├") {
			rule = i
			break
		}
	}
	if rule < 0 {
		t.Fatalf("the picker rendered no rule:\n%s", model.View())
	}
	if !pickerBlankRow(lines[rule+1]) {
		t.Fatalf("the body does not pad the first group with one blank row:\n%s", model.View())
	}
	if !strings.Contains(lines[rule+2], "● PROJECT") {
		t.Fatalf("the first group does not follow the body's padding row:\n%s", model.View())
	}
	for _, want := range []struct {
		group string
		index int
	}{{"● GLOBAL", 0}, {"● PATH", 0}} {
		at := -1
		for i, line := range lines {
			if strings.Contains(line, want.group) {
				at = i
				break
			}
		}
		if at < 0 {
			t.Fatalf("the picker rendered no %q group:\n%s", want.group, model.View())
		}
		if blank := interiorBlank(lines[at-1]); !blank {
			t.Fatalf("%q is not preceded by one blank row:\n%s", want.group, model.View())
		}
		if interiorBlank(lines[at-2]) {
			t.Fatalf("%q is preceded by two blank rows:\n%s", want.group, model.View())
		}
	}
}

// interiorBlank reports whether a view line is an empty interior row: the frame
// borders are there, nothing between them.
func interiorBlank(line string) bool {
	if !strings.Contains(line, "│") {
		return false
	}
	return strings.TrimSpace(strings.Map(func(r rune) rune {
		switch r {
		case ' ', '│', '╭', '╮', '╰', '╯', '├', '─', '┤':
			return -1
		}
		return r
	}, line)) == ""
}

// selectWithTrueColor renders for one assertion with the truecolor profile
// forced, so a test can read the role a cell is painted with.
func selectWithTrueColor(t *testing.T) {
	t.Helper()
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(previous) })
}

// selectRoleEscape is the escape a style renders.
func selectRoleEscape(style lipgloss.Style) string {
	rendered := style.Render("x")
	if i := strings.Index(rendered, "x"); i > 0 {
		return rendered[:i]
	}
	return rendered
}

// tintedRow is the one line of a view that carries the selection tint.
func tintedRow(t *testing.T, view string) string {
	t.Helper()
	var tinted []string
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, selectRoleEscape(theme.SelectionBG)) {
			tinted = append(tinted, line)
		}
	}
	if len(tinted) != 1 {
		t.Fatalf("view tinted %d rows, want exactly one:\n%s", len(tinted), strings.Join(tinted, "\n"))
	}
	return tinted[0]
}

// TestLayoutPickerSelectionKeepsItsFacts pins the selection surface in the
// picker: the tinted row keeps its name, workspace, counts and age, and those
// facts use the lifted muted role instead of the panel one.
func TestLayoutPickerSelectionKeepsItsFacts(t *testing.T) {
	selectWithTrueColor(t)
	entries := pickerFixture()
	entries[0].Modified = time.Now().Add(-2 * time.Hour)
	model := newLayoutPicker(entries, nil)
	model.width, model.height = 140, 40

	row := tintedRow(t, model.View())
	for _, want := range []string{"work", "project", "2 tabs · 4 panes", "2h ago"} {
		if !strings.Contains(row, want) {
			t.Fatalf("the selected layout lost %q on the tint: %q", want, row)
		}
	}
	if !strings.Contains(row, selectRoleEscape(theme.MutedOnSelection)) {
		t.Fatalf("the selected row's facts are not lifted onto the tint: %q", row)
	}
	if !strings.Contains(row, selectRoleEscape(theme.TextOnSelection)) {
		t.Fatalf("the selected layout's name is not painted with the selection text role: %q", row)
	}
	if !strings.Contains(row, text.SelectMark) || !strings.ContainsAny(row, "├└") {
		t.Fatalf("the selected row lost its edge or its guide: %q", row)
	}
	if strings.Contains(row, selectRoleEscape(theme.Muted)) {
		t.Fatalf("the selected row's facts kept the panel muted tone: %q", row)
	}
}

// TestWorkspacePickerSelectionKeepsTheAgentState pins that a selected row keeps
// the colours that carry meaning: the agent state stays green on the tint.
func TestWorkspacePickerSelectionKeepsTheAgentState(t *testing.T) {
	selectWithTrueColor(t)
	model := &workspacePicker{workspaces: captureWorkspaceList()}
	model.width, model.height = 140, 40

	row := tintedRow(t, model.View())
	if !strings.Contains(row, "project") || !strings.Contains(row, "working") {
		t.Fatalf("the selected workspace lost its facts: %q", row)
	}
	if !strings.Contains(row, selectRoleEscape(list.RowRoles(true).Green)) {
		t.Fatalf("the agent state lost its colour on the tint: %q", row)
	}
}

// pickerFrameBlock is a prompt's own frame lines: the top border through the
// bottom border, with the blank rows that centre it in a taller pane dropped.
func pickerFrameBlock(t *testing.T, view string) []string {
	t.Helper()
	lines := strings.Split(view, "\n")
	start, end := -1, -1
	for i, line := range lines {
		plain := text.Plain(line)
		if start < 0 && strings.Contains(plain, "╭") {
			start = i
		}
		if strings.Contains(plain, "╰") {
			end = i
		}
	}
	if start < 0 || end <= start {
		t.Fatalf("the view holds no closed frame:\n%s", view)
	}
	return lines[start : end+1]
}

// pickerBlankRow reports whether a frame line is an interior row with no content.
func pickerBlankRow(line string) bool {
	plain := text.Plain(line)
	if !strings.Contains(plain, "│") {
		return false
	}
	return strings.TrimSpace(strings.Map(func(r rune) rune {
		switch r {
		case ' ', '│', '╭', '╮', '╰', '╯', '├', '─', '┤', '┬', '┴':
			return -1
		}
		return r
	}, plain)) == ""
}

// TestLayoutPickerFrameIsTheClampedPaneHeight pins the size rule for the space
// picker: the frame stops at the compact height in a tall pane, the body is
// padded at both ends, and the footer sits on the last two inner rows.
func TestLayoutPickerFrameIsTheClampedPaneHeight(t *testing.T) {
	model := newLayoutPicker(pickerFixture(), nil)
	model.width, model.height = 180, 50
	block := pickerFrameBlock(t, model.View())

	if got, want := len(block), compactSize.MaxHeight; got != want {
		t.Fatalf("frame height in a 50-row pane = %d, want the compact %d:\n%s", got, want, strings.Join(block, "\n"))
	}

	bodyFirst := 3 // the top border, the header and its rule
	// The header and its rule sit above the body; the closing rule and the two
	// footer rows below it.
	pinned := frame.Surface{Size: compactSize, Top: make([]string, 2), Bottom: make([]string, 3)}
	room := pinned.BodyRows(50) + 2
	if !pickerBlankRow(block[bodyFirst]) {
		t.Fatalf("the body does not open with a blank row:\n%s", strings.Join(block, "\n"))
	}
	if !pickerBlankRow(block[bodyFirst+room-1]) {
		t.Fatalf("the body does not close with a blank row:\n%s", strings.Join(block, "\n"))
	}
	if !strings.Contains(text.Plain(block[bodyFirst+room]), "├") {
		t.Fatalf("the closing rule does not follow the body's padding row:\n%s", strings.Join(block, "\n"))
	}
	if hintBar := text.Plain(block[len(block)-2]); !strings.Contains(hintBar, " enter ") || !strings.Contains(hintBar, " load") {
		t.Fatalf("the last inner row is not the hint bar: %q", hintBar)
	}
	if last := text.Plain(block[len(block)-1]); !strings.Contains(last, "╰") {
		t.Fatalf("the frame does not close on its bottom border: %q", last)
	}
}

// TestWorkspacePickerFrameIsTheClampedPaneHeight pins the same rule for the
// workspace prompt.
func TestWorkspacePickerFrameIsTheClampedPaneHeight(t *testing.T) {
	prompt := &workspacePicker{workspaces: captureWorkspaceList()}
	prompt.width, prompt.height = 180, 50
	block := pickerFrameBlock(t, prompt.View())

	if got, want := len(block), compactSize.MaxHeight; got != want {
		t.Fatalf("frame height in a 50-row pane = %d, want the compact %d:\n%s", got, want, strings.Join(block, "\n"))
	}

	bodyFirst := 3 // the top border, the header and its rule
	pinned := frame.Surface{Size: compactSize, Top: make([]string, 2), Bottom: make([]string, 3)}
	room := pinned.BodyRows(50) + 2
	if !pickerBlankRow(block[bodyFirst]) || !pickerBlankRow(block[bodyFirst+room-1]) {
		t.Fatalf("the body is not padded at both ends:\n%s", strings.Join(block, "\n"))
	}
	if !strings.Contains(text.Plain(block[bodyFirst+room]), "├") {
		t.Fatalf("the closing rule does not follow the body's padding row:\n%s", strings.Join(block, "\n"))
	}
	if hintBar := text.Plain(block[len(block)-2]); !strings.Contains(hintBar, " enter ") {
		t.Fatalf("the last inner row is not the hint bar: %q", hintBar)
	}
}

// TestLayoutPickerTabPagesTheBindingBar pins the paging contract in the space
// picker: a narrow window names the position and tab moves through it.
func TestLayoutPickerTabPagesTheBindingBar(t *testing.T) {
	model := newLayoutPicker(pickerFixture(), nil)
	model.width, model.height = 40, 24

	first := model.View()
	if !strings.Contains(first, "more 1/3") || !strings.Contains(first, " ↑↓  move") {
		t.Fatalf("the narrow picker does not page its bindings:\n%s", first)
	}
	if strings.Contains(first, " enter  load") {
		t.Fatalf("the first page already holds the second hint:\n%s", first)
	}

	model.Update(tea.KeyMsg{Type: tea.KeyTab})
	second := model.View()
	if !strings.Contains(second, "more 2/3") || !strings.Contains(second, " enter  load") {
		t.Fatalf("tab did not show the second page:\n%s", second)
	}
	if model.chosen != "" || model.cancelled {
		t.Fatalf("tab answered the picker: chosen = %q, cancelled = %v", model.chosen, model.cancelled)
	}

	model.Update(tea.KeyMsg{Type: tea.KeyTab})
	third := model.View()
	if !strings.Contains(third, "more 3/3") || !strings.Contains(third, " esc  cancel") {
		t.Fatalf("tab did not show the third page:\n%s", third)
	}

	model.Update(tea.KeyMsg{Type: tea.KeyTab})
	wrapped := model.View()
	if !strings.Contains(wrapped, "more 1/3") || !strings.Contains(wrapped, " ↑↓  move") {
		t.Fatalf("tab did not wrap back to the first page:\n%s", wrapped)
	}
}

// TestWorkspacePickerTabPagesTheBindingBar pins the same contract in the
// workspace prompt, which has no filter for the key to reach.
func TestWorkspacePickerTabPagesTheBindingBar(t *testing.T) {
	prompt := &workspacePicker{workspaces: captureWorkspaceList()}
	prompt.width, prompt.height = 40, 24

	first := prompt.View()
	if !strings.Contains(first, "more 1/3") {
		t.Fatalf("the narrow prompt does not page its bindings:\n%s", first)
	}

	prompt.Update(tea.KeyMsg{Type: tea.KeyTab})
	if second := prompt.View(); !strings.Contains(second, "more 2/3") || !strings.Contains(second, " enter  load") {
		t.Fatalf("tab did not show the second page:\n%s", second)
	}
	if prompt.chosen != "" || prompt.cancelled {
		t.Fatalf("tab answered the prompt: chosen = %q, cancelled = %v", prompt.chosen, prompt.cancelled)
	}

	prompt.Update(tea.KeyMsg{Type: tea.KeyTab})
	prompt.Update(tea.KeyMsg{Type: tea.KeyTab})
	if wrapped := prompt.View(); !strings.Contains(wrapped, "more 1/3") {
		t.Fatalf("tab did not wrap back to the first page:\n%s", wrapped)
	}
}

// TestLayoutPickerKeepsTheHighlightInView pins what a list longer than the body
// does: it scrolls inside the frame instead of overflowing it, and the
// highlighted layout stays on screen.
func TestLayoutPickerKeepsTheHighlightInView(t *testing.T) {
	var entries []layout.Entry
	for i := 0; i < 30; i++ {
		entries = append(entries, layout.Entry{
			Name: "layout-" + fmt.Sprint(i), Scope: layout.ScopeGlobal,
			Path: "/tmp/layout-" + fmt.Sprint(i) + ".json", Workspace: "project", Tabs: 1, Panes: 1,
		})
	}
	model := newLayoutPicker(entries, nil)
	model.width, model.height = 100, 20
	model.cursor = len(entries) - 1

	lines := strings.Split(model.View(), "\n")
	if len(lines) > 20 {
		t.Fatalf("view has %d lines, want at most 20:\n%s", len(lines), model.View())
	}
	highlighted := entries[model.cursor].Name
	if !strings.Contains(model.View(), text.SelectMark) || !strings.Contains(model.View(), highlighted) {
		t.Fatalf("the highlighted layout %q is not on screen:\n%s", highlighted, model.View())
	}
	if !strings.Contains(lines[len(lines)-1], "╰") {
		t.Fatalf("the frame is not closed:\n%s", model.View())
	}
}

// TestWorkspacePickerKeepsTheHighlightInView pins the same scrolling in the
// workspace prompt.
func TestWorkspacePickerKeepsTheHighlightInView(t *testing.T) {
	var workspaces []herdrmock.Workspace
	for i := 0; i < 30; i++ {
		workspaces = append(workspaces, herdrmock.Workspace{ID: "w" + fmt.Sprint(i), Label: "workspace-" + fmt.Sprint(i), TabCount: 1, PaneCount: 1})
	}
	prompt := &workspacePicker{workspaces: workspaces}
	prompt.width, prompt.height = 100, 20
	prompt.cursor = len(workspaces) - 1

	lines := strings.Split(prompt.View(), "\n")
	if len(lines) > 20 {
		t.Fatalf("view has %d lines, want at most 20:\n%s", len(lines), prompt.View())
	}
	highlighted := workspaces[prompt.cursor].Label
	if !strings.Contains(prompt.View(), text.SelectMark) || !strings.Contains(prompt.View(), highlighted) {
		t.Fatalf("the highlighted workspace %q is not on screen:\n%s", highlighted, prompt.View())
	}
	if !strings.Contains(lines[len(lines)-1], "╰") {
		t.Fatalf("the frame is not closed:\n%s", prompt.View())
	}
}
