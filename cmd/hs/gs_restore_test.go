package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/WilberC/herdr-suite/internal/gsrestore"
)

type gsRun struct {
	d              deps
	env            map[string]string
	stdout, stderr *bytes.Buffer
	agentDir       string
	sourcePath     string
	source         []byte
}

func newGSRun(t *testing.T) *gsRun {
	t.Helper()
	root := t.TempDir()
	agentDir := filepath.Join(root, "agent")
	if err := os.MkdirAll(filepath.Join(agentDir, "extensions"), 0o755); err != nil {
		t.Fatal(err)
	}
	sourcePath := filepath.Join(root, "repo", "extensions", gsrestore.ReporterFileName)
	if err := os.MkdirAll(filepath.Dir(sourcePath), 0o755); err != nil {
		t.Fatal(err)
	}
	source := []byte(gsrestore.ReporterVersionLine + "\n// shipped reporter body\n")
	if err := os.WriteFile(sourcePath, source, 0o644); err != nil {
		t.Fatal(err)
	}

	run := &gsRun{
		agentDir:   agentDir,
		sourcePath: sourcePath,
		source:     source,
		stdout:     &bytes.Buffer{},
		stderr:     &bytes.Buffer{},
		env: map[string]string{
			"PI_CODING_AGENT_DIR": agentDir,
			"XDG_CONFIG_HOME":     filepath.Join(root, "config"),
			"XDG_STATE_HOME":      filepath.Join(root, "state"),
			"HERDR_SOCKET_PATH":   "/tmp/herdr-suite-test.sock",
		},
	}
	run.d = deps{
		getenv:         func(name string) string { return run.env[name] },
		now:            func() time.Time { return time.Date(2026, 11, 12, 20, 0, 0, 0, time.UTC) },
		lookPath:       func(string) (string, error) { return "", exec.ErrNotFound },
		reporterSource: func() (string, error) { return run.sourcePath, nil },
		out:            run.stdout,
		err:            run.stderr,
	}
	return run
}

func (r *gsRun) reporterPath() string {
	return filepath.Join(r.agentDir, "extensions", gsrestore.ReporterFileName)
}

func (r *gsRun) configPath() string {
	return filepath.Join(r.env["XDG_CONFIG_HOME"], "herdr", "plugins", "config", "herdr-suite", "config.json")
}

func (r *gsRun) writeConfig(t *testing.T, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(r.configPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(r.configPath(), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (r *gsRun) run(t *testing.T, args ...string) int {
	t.Helper()
	return runWith(r.d, args, r.stdout, r.stderr)
}

func TestGSRestoreInstallWritesTheReporterAndReportsIt(t *testing.T) {
	run := newGSRun(t)

	code := run.run(t, "gs-restore", "install")

	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", code, run.stderr)
	}
	want := "installed: " + run.reporterPath() + "\nversion:   v1\n"
	if got := run.stdout.String(); got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
	if run.stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", run.stderr.String())
	}
	data, err := os.ReadFile(run.reporterPath())
	if err != nil {
		t.Fatalf("read the installed reporter: %v", err)
	}
	if string(data) != string(run.source) {
		t.Fatalf("installed reporter = %q, want the shipped bytes", data)
	}
}

func TestGSRestoreInstallIsIdempotent(t *testing.T) {
	run := newGSRun(t)
	if code := run.run(t, "gs-restore", "install"); code != 0 {
		t.Fatalf("first install exit = %d\n%s", code, run.stderr)
	}
	run.stdout.Reset()
	run.stderr.Reset()

	code := run.run(t, "gs-restore", "install")

	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", code, run.stderr)
	}
	want := "installed: " + run.reporterPath() + " (already current)\nversion:   v1\n"
	if got := run.stdout.String(); got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}

