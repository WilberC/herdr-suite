package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/WilberC/herdr-suite/internal/layout"
	"github.com/WilberC/herdr-suite/internal/session"
)

const spaceShowUsage = `usage: hs space show <name> [options]

Print a stored layout without touching a session: the layout's name, scope,
source file and schema version, the captured timestamp when the document records
one, and then every workspace it holds, tab by tab, with the stored split tree,
pane labels, cwds and agent identifications.

The name is resolved like "hs space load" resolves it: by path when it holds a
separator, then in the nearest project declaration under .herdr/, then in the
global store. A missing layout, a name more than one place declares and a layout
written by a newer hs are refused with the same wording and exit status a load
refuses them, so a script can check a layout before replaying it.

Options:
  --json        Print the stored document instead of the view
  --dir <path>  Discover layouts from that directory instead of this one
`

// showOptions is the parsed "hs space show" command line.
type showOptions struct {
	reference string
	dir       string
	json      bool
}

func runSpaceShow(d deps, args []string) int {
	options, err := parseShowArgs(args)
	if err != nil {
		return report(err, d.err)
	}

	resolved, err := resolveStoredLayout(d, options.reference, options.dir, spaceShowUsage)
	if err != nil {
		return report(err, d.err)
	}
	if options.json {
		return report(layout.Write(d.out, resolved.Document), d.err)
	}
	fmt.Fprint(d.out, layout.Show(resolved.Document, resolved.Path, resolved.Scope))
	return 0
}

// parseShowArgs reads "<name> [options]" in any order and rejects anything else
// before a stored layout is read.
func parseShowArgs(args []string) (showOptions, error) {
	var options showOptions
	var names []string
	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch {
		case arg == "--json":
			options.json = true
		case arg == "--dir":
			if index+1 >= len(args) {
				return showOptions{}, usageError{reason: "--dir needs a directory", usage: spaceShowUsage}
			}
			index++
			options.dir = args[index]
		case strings.HasPrefix(arg, "-"):
			return showOptions{}, usageError{reason: fmt.Sprintf("unknown flag %q", arg), usage: spaceShowUsage}
		default:
			names = append(names, arg)
		}
	}
	if len(names) != 1 {
		return showOptions{}, usageError{reason: "expected one layout name", usage: spaceShowUsage}
	}
	options.reference = names[0]
	return options, nil
}

// resolvedLayout is a stored layout a command reads: the document, the file it
// came from, and the scope that file belongs to.
type resolvedLayout struct {
	Document layout.Layout
	Path     string
	Scope    string
}

// resolveStoredLayout reads the layout a reference names with the resolution
// order every space command shares, and reports a missing or ambiguous layout as
// the refusal its caller prints. usage is the command's own usage text, which a
// name the sanitizer rejects is reported against.
func resolveStoredLayout(d deps, reference, dir, usage string) (resolvedLayout, error) {
	if !layout.IsPath(reference) {
		name, err := session.SanitizeName(reference, "layout")
		if err != nil {
			return resolvedLayout{}, usageError{reason: err.Error(), usage: usage}
		}
		reference = name
	}

	sources, err := layoutSources(d, dir)
	if err != nil {
		return resolvedLayout{}, err
	}
	document, path, err := layout.Resolve(sources, reference)
	if err != nil {
		return resolvedLayout{}, layoutRefusal(err, reference)
	}
	return resolvedLayout{Document: document, Path: path, Scope: layout.ScopeOf(sources, path)}, nil
}

// layoutRefusal is the refusal a missing or ambiguous layout reports, in the one
// wording every space command prints: the fix for a missing layout, and every
// candidate for a name more than one place declares.
func layoutRefusal(err error, reference string) error {
	var missing layout.NotFoundError
	if errors.As(err, &missing) {
		return session.Refusal{Reason: fmt.Sprintf(
			"unknown layout '%s'; create it with hs space save %s", reference, reference)}
	}
	var ambiguous layout.AmbiguousError
	if errors.As(err, &ambiguous) {
		return session.Refusal{Reason: ambiguous.Error()}
	}
	return err
}
