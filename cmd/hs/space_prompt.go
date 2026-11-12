package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/WilberC/herdr-suite/internal/herdrmock"
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
