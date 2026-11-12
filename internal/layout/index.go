package layout

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// indexEntry is one layout a project index declares: the name it answers to and
// the file it is stored in, resolved against the directory holding the index.
type indexEntry struct {
	Name string
	Path string
}

// readIndex reads a project index and returns the layouts it declares, in the
// order it declares them.
func readIndex(path string) ([]indexEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read the project index: %w", err)
	}
	return parseIndex(path, data)
}

// parseIndex reads the narrow TOML subset a project index uses: one [layouts]
// table, and one name = "path" entry per layout, with '#' comments. Anything
// else is refused, so the file cannot drift into a format this package guesses
// at.
func parseIndex(path string, data []byte) ([]indexEntry, error) {
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	section := false
	declared := make(map[string]bool)
	var entries []indexEntry

	for offset, raw := range strings.Split(string(data), "\n") {
		number := offset + 1
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			if line != "[layouts]" {
				return nil, fmt.Errorf("%s:%d: unknown table '%s'; the index holds one [layouts] table", base, number, line)
			}
			if section {
				return nil, fmt.Errorf("%s:%d: the [layouts] table is declared twice", base, number)
			}
			section = true
			continue
		}
		if !section {
			return nil, fmt.Errorf("%s:%d: expected the [layouts] table before any entry", base, number)
		}

		line = stripComment(line)
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("%s:%d: expected 'name = \"path\"'", base, number)
		}
		name := strings.TrimSpace(key)
		if !validIndexName(name) {
			return nil, fmt.Errorf("%s:%d: expected 'name = \"path\"'", base, number)
		}
		relative, ok := quotedValue(strings.TrimSpace(value))
		if !ok {
			return nil, fmt.Errorf("%s:%d: '%s' needs a double-quoted path", base, number, name)
		}
		if declared[name] {
			return nil, fmt.Errorf("%s:%d: '%s' is already declared", base, number, name)
		}
		declared[name] = true
		resolved := filepath.Join(dir, relative)
		if outside, err := leavesDir(dir, resolved); err != nil || outside {
			return nil, fmt.Errorf("%s:%d: '%s' must name a file inside %s", base, number, name, dir)
		}
		entries = append(entries, indexEntry{Name: name, Path: resolved})
	}
	return entries, nil
}

// leavesDir reports whether path resolves outside dir, so an index cannot point
// a name at a file the project does not commit.
func leavesDir(dir, path string) (bool, error) {
	relative, err := filepath.Rel(dir, path)
	if err != nil {
		return false, err
	}
	return relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)), nil
}

// validIndexName reports whether name is a layout name the index may declare.
func validIndexName(name string) bool {
	if validateName(name) != nil {
		return false
	}
	return !strings.ContainsAny(name, "\t ")
}

// quotedValue reads the double-quoted string an index entry holds. Escape
// sequences are not part of the subset: a path is written as it is.
func quotedValue(value string) (string, bool) {
	if len(value) < 3 || value[0] != '"' || value[len(value)-1] != '"' {
		return "", false
	}
	inner := value[1 : len(value)-1]
	if inner == "" || strings.ContainsRune(inner, '"') {
		return "", false
	}
	return inner, true
}

// stripComment drops a trailing '#' comment, and keeps a '#' inside a quoted
// path.
func stripComment(line string) string {
	quoted := false
	for index, r := range line {
		switch {
		case r == '"':
			quoted = !quoted
		case r == '#' && !quoted:
			return strings.TrimSpace(line[:index])
		}
	}
	return line
}
