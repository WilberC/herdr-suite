package session

import (
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/WilberC/herdr-suite/internal/herdrmock"
	"github.com/WilberC/tui-kit/text"
	tea "github.com/charmbracelet/bubbletea"
)

// markedRow is the one line the selection bar painted.
func markedRow(view string) string {
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, text.SelectMark) {
			return line
		}
	}
	return ""
}

type fakePreviewer struct {
	requests []string
	spaces   map[string][]WorkspacePreview
	errs     map[string]error
}

func (f *fakePreviewer) Preview(ctx context.Context, session herdrmock.Session) ([]WorkspacePreview, error) {
	f.requests = append(f.requests, session.Name)
	if err := f.errs[session.Name]; err != nil {
		return nil, err
	}
	return f.spaces[session.Name], nil
}

func newPreviewFixture(t *testing.T, previewer Previewer, sessions ...herdrmock.Session) *Switcher {
	t.Helper()
	manager := New(Config{
		Client:    &herdrmock.Fake{Sessions: sessions},
		Exec:      &fakeExec{},
		Out:       io.Discard,
		Err:       io.Discard,
		Getenv:    func(string) string { return "" },
		AssumeYes: true,
	})
	switcher := NewSwitcher(SwitcherConfig{
		Manager:     manager,
		RestartFlag: filepath.Join(t.TempDir(), "restart"),
		Preview:     previewer,
	})
	if err := switcher.Prepare(context.Background()); err != nil {
		t.Fatalf("Prepare error = %v, want nil", err)
	}
	return switcher
}

func applyPreview(t *testing.T, switcher *Switcher, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatalf("no preview command was issued")
	}
	switcher.model.Update(cmd())
}

func TestSwitcherPreviewShowsTheHighlightedSessionWorkspaces(t *testing.T) {
	previewer := &fakePreviewer{spaces: map[string][]WorkspacePreview{
		"alpha": {
			{Label: "project", Tabs: 3, Panes: 5, Agent: "working"},
			{Label: "docs", Tabs: 1, Panes: 1, Agent: "idle"},
		},
	}}
	switcher := newPreviewFixture(t, previewer,
		herdrmock.Session{Name: "alpha", Running: true},
		herdrmock.Session{Name: "beta", Running: true},
	)

	applyPreview(t, switcher, switcher.model.Init())

	view := switcher.model.View()
	for _, want := range []string{
		"alpha",
		"project",
		"3 tabs · 5 panes",
		"working",
		"docs",
		"1 tab · 1 pane",
		"idle",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("view does not contain %q:\n%s", want, view)
		}
	}
	if !strings.Contains(markedRow(view), "alpha") {
		t.Fatalf("the highlighted session is not the one marked as selected:\n%s", view)
	}
	if got, want := previewer.requests, []string{"alpha"}; !equalStrings(got, want) {
		t.Fatalf("preview requests = %v, want %v", got, want)
	}
}

func TestSwitcherPreviewFollowsTheCursor(t *testing.T) {
	previewer := &fakePreviewer{spaces: map[string][]WorkspacePreview{
		"alpha": {{Label: "project", Tabs: 2, Panes: 3, Agent: "working"}},
		"beta":  {{Label: "ci", Tabs: 1, Panes: 2, Agent: "idle"}},
	}}
	switcher := newPreviewFixture(t, previewer,
		herdrmock.Session{Name: "alpha", Running: true},
		herdrmock.Session{Name: "beta", Running: true},
	)
	applyPreview(t, switcher, switcher.model.Init())

	applyPreview(t, switcher, switcher.model.handleKey(tea.KeyMsg{Type: tea.KeyDown}))

	view := switcher.model.View()
	for _, want := range []string{"ci", "1 tab · 2 panes", "idle"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view does not contain %q:\n%s", want, view)
		}
	}
	if marked := markedRow(view); !strings.Contains(marked, "beta") || strings.Contains(marked, "alpha") {
		t.Fatalf("the highlight did not move to beta:\n%s", view)
	}
	if strings.Contains(view, "3 tabs") || strings.Contains(view, "2 tabs") {
		t.Fatalf("view kept the previous session preview:\n%s", view)
	}
	if got, want := previewer.requests, []string{"alpha", "beta"}; !equalStrings(got, want) {
		t.Fatalf("preview requests = %v, want %v", got, want)
	}
}

