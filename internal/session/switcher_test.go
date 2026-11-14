package session

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/WilberC/herdr-suite/internal/herdrmock"
	"github.com/WilberC/herdr-suite/internal/ui"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func newSwitcherFixture(t *testing.T, sessions ...herdrmock.Session) (*Switcher, *herdrmock.Fake, *fakeExec, string) {
	t.Helper()
	client := &herdrmock.Fake{Sessions: sessions}
	exec := &fakeExec{}
	manager := New(Config{
		Client:    client,
		Exec:      exec,
		Out:       io.Discard,
		Err:       io.Discard,
		Getenv:    func(string) string { return "" },
		AssumeYes: true,
	})
	flag := filepath.Join(t.TempDir(), "restart")
	switcher := NewSwitcher(SwitcherConfig{
		Manager:     manager,
		RestartFlag: flag,
	})
	if err := switcher.Prepare(context.Background()); err != nil {
		t.Fatalf("Prepare error = %v, want nil", err)
	}
	return switcher, client, exec, flag
}

func pressKey(switcher *Switcher, msg tea.KeyMsg) {
	switcher.model.handleKey(msg)
}

func typeText(switcher *Switcher, text string) {
	for _, r := range text {
		pressKey(switcher, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

func matchedNames(switcher *Switcher) []string {
	var names []string
	for _, session := range switcher.model.matches {
		names = append(names, session.Name)
	}
	return names
}

func equalStrings(a, b []string) bool {
	return strings.Join(a, ",") == strings.Join(b, ",")
}

func TestSwitcherSearchesSessionNamesOnly(t *testing.T) {
	switcher, _, _, _ := newSwitcherFixture(t,
		herdrmock.Session{Name: "work", Running: true},
		herdrmock.Session{Name: "runner"},
		herdrmock.Session{Name: "scratch"},
	)

	typeText(switcher, "run")

	if got, want := matchedNames(switcher), []string{"runner"}; !equalStrings(got, want) {
		t.Fatalf("matches after %q = %v, want %v", "run", got, want)
	}
	view := switcher.model.View()
	if !strings.Contains(markedRow(view), "runner") {
		t.Fatalf("the search did not highlight the matching session:\n%s", view)
	}
	if strings.Contains(view, "work") {
		t.Fatalf("view matched the running state column:\n%s", view)
	}

	typeText(switcher, "stopped")

	if got := matchedNames(switcher); len(got) != 0 {
		t.Fatalf("matches after %q = %v, want none: the state column is not searched", "runstopped", got)
	}
}

func TestSwitcherEnterAttachesTheHighlightedSession(t *testing.T) {
	switcher, _, exec, _ := newSwitcherFixture(t,
		herdrmock.Session{Name: "work", Running: true},
		herdrmock.Session{Name: "runner"},
	)

	typeText(switcher, "wor")
	pressKey(switcher, tea.KeyMsg{Type: tea.KeyEnter})

	if got, want := switcher.model.target, "work"; got != want {
		t.Fatalf("target = %q, want %q", got, want)
	}
	if !switcher.model.quit {
		t.Fatalf("switcher did not quit after attaching")
	}
	if got, want := switcher.Target(), "work"; got != want {
		t.Fatalf("Target = %q, want %q", got, want)
	}
	if len(exec.names) != 0 {
		t.Fatalf("exec targets = %v, want none inside the model", exec.names)
	}
}

func TestSwitcherEnterCreatesTheTypedNameWhenNothingMatches(t *testing.T) {
	switcher, _, _, _ := newSwitcherFixture(t, herdrmock.Session{Name: "work", Running: true})

	typeText(switcher, "my project")
	pressKey(switcher, tea.KeyMsg{Type: tea.KeyEnter})

	if got, want := switcher.Target(), "my-project"; got != want {
		t.Fatalf("Target = %q, want %q", got, want)
	}
	if !switcher.model.quit {
		t.Fatalf("switcher did not quit after creating")
	}
}

func TestSwitcherAltEnterCreatesTheTypedNameEvenWhenAMatchExists(t *testing.T) {
	switcher, _, _, _ := newSwitcherFixture(t, herdrmock.Session{Name: "work", Running: true})

	typeText(switcher, "wor")
	if got, want := matchedNames(switcher), []string{"work"}; !equalStrings(got, want) {
		t.Fatalf("matches = %v, want %v", got, want)
	}

	pressKey(switcher, tea.KeyMsg{Type: tea.KeyEnter, Alt: true})

	if got, want := switcher.model.target, "wor"; got != want {
		t.Fatalf("target = %q, want the typed name %q", got, want)
	}
	if !switcher.model.quit {
		t.Fatalf("switcher did not quit after creating")
	}
}

func TestSwitcherEscQuitsWithoutATarget(t *testing.T) {
	switcher, _, _, _ := newSwitcherFixture(t, herdrmock.Session{Name: "work", Running: true})

	pressKey(switcher, tea.KeyMsg{Type: tea.KeyEsc})

	if !switcher.model.quit {
		t.Fatalf("switcher did not quit on esc")
	}
	if got := switcher.Target(); got != "" {
		t.Fatalf("Target = %q, want empty", got)
	}
}

func TestSwitcherStopAsksBeforeStopping(t *testing.T) {
	switcher, client, _, _ := newSwitcherFixture(t, herdrmock.Session{Name: "work", Running: true})

	pressKey(switcher, tea.KeyMsg{Type: tea.KeyCtrlS})

	if switcher.model.pending == nil {
		t.Fatalf("ctrl+s opened no confirmation")
	}
	if got := callTargets(client, "StopSession"); len(got) != 0 {
		t.Fatalf("stopped sessions = %v, want none before answering", got)
	}
	if view := switcher.model.View(); !strings.Contains(view, "stop 'work'? its agents will be killed [y/N]") {
		t.Fatalf("view does not show the confirmation:\n%s", view)
	}

	pressKey(switcher, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})

	if got := callTargets(client, "StopSession"); len(got) != 0 {
		t.Fatalf("stopped sessions = %v, want none after declining", got)
	}
	if got, want := switcher.model.status, "cancelled"; got != want {
		t.Fatalf("status = %q, want %q", got, want)
	}
	if switcher.model.quit {
		t.Fatalf("declining a stop quit the switcher")
	}
}

func TestSwitcherStopAnswersYesAndKeepsTheListOpen(t *testing.T) {
	switcher, client, _, _ := newSwitcherFixture(t, herdrmock.Session{Name: "work", Running: true})

	pressKey(switcher, tea.KeyMsg{Type: tea.KeyCtrlS})
	pressKey(switcher, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})

	if got, want := callTargets(client, "StopSession"), []string{"work"}; !equalStrings(got, want) {
		t.Fatalf("stopped sessions = %v, want %v", got, want)
	}
	if got, want := switcher.model.status, "stopped 'work'"; got != want {
		t.Fatalf("status = %q, want %q", got, want)
	}
	if switcher.model.quit {
		t.Fatalf("a stop quit the switcher")
	}
}

func TestSwitcherDeleteRefusesTheDefaultSession(t *testing.T) {
	switcher, client, _, _ := newSwitcherFixture(t, herdrmock.Session{Name: "default", Running: true, Default: true})

	pressKey(switcher, tea.KeyMsg{Type: tea.KeyCtrlX})

	if switcher.model.pending != nil {
		t.Fatalf("the default session opened a confirmation")
	}
	if got, want := switcher.model.status, "'default' is the default session and cannot be deleted"; got != want {
		t.Fatalf("status = %q, want %q", got, want)
	}
	if got := callTargets(client, "DeleteSession"); len(got) != 0 {
		t.Fatalf("deleted sessions = %v, want none", got)
	}
}

func TestSwitcherDeleteStopsTheSessionFirst(t *testing.T) {
	switcher, client, _, _ := newSwitcherFixture(t, herdrmock.Session{Name: "work", Running: true})

	pressKey(switcher, tea.KeyMsg{Type: tea.KeyCtrlX})
	if view := switcher.model.View(); !strings.Contains(view, "delete 'work'? it will be stopped first [y/N]") {
		t.Fatalf("view does not show the delete confirmation:\n%s", view)
	}
	pressKey(switcher, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})

	if got, want := callTargets(client, "StopSession"), []string{"work"}; !equalStrings(got, want) {
		t.Fatalf("stopped sessions = %v, want %v", got, want)
	}
	if got, want := callTargets(client, "DeleteSession"), []string{"work"}; !equalStrings(got, want) {
		t.Fatalf("deleted sessions = %v, want %v", got, want)
	}
	if got, want := switcher.model.status, "deleted 'work'"; got != want {
		t.Fatalf("status = %q, want %q", got, want)
	}
}

