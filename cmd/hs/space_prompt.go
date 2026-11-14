package main

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/WilberC/herdr-suite/internal/herdrmock"
	"github.com/WilberC/herdr-suite/internal/layout"
	"github.com/WilberC/herdr-suite/internal/session"
	"github.com/WilberC/tui-kit/chip"
	"github.com/WilberC/tui-kit/frame"
	"github.com/WilberC/tui-kit/list"
	"github.com/WilberC/tui-kit/spinner"
	"github.com/WilberC/tui-kit/text"
	"github.com/WilberC/tui-kit/theme"
	tea "github.com/charmbracelet/bubbletea"
)

// pickWorkspace asks which existing workspace a layout should replay into. It is
// the interactive half of "hs space load": the command refuses to guess when the
// stored workspace is not the active one.
func pickWorkspace(in io.Reader, out io.Writer, workspaces []herdrmock.Workspace) (string, error) {
	model := &workspacePicker{workspaces: workspaces}
	options := []tea.ProgramOption{tea.WithOutput(out)}
	if in != nil {
		options = append(options, tea.WithInput(in))
	}
	if _, err := tea.NewProgram(model, options...).Run(); err != nil {
		return "", err
	}
	if model.cancelled {
		return "", session.ErrCancelled
	}
	return model.chosen, nil
}

// workspacePicker is the workspace selection list.
type workspacePicker struct {
	workspaces []herdrmock.Workspace
	cursor     int
	chosen     string
	cancelled  bool
	bar        chip.HintBar
	barKey     string
	width      int
	height     int
}

func (p *workspacePicker) Init() tea.Cmd { return nil }

func (p *workspacePicker) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		p.width, p.height = size.Width, size.Height
		return p, nil
	}
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return p, nil
	}
	// The paging key comes before the bindings: the prompt reads no filter, so
	// tab only moves the hint bar.
	if key.String() == chip.PageKey {
		p.bar.Next(p.surface().Frame.ContentWidth(), p.syncBar()...)
		return p, nil
	}
	switch key.String() {
	case "ctrl+c", "esc":
		p.cancelled = true
		return p, tea.Quit
	case "up", "ctrl+k":
		if p.cursor > 0 {
			p.cursor--
		}
	case "down", "ctrl+j":
		if p.cursor+1 < len(p.workspaces) {
			p.cursor++
		}
	case "enter":
		if p.cursor < len(p.workspaces) {
			p.chosen = p.workspaces[p.cursor].ID
		}
		return p, tea.Quit
	}
	return p, nil
}

// surface is the prompt inside the kit's recommended bounds, its width already
// clamped so the rows can be composed against it.
func (p *workspacePicker) surface() frame.Surface {
	width, _ := pickerWindow(p.width, p.height)
	return spaceSurface(width)
}

// View renders the workspace prompt inside the shared surface: the question at
// the top, the workspaces as aligned rows in the body, and the footer pinned to
// the bottom.
func (p *workspacePicker) View() string {
	width, height := pickerWindow(p.width, p.height)
	surface := spaceSurface(width)
	panel := surface.Frame
	inner := panel.ContentWidth()

	right := theme.Muted.Render("esc to cancel")
	if len(p.workspaces) > 0 {
		right = theme.Muted.Render(countLabel(len(p.workspaces), "workspace") + " · esc to cancel")
	}
	surface.Top = []string{
		panel.Row(text.Join(
			theme.Label("load", "into which workspace?", theme.AccentBold),
			right,
			inner,
		)),
		panel.Rule(),
	}
	surface.Bottom = []string{
		panel.Rule(),
		panel.Row(p.detailContent(inner)),
		panel.Row(p.bar.Render(inner, p.syncBar()...)),
	}

	rows := p.bodyRows(panel)
	visible := pickerWindowRows(rows, p.cursor, surface.BodyRows(height))
	body := make([]string, len(visible))
	for i, row := range visible {
		body[i] = panel.Row(row)
	}
	return surface.Popup(width, height, body)
}

