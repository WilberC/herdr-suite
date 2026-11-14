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
	"github.com/WilberC/tui-kit/zone"
	tea "github.com/charmbracelet/bubbletea"
)

// pickWorkspace asks which existing workspace a layout should replay into. It is
// the interactive half of "hs space load": the command refuses to guess when the
// stored workspace is not the active one.
func pickWorkspace(in io.Reader, out io.Writer, workspaces []herdrmock.Workspace) (string, error) {
	model := &workspacePicker{workspaces: workspaces}
	if _, err := tea.NewProgram(model, pickerOptions(in, out)...).Run(); err != nil {
		return "", err
	}
	if model.cancelled {
		return "", session.ErrCancelled
	}
	return model.chosen, nil
}

// pickerOptions is what a prompt asks the terminal for: the streams the caller
// handed in, and all-motion mouse reporting, which is what a hover needs. herdr
// forwards a moved event to a popup pane whose program enabled mouse reporting
// (src/client/shell/mouse.rs "MouseEventKind::Moved if hit.mouse_reporting"), and
// its host terminal is in any-event tracking.
func pickerOptions(in io.Reader, out io.Writer) []tea.ProgramOption {
	options := []tea.ProgramOption{tea.WithOutput(out), tea.WithMouseAllMotion()}
	if in != nil {
		options = append(options, tea.WithInput(in))
	}
	return options
}

// workspacePicker is the workspace selection list.
type workspacePicker struct {
	workspaces []herdrmock.Workspace
	cursor     int
	chosen     string
	cancelled  bool
	bar        chip.HintBar
	pickerPointer

	width  int
	height int
}

func (p *workspacePicker) Init() tea.Cmd { return nil }

func (p *workspacePicker) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.width, p.height = msg.Width, msg.Height
		return p, nil
	case tea.MouseMsg:
		return p, p.handleMouse(msg)
	}
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return p, nil
	}
	// The paging key comes before the bindings: the prompt reads no filter, so
	// tab only moves the hint bar.
	if key.String() == chip.PageKey {
		p.bar.Next(p.surface().Frame.ContentWidth(), p.hints()...)
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
		return p.pick()
	}
	return p, nil
}

// pick reports the highlighted workspace, which is what enter and a double click
// both do.
func (p *workspacePicker) pick() (tea.Model, tea.Cmd) {
	if p.cursor < len(p.workspaces) {
		p.chosen = p.workspaces[p.cursor].ID
	}
	return p, tea.Quit
}

// handleMouse is the mouse half of the prompt: a press selects the workspace it
// landed on, a second press on the same row inside the window picks it (the same
// action as enter), the wheel moves the selection, motion hovers, and a click on
// the paging chip is chip.PageKey.
func (p *workspacePicker) handleMouse(mouse tea.MouseMsg) tea.Cmd {
	switch mouse.Action {
	case tea.MouseActionMotion:
		p.hover = p.rowAt(mouse.X, mouse.Y) + 1
		return nil
	case tea.MouseActionPress:
	default:
		return nil
	}

	if step, ok := wheelStep(mouse.Button); ok {
		p.move(step)
		return nil
	}
	if mouse.Button != tea.MouseButtonLeft {
		return nil
	}

	id, ok := p.zones.Hit(mouse.X, mouse.Y)
	if !ok {
		return nil
	}
	if id == chip.PageZone {
		p.bar.Next(p.surface().Frame.ContentWidth(), p.hints()...)
		return nil
	}
	index, ok := session.RowZoneIndex(id)
	if !ok {
		return nil
	}
	p.hover = index + 1
	p.cursor = index
	if p.press(id) {
		_, cmd := p.pick()
		return cmd
	}
	return nil
}

// move moves the highlight by step rows and stops at both ends, which is what the
// wheel and the cursor keys share.
func (p *workspacePicker) move(step int) {
	next := p.cursor + step
	if next < 0 || next >= len(p.workspaces) || next == p.cursor {
		return
	}
	p.cursor = next
}

// surface is the prompt inside the kit's recommended bounds, its width already
// clamped so the rows can be composed against it.
func (p *workspacePicker) surface() frame.Surface {
	width, _ := pickerWindow(p.width, p.height)
	return spaceSurface(width)
}

