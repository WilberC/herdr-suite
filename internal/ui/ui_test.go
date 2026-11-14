package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// The shared system is what keeps the two TUIs one product, so its guarantees
// are pinned here rather than in either screen: a frame never exceeds its
// width, a hint bar drops instead of wrapping, and a clipped fact keeps an
// ellipsis.

func TestFrameClipsEveryLineToItsWidth(t *testing.T) {
	frame := Frame{Title: GoldBold.Render("hs") + Quiet.Render(" ❯ ") + AccentBold.Render("sessions"), Width: 40}
	rows := []string{
		frame.Row("short"),
		frame.Rule(),
		frame.Row(strings.Repeat("long ", 30)),
	}

	if got, want := Width(frame.Top()), 40; got != want {
		t.Fatalf("top border width = %d, want %d (%q)", got, want, frame.Top())
	}
	if got, want := Width(frame.Bottom()), 40; got != want {
		t.Fatalf("bottom border width = %d, want %d", got, want)
	}
	if got, want := Width(rows[1]), 40; got != want {
		t.Fatalf("rule width = %d, want %d", got, want)
	}
	for _, row := range []string{rows[0], rows[2]} {
		if got, want := Width(row), 40; got != want {
			t.Fatalf("row width = %d, want %d (%q)", got, want, row)
		}
	}
	if !strings.Contains(rows[2], Ellipsis) {
		t.Fatalf("a clipped row does not end with an ellipsis: %q", rows[2])
	}
}

func TestFrameWithoutATitleStillCloses(t *testing.T) {
	frame := Frame{Width: 24}
	if got, want := Width(frame.Top()), 24; got != want {
		t.Fatalf("top border width = %d, want %d", got, want)
	}
}

func TestHintsDropWholeHintsInsteadOfWrapping(t *testing.T) {
	hints := []Hint{
		{Key: "↑↓", Label: "move"},
		{Key: "enter", Label: "attach"},
		{Key: "alt+enter", Label: "create"},
	}

	wide := Hints(80, hints...)
	for _, hint := range hints {
		if !strings.Contains(wide, " "+hint.Key+" ") {
			t.Fatalf("a wide bar dropped %q: %q", hint.Key, wide)
		}
	}
	if Width(wide) > 80 {
		t.Fatalf("hint bar width = %d, want at most 80", Width(wide))
	}

	narrow := Hints(20, hints...)
	if Width(narrow) > 20 {
		t.Fatalf("narrow hint bar width = %d, want at most 20 (%q)", Width(narrow), narrow)
	}
	if strings.Contains(narrow, " alt+enter ") {
		t.Fatalf("the narrowest bar kept the last hint instead of dropping it: %q", narrow)
	}
	if !strings.Contains(narrow, " ↑↓ ") {
		t.Fatalf("the narrowest bar dropped the first hint: %q", narrow)
	}
}

func TestClipAndFitNeverExceedTheWidth(t *testing.T) {
	cases := []struct {
		text  string
		width int
	}{
		{"work", 8},
		{"gentle-shell", 6},
		{AccentBold.Render("styled"), 3},
		{"", 4},
		{"日本語のセッション", 5},
	}
	for _, c := range cases {
		if got := Width(Clip(c.text, c.width)); got > c.width {
			t.Fatalf("Clip(%q, %d) width = %d", c.text, c.width, got)
		}
		if got := Width(Fit(c.text, c.width)); got != c.width {
			t.Fatalf("Fit(%q, %d) width = %d, want %d", c.text, c.width, got, c.width)
		}
		if got := Width(Center(c.text, c.width)); got != c.width {
			t.Fatalf("Center(%q, %d) width = %d, want %d", c.text, c.width, got, c.width)
		}
	}
}

func TestClipLeftKeepsTheTailOfAPath(t *testing.T) {
	path := "/home/ana/.local/share/herdr-suite/layouts/work.json"
	clipped := ClipLeft(path, 24)

	if got, want := Width(clipped), 24; got != want {
		t.Fatalf("width = %d, want %d", got, want)
	}
	if !strings.HasPrefix(clipped, Ellipsis) || !strings.HasSuffix(clipped, "work.json") {
		t.Fatalf("a clipped path kept the wrong end: %q", clipped)
	}
	if got := ClipLeft(path, 200); got != path {
		t.Fatalf("a path that fits was clipped: %q", got)
	}
}

