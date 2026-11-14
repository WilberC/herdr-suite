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
	"github.com/WilberC/herdr-suite/internal/ui"
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

// View renders the workspace prompt inside the shared surface: the question,
// the workspaces as aligned rows, and the footer the pickers share.
func (p *workspacePicker) View() string {
	width, height := pickerWindow(p.width, p.height)
	frame := spaceFrame(width)
	inner := frame.ContentWidth()

	right := ui.Muted.Render("esc to cancel")
	if len(p.workspaces) > 0 {
		right = ui.Muted.Render(countLabel(len(p.workspaces), "workspace") + " · esc to cancel")
	}
	rows := []string{frame.Row(ui.Join(
		ui.Label("load", "into which workspace?", ui.AccentBold),
		right,
		inner,
	)), frame.Rule(), frame.Row("")}
	rows = append(rows, p.bodyRows(frame)...)
	rows = append(rows, frame.Rule(), frame.Row(p.detailContent(inner)), frame.Row(p.hintsContent(inner)))
	return ui.Float(ui.Layout(frame, rows...), width, height)
}

// bodyRows renders the workspaces with their counts and agent state in fixed
// columns, the highlighted one as a solid bar.
func (p *workspacePicker) bodyRows(frame ui.Frame) []string {
	if len(p.workspaces) == 0 {
		return placeholderRows(frame, "no workspaces to load into", "open one in herdr, then run this again")
	}

	labelWidth, countWidth := 0, 0
	counts := make([]string, len(p.workspaces))
	for i, workspace := range p.workspaces {
		labelWidth = max(labelWidth, ui.Width(workspace.Label))
		counts[i] = countLabel(workspace.TabCount, "tab") + " · " + countLabel(workspace.PaneCount, "pane")
		countWidth = max(countWidth, ui.Width(counts[i]))
	}
	labelWidth = min(labelWidth, max(8, frame.ContentWidth()/3))

	rows := make([]string, 0, len(p.workspaces))
	for i, workspace := range p.workspaces {
		roles := ui.RowRoles(i == p.cursor)
		content := roles.Pad(roles.Name.Render(workspace.Label), labelWidth)
		content += roles.Gap(2) + roles.Pad(roles.Muted.Render(counts[i]), countWidth)
		if agent := roles.AgentState(workspace.AgentState); agent != "" {
			content += roles.Gap(2) + roles.Pad(agent, 9)
		}
		if workspace.Focused {
			content += roles.Gap(2) + roles.Gold.Render(ui.Mark)
		}
		row := ui.Row(roles, i == len(p.workspaces)-1, content)
		if i == p.cursor {
			row = ui.SelectedRow(row, frame.ContentWidth())
		}
		rows = append(rows, frame.Row(row))
	}
	return rows
}

// detailContent names the highlighted workspace and its facts.
func (p *workspacePicker) detailContent(width int) string {
	if len(p.workspaces) == 0 || p.cursor < 0 || p.cursor >= len(p.workspaces) {
		return ui.Muted.Render("no workspace to load into")
	}
	workspace := p.workspaces[p.cursor]
	left := ui.AccentBold.Render(workspace.Label) +
		ui.Dot() + ui.Text.Render(countLabel(workspace.TabCount, "tab")) +
		ui.Dot() + ui.Text.Render(countLabel(workspace.PaneCount, "pane"))
	if agent := ui.RowRoles(false).AgentState(workspace.AgentState); agent != "" {
		left += ui.Dot() + agent
	}
	if workspace.Focused {
		left += ui.Dot() + ui.Gold.Render(ui.Mark+" focused")
	}
	return ui.Footer(left, "", width)
}

