package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/WilberC/herdr-suite/internal/pathshim"
)

type pathRun struct {
	d              deps
	root           string
	home           string
	env            map[string]string
	stdout, stderr bytes.Buffer

	// pluginRoot is the checkout the scripted herdr reports. It is a temp
	// directory with bin/hs in place.
	pluginRoot string
	// herdrOutput overrides what the scripted herdr prints; when empty the
	// plugin list carries pluginRoot.
	herdrOutput string
	herdrErr    error
}

func newPathRun(t *testing.T) *pathRun {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	pluginRoot := filepath.Join(root, "plugin")
	if err := os.MkdirAll(filepath.Join(pluginRoot, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginRoot, "bin", "hs"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	run := &pathRun{root: root, home: home, pluginRoot: pluginRoot}
	run.env = map[string]string{
		"HOME": home,
		"PATH": "/usr/bin:/bin",
	}
	run.d = deps{
		getenv: func(name string) string { return run.env[name] },
		now:    func() time.Time { return time.Date(2026, 11, 13, 10, 30, 0, 0, time.UTC) },
		lookPath: func(name string) (string, error) {
			return filepath.Join(root, "bin", name), nil
		},
		herdrRun: func(context.Context, string, ...string) ([]byte, []byte, error) {
			if run.herdrErr != nil {
				return nil, []byte("herdr: not running"), run.herdrErr
			}
			if run.herdrOutput != "" {
				return []byte(run.herdrOutput), nil, nil
			}
			return []byte(`{"id":"cli:plugin","result":{"plugins":[{"plugin_id":"herdr-suite","plugin_root":"` +
				run.pluginRoot + `"}],"type":"plugin_list"}}`), nil, nil
		},
		socketTimeout: time.Second,
		out:           &run.stdout,
		err:           &run.stderr,
	}
	return run
}

func (r *pathRun) binDir() string {
	return filepath.Join(r.home, ".local", "bin")
}

func (r *pathRun) shimPath() string {
	return filepath.Join(r.binDir(), "hs")
}

func (r *pathRun) run(t *testing.T, args ...string) int {
	t.Helper()
	return runWith(r.d, args, &r.stdout, &r.stderr)
}

func (r *pathRun) writeShim(t *testing.T, body string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(r.binDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(r.shimPath(), []byte(body), mode); err != nil {
		t.Fatal(err)
	}
}

func (r *pathRun) backups(t *testing.T) []string {
	t.Helper()
	matches, err := filepath.Glob(r.shimPath() + ".bak-*")
	if err != nil {
		t.Fatal(err)
	}
	return matches
}

func (r *pathRun) wantStdout(t *testing.T, want string) {
	t.Helper()
	if r.stdout.String() != want {
		t.Fatalf("stdout = %q, want %q\nstderr:\n%s", r.stdout.String(), want, r.stderr.String())
	}
	if r.stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", r.stderr.String())
	}
}

// onPathLine is the PATH line every install prints: on PATH, and the warning
// with the export line to copy when it is not.
func onPathLine(dir string, onPath bool) string {
	if onPath {
		return "PATH:      " + dir + " is in PATH\n"
	}
	return "PATH:      " + dir + " is not in PATH; add it to your shell with: export PATH=\"" + dir + ":$PATH\"\n"
}

func TestPathInstallWritesTheShimAndReportsIt(t *testing.T) {
	run := newPathRun(t)

	code := run.run(t, "path", "install")

	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", code, run.stderr.String())
	}
	run.wantStdout(t, "installed: "+run.shimPath()+"\n"+onPathLine(run.binDir(), false))

	data, err := os.ReadFile(run.shimPath())
	if err != nil {
		t.Fatalf("read the shim: %v", err)
	}
	if string(data) != pathshim.Script {
		t.Fatalf("the shim differs from the generated script:\n%s", data)
	}
	info, err := os.Stat(run.shimPath())
	if err != nil {
		t.Fatalf("stat the shim: %v", err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("shim mode = %v, want 0755 so a shell can run it", info.Mode().Perm())
	}
	if backups := run.backups(t); len(backups) != 0 {
		t.Fatalf("backups = %v, want none for a fresh install", backups)
	}
}

func TestPathInstallCreatesTheDirectoryAndReportsAnOnPathBin(t *testing.T) {
	run := newPathRun(t)
	run.env["PATH"] = run.binDir() + ":/usr/bin:/bin"

	code := run.run(t, "path", "install")

	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", code, run.stderr.String())
	}
	run.wantStdout(t, "installed: "+run.shimPath()+"\n"+onPathLine(run.binDir(), true))
	info, err := os.Stat(run.binDir())
	if err != nil || !info.IsDir() {
		t.Fatalf("target directory was not created: %v", err)
	}
}

func TestPathInstallIsIdempotent(t *testing.T) {
	run := newPathRun(t)
	if code := run.run(t, "path", "install"); code != 0 {
		t.Fatalf("first install exit = %d, want 0\nstderr:\n%s", code, run.stderr.String())
	}
	before, err := os.ReadFile(run.shimPath())
	if err != nil {
		t.Fatal(err)
	}
	run.stdout.Reset()
	run.stderr.Reset()

	code := run.run(t, "path", "install")

	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", code, run.stderr.String())
	}
	run.wantStdout(t, "installed: "+run.shimPath()+" (already current)\n"+onPathLine(run.binDir(), false))
	after, err := os.ReadFile(run.shimPath())
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("the second install rewrote the shim")
	}
	if backups := run.backups(t); len(backups) != 0 {
		t.Fatalf("backups = %v, want none for an idempotent install", backups)
	}
}

