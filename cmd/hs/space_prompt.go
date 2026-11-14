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
	surface := spaceFrame(width)
	inner := surface.ContentWidth()

	right := theme.Muted.Render("esc to cancel")
	if len(p.workspaces) > 0 {
		right = theme.Muted.Render(countLabel(len(p.workspaces), "workspace") + " · esc to cancel")
	}
	rows := []string{surface.Row(text.Join(
		theme.Label("load", "into which workspace?", theme.AccentBold),
		right,
		inner,
	)), surface.Rule(), surface.Row("")}
	rows = append(rows, p.bodyRows(surface)...)
	rows = append(rows, surface.Rule(), surface.Row(p.detailContent(inner)), surface.Row(p.hintsContent(inner)))
	return frame.Float(frame.Layout(surface, rows...), width, height)
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
		rows = append(rows, surface.Row(row))
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

// hintsContent is the binding bar of the prompt.
func (p *workspacePicker) hintsContent(width int) string {
	return chip.Hints(width,
		chip.Hint{Key: "↑↓", Label: "move"},
		chip.Hint{Key: "enter", Label: "load"},
		chip.Hint{Key: "esc", Label: "cancel"},
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
	surface := spaceFrame(width)
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

func (p *layoutPicker) choosingView(surface frame.Frame, width, height int) string {
	inner := surface.ContentWidth()
	right := theme.Muted.Render("esc to cancel")
	if len(p.entries) > 0 {
		right = theme.Muted.Render(fmt.Sprintf("%d %s · esc to cancel", len(p.entries), plural(len(p.entries), "layout")))
	}
	rows := []string{surface.Row(text.Join(
		theme.Label("load", "which layout?", theme.AccentBold),
		right,
		inner,
	)), surface.Rule()}
	rows = append(rows, p.choosingRows(surface)...)
	rows = append(rows, surface.Rule(), surface.Row(p.detailContent(inner)), surface.Row(p.hintsContent(inner)))
	return frame.Float(frame.Layout(surface, rows...), width, height)
}

// choosingRows renders the layouts grouped by the scope they were found in:
// the project's first, then the global store, then explicit paths. The rows keep
// the order the store lists, so the cursor and the action it confirms cannot
// drift from what the CLI would pick.
func (p *layoutPicker) choosingRows(surface frame.Frame) []string {
	inner := surface.ContentWidth()
	if len(p.entries) == 0 {
		return placeholderRows(surface, "no layouts found", "save one with hs space save <name>")
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
	scope := ""
	for i, entry := range p.entries {
		if entry.Scope != scope {
			// The rule above already separates the first group; the groups after
			// it get one blank row, the same rhythm the switcher uses.
			if scope != "" {
				rows = append(rows, surface.Row(""))
			}
			scope = entry.Scope
			rows = append(rows, surface.Row(list.Section(scope, countScope(p.entries, scope))))
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
		}
		rows = append(rows, surface.Row(row))
	}
	return rows
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

// hintsContent is the binding bar of the picker.
func (p *layoutPicker) hintsContent(width int) string {
	return chip.Hints(width,
		chip.Hint{Key: "↑↓", Label: "move"},
		chip.Hint{Key: "enter", Label: "load"},
		chip.Hint{Key: "esc", Label: "cancel"},
	)
}

// restoringView shows the replay as it runs: the spinner and the step on the
// row they replace, the steps already done under them, and the one binding that
// still works while a restore is in flight.
func (p *layoutPicker) restoringView(surface frame.Frame, width, height int) string {
	inner := surface.ContentWidth()
	rows := []string{surface.Row(text.Join(
		theme.Label("restore", "in progress", theme.TealBold),
		theme.Muted.Render(countLabel(len(p.steps), "step")+" done"),
		inner,
	)), surface.Rule()}

	describe := "starting"
	if p.step != nil {
		describe = fmt.Sprintf("step %d/%d %s", p.step.Index, p.step.Total, p.step.Describe())
	}
	rows = append(rows, surface.Row(
		spinner.Render(p.frame)+" "+theme.AccentBold.Render(fmt.Sprintf("restoring '%s'", p.entry.Name))+theme.Dot()+theme.Text.Render(describe),
	))

	budget := max(1, height-pickerChrome-2)
	done := p.steps
	if len(done) > budget {
		done = done[len(done)-budget:]
	}
	for i, step := range done {
		rows = append(rows, surface.Row(list.Guide(theme.Quiet, i == len(done)-1)+" "+theme.Green.Render("✓")+" "+theme.Muted.Render(step.Describe())))
	}

	rows = append(rows, surface.Rule(),
		surface.Row(p.detailContent(inner)),
		surface.Row(chip.Hints(inner, chip.Hint{Key: "ctrl+c", Label: "stop watching"})))
	return frame.Float(frame.Layout(surface, rows...), width, height)
}

// doneView reports the replay that just finished.
func (p *layoutPicker) doneView(surface frame.Frame, width, height int) string {
	inner := surface.ContentWidth()
	rows := []string{
		surface.Row(theme.Label("restore", "done", theme.GreenBold)),
		surface.Rule(),
		surface.Row(text.Center(theme.GreenBold.Render(fmt.Sprintf("✓ restored '%s'", p.entry.Name))+theme.Dot()+theme.Muted.Render(countLabel(len(p.steps), "step")), inner)),
		surface.Rule(),
		surface.Row(chip.Hints(inner, chip.Hint{Key: "any key", Label: "close"})),
	}
	return frame.Float(frame.Layout(surface, rows...), width, height)
}

// failedView names the step that stopped the replay and keeps the picker up
// until a key, so the failure can be read.
func (p *layoutPicker) failedView(surface frame.Frame, width, height int) string {
	rows := []string{
		surface.Row(theme.Label("restore", "failed", theme.RedBold)),
		surface.Rule(),
		surface.Row(theme.RedBold.Render(fmt.Sprintf("✗ restoring '%s' failed", p.entry.Name))),
	}

	var step layout.StepError
	if errors.As(p.restoreErr, &step) {
		label := layout.Step{Index: step.Index, Total: step.Total, Action: step.Action, Workspace: step.Workspace}
		rows = append(rows, surface.Row(theme.Muted.Render(fmt.Sprintf("step %d/%d", step.Index, step.Total))+" "+theme.Text.Render(label.Describe())))
		rows = append(rows, surface.Row(theme.Red.Render(step.Err.Error())))
	} else {
		rows = append(rows, surface.Row(theme.Red.Render(p.restoreErr.Error())))
	}
	rows = append(rows, surface.Rule(), surface.Row(theme.Muted.Render("press any key to exit")))
	return frame.Float(frame.Layout(surface, rows...), width, height)
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
func spaceFrame(width int) frame.Frame {
	title := theme.GoldBold.Render("hs") + theme.Quiet.Render(" ❯ ") + theme.AccentBold.Render("space load")
	return frame.Frame{Title: title, Width: min(max(width, 32), pickerMaxWidth)}
}

// placeholderRows centers a calm message in a prompt's body.
func placeholderRows(surface frame.Frame, message, hint string) []string {
	lines := surface.Placeholder(message, hint)
	rows := make([]string, 0, len(lines))
	for _, line := range lines {
		rows = append(rows, surface.Row(line))
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
