// Package doctor reports the health of one hs installation: the herdr binary and
// its version, the herdr socket, the plugin config, this binary against the
// manifest version, the layout store, the GS session-restore files and the
// plugin manifest.
//
// A report is read-only. Every check reads what it can and reports a verdict
// instead of failing the run, so one broken piece still leaves the other lines
// printed. Each line is a Check: a stable id, one Status, and one line of detail
// that names what was read and, when it failed, the fix. A herdr call is bounded
// by Timeout, so a socket that never answers becomes a FAIL line rather than a
// hanging report.
package doctor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/WilberC/herdr-suite/internal/gsrestore"
	"github.com/WilberC/herdr-suite/internal/herdrcli"
	"github.com/WilberC/herdr-suite/internal/layout"
	"github.com/WilberC/herdr-suite/internal/manifest"
)

// Status is the verdict of one reported line.
type Status string

const (
	Pass Status = "PASS"
	Warn Status = "WARN"
	Fail Status = "FAIL"
	Skip Status = "SKIP"
)

// The ids every report carries, in the order Run prints them.
const (
	CheckHerdr           = "herdr"
	CheckSocket          = "socket"
	CheckConfig          = "config"
	CheckBinary          = "binary"
	CheckLayoutStore     = "layouts"
	CheckProject         = "project"
	CheckReporter        = "gs-restore reporter"
	CheckReporterVersion = "gs-restore version"
	CheckBackup          = "gs-restore backup"
	CheckLauncher        = "gs-restore launcher"
	CheckManifest        = "manifest"
)

// DefaultTimeout bounds every herdr call a report makes.
const DefaultTimeout = 3 * time.Second

// devBuild is the version a build without embedded release information reports.
const devBuild = "dev"

// Check is one reported line.
type Check struct {
	ID     string
	Status Status
	Detail string
}

// Report is one run of the doctor. Version is the build version the report was
// produced by, so a saved report says which binary wrote it.
type Report struct {
	Version string
	Checks  []Check
}

// Check returns the line with the given id.
func (r Report) Check(id string) (Check, bool) {
	for _, check := range r.Checks {
		if check.ID == id {
			return check, true
		}
	}
	return Check{}, false
}

// Failed reports whether any line failed. A warning and a skip do not fail a
// report.
func (r Report) Failed() bool {
	for _, check := range r.Checks {
		if check.Status == Fail {
			return true
		}
	}
	return false
}

// ExitCode is the status hs doctor exits with: 0 when nothing failed, 1 when a
// line did.
func (r Report) ExitCode() int {
	if r.Failed() {
		return 1
	}
	return 0
}

// Deps is what a report reads the machine through. Everything is a function or
// a path, so a test breaks exactly one piece.
type Deps struct {
	Version      string
	ManifestPath string
	Executable   string
	Cwd          string
	Getenv       func(string) string
	LookPath     func(string) (string, error)
	RunHerdr     func(ctx context.Context, bin string, args ...string) (stdout, stderr []byte, err error)
	Ping         func(ctx context.Context) (int, error)
	Timeout      time.Duration
}

// Run reads one installation and reports every line.
func Run(d Deps) Report {
	if d.Getenv == nil {
		d.Getenv = os.Getenv
	}
	if d.LookPath == nil {
		d.LookPath = exec.LookPath
	}
	timeout := d.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	document, manifestPath, manifestErr := readManifest(d)
	status := gsrestore.Inspect(d.Getenv, d.LookPath)
	note := agentDirNote(d.Getenv)

	return Report{
		Version: d.Version,
		Checks: []Check{
			checkHerdr(ctx, d, document, manifestErr),
			checkSocket(ctx, d, timeout),
			checkConfig(status),
			checkBinary(d.Version, document, manifestErr),
			checkLayoutStore(d),
			checkProject(d),
			checkReporter(status, note),
			checkReporterVersion(status, note),
			checkBackup(status, note),
			checkLauncher(status, note),
			checkManifest(manifestPath, document, manifestErr),
		},
	}
}

