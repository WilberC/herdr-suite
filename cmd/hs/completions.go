package main

import (
	"fmt"
	"strings"

	"github.com/WilberC/herdr-suite/internal/completions"
	"github.com/WilberC/herdr-suite/internal/session"
)

const completionsUsage = `usage: hs completions <shell> [options]

Print a shell completion script on stdout. "fish" is the only shell this build
generates. The script completes the command tree statically and reads session and
layout names from hs itself, so it offers exactly the names the CLI resolves,
project declarations included.

Options:
  --install  Write the script to $XDG_CONFIG_HOME/fish/completions/hs.fish, or
             ~/.config/fish/completions/hs.fish when XDG_CONFIG_HOME is unset,
             and print the path. The directory and its parents are created when
             they do not exist, so "hs completions fish --install" is the whole
             install.

The script never prints a herdr failure: a completion read that fails offers no
names instead of writing on the command line.
`

func runCompletions(d deps, args []string) int {
	install := false
	var shells []string
	for _, arg := range args {
		switch {
		case arg == "--install":
			install = true
		case strings.HasPrefix(arg, "-"):
			return report(usageError{reason: fmt.Sprintf("unknown flag %q", arg), usage: completionsUsage}, d.err)
		default:
			shells = append(shells, arg)
		}
	}
	if len(shells) != 1 {
		return report(usageError{reason: "expected one shell name", usage: completionsUsage}, d.err)
	}
	if shells[0] != "fish" {
		return report(usageError{reason: fmt.Sprintf("only \"fish\" is supported, not %q", shells[0]), usage: completionsUsage}, d.err)
	}

	spec := completionCommands()
	if !install {
		fmt.Fprint(d.out, completions.Fish(spec))
		return 0
	}
	path, err := completions.Install(spec, d.getenv)
	if err != nil {
		return report(session.Refusal{Reason: err.Error()}, d.err)
	}
	fmt.Fprintf(d.out, "installed: %s\n", path)
	return 0
}

// completionCommands is the command table as the completion spec: every command
// and subcommand, plus the kind of name each argument position reads.
func completionCommands() []completions.Command {
	spec := make([]completions.Command, 0, len(commands))
	for _, cmd := range commands {
		entry := completions.Command{
			Name:    cmd.name,
			Summary: cmd.summary,
			Names:   completionNames(cmd.name, ""),
		}
		for _, sub := range cmd.subcommands {
			entry.Children = append(entry.Children, completions.Child{
				Name:    sub.name,
				Summary: sub.summary,
				Names:   completionNames(cmd.name, sub.name),
			})
		}
		spec = append(spec, entry)
	}
	return spec
}

// completionNames is the live names one command position reads: a session name
// for the commands that take an existing session, and a layout name for the
// commands that read a stored layout. A name a command would create instead of
// reading is not completed from the store.
func completionNames(group, sub string) completions.Names {
	switch group {
	case "attach":
		return completions.SessionNames
	case "session":
		switch sub {
		case "create", "stop", "restart", "delete", "fork":
			return completions.SessionNames
		}
	case "space":
		switch sub {
		case "load", "show":
			return completions.LayoutNames
		}
	}
	return completions.NoNames
}