// bodyRows renders the workspaces with their counts and agent state in fixed
// columns, the highlighted one as a solid bar.
func (p *workspacePicker) bodyRows(surface frame.Frame) []string {
	if len(p.workspaces) == 0 {
		return placeholderRows(surface, "no workspaces to load into", "open one in herdr, then run this again")
	}

	labelWidth, countWidth := 0, 0
	counts := make([]string, len(p.workspaces))
	for i, workspace := range p.workspaces {
		labelWidth = max(labelWidth, text.Width(workspace.Label))
		counts[i] = countLabel(workspace.TabCount, "tab") + " · " + countLabel(workspace.PaneCount, "pane")
		countWidth = max(countWidth, text.Width(counts[i]))
	}
	labelWidth = min(labelWidth, max(8, surface.ContentWidth()/3))

	rows := make([]string, 0, len(p.workspaces))
	for i, workspace := range p.workspaces {
		roles := list.RowRoles(i == p.cursor)
		content := roles.Pad(roles.Name.Render(workspace.Label), labelWidth)
		content += roles.Gap(2) + roles.Pad(roles.Muted.Render(counts[i]), countWidth)
		if agent := roles.AgentState(workspace.AgentState); agent != "" {
			content += roles.Gap(2) + roles.Pad(agent, 9)
		}
		if workspace.Focused {
			content += roles.Gap(2) + roles.Gold.Render(text.Mark)
		}
		row := list.Row(roles, i == len(p.workspaces)-1, content)
		if i == p.cursor {
			row = list.SelectedRow(row, surface.ContentWidth())
		}
		rows = append(rows, row)
	}
	return rows
}

// detailContent names the highlighted workspace and its facts.
func (p *workspacePicker) detailContent(width int) string {
	if len(p.workspaces) == 0 || p.cursor < 0 || p.cursor >= len(p.workspaces) {
		return theme.Muted.Render("no workspace to load into")
	}
	workspace := p.workspaces[p.cursor]
	left := theme.AccentBold.Render(workspace.Label) +
		theme.Dot() + theme.Text.Render(countLabel(workspace.TabCount, "tab")) +
		theme.Dot() + theme.Text.Render(countLabel(workspace.PaneCount, "pane"))
	if agent := list.RowRoles(false).AgentState(workspace.AgentState); agent != "" {
		left += theme.Dot() + agent
	}
	if workspace.Focused {
		left += theme.Dot() + theme.Gold.Render(text.Mark+" focused")
	}
	return frame.Footer(left, "", width)
}

// hints is the binding bar of the prompt.
func (p *workspacePicker) hints() []chip.Hint {
	return []chip.Hint{
		{Key: "↑↓", Label: "move"},
		{Key: "enter", Label: "load"},
		{Key: "esc", Label: "cancel"},
	}
}

// syncBar returns the hints this state shows and starts the bar at its first
// page whenever the set changed.
func (p *workspacePicker) syncBar() []chip.Hint { return syncHintBar(&p.bar, &p.barKey, p.hints()) }

// layoutRestore replays the layout the user confirmed and reports every step as
// it starts. report may be nil, which restores silently.
type layoutRestore func(entry layout.Entry, report func(layout.Step)) (string, error)

// layoutPick is what the picker reports back: the entry the user confirmed, the
// summary of the replay it ran in-program, and whether a replay ran at all.
type layoutPick struct {
	Entry    layout.Entry
	Summary  string
	Restored bool
}

// layoutPickerFunc is the deps seam for "hs space load --select": it asks which
// entry to load and restores the confirmed one before it returns.
type layoutPickerFunc func(entries []layout.Entry, restore layoutRestore) (layoutPick, error)

// pickerPhase is the picker's state machine: choosing until enter, restoring
// while the replay runs, then done or failed.
type pickerPhase int

const (
	pickerChoosing pickerPhase = iota + 1
	pickerRestoring
	pickerDone
	pickerFailed
)

// restoreStepMsg carries one replay step to the picker, restoreDoneMsg carries
// the end of the replay, pickerTickMsg advances the spinner, and pickerQuitMsg
// closes the brief done state.
type (
	restoreStepMsg struct{ step layout.Step }
	restoreDoneMsg struct {
		summary string
		err     error
	}
	pickerTickMsg struct{}
	pickerQuitMsg struct{}
)

