package gsrestore

import (
	"encoding/json"
	"fmt"
	"io"
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

// StatusDocument is the machine-readable form of Status: one field per value the
// human report prints, so a script reads the same facts the report shows. A
// value the environment does not provide is left empty with its reason in the
// field beside it, instead of the report's prose being parsed.
type StatusDocument struct {
	ConfigPath    string `json:"config_path"`
	ConfigPresent bool   `json:"config_present"`
	ConfigError   string `json:"config_error,omitempty"`

	Install     bool `json:"install"`
	AutoInstall bool `json:"auto_install"`

	AgentDir      string `json:"agent_dir,omitempty"`
	AgentDirError string `json:"agent_dir_error,omitempty"`

	ReporterPath      string `json:"reporter_path,omitempty"`
	ReporterPresent   bool   `json:"reporter_present"`
	ReporterInstalled bool   `json:"reporter_installed"`
	ReporterVersion   string `json:"reporter_version,omitempty"`
	VersionMatches    bool   `json:"version_matches"`

	ShippedVersion string `json:"shipped_version"`
	Backup         string `json:"backup,omitempty"`
	GentleShell    string `json:"gentle_shell,omitempty"`
}

// Document is the machine-readable form of this Status, the document
// "hs gs-restore status --json" prints.
func (status Status) Document() StatusDocument {
	document := StatusDocument{
		ConfigPath:        status.ConfigPath,
		ConfigPresent:     status.ConfigPresent,
		Install:           status.Config.Install,
		AutoInstall:       status.Config.AutoInstall,
		ReporterPresent:   status.Present,
		ReporterInstalled: status.Installed,
		ReporterVersion:   status.InstalledVersion,
		VersionMatches:    status.VersionMatches,
		ShippedVersion:    ReporterVersion(),
		Backup:            status.Backup,
		GentleShell:       status.GentleShell,
	}
	if status.ConfigErr != nil {
		document.ConfigError = status.ConfigErr.Error()
	}
	if status.TargetErr != nil {
		document.AgentDirError = status.TargetErr.Error()
	} else {
		document.AgentDir = status.Target.AgentDir
		document.ReporterPath = status.Target.Path
	}
	return document
}

// WriteJSON prints the report as one JSON document on one line, so a script
// reads it without a parser that tolerates pretty printing.
func WriteJSON(w io.Writer, status Status) error {
	data, err := json.Marshal(status.Document())
	if err != nil {
		return fmt.Errorf("encode the report: %w", err)
	}
	if _, err := fmt.Fprintln(w, string(data)); err != nil {
		return fmt.Errorf("write the report: %w", err)
	}
	return nil
}
