package layout

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// schemaDocument is a minimal stored document whose schema_version line is the
// argument: an empty version is a file written before the field existed.
func schemaDocument(schemaVersion string) string {
	field := ""
	if schemaVersion != "" {
		field = `  "schema_version": ` + schemaVersion + ",\n"
	}
	return "{\n" + field + `  "name": "work",
  "workspace": {
    "label": "project",
    "tabs": []
  }
}
`
}

func writeSchemaFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "work.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write the stored document: %v", err)
	}
	return path
}

// TestSaveWritesTheCurrentSchemaVersion pins the top-level field every saved
// document carries, so a future build can tell which schema wrote a file.
func TestSaveWritesTheCurrentSchemaVersion(t *testing.T) {
	dir := t.TempDir()

	path, err := Save(dir, wantLayout(), false)
	if err != nil {
		t.Fatalf("Save error = %v, want nil", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the stored layout: %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatalf("the stored document is not valid JSON: %v\n%s", err, data)
	}
	if got := string(fields["schema_version"]); got != "1" {
		t.Fatalf("stored schema_version = %s, want 1; got:\n%s", got, data)
	}
	if !strings.HasPrefix(string(data), "{\n  \"schema_version\": 1,\n") {
		t.Fatalf("schema_version is not the first top-level field of the stored document:\n%s", data)
	}
}

// TestSchemaVersionRoundTripsByteIdentically pins that a save, a load and a
// second save of the same layout produce the same bytes: the schema version is
// written once, not accumulated per pass.
func TestSchemaVersionRoundTripsByteIdentically(t *testing.T) {
	dir := t.TempDir()
	if _, err := Save(dir, wantLayout(), false); err != nil {
		t.Fatalf("Save error = %v, want nil", err)
	}
	path := filepath.Join(dir, FileName("work"))
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the stored layout: %v", err)
	}

	loaded, _, err := Load(dir, "work")
	if err != nil {
		t.Fatalf("Load error = %v, want nil", err)
	}
	if loaded.SchemaVersion != CurrentSchema {
		t.Fatalf("loaded schema version = %d, want %d", loaded.SchemaVersion, CurrentSchema)
	}

	if _, err := Save(dir, loaded, true); err != nil {
		t.Fatalf("second Save error = %v, want nil", err)
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the re-saved layout: %v", err)
	}
	if string(second) != string(first) {
		t.Fatalf("a save/load/save round trip changed the document:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

// TestLoadReadsALegacyDocumentAsTheCurrentSchema pins the v0.1.0 files, which
// carry no schema_version: they load as the current schema and reading them
// never rewrites the file.
func TestLoadReadsALegacyDocumentAsTheCurrentSchema(t *testing.T) {
	body := schemaDocument("")
	path := writeSchemaFile(t, body)

	document, _, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile error = %v, want nil", err)
	}
	if document.SchemaVersion != CurrentSchema {
		t.Fatalf("legacy schema version = %d, want %d", document.SchemaVersion, CurrentSchema)
	}
	if document.Name != "work" || document.Workspace.Label != "project" {
		t.Fatalf("legacy document = %#v, want the stored name and workspace", document)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the legacy document: %v", err)
	}
	if string(after) != body {
		t.Fatalf("a load-only flow rewrote the legacy document:\nbefore:\n%s\nafter:\n%s", body, after)
	}
}

// TestSaveAddsOnlyTheSchemaVersionToALegacyDocument pins the accepted byte
// difference of a legacy save: the version field, and nothing else.
func TestSaveAddsOnlyTheSchemaVersionToALegacyDocument(t *testing.T) {
	body := schemaDocument("")
	path := writeSchemaFile(t, body)
	document, _, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile error = %v, want nil", err)
	}

	encoded, err := Marshal(document)
	if err != nil {
		t.Fatalf("Marshal error = %v, want nil", err)
	}
	want := strings.Replace(body, "{\n", "{\n  \"schema_version\": 1,\n", 1)
	if string(encoded) != want {
		t.Fatalf("saved legacy document =\n%s\nwant\n%s", encoded, want)
	}
}

// TestLoadRefusesASchemaThisBuildCannotRead pins the version gate: a file a
// newer herdr-suite wrote is refused with the reason and the fix, instead of
// being read with fields this build would silently drop.
func TestLoadRefusesASchemaThisBuildCannotRead(t *testing.T) {
	tests := []struct {
		name    string
		version string
		want    string
	}{
		{name: "a newer schema", version: "2", want: "written by a newer herdr-suite; update hs"},
		{name: "a far newer schema", version: "9", want: "written by a newer herdr-suite; update hs"},
		{name: "a negative version", version: "-1", want: "schema_version"},
		{name: "a fractional version", version: "1.5", want: "schema_version"},
		{name: "a quoted version", version: `"1"`, want: "schema_version"},
		{name: "a written version", version: `"one"`, want: "schema_version"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeSchemaFile(t, schemaDocument(tt.version))

			_, _, err := LoadFile(path)
			if err == nil {
				t.Fatalf("LoadFile error = nil, want a refusal of schema_version %s", tt.version)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("LoadFile error = %q, want it to contain %q", err, tt.want)
			}
			if !strings.Contains(err.Error(), path) {
				t.Fatalf("LoadFile error = %q, want it to name %s", err, path)
			}
		})
	}
}

// TestLoadRefusesANewerSchemaAsARefusal pins the error type: a document from a
// newer herdr-suite is a refusal the CLI reports as such, unlike a corrupt
// value, which is a read failure.
func TestLoadRefusesANewerSchemaAsARefusal(t *testing.T) {
	path := writeSchemaFile(t, schemaDocument("2"))

	_, _, err := LoadFile(path)
	if err == nil {
		t.Fatal("LoadFile error = nil, want a refusal")
	}
	var newer UnsupportedSchemaError
	if !errors.As(err, &newer) {
		t.Fatalf("LoadFile error = %v, want an UnsupportedSchemaError", err)
	}
	if newer.Version != 2 {
		t.Fatalf("UnsupportedSchemaError version = %d, want 2", newer.Version)
	}
}

// TestLoadAcceptsTheCurrentSchemaExplicitly pins that a file which names the
// current version reads exactly like a legacy one.
func TestLoadAcceptsTheCurrentSchemaExplicitly(t *testing.T) {
	path := writeSchemaFile(t, schemaDocument("1"))

	document, _, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile error = %v, want nil", err)
	}
	if document.SchemaVersion != CurrentSchema {
		t.Fatalf("schema version = %d, want %d", document.SchemaVersion, CurrentSchema)
	}
	if document.Workspace.Label != "project" {
		t.Fatalf("workspace label = %q, want %q", document.Workspace.Label, "project")
	}
}
