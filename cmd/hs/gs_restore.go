package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/WilberC/herdr-suite/internal/gsrestore"
)

const gsRestoreUsage = `usage: hs gs-restore <command>

Commands:
  install [--force]  Install the GS session-restore reporter
  remove             Restore the previous reporter, or remove ours
  status             Report the reporter, the backup and the config toggles
  startup            Apply the config gate; herdr runs this at server start

The feature is default off: the startup gate does nothing until gs_restore.install
is true in <plugin-config>/config.json.

The reporter overrides how a pane restores its session, so a gentle-shell session
comes back through the gentle-shell launcher instead of vanilla pi. It installs
into the gentle-shell agent directory named by PI_CODING_AGENT_DIR; a vanilla pi
installation has no such directory and the feature does not apply there.
`

const gsRestoreInstallUsage = `usage: hs gs-restore install [--force]

Copy the shipped reporter into $PI_CODING_AGENT_DIR/extensions. An existing
herdr reporter is backed up next to it first, and a repeated install refreshes
that single backup. A file that is not a herdr reporter is refused unless
--force is passed.

Options:
  --force  Replace a file that is not a herdr reporter, keeping a backup
`

const gsRestoreRemoveUsage = `usage: hs gs-restore remove

Put the latest backup back in place of the reporter, or delete the reporter
when no backup exists. A removal with nothing installed reports that and exits 0.
`

const gsRestoreStatusUsage = `usage: hs gs-restore status

Report whether the reporter is installed, whether its version marker matches
this build, whether a backup exists, whether the gentle-shell launcher is
resolvable, and the config toggle states.
`

const gsRestoreStartupUsage = `usage: hs gs-restore startup

Apply the config gate. With gs_restore.install false it does nothing; with
install true and auto_install true it installs idempotently and prints a brief
line when it changes anything; with install true and auto_install false it
prints one hint per server socket. It never blocks a herdr start.
`

func runGSRestore(d deps, args []string) int {
	if len(args) == 0 {
		fmt.Fprint(d.err, gsRestoreUsage)
		return 2
	}

	switch args[0] {
	case "install":
		return runGSRestoreInstall(d, args[1:])
	case "remove":
		return runGSRestoreRemove(d, args[1:])
	case "status":
		return runGSRestoreStatus(d, args[1:])
	case "startup":
		return runGSRestoreStartup(d, args[1:])
	}

	fmt.Fprintf(d.err, "unknown gs-restore command %q\n\n%s", args[0], gsRestoreUsage)
	return 2
}

func runGSRestoreInstall(d deps, args []string) int {
	force := false
	for _, arg := range args {
		switch {
		case arg == "--force":
			force = true
		case strings.HasPrefix(arg, "-"):
			return report(usageError{reason: fmt.Sprintf("unknown flag %q", arg), usage: gsRestoreInstallUsage}, d.err)
		default:
			return report(usageError{reason: "install takes no arguments", usage: gsRestoreInstallUsage}, d.err)
		}
	}

	result, err := installReporter(d, force)
	if err != nil {
		return report(err, d.err)
	}
	if result.Already {
		fmt.Fprintf(d.out, "installed: %s (already current)\nversion:   %s\n", result.Path, gsrestore.ReporterVersion())
		return 0
	}
	if result.Backup != "" {
		fmt.Fprintf(d.out, "backup:    %s\n", result.Backup)
	}
	fmt.Fprintf(d.out, "installed: %s\nversion:   %s\n", result.Path, gsrestore.ReporterVersion())
	return 0
}

// installReporter is the install every surface shares: resolve the target and
// the shipped file, then write it.
func installReporter(d deps, force bool) (gsrestore.InstallResult, error) {
	target, err := gsrestore.ResolveTarget(d.getenv)
	if err != nil {
		return gsrestore.InstallResult{}, err
	}
	source, err := d.reporterSource()
	if err != nil {
		return gsrestore.InstallResult{}, err
	}
	reporter, err := os.ReadFile(source)
	if err != nil {
		return gsrestore.InstallResult{}, fmt.Errorf("read the shipped reporter %s: %w", source, err)
	}
	return gsrestore.Install(target, reporter, force, d.now())
}

func runGSRestoreRemove(d deps, args []string) int {
	if len(args) != 0 {
		return report(usageError{reason: "remove takes no arguments", usage: gsRestoreRemoveUsage}, d.err)
	}
	target, err := gsrestore.ResolveTarget(d.getenv)
	if err != nil {
		return report(err, d.err)
	}
	result, err := gsrestore.Remove(target)
	if err != nil {
		return report(err, d.err)
	}

	switch {
	case result.Restored:
		fmt.Fprintf(d.out, "restored: %s\nfrom:     %s\n", result.Path, result.Backup)
	case result.Removed:
		fmt.Fprintf(d.out, "removed: %s\n", result.Path)
	default:
		fmt.Fprintf(d.out, "nothing to remove at %s\n", result.Path)
	}
	return 0
}

func runGSRestoreStatus(d deps, args []string) int {
	if len(args) != 0 {
		return report(usageError{reason: "status takes no arguments", usage: gsRestoreStatusUsage}, d.err)
	}
	writeGSStatus(d.out, gsrestore.Inspect(d.getenv, d.lookPath), d.getenv)
	return 0
}

