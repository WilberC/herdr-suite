package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/WilberC/herdr-suite/internal/herdrmock"
	"github.com/WilberC/herdr-suite/internal/layout"
	"github.com/WilberC/herdr-suite/internal/session"
	"github.com/WilberC/tui-kit/chip"
	"github.com/WilberC/tui-kit/text"
	"github.com/WilberC/tui-kit/theme"
	"github.com/WilberC/tui-kit/zone"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// These tests are the mouse contract of the space picker and the workspace
// prompt: a click selects, a double click runs the row's default action, hover is
// a preview, the wheel moves the selection inside the body and the paging chip
// answers like chip.PageKey. Every coordinate comes from the zone map the render
// recorded, so a test never measures a frame by hand.

// pickerRowCentre is the cell in the middle of a recorded body row.
func pickerRowCentre(t *testing.T, zones *zone.Map, index int) (int, int) {
	t.Helper()
	rect, ok := zones.Find(session.RowZone(index))
	if !ok {
		t.Fatalf("the render recorded no zone for row %d", index)
	}
	return rect.X + rect.W/2, rect.Y
}

func pickerMousePress(x, y int) tea.MouseMsg {
	return tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: x, Y: y}
}

func pickerMouseMove(x, y int) tea.MouseMsg {
	return tea.MouseMsg{Action: tea.MouseActionMotion, X: x, Y: y}
}

func pickerMouseWheel(x, y int, up bool) tea.MouseMsg {
	button := tea.MouseButtonWheelDown
	if up {
		button = tea.MouseButtonWheelUp
	}
	return tea.MouseMsg{Action: tea.MouseActionPress, Button: button, X: x, Y: y}
}

// TestLayoutPickerClickSelectsAndDoubleClickLoads pins the default action of the
// space picker: one click selects, a second one inside the window loads, and the
// narrow and wide panes behave the same because the zone map carries the frame.
func TestLayoutPickerClickSelectsAndDoubleClickLoads(t *testing.T) {
	for _, size := range []struct {
		name string
		w, h int
	}{
		{"180x50", 180, 50},
		{"80x24", 80, 24},
	} {
		t.Run(size.name, func(t *testing.T) {
			model := newTestPicker(nil)
			model.width, model.height = size.w, size.h
			at := time.Unix(1_700_000_000, 0)
			model.now = func() time.Time { return at }
			model.View()

			x, y := pickerRowCentre(t, model.zones, 1)
			model.Update(pickerMousePress(x, y))
			if got := model.cursor; got != 1 {
				t.Fatalf("cursor after a click on row 1 = %d, want 1", got)
			}
			if got := model.chosen; got != "" {
				t.Fatalf("one click loaded the layout: chosen = %q", got)
			}
			if model.finished || model.phase != pickerChoosing {
				t.Fatalf("one click left the choosing state: finished = %v, phase = %d", model.finished, model.phase)
			}

			at = at.Add(200 * time.Millisecond)
			_, cmd := model.Update(pickerMousePress(x, y))
			if got, want := model.chosen, pickerFixture()[1].Path; got != want {
				t.Fatalf("a double click loaded %q, want %q", got, want)
			}
			if !model.finished {
				t.Fatalf("a double click did not confirm the layout")
			}
			if cmd == nil {
				t.Fatalf("a double click issued no command, so the picker would not close")
			}
		})
	}
}

