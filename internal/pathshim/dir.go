package pathshim

import (
	"fmt"
	"path/filepath"
)

// Dir is where the shim lives: an explicit override, else $HOME/.local/bin. The
// value is never guessed from anything else, so a machine without HOME gets the
// refusal that names --dir instead of a path invented for it.
func Dir(getenv func(string) string, override string) (string, error) {
	if override != "" {
		if !filepath.IsAbs(override) {
			return "", Refusal{Reason: fmt.Sprintf("--dir %q is not an absolute path", override)}
		}
		return filepath.Clean(override), nil
	}
	home := getenv("HOME")
	if home == "" {
		return "", Refusal{Reason: "HOME is not set; it names the home directory that holds .local/bin, so pass --dir <path>"}
	}
	if !filepath.IsAbs(home) {
		return "", Refusal{Reason: fmt.Sprintf("HOME = %q is not an absolute path", home)}
	}
	return filepath.Join(filepath.Clean(home), ".local", "bin"), nil
}

// OnPath reports whether dir is one entry of the PATH a shell would search. It
// compares whole entries, so a parent directory that holds the target is not a
// match: a shell would not find hs there.
func OnPath(dir, pathEnv string) bool {
	target := filepath.Clean(dir)
	for _, entry := range filepath.SplitList(pathEnv) {
		if entry == "" {
			continue
		}
		if filepath.Clean(entry) == target {
			return true
		}
	}
	return false
}

// PathLine is what an install and a status report print about PATH: the fact,
// and the export line to copy when the directory is not searched.
func PathLine(dir string, onPath bool) string {
	if onPath {
		return fmt.Sprintf("%s is in PATH", dir)
	}
	return fmt.Sprintf("%s is not in PATH; add it to your shell with: export PATH=\"%s:$PATH\"", dir, dir)
}
