package session

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/WilberC/herdr-suite/internal/herdrmock"
	"github.com/WilberC/tui-kit/chip"
	"github.com/WilberC/tui-kit/text"
	"github.com/WilberC/tui-kit/theme"
	"github.com/WilberC/tui-kit/zone"
	tea "github.com/charmbracelet/bubbletea"
)

// These tests are the mouse contract of the switcher: a click selects, a double
// click runs the row's default action, a destructive action never fires on a
// click, hover is a preview and the wheel moves the selection. Every coordinate
// comes from the zone map the render recorded, so no test measures the frame by
// hand and a centred frame is checked at both sizes by the same assertions.

// rowCentre is the cell in the middle of a recorded row.
func rowCentre(t *testing.T, zones *zone.Map, index int) (int, int) {
	t.Helper()
	rect, ok := zones.Find(RowZone(index))
	if !ok {
		t.Fatalf("the render recorded no zone for row %d", index)
	}
	return rect.X + rect.W/2, rect.Y
}

func mousePress(x, y int) tea.MouseMsg {
	return tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: x, Y: y}
}

func mouseMove(x, y int) tea.MouseMsg {
	return tea.MouseMsg{Action: tea.MouseActionMotion, X: x, Y: y}
}

func mouseWheel(x, y int, up bool) tea.MouseMsg {
	button := tea.MouseButtonWheelDown
	if up {
		button = tea.MouseButtonWheelUp
	}
	return tea.MouseMsg{Action: tea.MouseActionPress, Button: button, X: x, Y: y}
}

// clickFixture is the switcher a terminal of w×h runs, with the client its
// actions would reach so a test can prove none of them fired.
func clickFixture(t *testing.T, w, h int) (*Switcher, *herdrmock.Fake, string) {
	t.Helper()
	switcher, client, _, flag := newSwitcherFixture(t,
		herdrmock.Session{Name: "work", Running: true, Default: true, Socket: "/tmp/herdr/work.sock"},
		herdrmock.Session{Name: "gentle-shell", Running: true, Socket: "/tmp/herdr/gentle-shell.sock"},
		herdrmock.Session{Name: "scratch", Socket: "/tmp/herdr/scratch.sock"},
	)
	switcher.model.width, switcher.model.height = w, h
	return switcher, client, flag
}

// fixedClock hands the model a clock a test drives, so a double click never
// waits.
func fixedClock(switcher *Switcher, at time.Time) *time.Time {
	current := at
	switcher.model.now = func() time.Time { return current }
	return &current
}

// TestClickSelectsTheSessionUnderThePointer is the core promise of S8: a click
// selects the row it landed on at a wide and at a narrow pane, and it never
// attaches by itself.
func TestClickSelectsTheSessionUnderThePointer(t *testing.T) {
	for _, size := range []struct {
		name string
		w, h int
	}{
		{"180x50", 180, 50},
		{"80x24", 80, 24},
	} {
		t.Run(size.name, func(t *testing.T) {
			switcher, _, _ := clickFixture(t, size.w, size.h)
			switcher.model.View()

			x, y := rowCentre(t, switcher.model.zones, 2)
			switcher.model.Update(mousePress(x, y))

			want := switcher.model.matches[2].Name
			if got := switcher.model.cursor; got != 2 {
				t.Fatalf("cursor after a click on row 2 = %d, want 2", got)
			}
			selected, ok := switcher.model.highlighted()
			if !ok || selected.Name != want {
				t.Fatalf("the click selected %#v, want %q", selected, want)
			}
			if switcher.model.quit {
				t.Fatalf("one click attached the session")
			}
			if got := switcher.Target(); got != "" {
				t.Fatalf("Target after one click = %q, want empty", got)
			}
		})
	}
}

// TestDoubleClickAttachesAndASingleClickOnlySelects pins the default action of a
// row: two presses on the same row inside the window attach it, and two presses
// further apart than the window only select.
func TestDoubleClickAttachesAndASingleClickOnlySelects(t *testing.T) {
	switcher, _, _ := clickFixture(t, 180, 50)
	at := time.Unix(1_700_000_000, 0)
	now := fixedClock(switcher, at)
	switcher.model.View()
	x, y := rowCentre(t, switcher.model.zones, 1)
	want := switcher.model.matches[1].Name

	switcher.model.Update(mousePress(x, y))
	if switcher.model.quit {
		t.Fatalf("the first press attached the session")
	}
	if got := switcher.Target(); got != "" {
		t.Fatalf("Target after the first press = %q, want empty", got)
	}

	*now = at.Add(300 * time.Millisecond)
	switcher.model.Update(mousePress(x, y))
	if !switcher.model.quit {
		t.Fatalf("a second press inside the window did not attach the session")
	}
	if got := switcher.Target(); got != want {
		t.Fatalf("Target after a double click = %q, want %q", got, want)
	}
}

