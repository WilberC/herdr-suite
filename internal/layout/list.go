package layout

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Entry is one layout a scope holds, with what the list prints about it: the
// name it answers to, where it was stored, and what it holds. Workspace is the
// first stored workspace and Workspaces how many the layout holds.
type Entry struct {
	Name       string
	Scope      string
	Path       string
	Workspace  string
	Workspaces int
	Tabs       int
	Panes      int
	Modified   time.Time
}

// workspaceCell is what the list prints in its workspace column: the label, and
// how many further workspaces a session layout holds.
func (e Entry) workspaceCell() string {
	if e.Workspaces > 1 {
		return fmt.Sprintf("%s +%d", e.Workspace, e.Workspaces-1)
	}
	return e.Workspace
}

// List returns every layout visible from sources: the project's first, then the
// global store, each scope sorted by name.
func List(sources Sources) ([]Entry, error) {
	project, err := projectEntries(sources)
	if err != nil {
		return nil, err
	}
	global, err := directoryEntries(sources.Global, ScopeGlobal)
	if err != nil {
		return nil, err
	}
	return append(project, global...), nil
}

// projectEntries reads the project declaration: its layout directory and the
// layouts its index points at.
func projectEntries(sources Sources) ([]Entry, error) {
	if sources.Project == "" {
		return nil, nil
	}
	entries, err := directoryEntries(sources.Project, ScopeProject)
	if err != nil {
		return nil, err
	}

	if sources.Index != "" {
		declared, err := readIndex(sources.Index)
		if err != nil {
			return nil, err
		}
		for _, entry := range declared {
			listed, err := readEntry(entry.Path, entry.Name, ScopeProject)
			if err != nil {
				return nil, fmt.Errorf("%s: the index declares '%s' at %s: %w", sources.Index, entry.Name, entry.Path, err)
			}
			entries = append(entries, listed)
		}
	}
	sortEntries(entries)
	return entries, nil
}

// directoryEntries reads the layouts stored in dir. A directory that does not
// exist holds none.
func directoryEntries(dir, scope string) ([]Entry, error) {
	if dir == "" {
		return nil, nil
	}
	items, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}

	var entries []Entry
	for _, item := range items {
		if item.IsDir() || !strings.HasSuffix(item.Name(), ".json") {
			continue
		}
		name := strings.TrimSuffix(item.Name(), ".json")
		path := filepath.Join(dir, item.Name())
		entry, err := readEntry(path, name, scope)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	sortEntries(entries)
	return entries, nil
}

// readEntry reads one stored layout and its file metadata.
func readEntry(path, name, scope string) (Entry, error) {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Entry{}, NotFoundError{Path: path}
		}
		return Entry{}, fmt.Errorf("read %s: %w", path, err)
	}
	document, _, err := LoadFile(path)
	if err != nil {
		return Entry{}, err
	}

	entry := Entry{Name: name, Scope: scope, Path: path, Modified: info.ModTime()}
	spaces := document.Spaces()
	if len(spaces) > 0 {
		entry.Workspace = spaces[0].Label
		entry.Workspaces = len(spaces)
	}
	for _, workspace := range spaces {
		entry.Tabs += len(workspace.Tabs)
		for _, tab := range workspace.Tabs {
			entry.Panes += countPanes(tab.Root)
		}
	}
	return entry, nil
}

func sortEntries(entries []Entry) {
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].Name != entries[j].Name {
			return entries[i].Name < entries[j].Name
		}
		return entries[i].Path < entries[j].Path
	})
}

// WriteList prints the layout table: a header row and one row per layout, in the
// order given, with every column padded to its widest value. The table is the
// stable, scriptable form: nothing is colored and the modified time is UTC.
func WriteList(w io.Writer, entries []Entry) error {
	header := []string{"NAME", "SCOPE", "WORKSPACE", "TABS", "PANES", "MODIFIED"}
	rows := make([][]string, 0, len(entries))
	for _, entry := range entries {
		rows = append(rows, []string{
			entry.Name,
			entry.Scope,
			entry.workspaceCell(),
			fmt.Sprint(entry.Tabs),
			fmt.Sprint(entry.Panes),
			entry.Modified.UTC().Format("2006-01-02 15:04"),
		})
	}

	widths := make([]int, len(header))
	for index, cell := range header {
		widths[index] = len(cell)
	}
	for _, row := range rows {
		for index, cell := range row {
			if len(cell) > widths[index] {
				widths[index] = len(cell)
			}
		}
	}

	if _, err := fmt.Fprintln(w, tableLine(header, widths)); err != nil {
		return err
	}
	for _, row := range rows {
		if _, err := fmt.Fprintln(w, tableLine(row, widths)); err != nil {
			return err
		}
	}
	return nil
}

// tableLine pads every column but the last and joins them with two spaces.
func tableLine(cells []string, widths []int) string {
	parts := make([]string, 0, len(cells))
	for index, cell := range cells {
		if index == len(cells)-1 {
			parts = append(parts, cell)
			continue
		}
		parts = append(parts, cell+strings.Repeat(" ", widths[index]-len(cell)))
	}
	return strings.Join(parts, "  ")
}

// ListDocument is the document "hs space list --json" prints.
type ListDocument struct {
	Layouts []ListRow `json:"layouts"`
}

// ListRow is one entry of the layout list document.
type ListRow struct {
	Name       string `json:"name"`
	Scope      string `json:"scope"`
	Path       string `json:"path"`
	Workspace  string `json:"workspace"`
	Workspaces int    `json:"workspaces"`
	Tabs       int    `json:"tabs"`
	Panes      int    `json:"panes"`
	Modified   string `json:"modified"`
}

// WriteListJSON prints the layout list as JSON, indented for reading.
func WriteListJSON(w io.Writer, entries []Entry) error {
	document := ListDocument{Layouts: make([]ListRow, 0, len(entries))}
	for _, entry := range entries {
		document.Layouts = append(document.Layouts, ListRow{
			Name:       entry.Name,
			Scope:      entry.Scope,
			Path:       entry.Path,
			Workspace:  entry.Workspace,
			Workspaces: entry.Workspaces,
			Tabs:       entry.Tabs,
			Panes:      entry.Panes,
			Modified:   entry.Modified.UTC().Format(time.RFC3339),
		})
	}

	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(document)
}