func TestGSRestoreInstallRefusesSomeoneElsesFile(t *testing.T) {
	run := newGSRun(t)
	if err := os.WriteFile(run.reporterPath(), []byte("console.log('someone else');\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	code := run.run(t, "gs-restore", "install")

	if code != 2 {
		t.Fatalf("exit = %d, want 2\nstdout:\n%s", code, run.stdout)
	}
	if !strings.Contains(run.stderr.String(), "--force") {
		t.Fatalf("stderr = %q, want a --force hint", run.stderr)
	}
	if data, _ := os.ReadFile(run.reporterPath()); string(data) != "console.log('someone else');\n" {
		t.Fatalf("refused install changed the file: %q", data)
	}
}

func TestGSRestoreInstallWithForceBacksUpAndReports(t *testing.T) {
	run := newGSRun(t)
	if err := os.WriteFile(run.reporterPath(), []byte("console.log('someone else');\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	code := run.run(t, "gs-restore", "install", "--force")

	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", code, run.stderr)
	}
	backup := run.reporterPath() + ".bak-20261112T200000"
	want := "backup:    " + backup + "\ninstalled: " + run.reporterPath() + "\nversion:   v1\n"
	if got := run.stdout.String(); got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
	if data, err := os.ReadFile(backup); err != nil || string(data) != "console.log('someone else');\n" {
		t.Fatalf("backup = %q, %v, want the replaced bytes", data, err)
	}
}

func TestGSRestoreInstallRefusesAnUnsetAgentDir(t *testing.T) {
	run := newGSRun(t)
	delete(run.env, "PI_CODING_AGENT_DIR")

	code := run.run(t, "gs-restore", "install")

	if code != 2 {
		t.Fatalf("exit = %d, want 2\nstdout:\n%s", code, run.stdout)
	}
	if !strings.Contains(run.stderr.String(), "PI_CODING_AGENT_DIR") {
		t.Fatalf("stderr = %q, want it to name PI_CODING_AGENT_DIR", run.stderr)
	}
}

func TestGSRestoreInstallRefusesAnAgentDirWithoutExtensions(t *testing.T) {
	run := newGSRun(t)
	empty := filepath.Join(t.TempDir(), "agent")
	if err := os.MkdirAll(empty, 0o755); err != nil {
		t.Fatal(err)
	}
	run.env["PI_CODING_AGENT_DIR"] = empty

	code := run.run(t, "gs-restore", "install")

	if code != 2 {
		t.Fatalf("exit = %d, want 2\nstdout:\n%s", code, run.stdout)
	}
	stderr := run.stderr.String()
	if !strings.Contains(stderr, "mkdir -p") || !strings.Contains(stderr, filepath.Join(empty, "extensions")) {
		t.Fatalf("stderr = %q, want the directory and a mkdir -p suggestion", stderr)
	}
}

func TestGSRestoreRemoveRestoresTheBackup(t *testing.T) {
	run := newGSRun(t)
	if code := run.run(t, "gs-restore", "install"); code != 0 {
		t.Fatalf("install exit = %d\n%s", code, run.stderr)
	}
	backup := run.reporterPath() + ".bak-20261001T000000"
	if err := os.WriteFile(backup, []byte("vanilla\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run.stdout.Reset()

	code := run.run(t, "gs-restore", "remove")

	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", code, run.stderr)
	}
	want := "restored: " + run.reporterPath() + "\nfrom:     " + backup + "\n"
	if got := run.stdout.String(); got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
	if data, err := os.ReadFile(run.reporterPath()); err != nil || string(data) != "vanilla\n" {
		t.Fatalf("reporter after remove = %q, %v, want the backup", data, err)
	}
}

func TestGSRestoreRemoveDeletesWithoutABackup(t *testing.T) {
	run := newGSRun(t)
	if code := run.run(t, "gs-restore", "install"); code != 0 {
		t.Fatalf("install exit = %d\n%s", code, run.stderr)
	}
	run.stdout.Reset()

	code := run.run(t, "gs-restore", "remove")

	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", code, run.stderr)
	}
	want := "removed: " + run.reporterPath() + "\n"
	if got := run.stdout.String(); got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
	if _, err := os.Stat(run.reporterPath()); !os.IsNotExist(err) {
		t.Fatalf("stat = %v, want the reporter gone", err)
	}
}

func TestGSRestoreStatusReportsTheDefaultState(t *testing.T) {
	run := newGSRun(t)

	code := run.run(t, "gs-restore", "status")

	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", code, run.stderr)
	}
	want := strings.Join([]string{
		"config:       " + run.configPath() + " (missing)",
		"install:      off",
		"auto-install: off",
		"agent dir:    " + run.agentDir,
		"reporter:     not installed",
		"version:      no reporter installed",
		"backup:       none",
		"gentle-shell: not resolvable",
		"",
	}, "\n")
	if got := run.stdout.String(); got != want {
		t.Fatalf("stdout =\n%s\nwant:\n%s", got, want)
	}
}

func TestGSRestoreStatusReportsAnInstalledReporterAndConfig(t *testing.T) {
	run := newGSRun(t)
	if code := run.run(t, "gs-restore", "install"); code != 0 {
		t.Fatalf("install exit = %d\n%s", code, run.stderr)
	}
	run.stdout.Reset()
	run.writeConfig(t, `{"gs_restore": {"install": true, "auto_install": true}}`)

	code := run.run(t, "gs-restore", "status")

	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", code, run.stderr)
	}
	want := strings.Join([]string{
		"config:       " + run.configPath() + " (present)",
		"install:      on",
		"auto-install: on",
		"agent dir:    " + run.agentDir,
		"reporter:     installed at " + run.reporterPath(),
		"version:      matches (v1)",
		"backup:       none",
		"gentle-shell: not resolvable",
		"",
	}, "\n")
	if got := run.stdout.String(); got != want {
		t.Fatalf("stdout =\n%s\nwant:\n%s", got, want)
	}
}

func TestGSRestoreStatusReportsABackupAndADifferentVersion(t *testing.T) {
	run := newGSRun(t)
	venv := []byte("// HERDR_SUITE_GS_RESTORE_VERSION=0\n")
	if err := os.WriteFile(run.reporterPath(), venv, 0o644); err != nil {
		t.Fatal(err)
	}
	backup := run.reporterPath() + ".bak-20261001T000000"
	if err := os.WriteFile(backup, []byte("vanilla\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	code := run.run(t, "gs-restore", "status")

	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", code, run.stderr)
	}
	want := strings.Join([]string{
		"config:       " + run.configPath() + " (missing)",
		"install:      off",
		"auto-install: off",
		"agent dir:    " + run.agentDir,
		"reporter:     installed at " + run.reporterPath(),
		"version:      differs (installed v0, shipped v1)",
		"backup:       present at " + backup,
		"gentle-shell: not resolvable",
		"",
	}, "\n")
	if got := run.stdout.String(); got != want {
		t.Fatalf("stdout =\n%s\nwant:\n%s", got, want)
	}
}

func TestGSRestoreStatusReportsAnUnsetAgentDir(t *testing.T) {
	run := newGSRun(t)
	delete(run.env, "PI_CODING_AGENT_DIR")

	code := run.run(t, "gs-restore", "status")

	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", code, run.stderr)
	}
	for _, want := range []string{
		"agent dir:    not set in PI_CODING_AGENT_DIR",
		"reporter:     unknown (no agent dir)",
		"version:      unknown (no agent dir)",
		"backup:       unknown (no agent dir)",
	} {
		if !strings.Contains(run.stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", run.stdout, want)
		}
	}
}

func TestGSRestoreStatusResolvesGentleShellThroughPATH(t *testing.T) {
	bin := t.TempDir()
	executable := filepath.Join(bin, "gentle-shell")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)

	run := newGSRun(t)
	run.d.lookPath = exec.LookPath

	code := run.run(t, "gs-restore", "status")

	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", code, run.stderr)
	}
	if want := "gentle-shell: resolvable at " + executable; !strings.Contains(run.stdout.String(), want) {
		t.Fatalf("stdout = %q, want %q", run.stdout, want)
	}
}