// pickerFrames is the spinner a restoring picker shows: the frame advances on
// its own tick, so a slow step reads as work in progress rather than a hang.
var pickerFrames = spinner.Frames

const (
	// pickerSpinnerDelay is how often the restoring spinner advances.
	pickerSpinnerDelay = 120 * time.Millisecond
	// pickerDonePause is how long the one-line done state stays up.
	pickerDonePause = 900 * time.Millisecond
	// pickerStepBuffer holds the steps a restore reports before the picker
	// reads them, so the replay never waits on the view.
	pickerStepBuffer = 64
)

// pickLayout asks which stored layout a load should replay, running the replay
// inside the program so the picker can show its progress. A nil restore only
// asks: it is the dry-run pick, which has nothing to report.
func pickLayout(in io.Reader, out io.Writer, entries []layout.Entry, restore layoutRestore) (layoutPick, error) {
	model := newLayoutPicker(entries, restore)
	options := []tea.ProgramOption{tea.WithOutput(out)}
	if in != nil {
		options = append(options, tea.WithInput(in))
	}
	if _, err := tea.NewProgram(model, options...).Run(); err != nil {
		return layoutPick{}, err
	}
	return pickResult(model)
}

// newLayoutPicker returns the picker a terminal runs.
func newLayoutPicker(entries []layout.Entry, restore layoutRestore) *layoutPicker {
	return &layoutPicker{entries: entries, restore: restore, phase: pickerChoosing, tickDelay: pickerSpinnerDelay}
}

// layoutPicker is the stored layout selection list and the restoring view that
// follows a confirmation.
type layoutPicker struct {
	entries   []layout.Entry
	cursor    int
	chosen    string
	cancelled bool

	restore layoutRestore
	phase   pickerPhase
	entry   layout.Entry

	steps      []layout.Step
	step       *layout.Step
	frame      int
	summary    string
	restoreErr error
	finished   bool

	bar    chip.HintBar
	barKey string

	tickDelay time.Duration
	stepsCh   chan layout.Step
	doneCh    chan restoreDoneMsg

	width  int
	height int
}

func (p *layoutPicker) Init() tea.Cmd { return nil }

func (p *layoutPicker) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.width, p.height = msg.Width, msg.Height
		return p, nil
	case tea.KeyMsg:
		return p, p.handleKey(msg)
	case pickerTickMsg:
		if p.phase != pickerRestoring {
			return p, nil
		}
		p.frame = (p.frame + 1) % len(pickerFrames)
		return p, p.tick()
	case restoreStepMsg:
		if p.phase != pickerRestoring {
			return p, nil
		}
		p.steps = append(p.steps, msg.step)
		step := msg.step
		p.step = &step
		return p, p.wait()
	case restoreDoneMsg:
		if p.phase != pickerRestoring {
			return p, nil
		}
		p.summary = strings.TrimSpace(msg.summary)
		if msg.err != nil {
			p.restoreErr = msg.err
			p.phase = pickerFailed
			return p, nil
		}
		p.phase = pickerDone
		return p, p.quitLater()
	case pickerQuitMsg:
		p.finished = true
		return p, tea.Quit
	}
	return p, nil
}

// handleKey moves the highlight while choosing, and leaves the picker from the
// restoring, done and failed states. The paging key only moves the hint bar, so
// it is answered before the phase's own bindings and never moves the cursor.
func (p *layoutPicker) handleKey(key tea.KeyMsg) tea.Cmd {
	if p.phase == pickerChoosing && key.String() == chip.PageKey {
		p.bar.Next(p.surface().Frame.ContentWidth(), p.syncBar()...)
		return nil
	}

	switch p.phase {
	case pickerRestoring:
		if key.String() == "ctrl+c" {
			p.cancelled = true
			p.finished = true
			return tea.Quit
		}
		return nil
	case pickerDone, pickerFailed:
		p.finished = true
		return tea.Quit
	}

	switch key.String() {
	case "ctrl+c", "esc":
		p.cancelled = true
		p.finished = true
		return tea.Quit
	case "up", "ctrl+k":
		if p.cursor > 0 {
			p.cursor--
		}
	case "down", "ctrl+j":
		if p.cursor+1 < len(p.entries) {
			p.cursor++
		}
	case "enter":
		return p.confirm()
	}
	return nil
}