// View renders the workspace prompt inside the shared surface: the question at
// the top, the workspaces as aligned rows in the body, and the footer pinned to
// the bottom. The render also records where it drew every clickable row and every
// chip, so a click is answered from the cell alone.
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
		surface.Row(width, text.Join(
			theme.Label("load", "into which workspace?", theme.AccentBold),
			right,
			inner,
		)),
		panel.Rule(),
	}
	surface.Bottom = []string{
		panel.Rule(),
		surface.Row(width, p.detailContent(inner)),
		surface.Row(width, ""),
	}

	zones := p.frameZones()
	hintLine := len(surface.Bottom) - 1
	surface.Bottom[hintLine] = surface.Row(width, p.bar.RenderZones(zones, 0, surface.BottomY(height, hintLine), inner, p.hints()...))

	rows := pickerWindowRows(p.bodyRows(panel), p.cursor, surface.BodyRows(height))
	body := make([]string, len(rows))
	for i, row := range rows {
		body[i] = surface.Row(width, paintPickerRow(row, p.rowState(row.index), inner))
		recordPickerRow(zones, row, surface, i)
	}
	return surface.PopupZones(width, height, body, zones)
}

// rowState is how one row is marked: the highlighted workspace, the row under the
// pointer, or neither. The selection wins where both apply.
func (p *workspacePicker) rowState(index int) list.State {
	return list.State{Selected: index >= 0 && index == p.cursor, Hovered: index >= 0 && index+1 == p.hover}
}

// bodyRows renders the workspaces with their counts and agent state in fixed
// columns. A row records what it selects and how it draws for the roles its state
// asks for, so the palette follows the pointer instead of being fixed at render.
func (p *workspacePicker) bodyRows(surface frame.Frame) []pickerRow {
	if len(p.workspaces) == 0 {
		return placeholderPickerRows(surface, "no workspaces to load into", "open one in herdr, then run this again")
	}

	labelWidth, countWidth := 0, 0
	counts := make([]string, len(p.workspaces))
	for i, workspace := range p.workspaces {
		labelWidth = max(labelWidth, text.Width(workspace.Label))
		counts[i] = countLabel(workspace.TabCount, "tab") + " · " + countLabel(workspace.PaneCount, "pane")
		countWidth = max(countWidth, text.Width(counts[i]))
	}
	labelWidth = min(labelWidth, max(8, surface.ContentWidth()/3))

	rows := make([]pickerRow, 0, len(p.workspaces))
	for i, workspace := range p.workspaces {
		workspace, last := workspace, i == len(p.workspaces)-1
		rows = append(rows, pickerRow{index: i, paint: func(roles list.Roles) string {
			content := roles.Pad(roles.Name.Render(workspace.Label), labelWidth)
			content += roles.Gap(2) + roles.Pad(roles.Muted.Render(counts[i]), countWidth)
			if agent := roles.AgentState(workspace.AgentState); agent != "" {
				content += roles.Gap(2) + roles.Pad(agent, 9)
			}
			if workspace.Focused {
				content += roles.Gap(2) + roles.Gold.Render(text.Mark)
			}
			return list.Row(roles, last, content)
		}})
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
	if _, err := tea.NewProgram(model, pickerOptions(in, out)...).Run(); err != nil {
		return layoutPick{}, err
	}
	return pickResult(model)
}

// newLayoutPicker returns the picker a terminal runs.
func newLayoutPicker(entries []layout.Entry, restore layoutRestore) *layoutPicker {
	return &layoutPicker{entries: entries, restore: restore, phase: pickerChoosing, tickDelay: pickerSpinnerDelay, now: time.Now}
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

	bar chip.HintBar
	pickerPointer

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
	case tea.MouseMsg:
		return p, p.handleMouse(msg)
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
		p.bar.Next(p.surface().Frame.ContentWidth(), p.hints()...)
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
		surface.Row(width, text.Join(
			theme.Label("load", "which layout?", theme.AccentBold),
			right,
			inner,
		)),
		panel.Rule(),
	}
	surface.Bottom = []string{
		panel.Rule(),
		surface.Row(width, p.detailContent(inner)),
		surface.Row(width, ""),
	}

	zones := p.frameZones()
	hintLine := len(surface.Bottom) - 1
	surface.Bottom[hintLine] = surface.Row(width, p.bar.RenderZones(zones, 0, surface.BottomY(height, hintLine), inner, p.hints()...))

	rows, cursorAt := p.choosingRows(panel)
	visible := pickerWindowRows(rows, cursorAt, surface.BodyRows(height))
	body := make([]string, len(visible))
	for i, row := range visible {
		body[i] = surface.Row(width, paintPickerRow(row, p.rowState(row.index), inner))
		recordPickerRow(zones, row, surface, i)
	}
	return surface.PopupZones(width, height, body, zones)
}

