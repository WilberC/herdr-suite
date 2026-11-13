package gsrestore

import (
	"os"
)

// Status is everything the status command reports. It never fails: a broken
// environment is a field so the diagnostic still prints the parts that work.
type Status struct {
	ConfigPath    string
	ConfigPresent bool
	ConfigErr     error
	Config        Config

	Target    Target
	TargetErr error

	Present          bool
	Installed        bool
	InstalledVersion string
	VersionMatches   bool
	Backup           string

	GentleShell string
}

// Inspect reads the config, the agent directory and the installed reporter.
// lookPath is exec.LookPath in production and a PATH-backed lookup in tests.
func Inspect(getenv func(string) string, lookPath func(string) (string, error)) Status {
	status := Status{}

	if path, err := ConfigPath(getenv); err != nil {
		status.ConfigErr = err
	} else {
		status.ConfigPath = path
		document, err := Load(getenv)
		if err != nil {
			status.ConfigErr = err
		} else {
			status.Config = document.Config
			status.ConfigPresent = document.Present
		}
	}

	target, err := ResolveTarget(getenv)
	if err != nil {
		status.TargetErr = err
	} else {
		status.Target = target
		if data, err := os.ReadFile(target.Path); err == nil {
			status.Present = true
			if version, ok := markerVersion(data); ok {
				status.Installed = true
				status.InstalledVersion = version
				status.VersionMatches = version == ReporterVersion()
			}
		}
		status.Backup = LatestBackup(target.Path)
	}

	if lookPath != nil {
		if path, err := lookPath("gentle-shell"); err == nil {
			status.GentleShell = path
		}
	}
	return status
}