// hintsContent is the binding bar of the prompt.
func (p *workspacePicker) hintsContent(width int) string {
	return ui.Hints(width,
		ui.Hint{Key: "↑↓", Label: "move"},
		ui.Hint{Key: "enter", Label: "load"},
		ui.Hint{Key: "esc", Label: "cancel"},
	)
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
var pickerFrames = ui.SpinnerFrames

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
// restoring, done and failed states.
func (p *layoutPicker) handleKey(key tea.KeyMsg) tea.Cmd {
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
	frame := spaceFrame(width)
	switch p.phase {
	case pickerRestoring:
		return p.restoringView(frame, width, height)
	case pickerDone:
		return p.doneView(frame, width, height)
	case pickerFailed:
		return p.failedView(frame, width, height)
	}
	return p.choosingView(frame, width, height)
}

func (p *layoutPicker) choosingView(frame ui.Frame, width, height int) string {
	inner := frame.ContentWidth()
	right := ui.Muted.Render("esc to cancel")
	if len(p.entries) > 0 {
		right = ui.Muted.Render(fmt.Sprintf("%d %s · esc to cancel", len(p.entries), plural(len(p.entries), "layout")))
	}
	rows := []string{frame.Row(ui.Join(
		ui.Label("load", "which layout?", ui.AccentBold),
		right,
		inner,
	)), frame.Rule()}
	rows = append(rows, p.choosingRows(frame)...)
	rows = append(rows, frame.Rule(), frame.Row(p.detailContent(inner)), frame.Row(p.hintsContent(inner)))
	return ui.Float(ui.Layout(frame, rows...), width, height)
}

// choosingRows renders the layouts grouped by the scope they were found in:
// the project's first, then the global store, then explicit paths. The rows keep
// the order the store lists, so the cursor and the action it confirms cannot
// drift from what the CLI would pick.
func (p *layoutPicker) choosingRows(frame ui.Frame) []string {
	inner := frame.ContentWidth()
	if len(p.entries) == 0 {
		return placeholderRows(frame, "no layouts found", "save one with hs space save <name>")
	}

	nameWidth, workspaceWidth, countWidth := 0, 0, 0
	counts := make([]string, len(p.entries))
	for i, entry := range p.entries {
		nameWidth = max(nameWidth, ui.Width(entry.Name))
		workspaceWidth = max(workspaceWidth, ui.Width(entry.Workspace))
		counts[i] = countLabel(entry.Tabs, "tab") + " · " + countLabel(entry.Panes, "pane")
		countWidth = max(countWidth, ui.Width(counts[i]))
	}
	nameWidth = min(nameWidth, max(8, inner/4))
	workspaceWidth = min(workspaceWidth, max(6, inner/5))

	rows := make([]string, 0, len(p.entries)+3)
	scope := ""
	for i, entry := range p.entries {
		if entry.Scope != scope {
			// The rule above already separates the first group; the groups after
			// it get one blank row, the same rhythm the switcher uses.
			if scope != "" {
				rows = append(rows, frame.Row(""))
			}
			scope = entry.Scope
			rows = append(rows, frame.Row(ui.Section(scope, countScope(p.entries, scope))))
		}
		roles := ui.RowRoles(i == p.cursor)
		content := roles.Pad(roles.Name.Render(entry.Name), nameWidth)
		content += roles.Gap(2) + roles.Pad(roles.Muted.Render(entry.Workspace), workspaceWidth)
		content += roles.Gap(2) + roles.Pad(roles.Muted.Render(counts[i]), countWidth)

		last := i == len(p.entries)-1 || p.entries[i+1].Scope != entry.Scope
		row := ui.Row(roles, last, content)
		if when := ui.RelativeTime(time.Now(), entry.Modified); when != "" {
			row = roles.AlignRight(row, roles.Muted.Render(when)+roles.Gap(1), inner)
		}
		if i == p.cursor {
			row = ui.SelectedRow(row, inner)
		}
		rows = append(rows, frame.Row(row))
	}
	return rows
}

// detailContent names the highlighted layout and its facts. The path is the
// first fact a narrow window drops: it is the one thing the columns above
// already implied.
func (p *layoutPicker) detailContent(width int) string {
	if len(p.entries) == 0 || p.cursor < 0 || p.cursor >= len(p.entries) {
		return ui.Muted.Render("no layout to load")
	}
	entry := p.entries[p.cursor]
	left := ui.AccentBold.Render(entry.Name) + " " + ui.Chip(ui.ChipGold, entry.Scope)
	if entry.Workspace != "" {
		left += ui.Dot() + ui.Label("workspace", entry.Workspace, ui.TealBold)
	}
	left += ui.Dot() + ui.Text.Render(countLabel(entry.Tabs, "tab")) +
		ui.Dot() + ui.Text.Render(countLabel(entry.Panes, "pane"))
	if when := ui.RelativeTime(time.Now(), entry.Modified); when != "" {
		left += ui.Dot() + ui.Muted.Render(when)
	}

	path := ui.ShortPath(entry.Path)
	room := width - ui.Width(left) - 2
	switch {
	case path == "":
		return left
	case ui.Width(path) <= room:
		return ui.Footer(left, ui.Muted.Render(path), width)
	case room >= 18:
		return ui.Footer(left, ui.Muted.Render(ui.EllipsisPath(path, room)), width)
	default:
		return left
	}
}

// hintsContent is the binding bar of the picker.
func (p *layoutPicker) hintsContent(width int) string {
	return ui.Hints(width,
		ui.Hint{Key: "↑↓", Label: "move"},
		ui.Hint{Key: "enter", Label: "load"},
		ui.Hint{Key: "esc", Label: "cancel"},
	)
}

// restoringView shows the replay as it runs: the spinner and the step on the
// row they replace, the steps already done under them, and the one binding that
// still works while a restore is in flight.
func (p *layoutPicker) restoringView(frame ui.Frame, width, height int) string {
	inner := frame.ContentWidth()
	rows := []string{frame.Row(ui.Join(
		ui.Label("restore", "in progress", ui.TealBold),
		ui.Muted.Render(countLabel(len(p.steps), "step")+" done"),
		inner,
	)), frame.Rule()}

	describe := "starting"
	if p.step != nil {
		describe = fmt.Sprintf("step %d/%d %s", p.step.Index, p.step.Total, p.step.Describe())
	}
	rows = append(rows, frame.Row(
		ui.Spinner(p.frame)+" "+ui.AccentBold.Render(fmt.Sprintf("restoring '%s'", p.entry.Name))+ui.Dot()+ui.Text.Render(describe),
	))

	budget := max(1, height-pickerChrome-2)
	done := p.steps
	if len(done) > budget {
		done = done[len(done)-budget:]
	}
	for i, step := range done {
		rows = append(rows, frame.Row(ui.Guide(ui.Quiet, i == len(done)-1)+" "+ui.Green.Render("✓")+" "+ui.Muted.Render(step.Describe())))
	}

	rows = append(rows, frame.Rule(),
		frame.Row(p.detailContent(inner)),
		frame.Row(ui.Hints(inner, ui.Hint{Key: "ctrl+c", Label: "stop watching"})))
	return ui.Float(ui.Layout(frame, rows...), width, height)
}

// doneView reports the replay that just finished.
func (p *layoutPicker) doneView(frame ui.Frame, width, height int) string {
	inner := frame.ContentWidth()
	rows := []string{
		frame.Row(ui.Label("restore", "done", ui.GreenBold)),
		frame.Rule(),
		frame.Row(ui.Center(ui.GreenBold.Render(fmt.Sprintf("✓ restored '%s'", p.entry.Name))+ui.Dot()+ui.Muted.Render(countLabel(len(p.steps), "step")), inner)),
		frame.Rule(),
		frame.Row(ui.Hints(inner, ui.Hint{Key: "any key", Label: "close"})),
	}
	return ui.Float(ui.Layout(frame, rows...), width, height)
}

// failedView names the step that stopped the replay and keeps the picker up
// until a key, so the failure can be read.
func (p *layoutPicker) failedView(frame ui.Frame, width, height int) string {
	rows := []string{
		frame.Row(ui.Label("restore", "failed", ui.RedBold)),
		frame.Rule(),
		frame.Row(ui.RedBold.Render(fmt.Sprintf("✗ restoring '%s' failed", p.entry.Name))),
	}

	var step layout.StepError
	if errors.As(p.restoreErr, &step) {
		label := layout.Step{Index: step.Index, Total: step.Total, Action: step.Action, Workspace: step.Workspace}
		rows = append(rows, frame.Row(ui.Muted.Render(fmt.Sprintf("step %d/%d", step.Index, step.Total))+" "+ui.Text.Render(label.Describe())))
		rows = append(rows, frame.Row(ui.Red.Render(step.Err.Error())))
	} else {
		rows = append(rows, frame.Row(ui.Red.Render(p.restoreErr.Error())))
	}
	rows = append(rows, frame.Rule(), frame.Row(ui.Muted.Render("press any key to exit")))
	return ui.Float(ui.Layout(frame, rows...), width, height)
}

const (
	// pickerDefaultWidth and pickerDefaultHeight are what a prompt renders for
	// before the terminal reports its size.
	pickerDefaultWidth  = 88
	pickerDefaultHeight = 24
	// pickerMaxWidth keeps the prompt a popup on a wide terminal.
	pickerMaxWidth = 96
	// pickerChrome is every prompt line that is not a list row.
	pickerChrome = 6
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

// spaceFrame is the surface both space prompts render inside: the same frame,
// title and rules the switcher uses, so the two read as one product.
func spaceFrame(width int) ui.Frame {
	title := ui.GoldBold.Render("hs") + ui.Quiet.Render(" ❯ ") + ui.AccentBold.Render("space load")
	return ui.Frame{Title: title, Width: min(max(width, 32), pickerMaxWidth)}
}

// placeholderRows centers a calm message in a prompt's body.
func placeholderRows(frame ui.Frame, message, hint string) []string {
	lines := frame.Placeholder(message, hint)
	rows := make([]string, 0, len(lines))
	for _, line := range lines {
		rows = append(rows, frame.Row(line))
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