func runGSRestoreStartup(d deps, args []string) int {
	if len(args) != 0 {
		return report(usageError{reason: "startup takes no arguments", usage: gsRestoreStartupUsage}, d.err)
	}

	document, err := gsrestore.Load(d.getenv)
	if err != nil {
		fmt.Fprintf(d.err, "herdr-suite: %v; GS session restore stays off\n", err)
		return 0
	}

	switch gsrestore.Decide(document.Config) {
	case gsrestore.PlanNothing:
		return 0
	case gsrestore.PlanHint:
		first, err := gsrestore.TakeHint(d.getenv, d.now())
		if err != nil {
			return report(err, d.err)
		}
		if first {
			fmt.Fprint(d.err, "herdr-suite: GS session restore is available; run \"bin/hs gs-restore install\" to enable it, or set gs_restore.auto_install = true\n")
		}
		return 0
	case gsrestore.PlanInstall:
		return runGSRestoreStartupInstall(d)
	}
	return 0
}

// runGSRestoreStartupInstall installs idempotently and speaks only when it
// changed something, so every server start stays quiet once the file is current.
func runGSRestoreStartupInstall(d deps) int {
	result, err := installReporter(d, false)
	if err != nil {
		return report(err, d.err)
	}
	if result.Already {
		return 0
	}
	if result.Backup != "" {
		fmt.Fprintf(d.err, "herdr-suite: backed up the previous reporter to %s\n", result.Backup)
	}
	fmt.Fprintf(d.err, "herdr-suite: installed GS session restore reporter at %s\n", result.Path)
	return 0
}

func writeGSStatus(out io.Writer, status gsrestore.Status, getenv func(string) string) {
	fmt.Fprintf(out, "%-13s %s\n", "config:", describeGSConfig(status))
	fmt.Fprintf(out, "%-13s %s\n", "install:", describeToggle(status.Config.Install))
	fmt.Fprintf(out, "%-13s %s\n", "auto-install:", describeToggle(status.Config.AutoInstall))
	fmt.Fprintf(out, "%-13s %s\n", "agent dir:", describeGSAgentDir(status, getenv))
	fmt.Fprintf(out, "%-13s %s\n", "reporter:", describeGSReporter(status))
	fmt.Fprintf(out, "%-13s %s\n", "version:", describeGSVersion(status))
	fmt.Fprintf(out, "%-13s %s\n", "backup:", describeGSBackup(status))
	fmt.Fprintf(out, "%-13s %s\n", "gentle-shell:", describeGSLauncher(status))
}

func describeToggle(on bool) string {
	if on {
		return "on"
	}
	return "off"
}

func describeGSConfig(status gsrestore.Status) string {
	if status.ConfigErr != nil {
		if status.ConfigPath != "" {
			return fmt.Sprintf("%s (invalid: %v)", status.ConfigPath, status.ConfigErr)
		}
		return fmt.Sprintf("unresolved: %v", status.ConfigErr)
	}
	if status.ConfigPresent {
		return status.ConfigPath + " (present)"
	}
	return status.ConfigPath + " (missing)"
}

func describeGSAgentDir(status gsrestore.Status, getenv func(string) string) string {
	if status.TargetErr == nil {
		return status.Target.AgentDir
	}
	if getenv("PI_CODING_AGENT_DIR") == "" {
		return "not set in PI_CODING_AGENT_DIR"
	}
	return status.TargetErr.Error()
}

func describeGSReporter(status gsrestore.Status) string {
	if status.TargetErr != nil {
		return "unknown (no agent dir)"
	}
	if status.Installed {
		return "installed at " + status.Target.Path
	}
	if status.Present {
		return "present without our marker at " + status.Target.Path
	}
	return "not installed"
}

func describeGSVersion(status gsrestore.Status) string {
	switch {
	case status.TargetErr != nil:
		return "unknown (no agent dir)"
	case !status.Present:
		return "no reporter installed"
	case !status.Installed:
		return "no version marker in the installed file"
	case status.VersionMatches:
		return fmt.Sprintf("matches (%s)", status.InstalledVersion)
	default:
		return fmt.Sprintf("differs (installed %s, shipped %s)", status.InstalledVersion, gsrestore.ReporterVersion())
	}
}

func describeGSBackup(status gsrestore.Status) string {
	if status.TargetErr != nil {
		return "unknown (no agent dir)"
	}
	if status.Backup != "" {
		return "present at " + status.Backup
	}
	return "none"
}

func describeGSLauncher(status gsrestore.Status) string {
	if status.GentleShell != "" {
		return "resolvable at " + status.GentleShell
	}
	return "not resolvable"
}

// defaultReporterSource finds the reporter this binary ships: the plugin root
// (bin/hs beside extensions/) first, then the working directory.
func defaultReporterSource() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		exe = ""
	}
	cwd, err := os.Getwd()
	if err != nil {
		cwd = ""
	}
	return gsrestore.SourcePath(exe, cwd)
}

// defaultLookPath is exec.LookPath behind the deps seam.
func defaultLookPath(name string) (string, error) {
	return exec.LookPath(name)
}

// defaultNow is time.Now behind the deps seam.
func defaultNow() time.Time {
	return time.Now()
}
