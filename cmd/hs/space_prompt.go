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
}

func (p *workspacePicker) Init() tea.Cmd { return nil }

func (p *workspacePicker) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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

func (p *workspacePicker) View() string {
	var view strings.Builder
	view.WriteString("load into which workspace?\n\n")
	for index, workspace := range p.workspaces {
		marker := "  "
		if index == p.cursor {
			marker = "> "
		}
		fmt.Fprintf(&view, "%s%s · %s · %s\n", marker, workspace.Label,
			countLabel(workspace.TabCount, "tab"), countLabel(workspace.PaneCount, "pane"))
	}
	view.WriteString("\nenter: load · esc: cancel\n")
	return view.String()
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
var pickerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

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
}

func (p *layoutPicker) Init() tea.Cmd { return nil }

func (p *layoutPicker) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
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

func (p *layoutPicker) View() string {
	switch p.phase {
	case pickerRestoring:
		var view strings.Builder
		fmt.Fprintf(&view, "%s restoring '%s'", pickerFrames[p.frame%len(pickerFrames)], p.entry.Name)
		if p.step != nil {
			fmt.Fprintf(&view, " — step %d/%d %s", p.step.Index, p.step.Total, p.step.Describe())
		} else {
			view.WriteString(" — starting")
		}
		view.WriteString("\n\nctrl+c: stop watching this restore\n")
		return view.String()
	case pickerDone:
		return fmt.Sprintf("✔ restored '%s' — %s\n", p.entry.Name, countLabel(len(p.steps), "step"))
	case pickerFailed:
		var view strings.Builder
		fmt.Fprintf(&view, "✗ restoring '%s' failed\n", p.entry.Name)
		var step layout.StepError
		if errors.As(p.restoreErr, &step) {
			label := layout.Step{Index: step.Index, Total: step.Total, Action: step.Action, Workspace: step.Workspace}
			fmt.Fprintf(&view, "  step %d/%d %s\n  %v\n", step.Index, step.Total, label.Describe(), step.Err)
		} else {
			fmt.Fprintf(&view, "  %v\n", p.restoreErr)
		}
		view.WriteString("\npress any key to exit\n")
		return view.String()
	}

	var view strings.Builder
	view.WriteString("load which layout?\n\n")
	for index, entry := range p.entries {
		marker := "  "
		if index == p.cursor {
			marker = "> "
		}
		fmt.Fprintf(&view, "%s%s · %s · %s · %s · %s\n", marker, entry.Name, entry.Scope,
			entry.Workspace, countLabel(entry.Tabs, "tab"), countLabel(entry.Panes, "pane"))
	}
	view.WriteString("\nenter: load · esc: cancel\n")
	return view.String()
}
