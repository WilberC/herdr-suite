package main

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/WilberC/herdr-suite/internal/pathshim"
)

const pathUsage = `usage: hs path <command>

Commands:
  install [--dir <path>] [--force]  Write the hs shim into your bin directory
  remove [--dir <path>] [--force]   Delete the shim this command wrote
  status [--dir <path>]             Report the shim, PATH and the plugin

The plugin ships bin/hs inside its own checkout, whose path carries a hash and a
generation number. The shim puts "hs" on PATH without naming that path: it
resolves the plugin root from "herdr plugin list" each time it runs, so a plugin
update never breaks it.

The default directory is $HOME/.local/bin. Nothing is installed automatically;
run install yourself, then status to see whether the directory is searched.
`

const pathInstallUsage = `usage: hs path install [options]

Write the hs shim into $HOME/.local/bin, or into --dir, creating the directory
when it is missing, and report the path and whether that directory is in PATH.
The shim resolves the plugin root at runtime, so a plugin update never breaks it.

An existing hs this command did not write is refused with exit status 2;
--force replaces it after copying it to <name>.bak-<timestamp>. An install that
finds the shim already current writes nothing.

Options:
  --dir <path>  Write the shim there instead of $HOME/.local/bin
  --force       Replace a file that is not the hs shim, keeping a backup
`

const pathRemoveUsage = `usage: hs path remove [options]

Delete the hs shim this command wrote. A file it did not write is removed only
when a backup of it already exists; otherwise the removal is refused with exit
status 2, and --force removes it after keeping a backup. Backups are never
deleted, and a removal with nothing installed reports that and exits 0.

Options:
  --dir <path>  Read the shim there instead of $HOME/.local/bin
  --force       Remove a file that is not the hs shim, keeping a backup
`

const pathStatusUsage = `usage: hs path status [options]

Report whether the shim is installed, whether it still matches the one this
build writes, whether its directory is in PATH, and whether the plugin the shim
would exec is resolvable. Nothing is written and the exit status is always 0.

Options:
  --dir <path>  Read the shim there instead of $HOME/.local/bin
`

// pathOptions is the parsed command line of the three path commands: --dir
// applies to all of them, --force only to the two that write.
type pathOptions struct {
	dir   string
	force bool
}

func runPath(d deps, args []string) int {
	if len(args) == 0 {
		fmt.Fprint(d.err, pathUsage)
		return 2
	}

	switch args[0] {
	case "install":
		return runPathInstall(d, args[1:])
	case "remove":
		return runPathRemove(d, args[1:])
	case "status":
		return runPathStatus(d, args[1:])
	}

	fmt.Fprintf(d.err, "unknown path command %q\n\n%s", args[0], pathUsage)
	return 2
}

func runPathInstall(d deps, args []string) int {
	options, err := parsePathArgs("install", args, pathInstallUsage, true)
	if err != nil {
		return report(err, d.err)
	}
	dir, err := pathshim.Dir(d.getenv, options.dir)
	if err != nil {
		return report(err, d.err)
	}

	result, err := pathshim.Install(dir, options.force, d.clock()())
	if err != nil {
		return report(err, d.err)
	}

	if result.Backup != "" {
		fmt.Fprintf(d.out, "backup:    %s\n", result.Backup)
	}
	line := result.Path
	if result.Already {
		line += " (already current)"
	}
	fmt.Fprintf(d.out, "installed: %s\n", line)
	fmt.Fprintf(d.out, "PATH:      %s\n", pathshim.PathLine(result.Dir, pathshim.OnPath(result.Dir, d.getenv("PATH"))))
	return 0
}

func runPathRemove(d deps, args []string) int {
	options, err := parsePathArgs("remove", args, pathRemoveUsage, true)
	if err != nil {
		return report(err, d.err)
	}
	dir, err := pathshim.Dir(d.getenv, options.dir)
	if err != nil {
		return report(err, d.err)
	}

	result, err := pathshim.Remove(dir, options.force, d.clock()())
	if err != nil {
		return report(err, d.err)
	}

	if result.Missing {
		fmt.Fprintf(d.out, "nothing to remove at %s\n", result.Path)
		return 0
	}
	if result.Backup != "" {
		fmt.Fprintf(d.out, "backup:    %s (kept)\n", result.Backup)
	}
	fmt.Fprintf(d.out, "removed:   %s\n", result.Path)
	return 0
}