func TestGSRestoreStartupDoesNothingWithBothTogglesOff(t *testing.T) {
	run := newGSRun(t)

	code := run.run(t, "gs-restore", "startup")

	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if run.stdout.Len() != 0 || run.stderr.Len() != 0 {
		t.Fatalf("stdout = %q, stderr = %q, want silence", run.stdout, run.stderr)
	}
	if _, err := os.Stat(run.reporterPath()); !os.IsNotExist(err) {
		t.Fatalf("startup wrote the agent dir with the install toggle off: %v", err)
	}
}

func TestGSRestoreStartupIgnoresAutoInstallWithoutInstall(t *testing.T) {
	run := newGSRun(t)
	run.writeConfig(t, `{"gs_restore": {"install": false, "auto_install": true}}`)

	code := run.run(t, "gs-restore", "startup")

	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if run.stdout.Len() != 0 || run.stderr.Len() != 0 {
		t.Fatalf("stdout = %q, stderr = %q, want install to stay the master gate", run.stdout, run.stderr)
	}
	if _, err := os.Stat(run.reporterPath()); !os.IsNotExist(err) {
		t.Fatalf("startup wrote the agent dir with install off: %v", err)
	}
}

func TestGSRestoreStartupHintsOncePerSocketWithoutAutoInstall(t *testing.T) {
	run := newGSRun(t)
	run.writeConfig(t, `{"gs_restore": {"install": true, "auto_install": false}}`)

	if code := run.run(t, "gs-restore", "startup"); code != 0 {
		t.Fatalf("first startup exit = %d", code)
	}
	want := "herdr-suite: GS session restore is available; run \"bin/hs gs-restore install\" to enable it, or set gs_restore.auto_install = true\n"
	if got := run.stderr.String(); got != want {
		t.Fatalf("stderr = %q, want %q", got, want)
	}
	if run.stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", run.stdout)
	}
	if _, err := os.Stat(run.reporterPath()); !os.IsNotExist(err) {
		t.Fatalf("startup wrote the agent dir with auto-install off: %v", err)
	}

	run.stderr.Reset()
	if code := run.run(t, "gs-restore", "startup"); code != 0 {
		t.Fatalf("second startup exit = %d", code)
	}
	if run.stderr.Len() != 0 {
		t.Fatalf("second startup stderr = %q, want the hint once per socket", run.stderr)
	}

	run.env["HERDR_SOCKET_PATH"] = "/tmp/herdr-suite-other.sock"
	if code := run.run(t, "gs-restore", "startup"); code != 0 {
		t.Fatalf("third startup exit = %d", code)
	}
	if run.stderr.String() != want {
		t.Fatalf("third startup stderr = %q, want the hint for the new socket", run.stderr)
	}
}

