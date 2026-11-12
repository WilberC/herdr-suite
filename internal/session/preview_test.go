package session

import (
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/WilberC/herdr-suite/internal/herdrmock"
	tea "github.com/charmbracelet/bubbletea"
)

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
		"session 'alpha'",
		"project",
		"3 tabs · 5 panes · working",
		"docs",
		"1 tab · 1 pane · idle",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("view does not contain %q:\n%s", want, view)
		}
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
	for _, want := range []string{"session 'beta'", "ci", "1 tab · 2 panes · idle"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view does not contain %q:\n%s", want, view)
		}
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
	if !strings.Contains(view, "session 'alpha'") || !strings.Contains(view, "unavailable") {
		t.Fatalf("view does not degrade to unavailable:\n%s", view)
	}
}

func TestSwitcherPreviewSkipsAStoppedSession(t *testing.T) {
	previewer := &fakePreviewer{}
	switcher := newPreviewFixture(t, previewer, herdrmock.Session{Name: "share"})

	if cmd := switcher.model.Init(); cmd != nil {
		t.Fatalf("a stopped session issued a preview command")
	}

	view := switcher.model.View()
	for _, want := range []string{"session 'share' · stopped", "enter starts it again"} {
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
	if !strings.Contains(view, "session 'beta'") || strings.Contains(view, "7 tabs") {
		t.Fatalf("a stale preview reply replaced the current one:\n%s", view)
	}
}

func TestSwitcherWithoutAPreviewerKeepsThePlainList(t *testing.T) {
	switcher := newPreviewFixture(t, nil, herdrmock.Session{Name: "alpha", Running: true})

	if cmd := switcher.model.Init(); cmd != nil {
		t.Fatalf("Init issued a command without a previewer")
	}
	view := switcher.model.View()
	if strings.Contains(view, "│") || strings.Contains(view, "session '") {
		t.Fatalf("view rendered a preview without a previewer:\n%s", view)
	}
}
