package gsrestore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// reporterPath is the reporter this repository ships, read from the package
// test directory.
func reporterPath(t *testing.T) string {
	t.Helper()
	path := filepath.Join("..", "..", "extensions", ReporterFileName)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the shipped reporter is missing at %s: %v", path, err)
	}
	return path
}

func readReporter(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(reporterPath(t))
	if err != nil {
		t.Fatalf("read the shipped reporter: %v", err)
	}
	return string(data)
}

func TestShippedReporterCarriesNoUserPaths(t *testing.T) {
	reporter := readReporter(t)

	for _, banned := range []string{"/home/", "/Users/", "C:\\Users\\", "process.env.HOME", "GENTLE_SHELL_HOME", "~"} {
		if strings.Contains(reporter, banned) {
			t.Fatalf("the shipped reporter contains %q; it must detect nothing about users", banned)
		}
	}
}

func TestShippedReporterKeepsTheVersionMarker(t *testing.T) {
	reporter := readReporter(t)
	if !strings.Contains(reporter, ReporterVersionLine) {
		t.Fatalf("the shipped reporter has no %q line; status matches on it", ReporterVersionLine)
	}
	if version, ok := markerVersion([]byte(reporter)); !ok || version != "v1" {
		t.Fatalf("markerVersion = %q, %v, want v1", version, ok)
	}
}

func TestShippedReporterKeepsTheGentleShellResumeExecution(t *testing.T) {
	reporter := readReporter(t)

	for _, want := range []string{
		`@ts-nocheck`,
		`"gentle-shell", "--", "--session"`,
		`spawnSync`,
		`gentle-pi:notification:source`,
		`mode !== "tui"`,
		`Single reporter rule`,
	} {
		if !strings.Contains(reporter, want) {
			t.Fatalf("the shipped reporter omits %q", want)
		}
	}
	if strings.Contains(reporter, `["gentle-shell", "--session"`) {
		t.Fatal("the shipped reporter drops the -- separator before --session")
	}
}