func TestSwitcherPreviewDegradesWhenTheSessionCannotBeRead(t *testing.T) {
	previewer := &fakePreviewer{errs: map[string]error{"alpha": io.ErrUnexpectedEOF}}
	switcher := newPreviewFixture(t, previewer, herdrmock.Session{Name: "alpha", Running: true})

	applyPreview(t, switcher, switcher.model.Init())

	view := switcher.model.View()
	if !strings.Contains(view, "unavailable") {
		t.Fatalf("view does not degrade to unavailable:\n%s", view)
	}
	if strings.Contains(view, "workspaces") {
		t.Fatalf("an unreadable session reported a workspace count:\n%s", view)
	}
}

func TestSwitcherPreviewSkipsAStoppedSession(t *testing.T) {
	previewer := &fakePreviewer{}
	switcher := newPreviewFixture(t, previewer, herdrmock.Session{Name: "share"})

	if cmd := switcher.model.Init(); cmd != nil {
		t.Fatalf("a stopped session issued a preview command")
	}

	view := switcher.model.View()
	for _, want := range []string{"share", "stopped", "enter starts it again"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view does not contain %q:\n%s", want, view)
		}
	}
	if len(previewer.requests) != 0 {
		t.Fatalf("preview requests = %v, want none for a stopped session", previewer.requests)
	}
}

func TestSwitcherPreviewIgnoresAStaleReply(t *testing.T) {
	previewer := &fakePreviewer{spaces: map[string][]WorkspacePreview{
		"alpha": {{Label: "project", Tabs: 7, Panes: 9, Agent: "working"}},
		"beta":  {{Label: "ci", Tabs: 1, Panes: 2, Agent: "idle"}},
	}}
	switcher := newPreviewFixture(t, previewer,
		herdrmock.Session{Name: "alpha", Running: true},
		herdrmock.Session{Name: "beta", Running: true},
	)
	stale := switcher.model.Init()

	applyPreview(t, switcher, switcher.model.handleKey(tea.KeyMsg{Type: tea.KeyDown}))
	switcher.model.Update(stale())

	view := switcher.model.View()
	if marked := markedRow(view); !strings.Contains(marked, "beta") || strings.Contains(view, "7 tabs") {
		t.Fatalf("a stale preview reply replaced the current one:\n%s", view)
	}
}

func TestSwitcherWithoutAPreviewerKeepsThePlainList(t *testing.T) {
	switcher := newPreviewFixture(t, nil, herdrmock.Session{Name: "alpha", Running: true})

	if cmd := switcher.model.Init(); cmd != nil {
		t.Fatalf("Init issued a command without a previewer")
	}
	view := switcher.model.View()
	for _, unwanted := range []string{"├─ project", "workspaces", "loading"} {
		if strings.Contains(view, unwanted) {
			t.Fatalf("view rendered a preview without a previewer (%q):\n%s", unwanted, view)
		}
	}
}

// TestSwitcherPreviewShowsAPendingGlyphUntilTheReplyArrives pins what a slow
// preview looks like: while the read is outstanding the preview says it is
// loading, so an empty panel never reads as "no workspaces", and the glyph is
// gone once the reply is in.
func TestSwitcherPreviewShowsAPendingGlyphUntilTheReplyArrives(t *testing.T) {
	previewer := &fakePreviewer{spaces: map[string][]WorkspacePreview{
		"alpha": {{Label: "project", Tabs: 1, Panes: 2, Agent: "working"}},
	}}
	switcher := newPreviewFixture(t, previewer, herdrmock.Session{Name: "alpha", Running: true})

	pending := switcher.model.Init()
	if pending == nil {
		t.Fatalf("no preview command was issued")
	}
	view := switcher.model.View()
	if !strings.Contains(view, "alpha") || !strings.Contains(view, "loading") {
		t.Fatalf("a pending preview does not say it is loading:\n%s", view)
	}

	switcher.model.Update(pending())

	view = switcher.model.View()
	if strings.Contains(view, "loading") {
		t.Fatalf("the loading glyph survived the reply:\n%s", view)
	}
	if !strings.Contains(view, "1 tab · 2 panes") || !strings.Contains(view, "working") {
		t.Fatalf("view does not show the reply:\n%s", view)
	}
}

// TestSwitcherPreviewGlyphSkipsAStoppedSession keeps the glyph for reads: a
// stopped session is never read, so it says it is stopped instead of loading.
func TestSwitcherPreviewGlyphSkipsAStoppedSession(t *testing.T) {
	switcher := newPreviewFixture(t, &fakePreviewer{}, herdrmock.Session{Name: "share"})

	if cmd := switcher.model.Init(); cmd != nil {
		t.Fatalf("a stopped session issued a preview command")
	}
	if view := switcher.model.View(); strings.Contains(view, "loading") {
		t.Fatalf("a stopped session shows a loading glyph:\n%s", view)
	}
}