// TestLayoutPickerClickNeverRestoresOnASingleClick pins the reversible half of
// the contract: selecting a layout must not replay it.
func TestLayoutPickerClickNeverRestoresOnASingleClick(t *testing.T) {
	restored := 0
	model := newTestPicker(func(layout.Entry, func(layout.Step)) (string, error) {
		restored++
		return "", nil
	})
	model.width, model.height = 140, 40
	at := time.Unix(1_700_000_000, 0)
	model.now = func() time.Time { return at }
	model.View()
	x, y := pickerRowCentre(t, model.zones, 1)

	model.Update(pickerMousePress(x, y))
	if restored != 0 || model.phase != pickerChoosing {
		t.Fatalf("one click replayed the layout: restored = %d, phase = %d", restored, model.phase)
	}

	at = at.Add(200 * time.Millisecond)
	_, cmd := model.Update(pickerMousePress(x, y))
	if model.phase != pickerRestoring {
		t.Fatalf("a double click did not replay the layout: phase = %d", model.phase)
	}
	drainPicker(t, model, cmd)
	if restored != 1 {
		t.Fatalf("a double click replayed the layout %d times, want once", restored)
	}
}

// TestLayoutPickerHoverTintsTheRowUnderThePointer pins hover in the picker: the
// row under the pointer wears the faint tint, the selection keeps its own.
func TestLayoutPickerHoverTintsTheRowUnderThePointer(t *testing.T) {
	selectWithTrueColor(t)
	model := newLayoutPicker(pickerFixture(), nil)
	model.width, model.height = 140, 40
	model.View()

	x, y := pickerRowCentre(t, model.zones, 1)
	model.Update(pickerMouseMove(x, y))
	view := model.View()
	line := strings.Split(view, "\n")[y]
	if !strings.Contains(line, pickerFixture()[1].Name) {
		t.Fatalf("the hovered row is not the layout under the pointer: %q", line)
	}
	if !strings.Contains(line, selectRoleEscape(theme.HoverBG)) {
		t.Fatalf("the row under the pointer wears no hover tint: %q", line)
	}
	if strings.Contains(line, selectRoleEscape(theme.SelectionBG)) {
		t.Fatalf("the hovered row wears the selection's tint: %q", line)
	}
	if got := pickerTintedLines(view, theme.SelectionBG); got != 1 {
		t.Fatalf("the view carries the selection tint on %d rows, want one:\n%s", got, view)
	}

	// Hovering the selected row keeps the selection.
	x, y = pickerRowCentre(t, model.zones, 0)
	model.Update(pickerMouseMove(x, y))
	if view := model.View(); strings.Contains(view, selectRoleEscape(theme.HoverBG)) {
		t.Fatalf("hovering the selection tinted a second row:\n%s", view)
	}
}

// TestLayoutPickerWheelMovesTheSelectionInsideTheBody pins the wheel: it moves the
// highlight, stops at the ends, and never draws a row the body has no room for.
func TestLayoutPickerWheelMovesTheSelectionInsideTheBody(t *testing.T) {
	var entries []layout.Entry
	for i := 0; i < 30; i++ {
		entries = append(entries, layout.Entry{
			Name: "layout-" + fmt.Sprint(i), Scope: layout.ScopeGlobal,
			Path: "/tmp/layout-" + fmt.Sprint(i) + ".json", Workspace: "project", Tabs: 1, Panes: 1,
		})
	}
	model := newLayoutPicker(entries, nil)
	model.width, model.height = 100, 20
	model.View()

	for i := 0; i < 3; i++ {
		model.Update(pickerMouseWheel(20, 10, false))
	}
	if got := model.cursor; got != 3 {
		t.Fatalf("cursor after three wheel-downs = %d, want 3", got)
	}
	model.View()
	if _, ok := model.zones.Find(session.RowZone(3)); !ok {
		t.Fatalf("the wheel moved the selection off the body:\n%s", model.View())
	}
	rows := model.surface().BodyRows(20)
	if got := pickerDrawnRows(model.zones); got > rows {
		t.Fatalf("the body drew %d rows, want at most BodyRows = %d", got, rows)
	}

	for i := 0; i < 100; i++ {
		model.Update(pickerMouseWheel(20, 10, false))
	}
	if got := model.cursor; got != len(entries)-1 {
		t.Fatalf("cursor after the wheel ran out = %d, want %d", got, len(entries)-1)
	}
	model.View()
	if _, ok := model.zones.Find(session.RowZone(model.cursor)); !ok {
		t.Fatalf("the last layout is not on screen:\n%s", model.View())
	}

	for i := 0; i < 100; i++ {
		model.Update(pickerMouseWheel(20, 10, true))
	}
	if got := model.cursor; got != 0 {
		t.Fatalf("cursor after the wheel ran back = %d, want 0", got)
	}
}

