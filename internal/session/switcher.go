package session

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/WilberC/herdr-suite/internal/herdrmock"
	"github.com/WilberC/herdr-suite/internal/ui"
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

	width  int
	height int
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
	case tea.WindowSizeMsg:
		s.width, s.height = msg.Width, msg.Height
		return s, nil
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

// View renders the switcher: the shared frame, a header naming what the list
// is sorted by, the live filter, the grouped list, and a footer that details
// the highlighted session and lists the bindings.
func (s *switcher) View() string {
	frame := s.frame()
	width := frame.ContentWidth()

	rows := []string{
		frame.Row(s.headerContent(width)),
		frame.Rule(),
		frame.Row(s.filterContent(width)),
	}

	body := s.bodyLines(width)
	body = window(body, s.bodyHeight())
	for _, line := range body {
		rows = append(rows, frame.Row(line.styled))
	}

	if body := s.statusContent(width); body != "" {
		rows = append(rows, frame.Row(""), frame.Row(body))
	}
	rows = append(rows, frame.Rule(), frame.Row(s.detailContent(width)))
	if s.pending != nil {
		rows = append(rows, frame.Row(s.pendingContent()), frame.Row(s.pendingHints()))
	} else {
		rows = append(rows, frame.Row(s.hintsContent(width)))
	}

	windowWidth, windowHeight := s.window()
	return ui.Float(ui.Layout(frame, rows...), windowWidth, windowHeight)
}

const (
	// switcherDefaultWidth and switcherDefaultHeight are what a view renders
	// for before the terminal has reported its size.
	switcherDefaultWidth  = 100
	switcherDefaultHeight = 30
	// switcherMaxWidth keeps the surface a popup on a wide terminal instead of
	// a row of text lost in the middle of the screen.
	switcherMaxWidth = 100
	// switcherChrome is every frame line that is not a list row: the two
	// borders, header, two rules, filter, detail and hints.
	switcherChrome = 8
)

// window is the size the view renders for: the reported size, or the default
// before the first report.
func (s *switcher) window() (int, int) {
	width, height := s.width, s.height
	if width <= 0 {
		width = switcherDefaultWidth
	}
	if height <= 0 {
		height = switcherDefaultHeight
	}
	return width, height
}

// frame is the surface every state of the switcher renders inside.
func (s *switcher) frame() ui.Frame {
	width, _ := s.window()
	title := ui.GoldBold.Render("hs") + ui.Quiet.Render(" ❯ ") + ui.AccentBold.Render("sessions")
	return ui.Frame{Title: title, Width: min(width, switcherMaxWidth)}
}

// bodyHeight is how many list rows fit under the chrome, leaving room for the
// status line and the confirmation when they are up.
func (s *switcher) bodyHeight() int {
	_, height := s.window()
	reserved := switcherChrome
	if s.status != "" {
		reserved += 2
	}
	if s.pending != nil {
		reserved += 2
	}
	return max(1, height-reserved)
}

// headerContent names what the list is sorted by on the left and counts the
// states it holds on the right, so the header answers "what am I looking at".
func (s *switcher) headerContent(width int) string {
	running, stopped := 0, 0
	for _, session := range s.sessions {
		if session.Running {
			running++
		} else {
			stopped++
		}
	}

	left := ui.Label("sort", "running first", ui.AccentBold)
	right := ui.Muted.Render("esc to close")
	if len(s.sessions) > 0 {
		right = ui.Green.Render(ui.Live) + " " + ui.GreenBold.Render(itoa(running)) + " " + ui.Muted.Render("running") +
			ui.Dot() + ui.Muted.Render(ui.Idle) + " " + ui.Muted.Render(itoa(stopped)) + " " + ui.Muted.Render("stopped") +
			ui.Dot() + right
	}
	return ui.Join(left, right, width)
}

// filterContent is the live filter field: the query on the left with the block
// cursor, how much of the list it leaves on the right.
func (s *switcher) filterContent(width int) string {
	left := ui.Accent.Render(FilterCue) + " "
	if s.query == "" {
		left += ui.Muted.Render("type a session name to filter…")
	} else {
		left += ui.Text.Render(s.query) + ui.Accent.Render(ui.Cursor)
	}
	if len(s.sessions) == 0 {
		return ui.Join(left, "", width)
	}
	right := ui.Muted.Render(itoa(len(s.matches)) + " / " + itoa(len(s.sessions)) + " shown")
	return ui.Join(left, right, width)
}

// bodyLine is one list row: the match it belongs to, and whether it is the
// highlighted one. A header, a workspace and a placeholder row belong to no
// match.
type bodyLine struct {
	match    int
	styled   string
	selected bool
}

