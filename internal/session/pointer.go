package session

import (
	"strconv"
	"strings"
	"time"
)

// DoubleClickWindow is how long two presses on the same zone stay one gesture. A
// slower second press is a second selection, which is what a user expects of a
// list that also attaches on enter.
const DoubleClickWindow = 400 * time.Millisecond

// Clicks recognises a double click without the terminal's help: the same zone
// pressed twice inside DoubleClickWindow. The clock is the caller's, so a test
// drives one instead of waiting. A double click starts the gesture over, so a
// third press is the first press of the next one.
type Clicks struct {
	last string
	at   time.Time
}

// Double reports whether pressing id at now completes a double click, and records
// the press. It returns false for the first press of a gesture, for a press on a
// different zone, and for a press after the window closed.
func (c *Clicks) Double(id string, now time.Time) bool {
	repeat := id != "" && id == c.last && now.Sub(c.at) <= DoubleClickWindow
	if repeat {
		c.last, c.at = "", time.Time{}
		return true
	}
	c.last, c.at = id, now
	return false
}

// RowZone is the zone id of the listed row that index names: a click anywhere on
// the row selects it.
func RowZone(index int) string { return rowZonePrefix + strconv.Itoa(index) }

// ChildZone is the zone id of a row nested under the listed row that index names.
// A click on it acts on the row it hangs under, never on itself, because a nested
// row is that row's detail rather than an entry of its own.
func ChildZone(index int) string { return RowZone(index) + rowZoneChild }

// RowZoneIndex is the listed row a row or nested-row zone id names.
func RowZoneIndex(id string) (int, bool) {
	rest, ok := strings.CutPrefix(id, rowZonePrefix)
	if !ok {
		return 0, false
	}
	rest, _, _ = strings.Cut(rest, rowZoneChild)
	index, err := strconv.Atoi(rest)
	if err != nil {
		return 0, false
	}
	return index, true
}

const (
	// rowZonePrefix opens the id of a listed row, and rowZoneChild marks a row
	// nested under one.
	rowZonePrefix = "row:"
	rowZoneChild  = ":child"
)