// rowState is how one row is marked: the highlighted layout, the row under the
// pointer, or neither. The selection wins where both apply.
func (p *layoutPicker) rowState(index int) list.State {
	return list.State{Selected: index >= 0 && index == p.cursor, Hovered: index >= 0 && index+1 == p.hover}
}

// choosingRows renders the layouts grouped by the scope they were found in:
// the project's first, then the global store, then explicit paths. The rows keep
// the order the store lists, so the cursor and the action it confirms cannot
// drift from what the CLI would pick. It returns the rows and the row that
// holds the cursor, so a body shorter than the list can keep it in view.
func (p *layoutPicker) choosingRows(panel frame.Frame) ([]pickerRow, int) {
	inner := panel.ContentWidth()
	if len(p.entries) == 0 {
		return placeholderPickerRows(panel, "no layouts found", "save one with hs space save <name>"), 0
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

	rows := make([]pickerRow, 0, len(p.entries)+3)
	scope, cursorAt := "", 0
	for i, entry := range p.entries {
		if entry.Scope != scope {
			// The body opens with its own blank row, so only the groups after the
			// first one get a separator row, the same rhythm the switcher uses.
			if scope != "" {
				rows = append(rows, pickerRow{index: -1, paint: func(list.Roles) string { return "" }})
			}
			scope = entry.Scope
			section := list.Section(scope, countScope(p.entries, scope))
			rows = append(rows, pickerRow{index: -1, paint: func(list.Roles) string { return section }})
		}

		entry, last := entry, i == len(p.entries)-1 || p.entries[i+1].Scope != entry.Scope
		count, when := counts[i], text.RelativeTime(time.Now(), entry.Modified)
		rows = append(rows, pickerRow{index: i, paint: func(roles list.Roles) string {
			content := roles.Pad(roles.Name.Render(entry.Name), nameWidth)
			content += roles.Gap(2) + roles.Pad(roles.Muted.Render(entry.Workspace), workspaceWidth)
			content += roles.Gap(2) + roles.Pad(roles.Muted.Render(count), countWidth)
			line := list.Row(roles, last, content)
			if when != "" {
				line = roles.AlignRight(line, roles.Muted.Render(when)+roles.Gap(1), inner)
			}
			return line
		}})
		if i == p.cursor {
			cursorAt = len(rows) - 1
		}
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
		panel.Row(p.bar.Render(inner, p.hints()...)),
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
		panel.Row(p.bar.Render(inner, p.hints()...)),
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

// pickerRow is one row of a prompt's body before it is painted: the entry a click
// on it selects (-1 for a row that selects nothing, a group header or a blank) and
// how it draws itself for the roles its state asks for.
type pickerRow struct {
	index int
	paint func(list.Roles) string
}

// paintPickerRow draws one body row with the roles its state asks for: the
// selected row keeps the selection's tint, the row under the pointer wears the
// fainter one, and the selection wins where both apply.
func paintPickerRow(row pickerRow, state list.State, width int) string {
	roles := list.RowRolesFor(state)
	line := row.paint(roles)
	switch {
	case roles.Selected:
		return list.SelectedRow(line, width)
	case roles.Hovered:
		return roles.Pad(line, width)
	}
	return line
}

// pickerPointer is the mouse state the two space prompts share: the row the
// pointer is over, the zones this render recorded, and the gesture a press
// completes. The hover is counted from one, so the zero value means the pointer
// is over no row and a prompt built as a literal needs no initialiser.
type pickerPointer struct {
	hover int
	zones *zone.Map
	// clicks recognises a double click, and now is the clock it reads: a test
	// drives the clock instead of waiting.
	clicks session.Clicks
	now    func() time.Time
}

// frameZones is the map this render records into, emptied so a click is never
// answered from a frame that is no longer on screen.
func (m *pickerPointer) frameZones() *zone.Map {
	if m.zones == nil {
		m.zones = zone.New()
	}
	m.zones.Reset()
	return m.zones
}

// rowAt is the row a cell hits, or -1 for a cell that is not a row: a group
// header, a rule, a footer or the blank space beside them.
func (m *pickerPointer) rowAt(x, y int) int {
	id, ok := m.zones.Hit(x, y)
	if !ok {
		return -1
	}
	index, ok := session.RowZoneIndex(id)
	if !ok {
		return -1
	}
	return index
}

// press records a press on one zone and answers whether it completes a double
// click, which is where the row's default action runs.
func (m *pickerPointer) press(id string) bool {
	now := time.Now
	if m.now != nil {
		now = m.now
	}
	return m.clicks.Double(id, now())
}

// wheelStep is the wheel notch a button names: -1 up, 1 down, and not a wheel at
// all for every other button.
func wheelStep(button tea.MouseButton) (int, bool) {
	switch button {
	case tea.MouseButtonWheelUp:
		return -1, true
	case tea.MouseButtonWheelDown:
		return 1, true
	}
	return 0, false
}

// recordPickerRow registers the cells of one drawn row, so a click is answered
// from the zone map instead of a frame the prompt measured itself. A row the body
// had no room for is not drawn, so it is not registered and cannot be clicked.
func recordPickerRow(zones *zone.Map, row pickerRow, surface frame.Surface, line int) {
	if row.index < 0 {
		return
	}
	zones.Add(session.RowZone(row.index), 0, surface.BodyY(line), surface.Frame.ContentWidth(), 1)
}

// handleMouse is the mouse half of the picker while it is choosing a layout: a
// press selects the layout it landed on, a second press on the same row inside the
// window loads it (the same action as enter), the wheel moves the selection,
// motion hovers, and a click on the paging chip is chip.PageKey. The restoring,
// done and failed views take no mouse action, because their only answer is \"any
// key\".
func (p *layoutPicker) handleMouse(mouse tea.MouseMsg) tea.Cmd {
	if p.phase != pickerChoosing {
		return nil
	}
	switch mouse.Action {
	case tea.MouseActionMotion:
		p.hover = p.rowAt(mouse.X, mouse.Y) + 1
		return nil
	case tea.MouseActionPress:
	default:
		return nil
	}

	if step, ok := wheelStep(mouse.Button); ok {
		p.move(step)
		return nil
	}
	if mouse.Button != tea.MouseButtonLeft {
		return nil
	}

	id, ok := p.zones.Hit(mouse.X, mouse.Y)
	if !ok {
		return nil
	}
	if id == chip.PageZone {
		p.bar.Next(p.surface().Frame.ContentWidth(), p.hints()...)
		return nil
	}
	index, ok := session.RowZoneIndex(id)
	if !ok {
		return nil
	}
	p.hover = index + 1
	p.cursor = index
	if p.press(id) {
		return p.confirm()
	}
	return nil
}

// move moves the highlight by step rows and stops at both ends, which is what the
// wheel and the cursor keys share.
func (p *layoutPicker) move(step int) {
	next := p.cursor + step
	if next < 0 || next >= len(p.entries) || next == p.cursor {
		return
	}
	p.cursor = next
}

// pickerWindowRows keeps the row that holds the cursor visible when the body is
// shorter than its content: the visible slice is centred on it, the way the
// switcher's list scrolls. The slice is the body the surface draws, so the y of a
// row is its index in this slice.
func pickerWindowRows(rows []pickerRow, cursorAt, height int) []pickerRow {
	if height <= 0 || len(rows) <= height {
		return rows
	}
	start := max(0, min(cursorAt-height/2, len(rows)-height))
	return rows[start : start+height]
}

// placeholderRows centers a calm message in a prompt's body: the state on the
// first row, what to do next on the second. The frame pads the body at both
// ends, so the placeholder's own blank rows are dropped instead of doubled.
func placeholderRows(surface frame.Frame, message, hint string) []string {
	lines := surface.Placeholder(message, hint)
	return lines[1 : len(lines)-1]
}

// placeholderPickerRows is placeholderRows as body rows that select nothing.
func placeholderPickerRows(surface frame.Frame, message, hint string) []pickerRow {
	lines := placeholderRows(surface, message, hint)
	rows := make([]pickerRow, 0, len(lines))
	for _, line := range lines {
		rows = append(rows, pickerRow{index: -1, paint: func(list.Roles) string { return line }})
	}
	return rows
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