func TestGSRestoreStartupInstallsIdempotentlyWithAutoInstall(t *testing.T) {
	run := newGSRun(t)
	run.writeConfig(t, `{"gs_restore": {"install": true, "auto_install": true}}`)

	if code := run.run(t, "gs-restore", "startup"); code != 0 {
		t.Fatalf("first startup exit = %d\nstderr:\n%s", code, run.stderr)
	}
	want := "herdr-suite: installed GS session restore reporter at " + run.reporterPath() + "\n"
	if got := run.stderr.String(); got != want {
		t.Fatalf("stderr = %q, want %q", got, want)
	}
	if run.stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", run.stdout)
	}
	if _, err := os.Stat(run.reporterPath()); err != nil {
		t.Fatalf("startup did not install the reporter: %v", err)
	}

	run.stderr.Reset()
	if code := run.run(t, "gs-restore", "startup"); code != 0 {
		t.Fatalf("second startup exit = %d", code)
	}
	if run.stderr.Len() != 0 {
		t.Fatalf("second startup stderr = %q, want silence when already current", run.stderr)
	}
}

func TestGSRestoreStartupReportsABackupItTakes(t *testing.T) {
	run := newGSRun(t)
	run.writeConfig(t, `{"gs_restore": {"install": true, "auto_install": true}}`)
	vanilla := "// HERDR_INTEGRATION_ID=pi\n"
	if err := os.WriteFile(run.reporterPath(), []byte(vanilla), 0o644); err != nil {
		t.Fatal(err)
	}

	code := run.run(t, "gs-restore", "startup")

	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", code, run.stderr)
	}
	backup := run.reporterPath() + ".bak-20261112T200000"
	want := "herdr-suite: backed up the previous reporter to " + backup + "\n" +
		"herdr-suite: installed GS session restore reporter at " + run.reporterPath() + "\n"
	if got := run.stderr.String(); got != want {
		t.Fatalf("stderr = %q, want %q", got, want)
	}
}

func TestGSRestoreStartupFailsLoudlyWhenItCannotInstall(t *testing.T) {
	run := newGSRun(t)
	run.writeConfig(t, `{"gs_restore": {"install": true, "auto_install": true}}`)
	delete(run.env, "PI_CODING_AGENT_DIR")

	code := run.run(t, "gs-restore", "startup")

	if code != 2 {
		t.Fatalf("exit = %d, want 2\nstdout:\n%s", code, run.stdout)
	}
	if !strings.Contains(run.stderr.String(), "PI_CODING_AGENT_DIR") {
		t.Fatalf("stderr = %q, want it to name PI_CODING_AGENT_DIR", run.stderr)
	}
}

func TestGSRestoreHelpDocumentsEverySubcommand(t *testing.T) {
	run := newGSRun(t)

	code := run.run(t, "gs-restore", "--help")

	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	for _, want := range []string{"install", "remove", "status", "startup", "--force"} {
		if !strings.Contains(run.stdout.String(), want) {
			t.Fatalf("help = %q, want %q", run.stdout, want)
		}
	}
}

