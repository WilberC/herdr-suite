package layout

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Scopes name where a layout comes from.
const (
	ScopeProject = "project"
	ScopeGlobal  = "global"
)

// Sources is where layouts are found from one working directory: the global
// store, and the declaration of the nearest project that has one. Root, Project
// and Index are empty outside a project.
type Sources struct {
	Global  string
	Project string
	Root    string
	Index   string
}

// Discover resolves the layout sources visible from cwd. The global store comes
// from Dir, and the project declaration is the nearest ".herdr" directory at or
// above cwd that holds a layouts directory or a layouts index, like git finds
// the repository root. The walk stops at the filesystem root.
func Discover(getenv func(string) string, cwd string) (Sources, error) {
	global, err := Dir(getenv)
	if err != nil {
		return Sources{}, err
	}
	sources := Sources{Global: global}
	if cwd == "" {
		return sources, nil
	}

	root, ok, err := findProject(cwd)
	if err != nil || !ok {
		return sources, err
	}
	herdr := filepath.Join(root, ".herdr")
	sources.Root = root
	sources.Project = filepath.Join(herdr, "layouts")
	if fileExists(filepath.Join(herdr, "layouts.toml")) {
		sources.Index = filepath.Join(herdr, "layouts.toml")
	}
	return sources, nil
}

// findProject walks start and its parents until one of them declares layouts.
func findProject(start string) (string, bool, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", false, fmt.Errorf("resolve the working directory: %w", err)
	}
	for {
		declared, err := declaresLayouts(filepath.Join(dir, ".herdr"))
		if err != nil {
			return "", false, err
		}
		if declared {
			return dir, true, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false, nil
		}
		dir = parent
	}
}

// declaresLayouts reports whether herdr holds a layouts directory or a layouts
// index.
func declaresLayouts(herdr string) (bool, error) {
	if dir, err := os.Stat(filepath.Join(herdr, "layouts")); err == nil && dir.IsDir() {
		return true, nil
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return false, fmt.Errorf("read %s: %w", filepath.Join(herdr, "layouts"), err)
	}
	if fileExists(filepath.Join(herdr, "layouts.toml")) {
		return true, nil
	}
	return false, nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