// confirm enters the restoring state: the replay runs in the picker, reporting
// each step through a buffered channel so a slow step never blocks it.
func (p *layoutPicker) confirm() tea.Cmd {
	if p.cursor < 0 || p.cursor >= len(p.entries) {
		return nil
	}
	entry := p.entries[p.cursor]
	p.chosen = entry.Path
	p.entry = entry
	if p.restore == nil {
		p.finished = true
		return tea.Quit
	}

	p.phase = pickerRestoring
	steps := make(chan layout.Step, pickerStepBuffer)
	done := make(chan restoreDoneMsg, 1)
	p.stepsCh, p.doneCh = steps, done
	restore := p.restore
	go func() {
		summary, err := restore(entry, func(step layout.Step) { steps <- step })
		close(steps)
		done <- restoreDoneMsg{summary: summary, err: err}
	}()
	return tea.Batch(tea.ClearScreen, p.tick(), p.wait())
}

// wait reads the next report from the restore: a step, or the end of the replay.
func (p *layoutPicker) wait() tea.Cmd {
	steps, done := p.stepsCh, p.doneCh
	return func() tea.Msg {
		if step, ok := <-steps; ok {
			return restoreStepMsg{step: step}
		}
		return <-done
	}
}

// tick advances the spinner while the restore runs.
func (p *layoutPicker) tick() tea.Cmd {
	delay := p.tickDelay
	if delay <= 0 {
		delay = pickerSpinnerDelay
	}
	return tea.Tick(delay, func(time.Time) tea.Msg { return pickerTickMsg{} })
}

// quitLater closes the one-line done state after a brief pause.
func (p *layoutPicker) quitLater() tea.Cmd {
	return tea.Tick(pickerDonePause, func(time.Time) tea.Msg { return pickerQuitMsg{} })
}

// pickResult is what the command reads once the program exits: the confirmed
// entry, the replay summary, and why the restore stopped.
func pickResult(model *layoutPicker) (layoutPick, error) {
	picked := layoutPick{Entry: model.entry, Summary: model.summary, Restored: model.phase == pickerDone}
	if model.cancelled {
		return layoutPick{}, session.ErrCancelled
	}
	if model.restoreErr != nil {
		return picked, model.restoreErr
	}
	return picked, nil
}

// View renders whichever state the picker is in, always inside the shared
// surface: choosing a layout, restoring one, done, or failed.
func (p *layoutPicker) View() string {
	width, height := pickerWindow(p.width, p.height)
	surface := spaceSurface(width)
	switch p.phase {
	case pickerRestoring:
		return p.restoringView(surface, width, height)
	case pickerDone:
		return p.doneView(surface, width, height)
	case pickerFailed:
		return p.failedView(surface, width, height)
	}
	return p.choosingView(surface, width, height)
}

// surface is the picker inside the kit's recommended bounds, its width already
// clamped so the rows can be composed against it.
func (p *layoutPicker) surface() frame.Surface {
	width, _ := pickerWindow(p.width, p.height)
	return spaceSurface(width)
}

func (p *layoutPicker) choosingView(surface frame.Surface, width, height int) string {
	panel := surface.Frame
	inner := panel.ContentWidth()
	right := theme.Muted.Render("esc to cancel")
	if len(p.entries) > 0 {
		right = theme.Muted.Render(fmt.Sprintf("%d %s · esc to cancel", len(p.entries), plural(len(p.entries), "layout")))
	}
	surface.Top = []string{
		panel.Row(text.Join(
			theme.Label("load", "which layout?", theme.AccentBold),
			right,
			inner,
		)),
		panel.Rule(),
	}
	surface.Bottom = []string{
		panel.Rule(),
		panel.Row(p.detailContent(inner)),
		panel.Row(p.bar.Render(inner, p.syncBar()...)),
	}

	rows, cursorAt := p.choosingRows(panel)
	visible := pickerWindowRows(rows, cursorAt, surface.BodyRows(height))
	body := make([]string, len(visible))
	for i, row := range visible {
		body[i] = panel.Row(row)
	}
	return surface.Popup(width, height, body)
}

