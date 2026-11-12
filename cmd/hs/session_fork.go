package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/WilberC/herdr-suite/internal/herdrcli"
	"github.com/WilberC/herdr-suite/internal/herdrmock"
	"github.com/WilberC/herdr-suite/internal/layout"
	"github.com/WilberC/herdr-suite/internal/session"
)

const sessionForkUsage = `usage: hs session fork [session] [layout-name] [options]

Snapshot every workspace of a herdr session into one layout file. Without a
session it forks the current one, the session whose socket this process already
talks to. The layout name defaults to the session name.

Options:
  --force  Replace a stored layout with the same name
  --json   Print the snapshot document instead of the report

A session layout holds every workspace the fork read, in the order herdr
reported them. "hs space load <layout-name>" creates every workspace again, with
its tabs, splits, labels, cwds, agents and focus, and leaves the last one
focused. Saving always writes the global store.
`

// forkOptions is the parsed "hs session fork" command line.
type forkOptions struct {
	session    string
	layoutName string
	force      bool
	json       bool
}

func runSessionFork(d deps, args []string) int {
	options, err := parseForkArgs(args)
	if err != nil {
		return report(err, d.err)
	}

	name := ""
	if options.layoutName != "" {
		name, err = session.SanitizeName(options.layoutName, "layout")
		if err != nil {
			return report(usageError{reason: err.Error(), usage: sessionForkUsage}, d.err)
		}
	}

	ctx := context.Background()
	target, sessionName, err := forkTarget(ctx, d, options.session)
	if err != nil {
		return report(err, d.err)
	}
	if name == "" {
		name, err = session.SanitizeName(sessionName, "layout")
		if err != nil {
			return report(usageError{reason: err.Error(), usage: sessionForkUsage}, d.err)
		}
	}

	space, err := d.spaces.Snapshot(ctx, target)
	if err != nil {
		return report(fmt.Errorf("snapshot %s: %w", describeTarget(target), err), d.err)
	}
	document, err := layout.CaptureSession(name, space)
	if err != nil {
		return report(err, d.err)
	}
	document.Session = sessionName

	dir, err := layout.Dir(d.getenv)
	if err != nil {
		return report(err, d.err)
	}
	path, err := layout.Save(dir, document, options.force)
	if err != nil {
		return report(err, d.err)
	}

	if options.json {
		return report(layout.Write(d.out, document), d.err)
	}
	fmt.Fprint(d.out, layout.Summary(document, path))
	return 0
}

// parseForkArgs reads "[session] [layout-name] [--force] [--json]" and rejects
// anything else before a client call happens.
func parseForkArgs(args []string) (forkOptions, error) {
	var options forkOptions
	var names []string
	for _, arg := range args {
		switch {
		case arg == "--force":
			options.force = true
		case arg == "--json":
			options.json = true
		case strings.HasPrefix(arg, "-"):
			return forkOptions{}, usageError{reason: fmt.Sprintf("unknown flag %q", arg), usage: sessionForkUsage}
		default:
			names = append(names, arg)
		}
	}
	if len(names) > 2 {
		return forkOptions{}, usageError{reason: "expected at most a session and a layout name", usage: sessionForkUsage}
	}
	if len(names) > 0 {
		options.session = names[0]
	}
	if len(names) > 1 {
		options.layoutName = names[1]
	}
	return options, nil
}

// forkTarget resolves the session a fork snapshots and the name it stores.
func forkTarget(ctx context.Context, d deps, requested string) (herdrmock.Target, string, error) {
	if requested != "" {
		name, err := session.Sanitize(requested)
		if err != nil {
			return herdrmock.Target{}, "", usageError{reason: err.Error(), usage: sessionForkUsage}
		}
		target, err := sessionTarget(ctx, d, name, sessionForkUsage)
		if err != nil {
			return herdrmock.Target{}, "", err
		}
		return target, target.Name, nil
	}

	current, err := currentSession(ctx, d)
	if err != nil {
		return herdrmock.Target{}, "", err
	}
	return herdrmock.Target{Name: current.Name, Socket: current.Socket}, current.Name, nil
}

// currentSession finds the session this process runs inside by matching the
// socket herdr exported against the sessions herdr lists.
func currentSession(ctx context.Context, d deps) (herdrmock.Session, error) {
	unnamed := session.Refusal{Reason: "cannot name the current session; pass the session name to fork"}
	socket := d.getenv(herdrcli.EnvSocketPath)
	if socket == "" {
		return herdrmock.Session{}, unnamed
	}

	sessions, err := d.manager(false).List(ctx)
	if err != nil {
		return herdrmock.Session{}, err
	}
	for _, candidate := range sessions {
		if candidate.Socket == socket {
			return candidate, nil
		}
	}
	return herdrmock.Session{}, unnamed
}