// TestASecondPressOutsideTheWindowOnlySelects pins the other side of the window:
// two ordinary clicks are two selections, never a double click.
func TestASecondPressOutsideTheWindowOnlySelects(t *testing.T) {
	switcher, _, _ := clickFixture(t, 140, 40)
	at := time.Unix(1_700_000_000, 0)
	now := fixedClock(switcher, at)
	switcher.model.View()
	x, y := rowCentre(t, switcher.model.zones, 2)

	switcher.model.Update(mousePress(x, y))
	*now = at.Add(500 * time.Millisecond)
	switcher.model.Update(mousePress(x, y))

	if switcher.model.quit {
		t.Fatalf("two clicks 500ms apart attached the session")
	}
	if got := switcher.Target(); got != "" {
		t.Fatalf("Target = %q, want empty", got)
	}
	if got := switcher.model.cursor; got != 2 {
		t.Fatalf("cursor = %d, want 2", got)
	}
}

// TestAClickNeverStopsRestartsOrDeletesTheSession pins the boundary of S8: the
// destructive bindings have no click path, not even a double click, and a click
// never answers a confirmation.
func TestAClickNeverStopsRestartsOrDeletesTheSession(t *testing.T) {
	for index := 0; index < 3; index++ {
		switcher, client, flag := clickFixture(t, 140, 40)
		at := time.Unix(1_700_000_000, 0)
		fixedClock(switcher, at)
		switcher.model.View()
		x, y := rowCentre(t, switcher.model.zones, index)

		switcher.model.Update(mousePress(x, y))
		switcher.model.Update(mousePress(x, y))

		for _, method := range []string{"StopSession", "DeleteSession"} {
			if got := callTargets(client, method); len(got) != 0 {
				t.Fatalf("a double click on row %d called %s(%v)", index, method, got)
			}
		}
		if _, err := os.Stat(flag); err == nil {
			t.Fatalf("a double click on row %d wrote the restart flag", index)
		}
		if switcher.model.pending != nil {
			t.Fatalf("a click opened a confirmation")
		}
		if !switcher.model.quit {
			t.Fatalf("the default action of row %d is attach, and it did not run", index)
		}
	}
}

// TestAClickKeepsAPendingConfirmation pins that the keyboard owns a confirmation:
// a click neither answers it, cancels it, moves the selection nor attaches.
func TestAClickKeepsAPendingConfirmation(t *testing.T) {
	switcher, client, _ := clickFixture(t, 140, 40)
	fixedClock(switcher, time.Unix(1_700_000_000, 0))

	pressKey(switcher, tea.KeyMsg{Type: tea.KeyCtrlS})
	if switcher.model.pending == nil {
		t.Fatalf("ctrl+s opened no confirmation")
	}
	switcher.model.View()
	x, y := rowCentre(t, switcher.model.zones, 2)

	switcher.model.Update(mousePress(x, y))
	switcher.model.Update(mousePress(x, y))

	if switcher.model.pending == nil {
		t.Fatalf("a click closed the confirmation")
	}
	if got := switcher.model.status; got != "" {
		t.Fatalf("a click answered the confirmation: status = %q, want empty", got)
	}
	if got := switcher.model.cursor; got != 0 {
		t.Fatalf("a click moved the selection under a confirmation: cursor = %d, want 0", got)
	}
	if got := callTargets(client, "StopSession"); len(got) != 0 {
		t.Fatalf("a click stopped sessions = %v, want none", got)
	}
	if switcher.model.quit {
		t.Fatalf("a click during a confirmation attached the session")
	}
}