// choosingRows renders the layouts grouped by the scope they were found in:
// the project's first, then the global store, then explicit paths. The rows keep
// the order the store lists, so the cursor and the action it confirms cannot
// drift from what the CLI would pick. It returns the rows and the row that
// holds the cursor, so a body shorter than the list can keep it in view.
func (p *layoutPicker) choosingRows(panel frame.Frame) ([]string, int) {
	inner := panel.ContentWidth()
	if len(p.entries) == 0 {
		return placeholderRows(panel, "no layouts found", "save one with hs space save <name>"), 0
	}

	nameWidth, workspaceWidth, countWidth := 0, 0, 0
	counts := make([]string, len(p.entries))
	for i, entry := range p.entries {
		nameWidth = max(nameWidth, text.Width(entry.Name))
		workspaceWidth = max(workspaceWidth, text.Width(entry.Workspace))
		counts[i] = countLabel(entry.Tabs, "tab") + " · " + countLabel(entry.Panes, "pane")
		countWidth = max(countWidth, text.Width(counts[i]))
	}
	nameWidth = min(nameWidth, max(8, inner/4))
	workspaceWidth = min(workspaceWidth, max(6, inner/5))

	rows := make([]string, 0, len(p.entries)+3)
	scope, cursorAt := "", 0
	for i, entry := range p.entries {
		if entry.Scope != scope {
			// The body opens with its own blank row, so only the groups after the
			// first one get a separator row, the same rhythm the switcher uses.
			if scope != "" {
				rows = append(rows, "")
			}
			scope = entry.Scope
			rows = append(rows, list.Section(scope, countScope(p.entries, scope)))
		}
		roles := list.RowRoles(i == p.cursor)
		content := roles.Pad(roles.Name.Render(entry.Name), nameWidth)
		content += roles.Gap(2) + roles.Pad(roles.Muted.Render(entry.Workspace), workspaceWidth)
		content += roles.Gap(2) + roles.Pad(roles.Muted.Render(counts[i]), countWidth)

		last := i == len(p.entries)-1 || p.entries[i+1].Scope != entry.Scope
		row := list.Row(roles, last, content)
		if when := text.RelativeTime(time.Now(), entry.Modified); when != "" {
			row = roles.AlignRight(row, roles.Muted.Render(when)+roles.Gap(1), inner)
		}
		if i == p.cursor {
			row = list.SelectedRow(row, inner)
			cursorAt = len(rows)
		}
		rows = append(rows, row)
	}
	return rows, cursorAt
}

// detailContent names the highlighted layout and its facts. The path is the
// first fact a narrow window drops: it is the one thing the columns above
// already implied.
func (p *layoutPicker) detailContent(width int) string {
	if len(p.entries) == 0 || p.cursor < 0 || p.cursor >= len(p.entries) {
		return theme.Muted.Render("no layout to load")
	}
	entry := p.entries[p.cursor]
	left := theme.AccentBold.Render(entry.Name) + " " + chip.Chip(theme.ChipGold, entry.Scope)
	if entry.Workspace != "" {
		left += theme.Dot() + theme.Label("workspace", entry.Workspace, theme.TealBold)
	}
	left += theme.Dot() + theme.Text.Render(countLabel(entry.Tabs, "tab")) +
		theme.Dot() + theme.Text.Render(countLabel(entry.Panes, "pane"))
	if when := text.RelativeTime(time.Now(), entry.Modified); when != "" {
		left += theme.Dot() + theme.Muted.Render(when)
	}

	path := text.ShortPath(entry.Path)
	room := width - text.Width(left) - 2
	switch {
	case path == "":
		return left
	case text.Width(path) <= room:
		return frame.Footer(left, theme.Muted.Render(path), width)
	case room >= 18:
		return frame.Footer(left, theme.Muted.Render(text.EllipsisPath(path, room)), width)
	default:
		return left
	}
}

