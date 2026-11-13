package pathshim

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// shimGolden is the frozen shim. It is the file a user keeps in their bin
// directory, so a change here is a change to their shell: the test fails until
// the new content is written down on purpose.
const shimGolden = `#!/bin/sh
# hs - herdr-suite shim written by "hs path install" (remove: hs path remove).
# The plugin root is resolved at run time, so a plugin update never breaks this.
root=$(herdr plugin list --plugin herdr-suite --json 2>/dev/null \
  | grep -o '"plugin_root"[[:space:]]*:[[:space:]]*"[^"]*"' \
  | head -n 1 | sed 's/.*"\([^"]*\)"$/\1/')
if [ -z "$root" ]; then
  echo 'hs: the herdr-suite plugin is not installed; run: herdr plugin install WilberC/herdr-suite' >&2
  exit 1
fi
if [ ! -x "$root/bin/hs" ]; then
  echo "hs: $root/bin/hs is missing or not executable; run: herdr plugin update herdr-suite" >&2
  exit 1
fi
exec "$root/bin/hs" "$@"
`

func TestScriptIsTheFrozenShim(t *testing.T) {
	if Script != shimGolden {
		t.Fatalf("Script changed:\n--- got ---\n%s\n--- want ---\n%s", Script, shimGolden)
	}
	if !strings.HasSuffix(Script, "\n") {
		t.Fatalf("Script does not end with a newline:\n%q", Script)
	}
	if lines := strings.Count(Script, "\n"); lines > 15 {
		t.Fatalf("Script is %d lines, want 15 or fewer so it stays readable", lines)
	}
}

// TestScriptCarriesNoMachineSpecificPath pins the reason the shim survives a
// plugin update: it resolves the root when it runs, so no checkout path may be
// baked into it.
func TestScriptCarriesNoMachineSpecificPath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}
	cwd, err := os.Getwd()
	if err != nil {
		cwd = ""
	}
	for _, path := range []string{home, cwd, "/home/", "/Users/", "/opt/", "/usr/local/"} {
		if path == "" {
			continue
		}
		if strings.Contains(Script, path) {
			t.Fatalf("Script contains %q:\n%s", path, Script)
		}
	}
	if strings.Contains(Script, "python") || strings.Contains(Script, "jq") {
		t.Fatalf("Script depends on a tool beyond the shell:\n%s", Script)
	}
}

// TestParsePluginRoot reads the field the shim reads, from the shapes herdr
// prints today and the shapes a future pretty-printer would print.
func TestParsePluginRoot(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   string
	}{
		{
			name:   "herdr's minified plugin list",
			output: `{"id":"cli:plugin","result":{"plugins":[{"description":"x","plugin_id":"herdr-suite","plugin_root":"/tmp/checkout","version":"0.2.0"}],"type":"plugin_list"}}`,
			want:   "/tmp/checkout",
		},
		{
			name:   "the field printed with spaces",
			output: "{\n  \"plugins\": [\n    {\n      \"plugin_root\": \"/tmp/spaced\"\n    }\n  ]\n}\n",
			want:   "/tmp/spaced",
		},
		{
			name:   "several plugins, ours not first",
			output: `{"plugins":[{"plugin_root":"/tmp/other"},{"plugin_root":"/tmp/ours"}]}`,
			want:   "/tmp/other",
		},
		{
			name:   "no plugin registered",
			output: `{"id":"cli:plugin","result":{"plugins":[],"type":"plugin_list"}}`,
			want:   "",
		},
		{
			name:   "a null field",
			output: `{"plugins":[{"plugin_root":null}]}`,
			want:   "",
		},
		{
			name:   "empty output",
			output: "",
			want:   "",
		},
		{
			name:   "not json at all",
			output: "herdr: not running\n",
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ParsePluginRoot([]byte(tt.output)); got != tt.want {
				t.Fatalf("ParsePluginRoot = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolvePluginReadsTheRootAndChecksTheBinary(t *testing.T) {
	root := pluginCheckout(t, 0o755)

	plugin, err := Resolve(func() ([]byte, error) {
		return []byte(`{"plugins":[{"plugin_root":"` + root + `"}]}`), nil
	})

	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if plugin.Root != root {
		t.Fatalf("Root = %q, want %q", plugin.Root, root)
	}
	if want := filepath.Join(root, "bin", "hs"); plugin.Binary != want {
		t.Fatalf("Binary = %q, want %q", plugin.Binary, want)
	}
}

func TestResolvePluginRefusesWhenThePluginIsNotInstalled(t *testing.T) {
	_, err := Resolve(func() ([]byte, error) {
		return []byte(`{"plugins":[]}`), nil
	})

	var refusal Refusal
	if !asRefusal(err, &refusal) {
		t.Fatalf("err = %v, want a Refusal", err)
	}
	for _, want := range []string{"herdr-suite plugin is not installed", "herdr plugin install WilberC/herdr-suite"} {
		if !strings.Contains(refusal.Reason, want) {
			t.Fatalf("reason = %q, want it to contain %q", refusal.Reason, want)
		}
	}
}

func TestResolvePluginRefusesAMissingOrNonExecutableBinary(t *testing.T) {
	tests := []struct {
		name string
		mode os.FileMode
	}{
		{name: "missing", mode: 0},
		{name: "not executable", mode: 0o644},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			if tt.mode != 0 {
				writeBinary(t, root, tt.mode)
			}

			_, err := Resolve(func() ([]byte, error) {
				return []byte(`{"plugins":[{"plugin_root":"` + root + `"}]}`), nil
			})

			var refusal Refusal
			if !asRefusal(err, &refusal) {
				t.Fatalf("err = %v, want a Refusal", err)
			}
			for _, want := range []string{filepath.Join(root, "bin", "hs"), "herdr plugin update herdr-suite"} {
				if !strings.Contains(refusal.Reason, want) {
					t.Fatalf("reason = %q, want it to contain %q", refusal.Reason, want)
				}
			}
		})
	}
}

// TestResolvePluginSurfacesAHerdrFailure keeps a broken herdr distinct from an
// uninstalled plugin: the first is an error, the second is a refusal.
func TestResolvePluginSurfacesAHerdrFailure(t *testing.T) {
	_, err := Resolve(func() ([]byte, error) {
		return nil, os.ErrPermission
	})

	if err == nil {
		t.Fatal("Resolve accepted a failing herdr run")
	}
	var refusal Refusal
	if asRefusal(err, &refusal) {
		t.Fatalf("err = %v, want a plain error instead of a Refusal", err)
	}
	if !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("err = %v, want it to name the herdr failure", err)
	}
}

