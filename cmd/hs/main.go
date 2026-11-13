package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/WilberC/herdr-suite/internal/gsrestore"
	"github.com/WilberC/herdr-suite/internal/herdrcli"
	"github.com/WilberC/herdr-suite/internal/herdrmock"
	"github.com/WilberC/herdr-suite/internal/last"
	"github.com/WilberC/herdr-suite/internal/layout"
	"github.com/WilberC/herdr-suite/internal/pathshim"
	"github.com/WilberC/herdr-suite/internal/session"
	"github.com/mattn/go-isatty"
)

const usage = `usage: hs [command]

Commands:
  attach <name>   Attach to a session; run it outside herdr
  session         Manage herdr sessions
  space           Save and restore herdr spaces
  gs-restore      Install the GS session-restore reporter (default off)
  path            Put hs on PATH for your shell
  last            Toggle to the previous workspace or tab
  completions     Generate a shell completion script
  doctor          Report the health of this installation
  version         Print the version this binary was built as

Run "hs" with no command for the interactive session switcher.
Run "hs <command> --help" for command details.
`

const attachUsage = `usage: hs attach <name>

Replace this shell with a herdr client attached to <name>, creating the
session when it does not exist. Herdr forbids nested launches, so run it
outside herdr.
`

const sessionUsage = `usage: hs session <command>

Commands:
  list [--json]           List sessions as a stable table, or as JSON
  create <name>           Create a session by attaching it
  stop <name> [--yes]     Stop a session
  restart <name> [--yes]  Stop a session so the next attach starts it fresh
  delete <name> [--yes]   Stop and delete a session
  fork [session] [layout] Snapshot every workspace of a session as one layout

Names may hold letters, digits, '.', '_' and '-'; whitespace runs become '-'.
Destructive commands need --yes when stdin is not a terminal.
`

type command struct {
	name    string
	summary string
	usage   string
	// subcommands are the names the command dispatches itself. The completion
	// script is generated from them, so a subcommand cannot be dispatched
	// without also being offered by the shell.
	subcommands []subcommand
}

// subcommand is one name a command group dispatches.
type subcommand struct {
	name    string
	summary string
}

var commands = []command{
	{
		name:    "attach",
		summary: "Attach to a session",
		usage:   attachUsage,
	},
	{
		name:    "session",
		summary: "Manage herdr sessions",
		usage:   sessionUsage,
		subcommands: []subcommand{
			{name: "list", summary: "List sessions as a stable table, or as JSON"},
			{name: "create", summary: "Create a session by attaching it"},
			{name: "stop", summary: "Stop a session"},
			{name: "restart", summary: "Stop a session so the next attach starts it fresh"},
			{name: "delete", summary: "Stop and delete a session"},
			{name: "fork", summary: "Snapshot a session as one layout"},
		},
	},
	{
		name:    "space",
		summary: "Save and restore herdr spaces",
		usage:   spaceUsage,
		subcommands: []subcommand{
			{name: "save", summary: "Snapshot the active workspace as a layout"},
			{name: "load", summary: "Recreate a stored layout in a workspace"},
			{name: "show", summary: "Print a stored layout without loading it"},
			{name: "list", summary: "List the layouts visible from here"},
		},
	},
	{
		name:    "gs-restore",
		summary: "Install the GS session-restore reporter",
		usage:   gsRestoreUsage,
		subcommands: []subcommand{
			{name: "install", summary: "Install the GS session-restore reporter"},
			{name: "remove", summary: "Restore the previous reporter, or remove ours"},
			{name: "status", summary: "Report the reporter, the backup and the config toggles"},
			{name: "startup", summary: "Apply the config gate at server start"},
		},
	},
	{
		name:    "path",
		summary: "Put hs on PATH for your shell",
		usage:   pathUsage,
		subcommands: []subcommand{
			{name: "install", summary: "Write the hs shim into your bin directory"},
			{name: "remove", summary: "Delete the shim this command wrote"},
			{name: "status", summary: "Report the shim, PATH and the plugin"},
		},
	},
	{
		name:    "last",
		summary: "Toggle to the previous workspace or tab",
		usage:   lastUsage,
		subcommands: []subcommand{
			{name: "workspace", summary: "Focus the previous workspace and swap back"},
			{name: "tab", summary: "Focus the previous tab and swap back"},
			{name: "observe", summary: "Record a focus event from the plugin hooks"},
		},
	},
	{
		name:    "completions",
		summary: "Generate a shell completion script",
		usage:   completionsUsage,
	},
	{
		name:    "doctor",
		summary: "Report the health of this installation",
		usage:   doctorUsage,
	},
	{
		name:    "version",
		summary: "Print the build version",
		usage:   versionUsage,
	},
}

