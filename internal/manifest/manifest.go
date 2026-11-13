// Package manifest reads the plugin manifest, herdr-plugin.toml, for the fields
// the binary reports at runtime: the plugin version and the herdr version floor.
//
// herdr reads the whole manifest with a TOML parser, and this package does not
// try to replace it. Parse is a structural reader: it collects the top-level
// string keys it needs and ignores everything else, including the tables and the
// multi-line arrays the manifest holds. That keeps the binary free of a TOML
// dependency, and the repository's tests keep validating the whole file with
// python's tomllib, so a manifest this reader accepts but herdr rejects still
// fails the suite.
package manifest

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// FileName is the name herdr requires of a plugin manifest.
const FileName = "herdr-plugin.toml"

// Manifest is the part of the manifest the binary reads.
type Manifest struct {
	ID              string
	Name            string
	Version         string
	MinHerdrVersion string
}

// required names the keys a manifest must declare for the binary to report it.
var required = []string{"id", "version", "min_herdr_version"}

// Parse reads the fields the binary reports from manifest bytes. A key declared
// twice, a value that is not a quoted string, or a missing required key is an
// error; anything else in the file is left to herdr.
func Parse(data []byte) (Manifest, error) {
	values := make(map[string]string, 4)
	section := ""

	for index, raw := range strings.Split(string(data), "\n") {
		line := stripComment(raw)
		if strings.TrimSpace(line) == "" {
			continue
		}
		if strings.HasPrefix(line, "[") {
			section = line
			continue
		}
		if section != "" {
			continue
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if !reported(key) {
			continue
		}
		value = strings.TrimSpace(value)
		if len(value) < 2 || value[0] != '"' || value[len(value)-1] != '"' {
			return Manifest{}, fmt.Errorf("line %d: %s must be a quoted string", index+1, key)
		}
		if _, again := values[key]; again {
			return Manifest{}, fmt.Errorf("line %d: %s is declared twice", index+1, key)
		}
		values[key] = value[1 : len(value)-1]
	}

	for _, key := range required {
		if values[key] == "" {
			return Manifest{}, fmt.Errorf("%s declares no %s", FileName, key)
		}
	}
	return Manifest{
		ID:              values["id"],
		Name:            values["name"],
		Version:         values["version"],
		MinHerdrVersion: values["min_herdr_version"],
	}, nil
}

// reported names the keys Parse collects: the ones the binary prints and the
// display name doctor shows.
func reported(key string) bool {
	switch key {
	case "id", "name", "version", "min_herdr_version":
		return true
	}
	return false
}

// stripComment removes a trailing '#' comment. The manifest's string values
// never hold a '#', so the first one outside a quote ends the line; a '#' inside
// quotes is kept.
func stripComment(line string) string {
	quoted := false
	for index, r := range line {
		switch {
		case r == '"':
			quoted = !quoted
		case r == '#' && !quoted:
			return line[:index]
		}
	}
	return line
}

// Read locates nothing and reads the manifest at path, naming the file in every
// error it reports.
func Read(path string) (Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, fmt.Errorf("read %s: %w", path, err)
	}
	document, err := Parse(data)
	if err != nil {
		return Manifest{}, fmt.Errorf("%s: %w", path, err)
	}
	return document, nil
}

// Locate returns the manifest an installation ships: the explicit path when one
// was given, otherwise the manifest beside the binary — its own directory first,
// then the plugin root above it, which is where bin/hs sits under
// herdr-plugin.toml — and finally the one in the working directory.
func Locate(explicit, executable, cwd string) (string, error) {
	if explicit != "" {
		if !isFile(explicit) {
			return "", fmt.Errorf("no %s at %s", FileName, explicit)
		}
		return explicit, nil
	}

	candidates := make([]string, 0, 3)
	if executable != "" {
		dir := filepath.Dir(executable)
		candidates = append(candidates, filepath.Join(dir, FileName))
		candidates = append(candidates, filepath.Join(dir, "..", FileName))
	}
	if cwd != "" {
		candidates = append(candidates, filepath.Join(cwd, FileName))
	}
	for _, candidate := range candidates {
		candidate = filepath.Clean(candidate)
		if isFile(candidate) {
			return candidate, nil
		}
	}
	if len(candidates) == 0 {
		return "", fmt.Errorf("no %s found", FileName)
	}
	return "", fmt.Errorf("no %s found; looked at %s", FileName, strings.Join(candidates, ", "))
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// Compare orders two dotted versions, the way herdr versions are written: at
// least two numeric parts, compared part by part, and a missing trailing part
// read as zero so 1.0 and 1.0.0 are the same version. A version that is not
// dotted numbers is an error, so a caller reports an unreadable floor instead of
// comparing against a guess.
func Compare(left, right string) (int, error) {
	leftParts, err := parseVersion(left)
	if err != nil {
		return 0, err
	}
	rightParts, err := parseVersion(right)
	if err != nil {
		return 0, err
	}

	for index := 0; index < len(leftParts) || index < len(rightParts); index++ {
		got, want := 0, 0
		if index < len(leftParts) {
			got = leftParts[index]
		}
		if index < len(rightParts) {
			want = rightParts[index]
		}
		switch {
		case got < want:
			return -1, nil
		case got > want:
			return 1, nil
		}
	}
	return 0, nil
}

func parseVersion(version string) ([]int, error) {
	parts := strings.Split(version, ".")
	if len(parts) < 2 {
		return nil, fmt.Errorf("%q is not a dotted version", version)
	}
	numbers := make([]int, 0, len(parts))
	for _, part := range parts {
		number, err := strconv.Atoi(part)
		if err != nil || number < 0 {
			return nil, fmt.Errorf("%q is not a dotted version", version)
		}
		numbers = append(numbers, number)
	}
	return numbers, nil
}