func TestJoinKeepsTheRightSideWhenBothCannotFit(t *testing.T) {
	got := Join("a very long left side indeed", "7 / 8 shown", 20)

	if width := Width(got); width != 20 {
		t.Fatalf("width = %d, want 20 (%q)", width, got)
	}
	if !strings.HasSuffix(got, "7 / 8 shown") {
		t.Fatalf("join dropped the right side: %q", got)
	}
}

// withColors renders for one assertion with the truecolor profile forced, then
// restores the profile the rest of the package runs with.
func withColors(t *testing.T) {
	t.Helper()
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(previous) })
}

// sgr is the escape sequence a style renders, so a test can look for a role in a
// row without depending on the colour arithmetic lipgloss uses.
func sgr(style lipgloss.Style) string {
	rendered := style.Render("x")
	if i := strings.Index(rendered, "x"); i > 0 {
		return rendered[:i]
	}
	return rendered
}

// TestSelectionTintsInsteadOfPainting pins the selection surface: the row gains
// a tint background and every element keeps the style that carries its meaning.
func TestSelectionTintsInsteadOfPainting(t *testing.T) {
	withColors(t)
	roles := RowRoles(true)
	row := SelectedRow(Row(roles, false, roles.Name.Render("work")+"  "+roles.Muted.Render("3 tabs")), 30)

	for name, want := range map[string]string{
		"tint":  sgr(SelectionBG),
		"name":  sgr(TextOnSelection),
		"facts": sgr(MutedOnSelection),
		"edge":  sgr(SelectionEdge),
		"guide": sgr(GuideOnSelection),
	} {
		if !strings.Contains(row, want) {
			t.Fatalf("the selected row does not carry the %s role (%q): %q", name, want, row)
		}
	}
	if got, want := Width(row), 30; got != want {
		t.Fatalf("selected row width = %d, want %d", got, want)
	}
}

// TestSelectionKeepsTheGuideAndTheColumns pins the two things the tint must not
// cost: the tree guide stays readable, and the row's columns keep their x.
func TestSelectionKeepsTheGuideAndTheColumns(t *testing.T) {
	withColors(t)
	body := Text.Render("api")

	plain := Plain(Row(RowRoles(false), false, body))
	selected := Plain(SelectedRow(Row(RowRoles(true), false, body), 24))

	if !strings.Contains(selected, "├─") {
		t.Fatalf("the selected row lost its tree guide: %q", selected)
	}
	// Columns are compared in cells, not bytes: the guide glyphs are multi-byte.
	at := func(row string) int { return Width(row[:strings.Index(row, "api")]) }
	if got, want := at(selected), at(plain); got != want {
		t.Fatalf("the selected row's body starts at cell %d, want %d", got, want)
	}
	if got, want := Width(selected), 24; got != want {
		t.Fatalf("the selected row is %d cells, want %d", got, want)
	}
}

// TestDefaultMarkKeepsGoldOnSelection pins that state which outlives the
// selection keeps its colour: the default marker is gold on the tint too.
func TestDefaultMarkKeepsGoldOnSelection(t *testing.T) {
	withColors(t)
	mark := RowRoles(true).Gold.Render(Mark)

	if !strings.Contains(mark, sgr(OnSelection(Gold))) {
		t.Fatalf("the gold marker lost its colour on the tint: %q", mark)
	}
	if strings.Contains(mark, sgr(Gold)) {
		t.Fatalf("the gold marker is rendered without the tint: %q", mark)
	}
}

// TestChipOnSelectionIsOneStepBrighter pins the token surface on the tint.
func TestChipOnSelectionIsOneStepBrighter(t *testing.T) {
	withColors(t)
	panel := Chip(ChipText, "x")
	tinted := Chip(ChipOnSelection, "x")

	if panel == tinted {
		t.Fatalf("the chip looks the same on both surfaces: %q", tinted)
	}
	if !strings.Contains(tinted, sgr(ChipOnSelection)) {
		t.Fatalf("the chip on the selection does not use its surface: %q", tinted)
	}
	if !strings.Contains(panel, sgr(ChipText)) {
		t.Fatalf("the panel chip does not use its surface: %q", panel)
	}
}

