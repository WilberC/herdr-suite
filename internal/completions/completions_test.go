package completions

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func testCommands() []Command {
	return []Command{
		{Name: "attach", Summary: "Attach to a session", Names: SessionNames},
		{
			Name:    "session",
			Summary: "Manage herdr sessions",
			Children: []Child{
				{Name: "list", Summary: "List sessions as a stable table, or as JSON"},
				{Name: "stop", Summary: "Stop a session", Names: SessionNames},
			},
		},
		{
			Name:    "space",
			Summary: "Save and restore herdr spaces",
			Children: []Child{
				{Name: "save", Summary: "Snapshot the active workspace as a layout"},
				{Name: "load", Summary: "Recreate a stored layout in a workspace", Names: LayoutNames},
			},
		},
		{Name: "doctor", Summary: "Report the health of this installation"},
	}
}

func TestFishCompletesEveryCommandOfTheSpec(t *testing.T) {
	script := Fish(testCommands())

	for _, command := range testCommands() {
		if !strings.Contains(script, "-a '"+command.Name+"' -d '"+command.Summary+"'") {
			t.Fatalf("script does not offer %q with its summary:\n%s", command.Name, script)
		}
		if len(command.Children) == 0 {
			continue
		}
		offered := make([]string, 0, len(command.Children))
		for _, child := range command.Children {
			offered = append(offered, child.Name)
		}
		condition := "__fish_seen_subcommand_from " + command.Name +
			"; and not __fish_seen_subcommand_from " + strings.Join(offered, " ")
		for _, child := range command.Children {
			want := condition + "' -f -a '" + child.Name + "' -d '" + child.Summary + "'"
			if !strings.Contains(script, want) {
				t.Fatalf("script does not offer %q under %q:\n%s", child.Name, command.Name, script)
			}
		}
	}
}

func TestFishReadsNamesThroughTheCLI(t *testing.T) {
	script := Fish(testCommands())

	for _, want := range []string{
		"command hs session list --json 2>/dev/null",
		"command hs space list --json 2>/dev/null",
		"'(__hs_sessions)'",
		"'(__hs_layouts)'",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("script does not contain %q:\n%s", want, script)
		}
	}
	if !strings.Contains(script, "__fish_seen_subcommand_from space; and __fish_seen_subcommand_from load' -a '(__hs_layouts)'") {
		t.Fatalf("layout names are not offered for load:\n%s", script)
	}
	if strings.Contains(script, "__fish_seen_subcommand_from save load' -a '(__hs_layouts)'") {
		t.Fatalf("save gained a layout-name completion:\n%s", script)
	}
}

func TestFishDirFollowsXDGThenHome(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{
			name: "xdg first",
			env:  map[string]string{"XDG_CONFIG_HOME": "/xdg", "HOME": "/home/me"},
			want: "/xdg/fish/completions",
		},
		{
			name: "home without xdg",
			env:  map[string]string{"HOME": "/home/me"},
			want: "/home/me/.config/fish/completions",
		},
		{
			name: "neither",
			env:  map[string]string{},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := FishDir(func(name string) string { return tt.env[name] })
			if tt.want == "" {
				if err == nil {
					t.Fatalf("FishDir = %q, want an error when neither variable is set", got)
				}
				if !strings.Contains(err.Error(), "XDG_CONFIG_HOME") || !strings.Contains(err.Error(), "HOME") {
					t.Fatalf("FishDir error = %q, want it to name both variables", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("FishDir error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("FishDir = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestInstallCreatesTheDirectoryAndWritesTheScript(t *testing.T) {
	root := t.TempDir()
	getenv := func(name string) string {
		if name == "XDG_CONFIG_HOME" {
			return root
		}
		return ""
	}

	path, err := Install(testCommands(), getenv)

	if err != nil {
		t.Fatalf("Install error = %v", err)
	}
	if want := filepath.Join(root, "fish", "completions", "hs.fish"); path != want {
		t.Fatalf("Install path = %q, want %q", path, want)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the installed script: %v", err)
	}
	if got, want := string(data), Fish(testCommands()); got != want {
		t.Fatalf("installed script =\n%s\nwant:\n%s", got, want)
	}
}

func TestInstallRefusesWithoutAConfigHome(t *testing.T) {
	_, err := Install(testCommands(), func(string) string { return "" })

	if err == nil {
		t.Fatal("Install succeeded without XDG_CONFIG_HOME or HOME")
	}
}

func TestFishCompletesNamesAndKeepsFailuresQuiet(t *testing.T) {
	fish := fishBinary(t)
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name      string
		stub      string
		line      string
		want      string
		wantEmpty bool
	}{
		{
			name: "session names come from hs session list --json",
			stub: `case "$*" in
  "session list --json") printf '{\n  "sessions": [\n    {\n      "name": "work",\n      "running": true\n    }\n  ]\n}\n' ;;
  *) echo "unexpected $*" >&2; exit 1 ;;
esac`,
			line: "hs session stop ",
			want: "work",
		},
		{
			name: "layout names come from hs space list --json",
			stub: `case "$*" in
  "space list --json") printf '{\n  "layouts": [\n    {\n      "name": "demo",\n      "scope": "global"\n    }\n  ]\n}\n' ;;
  *) echo "unexpected $*" >&2; exit 1 ;;
esac`,
			line: "hs space load ",
			want: "demo",
		},
		{
			name: "a failing herdr never prints on the command line",
			stub: `echo "herdr is not installed" >&2
exit 1`,
			line:      "hs session stop ",
			wantEmpty: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := os.WriteFile(filepath.Join(bin, "hs"), []byte("#!/bin/sh\n"+tt.stub+"\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			stdout, stderr := runFish(t, fish, root, Fish(testCommands())+"\ncomplete -C '"+tt.line+"'")

			if strings.Contains(stderr, "herdr is not installed") {
				t.Fatalf("the stub's stderr reached the shell: %q", stderr)
			}
			if stderr != "" {
				t.Fatalf("stderr = %q, want silence", stderr)
			}
			if tt.wantEmpty {
				if strings.TrimSpace(stdout) != "" {
					t.Fatalf("completions = %q, want none when herdr fails", stdout)
				}
				return
			}
			if !strings.Contains(stdout, tt.want) {
				t.Fatalf("completions = %q, want it to contain %q", stdout, tt.want)
			}
		})
	}
}

// fishBinary is the fish that runs the generated script. The suite stays offline
// and deterministic: a machine without fish skips these behaviour tests instead
// of failing them.
func fishBinary(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("fish")
	if err != nil {
		t.Skip("fish is not installed; the completion behaviour is checked structurally")
	}
	return path
}

// runFish runs one fish snippet with a scratch HOME, so the user's own fish
// configuration and completion directory are never read or written.
func runFish(t *testing.T, fish, root, snippet string) (string, string) {
	t.Helper()
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(fish, "--no-config", "-c", snippet)
	cmd.Env = []string{
		"PATH=" + filepath.Join(root, "bin") + ":/usr/bin:/bin",
		"HOME=" + home,
		"XDG_CONFIG_HOME=" + filepath.Join(home, ".config"),
		"XDG_DATA_HOME=" + filepath.Join(home, ".local", "share"),
	}
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		t.Fatalf("fish failed: %v\nstderr:\n%s\nstdout:\n%s", err, stderr.String(), stdout.String())
	}
	return stdout.String(), stderr.String()
}