type deps struct {
	client         herdrmock.SessionClient
	spaces         herdrmock.SpaceClient
	writer         herdrmock.Writer
	exec           session.Execer
	getenv         func(string) string
	cwd            func() (string, error)
	in             io.Reader
	interactive    bool
	confirm        session.Confirmer
	switcher       func(context.Context) (string, error)
	pick           func([]herdrmock.Workspace) (string, error)
	pickLayout     layoutPickerFunc
	reporterSource func() (string, error)
	now            func() time.Time
	lookPath       func(string) (string, error)
	out            io.Writer
	err            io.Writer

	// version is the build version the binary reports. It is a field so a test
	// can pin an embedded release version; a build without one keeps the dev
	// default.
	version string
	// executable is where this binary runs from, which anchors the plugin
	// manifest the version check and the doctor read.
	executable func() (string, error)
	// herdrRun runs the herdr binary for the doctor's version probe.
	herdrRun func(ctx context.Context, bin string, args ...string) (stdout, stderr []byte, err error)
	// socketTimeout bounds every herdr probe the doctor makes.
	socketTimeout time.Duration
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	return runWith(defaultDeps(stdout, stderr), args, stdout, stderr)
}

func defaultDeps(stdout, stderr io.Writer) deps {
	herdr := herdrcli.New()
	d := deps{
		client:         herdr,
		spaces:         herdr,
		writer:         herdr,
		exec:           herdrcli.Execer{},
		getenv:         os.Getenv,
		cwd:            os.Getwd,
		in:             os.Stdin,
		interactive:    isatty.IsTerminal(os.Stdin.Fd()) && isatty.IsTerminal(os.Stdout.Fd()),
		reporterSource: defaultReporterSource,
		now:            defaultNow,
		lookPath:       defaultLookPath,
		version:        version,
		executable:     os.Executable,
		herdrRun:       defaultHerdrRun,
		socketTimeout:  defaultSocketTimeout,
		out:            stdout,
		err:            stderr,
	}
	if d.interactive {
		d.confirm = session.TerminalConfirmer(os.Stdin, stderr)
	}
	d.switcher = defaultSwitcher(d, stdout)
	if d.interactive {
		d.pick = func(workspaces []herdrmock.Workspace) (string, error) {
			return pickWorkspace(os.Stdin, stdout, workspaces)
		}
		d.pickLayout = func(entries []layout.Entry, restore layoutRestore) (layoutPick, error) {
			return pickLayout(os.Stdin, stdout, entries, restore)
		}
	}
	return d
}

// defaultSwitcher runs the interactive switcher with the restart handoff the
// old fzf flow used: ctrl-r leaves the stopped name in a temp file, and the
// attach happens after the switcher closes.
func defaultSwitcher(d deps, out io.Writer) func(context.Context) (string, error) {
	return func(ctx context.Context) (string, error) {
		flag, err := os.CreateTemp("", "hs-restart-*")
		if err != nil {
			return "", err
		}
		path := flag.Name()
		if err := flag.Close(); err != nil {
			os.Remove(path)
			return "", err
		}
		defer os.Remove(path)

		switcher := session.NewSwitcher(session.SwitcherConfig{
			Manager:     d.manager(true),
			RestartFlag: path,
			Preview:     session.SpacePreviewer{Client: d.spaces},
		})
		if err := switcher.Prepare(ctx); err != nil {
			return "", err
		}
		if err := switcher.Run(os.Stdin, out); err != nil {
			return "", err
		}
		return switcher.Target(), nil
	}
}

func (d deps) manager(assumeYes bool) *session.Manager {
	return session.New(session.Config{
		Client:      d.client,
		Exec:        d.exec,
		Out:         d.out,
		Err:         d.err,
		Getenv:      d.getenv,
		Interactive: d.interactive,
		AssumeYes:   assumeYes,
		Confirmer:   d.confirm,
	})
}