func TestSectionNamesTheGroupInSmallCaps(t *testing.T) {
	section := Plain(Section("project", 2))

	if section != "● PROJECT  2" {
		t.Fatalf("section = %q, want %q", section, "● PROJECT  2")
	}
}

func TestRelativeTimeNamesTheAge(t *testing.T) {
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		when time.Time
		want string
	}{
		{now.Add(-10 * time.Second), "just now"},
		{now.Add(-5 * time.Minute), "5m ago"},
		{now.Add(-2 * time.Hour), "2h ago"},
		{now.Add(-26 * time.Hour), "1d ago"},
		{now.Add(-31 * 24 * time.Hour), "1mo ago"},
		{now.Add(-400 * 24 * time.Hour), "1y ago"},
		{time.Time{}, ""},
	}
	for _, c := range cases {
		if got := RelativeTime(now, c.when); got != c.want {
			t.Fatalf("RelativeTime(%v) = %q, want %q", c.when, got, c.want)
		}
	}
}

func TestAgentStateUsesTheStateRoles(t *testing.T) {
	lipgloss.SetColorProfile(0)
	for state, want := range map[string]string{
		"":        "",
		"working": "working",
		"idle":    "idle",
		"waiting": "waiting",
	} {
		if got := Plain(RowRoles(false).AgentState(state)); got != want {
			t.Fatalf("AgentState(%q) = %q, want %q", state, got, want)
		}
	}
}

// TestChipIsAPaddedSlab pins the chip shape: the background carries the padding
// and nothing else, so a chip never looks like a bracketed word.
func TestChipIsAPaddedSlab(t *testing.T) {
	chip := Plain(Chip(ChipText, "enter"))

	if chip != " enter " {
		t.Fatalf("chip = %q, want %q", chip, " enter ")
	}
	if strings.ContainsAny(chip, "[]") {
		t.Fatalf("chip carries brackets: %q", chip)
	}
}

// TestFooterKeepsTheSummaryFlush pins the footer contract: the summary ends at
// the inner edge, on the same line, with a plain gutter before it.
func TestFooterKeepsTheSummaryFlush(t *testing.T) {
	footer := Plain(Footer(AccentBold.Render("work")+" "+Muted.Render("running"), "4 workspaces", 40))

	if got, want := Width(footer), 40; got != want {
		t.Fatalf("footer width = %d, want %d", got, want)
	}
	if !strings.HasSuffix(footer, "4 workspaces") {
		t.Fatalf("footer does not end with the summary: %q", footer)
	}
	gutter := strings.TrimSuffix(footer, "4 workspaces")
	if len(gutter) < len("work running")+2 || strings.TrimSpace(gutter) != "work running" {
		t.Fatalf("footer has no plain gutter before the summary: %q", footer)
	}
	if strings.Contains(footer, "·") {
		t.Fatalf("footer invented a separator: %q", footer)
	}

	long := Plain(Footer(strings.Repeat("fact ", 12), "4 workspaces", 40))
	if got, want := Width(long), 40; got != want {
		t.Fatalf("a clipped footer is %d cells wide, want %d (%q)", got, want, long)
	}
	if !strings.HasSuffix(long, "4 workspaces") {
		t.Fatalf("clipping dropped the summary: %q", long)
	}
}

// TestSelectedRowStartsAtTheBarsEdge pins the selection cap: it is the bar's own
// first cell, and the row keeps every other cell it had.
func TestSelectedRowStartsAtTheBarsEdge(t *testing.T) {
	row := "├─ work  ◆"
	selected := Plain(SelectedRow(row, 20))

	if got, want := Width(selected), 20; got != want {
		t.Fatalf("selected row width = %d, want %d", got, want)
	}
	if !strings.HasPrefix(selected, SelectMark) {
		t.Fatalf("the bar does not open with the cap: %q", selected)
	}
	if !strings.Contains(selected, "work  ◆") {
		t.Fatalf("the selected row lost its content: %q", selected)
	}
}
