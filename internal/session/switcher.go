package session

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/WilberC/herdr-suite/internal/herdrmock"
	tea "github.com/charmbracelet/bubbletea"
)

// WorkspacePreview is one workspace of the highlighted session, as the
// switcher preview shows it.
type WorkspacePreview struct {
	Label string
	Tabs  int
	Panes int
	Agent string
}

// Previewer reads the workspaces of one session for the switcher preview. The
// switcher never talks to herdr itself; the CLI wires the topology client in.
type Previewer interface {
	Preview(ctx context.Context, session herdrmock.Session) ([]WorkspacePreview, error)
}

// SpacePreviewer adapts a topology client to the switcher preview, so the
// preview reads the same access layer space commands use.
type SpacePreviewer struct {
	Client herdrmock.SpaceClient
}

// Preview reads the workspaces of session.
func (p SpacePreviewer) Preview(ctx context.Context, session herdrmock.Session) ([]WorkspacePreview, error) {
	if p.Client == nil {
		return nil, errors.New("no herdr client")
	}
	space, err := p.Client.Snapshot(ctx, herdrmock.Target{Name: session.Name, Socket: session.Socket})
	if err != nil {
		return nil, err
	}

	previews := make([]WorkspacePreview, 0, len(space.Workspaces))
	for _, workspace := range space.Workspaces {
		previews = append(previews, WorkspacePreview{
			Label: workspace.Label,
			Tabs:  workspace.TabCount,
			Panes: workspace.PaneCount,
			Agent: workspace.AgentState,
		})
	}
	return previews, nil
}

// SwitcherConfig carries what the interactive switcher needs. Manager runs the
// confirmed actions, so build it with AssumeYes: the switcher asks its own
// confirmation for every destructive action. RestartFlag is the handoff file
// the ctrl-r bind writes the restarted session to; the caller attaches it once
// the switcher exits, mirroring the old temp-file design. Preview reads the
// highlighted session's workspaces; a nil Previewer keeps the plain list.
type SwitcherConfig struct {
	Manager     *Manager
	RestartFlag string
	Preview     Previewer
}

// Switcher is the interactive session list. It filters by session name and
// reports the session to attach after the program exits.
type Switcher struct {
	config SwitcherConfig
	model  *switcher
}

// NewSwitcher returns a switcher over config.
func NewSwitcher(config SwitcherConfig) *Switcher {
	return &Switcher{config: config, model: newSwitcher(config)}
}

// Prepare loads the sessions the switcher lists.
func (s *Switcher) Prepare(ctx context.Context) error {
	sessions, err := s.config.Manager.List(ctx)
	if err != nil {
		return err
	}
	s.model.setSessions(sessions)
	return nil
}

// Run shows the switcher until the user leaves it.
func (s *Switcher) Run(in io.Reader, out io.Writer) error {
	options := []tea.ProgramOption{tea.WithOutput(out)}
	if in != nil {
		options = append(options, tea.WithInput(in))
	}
	_, err := tea.NewProgram(s.model, options...).Run()
	return err
}

// Target is the session to attach once the switcher exits: the restarted
// session when ctrl-r stopped one, otherwise the session the user picked or
// created. An empty result means there is nothing to attach.
func (s *Switcher) Target() string {
	if s.config.RestartFlag != "" {
		if data, err := os.ReadFile(s.config.RestartFlag); err == nil {
			if name := strings.TrimSpace(string(data)); name != "" {
				return name
			}
		}
	}
	return s.model.target
}

type promptKind int

const (
	promptStop promptKind = iota + 1
	promptRestart
	promptDelete
)

type pendingAction struct {
	kind promptKind
	name string
}

type switcher struct {
	manager  *Manager
	flagPath string
	preview  Previewer
	sessions []herdrmock.Session
	matches  []herdrmock.Session
	query    string
	cursor   int
	status   string
	pending  *pendingAction
	target   string
	quit     bool

	previewName       string
	previewWorkspaces []WorkspacePreview
	previewErr        error
	previewDone       bool
}

func newSwitcher(config SwitcherConfig) *switcher {
	return &switcher{manager: config.Manager, flagPath: config.RestartFlag, preview: config.Preview}
}

// previewMsg carries one session's preview back to the model.
type previewMsg struct {
	name       string
	workspaces []WorkspacePreview
	err        error
}

// Init loads the preview of the first highlighted session: bubbletea calls it
// once the program starts, after Prepare filled the list.
func (s *switcher) Init() tea.Cmd { return s.previewCmd() }

