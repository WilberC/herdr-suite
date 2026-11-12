package main

import (
	"fmt"
	"io"
	"os"
)

const usage = `usage: hsd [command]

Commands:
  session   Manage herdr sessions
  space     Save and restore herdr spaces
  layout    Inspect stored layouts

Run "hsd <command> --help" for command details.
`

type command struct {
	name    string
	summary string
	usage   string
}

var commands = []command{
	{
		name:    "session",
		summary: "Manage herdr sessions",
		usage: `usage: hsd session [command]

Manage herdr sessions: list, attach, stop, delete.
`,
	},
	{
		name:    "space",
		summary: "Save and restore herdr spaces",
		usage: `usage: hsd space [command]

Save and restore herdr spaces, including pane layout, labels and commands.
`,
	},
	{
		name:    "layout",
		summary: "Inspect stored layouts",
		usage: `usage: hsd layout [command]

List and inspect stored layouts.
`,
	},
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	if isHelp(args[0]) {
		fmt.Fprint(stdout, usage)
		return 0
	}
	cmd, ok := lookup(args[0])
	if !ok {
		fmt.Fprintf(stderr, "unknown command %q\n\n%s", args[0], usage)
		return 2
	}
	if hasHelp(args[1:]) {
		fmt.Fprint(stdout, cmd.usage)
		return 0
	}
	fmt.Fprintf(stderr, "%s: not implemented\n\n%s", cmd.name, cmd.usage)
	return 2
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