// Write prints one line per check: the status, the check id and its detail,
// padded so the lines line up. Nothing is colored, so the text is what a script
// reads too.
func Write(w io.Writer, report Report) error {
	width := 0
	for _, check := range report.Checks {
		if len(check.ID) > width {
			width = len(check.ID)
		}
	}
	for _, check := range report.Checks {
		if _, err := fmt.Fprintf(w, "%-4s  %-*s  %s\n", check.Status, width, check.ID, check.Detail); err != nil {
			return fmt.Errorf("write the report: %w", err)
		}
	}
	return nil
}

// jsonDocument is the machine-readable form of a report.
type jsonDocument struct {
	Version string      `json:"version"`
	OK      bool        `json:"ok"`
	Checks  []jsonCheck `json:"checks"`
}

type jsonCheck struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

// WriteJSON prints the whole report as JSON, one document, without indenting:
// scripts read it, humans read Write.
func WriteJSON(w io.Writer, report Report) error {
	document := jsonDocument{
		Version: report.Version,
		OK:      !report.Failed(),
		Checks:  make([]jsonCheck, 0, len(report.Checks)),
	}
	for _, check := range report.Checks {
		document.Checks = append(document.Checks, jsonCheck{
			ID:     check.ID,
			Status: string(check.Status),
			Detail: check.Detail,
		})
	}
	data, err := json.Marshal(document)
	if err != nil {
		return fmt.Errorf("encode the report: %w", err)
	}
	if _, err := fmt.Fprintln(w, string(data)); err != nil {
		return fmt.Errorf("write the report: %w", err)
	}
	return nil
}

func fail(check Check, detail string) Check {
	check.Status = Fail
	check.Detail = detail
	return check
}

func warn(check Check, detail string) Check {
	check.Status = Warn
	check.Detail = detail
	return check
}

func skip(check Check, detail string) Check {
	check.Status = Skip
	check.Detail = detail
	return check
}

func pass(check Check, detail string) Check {
	check.Status = Pass
	check.Detail = detail
	return check
}

// readManifest locates and reads the plugin manifest, reporting the path it read
// when it found one and the reason when it did not.
func readManifest(d Deps) (manifest.Manifest, string, error) {
	path, err := manifest.Locate(d.ManifestPath, d.Executable, d.Cwd)
	if err != nil {
		return manifest.Manifest{}, "", err
	}
	document, err := manifest.Read(path)
	if err != nil {
		return manifest.Manifest{}, path, err
	}
	return document, path, nil
}

var versionPattern = regexp.MustCompile(`\d+\.\d+(?:\.\d+)?`)

// checkHerdr resolves the herdr binary, asks it for its version and compares it
// with the floor the manifest declares.
func checkHerdr(ctx context.Context, d Deps, document manifest.Manifest, manifestErr error) Check {
	check := Check{ID: CheckHerdr}
	bin, err := herdrBinary(d)
	if err != nil {
		return fail(check, err.Error())
	}
	if d.RunHerdr == nil {
		return fail(check, fmt.Sprintf("cannot run %s --version: no runner is configured", bin))
	}
	stdout, stderr, err := d.RunHerdr(ctx, bin, "--version")
	if err != nil {
		return fail(check, fmt.Sprintf("cannot run %s --version: %v; install herdr or fix %s", bin, err, herdrcli.EnvBinary))
	}
	version, ok := findVersion(stdout, stderr)
	if !ok {
		return fail(check, fmt.Sprintf("%s --version reported no version: %q; reinstall herdr or fix %s",
			bin, firstLine(stdout, stderr), herdrcli.EnvBinary))
	}

	detail := fmt.Sprintf("herdr %s at %s", version, bin)
	if manifestErr != nil {
		return pass(check, detail+" (min_herdr_version unknown)")
	}
	comparison, err := manifest.Compare(version, document.MinHerdrVersion)
	if err != nil {
		return fail(check, fmt.Sprintf("%v; fix min_herdr_version in %s", err, manifest.FileName))
	}
	if comparison < 0 {
		return fail(check, fmt.Sprintf("herdr %s is older than min_herdr_version %s; update herdr", version, document.MinHerdrVersion))
	}
	return pass(check, fmt.Sprintf("%s (min_herdr_version %s)", detail, document.MinHerdrVersion))
}

