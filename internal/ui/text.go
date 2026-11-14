package ui

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Ellipsis is how a clipped string ends: one character, so a cut reads as a
// cut and never as content.
const Ellipsis = "…"

// Dot separates two facts: spaces on both sides, quiet colour, so it recedes
// between the facts instead of competing with them.
func Dot() string { return Quiet.Render(" · ") }

// Width is the visible width of s: ANSI escapes and styling cost no cells.
func Width(s string) int { return ansi.StringWidth(s) }

// Clip shortens s to width cells, ending with an ellipsis when it had to cut.
func Clip(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if Width(s) <= width {
		return s
	}
	return ansi.Truncate(s, width, Ellipsis)
}

// ClipLeft shortens s from the start, keeping its tail: a path is identified by
// where it ends, not by where it starts.
func ClipLeft(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if Width(s) <= width {
		return s
	}
	drop := Width(s) - (width - Width(Ellipsis))
	return ansi.TruncateLeft(s, drop, Ellipsis)
}

// EllipsisPath shortens a path to width by keeping its trailing segments, the
// way a shell prompt shows the part that identifies it: "…/herdr/work.sock"
// instead of a cut in the middle of a directory name.
func EllipsisPath(path string, width int) string {
	if width <= 0 {
		return ""
	}
	if Width(path) <= width {
		return path
	}
	if width <= Width(Ellipsis)+1 {
		return ClipLeft(path, width)
	}

	segments := strings.Split(path, "/")
	tail := ""
	for i := len(segments) - 1; i >= 0; i-- {
		segment := segments[i]
		if segment == "" {
			continue
		}
		candidate := segment
		if tail != "" {
			candidate = segment + "/" + tail
		}
		if Width(Ellipsis+"/"+candidate) > width {
			break
		}
		tail = candidate
	}
	if tail == "" {
		return ClipLeft(path, width)
	}
	return Ellipsis + "/" + tail
}

// Fit clips s to width cells and pads it with spaces, so the caller can place
// the next column at a fixed x.
func Fit(s string, width int) string {
	clipped := Clip(s, width)
	if pad := width - Width(clipped); pad > 0 {
		return clipped + strings.Repeat(" ", pad)
	}
	return clipped
}

// Join places left at the start and right at the end of width cells. When both
// cannot fit, the left side is clipped first: the right side carries the short
// facts a narrow window must keep.
func Join(left, right string, width int) string {
	if gap := width - Width(left) - Width(right); gap >= 0 {
		return left + strings.Repeat(" ", gap) + right
	}
	left = Clip(left, max(0, width-Width(right)-1))
	gap := width - Width(left) - Width(right)
	if gap < 0 {
		return Clip(right, width)
	}
	return left + strings.Repeat(" ", gap) + right
}

// Center places s in the middle of width cells.
func Center(s string, width int) string {
	s = Clip(s, width)
	if pad := width - Width(s); pad > 0 {
		left := pad / 2
		return strings.Repeat(" ", left) + s + strings.Repeat(" ", pad-left)
	}
	return s
}

// ShortPath replaces the user's home prefix with ~, the way the rest of the
// terminal shortens a path a user recognizes. It is capability-based: no path
// is assumed, and a path outside the home directory is returned unchanged.
func ShortPath(path string) string {
	if path == "" {
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" || home == string(filepath.Separator) {
		return path
	}
	if path == home {
		return "~"
	}
	if rest, ok := strings.CutPrefix(path, home+string(filepath.Separator)); ok {
		return "~/" + rest
	}
	return path
}

// RelativeTime renders how long ago t was, in the compact form a list column
// holds: just now, 5m ago, 2h ago, 3d ago.
func RelativeTime(now, t time.Time) string {
	if t.IsZero() {
		return ""
	}
	elapsed := now.Sub(t)
	switch {
	case elapsed < time.Minute:
		return "just now"
	case elapsed < time.Hour:
		return shortAgo(int(elapsed.Minutes()), "m")
	case elapsed < 24*time.Hour:
		return shortAgo(int(elapsed.Hours()), "h")
	case elapsed < 30*24*time.Hour:
		return shortAgo(int(elapsed.Hours()/24), "d")
	case elapsed < 365*24*time.Hour:
		return shortAgo(int(elapsed.Hours()/(24*30)), "mo")
	default:
		return shortAgo(int(elapsed.Hours()/(24*365)), "y")
	}
}

func shortAgo(value int, unit string) string {
	if value < 1 {
		value = 1
	}
	return itoa(value) + unit + " ago"
}

func itoa(value int) string {
	const digits = "0123456789"
	if value == 0 {
		return "0"
	}
	var out [20]byte
	i := len(out)
	for value > 0 {
		i--
		out[i] = digits[value%10]
		value /= 10
	}
	return string(out[i:])
}

// Live is the glyph a live thing carries: a running session, a focused pane.
const Live = "●"

// Idle is the glyph a quiescent thing carries.
const Idle = "○"

// Mark is the glyph for state that outlives the selection: the default
// session, the focused workspace. It is gold and quieter than the selection.
const Mark = "◆"

// SelectMark caps the selected row. The bar carries the highlight; this one
// cell keeps the row's left edge readable.
const SelectMark = "▌"

// Cursor is the block cursor of the live filter query.
const Cursor = "▏"

// StateGlyph is the glyph a session's running state renders with.
func StateGlyph(running bool) string {
	if running {
		return Green.Render(Live)
	}
	return Muted.Render(Idle)
}

// StateWord names a session's running state on the palette: live is green,
// quiescent is muted.
func StateWord(running bool) string {
	if running {
		return Green.Render("running")
	}
	return Muted.Render("stopped")
}

// lower normalises a herdr state word for the role switch.
func lower(word string) string { return strings.ToLower(word) }

// Label renders a dim label followed by its value, the label/value pair the
// header and the footer are built from.
func Label(label, value string, valueStyle lipgloss.Style) string {
	return Muted.Render(label) + " " + valueStyle.Render(value)
}

// Plain strips styling from s: the selection paints a row it did not compose,
// so it needs the row's text with none of its colours.
func Plain(s string) string { return ansi.Strip(s) }
