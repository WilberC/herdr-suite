package layout

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// IsPath reports whether a reference names a layout file by path instead of by a
// stored name. A reference is a path when it contains a separator, so a plain
// name stays a name even when it holds a dot.
func IsPath(reference string) bool {
	return strings.ContainsRune(reference, '/') || strings.ContainsRune(reference, filepath.Separator)
}

// ScopePath is where a layout resolved by an explicit path lives: a file the
// sources do not declare.
const ScopePath = "path"

// ScopeOf names where a resolved layout lives: the project declaration when the
// file sits under the project's .herdr, the global store when it sits under that
// directory, and ScopePath otherwise. It is what the show view prints beside the
// layout name, so a reader sees which rule resolved the file.
func ScopeOf(sources Sources, path string) string {
	if sources.Global != "" && within(sources.Global, path) {
		return ScopeGlobal
	}
	if sources.Project != "" && within(filepath.Dir(sources.Project), path) {
		return ScopeProject
	}
	return ScopePath
}

// within reports whether path sits in dir or below it.
func within(dir, path string) bool {
	relative, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

// AmbiguousError reports a name that more than one place declares.
type AmbiguousError struct {
	Name       string
	Candidates []Entry
}

func (e AmbiguousError) Error() string {
	places := make([]string, 0, len(e.Candidates))
	for _, candidate := range e.Candidates {
		places = append(places, candidate.Scope+" "+candidate.Path)
	}
	return fmt.Sprintf("layout '%s' is declared in %d places: %s; remove one or load the file by path",
		e.Name, len(e.Candidates), strings.Join(places, ", "))
}

// Resolve reads the layout a reference names. The order is: an explicit path,
// then the project declaration, then the global store. A name the project does
// not declare falls through to the global store, and a name the project declares
// twice is refused with every candidate listed.
func Resolve(sources Sources, reference string) (Layout, string, error) {
	if IsPath(reference) {
		return LoadFile(reference)
	}
	if err := validateName(reference); err != nil {
		return Layout{}, "", err
	}

	project, err := projectCandidates(sources, reference)
	if err != nil {
		return Layout{}, "", err
	}
	switch len(project) {
	case 1:
		return LoadFile(project[0].Path)
	case 0:
	default:
		return Layout{}, "", AmbiguousError{Name: reference, Candidates: project}
	}

	global := filepath.Join(sources.Global, FileName(reference))
	if fileExists(global) {
		return LoadFile(global)
	}
	return Layout{}, global, NotFoundError{Path: global}
}

// projectCandidates lists the project files that answer to name: the layout the
// project directory stores under it, and the layouts its index declares under it.
func projectCandidates(sources Sources, name string) ([]Entry, error) {
	if sources.Project == "" {
		return nil, nil
	}

	var candidates []Entry
	if path := filepath.Join(sources.Project, FileName(name)); fileExists(path) {
		candidates = append(candidates, Entry{Name: name, Scope: ScopeProject, Path: path})
	}
	if sources.Index != "" {
		declared, err := readIndex(sources.Index)
		if err != nil {
			return nil, err
		}
		for _, entry := range declared {
			if entry.Name == name {
				candidates = append(candidates, Entry{Name: name, Scope: ScopeProject, Path: entry.Path})
			}
		}
	}

	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].Path < candidates[j].Path })
	return candidates, nil
}