func TestPathInstallRefusesAFileItDidNotWrite(t *testing.T) {
	run := newPathRun(t)
	run.writeShim(t, "#!/bin/sh\necho another hs\n", 0o755)

	code := run.run(t, "path", "install")

	if code != 2 {
		t.Fatalf("exit = %d, want 2\nstdout:\n%s\nstderr:\n%s", code, run.stdout.String(), run.stderr.String())
	}
	for _, want := range []string{
		"hs: " + run.shimPath() + " already exists and is not an hs path shim",
		"pass --force to replace it and keep a backup",
	} {
		if !strings.Contains(run.stderr.String(), want) {
			t.Fatalf("stderr = %q, want it to contain %q", run.stderr.String(), want)
		}
	}
	if run.stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", run.stdout.String())
	}
	data, err := os.ReadFile(run.shimPath())
	if err != nil || string(data) != "#!/bin/sh\necho another hs\n" {
		t.Fatalf("the refused file changed: %q (%v)", data, err)
	}
	if backups := run.backups(t); len(backups) != 0 {
		t.Fatalf("backups = %v, want none after a refusal", backups)
	}
}

func TestPathInstallForceBacksUpTheFileItReplaces(t *testing.T) {
	run := newPathRun(t)
	run.writeShim(t, "#!/bin/sh\necho another hs\n", 0o644)

	code := run.run(t, "path", "install", "--force")

	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", code, run.stderr.String())
	}
	backup := run.shimPath() + ".bak-20261113T103000"
	run.wantStdout(t, "backup:    "+backup+"\ninstalled: "+run.shimPath()+"\n"+onPathLine(run.binDir(), false))
	kept, err := os.ReadFile(backup)
	if err != nil {
		t.Fatalf("read the backup: %v", err)
	}
	if string(kept) != "#!/bin/sh\necho another hs\n" {
		t.Fatalf("backup = %q, want the replaced file", kept)
	}
	data, err := os.ReadFile(run.shimPath())
	if err != nil || string(data) != pathshim.Script {
		t.Fatalf("the shim was not written over the replaced file: %v", err)
	}
	info, err := os.Stat(run.shimPath())
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("shim mode = %v, want 0755 even over a file that was not executable", info.Mode().Perm())
	}
}

// TestPathInstallKeepsASingleBackup pins the gs-restore pattern: a repeated
// --force install refreshes the one backup instead of accumulating copies.
func TestPathInstallKeepsASingleBackup(t *testing.T) {
	run := newPathRun(t)
	run.writeShim(t, "#!/bin/sh\necho first\n", 0o755)
	if code := run.run(t, "path", "install", "--force"); code != 0 {
		t.Fatalf("first install exit = %d, want 0\nstderr:\n%s", code, run.stderr.String())
	}
	run.writeShim(t, "#!/bin/sh\necho second\n", 0o755)
	run.stdout.Reset()
	run.stderr.Reset()

	if code := run.run(t, "path", "install", "--force"); code != 0 {
		t.Fatalf("second install exit = %d, want 0\nstderr:\n%s", code, run.stderr.String())
	}

	backups := run.backups(t)
	if len(backups) != 1 {
		t.Fatalf("backups = %v, want exactly one", backups)
	}
	kept, err := os.ReadFile(backups[0])
	if err != nil {
		t.Fatal(err)
	}
	if string(kept) != "#!/bin/sh\necho second\n" {
		t.Fatalf("backup = %q, want the latest replaced file", kept)
	}
}