func TestDirPrefersTheOverrideAndFallsBackToHome(t *testing.T) {
	env := map[string]string{"HOME": "/tmp/home"}

	if got, want := mustDir(t, env, "/tmp/custom/bin"), "/tmp/custom/bin"; got != want {
		t.Fatalf("Dir = %q, want %q", got, want)
	}
	if got, want := mustDir(t, env, ""), "/tmp/home/.local/bin"; got != want {
		t.Fatalf("Dir = %q, want %q", got, want)
	}
}

func TestDirRefusesAnUnresolvableHome(t *testing.T) {
	tests := []struct {
		name string
		home string
		dir  string
		want string
	}{
		{name: "no HOME", home: "", dir: "", want: "HOME is not set"},
		{name: "relative HOME", home: "home", dir: "", want: `HOME = "home" is not an absolute path`},
		{name: "relative override", home: "/tmp/home", dir: "bin", want: `--dir "bin" is not an absolute path`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Dir(func(name string) string { return map[string]string{"HOME": tt.home}[name] }, tt.dir)

			var refusal Refusal
			if !asRefusal(err, &refusal) {
				t.Fatalf("err = %v, want a Refusal", err)
			}
			if !strings.Contains(refusal.Reason, tt.want) {
				t.Fatalf("reason = %q, want it to contain %q", refusal.Reason, tt.want)
			}
		})
	}
}

