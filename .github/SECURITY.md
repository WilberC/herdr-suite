# Security policy

## Supported versions

One line: the latest release is supported, and upgrades are as simple as
re-running `herdr plugin install WilberC/herdr-suite` (or `herdr plugin update`).

## What this plugin touches

Reading what a plugin can reach is the first question. This one:

- talks to herdr through the local CLI and session socket, like every plugin;
- reads and writes layout files in its own config directory and in `.herdr/`
  directories of the projects you run it from;
- optionally (default off) installs a reporter file into the gentle-shell agent
  directory after showing what it does;
- downloads release binaries and verifies them against `SHA256SUMS`.

It collects no telemetry, runs no background daemons beyond the startup gate
documented with its config toggles, and sends nothing anywhere except herdr's
own socket.

## How to report

Open a private security advisory through GitHub's "Report a vulnerability" on
this repository. Do not open a public issue with exploit details.
