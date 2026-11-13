package gsrestore

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var markerPattern = regexp.MustCompile(`(?m)^// HERDR_SUITE_GS_RESTORE_VERSION=(\d+)[ \t]*$`)

// markerVersion reads the version marker line from reporter bytes.
func markerVersion(data []byte) (string, bool) {
	match := markerPattern.FindSubmatch(data)
	if match == nil {
		return "", false
	}
	return "v" + string(match[1]), true
}

// ReporterVersion is the version of the reporter this build ships.
func ReporterVersion() string {
	version, _ := markerVersion([]byte(ReporterVersionLine))
	return version
}

// SourcePath finds the shipped reporter: next to the executable's plugin root
// (bin/hs beside extensions/) first, then under the working directory.
func SourcePath(exe, cwd string) (string, error) {
	var candidates []string
	if exe != "" {
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), "..", "extensions", ReporterFileName))
	}
	if cwd != "" {
		candidates = append(candidates, filepath.Join(cwd, "extensions", ReporterFileName))
	}
	for _, candidate := range candidates {
		info, err := os.Stat(candidate)
		if err == nil && !info.IsDir() {
			return filepath.Clean(candidate), nil
		}
	}
	return "", fmt.Errorf("the shipped reporter was not found; looked in %s", strings.Join(candidates, ", "))
}

// looksLikeReporter recognizes a herdr pi integration, so replacing it needs no
// --force. A file that is not one is someone else's and is refused without it.
func looksLikeReporter(data []byte) bool {
	text := string(data)
	for _, marker := range []string{
		"HERDR_SUITE_GS_RESTORE_VERSION=",
		"HERDR_INTEGRATION_ID=pi",
		"pane.report_agent_session",
		"report_agent",
		"herdr:pi",
	} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}
