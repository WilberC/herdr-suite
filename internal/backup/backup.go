// Package backup names and finds the timestamped copies an install keeps when it
// replaces a file it did not write.
//
// One pattern serves every surface: <path>.bak-<UTC timestamp>, sortable by name
// so the greatest name is the most recent copy. An install that already keeps a
// backup refreshes it instead of accumulating copies, which is what a reader of
// the directory sees.
package backup

import (
	"path/filepath"
	"sort"
	"time"
)

// TimestampLayout names a backup: deterministic, sortable and free of colons.
const TimestampLayout = "20060102T150405"

// Path is where the copy of path taken at now lives.
func Path(path string, now time.Time) string {
	return path + ".bak-" + now.UTC().Format(TimestampLayout)
}

// Latest is the greatest backup name, which the timestamp suffix makes the most
// recent, or "" when there is none.
func Latest(path string) string {
	matches, err := filepath.Glob(path + ".bak-*")
	if err != nil || len(matches) == 0 {
		return ""
	}
	sort.Strings(matches)
	return matches[len(matches)-1]
}
