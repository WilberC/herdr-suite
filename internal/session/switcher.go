package session

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/WilberC/herdr-suite/internal/herdrmock"
	"github.com/WilberC/tui-kit/chip"
	"github.com/WilberC/tui-kit/frame"
	"github.com/WilberC/tui-kit/list"
	"github.com/WilberC/tui-kit/spinner"
	"github.com/WilberC/tui-kit/text"
	"github.com/WilberC/tui-kit/theme"
	"github.com/WilberC/tui-kit/zone"
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

// Run shows the switcher until the user leaves it. The program reports every
// motion, not only the motion between two presses, because hover needs it: herdr
// forwards a moved event to a popup pane whose program enabled mouse reporting
// (src/client/shell/mouse.rs "MouseEventKind::Moved if hit.mouse_reporting"), and
// its host terminal is in any-event tracking.
func (s *Switcher) Run(in io.Reader, out io.Writer) error {
	_, err := tea.NewProgram(s.model, programOptions(in, out)...).Run()
	return err
}

// programOptions is what the switcher asks the terminal for: the streams the
// caller handed in, and all-motion mouse reporting for the hover.
func programOptions(in io.Reader, out io.Writer) []tea.ProgramOption {
	options := []tea.ProgramOption{tea.WithOutput(out), tea.WithMouseAllMotion()}
	if in != nil {
		options = append(options, tea.WithInput(in))
	}
	return options
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

	bar chip.HintBar

	// hover is the match the pointer is over, counted from one so the zero value
	// means the pointer is over no match at all. zones holds where this render
	// drew each clickable element; the kit rewrites it into pane cells.
	hover int
	zones *zone.Map
	// clicks recognises a double click, and now is the clock it reads: a test
	// drives the clock instead of waiting.
	clicks Clicks
	now    func() time.Time

	width  int
	height int
}

