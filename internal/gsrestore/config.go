package gsrestore

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Config is the gs_restore section: install is the master gate, and
// auto_install only decides whether the startup hook installs or hints.
type Config struct {
	Install     bool
	AutoInstall bool
}

// Document is a loaded config file. The raw maps keep keys this version does
// not know, so writing the file back never drops another version's setting.
type Document struct {
	Config  Config
	Present bool
	top     map[string]json.RawMessage
	section map[string]json.RawMessage
}

const sectionKey = "gs_restore"

// ConfigPath resolves the plugin config file:
// HERDR_PLUGIN_CONFIG_DIR/config.json when herdr supplies it, otherwise
// <XDG_CONFIG_HOME>/herdr/plugins/config/herdr-suite/config.json, otherwise the
// same path under $HOME/.config.
func ConfigPath(getenv func(string) string) (string, error) {
	if dir := getenv("HERDR_PLUGIN_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, "config.json"), nil
	}
	base := getenv("XDG_CONFIG_HOME")
	if base == "" {
		home := getenv("HOME")
		if home == "" {
			return "", fmt.Errorf("cannot locate the plugin config: HERDR_PLUGIN_CONFIG_DIR, XDG_CONFIG_HOME and HOME are all unset")
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "herdr", "plugins", "config", "herdr-suite", "config.json"), nil
}

// Load reads the config. A missing file is not an error: it means both toggles
// are off, which is the documented default. A malformed file is an error the
// caller reports; the startup hook treats it as off.
func Load(getenv func(string) string) (Document, error) {
	path, err := ConfigPath(getenv)
	if err != nil {
		return Document{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Document{Config: Config{}}, nil
		}
		return Document{}, fmt.Errorf("read %s: %w", path, err)
	}
	return parseConfig(data, path)
}

func parseConfig(data []byte, path string) (Document, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		return Document{}, fmt.Errorf("parse %s: %w", path, err)
	}
	document := Document{Present: true, top: top}

	raw, ok := top[sectionKey]
	if !ok {
		return document, nil
	}
	if err := json.Unmarshal(raw, &document.section); err != nil {
		return Document{}, fmt.Errorf("parse %s: %s must be a JSON object: %w", path, sectionKey, err)
	}

	var wire struct {
		Install     *bool `json:"install"`
		AutoInstall *bool `json:"auto_install"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return Document{}, fmt.Errorf("parse %s: %s.install and %s.auto_install must be true or false: %w",
			path, sectionKey, sectionKey, err)
	}
	if wire.Install != nil {
		document.Config.Install = *wire.Install
	}
	if wire.AutoInstall != nil {
		document.Config.AutoInstall = *wire.AutoInstall
	}
	return document, nil
}

// Save writes the document back, merging the two known toggles into the raw
// JSON it was loaded from.
func Save(path string, document Document) error {
	top := document.top
	if top == nil {
		top = map[string]json.RawMessage{}
	}
	section := document.section
	if section == nil {
		section = map[string]json.RawMessage{}
	}

	install, err := json.Marshal(document.Config.Install)
	if err != nil {
		return err
	}
	autoInstall, err := json.Marshal(document.Config.AutoInstall)
	if err != nil {
		return err
	}
	section["install"] = install
	section["auto_install"] = autoInstall

	encoded, err := json.MarshalIndent(section, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", sectionKey, err)
	}
	top[sectionKey] = encoded

	data, err := json.MarshalIndent(top, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create the config directory: %w", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