func TestSwitcherRestartWritesTheFlagAndLeavesTheListOpen(t *testing.T) {
	switcher, client, _, flag := newSwitcherFixture(t, herdrmock.Session{Name: "work", Running: true})

	pressKey(switcher, tea.KeyMsg{Type: tea.KeyCtrlR})
	pressKey(switcher, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})

	if got, want := callTargets(client, "StopSession"), []string{"work"}; !equalStrings(got, want) {
		t.Fatalf("stopped sessions = %v, want %v", got, want)
	}
	data, err := os.ReadFile(flag)
	if err != nil {
		t.Fatalf("restart flag not written: %v", err)
	}
	if got, want := string(data), "work"; got != want {
		t.Fatalf("restart flag = %q, want %q", got, want)
	}
	if switcher.model.quit {
		t.Fatalf("a restart quit the switcher")
	}
	if got, want := switcher.Target(), "work"; got != want {
		t.Fatalf("Target after a restart = %q, want %q", got, want)
	}
}

func TestSwitcherReportsAnInvalidTypedName(t *testing.T) {
	switcher, _, _, _ := newSwitcherFixture(t)

	typeText(switcher, "work!")
	pressKey(switcher, tea.KeyMsg{Type: tea.KeyEnter})

	if got, want := switcher.model.status, "invalid session name 'work!'; use letters, digits, '.', '_' and '-'"; got != want {
		t.Fatalf("status = %q, want %q", got, want)
	}
	if switcher.model.quit {
		t.Fatalf("an invalid name quit the switcher")
	}
	if got := switcher.Target(); got != "" {
		t.Fatalf("Target = %q, want empty", got)
	}
}

