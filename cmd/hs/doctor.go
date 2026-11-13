package main

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/WilberC/herdr-suite/internal/doctor"
)

const doctorUsage = `usage: hs doctor [options]

Report the health of this hs installation, one line per check: the herdr binary
and its version against min_herdr_version, the herdr socket, the plugin config
and its gs_restore toggles, this binary against the manifest version, the layout
store and what the current directory declares, the GS session-restore files, and
the plugin manifest itself. Nothing is written.

Each line prints PASS, WARN, FAIL or SKIP, and a failing line names the fix. The
exit status is 0 when nothing failed and 1 when any line failed, so a warning
keeps a script green.

Options:
  --json             Print the whole report as JSON
  --manifest <path>  Read that manifest instead of the one beside the binary

A herdr call that does not answer within a few seconds is a FAIL line, never a
report that hangs.
`

// defaultSocketTimeout bounds the herdr probes. A socket that does not answer is
// reported as a failure instead of holding the terminal.
const defaultSocketTimeout = doctor.DefaultTimeout

func runDoctor(d deps, args []string) int {
	options, err := parseReportArgs("doctor", args, doctorUsage, false)
	if err != nil {
		return report(err, d.err)
	}

	health := doctor.Run(doctor.Deps{
		Version:      d.buildVersion(),
		ManifestPath: options.manifest,
		Executable:   d.executablePath(),
		Cwd:          d.workingDir(),
		Getenv:       d.getenv,
		LookPath:     d.lookPath,
		RunHerdr:     d.herdrRun,
		Ping:         d.sessionPing(),
		Timeout:      d.socketTimeout,
	})

	write := doctor.Write
	if options.json {
		write = doctor.WriteJSON
	}
	if err := write(d.out, health); err != nil {
		return report(err, d.err)
	}
	return health.ExitCode()
}

// reportOptions is the parsed command line of the commands that report on the
// installation: both read the manifest and both can print JSON, and --check
// belongs to hs version alone.
type reportOptions struct {
	check    bool
	json     bool
	manifest string
}

// parseReportArgs reads the flags a reporting command accepts and rejects
// anything else before the manifest, the socket or an agent directory is read.
func parseReportArgs(name string, args []string, usage string, allowCheck bool) (reportOptions, error) {
	var options reportOptions
	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch {
		case arg == "--check" && allowCheck:
			options.check = true
		case arg == "--json":
			options.json = true
		case arg == "--manifest":
			if index+1 >= len(args) {
				return reportOptions{}, usageError{reason: "--manifest needs a path", usage: usage}
			}
			index++
			options.manifest = args[index]
		case strings.HasPrefix(arg, "-"):
			return reportOptions{}, usageError{reason: fmt.Sprintf("unknown flag %q", arg), usage: usage}
		default:
			return reportOptions{}, usageError{reason: name + " takes no arguments", usage: usage}
		}
	}
	return options, nil
}

// executablePath is where this binary runs from, which anchors the manifest
// beside it. A binary that cannot report its own path leaves the doctor with the
// working directory.
func (d deps) executablePath() string {
	if d.executable == nil {
		return ""
	}
	path, err := d.executable()
	if err != nil {
		return ""
	}
	return path
}

func (d deps) workingDir() string {
	if d.cwd == nil {
		return ""
	}
	dir, err := d.cwd()
	if err != nil {
		return ""
	}
	return dir
}

// sessionPing probes the socket the way the switcher reads sessions, bounded by
// the doctor's timeout. It reports how many sessions answered.
func (d deps) sessionPing() func(context.Context) (int, error) {
	if d.client == nil {
		return nil
	}
	return func(ctx context.Context) (int, error) {
		sessions, err := d.client.ListSessions(ctx)
		if err != nil {
			return 0, err
		}
		return len(sessions), nil
	}
}

// defaultHerdrRun runs the herdr binary with this process's environment, so the
// doctor probes the same herdr a plugin action would.
func defaultHerdrRun(ctx context.Context, bin string, args ...string) ([]byte, []byte, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}