// bodyLines renders the grouped list: a header per state, one row per session,
// and the highlighted session's workspaces nested under it.
func (s *switcher) bodyLines(width int) []bodyLine {
	if len(s.matches) == 0 {
		if s.query == "" {
			return s.placeholder("no sessions yet", "type a name, then press enter to create it")
		}
		hint := "esc to quit"
		if name, err := Sanitize(s.query); err == nil {
			hint = "enter creates '" + name + "' · esc to quit"
		}
		return s.placeholder("nothing matches \""+s.query+"\"", hint)
	}

	nameWidth := max(8, width-24)

	lines := make([]bodyLine, 0, len(s.matches)+6)
	state := -1
	for i, session := range s.matches {
		running := 0
		if session.Running {
			running = 1
		}
		if running != state {
			state = running
			lines = append(lines, bodyLine{match: -1}, bodyLine{match: -1, styled: groupHeader(session.Running, countState(s.matches, running))})
		}

		last := i == len(s.matches)-1 || s.matches[i+1].Running != session.Running
		roles := ui.RowRoles(i == s.cursor)
		body := roles.Name.Render(ui.Clip(session.Name, nameWidth)) + defaultMark(roles, session)
		row := ui.Row(roles, last, body)
		if i == s.cursor {
			row = ui.SelectedRow(row, width)
		}
		lines = append(lines, bodyLine{match: i, selected: i == s.cursor, styled: row})

		if i != s.cursor {
			continue
		}
		// The children hang under their session: the continuation column keeps
		// the parent's guide open, or leaves it blank when the session closes
		// its group, so a child always starts three cells past its parent.
		continuation := "  " + " " + "  "
		if !last {
			continuation = "  " + ui.Quiet.Render("│") + "  "
		}
		lines = append(lines, s.workspaceLines(continuation, width)...)
	}
	return lines
}

// countState counts the matches in one state group.
func countState(matches []herdrmock.Session, running int) int {
	count := 0
	for _, session := range matches {
		if (session.Running && running == 1) || (!session.Running && running == 0) {
			count++
		}
	}
	return count
}

// groupHeader is a section row: the state glyph, the state in small caps and
// how many sessions it holds.
func groupHeader(running bool, count int) string {
	label, glyph := "STOPPED", ui.Idle
	style, glyphStyle := ui.Muted, ui.Muted
	if running {
		label, glyph = "RUNNING", ui.Live
		style, glyphStyle = ui.GreenBold, ui.Green
	}
	return glyphStyle.Render(glyph) + " " + style.Render(label) + "  " + ui.Muted.Render(itoa(count))
}

// defaultMark is the gold marker of the default session: it outlives the
// selection, so it keeps its own colour on a tinted row too.
func defaultMark(roles ui.Roles, session herdrmock.Session) string {
	if !session.Default {
		return ""
	}
	return roles.Gap(2) + roles.Gold.Render(ui.Mark)
}

// workspaceLines renders the highlighted session's workspaces as the rows
// nested under it, with the counts and the agent state in fixed columns.
func (s *switcher) workspaceLines(continuation string, width int) []bodyLine {
	rows := make([]bodyLine, 0, len(s.previewWorkspaces)+1)
	guide := func(last bool) string {
		return continuation + ui.Guide(ui.Quiet, last) + " "
	}
	line := func(last bool, content string) bodyLine {
		return bodyLine{match: -1, styled: guide(last) + content}
	}

	session, ok := s.highlighted()
	if !ok || s.preview == nil {
		return nil
	}
	if !session.Running {
		return []bodyLine{line(true, ui.Muted.Render("stopped")+ui.Dot()+ui.Muted.Render("enter starts it again"))}
	}
	if !s.previewDone {
		return []bodyLine{line(true, ui.Spinner(0)+" "+ui.Muted.Render("loading workspaces…"))}
	}
	if s.previewErr != nil {
		return []bodyLine{line(true, ui.Red.Render("unavailable"))}
	}
	if len(s.previewWorkspaces) == 0 {
		return []bodyLine{line(true, ui.Muted.Render("no workspaces yet"))}
	}

	labelWidth, countWidth := 0, 0
	counts := make([]string, len(s.previewWorkspaces))
	for i, workspace := range s.previewWorkspaces {
		labelWidth = max(labelWidth, ui.Width(workspace.Label))
		counts[i] = countLabel(workspace.Tabs, "tab") + " · " + countLabel(workspace.Panes, "pane")
		countWidth = max(countWidth, ui.Width(counts[i]))
	}
	labelWidth = min(labelWidth, max(8, width/3))

	for i, workspace := range s.previewWorkspaces {
		content := ui.Fit(ui.Text.Render(workspace.Label), labelWidth)
		content += "  " + ui.Muted.Render(ui.Fit(counts[i], countWidth))
		if agent := ui.RowRoles(false).AgentState(workspace.Agent); agent != "" {
			content += "  " + agent
		}
		rows = append(rows, line(i == len(s.previewWorkspaces)-1, content))
	}
	return rows
}