func TestOnPathComparesWholeEntries(t *testing.T) {
	tests := []struct {
		name    string
		dir     string
		pathEnv string
		want    bool
	}{
		{name: "listed first", dir: "/home/me/.local/bin", pathEnv: "/home/me/.local/bin:/usr/bin", want: true},
		{name: "listed with a trailing slash", dir: "/home/me/.local/bin", pathEnv: "/home/me/.local/bin/:/usr/bin", want: true},
		{name: "listed last", dir: "/home/me/.local/bin", pathEnv: "/usr/bin:/home/me/.local/bin", want: true},
		{name: "a parent is not a match", dir: "/home/me/.local/bin", pathEnv: "/home/me:/usr/bin", want: false},
		{name: "empty PATH", dir: "/home/me/.local/bin", pathEnv: "", want: false},
		{name: "an empty entry is skipped", dir: "/home/me/.local/bin", pathEnv: "::/usr/bin", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := OnPath(tt.dir, tt.pathEnv); got != tt.want {
				t.Fatalf("OnPath = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestShimResolvesWhatTheGoResolverResolves is the shared-logic pin: the shell
// pipeline in the shim and ParsePluginRoot must read the same field from the
// same output, so "hs path status" cannot disagree with the shim a shell runs.
func TestShimResolvesWhatTheGoResolverResolves(t *testing.T) {
	shell := shellPath(t)
	if !resolvable("grep") || !resolvable("sed") {
		t.Skip("running the shim needs grep and sed")
	}

	root := pluginCheckout(t, 0o755)
	// The fake binary prints where it lives, so the shim's own resolution is the
	// only thing the assertion reads.
	if err := os.WriteFile(filepath.Join(root, "bin", "hs"), []byte("#!/bin/sh\nprintf '%s\\n' \"$0\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	outputs := []string{
		`{"id":"cli:plugin","result":{"plugins":[{"description":"x","plugin_id":"herdr-suite","plugin_root":"` + root + `","version":"0.2.0"}],"type":"plugin_list"}}`,
		"{\n  \"plugins\": [\n    {\n      \"plugin_root\": \"" + root + "\"\n    }\n  ]\n}\n",
	}

	for _, output := range outputs {
		binDir := t.TempDir()
		writeFakeHerdr(t, binDir, output)
		script := writeShim(t, t.TempDir())

		cmd := exec.Command(shell, script)
		cmd.Env = []string{"PATH=" + binDir + string(os.PathListSeparator) + os.Getenv("PATH")}
		stdout, err := cmd.Output()
		if err != nil {
			t.Fatalf("run the shim: %v\n%s", err, stdout)
		}
		executed := strings.TrimSpace(string(stdout))
		if want := filepath.Join(root, "bin", "hs"); executed != want {
			t.Fatalf("shim exec'd %q, want %q", executed, want)
		}
		if got := ParsePluginRoot([]byte(output)); got != root {
			t.Fatalf("ParsePluginRoot = %q, want the root the shim used (%q)", got, root)
		}
	}
}

func TestShimFailsWhenThePluginIsNotInstalled(t *testing.T) {
	shell := shellPath(t)

	binDir := t.TempDir()
	writeFakeHerdr(t, binDir, `{"id":"cli:plugin","result":{"plugins":[],"type":"plugin_list"}}`)
	script := writeShim(t, t.TempDir())

	cmd := exec.Command(shell, script)
	cmd.Env = []string{"PATH=" + binDir + string(os.PathListSeparator) + os.Getenv("PATH")}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	err := cmd.Run()

	if err == nil {
		t.Fatal("the shim succeeded without a plugin")
	}
	for _, want := range []string{"the herdr-suite plugin is not installed", "herdr plugin install WilberC/herdr-suite"} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr = %q, want it to contain %q", stderr.String(), want)
		}
	}
}

func TestShimFailsWhenThePluginBinaryIsMissing(t *testing.T) {
	shell := shellPath(t)

	root := t.TempDir()
	binDir := t.TempDir()
	writeFakeHerdr(t, binDir, `{"plugins":[{"plugin_root":"`+root+`"}]}`)
	script := writeShim(t, t.TempDir())

	cmd := exec.Command(shell, script)
	cmd.Env = []string{"PATH=" + binDir + string(os.PathListSeparator) + os.Getenv("PATH")}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	err := cmd.Run()

	if err == nil {
		t.Fatal("the shim succeeded without a binary")
	}
	for _, want := range []string{filepath.Join(root, "bin", "hs"), "herdr plugin update herdr-suite"} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr = %q, want it to contain %q", stderr.String(), want)
		}
	}
}

// TestShimPassesItsArgumentsThrough proves the shim is transparent: the plugin
// binary sees the same argv a user typed.
func TestShimPassesItsArgumentsThrough(t *testing.T) {
	shell := shellPath(t)

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "#!/bin/sh\nprintf 'arg:%s\\n' \"$@\"\n"
	if err := os.WriteFile(filepath.Join(root, "bin", "hs"), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	binDir := t.TempDir()
	writeFakeHerdr(t, binDir, `{"plugins":[{"plugin_root":"`+root+`"}]}`)
	script := writeShim(t, t.TempDir())

	cmd := exec.Command(shell, script, "space", "load", "work")
	cmd.Env = []string{"PATH=" + binDir + string(os.PathListSeparator) + os.Getenv("PATH")}
	stdout, err := cmd.Output()
	if err != nil {
		t.Fatalf("run the shim: %v", err)
	}
	if got, want := string(stdout), "arg:space\narg:load\narg:work\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}

func mustDir(t *testing.T, env map[string]string, override string) string {
	t.Helper()
	dir, err := Dir(func(name string) string { return env[name] }, override)
	if err != nil {
		t.Fatalf("Dir: %v", err)
	}
	return dir
}

// shellPath is the shell the shim runs under; a machine without one skips the
// tests that execute it.
func shellPath(t *testing.T) string {
	t.Helper()
	shell, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("running the shim needs sh")
	}
	return shell
}

func resolvable(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// asRefusal is errors.As without importing it in every case of the tables above.
func asRefusal(err error, target *Refusal) bool {
	refusal, ok := err.(Refusal)
	if !ok {
		return false
	}
	*target = refusal
	return true
}

// pluginCheckout is a plugin root with bin/hs at mode.
func pluginCheckout(t *testing.T, mode os.FileMode) string {
	t.Helper()
	root := t.TempDir()
	writeBinary(t, root, mode)
	return root
}

func writeBinary(t *testing.T, root string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "bin", "hs"), []byte("#!/bin/sh\nexit 0\n"), mode); err != nil {
		t.Fatal(err)
	}
}

// writeFakeHerdr puts a herdr on a PATH that prints output, so the shim's own
// parsing runs without a herdr installation.
func writeFakeHerdr(t *testing.T, dir, output string) {
	t.Helper()
	body := "#!/bin/sh\ncat <<'HERDR_OUTPUT'\n" + output + "\nHERDR_OUTPUT\n"
	if err := os.WriteFile(filepath.Join(dir, "herdr"), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

// writeShim writes the frozen script to a temp dir and returns its path.
func writeShim(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "hs")
	if err := os.WriteFile(path, []byte(Script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}
