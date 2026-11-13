package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/WilberC/herdr-suite/internal/manifest"
)

// version is the build version. A release build receives the tag through
// -ldflags "-X main.version=<tag>"; a working tree keeps the dev default, which
// every comparison reads as "no release version embedded".
var version = "dev"

// devVersion is what a build without embedded release information reports.
const devVersion = "dev"

const versionUsage = `usage: hs version [options]

Print the version this binary was built as: the release tag a published binary
carries, or "dev" for a build from a working tree.

Options:
  --check            Compare with the version herdr-plugin.toml declares
  --json             Print the version, or the check, as JSON
  --manifest <path>  Read that manifest instead of the one beside the binary

--check exits 0 when the two versions match and 1 when they do not, which is how
a stale binary is caught. A dev build embeds no release version, reports "dev
build; no release version embedded" and always exits 0, so it never fails a
working tree.
`

func runVersion(d deps, args []string) int {
	options, err := parseReportArgs("version", args, versionUsage, true)
	if err != nil {
		return report(err, d.err)
	}
	if !options.check {
		if options.json {
			return report(writeVersionJSON(d.out, versionDocument{Version: d.buildVersion()}), d.err)
		}
		fmt.Fprintf(d.out, "hs version %s\n", d.buildVersion())
		return 0
	}

	check, err := inspectVersion(d, options.manifest)
	if err != nil {
		return report(err, d.err)
	}
	if options.json {
		document := versionCheckDocument{
			Version:  check.Version,
			Manifest: check.Manifest,
			OK:       check.OK,
			Detail:   check.Detail,
		}
		if err := writeVersionJSON(d.out, document); err != nil {
			return report(err, d.err)
		}
		return exitCode(check.OK)
	}

	fmt.Fprintln(d.out, check.describe())
	return exitCode(check.OK)
}

// exitCode is the status the version check reports: 0 when the versions agree,
// 1 when the binary is stale.
func exitCode(ok bool) int {
	if ok {
		return 0
	}
	return 1
}

// buildVersion is the version this binary reports, with the dev default applied
// so an unset deps field still reports something a script can read.
func (d deps) buildVersion() string {
	if d.version == "" {
		return devVersion
	}
	return d.version
}

// versionCheck is the comparison "hs version --check" prints and the doctor's
// stale-binary line repeats: the build version, the manifest's, the verdict and
// the one-line detail.
type versionCheck struct {
	Version  string
	Manifest string
	Detail   string
	OK       bool
}

// describe is the stable one-line form of the check: both versions, then the
// verdict.
func (c versionCheck) describe() string {
	parts := []string{"hs version " + c.Version}
	if c.Manifest != "" {
		parts = append(parts, "manifest "+c.Manifest)
	}
	return strings.Join(append(parts, c.Detail), " · ")
}

// inspectVersion compares this build with the manifest version. A dev build
// short-circuits before the manifest is read: it carries no release version, so
// there is nothing to compare and nothing that could fail.
func inspectVersion(d deps, manifestPath string) (versionCheck, error) {
	check := versionCheck{Version: d.buildVersion()}
	if check.Version == devVersion {
		check.OK = true
		check.Detail = "dev build; no release version embedded"
		return check, nil
	}

	path, err := resolveManifest(d, manifestPath)
	if err != nil {
		return versionCheck{}, err
	}
	document, err := manifest.Read(path)
	if err != nil {
		return versionCheck{}, err
	}
	check.Manifest = document.Version
	if document.Version == check.Version {
		check.OK = true
		check.Detail = "ok"
		return check, nil
	}
	check.Detail = "the binary is stale; run make hs or reinstall the plugin"
	return check, nil
}

// resolveManifest finds the manifest this installation ships: --manifest when it
// was given, otherwise the one beside the running binary, then the one in the
// working directory. The error names the places it looked and the flag that
// overrides them.
func resolveManifest(d deps, explicit string) (string, error) {
	executable := ""
	if d.executable != nil {
		if path, err := d.executable(); err == nil {
			executable = path
		}
	}
	cwd := ""
	if d.cwd != nil {
		if dir, err := d.cwd(); err == nil {
			cwd = dir
		}
	}
	path, err := manifest.Locate(explicit, executable, cwd)
	if err != nil {
		return "", fmt.Errorf("%w; pass --manifest <path>", err)
	}
	return path, nil
}

// versionDocument is what "hs version --json" prints.
type versionDocument struct {
	Version string `json:"version"`
}

// versionCheckDocument is what "hs version --check --json" prints.
type versionCheckDocument struct {
	Version  string `json:"version"`
	Manifest string `json:"manifest,omitempty"`
	OK       bool   `json:"ok"`
	Detail   string `json:"detail"`
}

// writeVersionJSON prints one JSON document on one line, so a script can read
// the version without a parser that tolerates pretty printing.
func writeVersionJSON(out io.Writer, document any) error {
	data, err := json.Marshal(document)
	if err != nil {
		return fmt.Errorf("encode the version: %w", err)
	}
	if _, err := fmt.Fprintln(out, string(data)); err != nil {
		return fmt.Errorf("write the version: %w", err)
	}
	return nil
}
