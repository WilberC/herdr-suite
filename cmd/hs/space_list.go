package main

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/WilberC/herdr-suite/internal/layout"
)

const spaceListUsage = `usage: hs space list [options]

List the layouts visible from here: the ones the nearest project declares under
.herdr/ — a layouts directory, or a layouts.toml index that names them — and the
ones the global store holds. Each row prints the layout's scope, the workspace
it holds and its tab and pane counts.

Options:
  --json         Print the list as JSON instead of the table
  --pane         Keep the table on screen until enter is pressed
  --dir <path>   Discover from that directory instead of the working directory

A project declaration is found like a git repository: from the working directory
upward, and the nearest one wins. A project layout answers before a global one
with the same name.
`

// listOptions is the parsed "hs space list" command line.
type listOptions struct {
	dir  string
	json bool
	pane bool
}

func runSpaceList(d deps, args []string) int {
	options, err := parseListArgs(args)
	if err != nil {
		return report(err, d.err)
	}

	sources, err := layoutSources(d, options.dir)
	if err != nil {
		return report(err, d.err)
	}
	entries, err := layout.List(sources)
	if err != nil {
		return report(err, d.err)
	}

	if options.json {
		return report(layout.WriteListJSON(d.out, entries), d.err)
	}
	if err := layout.WriteList(d.out, entries); err != nil {
		return report(err, d.err)
	}
	if options.pane {
		fmt.Fprint(d.out, "\npress enter to close\n")
		waitForEnter(d.in)
	}
	return 0
}

// parseListArgs reads the list flags and rejects anything else before the store
// is read.
func parseListArgs(args []string) (listOptions, error) {
	var options listOptions
	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch {
		case arg == "--json":
			options.json = true
		case arg == "--pane":
			options.pane = true
		case arg == "--dir":
			if index+1 >= len(args) {
				return listOptions{}, usageError{reason: "--dir needs a directory", usage: spaceListUsage}
			}
			index++
			options.dir = args[index]
		case strings.HasPrefix(arg, "-"):
			return listOptions{}, usageError{reason: fmt.Sprintf("unknown flag %q", arg), usage: spaceListUsage}
		default:
			return listOptions{}, usageError{reason: "list takes no arguments", usage: spaceListUsage}
		}
	}
	if options.json && options.pane {
		return listOptions{}, usageError{reason: "--json and --pane cannot be combined", usage: spaceListUsage}
	}
	return options, nil
}

// waitForEnter keeps a plugin pane holding the table until the user dismisses
// it. A reader that fails or ends simply closes the pane.
func waitForEnter(in io.Reader) {
	if in == nil {
		return
	}
	_, _ = bufio.NewReader(in).ReadString('\n')
}