// hints is the binding bar of the phase the picker is in. A phase with no
// binding of its own returns nil and renders no bar.
func (p *layoutPicker) hints() []chip.Hint {
	switch p.phase {
	case pickerRestoring:
		return []chip.Hint{{Key: "ctrl+c", Label: "stop watching"}}
	case pickerDone:
		return []chip.Hint{{Key: "any key", Label: "close"}}
	case pickerFailed:
		return nil
	}
	return []chip.Hint{
		{Key: "↑↓", Label: "move"},
		{Key: "enter", Label: "load"},
		{Key: "esc", Label: "cancel"},
	}
}

// syncBar returns the hints this state shows and starts the bar at its first
// page whenever the set changed.
func (p *layoutPicker) syncBar() []chip.Hint { return syncHintBar(&p.bar, &p.barKey, p.hints()) }

// restoringView shows the replay as it runs: the spinner and the step on the
// row they replace, the steps already done under them, and the one binding that
// still works while a restore is in flight.
func (p *layoutPicker) restoringView(surface frame.Surface, width, height int) string {
	panel := surface.Frame
	inner := panel.ContentWidth()
	surface.Top = []string{
		panel.Row(text.Join(
			theme.Label("restore", "in progress", theme.TealBold),
			theme.Muted.Render(countLabel(len(p.steps), "step")+" done"),
			inner,
		)),
		panel.Rule(),
	}
	surface.Bottom = []string{
		panel.Rule(),
		panel.Row(p.detailContent(inner)),
		panel.Row(p.bar.Render(inner, p.syncBar()...)),
	}

	describe := "starting"
	if p.step != nil {
		describe = fmt.Sprintf("step %d/%d %s", p.step.Index, p.step.Total, p.step.Describe())
	}
	// The spinner row is the body's first content row; the steps fill the rest.
	body := []string{panel.Row(
		spinner.Render(p.frame) + " " + theme.AccentBold.Render(fmt.Sprintf("restoring '%s'", p.entry.Name)) + theme.Dot() + theme.Text.Render(describe),
	)}
	budget := max(0, surface.BodyRows(height)-len(body))
	done := p.steps
	if len(done) > budget {
		done = done[len(done)-budget:]
	}
	for i, step := range done {
		body = append(body, panel.Row(list.Guide(theme.Quiet, i == len(done)-1)+" "+theme.Green.Render("✓")+" "+theme.Muted.Render(step.Describe())))
	}
	return surface.Popup(width, height, body)
}

// doneView reports the replay that just finished.
func (p *layoutPicker) doneView(surface frame.Surface, width, height int) string {
	panel := surface.Frame
	inner := panel.ContentWidth()
	surface.Top = []string{
		panel.Row(theme.Label("restore", "done", theme.GreenBold)),
		panel.Rule(),
	}
	surface.Bottom = []string{
		panel.Rule(),
		panel.Row(p.bar.Render(inner, p.syncBar()...)),
	}
	body := []string{panel.Row(text.Center(
		theme.GreenBold.Render(fmt.Sprintf("✓ restored '%s'", p.entry.Name))+theme.Dot()+theme.Muted.Render(countLabel(len(p.steps), "step")),
		inner,
	))}
	return surface.Popup(width, height, body)
}

// failedView names the step that stopped the replay and keeps the picker up
// until a key, so the failure can be read.
func (p *layoutPicker) failedView(surface frame.Surface, width, height int) string {
	panel := surface.Frame
	surface.Top = []string{
		panel.Row(theme.Label("restore", "failed", theme.RedBold)),
		panel.Rule(),
	}
	surface.Bottom = []string{panel.Rule(), panel.Row(theme.Muted.Render("press any key to exit"))}

	body := []string{panel.Row(theme.RedBold.Render(fmt.Sprintf("✗ restoring '%s' failed", p.entry.Name)))}
	var step layout.StepError
	if errors.As(p.restoreErr, &step) {
		label := layout.Step{Index: step.Index, Total: step.Total, Action: step.Action, Workspace: step.Workspace}
		body = append(body, panel.Row(theme.Muted.Render(fmt.Sprintf("step %d/%d", step.Index, step.Total))+" "+theme.Text.Render(label.Describe())))
		body = append(body, panel.Row(theme.Red.Render(step.Err.Error())))
	} else {
		body = append(body, panel.Row(theme.Red.Render(p.restoreErr.Error())))
	}
	return surface.Popup(width, height, body)
}