func TestPathInstallRefusesWithoutAHome(t *testing.T) {
	run := newPathRun(t)
	delete(run.env, "HOME")

	code := run.run(t, "path", "install")

	if code != 2 {
		t.Fatalf("exit = %d, want 2\nstdout:\n%s\nstderr:\n%s", code, run.stdout.String(), run.stderr.String())
	}
	for _, want := range []string{"hs: HOME is not set", "--dir"} {
		if !strings.Contains(run.stderr.String(), want) {
			t.Fatalf("stderr = %q, want it to contain %q", run.stderr.String(), want)
		}
	}
	if run.stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", run.stdout.String())
	}
}

func TestPathInstallHonoursDir(t *testing.T) {
	run := newPathRun(t)
	dir := filepath.Join(run.root, "custom", "bin")

	code := run.run(t, "path", "install", "--dir", dir)

	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", code, run.stderr.String())
	}
	run.wantStdout(t, "installed: "+filepath.Join(dir, "hs")+"\n"+onPathLine(dir, false))
	if _, err := os.Stat(run.binDir()); !os.IsNotExist(err) {
		t.Fatalf("the default directory was created despite --dir: %v", err)
	}
}

func TestPathInstallRejectsABadDirOrExtraArguments(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "a relative dir", args: []string{"path", "install", "--dir", "bin"}, want: `--dir "bin" is not an absolute path`},
		{name: "a missing dir value", args: []string{"path", "install", "--dir"}, want: "--dir needs a path"},
		{name: "an unknown flag", args: []string{"path", "install", "--bogus"}, want: `unknown flag "--bogus"`},
		{name: "an extra argument", args: []string{"path", "install", "extra"}, want: "install takes no arguments"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			run := newPathRun(t)

			code := run.run(t, tt.args...)

			if code != 2 {
				t.Fatalf("exit = %d, want 2\nstderr:\n%s", code, run.stderr.String())
			}
			for _, want := range []string{"hs: " + tt.want, "usage: hs path install"} {
				if !strings.Contains(run.stderr.String(), want) {
					t.Fatalf("stderr = %q, want it to contain %q", run.stderr.String(), want)
				}
			}
			if run.stdout.Len() != 0 {
				t.Fatalf("stdout = %q, want empty", run.stdout.String())
			}
		})
	}
}

func TestPathRemoveGrammar(t *testing.T) {
	t.Run("nothing installed", func(t *testing.T) {
		run := newPathRun(t)

		code := run.run(t, "path", "remove")

		if code != 0 {
			t.Fatalf("exit = %d, want 0\nstderr:\n%s", code, run.stderr.String())
		}
		run.wantStdout(t, "nothing to remove at "+run.shimPath()+"\n")
	})

	t.Run("our shim", func(t *testing.T) {
		run := newPathRun(t)
		run.writeShim(t, pathshim.Script, 0o755)

		code := run.run(t, "path", "remove")

		if code != 0 {
			t.Fatalf("exit = %d, want 0\nstderr:\n%s", code, run.stderr.String())
		}
		run.wantStdout(t, "removed:   "+run.shimPath()+"\n")
		if _, err := os.Stat(run.shimPath()); !os.IsNotExist(err) {
			t.Fatalf("the shim survived remove: %v", err)
		}
	})

	t.Run("a file we did not write", func(t *testing.T) {
		run := newPathRun(t)
		run.writeShim(t, "#!/bin/sh\necho another hs\n", 0o755)

		code := run.run(t, "path", "remove")

		if code != 2 {
			t.Fatalf("exit = %d, want 2\nstdout:\n%s\nstderr:\n%s", code, run.stdout.String(), run.stderr.String())
		}
		for _, want := range []string{
			"hs: " + run.shimPath() + " is not an hs path shim",
			"pass --force to remove it and keep a backup",
		} {
			if !strings.Contains(run.stderr.String(), want) {
				t.Fatalf("stderr = %q, want it to contain %q", run.stderr.String(), want)
			}
		}
		if _, err := os.Stat(run.shimPath()); err != nil {
			t.Fatalf("the refused file was removed: %v", err)
		}
	})

	t.Run("a file we did not write, forced", func(t *testing.T) {
		run := newPathRun(t)
		run.writeShim(t, "#!/bin/sh\necho another hs\n", 0o755)

		code := run.run(t, "path", "remove", "--force")

		if code != 0 {
			t.Fatalf("exit = %d, want 0\nstderr:\n%s", code, run.stderr.String())
		}
		backup := run.shimPath() + ".bak-20261113T103000"
		run.wantStdout(t, "backup:    "+backup+" (kept)\nremoved:   "+run.shimPath()+"\n")
		kept, err := os.ReadFile(backup)
		if err != nil {
			t.Fatalf("read the backup: %v", err)
		}
		if string(kept) != "#!/bin/sh\necho another hs\n" {
			t.Fatalf("backup = %q, want the removed file", kept)
		}
	})

	t.Run("a file we did not write, with a backup present", func(t *testing.T) {
		run := newPathRun(t)
		run.writeShim(t, "#!/bin/sh\necho another hs\n", 0o755)
		backup := run.shimPath() + ".bak-20260101T000000"
		if err := os.WriteFile(backup, []byte("#!/bin/sh\necho original\n"), 0o755); err != nil {
			t.Fatal(err)
		}

		code := run.run(t, "path", "remove")

		if code != 0 {
			t.Fatalf("exit = %d, want 0\nstderr:\n%s", code, run.stderr.String())
		}
		run.wantStdout(t, "backup:    "+backup+" (kept)\nremoved:   "+run.shimPath()+"\n")
	})

	t.Run("our shim with a backup present", func(t *testing.T) {
		run := newPathRun(t)
		run.writeShim(t, pathshim.Script, 0o755)
		backup := run.shimPath() + ".bak-20260101T000000"
		if err := os.WriteFile(backup, []byte("#!/bin/sh\necho original\n"), 0o755); err != nil {
			t.Fatal(err)
		}

		code := run.run(t, "path", "remove")

		if code != 0 {
			t.Fatalf("exit = %d, want 0\nstderr:\n%s", code, run.stderr.String())
		}
		run.wantStdout(t, "backup:    "+backup+" (kept)\nremoved:   "+run.shimPath()+"\n")
	})

	t.Run("extra arguments", func(t *testing.T) {
		run := newPathRun(t)

		code := run.run(t, "path", "remove", "extra")

		if code != 2 {
			t.Fatalf("exit = %d, want 2\nstderr:\n%s", code, run.stderr.String())
		}
		if !strings.Contains(run.stderr.String(), "remove takes no arguments") {
			t.Fatalf("stderr = %q, want the remove usage refusal", run.stderr.String())
		}
	})
}