func runWith(d deps, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return runSwitcher(d)
	}
	if isHelp(args[0]) {
		fmt.Fprint(stdout, usage)
		return 0
	}

	cmd, ok := lookup(args[0])
	if !ok {
		return unknownCommand(args[0], stderr)
	}
	if hasHelp(args[1:]) {
		fmt.Fprint(stdout, helpFor(cmd, args[1:]))
		return 0
	}

	switch cmd.name {
	case "attach":
		return runAttach(d, args[1:])
	case "session":
		return runSession(d, args[1:])
	case "space":
		return runSpace(d, args[1:])
	case "gs-restore":
		return runGSRestore(d, args[1:])
	case "path":
		return runPath(d, args[1:])
	case "last":
		return runLast(d, args[1:])
	case "completions":
		return runCompletions(d, args[1:])
	case "doctor":
		return runDoctor(d, args[1:])
	case "version":
		return runVersion(d, args[1:])
	}

	return unknownCommand(cmd.name, stderr)
}

// unknownCommand is what any name the table does not dispatch answers: the exit
// status scripts rely on, with the usage they can read.
func unknownCommand(name string, stderr io.Writer) int {
	fmt.Fprintf(stderr, "unknown command %q\n\n%s", name, usage)
	return 2
}

// runSwitcher is "hs" with no command: the interactive list. Attaching
// replaces the process, so it runs after the switcher has released the
// terminal.
func runSwitcher(d deps) int {
	if d.getenv("HERDR_ENV") != "" {
		return report(session.Refusal{Reason: "run it outside herdr; herdr forbids nested launches"}, d.err)
	}
	if !d.interactive {
		fmt.Fprint(d.err, "hs: the session switcher needs a terminal\n\n"+usage)
		return 2
	}

	target, err := d.switcher(context.Background())
	if err != nil {
		return report(err, d.err)
	}
	if target == "" {
		return 0
	}
	return report(d.manager(false).Attach(target), d.err)
}

func runAttach(d deps, args []string) int {
	if len(args) != 1 || strings.HasPrefix(args[0], "-") {
		return report(usageError{reason: "expected one session name", usage: attachUsage}, d.err)
	}
	return report(d.manager(false).Attach(args[0]), d.err)
}

func runSession(d deps, args []string) int {
	if len(args) == 0 {
		fmt.Fprint(d.err, sessionUsage)
		return 2
	}

	switch args[0] {
	case "list":
		return runSessionList(d, args[1:])
	case "create":
		return runSessionAction(d, args[1:], actionCreate)
	case "stop":
		return runSessionAction(d, args[1:], actionStop)
	case "restart":
		return runSessionAction(d, args[1:], actionRestart)
	case "delete":
		return runSessionAction(d, args[1:], actionDelete)
	case "fork":
		return runSessionFork(d, args[1:])
	}

	fmt.Fprintf(d.err, "unknown session command %q\n\n%s", args[0], sessionUsage)
	return 2
}

func runSessionList(d deps, args []string) int {
	jsonOutput := false
	for _, arg := range args {
		switch {
		case arg == "--json":
			jsonOutput = true
		case strings.HasPrefix(arg, "-"):
			return report(usageError{reason: fmt.Sprintf("unknown flag %q", arg), usage: sessionUsage}, d.err)
		default:
			return report(usageError{reason: "list takes no arguments", usage: sessionUsage}, d.err)
		}
	}

	sessions, err := d.manager(false).List(context.Background())
	if err != nil {
		return report(err, d.err)
	}
	if jsonOutput {
		return report(session.WriteJSON(d.out, sessions), d.err)
	}
	return report(session.WriteTable(d.out, sessions), d.err)
}

type action int

const (
	actionCreate action = iota + 1
	actionStop
	actionRestart
	actionDelete
)

func runSessionAction(d deps, args []string, kind action) int {
	name, yes, err := parseLifecycle(args, kind != actionCreate)
	if err != nil {
		return report(err, d.err)
	}

	manager := d.manager(yes)
	ctx := context.Background()
	switch kind {
	case actionStop:
		return report(manager.Stop(ctx, name), d.err)
	case actionRestart:
		return report(manager.Restart(ctx, name, ""), d.err)
	case actionDelete:
		return report(manager.Delete(ctx, name), d.err)
	}
	return report(manager.Attach(name), d.err)
}

