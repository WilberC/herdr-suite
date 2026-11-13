package gsrestore

import (
	"fmt"
	"os"
	"path/filepath"
)

// ReporterFileName is the file the reporter is installed as.
const ReporterFileName = "herdr-agent-state.ts"

// ReporterVersionLine is the marker status matches on. It is a single line so
// a reader can diff versions without parsing the reporter.
const ReporterVersionLine = "// HERDR_SUITE_GS_RESTORE_VERSION=1"

// Refusal is a request this package will not carry out because the environment
// does not describe a gentle-shell installation.
type Refusal struct {
	Reason string
}

func (r Refusal) Error() string { return r.Reason }

// Target is where a reporter install writes.
type Target struct {
	AgentDir      string
	ExtensionsDir string
	Path          string
}

// ResolveTarget reads the agent directory from PI_CODING_AGENT_DIR. The value
// is never guessed: a vanilla pi installation has no such variable, so the
// caller gets a refusal naming what is missing instead of a path invented from
// a home directory.
func ResolveTarget(getenv func(string) string) (Target, error) {
	dir := getenv("PI_CODING_AGENT_DIR")
	if dir == "" {
		return Target{}, Refusal{Reason: "PI_CODING_AGENT_DIR is not set; it names the gentle-shell agent directory, and the reporter installs into its extensions/ directory"}
	}
	if !filepath.IsAbs(dir) {
		return Target{}, Refusal{Reason: fmt.Sprintf("PI_CODING_AGENT_DIR = %q is not an absolute path", dir)}
	}

	extensions := filepath.Join(dir, "extensions")
	info, err := os.Stat(extensions)
	if err != nil || !info.IsDir() {
		return Target{}, Refusal{Reason: fmt.Sprintf(
			"%s is not a gentle-shell agent directory: it has no extensions/ directory; create it with 'mkdir -p %s'",
			dir, extensions)}
	}
	return Target{
		AgentDir:      dir,
		ExtensionsDir: extensions,
		Path:          filepath.Join(extensions, ReporterFileName),
	}, nil
}
