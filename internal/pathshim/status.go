package pathshim

import (
	"bytes"
	"os"
	"path/filepath"

	"github.com/WilberC/herdr-suite/internal/backup"
)

// Status is everything "hs path status" reports. It never fails: a broken
// environment becomes a field, so the parts that do work are still printed.
type Status struct {
	Dir    string
	DirErr error

	Path    string
	Present bool
	Matches bool
	Backup  string
	OnPath  bool

	PluginRoot string
	PluginErr  error
}

// Inspect reads the shim, the PATH it would need and the plugin the shim would
// exec. resolve is the read-only half of the shim, injected so a test never
// needs a real herdr.
func Inspect(getenv func(string) string, override string, resolve func() (string, error)) Status {
	status := Status{}

	dir, err := Dir(getenv, override)
	if err != nil {
		status.DirErr = err
	} else {
		status.Dir = dir
		status.Path = filepath.Join(dir, FileName)
		if data, err := os.ReadFile(status.Path); err == nil {
			status.Present = true
			status.Matches = bytes.Equal(data, []byte(Script))
		}
		status.Backup = backup.Latest(status.Path)
		status.OnPath = OnPath(dir, getenv("PATH"))
	}

	if resolve != nil {
		root, err := resolve()
		if err != nil {
			status.PluginErr = err
		} else {
			status.PluginRoot = root
		}
	}
	return status
}