func (s *switcher) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return s, s.handleKey(msg)
	case previewMsg:
		if msg.name != s.previewName {
			return s, nil
		}
		s.previewWorkspaces = msg.workspaces
		s.previewErr = msg.err
		s.previewDone = true
		return s, nil
	}
	return s, nil
}

// previewCmd reads the highlighted session's preview in the background. It
// records which session the reply belongs to, so a reply that arrives after the
// highlight moved on is discarded.
func (s *switcher) previewCmd() tea.Cmd {
	if s.preview == nil {
		return nil
	}
	session, ok := s.highlighted()
	if !ok {
		return nil
	}

	s.previewName = session.Name
	s.previewWorkspaces = nil
	s.previewErr = nil
	s.previewDone = false
	if !session.Running {
		return nil
	}

	preview := s.preview
	return func() tea.Msg {
		workspaces, err := preview.Preview(context.Background(), session)
		return previewMsg{name: session.Name, workspaces: workspaces, err: err}
	}
}

func (s *switcher) handleKey(key tea.KeyMsg) tea.Cmd {
	if s.pending != nil {
		action := *s.pending
		s.pending = nil
		switch strings.ToLower(key.String()) {
		case "y", "yes":
			s.apply(action)
			return s.previewCmd()
		default:
			s.status = "cancelled"
		}
		return nil
	}

	switch key.String() {
	case "ctrl+c", "esc":
		s.quit = true
		return tea.Quit
	case "enter":
		return s.pick(false)
	case "alt+enter":
		return s.pick(true)
	case "ctrl+r":
		s.ask(promptRestart)
		return nil
	case "ctrl+s":
		s.ask(promptStop)
		return nil
	case "ctrl+x":
		s.ask(promptDelete)
		return nil
	case "up", "ctrl+k":
		if s.cursor > 0 {
			s.cursor--
		}
		return s.previewCmd()
	case "down", "ctrl+j":
		if s.cursor+1 < len(s.matches) {
			s.cursor++
		}
		return s.previewCmd()
	case "backspace":
		s.dropLastRune()
		return s.previewCmd()
	}

	if key.Type == tea.KeyRunes {
		s.query += string(key.Runes)
		s.refilter()
		return s.previewCmd()
	}
	return nil
}

// pick attaches the highlighted session, or creates the typed name when
// nothing matches. A forced pick always uses the typed name, the alt-enter
// behavior.
func (s *switcher) pick(forceTyped bool) tea.Cmd {
	if !forceTyped && len(s.matches) > 0 {
		s.target = s.matches[s.cursor].Name
		s.quit = true
		return tea.Quit
	}

	name, err := Sanitize(s.query)
	if err != nil {
		s.status = err.Error()
		return nil
	}
	s.target = name
	s.quit = true
	return tea.Quit
}

// ask opens the confirmation for a destructive action on the highlighted
// session. The default session is refused before any prompt.
func (s *switcher) ask(kind promptKind) {
	session, ok := s.highlighted()
	if !ok {
		return
	}
	if kind == promptDelete && session.Default {
		s.status = fmt.Sprintf("'%s' is the default session and cannot be deleted", session.Name)
		return
	}
	s.pending = &pendingAction{kind: kind, name: session.Name}
}

func (s *switcher) apply(action pendingAction) {
	ctx := context.Background()
	switch action.kind {
	case promptStop:
		if err := s.manager.Stop(ctx, action.name); err != nil {
			s.status = err.Error()
			return
		}
		s.status = fmt.Sprintf("stopped '%s'", action.name)
	case promptRestart:
		if err := s.manager.Restart(ctx, action.name, s.flagPath); err != nil {
			s.status = err.Error()
			return
		}
		s.status = fmt.Sprintf("restarted '%s': it will attach when you leave", action.name)
	case promptDelete:
		if err := s.manager.Delete(ctx, action.name); err != nil {
			s.status = err.Error()
			return
		}
		s.status = fmt.Sprintf("deleted '%s'", action.name)
	}
	s.reload(ctx)
}

func (s *switcher) reload(ctx context.Context) {
	sessions, err := s.manager.List(ctx)
	if err != nil {
		s.status = err.Error()
		return
	}
	s.setSessions(sessions)
}

func (s *switcher) setSessions(sessions []herdrmock.Session) {
	s.sessions = sessions
	s.refilter()
}

func (s *switcher) refilter() {
	query := strings.ToLower(s.query)
	s.matches = make([]herdrmock.Session, 0, len(s.sessions))
	for _, session := range s.sessions {
		if strings.Contains(strings.ToLower(session.Name), query) {
			s.matches = append(s.matches, session)
		}
	}
	if s.cursor >= len(s.matches) {
		s.cursor = len(s.matches) - 1
	}
	if s.cursor < 0 {
		s.cursor = 0
	}
}