// pickerTintedLines is how many view lines carry one escape.
func pickerTintedLines(view string, style lipgloss.Style) int {
	count := 0
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, selectRoleEscape(style)) {
			count++
		}
	}
	return count
}

// pickerDrawnRows is how many distinct body rows the render recorded a row zone on.
func pickerDrawnRows(zones *zone.Map) int {
	seen := map[int]bool{}
	for _, recorded := range zones.Zones() {
		if _, ok := session.RowZoneIndex(recorded.ID); ok {
			seen[recorded.Rect.Y] = true
		}
	}
	return len(seen)
}

// TestLayoutPickerPagingChipPagesTheBar pins the paging chip in the picker.
func TestLayoutPickerPagingChipPagesTheBar(t *testing.T) {
	model := newLayoutPicker(pickerFixture(), nil)
	model.width, model.height = 40, 24
	view := model.View()
	if !strings.Contains(view, "more 1/3") {
		t.Fatalf("the narrow picker does not page its bindings:\n%s", view)
	}

	rect, ok := model.zones.Find(chip.PageZone)
	if !ok {
		t.Fatalf("the render recorded no zone for the paging chip:\n%s", view)
	}
	model.Update(pickerMousePress(rect.X+1, rect.Y))
	clicked := model.View()

	keyed := newLayoutPicker(pickerFixture(), nil)
	keyed.width, keyed.height = 40, 24
	keyed.View()
	keyed.Update(tea.KeyMsg{Type: tea.KeyTab})

	if clicked != keyed.View() {
		t.Fatalf("a click on the paging chip differs from %q:\nclick: %s\nkey:   %s", chip.PageKey, clicked, keyed.View())
	}
}

// TestWorkspacePickerClickSelectsAndDoubleClickPicks pins the workspace prompt:
// one click selects, a second one inside the window picks, and neither cancels.
func TestWorkspacePickerClickSelectsAndDoubleClickPicks(t *testing.T) {
	for _, size := range []struct {
		name string
		w, h int
	}{
		{"180x50", 180, 50},
		{"80x24", 80, 24},
	} {
		t.Run(size.name, func(t *testing.T) {
			prompt := &workspacePicker{workspaces: captureWorkspaceList()}
			prompt.width, prompt.height = size.w, size.h
			at := time.Unix(1_700_000_000, 0)
			prompt.now = func() time.Time { return at }
			prompt.View()

			x, y := pickerRowCentre(t, prompt.zones, 2)
			prompt.Update(pickerMousePress(x, y))
			if got := prompt.cursor; got != 2 {
				t.Fatalf("cursor after a click on row 2 = %d, want 2", got)
			}
			if got := prompt.chosen; got != "" {
				t.Fatalf("one click picked workspace %q", got)
			}
			if prompt.cancelled {
				t.Fatalf("one click cancelled the prompt")
			}

			at = at.Add(300 * time.Millisecond)
			_, cmd := prompt.Update(pickerMousePress(x, y))
			if got, want := prompt.chosen, captureWorkspaceList()[2].ID; got != want {
				t.Fatalf("a double click picked %q, want %q", got, want)
			}
			if cmd == nil {
				t.Fatalf("a double click issued no command, so the prompt would not close")
			}
		})
	}
}

