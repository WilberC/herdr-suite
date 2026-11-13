package gsrestore

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func envFrom(values map[string]string) func(string) string {
	return func(name string) string { return values[name] }
}

func TestConfigDefaultsAndMissingFile(t *testing.T) {
	root := t.TempDir()
	getenv := envFrom(map[string]string{"XDG_CONFIG_HOME": root})

	path, err := ConfigPath(getenv)
	if err != nil {
		t.Fatalf("ConfigPath: %v", err)
	}
	want := filepath.Join(root, "herdr", "plugins", "config", "herdr-suite", "config.json")
	if path != want {
		t.Fatalf("ConfigPath = %q, want %q", path, want)
	}

	document, err := Load(getenv)
	if err != nil {
		t.Fatalf("Load a missing config: %v", err)
	}
	if document.Present {
		t.Fatalf("Present = true, want false for a missing config")
	}
	if document.Config.Install || document.Config.AutoInstall {
		t.Fatalf("Config = %#v, want both toggles off by default", document.Config)
	}
}

func TestConfigPluginConfigDirWinsOverXDG(t *testing.T) {
	root := t.TempDir()
	override := filepath.Join(root, "plugin-config")
	getenv := envFrom(map[string]string{
		"HERDR_PLUGIN_CONFIG_DIR": override,
		"XDG_CONFIG_HOME":         filepath.Join(root, "xdg"),
	})

	path, err := ConfigPath(getenv)
	if err != nil {
		t.Fatalf("ConfigPath: %v", err)
	}
	if want := filepath.Join(override, "config.json"); path != want {
		t.Fatalf("ConfigPath = %q, want %q", path, want)
	}
}

func TestConfigIoReadsOnlyTheKnownToggles(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "herdr", "plugins", "config", "herdr-suite")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.json")
	body := `{"gs_restore": {"install": true}}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	document, err := Load(envFrom(map[string]string{"XDG_CONFIG_HOME": root}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !document.Present {
		t.Fatalf("Present = false, want true")
	}
	if !document.Config.Install {
		t.Fatalf("Install = false, want true")
	}
	if document.Config.AutoInstall {
		t.Fatalf("AutoInstall = true, want the documented false default")
	}
}

func TestConfigPreservesUnknownKeysOnSave(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "herdr", "plugins", "config", "herdr-suite")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.json")
	body := `{
  "gs_restore": {"auto_install": false, "install": false, "note": "keep me"},
  "other_plugin": {"enabled": true}
}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	getenv := envFrom(map[string]string{"XDG_CONFIG_HOME": root})
	document, err := Load(getenv)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	document.Config.Install = true
	if err := Save(path, document); err != nil {
		t.Fatalf("Save: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		t.Fatalf("decode the saved config: %v\n%s", err, data)
	}
	if _, ok := top["other_plugin"]; !ok {
		t.Fatalf("saved config = %s, want the unknown top-level key preserved", data)
	}
	var section map[string]json.RawMessage
	if err := json.Unmarshal(top["gs_restore"], &section); err != nil {
		t.Fatalf("decode gs_restore: %v", err)
	}
	if got := string(section["note"]); got != `"keep me"` {
		t.Fatalf("gs_restore.note = %s, want it preserved", got)
	}
	if got := string(section["install"]); got != "true" {
		t.Fatalf("gs_restore.install = %s, want true", got)
	}
	if got := string(section["auto_install"]); got != "false" {
		t.Fatalf("gs_restore.auto_install = %s, want the previous false preserved", got)
	}

	reloaded, err := Load(getenv)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !reloaded.Config.Install || reloaded.Config.AutoInstall {
		t.Fatalf("reloaded Config = %#v, want install on and auto-install off", reloaded.Config)
	}
}

func TestConfigRefusesANonBooleanToggle(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "herdr", "plugins", "config", "herdr-suite")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"gs_restore": {"install": "yes"}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(envFrom(map[string]string{"XDG_CONFIG_HOME": root}))
	if err == nil {
		t.Fatal("Load accepted a string where the config documents a boolean")
	}
	if !strings.Contains(err.Error(), "gs_restore.install") {
		t.Fatalf("Load error = %q, want it to name gs_restore.install", err)
	}
}
