package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Frame is the bordered surface both TUIs render inside: the title sits in the
// top border, every interior line is padded by one cell on each side, and the
// sides are joined by rules. It clips every line to its own width, so a view
// can never wrap, whatever the window size.
type Frame struct {
	// Title is the surface's name, drawn in the top border. It may be styled.
	Title string
	// Width is the whole surface, borders included.
	Width int
}

// framePad is the cell of breathing room between a border and its content.
const framePad = 1

// ContentWidth is the width a row's content may use: the frame minus its two
// borders and the padding on each side.
func (f Frame) ContentWidth() int { return max(0, f.Width-2-2*framePad) }

// Top renders the top border with the title embedded in it.
func (f Frame) Top() string {
	inner := max(0, f.Width-2)
	if Width(f.Title) == 0 {
		return Border.Render("╭" + strings.Repeat("─", inner) + "╮")
	}
	fill := max(0, inner-3-Width(f.Title))
	return Border.Render("╭─ ") + f.Title + Border.Render(" "+strings.Repeat("─", fill)+"╮")
}

// Bottom renders the closing border.
func (f Frame) Bottom() string {
	return Border.Render("╰" + strings.Repeat("─", max(0, f.Width-2)) + "╯")
}

// Row renders one interior line: the side borders, one cell of padding, the
// content fitted to the width, one cell of padding.
func (f Frame) Row(content string) string {
	return Border.Render("│") + " " + Fit(content, f.ContentWidth()) + " " + Border.Render("│")
}

// Rule renders a horizontal rule joined to the frame's sides, so sections
// connect instead of floating inside the box.
func (f Frame) Rule() string {
	return Border.Render("├") + Quiet.Render(strings.Repeat("─", max(0, f.Width-2))) + Border.Render("┤")
}

// Render wraps plain rows into the frame.
func (f Frame) Render(rows []string) string {
	lines := make([]string, 0, len(rows)+2)
	lines = append(lines, f.Top())
	for _, row := range rows {
		lines = append(lines, f.Row(row))
	}
	lines = append(lines, f.Bottom())
	return strings.Join(lines, "\n")
}

// Placeholder renders the calm, centered message a frame shows when it has no
// rows to list: one line naming the state, one dim line naming what to do next.
func (f Frame) Placeholder(message, hint string) []string {
	rows := []string{""}
	rows = append(rows, Center(Strong.Render(message), f.ContentWidth()))
	if hint != "" {
		rows = append(rows, Center(Muted.Render(hint), f.ContentWidth()))
	}
	return append(rows, "")
}

// Layout lays a frame out: the top border, the rows the caller composed, and
// the closing border.
func Layout(f Frame, rows ...string) string {
	lines := make([]string, 0, len(rows)+2)
	lines = append(lines, f.Top())
	lines = append(lines, rows...)
	lines = append(lines, f.Bottom())
	return strings.Join(lines, "\n")
}

// Float places a rendered block in a window the way a popup opens: centered on
// both axes, with the block's own edges as the only boundary. A block wider
// than the window is left at the left edge rather than clipped, so a caller
// that measured its frame keeps the whole frame.
func Float(block string, width, height int) string {
	lines := strings.Split(block, "\n")
	blockWidth := 0
	for _, line := range lines {
		blockWidth = max(blockWidth, Width(line))
	}
	left := max(0, (width-blockWidth)/2)
	top := max(0, (height-len(lines))/2)

	padding := strings.Repeat(" ", left)
	var out strings.Builder
	for i := 0; i < top; i++ {
		out.WriteString("\n")
	}
	for i, line := range lines {
		if i > 0 {
			out.WriteString("\n")
		}
		if line != "" {
			out.WriteString(padding)
			out.WriteString(line)
		}
	}
	return out.String()
}

// Guide renders the tree guide of a list entry: two cells, joined to the last
// entry of its group so the list closes instead of trailing off.
func Guide(role lipgloss.Style, last bool) string {
	if last {
		return "  " + role.Render("└─")
	}
	return "  " + role.Render("├─")
}

// Row renders one listed entry: the indent and tree guide its position asks for,
// then the body the caller composed. On a selected row the indent is tinted, so
// the background reaches the row's edge, and the caller wraps the result in
// SelectedRow to add the accent edge.
func Row(roles Roles, last bool, body string) string {
	guide := "├─"
	if last {
		guide = "└─"
	}
	return roles.Space.Render("  ") + roles.Guide.Render(guide) + roles.Space.Render(" ") + body
}

// SelectedRow paints the selected row: the accent edge in the first cell, then
// the row itself, which keeps every element's colour, and the tint carried to
// the row's width so the highlight has no ragged end.
func SelectedRow(row string, width int) string {
	if width <= 0 {
		return ""
	}
	// The edge takes the row's first cell, so every column keeps its x: the
	// indent the row already spent becomes the mark.
	body := ansi.TruncateLeft(row, 1, "")
	if pad := width - 1 - Width(body); pad > 0 {
		body += SelectionBG.Render(strings.Repeat(" ", pad))
	} else if Width(body) > width-1 {
		body = Clip(body, width-1)
	}
	return SelectionEdge.Render(SelectMark) + body
}

// Footer joins the detail of the selected item with its short summary: the
// summary sits flush with the inner edge, a plain gutter separates the two, and
// the left side is clipped first so the summary never moves.
func Footer(left, right string, width int) string {
	if right == "" {
		return Clip(left, width)
	}
	if Width(left)+2+Width(right) > width {
		left = Clip(left, max(0, width-Width(right)-2))
	}
	if gap := width - Width(left) - Width(right); gap > 0 {
		return left + strings.Repeat(" ", gap) + right
	}
	return right
}

// Section renders a group header: the filled gold glyph that opens a group (the
// same glyph family the switcher marks its state groups with), its name in small
// caps and how many entries it holds.
func Section(label string, count int) string {
	return Gold.Render(Live) + " " + GoldBold.Render(strings.ToUpper(label)) + "  " + Muted.Render(itoa(count))
}