func runPathStatus(d deps, args []string) int {
	options, err := parsePathArgs("status", args, pathStatusUsage, false)
	if err != nil {
		return report(err, d.err)
	}

	status := pathshim.Inspect(d.getenv, options.dir, d.pathPluginResolver())
	writePathStatus(d.out, status)
	return 0
}

func writePathStatus(out io.Writer, status pathshim.Status) {
	fmt.Fprintf(out, "%-11s %s\n", "shim:", describePathShim(status))
	fmt.Fprintf(out, "%-11s %s\n", "installed:", yesNo(status.Present))
	fmt.Fprintf(out, "%-11s %s\n", "matches:", yesNo(status.Matches))
	fmt.Fprintf(out, "%-11s %s\n", "on PATH:", describePathOnPath(status))
	fmt.Fprintf(out, "%-11s %s\n", "plugin:", describePathPlugin(status))
}

func describePathShim(status pathshim.Status) string {
	if status.DirErr != nil {
		return fmt.Sprintf("unresolved (%v)", status.DirErr)
	}
	return status.Path
}

func describePathOnPath(status pathshim.Status) string {
	if status.OnPath {
		return "yes"
	}
	if status.DirErr != nil {
		return "no (no target directory)"
	}
	return "no; add it to your shell with: export PATH=\"" + status.Dir + ":$PATH\""
}

func describePathPlugin(status pathshim.Status) string {
	if status.PluginErr != nil {
		return fmt.Sprintf("no (%s)", strings.TrimSpace(status.PluginErr.Error()))
	}
	if status.PluginRoot == "" {
		return "no (not checked)"
	}
	return "yes (" + status.PluginRoot + ")"
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

// parsePathArgs reads the flags one path command accepts and rejects everything
// else before a directory or a plugin is touched.
func parsePathArgs(name string, args []string, usage string, allowForce bool) (pathOptions, error) {
	var options pathOptions
	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch {
		case arg == "--force" && allowForce:
			options.force = true
		case arg == "--dir":
			if index+1 >= len(args) {
				return pathOptions{}, usageError{reason: "--dir needs a path", usage: usage}
			}
			index++
			options.dir = args[index]
			if !filepath.IsAbs(options.dir) {
				return pathOptions{}, usageError{reason: fmt.Sprintf("--dir %q is not an absolute path", options.dir), usage: usage}
			}
		case strings.HasPrefix(arg, "-"):
			return pathOptions{}, usageError{reason: fmt.Sprintf("unknown flag %q", arg), usage: usage}
		default:
			return pathOptions{}, usageError{reason: name + " takes no arguments", usage: usage}
		}
	}
	return options, nil
}

// pathPluginResolver is the read-only half of the shim: the same herdr call and
// the same field, run in Go so status can report the plugin without a shell.
func (d deps) pathPluginResolver() func() (string, error) {
	return func() (string, error) {
		bin := "herdr"
		if d.lookPath != nil {
			if path, err := d.lookPath("herdr"); err == nil {
				bin = path
			}
		}
		timeout := d.socketTimeout
		if timeout <= 0 {
			timeout = defaultSocketTimeout
		}
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()

		run := d.herdrRun
		if run == nil {
			run = defaultHerdrRun
		}
		plugin, err := pathshim.Resolve(func() ([]byte, error) {
			stdout, stderr, err := run(ctx, bin, "plugin", "list", "--plugin", "herdr-suite", "--json")
			if err != nil {
				detail := strings.TrimSpace(string(stderr))
				if detail == "" {
					return stdout, err
				}
				return stdout, fmt.Errorf("%v: %s", err, detail)
			}
			return stdout, nil
		})
		if err != nil {
			return "", err
		}
		return plugin.Root, nil
	}
}
