package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompletionsPrintsTheFishScript(t *testing.T) {
	run := newSpaceRun(t)

	code := run.run(t, "completions", "fish")

	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", code, run.stderr)
	}
	for _, want := range []string{
		"complete -c hs",
		"command hs session list --json 2>/dev/null",
		"command hs space list --json 2>/dev/null",
	} {
		if !strings.Contains(run.stdout.String(), want) {
			t.Fatalf("stdout = %q, want it to contain %q", run.stdout.String(), want)
		}
	}
	if run.stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", run.stderr.String())
	}
	if calls := run.client.Calls(); len(calls) != 0 {
		t.Fatalf("printing the script issued calls: %v", calls)
	}
}

// TestCompletionsCoversEverySubcommandInTheTable is the drift guard: the script
// is generated from the command table, so a command the CLI dispatches cannot
// silently lose its completion.
func TestCompletionsCoversEverySubcommandInTheTable(t *testing.T) {
	run := newSpaceRun(t)

	if code := run.run(t, "completions", "fish"); code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", code, run.stderr)
	}
	script := run.stdout.String()

	for _, cmd := range commands {
		if !strings.Contains(script, "-a '"+cmd.name+"' -d '"+cmd.summary+"'") {
			t.Errorf("script does not complete the command %q", cmd.name)
		}
		for _, sub := range cmd.subcommands {
			if !strings.Contains(script, "-a '"+sub.name+"' -d '"+sub.summary+"'") {
				t.Errorf("script does not complete %q under %q", sub.name, cmd.name)
			}
		}
	}
}

// TestCompletionSubcommandsAreDispatched is the other direction of the drift
// guard: every subcommand the script advertises must reach its own parser
// instead of the group's unknown-subcommand refusal.
func TestCompletionSubcommandsAreDispatched(t *testing.T) {
	for _, cmd := range commands {
		for _, sub := range cmd.subcommands {
			t.Run(cmd.name+" "+sub.name, func(t *testing.T) {
				run := newSpaceRun(t)

				code := run.run(t, cmd.name, sub.name, "--__drift")

				if code != 2 {
					t.Fatalf("exit = %d, want 2\nstdout:\n%s\nstderr:\n%s", code, run.stdout, run.stderr)
				}
				if strings.Contains(run.stderr.String(), "unknown "+cmd.name+" command") {
					t.Fatalf("subcommand %q is advertised but not dispatched:\n%s", sub.name, run.stderr)
				}
				if !strings.Contains(run.stderr.String(), "hs: ") || run.stderr.Len() == 0 {
					t.Fatalf("stderr = %q, want the subcommand's own refusal", run.stderr.String())
				}
			})
		}
	}
}

func TestCompletionsInstallsIntoTheFishCompletionDirectory(t *testing.T) {
	run := newSpaceRun(t)

	code := run.run(t, "completions", "fish", "--install")

	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", code, run.stderr)
	}
	path := filepath.Join(run.configDir, "fish", "completions", "hs.fish")
	if want := "installed: " + path + "\n"; run.stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", run.stdout.String(), want)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the installed script: %v", err)
	}
	if !strings.Contains(string(data), "complete -c hs") {
		t.Fatalf("installed script = %q, want the fish completions", data)
	}
}

func TestCompletionsInstallsUnderHomeWithoutXDG(t *testing.T) {
	run := newSpaceRun(t)
	home := t.TempDir()
	run.d.getenv = func(name string) string {
		if name == "HOME" {
			return home
		}
		return ""
	}

	code := run.run(t, "completions", "fish", "--install")

	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", code, run.stderr)
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "fish", "completions", "hs.fish")); err != nil {
		t.Fatalf("the script was not installed under HOME: %v", err)
	}
}

func TestCompletionsInstallRefusesWithoutAConfigHome(t *testing.T) {
	run := newSpaceRun(t)
	run.d.getenv = func(string) string { return "" }

	code := run.run(t, "completions", "fish", "--install")

	if code != 2 {
		t.Fatalf("exit = %d, want 2\nstdout:\n%s\nstderr:\n%s", code, run.stdout, run.stderr)
	}
	for _, want := range []string{"hs: ", "XDG_CONFIG_HOME", "HOME", "export"} {
		if !strings.Contains(run.stderr.String(), want) {
			t.Fatalf("stderr = %q, want it to contain %q", run.stderr.String(), want)
		}
	}
	if run.stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", run.stdout.String())
	}
}

func TestCompletionsRejectsBadArguments(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "no shell",
			args: []string{"completions"},
			want: "expected one shell name",
		},
		{
			name: "an unsupported shell",
			args: []string{"completions", "bash"},
			want: `only "fish" is supported`,
		},
		{
			name: "an unknown flag",
			args: []string{"completions", "fish", "--bogus"},
			want: `unknown flag "--bogus"`,
		},
		{
			name: "two shells",
			args: []string{"completions", "fish", "zsh"},
			want: "expected one shell name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			run := newSpaceRun(t)

			code := run.run(t, tt.args...)

			if code != 2 {
				t.Fatalf("exit = %d, want 2\nstdout:\n%s\nstderr:\n%s", code, run.stdout, run.stderr)
			}
			if !strings.Contains(run.stderr.String(), tt.want) {
				t.Fatalf("stderr = %q, want it to contain %q", run.stderr.String(), tt.want)
			}
			if !strings.Contains(run.stderr.String(), "usage: hs completions") {
				t.Fatalf("stderr = %q, want the completions usage", run.stderr.String())
			}
			if run.stdout.Len() != 0 {
				t.Fatalf("stdout = %q, want empty", run.stdout.String())
			}
		})
	}
}

func TestCompletionsHelpDescribesTheInstall(t *testing.T) {
	run := newSpaceRun(t)

	code := run.run(t, "completions", "--help")

	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", code, run.stderr)
	}
	for _, want := range []string{"usage: hs completions", "fish", "--install", "$XDG_CONFIG_HOME/fish/completions"} {
		if !strings.Contains(run.stdout.String(), want) {
			t.Fatalf("stdout = %q, want it to contain %q", run.stdout.String(), want)
		}
	}
}