func (s *switcher) dropLastRune() {
	runes := []rune(s.query)
	if len(runes) == 0 {
		return
	}
	s.query = string(runes[:len(runes)-1])
	s.refilter()
}

func (s *switcher) highlighted() (herdrmock.Session, bool) {
	if s.cursor < 0 || s.cursor >= len(s.matches) {
		return herdrmock.Session{}, false
	}
	return s.matches[s.cursor], true
}

// hint follows the query the same way the old fzf header did: it invites a
// name when nothing matches, and lists the binds otherwise.
func (s *switcher) hint() string {
	if len(s.sessions) == 0 && s.query == "" {
		return "no sessions · type a name, enter to create · esc: quit"
	}
	if len(s.matches) == 0 && s.query != "" {
		name, err := Sanitize(s.query)
		if err != nil {
			return fmt.Sprintf("enter: %s · esc: quit", err)
		}
		return fmt.Sprintf("enter: create '%s' · esc: quit", name)
	}
	return "enter: attach · alt-enter: create · ctrl-r: restart · ctrl-s: stop · ctrl-x: delete · esc: quit"
}

func (s *switcher) View() string {
	var view strings.Builder
	view.WriteString("herdr sessions\n")
	fmt.Fprintf(&view, "herdr ❯ %s\n\n", s.query)

	width := 0
	for _, session := range s.matches {
		if n := len([]rune(session.Name)); n > width {
			width = n
		}
	}
	rows := make([]string, 0, len(s.matches))
	for i, session := range s.matches {
		marker := "  "
		if i == s.cursor {
			marker = "> "
		}
		rows = append(rows, fmt.Sprintf("%s%s  %s", marker, padName(session.Name, width), state(session)))
	}
	view.WriteString(s.columns(rows))

	if s.status != "" {
		fmt.Fprintf(&view, "\n› %s\n", s.status)
	}
	view.WriteString("\n")
	if s.pending != nil {
		fmt.Fprintf(&view, "%s [y/N]\n", promptText(*s.pending))
	} else {
		view.WriteString(s.hint() + "\n")
	}
	return view.String()
}

// columns places the session rows beside the preview, padding the list so the
// divider lines up. Without a preview the rows render exactly as before.
func (s *switcher) columns(rows []string) string {
	preview := s.previewLines()
	if len(preview) == 0 {
		var plain strings.Builder
		for _, row := range rows {
			plain.WriteString(row)
			plain.WriteString("\n")
		}
		return plain.String()
	}

	width := minListWidth
	for _, row := range rows {
		if n := len([]rune(row)); n > width {
			width = n
		}
	}

	var columns strings.Builder
	for i := 0; i < len(rows) || i < len(preview); i++ {
		left, right := "", ""
		if i < len(rows) {
			left = rows[i]
		}
		if i < len(preview) {
			right = preview[i]
		}
		line := padName(left, width) + "  │ " + right
		columns.WriteString(strings.TrimRight(line, " "))
		columns.WriteString("\n")
	}
	return columns.String()
}

const minListWidth = 24

// previewLines renders the highlighted session's workspaces: each workspace
// with its tab and pane counts and herdr's agent state. A session that cannot
// be read degrades to "unavailable" instead of failing the switcher.
func (s *switcher) previewLines() []string {
	if s.preview == nil {
		return nil
	}
	session, ok := s.highlighted()
	if !ok {
		return nil
	}

	header := fmt.Sprintf("session '%s'", session.Name)
	if !session.Running {
		return []string{header + " · stopped", "  enter starts it again"}
	}
	lines := []string{header}
	if !s.previewDone {
		return lines
	}
	if s.previewErr != nil {
		return append(lines, "  unavailable")
	}
	if len(s.previewWorkspaces) == 0 {
		return append(lines, "  no workspaces")
	}

	for _, workspace := range s.previewWorkspaces {
		lines = append(lines, "  "+workspace.Label)
		detail := countLabel(workspace.Tabs, "tab") + " · " + countLabel(workspace.Panes, "pane")
		if workspace.Agent != "" {
			detail += " · " + workspace.Agent
		}
		lines = append(lines, "    "+detail)
	}
	return lines
}

func countLabel(count int, noun string) string {
	if count == 1 {
		return fmt.Sprintf("%d %s", count, noun)
	}
	return fmt.Sprintf("%d %ss", count, noun)
}

func promptText(action pendingAction) string {
	switch action.kind {
	case promptStop:
		return stopPrompt(action.name)
	case promptRestart:
		return restartPrompt(action.name)
	case promptDelete:
		return deletePrompt(action.name)
	}
	return ""
}