// TestAClickOnANestedWorkspaceRowSelectsItsParent pins the nested rows: they are
// rows of the session they hang under, so a click there acts on that session and
// never on the workspace.
func TestAClickOnANestedWorkspaceRowSelectsItsParent(t *testing.T) {
	switcher := newSizedSwitcher(t, 140, 40)
	fixedClock(switcher, time.Unix(1_700_000_000, 0))
	switcher.model.View()

	// The highlighted session is the one that nests its workspaces, so the row
	// under it is its first workspace row.
	cursor := switcher.model.cursor
	rect, ok := switcher.model.zones.Find(RowZone(cursor))
	if !ok {
		t.Fatalf("the render recorded no zone for the highlighted session")
	}
	x := rect.X + rect.W/2
	workspaceY := rect.Bottom()
	lines := strings.Split(switcher.model.View(), "\n")
	if workspaceY >= len(lines) || !strings.Contains(lines[workspaceY], "project") {
		t.Fatalf("the row under the highlighted session is not its first workspace: %q",
			lines[min(workspaceY, len(lines)-1)])
	}

	switcher.model.Update(mousePress(x, workspaceY))

	if got := switcher.model.cursor; got != cursor {
		t.Fatalf("a click on a workspace row moved the selection to %d, want %d", got, cursor)
	}
	if switcher.model.quit {
		t.Fatalf("a click on a workspace row attached the session")
	}
	if got := switcher.Target(); got != "" {
		t.Fatalf("Target = %q, want empty", got)
	}
}

// TestHoverTintsTheRowUnderThePointerAndTheSelectionWins pins the hover surface:
// the row under the pointer wears the faint tint, exactly one row carries it, it
// is not the selection's tint, and hovering the selected row changes nothing.
func TestHoverTintsTheRowUnderThePointerAndTheSelectionWins(t *testing.T) {
	withTrueColor(t)
	switcher := newSizedSwitcher(t, 140, 40)
	switcher.model.View()

	// The pointer goes over a row that is not the selection, so both tints are on
	// screen at once.
	other := (switcher.model.cursor + 1) % len(switcher.model.matches)
	x, y := rowCentre(t, switcher.model.zones, other)
	switcher.model.Update(mouseMove(x, y))
	view := switcher.model.View()

	lines := strings.Split(view, "\n")
	if !strings.Contains(lines[y], switcher.model.matches[other].Name) {
		t.Fatalf("the hovered row is not the session under the pointer: %q", lines[y])
	}
	if !strings.Contains(lines[y], roleEscape(theme.HoverBG)) {
		t.Fatalf("the row under the pointer wears no hover tint: %q", lines[y])
	}
	if strings.Contains(lines[y], roleEscape(theme.SelectionBG)) {
		t.Fatalf("the hovered row wears the selection's tint: %q", lines[y])
	}
	if strings.Contains(lines[y], text.SelectMark) {
		t.Fatalf("the hovered row carries the selection's edge: %q", lines[y])
	}
	if got := countLines(lines, roleEscape(theme.HoverBG)); got != 1 {
		t.Fatalf("the view tinted %d hovered rows, want exactly one:\n%s", got, view)
	}
	if got := countLines(lines, roleEscape(theme.SelectionBG)); got != 1 {
		t.Fatalf("the view tinted %d selected rows, want exactly one:\n%s", got, view)
	}

	// The pointer over the selected row: the selection wins, so no row is hovered.
	x, y = rowCentre(t, switcher.model.zones, switcher.model.cursor)
	switcher.model.Update(mouseMove(x, y))
	view = switcher.model.View()
	if got := countLines(strings.Split(view, "\n"), roleEscape(theme.HoverBG)); got != 0 {
		t.Fatalf("hovering the selection tinted %d rows, want none:\n%s", got, view)
	}

	// The pointer off every row: nothing is hovered.
	switcher.model.Update(mouseMove(0, 0))
	view = switcher.model.View()
	if got := countLines(strings.Split(view, "\n"), roleEscape(theme.HoverBG)); got != 0 {
		t.Fatalf("a pointer outside the list tinted %d rows, want none:\n%s", got, view)
	}
}

// countLines counts the view lines that carry one escape.
func countLines(lines []string, escaped string) int {
	count := 0
	for _, line := range lines {
		if strings.Contains(line, escaped) {
			count++
		}
	}
	return count
}