func TestPathStatusReportsTheFourFacts(t *testing.T) {
	run := newPathRun(t)
	run.pluginRoot = filepath.Join(run.root, "plugin")

	code := run.run(t, "path", "status")

	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", code, run.stderr.String())
	}
	want := strings.Join([]string{
		"shim:       " + run.shimPath(),
		"installed:  no",
		"matches:    no",
		"on PATH:    no; add it to your shell with: export PATH=\"" + run.binDir() + ":$PATH\"",
		"plugin:     yes (" + run.pluginRoot + ")",
		"",
	}, "\n")
	run.wantStdout(t, want)
}

func TestPathStatusReportsAnInstalledCurrentShimOnPath(t *testing.T) {
	run := newPathRun(t)
	run.writeShim(t, pathshim.Script, 0o755)
	run.env["PATH"] = run.binDir() + ":/usr/bin:/bin"

	code := run.run(t, "path", "status")

	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", code, run.stderr.String())
	}
	want := strings.Join([]string{
		"shim:       " + run.shimPath(),
		"installed:  yes",
		"matches:    yes",
		"on PATH:    yes",
		"plugin:     yes (" + run.pluginRoot + ")",
		"",
	}, "\n")
	run.wantStdout(t, want)
}

func TestPathStatusReportsAChangedShim(t *testing.T) {
	run := newPathRun(t)
	run.writeShim(t, "#!/bin/sh\necho another hs\n", 0o755)

	code := run.run(t, "path", "status")

	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", code, run.stderr.String())
	}
	for _, want := range []string{"installed:  yes", "matches:    no"} {
		if !strings.Contains(run.stdout.String(), want) {
			t.Fatalf("stdout = %q, want it to contain %q", run.stdout.String(), want)
		}
	}
}

func TestPathStatusReportsAMissingPlugin(t *testing.T) {
	run := newPathRun(t)
	run.herdrOutput = `{"id":"cli:plugin","result":{"plugins":[],"type":"plugin_list"}}`

	code := run.run(t, "path", "status")

	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", code, run.stderr.String())
	}
	want := "plugin:     no (the herdr-suite plugin is not installed; run: herdr plugin install WilberC/herdr-suite)"
	if !strings.Contains(run.stdout.String(), want) {
		t.Fatalf("stdout = %q, want it to contain %q", run.stdout.String(), want)
	}
}