func TestSwitcherCursorStaysWithinTheMatches(t *testing.T) {
	switcher, _, _, _ := newSwitcherFixture(t,
		herdrmock.Session{Name: "alpha"},
		herdrmock.Session{Name: "beta"},
	)

	pressKey(switcher, tea.KeyMsg{Type: tea.KeyDown})
	if got, want := switcher.model.cursor, 1; got != want {
		t.Fatalf("cursor after down = %d, want %d", got, want)
	}
	pressKey(switcher, tea.KeyMsg{Type: tea.KeyDown})
	if got, want := switcher.model.cursor, 1; got != want {
		t.Fatalf("cursor after a second down = %d, want %d", got, want)
	}
	pressKey(switcher, tea.KeyMsg{Type: tea.KeyUp})
	if got, want := switcher.model.cursor, 0; got != want {
		t.Fatalf("cursor after up = %d, want %d", got, want)
	}
	pressKey(switcher, tea.KeyMsg{Type: tea.KeyUp})
	if got, want := switcher.model.cursor, 0; got != want {
		t.Fatalf("cursor after a second up = %d, want %d", got, want)
	}
}

func TestSwitcherViewFollowsTheQuery(t *testing.T) {
	switcher, _, _, _ := newSwitcherFixture(t)

	if view := switcher.model.View(); !strings.Contains(view, "no sessions") {
		t.Fatalf("empty view does not invite a name:\n%s", view)
	}

	typeText(switcher, "my project")
	if view := switcher.model.View(); !strings.Contains(view, "enter creates 'my-project'") {
		t.Fatalf("no-match view does not offer creation:\n%s", view)
	}

	switcher, _, _, _ = newSwitcherFixture(t, herdrmock.Session{Name: "work", Running: true})
	view := switcher.model.View()
	for _, want := range []string{"RUNNING", " alt+enter ", "create", " ctrl+r ", "restart", " ctrl+x ", "delete", "esc to close"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view does not contain %q:\n%s", want, view)
		}
	}
	if !strings.Contains(markedRow(view), "work") {
		t.Fatalf("the highlighted session is not marked as selected:\n%s", view)
	}
}

// newSizedSwitcher builds the switcher a terminal of size w×h runs, with the
// highlighted session's preview already read: what every visual guarantee is
// checked against.
func newSizedSwitcher(t *testing.T, w, h int) *Switcher {
	t.Helper()
	previewer := &fakePreviewer{spaces: map[string][]WorkspacePreview{
		"work": {
			{Label: "project", Tabs: 3, Panes: 5, Agent: "working"},
			{Label: "docs", Tabs: 1, Panes: 1, Agent: "idle"},
			{Label: "ci", Tabs: 2, Panes: 3, Agent: "working"},
		},
		"gentle-shell": {{Label: "notes", Tabs: 1, Panes: 2}},
	}}
	switcher := newPreviewFixture(t, previewer,
		herdrmock.Session{Name: "work", Running: true, Default: true, Socket: "/home/ana/.local/share/herdr/sessions/work.sock"},
		herdrmock.Session{Name: "gentle-shell", Running: true, Socket: "/home/ana/.local/share/herdr/sessions/gentle-shell.sock"},
		herdrmock.Session{Name: "herdr-suite", Running: true, Socket: "/home/ana/.local/share/herdr/sessions/herdr-suite.sock"},
		herdrmock.Session{Name: "api-gateway", Running: true, Socket: "/home/ana/.local/share/herdr/sessions/api-gateway.sock"},
		herdrmock.Session{Name: "scratch", Socket: "/home/ana/.local/share/herdr/sessions/scratch.sock"},
		herdrmock.Session{Name: "research", Socket: "/home/ana/.local/share/herdr/sessions/research.sock"},
		herdrmock.Session{Name: "demo", Socket: "/home/ana/.local/share/herdr/sessions/demo.sock"},
		herdrmock.Session{Name: "a-session-with-a-name-long-enough-to-need-clipping", Socket: "/home/ana/.local/share/herdr/sessions/very-long-socket-name.sock"},
	)
	switcher.model.width, switcher.model.height = w, h
	// The manager lists running sessions first, so the shot highlights the
	// default one by moving the cursor the way a user would.
	for i, session := range switcher.model.matches {
		if session.Default {
			switcher.model.cursor = i
		}
	}
	applyPreview(t, switcher, switcher.model.previewCmd())
	return switcher
}

