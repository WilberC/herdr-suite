// Package ui is the shared visual system of the hs TUIs: one palette, one
// frame, one chip, one hint bar. The switcher and the space picker render
// through it, so the two surfaces read as the same product instead of two
// screens that happen to share a binary.
//
// Render code names a role, never a colour: the palette lives here, and only
// here. The palette is Tokyo Night, the reference the redesign was measured
// against.
package ui

import "github.com/charmbracelet/lipgloss"

// Palette roles. Three accents carry the interface (blue, gold, teal); green
// means live, red means failure, and everything explanatory recedes to muted.
const (
	colorText        = "#c0caf5"
	colorMuted       = "#565f89"
	colorQuiet       = "#3b4261"
	colorBorder      = "#3d59a1"
	colorAccent      = "#7aa2f7"
	colorGold        = "#e0af68"
	colorTeal        = "#73daca"
	colorGreen       = "#9ece6a"
	colorRed         = "#f7768e"
	colorChip        = "#2f3549"
	colorSelection   = "#283457"
	colorMutedOnTint = "#737aa2"
	colorGuideOnTint = "#5f6b96"
	colorChipOnTint  = "#3d59a1"
)

// Roles, as styles a view composes with. Text is prose, Muted is explanation,
// Quiet is structure, Border is the frame, Accent is the product's voice,
// Gold marks state that outlives the selection, Teal marks work in progress,
// Green marks live things and Red marks failure.
var (
	Text       = lipgloss.NewStyle().Foreground(lipgloss.Color(colorText))
	Strong     = Text.Bold(true)
	Muted      = lipgloss.NewStyle().Foreground(lipgloss.Color(colorMuted))
	Quiet      = lipgloss.NewStyle().Foreground(lipgloss.Color(colorQuiet))
	Border     = lipgloss.NewStyle().Foreground(lipgloss.Color(colorBorder))
	Accent     = lipgloss.NewStyle().Foreground(lipgloss.Color(colorAccent))
	AccentBold = Accent.Bold(true)
	Gold       = lipgloss.NewStyle().Foreground(lipgloss.Color(colorGold))
	GoldBold   = Gold.Bold(true)
	Teal       = lipgloss.NewStyle().Foreground(lipgloss.Color(colorTeal))
	TealBold   = Teal.Bold(true)
	Green      = lipgloss.NewStyle().Foreground(lipgloss.Color(colorGreen))
	GreenBold  = Green.Bold(true)
	Red        = lipgloss.NewStyle().Foreground(lipgloss.Color(colorRed))
	RedBold    = Red.Bold(true)
	// SelectionBG tints a selected row instead of painting over it, so every
	// element on the row keeps its own colour.
	SelectionBG = lipgloss.NewStyle().Background(lipgloss.Color(colorSelection))
	// SelectionEdge is the selected row's first cell: the accent mark on the
	// tint, never a fill over the row's content.
	SelectionEdge = SelectionBG.Foreground(lipgloss.Color(colorAccent)).Bold(true)
	// TextOnSelection is the selected row's primary text: the text role, bold,
	// on the tint.
	TextOnSelection = SelectionBG.Foreground(lipgloss.Color(colorText)).Bold(true)
	// MutedOnSelection lifts the muted role enough to read on the tint.
	MutedOnSelection = SelectionBG.Foreground(lipgloss.Color(colorMutedOnTint))
	// GuideOnSelection lifts the guide role, so a tree guide stays readable on
	// the tint without competing with the row's facts.
	GuideOnSelection = SelectionBG.Foreground(lipgloss.Color(colorGuideOnTint))
	// ChipOnSelection is the chip slab one step brighter, for a token that sits
	// on the tint.
	ChipOnSelection = lipgloss.NewStyle().
			Background(lipgloss.Color(colorChipOnTint)).
			Foreground(lipgloss.Color(colorText)).
			Bold(true)
)

// chips are the tokens of the interface: keys, modes and short state words.
// One dark surface, one role for the label, so a chip is always a chip.
var (
	ChipText   = chipSurface(colorText)
	ChipAccent = chipSurface(colorAccent)
	ChipGold   = chipSurface(colorGold)
	ChipTeal   = chipSurface(colorTeal)
	ChipGreen  = chipSurface(colorGreen)
	ChipRed    = chipSurface(colorRed)
)

// OnSelection lifts a role onto the selection tint. The role keeps its colour
// and gains the background, so green, gold, teal and red mean the same thing on
// a selected row as off it.
func OnSelection(role lipgloss.Style) lipgloss.Style {
	return role.Background(lipgloss.Color(colorSelection))
}

func chipSurface(fg string) lipgloss.Style {
	return lipgloss.NewStyle().
		Background(lipgloss.Color(colorChip)).
		Foreground(lipgloss.Color(fg)).
		Bold(true)
}

// Chip renders one token on the chip surface: the slab carries one cell of
// padding on each side, so a chip is a shape rather than a bracketed word.
func Chip(role lipgloss.Style, label string) string {
	return role.Render(" " + label + " ")
}

// Hint is one key/label pair of a hint bar.
type Hint struct {
	Key   string
	Label string
}

// Hint renders the pair: the key as a chip, the label as muted prose.
func (h Hint) render() string {
	return Chip(ChipText, h.Key) + " " + Muted.Render(h.Label)
}

// Hints lays out as many hints as fit in width. A hint that does not fit is
// dropped whole, from the end, so a narrow window loses detail instead of
// wrapping the footer.
func Hints(width int, hints ...Hint) string {
	line := ""
	for _, hint := range hints {
		next := hint.render()
		if line != "" {
			next = "  " + next
		}
		if Width(line)+Width(next) > width {
			break
		}
		line += next
	}
	if line == "" && len(hints) > 0 {
		return Clip(hints[0].render(), width)
	}
	return line
}