func newSwitcher(config SwitcherConfig) *switcher {
	return &switcher{
		manager:  config.Manager,
		flagPath: config.RestartFlag,
		preview:  config.Preview,
		zones:    zone.New(),
		now:      time.Now,
	}
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
	case tea.MouseMsg:
		return s, s.handleMouse(msg)
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
	// The paging key comes first: a pending confirmation is answered by y or n
	// and the filter only takes runes, so the key reaches neither and pages the
	// hints instead.
	if key.String() == chip.PageKey {
		s.bar.Next(s.surface().Frame.ContentWidth(), s.hints()...)
		return nil
	}

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
	// The rows moved under the pointer, so no row is hovered until the pointer
	// moves again: a stale index would tint a row the pointer never reached.
	s.hover = 0
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

// View renders the switcher: the shared surface, a header naming what the list
// is sorted by, the live filter, the grouped list, and a footer pinned to the
// bottom that details the highlighted session and lists the bindings.
//
// The render also records where it drew every clickable element: one zone per
// session row and, under it, the rows of its preview, which act on the session
// they hang under. The zones are rewritten into pane cells by the surface, so a
// click is answered from the cell alone.
func (s *switcher) View() string {
	surface := s.surface()
	panel := surface.Frame
	width, height := s.window()
	inner := panel.ContentWidth()

	surface.Top = []string{
		surface.Row(width, s.headerContent(inner)),
		panel.Rule(),
		surface.Row(width, s.filterContent(inner)),
	}

	bottom := make([]string, 0, 7)
	if status := s.statusContent(inner); status != "" {
		bottom = append(bottom, surface.Row(width, ""), surface.Row(width, status))
	}
	bottom = append(bottom, panel.Rule(), surface.Row(width, s.detailContent(inner)))
	if s.pending != nil {
		bottom = append(bottom, surface.Row(width, s.pendingContent()))
	}
	// The hint bar is the last inner row, whatever the state added above it, so
	// its placeholder is what tells BottomY where its chips land.
	bottom = append(bottom, surface.Row(width, ""))
	surface.Bottom = bottom

	zones := s.frameZones()
	hintLine := len(bottom) - 1
	bottom[hintLine] = surface.Row(width, s.bar.RenderZones(zones, 0, surface.BottomY(height, hintLine), inner, s.hints()...))

	rows := windowRows(s.bodyRows(inner), surface.BodyRows(height))
	nameWidth := max(8, inner-24)
	lines := make([]string, len(rows))
	for i, row := range rows {
		lines[i] = surface.Row(width, s.paintRow(row, inner, nameWidth))
		s.recordRow(zones, row, surface, i)
	}
	return surface.PopupZones(width, height, lines, zones)
}

// frameZones is the map this render records into, emptied so a click is never
// answered from a frame that is no longer on screen.
func (s *switcher) frameZones() *zone.Map {
	if s.zones == nil {
		s.zones = zone.New()
	}
	s.zones.Reset()
	return s.zones
}

// recordRow registers the cells of one drawn body row: a session row under its
// own id, and a row nested under a session as that session's child, so a click on
// a workspace selects the session it belongs to. A row the body had no room for
// is not drawn, so it is not registered and cannot be clicked.
func (s *switcher) recordRow(zones *zone.Map, row bodyRow, surface frame.Surface, line int) {
	if row.match < 0 {
		return
	}
	id := RowZone(row.match)
	if row.session == nil {
		id = ChildZone(row.match)
	}
	zones.Add(id, 0, surface.BodyY(line), surface.Frame.ContentWidth(), 1)
}

// handleMouse is the mouse half of S8: a press selects the row it landed on, a
// second press on the same row inside DoubleClickWindow runs that row's default
// action, the wheel moves the selection, motion hovers, and a click on the paging
// chip is chip.PageKey. No destructive action has a click path: stop, restart and
// delete stay on their keys and keep their confirmation.
func (s *switcher) handleMouse(mouse tea.MouseMsg) tea.Cmd {
	switch mouse.Action {
	case tea.MouseActionMotion:
		s.hover = s.matchAt(mouse.X, mouse.Y) + 1
		return nil
	case tea.MouseActionPress:
	default:
		return nil
	}

	switch mouse.Button {
	case tea.MouseButtonWheelUp, tea.MouseButtonWheelDown:
		if s.pending != nil {
			return nil
		}
		delta := 1
		if mouse.Button == tea.MouseButtonWheelUp {
			delta = -1
		}
		return s.moveSelection(delta)
	case tea.MouseButtonLeft:
	default:
		return nil
	}

	id, ok := s.zones.Hit(mouse.X, mouse.Y)
	if !ok {
		return nil
	}
	if id == chip.PageZone {
		s.bar.Next(s.surface().Frame.ContentWidth(), s.hints()...)
		return nil
	}
	// A pending confirmation belongs to the keyboard: y or n answers it, and a
	// click neither answers it nor moves what it is about.
	if s.pending != nil {
		return nil
	}
	index, ok := RowZoneIndex(id)
	if !ok {
		return nil
	}

	s.hover = index + 1
	s.cursor = index
	if s.clicks.Double(id, s.clock()) {
		return s.pick(false)
	}
	return s.previewCmd()
}

// matchAt is the match a click at (x, y) landed on, or -1 for a cell that is not
// a row.
func (s *switcher) matchAt(x, y int) int {
	id, ok := s.zones.Hit(x, y)
	if !ok {
		return -1
	}
	index, ok := RowZoneIndex(id)
	if !ok {
		return -1
	}
	return index
}

// moveSelection moves the highlight by delta rows and stops at both ends, which is
// what the wheel and the cursor keys share.
func (s *switcher) moveSelection(delta int) tea.Cmd {
	next := s.cursor + delta
	if next < 0 || next >= len(s.matches) || next == s.cursor {
		return nil
	}
	s.cursor = next
	return s.previewCmd()
}

// clock is the time a press happened, from the clock the caller installed.
func (s *switcher) clock() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

const (
	// switcherDefaultWidth and switcherDefaultHeight are what a view renders
	// for before the terminal has reported its size.
	switcherDefaultWidth  = 100
	switcherDefaultHeight = 30
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

// surface is the screen every state of the switcher renders inside: the shared
// frame within the kit's recommended bounds, its width already clamped so the
// rows can be composed against it.
func (s *switcher) surface() frame.Surface {
	width, _ := s.window()
	title := theme.GoldBold.Render("hs") + theme.Quiet.Render(" ❯ ") + theme.AccentBold.Render("sessions")
	surface := frame.Surface{Frame: frame.Frame{Title: title}, Size: frame.Recommended}
	surface.Frame.Width = surface.Width(width)
	return surface
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

	left := theme.Label("sort", "running first", theme.AccentBold)
	right := theme.Muted.Render("esc to close")
	if len(s.sessions) > 0 {
		right = theme.Green.Render(text.Live) + " " + theme.GreenBold.Render(itoa(running)) + " " + theme.Muted.Render("running") +
			theme.Dot() + theme.Muted.Render(text.Idle) + " " + theme.Muted.Render(itoa(stopped)) + " " + theme.Muted.Render("stopped") +
			theme.Dot() + right
	}
	return text.Join(left, right, width)
}

// filterContent is the live filter field: the query on the left with the block
// cursor, how much of the list it leaves on the right.
func (s *switcher) filterContent(width int) string {
	left := theme.Accent.Render(FilterCue) + " "
	if s.query == "" {
		left += theme.Muted.Render("type a session name to filter…")
	} else {
		left += theme.Text.Render(s.query) + theme.Accent.Render(text.Cursor)
	}
	if len(s.sessions) == 0 {
		return text.Join(left, "", width)
	}
	right := theme.Muted.Render(itoa(len(s.matches)) + " / " + itoa(len(s.sessions)) + " shown")
	return text.Join(left, right, width)
}

// bodyRow is one row of the list before it is painted: the match a click on it
// selects (-1 for a row that selects nothing), the session whose name it draws
// (nil for a row that is already styled), the guide its place in the group asks
// for, and the content of a row that is not a session.
type bodyRow struct {
	match    int
	selected bool
	session  *herdrmock.Session
	last     bool
	content  string
}

// bodyRows renders the grouped list: a header per state, one row per session,
// and the highlighted session's workspaces nested under it. A row records what it
// selects instead of its final colours, because those depend on whether the
// pointer is over it this frame.
func (s *switcher) bodyRows(width int) []bodyRow {
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

	rows := make([]bodyRow, 0, len(s.matches)+6)
	state := -1
	for i, session := range s.matches {
		running := 0
		if session.Running {
			running = 1
		}
		if running != state {
			state = running
			// The body opens with its own blank row, so only the groups after the
			// first one get a separator row.
			if len(rows) > 0 {
				rows = append(rows, bodyRow{match: -1})
			}
			rows = append(rows, bodyRow{match: -1, content: groupHeader(session.Running, countState(s.matches, running))})
		}

		last := i == len(s.matches)-1 || s.matches[i+1].Running != session.Running
		session := session
		rows = append(rows, bodyRow{match: i, selected: i == s.cursor, session: &session, last: last})

		if i != s.cursor {
			continue
		}
		// The children hang under their session: the continuation column keeps
		// the parent's guide open, or leaves it blank when the session closes
		// its group, so a child always starts three cells past its parent.
		continuation := "  " + " " + "  "
		if !last {
			continuation = "  " + theme.Quiet.Render("│") + "  "
		}
		// A nested row is the session's detail, so a click on it acts on the
		// session it belongs to.
		for _, child := range s.workspaceLines(continuation, width) {
			child.match = i
			rows = append(rows, child)
		}
	}
	return rows
}

// paintRow draws one body row with the roles its state asks for: the selected row
// keeps the selection's tint, the row under the pointer wears the fainter one, and
// the selection wins where both apply. Only a session row can wear either, because
// the rows nested under it are facts about it, not targets of their own.
func (s *switcher) paintRow(row bodyRow, width, nameWidth int) string {
	roles := list.RowRolesFor(list.State{
		Selected: row.selected,
		Hovered:  row.session != nil && !row.selected && row.match+1 == s.hover,
	})
	if row.session == nil {
		if roles.Selected {
			return list.SelectedRow(row.content, width)
		}
		if roles.Hovered {
			return roles.Pad(row.content, width)
		}
		return row.content
	}

	body := roles.Name.Render(text.Clip(row.session.Name, nameWidth)) + defaultMark(roles, *row.session)
	line := list.Row(roles, row.last, body)
	switch {
	case roles.Selected:
		return list.SelectedRow(line, width)
	case roles.Hovered:
		return roles.Pad(line, width)
	}
	return line
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
	label, glyph := "STOPPED", text.Idle
	style, glyphStyle := theme.Muted, theme.Muted
	if running {
		label, glyph = "RUNNING", text.Live
		style, glyphStyle = theme.GreenBold, theme.Green
	}
	return glyphStyle.Render(glyph) + " " + style.Render(label) + "  " + theme.Muted.Render(itoa(count))
}

// defaultMark is the gold marker of the default session: it outlives the
// selection, so it keeps its own colour on a tinted or hovered row too.
func defaultMark(roles list.Roles, session herdrmock.Session) string {
	if !session.Default {
		return ""
	}
	return list.Mark(roles)
}

// workspaceLines renders the highlighted session's workspaces as the rows
// nested under it, with the counts and the agent state in fixed columns.
func (s *switcher) workspaceLines(continuation string, width int) []bodyRow {
	rows := make([]bodyRow, 0, len(s.previewWorkspaces)+1)
	guide := func(last bool) string {
		return continuation + list.Guide(theme.Quiet, last) + " "
	}
	line := func(last bool, content string) bodyRow {
		return bodyRow{match: -1, content: guide(last) + content}
	}

	session, ok := s.highlighted()
	if !ok || s.preview == nil {
		return nil
	}
	if !session.Running {
		return []bodyRow{line(true, theme.Muted.Render("stopped")+theme.Dot()+theme.Muted.Render("enter starts it again"))}
	}
	if !s.previewDone {
		return []bodyRow{line(true, spinner.Render(0)+" "+theme.Muted.Render("loading workspaces…"))}
	}
	if s.previewErr != nil {
		return []bodyRow{line(true, theme.Red.Render("unavailable"))}
	}
	if len(s.previewWorkspaces) == 0 {
		return []bodyRow{line(true, theme.Muted.Render("no workspaces yet"))}
	}

	labelWidth, countWidth := 0, 0
	counts := make([]string, len(s.previewWorkspaces))
	for i, workspace := range s.previewWorkspaces {
		labelWidth = max(labelWidth, text.Width(workspace.Label))
		counts[i] = countLabel(workspace.Tabs, "tab") + " · " + countLabel(workspace.Panes, "pane")
		countWidth = max(countWidth, text.Width(counts[i]))
	}
	labelWidth = min(labelWidth, max(8, width/3))

	for i, workspace := range s.previewWorkspaces {
		content := text.Fit(theme.Text.Render(workspace.Label), labelWidth)
		content += "  " + theme.Muted.Render(text.Fit(counts[i], countWidth))
		if agent := list.RowRoles(false).AgentState(workspace.Agent); agent != "" {
			content += "  " + agent
		}
		rows = append(rows, line(i == len(s.previewWorkspaces)-1, content))
	}
	return rows
}

// placeholder centers a calm message in the body: the state on the first line,
// what to do next on the second. The frame pads the body at both ends already,
// so the placeholder's own blank edges are dropped rather than doubled.
func (s *switcher) placeholder(message, hint string) []bodyRow {
	lines := s.surface().Frame.Placeholder(message, hint)
	lines = lines[1 : len(lines)-1]
	rows := make([]bodyRow, 0, len(lines))
	for _, line := range lines {
		rows = append(rows, bodyRow{match: -1, content: line})
	}
	return rows
}

// windowRows keeps the highlighted row in view when the list is longer than the
// body, centering the visible slice on it. The slice is the body the surface
// draws, so the y of a row is its index in this slice.
func windowRows(rows []bodyRow, height int) []bodyRow {
	if len(rows) <= height {
		return rows
	}
	cursor := 0
	for i, row := range rows {
		if row.selected {
			cursor = i
			break
		}
	}
	start := max(0, min(cursor-height/2, len(rows)-height))
	return rows[start : start+height]
}

// statusContent is the one-line result of the last action.
func (s *switcher) statusContent(width int) string {
	if s.status == "" {
		return ""
	}
	return theme.Teal.Render("›") + " " + theme.Text.Render(s.status)
}

// detailContent names the highlighted session and its facts: state, the default
// marker, the socket path, and how many workspaces it holds when the preview is
// in. The path is the first thing a narrow window drops.
func (s *switcher) detailContent(width int) string {
	session, ok := s.highlighted()
	if !ok {
		if len(s.sessions) == 0 {
			return theme.Muted.Render("no session to attach")
		}
		return theme.Muted.Render("no match to attach")
	}

	left := theme.AccentBold.Render(session.Name) + " " + list.StateGlyph(session.Running) + " " + list.StateWord(session.Running)
	if session.Default {
		left += theme.Dot() + theme.Gold.Render(text.Mark+" default")
	}

	right := ""
	if session.Running && s.previewDone && s.previewErr == nil && len(s.previewWorkspaces) > 0 {
		noun := "workspaces"
		if len(s.previewWorkspaces) == 1 {
			noun = "workspace"
		}
		right = theme.TealBold.Render(itoa(len(s.previewWorkspaces))) + " " + theme.Muted.Render(noun)
	}

	path := text.ShortPath(session.Socket)
	room := width - text.Width(left) - text.Width(right) - text.Width(theme.Dot()) - 2
	switch {
	case path == "":
		return frame.Footer(left, right, width)
	case text.Width(path) <= room:
		return frame.Footer(left+theme.Dot()+theme.Muted.Render(path), right, width)
	case room >= 14:
		return frame.Footer(left+theme.Dot()+theme.Muted.Render(text.EllipsisPath(path, room)), right, width)
	default:
		return frame.Footer(left, right, width)
	}
}

// pendingContent is the confirmation a destructive binding opened, worded by the
// manager so the model and the CLI cannot drift.
func (s *switcher) pendingContent() string {
	return theme.RedBold.Render(promptText(*s.pending) + " [y/N]")
}

// hints is the binding bar of the current state: the two answers a pending
// confirmation takes, or the keys that act on the list in the order a user
// reaches for them. A narrow window pages the bar instead of dropping a hint.
// The confirmation's yes keeps its gold slab, so the destructive answer reads as
// the one that acts while its neighbour stays the default surface.
func (s *switcher) hints() []chip.Hint {
	if s.pending != nil {
		return []chip.Hint{
			{Key: "y", Label: "yes", Slab: &theme.ChipGold},
			{Key: "n", Label: "no, keep it"},
		}
	}
	return []chip.Hint{
		{Key: "↑↓", Label: "move"},
		{Key: "enter", Label: "attach"},
		{Key: "alt+enter", Label: "create"},
		{Key: "ctrl+r", Label: "restart"},
		{Key: "ctrl+s", Label: "stop"},
		{Key: "ctrl+x", Label: "delete"},
	}
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