// TestSwitcherViewFitsTheWindow pins the responsive contract: at a wide, a
// normal and a narrow window every line stays inside the window, nothing wraps,
// and the frame is always closed.
func TestSwitcherViewFitsTheWindow(t *testing.T) {
	for _, size := range []struct {
		name string
		w, h int
	}{
		{"180x50", 180, 50},
		{"140x40", 140, 40},
		{"80x24", 80, 24},
	} {
		switcher := newSizedSwitcher(t, size.w, size.h)
		lines := strings.Split(switcher.model.View(), "\n")

		if len(lines) > size.h {
			t.Fatalf("%s: view has %d lines, want at most %d", size.name, len(lines), size.h)
		}
		for i, line := range lines {
			if got := ui.Width(line); got > size.w {
				t.Fatalf("%s: line %d is %d cells wide, want at most %d: %q", size.name, i, got, size.w, line)
			}
		}
		frame := strings.TrimSpace(strings.Join(lines, "\n"))
		if !strings.Contains(frame, "╭") || !strings.Contains(frame, "╯") {
			t.Fatalf("%s: the frame is not closed:\n%s", size.name, frame)
		}

		// The status line and the confirmation are the two rows whose length the
		// model does not control, so they are checked at the same window.
		switcher.model.status = "invalid session name 'work!'; use letters, digits, '.', '_' and '-' and nothing else at all"
		switcher.model.cursor = len(switcher.model.matches) - 1
		pressKey(switcher, tea.KeyMsg{Type: tea.KeyCtrlX})
		for i, line := range strings.Split(switcher.model.View(), "\n") {
			if got := ui.Width(line); got > size.w {
				t.Fatalf("%s: line %d is %d cells wide with a status and a confirmation, want at most %d: %q", size.name, i, got, size.w, line)
			}
		}
	}
}

// TestSwitcherMarksExactlyOneSelectedRow pins the selection affordance: one row
// carries the mark and it is the highlighted session's.
func TestSwitcherMarksExactlyOneSelectedRow(t *testing.T) {
	switcher := newSizedSwitcher(t, 140, 40)

	for range switcher.model.matches {
		session, ok := switcher.model.highlighted()
		if !ok {
			t.Fatalf("no session is highlighted")
		}
		var marked []string
		for _, line := range strings.Split(switcher.model.View(), "\n") {
			if strings.Contains(line, ui.SelectMark) {
				marked = append(marked, line)
			}
		}
		if len(marked) != 1 {
			t.Fatalf("view marked %d rows, want exactly one:\n%s", len(marked), strings.Join(marked, "\n"))
		}
		if !strings.Contains(marked[0], session.Name) {
			t.Fatalf("the marked row is %q, want the highlighted session %q", marked[0], session.Name)
		}
		pressKey(switcher, tea.KeyMsg{Type: tea.KeyDown})
	}
}

// TestSwitcherFooterFollowsTheSelection pins the footer: it names the
// highlighted session and its facts, and it moves with the highlight.
func TestSwitcherFooterFollowsTheSelection(t *testing.T) {
	switcher := newSizedSwitcher(t, 140, 40)

	view := switcher.model.View()
	for _, want := range []string{"work", "● running", "◆ default", "3 workspaces"} {
		if !strings.Contains(footerLines(view), want) {
			t.Fatalf("the footer does not contain %q:\n%s", want, footerLines(view))
		}
	}

	pressKey(switcher, tea.KeyMsg{Type: tea.KeyDown})
	next, ok := switcher.model.highlighted()
	if !ok || next.Name == "work" {
		t.Fatalf("the highlight did not move: %#v", next)
	}
	view = switcher.model.View()
	if !strings.Contains(footerLines(view), next.Name) {
		t.Fatalf("the footer did not follow the highlight to %q:\n%s", next.Name, footerLines(view))
	}
	if strings.Contains(footerLines(view), "◆ default") {
		t.Fatalf("the footer kept the previous session's facts:\n%s", footerLines(view))
	}
}

