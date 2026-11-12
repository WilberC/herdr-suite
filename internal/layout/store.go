package layout

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// ExistsError reports a layout that is already stored under the same name.
type ExistsError struct {
	Path string
}

func (e ExistsError) Error() string {
	return fmt.Sprintf("layout already exists at %s; pass --force to overwrite", e.Path)
}

// Dir returns the global layout directory: <XDG_CONFIG_HOME>/herdr/herdr-suite/
// layouts, or the same path under $HOME/.config when XDG_CONFIG_HOME is unset.
// getenv reads the environment, so callers and tests share one resolution.
func Dir(getenv func(string) string) (string, error) {
	if base := getenv("XDG_CONFIG_HOME"); base != "" {
		return filepath.Join(base, "herdr", "herdr-suite", "layouts"), nil
	}
	home := getenv("HOME")
	if home == "" {
		return "", errNoConfigHome
	}
	return filepath.Join(home, ".config", "herdr", "herdr-suite", "layouts"), nil
}

// FileName is the file a layout named name is stored in.
func FileName(name string) string {
	return name + ".json"
}

// Save stores document in dir and returns the path it wrote. A stored layout is
// never replaced unless force is set, and a name that could escape dir is
// refused.
func Save(dir string, document Layout, force bool) (string, error) {
	if err := validateName(document.Name); err != nil {
		return "", err
	}
	data, err := Marshal(document)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create layout directory: %w", err)
	}

	path := filepath.Join(dir, FileName(document.Name))
	flags := os.O_WRONLY | os.O_CREATE | os.O_EXCL
	if force {
		flags = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	}
	file, err := os.OpenFile(path, flags, 0o644)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return "", ExistsError{Path: path}
		}
		return "", fmt.Errorf("create layout: %w", err)
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return "", fmt.Errorf("write layout: %w", err)
	}
	if err := file.Close(); err != nil {
		return "", fmt.Errorf("write layout: %w", err)
	}
	return path, nil
}

// NotFoundError reports a layout that is not stored.
type NotFoundError struct {
	Path string
}

func (e NotFoundError) Error() string {
	return fmt.Sprintf("no stored layout at %s", e.Path)
}

// Load reads the layout named name from dir and returns it with the path it was
// read from. A name that is not stored reports NotFoundError.
func Load(dir, name string) (Layout, string, error) {
	if err := validateName(name); err != nil {
		return Layout{}, "", err
	}
	path := filepath.Join(dir, FileName(name))
	document, path, err := LoadFile(path)
	if err != nil {
		return Layout{}, path, err
	}
	if document.Name != name {
		return Layout{}, path, fmt.Errorf("%s holds layout '%s', want '%s'", path, document.Name, name)
	}
	return document, path, nil
}

// LoadFile reads the layout stored at path, whatever name the document holds.
// It is how an explicit path and a project index entry are read: the reference
// names the file, so the stored name is not a second check.
func LoadFile(path string) (Layout, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Layout{}, path, NotFoundError{Path: path}
		}
		return Layout{}, path, fmt.Errorf("read layout: %w", err)
	}
	document, err := Unmarshal(data)
	if err != nil {
		return Layout{}, path, fmt.Errorf("%s: %w", path, err)
	}
	return document, path, nil
}

// validateName keeps a layout name inside its directory. Names reach this
// package already sanitized, so this is the last guard, not a user-facing rule.
func validateName(name string) error {
	if name == "" || name == "." || name == ".." {
		return fmt.Errorf("invalid layout name '%s'", name)
	}
	for _, r := range name {
		if !validNameRune(r) {
			return fmt.Errorf("invalid layout name '%s'", name)
		}
	}
	return nil
}

func validNameRune(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z':
		return true
	case r >= 'A' && r <= 'Z':
		return true
	case r >= '0' && r <= '9':
		return true
	}
	return r == '.' || r == '_' || r == '-'
}