const (
	// pickerDefaultWidth and pickerDefaultHeight are what a prompt renders for
	// before the terminal reports its size.
	pickerDefaultWidth  = 88
	pickerDefaultHeight = 24
)

// pickerWindow is the size a prompt renders for.
func pickerWindow(width, height int) (int, int) {
	if width <= 0 {
		width = pickerDefaultWidth
	}
	if height <= 0 {
		height = pickerDefaultHeight
	}
	return width, height
}

// compactSize is the size range the space prompts stay within. They list a
// short, fixed set -- the layouts a store holds and the workspaces a session
// holds -- so a frame as tall as the switcher's would be mostly empty under the
// last row, which is the empty band the clamping is meant to remove: moving that
// band inside the frame does not make it smaller. 60 to 120 wide holds the four
// columns of a layout row (name, workspace, counts, age) and the detail line;
// 16 to 24 tall is the header, the rules and the two footer rows around a body
// of up to 15 rows, which covers a store or a session of ten entries. Past that
// the body windows on the highlight instead of growing, so the cap never hides
// an entry, and a frame smaller than its pane is centred in it.
var compactSize = frame.Size{MinWidth: 60, MaxWidth: 120, MinHeight: 16, MaxHeight: 24}

// spaceSurface is the surface both space prompts render inside: the same frame
// and title the switcher uses, so the two read as one product, inside their own
// compact bounds. The width is already clamped, so the rows can be composed
// against it.
func spaceSurface(width int) frame.Surface {
	title := theme.GoldBold.Render("hs") + theme.Quiet.Render(" ❯ ") + theme.AccentBold.Render("space load")
	surface := frame.Surface{Frame: frame.Frame{Title: title}, Size: compactSize}
	surface.Frame.Width = surface.Width(width)
	return surface
}

// pickerWindowRows keeps the row that holds the cursor visible when the body is
// shorter than its content: the visible slice is centred on it, the way the
// switcher's list scrolls.
func pickerWindowRows(rows []string, cursorAt, height int) []string {
	if height <= 0 || len(rows) <= height {
		return rows
	}
	start := max(0, min(cursorAt-height/2, len(rows)-height))
	return rows[start : start+height]
}

// syncHintBar returns the hints a screen shows and starts the bar at its first
// page whenever the set changed, so a state change never leaves the bar on a
// page the new set does not have.
func syncHintBar(bar *chip.HintBar, seen *string, hints []chip.Hint) []chip.Hint {
	key := hintKey(hints)
	if key != *seen {
		*seen = key
		bar.Page = 0
	}
	return hints
}

// hintKey names a set of hints, so the bar compares the set and not the window.
func hintKey(hints []chip.Hint) string {
	var key strings.Builder
	for _, hint := range hints {
		key.WriteString(hint.Key)
		key.WriteByte(0)
		key.WriteString(hint.Label)
		key.WriteByte(0)
	}
	return key.String()
}

// placeholderRows centers a calm message in a prompt's body: the state on the
// first row, what to do next on the second. The frame pads the body at both
// ends, so the placeholder's own blank rows are dropped instead of doubled.
func placeholderRows(surface frame.Frame, message, hint string) []string {
	lines := surface.Placeholder(message, hint)
	return lines[1 : len(lines)-1]
}

// countScope counts the layouts one scope contributes, so a section header can
// say how much it holds.
func countScope(entries []layout.Entry, scope string) int {
	count := 0
	for _, entry := range entries {
		if entry.Scope == scope {
			count++
		}
	}
	return count
}

// plural names a count's noun, singular for one.
func plural(count int, noun string) string {
	if count == 1 {
		return noun
	}
	return noun + "s"
}