func TestPathStatusReportsAFailingHerdr(t *testing.T) {
	run := newPathRun(t)
	run.herdrErr = errors.New("exit status 1")

	code := run.run(t, "path", "status")

	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", code, run.stderr.String())
	}
	for _, want := range []string{"plugin:     no (read herdr plugin list: ", "exit status 1", "herdr: not running"} {
		if !strings.Contains(run.stdout.String(), want) {
			t.Fatalf("stdout = %q, want it to contain %q", run.stdout.String(), want)
		}
	}
}

func TestPathStatusWithoutAHome(t *testing.T) {
	run := newPathRun(t)
	delete(run.env, "HOME")

	code := run.run(t, "path", "status")

	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", code, run.stderr.String())
	}
	want := strings.Join([]string{
		"shim:       unresolved (HOME is not set; it names the home directory that holds .local/bin, so pass --dir <path>)",
		"installed:  no",
		"matches:    no",
		"on PATH:    no (no target directory)",
		"plugin:     yes (" + run.pluginRoot + ")",
		"",
	}, "\n")
	run.wantStdout(t, want)
}

func TestPathStatusHonoursDir(t *testing.T) {
	run := newPathRun(t)
	dir := filepath.Join(run.root, "custom", "bin")
	if code := run.run(t, "path", "install", "--dir", dir); code != 0 {
		t.Fatalf("install exit = %d, want 0\nstderr:\n%s", code, run.stderr.String())
	}
	run.stdout.Reset()
	run.stderr.Reset()

	code := run.run(t, "path", "status", "--dir", dir)

	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", code, run.stderr.String())
	}
	for _, want := range []string{"shim:       " + filepath.Join(dir, "hs"), "installed:  yes", "matches:    yes"} {
		if !strings.Contains(run.stdout.String(), want) {
			t.Fatalf("stdout = %q, want it to contain %q", run.stdout.String(), want)
		}
	}
}

func TestPathGrammarRejectsWhatItDoesNotDispatch(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		want       string
		wantStatus int
	}{
		{name: "bare path", args: []string{"path"}, want: "usage: hs path", wantStatus: 2},
		{name: "an unknown subcommand", args: []string{"path", "bogus"}, want: `unknown path command "bogus"`, wantStatus: 2},
		{name: "status with force", args: []string{"path", "status", "--force"}, want: `unknown flag "--force"`, wantStatus: 2},
		{name: "status with an argument", args: []string{"path", "status", "extra"}, want: "status takes no arguments", wantStatus: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			run := newPathRun(t)

			code := run.run(t, tt.args...)

			if code != tt.wantStatus {
				t.Fatalf("exit = %d, want %d\nstdout:\n%s\nstderr:\n%s", code, tt.wantStatus, run.stdout.String(), run.stderr.String())
			}
			if !strings.Contains(run.stderr.String(), tt.want) {
				t.Fatalf("stderr = %q, want it to contain %q", run.stderr.String(), tt.want)
			}
			if run.stdout.Len() != 0 {
				t.Fatalf("stdout = %q, want empty", run.stdout.String())
			}
		})
	}
}

func TestPathHelpDescribesEverySubcommand(t *testing.T) {
	run := newPathRun(t)

	code := run.run(t, "path", "--help")

	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", code, run.stderr.String())
	}
	for _, want := range []string{"usage: hs path", "install", "remove", "status", "$HOME/.local/bin"} {
		if !strings.Contains(run.stdout.String(), want) {
			t.Fatalf("stdout = %q, want it to contain %q", run.stdout.String(), want)
		}
	}
}

func TestPathSubcommandHelpPrintsItsOwnUsage(t *testing.T) {
	tests := []struct {
		sub  string
		want []string
	}{
		{sub: "install", want: []string{"usage: hs path install", "--dir <path>", "--force", "runtime"}},
		{sub: "remove", want: []string{"usage: hs path remove", "--force", "backup"}},
		{sub: "status", want: []string{"usage: hs path status", "PATH", "plugin"}},
	}

	for _, tt := range tests {
		t.Run(tt.sub, func(t *testing.T) {
			run := newPathRun(t)

			code := run.run(t, "path", tt.sub, "--help")

			if code != 0 {
				t.Fatalf("exit = %d, want 0\nstderr:\n%s", code, run.stderr.String())
			}
			for _, want := range tt.want {
				if !strings.Contains(run.stdout.String(), want) {
					t.Fatalf("stdout = %q, want it to contain %q", run.stdout.String(), want)
				}
			}
		})
	}
}
