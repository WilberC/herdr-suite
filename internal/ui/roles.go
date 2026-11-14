package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Roles is the set of roles one row paints with. A row is either on the panel
// surface or selected, and the selection is a tint: every element keeps the
// meaning its colour carries, with the muted and structural tones lifted so
// they still read on the darker background.
type Roles struct {
	// Selected says whether the row is the highlighted one.
	Selected bool
	// Name is the row's primary text: the session, the layout, the workspace.
	Name lipgloss.Style
	// Muted is every secondary fact: workspace, counts, age, path.
	Muted lipgloss.Style
	// Guide is the tree structure that places the row.
	Guide lipgloss.Style
	// Space fills the cells of a row that carry no text, so a selected row's
	// tint reaches its edges.
	Space lipgloss.Style
	// Chip is the token surface a row may carry.
	Chip lipgloss.Style
	// Gold, Green, Teal and Red keep their meaning on both surfaces.
	Gold  lipgloss.Style
	Green lipgloss.Style
	Teal  lipgloss.Style
	Red   lipgloss.Style
}

var (
	panelRoles = Roles{
		Name:  Text,
		Muted: Muted,
		Guide: Quiet,
		Space: lipgloss.NewStyle(),
		Chip:  ChipText,
		Gold:  Gold,
		Green: Green,
		Teal:  Teal,
		Red:   Red,
	}
	selectionRoles = Roles{
		Selected: true,
		Name:     TextOnSelection,
		Muted:    MutedOnSelection,
		Guide:    GuideOnSelection,
		Space:    SelectionBG,
		Chip:     ChipOnSelection,
		Gold:     OnSelection(Gold),
		Green:    OnSelection(Green),
		Teal:     OnSelection(Teal),
		Red:      OnSelection(Red),
	}
)

// RowRoles returns the roles a row paints with: the panel surface, or the
// selection variants when the row is the highlighted one.
func RowRoles(selected bool) Roles {
	if selected {
		return selectionRoles
	}
	return panelRoles
}

// Pad fits s into width cells, filling the rest with the row's own background,
// so a selected row's tint has no gaps between its columns.
func (r Roles) Pad(s string, width int) string {
	clipped := Clip(s, width)
	return clipped + r.Gap(width-Width(clipped))
}

// Gap is n cells of the row's own background: the gutter between two columns.
func (r Roles) Gap(n int) string {
	if n <= 0 {
		return ""
	}
	return r.Space.Render(strings.Repeat(" ", n))
}

// AlignRight places right at the end of width cells, filling the space between
// with the row's own background.
func (r Roles) AlignRight(left, right string, width int) string {
	gap := width - Width(left) - Width(right)
	if gap < 0 {
		left = Clip(left, max(0, width-Width(right)-1))
		gap = width - Width(left) - Width(right)
	}
	if gap < 0 {
		return Clip(right, width)
	}
	return left + r.Gap(gap) + right
}

// AgentState renders the agent state herdr reports for a workspace on these
// roles: live work is green, a worked-up agent is gold, an unknown state stays
// muted, and a failed agent is the one red thing in a list.
func (r Roles) AgentState(state string) string {
	switch lower(state) {
	case "":
		return ""
	case "working", "running", "busy":
		return r.Green.Render(state)
	case "idle", "done", "stopped":
		return r.Muted.Render(state)
	case "waiting", "blocked", "paused", "needs-input":
		return r.Gold.Render(state)
	case "error", "failed", "dead":
		return r.Red.Render(state)
	default:
		return r.Muted.Render(state)
	}
}
