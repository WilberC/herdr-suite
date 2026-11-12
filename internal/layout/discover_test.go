package layout

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func projectHome(t *testing.T) (root, cwd string) {
	t.Helper()
	root = t.TempDir()
	cwd = filepath.Join(root, "src", "deep")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatalf("create the project tree: %v", err)
	}
	return root, cwd
}

func writeLayoutFile(t *testing.T, path string, document Layout) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create %s: %v", filepath.Dir(path), err)
	}
	data, err := Marshal(document)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func writeIndex(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "layouts.toml"), []byte(body), 0o644); err != nil {
		t.Fatalf("write the project index: %v", err)
	}
}

func configEnv(configHome string) func(string) string {
	return env(map[string]string{"XDG_CONFIG_HOME": configHome, "HOME": "/home/me"})
}

func TestDiscoverFindsTheNearestProjectDeclaration(t *testing.T) {
	root, cwd := projectHome(t)
	document := wantLayout()
	document.Name = "api"
	writeLayoutFile(t, filepath.Join(root, ".herdr", "layouts", "api.json"), document)

	sources, err := Discover(configEnv("/config"), cwd)
	if err != nil {
		t.Fatalf("Discover error = %v, want nil", err)
	}

	herdr := filepath.Join(root, ".herdr")
	want := Sources{
		Global:  filepath.Join("/config", "herdr", "herdr-suite", "layouts"),
		Project: filepath.Join(herdr, "layouts"),
		Root:    root,
		Index:   "",
	}
	if sources != want {
		t.Fatalf("Discover = %#v, want %#v", sources, want)
	}
}

func TestDiscoverStopsAtTheDeclarationNearestToTheWorkingDirectory(t *testing.T) {
	root, cwd := projectHome(t)
	writeLayoutFile(t, filepath.Join(root, ".herdr", "layouts", "outer.json"), wantLayout())
	writeIndex(t, filepath.Join(root, "src", ".herdr"), "[layouts]\ninner = \"inner.json\"\n")

	sources, err := Discover(configEnv("/config"), cwd)
	if err != nil {
		t.Fatalf("Discover error = %v, want nil", err)
	}

	herdr := filepath.Join(root, "src", ".herdr")
	if sources.Root != filepath.Join(root, "src") {
		t.Fatalf("project root = %q, want %q", sources.Root, filepath.Join(root, "src"))
	}
	if want := filepath.Join(herdr, "layouts"); sources.Project != want {
		t.Fatalf("project layouts = %q, want %q", sources.Project, want)
	}
	if want := filepath.Join(herdr, "layouts.toml"); sources.Index != want {
		t.Fatalf("project index = %q, want %q", sources.Index, want)
	}
}

func TestDiscoverReadsAProjectIndexWithoutALayoutDirectory(t *testing.T) {
	root, cwd := projectHome(t)
	writeIndex(t, filepath.Join(root, ".herdr"), "[layouts]\napi = \"api.json\"\n")

	sources, err := Discover(configEnv("/config"), cwd)
	if err != nil {
		t.Fatalf("Discover error = %v, want nil", err)
	}

	if sources.Root != root {
		t.Fatalf("project root = %q, want %q", sources.Root, root)
	}
	if sources.Index == "" {
		t.Fatalf("project index = %q, want the declared index", sources.Index)
	}
	if want := filepath.Join(root, ".herdr", "layouts"); sources.Project != want {
		t.Fatalf("project layouts = %q, want %q", sources.Project, want)
	}
}

func TestDiscoverOutsideAProjectKeepsOnlyTheGlobalStore(t *testing.T) {
	sources, err := Discover(configEnv("/config"), t.TempDir())
	if err != nil {
		t.Fatalf("Discover error = %v, want nil", err)
	}

	want := Sources{Global: filepath.Join("/config", "herdr", "herdr-suite", "layouts")}
	if sources != want {
		t.Fatalf("Discover = %#v, want %#v", sources, want)
	}
}

func TestDiscoverReportsWhenNoConfigHomeIsKnown(t *testing.T) {
	_, err := Discover(env(nil), "")

	if err == nil || !strings.Contains(err.Error(), "XDG_CONFIG_HOME") {
		t.Fatalf("Discover error = %v, want it to name XDG_CONFIG_HOME", err)
	}
}

func TestProjectIndexReadsItsDeclaredLayouts(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "layouts.toml")
	body := strings.Join([]string{
		"# Layouts this project commits.",
		"",
		"[layouts]",
		`api = "api.json" # the api workspace`,
		`web = "layouts/web.json"`,
		"",
	}, "\n")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write the index: %v", err)
	}

	entries, err := readIndex(path)
	if err != nil {
		t.Fatalf("readIndex error = %v, want nil", err)
	}

	want := []indexEntry{
		{Name: "api", Path: filepath.Join(dir, "api.json")},
		{Name: "web", Path: filepath.Join(dir, "layouts", "web.json")},
	}
	if len(entries) != len(want) {
		t.Fatalf("index = %#v, want %#v", entries, want)
	}
	for index, entry := range entries {
		if entry != want[index] {
			t.Fatalf("entry %d = %#v, want %#v", index, entry, want[index])
		}
	}
}

func TestProjectIndexRejectsWhatItsFormatDoesNotAllow(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "an entry outside the layouts table",
			body: "api = \"api.json\"\n",
			want: `layouts.toml:1: expected the [layouts] table before any entry`,
		},
		{
			name: "a line that is not an assignment",
			body: "[layouts]\napi\n",
			want: `layouts.toml:2: expected 'name = "path"'`,
		},
		{
			name: "an unquoted path",
			body: "[layouts]\napi = api.json\n",
			want: "layouts.toml:2: 'api' needs a double-quoted path",
		},
		{
			name: "an empty path",
			body: "[layouts]\napi = \"\"\n",
			want: "layouts.toml:2: 'api' needs a double-quoted path",
		},
		{
			name: "an invalid layout name",
			body: "[layouts]\n\"bad name\" = \"api.json\"\n",
			want: "layouts.toml:2: expected 'name = \"path\"'",
		},
		{
			name: "a name declared twice",
			body: "[layouts]\napi = \"a.json\"\napi = \"b.json\"\n",
			want: "layouts.toml:3: 'api' is already declared",
		},
		{
			name: "a path outside the project",
			body: "[layouts]\napi = \"../outside.json\"\n",
			want: "layouts.toml:2: 'api' must name a file inside",
		},
		{
			name: "a second table",
			body: "[layouts]\n[other]\n",
			want: "layouts.toml:2: unknown table '[other]'; the index holds one [layouts] table",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "layouts.toml")
			if err := os.WriteFile(path, []byte(tt.body), 0o644); err != nil {
				t.Fatalf("write the index: %v", err)
			}

			_, err := readIndex(path)

			if err == nil {
				t.Fatalf("readIndex error = nil, want a refusal")
			}
			if got := err.Error(); !strings.Contains(got, tt.want) {
				t.Fatalf("readIndex error = %q, want it to contain %q", got, tt.want)
			}
		})
	}
}