// placeholder centers a calm message in the body: the state on the first line,
// what to do next on the second.
func (s *switcher) placeholder(message, hint string) []bodyLine {
	lines := s.frame().Placeholder(message, hint)
	rows := make([]bodyLine, 0, len(lines))
	for _, line := range lines {
		rows = append(rows, bodyLine{match: -1, styled: line})
	}
	return rows
}

// window keeps the highlighted row in view when the list is longer than the
// body, centering the visible slice on it.
func window(lines []bodyLine, height int) []bodyLine {
	if len(lines) <= height {
		return lines
	}
	cursor := 0
	for i, line := range lines {
		if line.selected {
			cursor = i
			break
		}
	}
	start := cursor - height/2
	start = max(0, min(start, len(lines)-height))
	return lines[start : start+height]
}

// statusContent is the one-line result of the last action.
func (s *switcher) statusContent(width int) string {
	if s.status == "" {
		return ""
	}
	return ui.Teal.Render("›") + " " + ui.Text.Render(s.status)
}

// detailContent names the highlighted session and its facts: state, the default
// marker, the socket path, and how many workspaces it holds when the preview is
// in. The path is the first thing a narrow window drops.
func (s *switcher) detailContent(width int) string {
	session, ok := s.highlighted()
	if !ok {
		if len(s.sessions) == 0 {
			return ui.Muted.Render("no session to attach")
		}
		return ui.Muted.Render("no match to attach")
	}

	left := ui.AccentBold.Render(session.Name) + " " + ui.StateGlyph(session.Running) + " " + ui.StateWord(session.Running)
	if session.Default {
		left += ui.Dot() + ui.Gold.Render(ui.Mark+" default")
	}

	right := ""
	if session.Running && s.previewDone && s.previewErr == nil && len(s.previewWorkspaces) > 0 {
		noun := "workspaces"
		if len(s.previewWorkspaces) == 1 {
			noun = "workspace"
		}
		right = ui.TealBold.Render(itoa(len(s.previewWorkspaces))) + " " + ui.Muted.Render(noun)
	}

	path := ui.ShortPath(session.Socket)
	room := width - ui.Width(left) - ui.Width(right) - ui.Width(ui.Dot()) - 2
	switch {
	case path == "":
		return ui.Footer(left, right, width)
	case ui.Width(path) <= room:
		return ui.Footer(left+ui.Dot()+ui.Muted.Render(path), right, width)
	case room >= 14:
		return ui.Footer(left+ui.Dot()+ui.Muted.Render(ui.EllipsisPath(path, room)), right, width)
	default:
		return ui.Footer(left, right, width)
	}
}

// pendingContent is the confirmation a destructive binding opened, worded by the
// manager so the model and the CLI cannot drift.
func (s *switcher) pendingContent() string {
	return ui.RedBold.Render(promptText(*s.pending) + " [y/N]")
}

// pendingHints answers the confirmation.
func (s *switcher) pendingHints() string {
	return ui.Chip(ui.ChipGold, "y") + " " + ui.Muted.Render("yes") + "  " +
		ui.Chip(ui.ChipText, "n") + " " + ui.Muted.Render("no, keep it")
}

// hintsContent is the binding bar: the keys that act on the list, in the order
// a user reaches for them. A narrow window drops the last ones instead of
// wrapping.
func (s *switcher) hintsContent(width int) string {
	return ui.Hints(width,
		ui.Hint{Key: "↑↓", Label: "move"},
		ui.Hint{Key: "enter", Label: "attach"},
		ui.Hint{Key: "alt+enter", Label: "create"},
		ui.Hint{Key: "ctrl+r", Label: "restart"},
		ui.Hint{Key: "ctrl+s", Label: "stop"},
		ui.Hint{Key: "ctrl+x", Label: "delete"},
	)
}

// FilterCue opens the filter field: the glyph the font can always draw, in the
// accent the field takes when it holds a query.
const FilterCue = "›"

// itoa is the small integer formatter the view uses; the CLI keeps its own.
func itoa(value int) string {
	return strconv.Itoa(value)
}

// countLabel names a count with the noun agreeing with it.
func countLabel(count int, noun string) string {
	if count == 1 {
		return itoa(count) + " " + noun
	}
	return itoa(count) + " " + noun + "s"
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
