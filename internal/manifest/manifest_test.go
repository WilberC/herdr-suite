package manifest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// shippedManifest is a manifest shaped like herdr-plugin.toml: top-level string
// keys, a multi-line array, and tables that repeat key names the reader must
// ignore.
const shippedManifest = `id = "herdr-suite"
name = "Herd Suite"
version = "0.2.0"
min_herdr_version = "0.7.5"   # the first release with these actions
description = "Session and space management for herdr"
platforms = ["linux", "macos"]

build = [
  { command = ["bash", "bin/ensure-binary.sh", "--in-tree"] }
]

[[startup]]
command = ["bin/hs", "gs-restore", "startup"]

[[actions]]
id = "save"
version = "9.9.9"
min_herdr_version = "0.0.1"
`

func TestParseReadsTheFieldsTheBinaryReports(t *testing.T) {
	document, err := Parse([]byte(shippedManifest))
	if err != nil {
		t.Fatalf("Parse error = %v, want nil", err)
	}

	if document.ID != "herdr-suite" {
		t.Errorf("id = %q, want %q", document.ID, "herdr-suite")
	}
	if document.Name != "Herd Suite" {
		t.Errorf("name = %q, want %q", document.Name, "Herd Suite")
	}
	if document.Version != "0.2.0" {
		t.Errorf("version = %q, want %q", document.Version, "0.2.0")
	}
	if document.MinHerdrVersion != "0.7.5" {
		t.Errorf("min_herdr_version = %q, want %q", document.MinHerdrVersion, "0.7.5")
	}
}

func TestParseRefusesAManifestWithoutAFieldItReports(t *testing.T) {
	for _, key := range []string{"id", "version", "min_herdr_version"} {
		t.Run(key, func(t *testing.T) {
			var kept []string
			for _, line := range strings.Split(shippedManifest, "\n") {
				if strings.HasPrefix(line, key+" ") {
					continue
				}
				kept = append(kept, line)
			}

			_, err := Parse([]byte(strings.Join(kept, "\n")))
			if err == nil {
				t.Fatalf("Parse error = nil, want a refusal naming %s", key)
			}
			if !strings.Contains(err.Error(), key) {
				t.Fatalf("Parse error = %q, want it to name %s", err, key)
			}
		})
	}
}

func TestParseRefusesANonStringValue(t *testing.T) {
	body := strings.Replace(shippedManifest, `version = "0.2.0"`, "version = 2", 1)

	_, err := Parse([]byte(body))
	if err == nil {
		t.Fatal("Parse error = nil, want a refusal of an unquoted version")
	}
	if !strings.Contains(err.Error(), "version") {
		t.Fatalf("Parse error = %q, want it to name version", err)
	}
}

func TestParseRefusesADuplicateKey(t *testing.T) {
	body := "version = \"0.3.0\"\n" + shippedManifest

	_, err := Parse([]byte(body))
	if err == nil {
		t.Fatal("Parse error = nil, want a refusal of the duplicate version")
	}
	if !strings.Contains(err.Error(), "declared twice") {
		t.Fatalf("Parse error = %q, want it to report the duplicate", err)
	}
}

func TestReadReportsTheFileItCouldNotRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)

	_, err := Read(path)
	if err == nil {
		t.Fatal("Read error = nil, want the missing file reported")
	}
	if !strings.Contains(err.Error(), path) {
		t.Fatalf("Read error = %q, want it to name %s", err, path)
	}
}

// TestLocateFindsTheManifestThePluginShips pins the resolution order: an
// explicit path, then beside the binary (bin/hs inside the plugin root), then
// the working directory.
func TestLocateFindsTheManifestThePluginShips(t *testing.T) {
	root := t.TempDir()
	plugin := filepath.Join(root, "plugin")
	bin := filepath.Join(plugin, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	alongside := filepath.Join(bin, FileName)
	if err := os.WriteFile(alongside, []byte(shippedManifest), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := Locate("", filepath.Join(bin, "hs"), filepath.Join(root, "work"))
	if err != nil {
		t.Fatalf("Locate error = %v, want the manifest beside the binary", err)
	}
	if got != alongside {
		t.Fatalf("Locate = %q, want %q", got, alongside)
	}
}

func TestLocateFindsTheManifestInThePluginRootAboveTheBinary(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	shipped := filepath.Join(root, FileName)
	if err := os.WriteFile(shipped, []byte(shippedManifest), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := Locate("", filepath.Join(bin, "hs"), t.TempDir())
	if err != nil {
		t.Fatalf("Locate error = %v, want the manifest in the plugin root", err)
	}
	if got != shipped {
		t.Fatalf("Locate = %q, want %q", got, shipped)
	}
}

func TestLocateFallsBackToTheWorkingDirectory(t *testing.T) {
	cwd := t.TempDir()
	shipped := filepath.Join(cwd, FileName)
	if err := os.WriteFile(shipped, []byte(shippedManifest), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := Locate("", "", cwd)
	if err != nil {
		t.Fatalf("Locate error = %v, want the manifest in the working directory", err)
	}
	if got != shipped {
		t.Fatalf("Locate = %q, want %q", got, shipped)
	}
}

func TestLocateHonoursAnExplicitPath(t *testing.T) {
	explicit := filepath.Join(t.TempDir(), "custom.toml")
	if err := os.WriteFile(explicit, []byte(shippedManifest), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := Locate(explicit, "", "")
	if err != nil {
		t.Fatalf("Locate error = %v, want the explicit path", err)
	}
	if got != explicit {
		t.Fatalf("Locate = %q, want %q", got, explicit)
	}
}

func TestLocateReportsEveryPlaceItLooked(t *testing.T) {
	root := t.TempDir()

	_, err := Locate("", filepath.Join(root, "bin", "hs"), root)
	if err == nil {
		t.Fatal("Locate error = nil, want the missing manifest reported")
	}
	if !strings.Contains(err.Error(), FileName) || !strings.Contains(err.Error(), root) {
		t.Fatalf("Locate error = %q, want it to name %s and the places it looked", err, FileName)
	}
}

func TestCompareOrdersDottedVersions(t *testing.T) {
	tests := []struct {
		left, right string
		want        int
	}{
		{left: "0.7.5", right: "0.7.5", want: 0},
		{left: "0.9.5", right: "0.7.5", want: 1},
		{left: "0.7.4", right: "0.7.5", want: -1},
		{left: "0.10.0", right: "0.9.9", want: 1},
		{left: "1.0", right: "1.0.0", want: 0},
	}
	for _, tt := range tests {
		got, err := Compare(tt.left, tt.right)
		if err != nil {
			t.Fatalf("Compare(%q, %q) error = %v, want nil", tt.left, tt.right, err)
		}
		if got != tt.want {
			t.Fatalf("Compare(%q, %q) = %d, want %d", tt.left, tt.right, got, tt.want)
		}
	}
}

func TestCompareRefusesAVersionItCannotRead(t *testing.T) {
	for _, version := range []string{"dev", "", "0.7.x", "v0.7.5"} {
		if _, err := Compare(version, "0.7.5"); err == nil {
			t.Fatalf("Compare(%q, %q) error = nil, want a refusal", version, "0.7.5")
		}
	}
}
