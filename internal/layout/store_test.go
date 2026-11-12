package layout

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func env(values map[string]string) func(string) string {
	return func(name string) string { return values[name] }
}

func TestDirFollowsXDGConfigHome(t *testing.T) {
	dir, err := Dir(env(map[string]string{
		"XDG_CONFIG_HOME": "/home/me/.config-custom",
		"HOME":            "/home/me",
	}))
	if err != nil {
		t.Fatalf("Dir error = %v, want nil", err)
	}

	if want := filepath.Join("/home/me/.config-custom", "herdr", "herdr-suite", "layouts"); dir != want {
		t.Fatalf("Dir = %q, want %q", dir, want)
	}
}

func TestDirFallsBackToTheHomeConfigDirectory(t *testing.T) {
	dir, err := Dir(env(map[string]string{"HOME": "/home/me"}))
	if err != nil {
		t.Fatalf("Dir error = %v, want nil", err)
	}

	if want := filepath.Join("/home/me", ".config", "herdr", "herdr-suite", "layouts"); dir != want {
		t.Fatalf("Dir = %q, want %q", dir, want)
	}
}

func TestDirReportsWhenNoConfigHomeIsKnown(t *testing.T) {
	_, err := Dir(env(nil))

	if err == nil || !strings.Contains(err.Error(), "XDG_CONFIG_HOME") {
		t.Fatalf("Dir error = %v, want it to name XDG_CONFIG_HOME", err)
	}
}

func TestSaveStoresTheLayoutUnderItsName(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "layouts")
	document := wantLayout()
	document.Name = "my-space"

	path, err := Save(dir, document, false)
	if err != nil {
		t.Fatalf("Save error = %v, want nil", err)
	}

	if want := filepath.Join(dir, "my-space.json"); path != want {
		t.Fatalf("Save path = %q, want %q", path, want)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the stored layout: %v", err)
	}
	got, err := Unmarshal(data)
	if err != nil {
		t.Fatalf("Unmarshal stored layout: %v", err)
	}
	if !reflect.DeepEqual(got, document) {
		t.Fatalf("stored layout = %#v, want %#v", got, document)
	}
}

func TestSaveRefusesToReplaceAStoredLayout(t *testing.T) {
	dir := t.TempDir()
	document := wantLayout()
	path, err := Save(dir, document, false)
	if err != nil {
		t.Fatalf("first Save error = %v, want nil", err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the stored layout: %v", err)
	}

	document.Workspace.Label = "replaced"
	_, err = Save(dir, document, false)

	var exists ExistsError
	if !errors.As(err, &exists) {
		t.Fatalf("Save error = %v, want an ExistsError", err)
	}
	if exists.Path != path {
		t.Fatalf("ExistsError path = %q, want %q", exists.Path, path)
	}
	if !strings.Contains(err.Error(), "pass --force to overwrite") {
		t.Fatalf("Save error = %q, want it to mention --force", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the stored layout: %v", err)
	}
	if string(after) != string(before) {
		t.Fatalf("a refused save rewrote the stored layout")
	}
}

func TestSaveReplacesAStoredLayoutWithForce(t *testing.T) {
	dir := t.TempDir()
	document := wantLayout()
	path, err := Save(dir, document, false)
	if err != nil {
		t.Fatalf("first Save error = %v, want nil", err)
	}

	document.Workspace.Label = "replaced"
	if _, err := Save(dir, document, true); err != nil {
		t.Fatalf("Save with force error = %v, want nil", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the stored layout: %v", err)
	}
	got, err := Unmarshal(data)
	if err != nil {
		t.Fatalf("Unmarshal stored layout: %v", err)
	}
	if got.Workspace.Label != "replaced" {
		t.Fatalf("stored label = %q, want %q", got.Workspace.Label, "replaced")
	}
}

func TestSaveRejectsANameThatEscapesTheDirectory(t *testing.T) {
	for _, name := range []string{"../evil", "nested/name", "", ".", "..", "with space"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			document := wantLayout()
			document.Name = name

			if _, err := Save(dir, document, false); err == nil {
				t.Fatalf("Save(%q) error = nil, want a refusal", name)
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatalf("read dir: %v", err)
			}
			if len(entries) != 0 {
				t.Fatalf("Save(%q) wrote %d entries, want none", name, len(entries))
			}
		})
	}
}