// TestWorkspacePickerHoverTintsTheRowUnderThePointer pins hover in the prompt.
func TestWorkspacePickerHoverTintsTheRowUnderThePointer(t *testing.T) {
	selectWithTrueColor(t)
	prompt := &workspacePicker{workspaces: captureWorkspaceList()}
	prompt.width, prompt.height = 140, 40
	prompt.View()

	x, y := pickerRowCentre(t, prompt.zones, 1)
	prompt.Update(pickerMouseMove(x, y))
	view := prompt.View()
	line := strings.Split(view, "\n")[y]
	if !strings.Contains(line, captureWorkspaceList()[1].Label) {
		t.Fatalf("the hovered row is not the workspace under the pointer: %q", line)
	}
	if !strings.Contains(line, selectRoleEscape(theme.HoverBG)) {
		t.Fatalf("the row under the pointer wears no hover tint: %q", line)
	}
	if strings.Contains(line, selectRoleEscape(theme.SelectionBG)) {
		t.Fatalf("the hovered row wears the selection's tint: %q", line)
	}
}

// TestWorkspacePickerWheelMovesTheSelection pins the wheel in the prompt.
func TestWorkspacePickerWheelMovesTheSelection(t *testing.T) {
	var workspaces []herdrmock.Workspace
	for i := 0; i < 30; i++ {
		workspaces = append(workspaces, herdrmock.Workspace{ID: "w" + fmt.Sprint(i), Label: "workspace-" + fmt.Sprint(i), TabCount: 1, PaneCount: 1})
	}
	prompt := &workspacePicker{workspaces: workspaces}
	prompt.width, prompt.height = 100, 20
	prompt.View()

	prompt.Update(pickerMouseWheel(20, 10, false))
	if got := prompt.cursor; got != 1 {
		t.Fatalf("cursor after a wheel-down = %d, want 1", got)
	}
	prompt.View()
	if _, ok := prompt.zones.Find(session.RowZone(1)); !ok {
		t.Fatalf("the wheel moved the selection off the body:\n%s", prompt.View())
	}
	rows := prompt.surface().BodyRows(20)
	if got := pickerDrawnRows(prompt.zones); got > rows {
		t.Fatalf("the body drew %d rows, want at most BodyRows = %d", got, rows)
	}

	prompt.Update(pickerMouseWheel(20, 10, true))
	if got := prompt.cursor; got != 0 {
		t.Fatalf("cursor after a wheel-up = %d, want 0", got)
	}
}

// TestWorkspacePickerPagingChipPagesTheBar pins the paging chip in the prompt.
func TestWorkspacePickerPagingChipPagesTheBar(t *testing.T) {
	prompt := &workspacePicker{workspaces: captureWorkspaceList()}
	prompt.width, prompt.height = 40, 24
	view := prompt.View()

	rect, ok := prompt.zones.Find(chip.PageZone)
	if !ok {
		t.Fatalf("the render recorded no zone for the paging chip:\n%s", view)
	}
	prompt.Update(pickerMousePress(rect.X+1, rect.Y))
	if got := prompt.View(); !strings.Contains(got, "more 2/3") {
		t.Fatalf("the click did not page the bar:\n%s", got)
	}
	if prompt.chosen != "" || prompt.cancelled {
		t.Fatalf("the paging click answered the prompt: chosen = %q, cancelled = %v", prompt.chosen, prompt.cancelled)
	}
}

// TestAClickOutsideEveryRowAnswersNothing pins the footer and the header: they are
// not targets, so a click there selects nothing and confirms nothing.
func TestAClickOutsideEveryRowAnswersNothing(t *testing.T) {
	model := newLayoutPicker(pickerFixture(), nil)
	model.width, model.height = 140, 40
	view := model.View()

	for y, line := range strings.Split(view, "\n") {
		if !strings.Contains(text.Plain(line), "╭") {
			continue
		}
		model.Update(pickerMousePress(2, y))
		if model.cursor != 0 || model.chosen != "" {
			t.Fatalf("a click on the top border changed the picker: cursor = %d, chosen = %q", model.cursor, model.chosen)
		}
	}
}