// TestSwitcherFooterKeepsTheNameAt80Columns pins the narrow-window priority:
// the socket path is the first fact dropped, never the name or its state.
func TestSwitcherFooterKeepsTheNameAt80Columns(t *testing.T) {
	switcher := newSizedSwitcher(t, 80, 24)

	footer := footerLines(switcher.model.View())
	if !strings.Contains(footer, "work") || !strings.Contains(footer, "● running") {
		t.Fatalf("the footer lost the selected session's name or state:\n%s", footer)
	}
	if strings.Contains(footer, "/home/ana/") {
		t.Fatalf("the footer kept a path that cannot fit:\n%s", footer)
	}
}

// footerLines is the view's last two interior lines: the detail of the
// selected item and the binding bar.
func footerLines(view string) string {
	lines := strings.Split(view, "\n")
	if len(lines) < 4 {
		return view
	}
	return strings.Join(lines[len(lines)-3:len(lines)-1], "\n")
}

// TestSwitcherKeepsTheHighlightInView pins what a list longer than the window
// does: it scrolls instead of overflowing, and the highlighted session stays on
// screen with the frame still closed.
func TestSwitcherKeepsTheHighlightInView(t *testing.T) {
	var sessions []herdrmock.Session
	for i := 0; i < 40; i++ {
		sessions = append(sessions, herdrmock.Session{Name: "session-" + string(rune('a'+i%26)) + string(rune('a'+i/26)), Running: true})
	}
	switcher := newPreviewFixture(t, nil, sessions...)
	switcher.model.width, switcher.model.height = 90, 24
	switcher.model.cursor = len(switcher.model.matches) - 1

	view := switcher.model.View()
	lines := strings.Split(view, "\n")
	if len(lines) > 24 {
		t.Fatalf("view has %d lines, want at most 24", len(lines))
	}
	session, _ := switcher.model.highlighted()
	if !strings.Contains(view, ui.SelectMark+" ") || !strings.Contains(view, session.Name) {
		t.Fatalf("the highlighted session %q is not on screen:\n%s", session.Name, view)
	}
	if !strings.Contains(lines[len(lines)-1], "╯") {
		t.Fatalf("the frame is not closed:\n%s", view)
	}
}

// withTrueColor renders for one assertion with the truecolor profile forced, so
// a test can read the role a cell is painted with.
func withTrueColor(t *testing.T) {
	t.Helper()
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(previous) })
}

// roleEscape is the escape a style renders, so a test can look for one role in a
// rendered row.
func roleEscape(style lipgloss.Style) string {
	rendered := style.Render("x")
	if i := strings.Index(rendered, "x"); i > 0 {
		return rendered[:i]
	}
	return rendered
}

// TestSwitcherSelectionTintsTheChosenRow pins the selection surface: exactly one
// row carries the tint, it opens with the accent edge, and the default marker on
// it keeps its gold.
func TestSwitcherSelectionTintsTheChosenRow(t *testing.T) {
	withTrueColor(t)
	switcher := newSizedSwitcher(t, 140, 40)

	var tinted []string
	for _, line := range strings.Split(switcher.model.View(), "\n") {
		if strings.Contains(line, roleEscape(ui.SelectionBG)) {
			tinted = append(tinted, line)
		}
	}
	if len(tinted) != 1 {
		t.Fatalf("view tinted %d rows, want exactly one:\n%s", len(tinted), strings.Join(tinted, "\n"))
	}
	session, _ := switcher.model.highlighted()
	if !strings.Contains(tinted[0], session.Name) || !strings.Contains(tinted[0], ui.SelectMark) {
		t.Fatalf("the tinted row is not the highlighted session: %q", tinted[0])
	}
	if !strings.Contains(tinted[0], roleEscape(ui.RowRoles(true).Gold)) {
		t.Fatalf("the default marker lost its gold on the tint: %q", tinted[0])
	}
	if !strings.Contains(tinted[0], roleEscape(ui.TextOnSelection)) {
		t.Fatalf("the selected name is not painted with the selection text role: %q", tinted[0])
	}
	if strings.Contains(tinted[0], "48;2;121;162;247") {
		t.Fatalf("the row is painted with the accent fill instead of a tint: %q", tinted[0])
	}
}