// herdrBinary resolves the herdr a plugin action would run: HERDR_BIN_PATH when
// it is set, then PATH.
func herdrBinary(d Deps) (string, error) {
	if path := d.Getenv(herdrcli.EnvBinary); path != "" {
		return path, nil
	}
	path, err := d.LookPath("herdr")
	if err != nil {
		return "", fmt.Errorf("herdr is not on PATH; install it or set %s", herdrcli.EnvBinary)
	}
	return path, nil
}

func findVersion(streams ...[]byte) (string, bool) {
	for _, stream := range streams {
		if match := versionPattern.Find(stream); match != nil {
			return string(match), true
		}
	}
	return "", false
}

func firstLine(streams ...[]byte) string {
	for _, stream := range streams {
		trimmed := strings.TrimSpace(string(stream))
		if trimmed == "" {
			continue
		}
		line, _, _ := strings.Cut(trimmed, "\n")
		return line
	}
	return ""
}

// checkSocket proves herdr answers: one session list, bounded by the timeout.
func checkSocket(ctx context.Context, d Deps, timeout time.Duration) Check {
	check := Check{ID: CheckSocket}
	if d.Ping == nil {
		return skip(check, "no socket probe is configured")
	}
	sessions, err := d.Ping(ctx)
	if err == nil {
		return pass(check, fmt.Sprintf("reachable · %s", plural(sessions, "session")))
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fail(check, fmt.Sprintf("no answer within %s; start a herdr session, or check %s", timeout, herdrcli.EnvSocketPath))
	}
	return fail(check, fmt.Sprintf("unreachable (%v); start a herdr session, or check %s", err, herdrcli.EnvSocketPath))
}

// checkConfig reports the plugin config and the two gs_restore toggles.
func checkConfig(status gsrestore.Status) Check {
	check := Check{ID: CheckConfig}
	if status.ConfigErr != nil {
		if status.ConfigPath != "" {
			return fail(check, fmt.Sprintf("invalid %s: %v; fix or delete the file", status.ConfigPath, status.ConfigErr))
		}
		return fail(check, fmt.Sprintf("%v; set XDG_CONFIG_HOME or HOME", status.ConfigErr))
	}
	detail := status.ConfigPath
	if !status.ConfigPresent {
		detail += " (missing)"
	}
	return pass(check, fmt.Sprintf("%s · gs_restore install %s, auto_install %s",
		detail, toggle(status.Config.Install), toggle(status.Config.AutoInstall)))
}

func toggle(on bool) string {
	if on {
		return "on"
	}
	return "off"
}

// checkBinary catches the stale binary: a built hs that predates the manifest's
// version answers with commands or fixes it does not carry.
func checkBinary(buildVersion string, document manifest.Manifest, manifestErr error) Check {
	check := Check{ID: CheckBinary}
	if buildVersion == "" || buildVersion == devBuild {
		return skip(check, "dev build; no release version embedded")
	}
	if manifestErr != nil {
		return skip(check, "no manifest version to compare against")
	}
	if buildVersion == document.Version {
		return pass(check, fmt.Sprintf("%s matches %s", buildVersion, manifest.FileName))
	}
	return fail(check, fmt.Sprintf("stale binary: embedded %s, manifest %s; run make hs or reinstall the plugin",
		buildVersion, document.Version))
}

// checkLayoutStore reports the global layout directory and how many layouts it
// holds. Only counts are printed, never a layout's contents.
func checkLayoutStore(d Deps) Check {
	check := Check{ID: CheckLayoutStore}
	dir, err := layout.Dir(d.Getenv)
	if err != nil {
		return fail(check, fmt.Sprintf("%v; set XDG_CONFIG_HOME or HOME", err))
	}
	entries, err := layout.List(layout.Sources{Global: dir})
	if err != nil {
		return fail(check, fmt.Sprintf("%v; fix or remove that file", err))
	}
	return pass(check, fmt.Sprintf("%s · %s", dir, plural(len(entries), "layout")))
}