// TestTheWheelMovesTheSelectionInsideTheBody pins the wheel: it moves the
// highlight, never past the ends, and never draws a row the body has no room for.
func TestTheWheelMovesTheSelectionInsideTheBody(t *testing.T) {
	var sessions []herdrmock.Session
	for i := 0; i < 40; i++ {
		sessions = append(sessions, herdrmock.Session{Name: "session-" + string(rune('a'+i%26)) + string(rune('a'+i/26)), Running: true})
	}
	switcher := newPreviewFixture(t, nil, sessions...)
	switcher.model.width, switcher.model.height = 90, 24
	switcher.model.View()

	for i := 0; i < 3; i++ {
		switcher.model.Update(mouseWheel(20, 10, false))
	}
	if got := switcher.model.cursor; got != 3 {
		t.Fatalf("cursor after three wheel-downs = %d, want 3", got)
	}
	view := switcher.model.View()
	if _, ok := switcher.model.zones.Find(RowZone(3)); !ok {
		t.Fatalf("the wheel moved the selection off the body:\n%s", view)
	}
	rows := switcher.model.surface().BodyRows(24)
	if got := drawnRows(switcher.model.zones); got > rows {
		t.Fatalf("the body drew %d rows, want at most BodyRows = %d", got, rows)
	}

	for i := 0; i < 100; i++ {
		switcher.model.Update(mouseWheel(20, 10, false))
	}
	if got, want := switcher.model.cursor, len(switcher.model.matches)-1; got != want {
		t.Fatalf("cursor after the wheel ran out = %d, want the last row %d", got, want)
	}
	view = switcher.model.View()
	if _, ok := switcher.model.zones.Find(RowZone(switcher.model.cursor)); !ok {
		t.Fatalf("the last row of the list is not on screen:\n%s", view)
	}

	for i := 0; i < 100; i++ {
		switcher.model.Update(mouseWheel(20, 10, true))
	}
	if got := switcher.model.cursor; got != 0 {
		t.Fatalf("cursor after the wheel ran back = %d, want 0", got)
	}
}

// drawnRows is how many distinct body rows the render drew a clickable zone on.
func drawnRows(zones *zone.Map) int {
	seen := map[int]bool{}
	for _, recorded := range zones.Zones() {
		if _, ok := RowZoneIndex(recorded.ID); ok {
			seen[recorded.Rect.Y] = true
		}
	}
	return len(seen)
}

// TestAClickOnThePagingChipPagesTheHints pins the paging chip: clicking it does
// exactly what chip.PageKey does, on the same bar.
func TestAClickOnThePagingChipPagesTheHints(t *testing.T) {
	switcher := newSizedSwitcher(t, 80, 24)
	view := switcher.model.View()
	if !strings.Contains(view, "more 1/2") {
		t.Fatalf("the narrow view does not say more bindings exist:\n%s", view)
	}

	rect, ok := switcher.model.zones.Find(chip.PageZone)
	if !ok {
		t.Fatalf("the render recorded no zone for the paging chip:\n%s", view)
	}
	switcher.model.Update(mousePress(rect.X+1, rect.Y))
	clicked := switcher.model.View()

	keyed := newSizedSwitcher(t, 80, 24)
	keyed.model.View()
	keyed.model.Update(tea.KeyMsg{Type: tea.KeyTab})

	if clicked != keyed.model.View() {
		t.Fatalf("a click on the paging chip differs from %q:\nclick: %s\nkey:   %s",
			chip.PageKey, clicked, keyed.model.View())
	}
	if !strings.Contains(clicked, "more 2/2") {
		t.Fatalf("the click did not page the bar:\n%s", clicked)
	}
}

// TestTheConfirmationKeepsItsGoldYesSlab pins the follow-up the migration owed:
// the y of a confirmation keeps the gold slab, its neighbour keeps the default
// one.
func TestTheConfirmationKeepsItsGoldYesSlab(t *testing.T) {
	withTrueColor(t)
	switcher := newSizedSwitcher(t, 140, 40)

	pressKey(switcher, tea.KeyMsg{Type: tea.KeyCtrlS})
	if switcher.model.pending == nil {
		t.Fatalf("ctrl+s opened no confirmation")
	}

	view := switcher.model.View()
	if want := roleEscape(theme.ChipGold) + " y "; !strings.Contains(view, want) {
		t.Fatalf("the confirmation's y lost the gold slab:\n%s", view)
	}
	if want := roleEscape(theme.ChipText) + " n "; !strings.Contains(view, want) {
		t.Fatalf("the confirmation's n lost the default slab:\n%s", view)
	}
}
