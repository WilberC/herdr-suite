package session

import (
	"testing"
	"time"
)

// TestClicksCountsASecondPressInsideTheWindow pins the gesture the screens share:
// the same zone pressed twice inside the window is a double click, and a double
// click starts the gesture over so a third press is not a second double click.
func TestClicksCountsASecondPressInsideTheWindow(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	var clicks Clicks

	if clicks.Double(RowZone(1), start) {
		t.Fatalf("the first press of a gesture was a double click")
	}
	if !clicks.Double(RowZone(1), start.Add(DoubleClickWindow)) {
		t.Fatalf("a second press on the same row %v later was not a double click", DoubleClickWindow)
	}
	if clicks.Double(RowZone(1), start.Add(DoubleClickWindow+time.Millisecond)) {
		t.Fatalf("the press after a double click was counted as the double click's partner")
	}
	if !clicks.Double(RowZone(1), start.Add(DoubleClickWindow+100*time.Millisecond)) {
		t.Fatalf("two presses 99ms apart were not a double click")
	}
}

// TestClicksForgivesTheWrongRowAndALongPause pins what is not a double click: a
// press on another row, and a press after the window closed.
func TestClicksForgivesTheWrongRowAndALongPause(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	var clicks Clicks

	if clicks.Double(RowZone(1), start) {
		t.Fatalf("the first press was a double click")
	}
	if clicks.Double(RowZone(2), start.Add(50*time.Millisecond)) {
		t.Fatalf("two presses on different rows were a double click")
	}
	if clicks.Double(RowZone(2), start.Add(50*time.Millisecond+DoubleClickWindow+time.Millisecond)) {
		t.Fatalf("two presses further apart than the window were a double click")
	}
}

// TestRowZoneRoundTripsAnIndexOfARowAndOfANestedRow pins the zone ids the screens
// share: a row and a row nested under it both name the row they belong to, and
// nothing else names a row.
func TestRowZoneRoundTripsAnIndex(t *testing.T) {
	for _, index := range []int{0, 3, 41} {
		for _, id := range []string{RowZone(index), ChildZone(index)} {
			got, ok := RowZoneIndex(id)
			if !ok || got != index {
				t.Fatalf("RowZoneIndex(%q) = %d, %v; want %d, true", id, got, ok, index)
			}
		}
	}
	for _, id := range []string{"", "row:", "row:x", "chip:enter", "chip:page", "session:1"} {
		if _, ok := RowZoneIndex(id); ok {
			t.Fatalf("RowZoneIndex(%q) named a row", id)
		}
	}
}