// checkProject summarizes what the current directory declares: the nearest
// project and how many layouts it has, or that none declares any.
func checkProject(d Deps) Check {
	check := Check{ID: CheckProject}
	if d.Cwd == "" {
		return skip(check, "no working directory")
	}
	sources, err := layout.Discover(d.Getenv, d.Cwd)
	if err != nil {
		return fail(check, fmt.Sprintf("%v; fix the project declaration", err))
	}
	if sources.Project == "" && sources.Index == "" {
		return pass(check, "no .herdr declaration above "+d.Cwd)
	}
	entries, err := layout.List(layout.Sources{Project: sources.Project, Index: sources.Index})
	if err != nil {
		return fail(check, fmt.Sprintf("%v; fix or remove that file", err))
	}
	return pass(check, fmt.Sprintf("project %s · %s", sources.Root, plural(len(entries), "layout")))
}

// checkReporter reports whether the GS session-restore reporter is installed.
func checkReporter(status gsrestore.Status, note string) Check {
	check := Check{ID: CheckReporter}
	if status.TargetErr != nil {
		return skip(check, "not applicable: "+note)
	}
	switch {
	case status.Installed:
		return pass(check, "installed at "+status.Target.Path)
	case status.Present:
		return warn(check, status.Target.Path+" holds no herdr reporter marker; run hs gs-restore install --force")
	}
	return skip(check, "not installed (feature default off)")
}

// checkReporterVersion compares the installed reporter with the shipped one.
func checkReporterVersion(status gsrestore.Status, note string) Check {
	check := Check{ID: CheckReporterVersion}
	switch {
	case status.TargetErr != nil:
		return skip(check, "unknown: "+note)
	case !status.Present:
		return skip(check, "no reporter installed")
	case !status.Installed:
		return warn(check, "no version marker in the installed file; run hs gs-restore install --force")
	case status.VersionMatches:
		return pass(check, fmt.Sprintf("matches (%s)", status.InstalledVersion))
	}
	return warn(check, fmt.Sprintf("differs (installed %s, shipped %s); run hs gs-restore install",
		status.InstalledVersion, gsrestore.ReporterVersion()))
}

// checkBackup reports whether a previous reporter was kept next to the file.
func checkBackup(status gsrestore.Status, note string) Check {
	check := Check{ID: CheckBackup}
	if status.TargetErr != nil {
		return skip(check, "unknown: "+note)
	}
	if status.Backup != "" {
		return pass(check, "present at "+status.Backup)
	}
	return pass(check, "none")
}

// checkLauncher reports whether the launcher the installed reporter reports is
// resolvable, which is what herdr will run when it restores a pane.
func checkLauncher(status gsrestore.Status, note string) Check {
	check := Check{ID: CheckLauncher}
	switch {
	case status.TargetErr != nil:
		return skip(check, "unknown: "+note)
	case !status.Present:
		return skip(check, "not checked; no reporter installed")
	case status.GentleShell != "":
		return pass(check, "gentle-shell at "+status.GentleShell)
	}
	return warn(check, "gentle-shell is not on PATH; the installed reporter would report a launcher herdr cannot run")
}

// checkManifest reports the plugin manifest this installation ships.
func checkManifest(path string, document manifest.Manifest, manifestErr error) Check {
	check := Check{ID: CheckManifest}
	if manifestErr != nil {
		if path == "" {
			return fail(check, fmt.Sprintf("%v; reinstall the plugin or pass --manifest <path>", manifestErr))
		}
		return fail(check, fmt.Sprintf("%s: %v; reinstall the plugin or pass --manifest <path>", path, manifestErr))
	}
	name := document.Name
	if name == "" {
		name = document.ID
	}
	return pass(check, fmt.Sprintf("%s · %s %s (min_herdr_version %s)", path, name, document.Version, document.MinHerdrVersion))
}

// agentDirNote explains why the gentle-shell agent directory is unavailable, in
// the words the status command uses.
func agentDirNote(getenv func(string) string) string {
	if getenv("PI_CODING_AGENT_DIR") == "" {
		return "PI_CODING_AGENT_DIR is not set"
	}
	return "PI_CODING_AGENT_DIR is not a gentle-shell agent directory"
}

func plural(count int, noun string) string {
	if count == 1 {
		return fmt.Sprintf("%d %s", count, noun)
	}
	return fmt.Sprintf("%d %ss", count, noun)
}