func TestPluginManifestDeclaresTheDefaultOffGSRestoreFeature(t *testing.T) {
	data := readManifestBytes(t)
	document := parseManifest(t, data)

	var startup []string
	for _, entry := range document.Startup {
		command, _ := entry["command"].([]any)
		joined := make([]string, 0, len(command))
		for _, token := range command {
			joined = append(joined, token.(string))
		}
		if strings.Join(joined, " ") == "bin/hs gs-restore startup" {
			startup = joined
		}
	}
	if startup == nil {
		t.Fatalf("startup = %v, want a bin/hs gs-restore startup entry", document.Startup)
	}

	var action *manifestAction
	for index := range document.Actions {
		if document.Actions[index].ID == "gs-install" {
			action = &document.Actions[index]
		}
	}
	if action == nil {
		t.Fatalf("actions = %v, want a gs-install action", document.Actions)
	}
	if action.Title != "Install GS session restore" {
		t.Fatalf("gs-install title = %q, want %q", action.Title, "Install GS session restore")
	}
	if strings.Join(action.Contexts, ",") != "global" {
		t.Fatalf("gs-install contexts = %v, want [global]", action.Contexts)
	}
	if strings.Join(action.Command, " ") != "bin/hs gs-restore install" {
		t.Fatalf("gs-install command = %v, want bin/hs gs-restore install", action.Command)
	}

	if !strings.Contains(string(data), "GS session restore is default off") {
		t.Fatal("the manifest comment must state that the feature is default off")
	}
}

func TestPluginManifestKeepsTheExistingSurface(t *testing.T) {
	document := parseManifest(t, readManifestBytes(t))

	ids := make(map[string]bool, len(document.Actions))
	for _, action := range document.Actions {
		ids[action.ID] = true
	}
	for _, want := range []string{"save", "load", "list"} {
		if !ids[want] {
			t.Fatalf("actions = %v, want %q preserved", document.Actions, want)
		}
	}
	if len(document.Panes) != 2 {
		t.Fatalf("panes = %v, want the load and layouts panes unchanged", document.Panes)
	}
	if len(document.Events) != 0 {
		t.Fatalf("events = %v, want none", document.Events)
	}
}

func TestGSRestoreStatusReportsAnInvalidConfig(t *testing.T) {
	run := newGSRun(t)
	run.writeConfig(t, `{"gs_restore": {"install": "yes"}}`)

	code := run.run(t, "gs-restore", "status")

	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", code, run.stderr)
	}
	if !strings.Contains(run.stdout.String(), "(invalid: ") {
		t.Fatalf("stdout = %q, want the invalid config reported", run.stdout)
	}
}

func TestGSRestoreStartupStaysOffWithAnInvalidConfig(t *testing.T) {
	run := newGSRun(t)
	run.writeConfig(t, `{"gs_restore": {"install": "yes"}}`)

	code := run.run(t, "gs-restore", "startup")

	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if _, err := os.Stat(run.reporterPath()); !os.IsNotExist(err) {
		t.Fatalf("startup installed from an invalid config: %v", err)
	}
	if !strings.Contains(run.stderr.String(), "stays off") {
		t.Fatalf("stderr = %q, want a brief line explaining the feature stays off", run.stderr)
	}
}

// TestGSRestoreStatusPrintsTheReportAsJSON pins the machine-readable form of the
// report: one document carrying every value the human lines print.
func TestGSRestoreStatusPrintsTheReportAsJSON(t *testing.T) {
	run := newGSRun(t)
	if code := run.run(t, "gs-restore", "install"); code != 0 {
		t.Fatalf("install exit = %d\n%s", code, run.stderr)
	}
	run.stdout.Reset()
	run.writeConfig(t, `{"gs_restore": {"install": true, "auto_install": true}}`)

	code := run.run(t, "gs-restore", "status", "--json")

	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", code, run.stderr)
	}
	if run.stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", run.stderr.String())
	}
	var document gsrestore.StatusDocument
	if err := json.Unmarshal(run.stdout.Bytes(), &document); err != nil {
		t.Fatalf("stdout is not the status document: %v\n%s", err, run.stdout)
	}
	if !strings.Contains(run.stdout.String(), "\n") || len(strings.Split(strings.TrimSpace(run.stdout.String()), "\n")) != 1 {
		t.Fatalf("stdout = %q, want one JSON document", run.stdout.String())
	}

	if document.ConfigPath != run.configPath() || !document.ConfigPresent || document.ConfigError != "" {
		t.Fatalf("config fields = %#v, want the present config at %s", document, run.configPath())
	}
	if !document.Install || !document.AutoInstall {
		t.Fatalf("toggles = install %v, auto_install %v, want both on", document.Install, document.AutoInstall)
	}
	if document.AgentDir != run.agentDir || document.AgentDirError != "" {
		t.Fatalf("agent dir fields = %#v, want %s", document, run.agentDir)
	}
	if document.ReporterPath != run.reporterPath() || !document.ReporterPresent || !document.ReporterInstalled {
		t.Fatalf("reporter fields = %#v, want the installed reporter at %s", document, run.reporterPath())
	}
	if document.ReporterVersion != "v1" || !document.VersionMatches {
		t.Fatalf("version fields = %#v, want the shipped v1", document)
	}
	if document.ShippedVersion != gsrestore.ReporterVersion() {
		t.Fatalf("ShippedVersion = %q, want %q", document.ShippedVersion, gsrestore.ReporterVersion())
	}
	if document.Backup != "" || document.GentleShell != "" {
		t.Fatalf("document = %#v, want no backup and no launcher", document)
	}
}