// parseLifecycle reads "<name> [--yes]" in either order and rejects anything
// else before a client call happens.
func parseLifecycle(args []string, allowYes bool) (string, bool, error) {
	var names []string
	yes := false
	for _, arg := range args {
		switch {
		case arg == "--yes" && allowYes:
			yes = true
		case strings.HasPrefix(arg, "-"):
			return "", false, usageError{reason: fmt.Sprintf("unknown flag %q", arg), usage: sessionUsage}
		default:
			names = append(names, arg)
		}
	}
	if len(names) != 1 {
		return "", false, usageError{reason: "expected one session name", usage: sessionUsage}
	}
	return names[0], yes, nil
}

// usageError is a command-line mistake: it prints its reason, then the usage
// of the command that rejected the arguments.
type usageError struct {
	reason string
	usage  string
}

func (e usageError) Error() string { return e.reason }

// report turns an error into the exit status contract callers rely on: 0 when
// nothing failed or the user declined, 2 for a refusal or bad arguments, and 1
// when herdr failed.
func report(err error, stderr io.Writer) int {
	if err == nil {
		return 0
	}
	if errors.Is(err, session.ErrCancelled) {
		fmt.Fprintln(stderr, "cancelled")
		return 0
	}

	var usageErr usageError
	if errors.As(err, &usageErr) {
		fmt.Fprintf(stderr, "hs: %s\n\n%s", usageErr.reason, usageErr.usage)
		return 2
	}
	var refusal session.Refusal
	if errors.As(err, &refusal) {
		fmt.Fprintf(stderr, "hs: %s\n", refusal.Reason)
		return 2
	}
	var gsRefusal gsrestore.Refusal
	if errors.As(err, &gsRefusal) {
		fmt.Fprintf(stderr, "hs: %s\n", gsRefusal.Reason)
		return 2
	}
	var shimRefusal pathshim.Refusal
	if errors.As(err, &shimRefusal) {
		fmt.Fprintf(stderr, "hs: %s\n", shimRefusal.Reason)
		return 2
	}
	var historyRefusal last.Refusal
	if errors.As(err, &historyRefusal) {
		fmt.Fprintf(stderr, "hs: %s\n", historyRefusal.Reason)
		return 2
	}
	var stored layout.ExistsError
	if errors.As(err, &stored) {
		fmt.Fprintf(stderr, "hs: %v\n", err)
		return 2
	}
	var newer layout.UnsupportedSchemaError
	if errors.As(err, &newer) {
		fmt.Fprintf(stderr, "hs: %v\n", err)
		return 2
	}

	fmt.Fprintf(stderr, "hs: %v\n", err)
	return 1
}

// groupUsage is the help a subcommand prints for itself. A subcommand without
// an entry falls back to the help of its group.
var groupUsage = map[string]map[string]string{
	"space": {
		"save": spaceSaveUsage,
		"load": spaceLoadUsage,
		"show": spaceShowUsage,
		"list": spaceListUsage,
	},
	"session": {
		"fork": sessionForkUsage,
	},
	"gs-restore": {
		"install": gsRestoreInstallUsage,
		"remove":  gsRestoreRemoveUsage,
		"status":  gsRestoreStatusUsage,
		"startup": gsRestoreStartupUsage,
	},
	"path": {
		"install": pathInstallUsage,
		"remove":  pathRemoveUsage,
		"status":  pathStatusUsage,
	},
	"last": {
		"workspace": lastWorkspaceUsage,
		"tab":       lastTabUsage,
		"observe":   lastObserveUsage,
	},
}

// helpFor is the text a help flag prints: the subcommand's own usage when it has
// one, otherwise the usage of the command group.
func helpFor(cmd command, args []string) string {
	if len(args) > 0 {
		if usage, ok := groupUsage[cmd.name][args[0]]; ok {
			return usage
		}
	}
	return cmd.usage
}

func lookup(name string) (command, bool) {
	for _, cmd := range commands {
		if cmd.name == name {
			return cmd, true
		}
	}
	return command{}, false
}

func isHelp(arg string) bool {
	return arg == "help" || arg == "-h" || arg == "--help"
}

func hasHelp(args []string) bool {
	for _, arg := range args {
		if isHelp(arg) {
			return true
		}
	}
	return false
}
