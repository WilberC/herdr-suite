package session

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/WilberC/herdr-suite/internal/herdrmock"
	tea "github.com/charmbracelet/bubbletea"
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
	if view := switcher.model.View(); strings.Contains(view, "> work") {
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
	if view := switcher.model.View(); !strings.Contains(view, "enter: create 'my-project'") {
		t.Fatalf("no-match view does not offer creation:\n%s", view)
	}

	switcher, _, _, _ = newSwitcherFixture(t, herdrmock.Session{Name: "work", Running: true})
	view := switcher.model.View()
	for _, want := range []string{"> work  running", "alt-enter: create", "ctrl-r: restart", "ctrl-x: delete", "esc: quit"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view does not contain %q:\n%s", want, view)
		}
	}
}