// TestGSRestoreStatusJSONMirrorsTheHumanReport keeps the two surfaces in step:
// every field the human report prints has the same value in the document.
func TestGSRestoreStatusJSONMirrorsTheHumanReport(t *testing.T) {
	run := newGSRun(t)

	if code := run.run(t, "gs-restore", "status"); code != 0 {
		t.Fatalf("status exit = %d\n%s", code, run.stderr)
	}
	human := run.stdout.String()
	run.stdout.Reset()

	if code := run.run(t, "gs-restore", "status", "--json"); code != 0 {
		t.Fatalf("status --json exit = %d\n%s", code, run.stderr)
	}
	var document gsrestore.StatusDocument
	if err := json.Unmarshal(run.stdout.Bytes(), &document); err != nil {
		t.Fatalf("stdout is not the status document: %v\n%s", err, run.stdout)
	}

	lines := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(human), "\n") {
		label, value, ok := strings.Cut(line, ":")
		if !ok {
			t.Fatalf("human line %q holds no label", line)
		}
		lines[strings.TrimSpace(label)] = strings.TrimSpace(value)
	}

	checks := []struct {
		label string
		ok    bool
		want  bool
	}{
		{label: "install", ok: document.Install, want: lines["install"] == "on"},
		{label: "auto-install", ok: document.AutoInstall, want: lines["auto-install"] == "on"},
		{label: "config", ok: document.ConfigPresent, want: strings.HasSuffix(lines["config"], "(present)")},
		{label: "reporter", ok: document.ReporterInstalled, want: strings.HasPrefix(lines["reporter"], "installed at "+document.ReporterPath)},
		{label: "version", ok: document.VersionMatches, want: strings.HasPrefix(lines["version"], "matches")},
	}
	for _, check := range checks {
		if check.ok != check.want {
			t.Fatalf("%s: document says %v, human line %q says otherwise", check.label, check.ok, lines[check.label])
		}
	}
	if got, want := lines["agent dir"], document.AgentDir; got != want {
		t.Fatalf("agent dir = %q, document says %q", got, want)
	}
	if document.Backup != "" || lines["backup"] != "none" {
		t.Fatalf("backup = %q, human line = %q, want none", document.Backup, lines["backup"])
	}
	if document.GentleShell != "" || lines["gentle-shell"] != "not resolvable" {
		t.Fatalf("gentle-shell = %q, human line = %q, want unresolved", document.GentleShell, lines["gentle-shell"])
	}
}

func TestGSRestoreStatusRejectsBadArguments(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "an unknown flag",
			args: []string{"gs-restore", "status", "--bogus"},
			want: `unknown flag "--bogus"`,
		},
		{
			name: "a positional argument",
			args: []string{"gs-restore", "status", "extra"},
			want: "status takes no arguments",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			run := newGSRun(t)

			code := run.run(t, tt.args...)

			if code != 2 {
				t.Fatalf("exit = %d, want 2\nstdout:\n%s\nstderr:\n%s", code, run.stdout, run.stderr)
			}
			if !strings.Contains(run.stderr.String(), tt.want) {
				t.Fatalf("stderr = %q, want it to contain %q", run.stderr.String(), tt.want)
			}
			if !strings.Contains(run.stderr.String(), "usage: hs gs-restore status") {
				t.Fatalf("stderr = %q, want the status usage", run.stderr.String())
			}
			if run.stdout.Len() != 0 {
				t.Fatalf("stdout = %q, want empty", run.stdout.String())
			}
		})
	}
}
