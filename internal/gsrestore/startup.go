package gsrestore

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Plan is what the startup hook does for a config.
type Plan string

const (
	// PlanNothing leaves the agent directory untouched.
	PlanNothing Plan = "nothing"
	// PlanHint prints the one-line reminder once per server.
	PlanHint Plan = "hint"
	// PlanInstall installs or refreshes the reporter idempotently.
	PlanInstall Plan = "install"
)

// Decide applies the gate: install is the master switch, and auto_install only
// chooses between installing and hinting. auto_install alone is nonsensical and
// does nothing, which the README documents.
func Decide(config Config) Plan {
	if !config.Install {
		return PlanNothing
	}
	if config.AutoInstall {
		return PlanInstall
	}
	return PlanHint
}

const hintFileName = "startup-hint.json"

// HintPath is the file recording which server sockets already saw the hint.
func HintPath(getenv func(string) string) (string, error) {
	dir, err := stateDir(getenv)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, hintFileName), nil
}

func stateDir(getenv func(string) string) (string, error) {
	if dir := getenv("HERDR_PLUGIN_STATE_DIR"); dir != "" {
		return dir, nil
	}
	base := getenv("XDG_STATE_HOME")
	if base == "" {
		home := getenv("HOME")
		if home == "" {
			return "", fmt.Errorf("cannot locate the plugin state directory: HERDR_PLUGIN_STATE_DIR, XDG_STATE_HOME and HOME are all unset")
		}
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "herdr", "plugins", "herdr-suite"), nil
}

// hintState keys a hinted server by its socket path, so a new server hints once
// and a restarted session stays quiet.
type hintState struct {
	Hinted map[string]string `json:"hinted"`
}

const defaultSocketKey = "default"

// TakeHint reports whether this is the first start recorded for the current
// server socket, and records it.
func TakeHint(getenv func(string) string, now time.Time) (bool, error) {
	path, err := HintPath(getenv)
	if err != nil {
		return false, err
	}
	key := getenv("HERDR_SOCKET_PATH")
	if key == "" {
		key = defaultSocketKey
	}

	state := hintState{}
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		if err := json.Unmarshal(data, &state); err != nil {
			state = hintState{}
		}
	case os.IsNotExist(err):
	default:
		return false, fmt.Errorf("read %s: %w", path, err)
	}
	if state.Hinted == nil {
		state.Hinted = map[string]string{}
	}
	if _, seen := state.Hinted[key]; seen {
		return false, nil
	}
	state.Hinted[key] = now.UTC().Format(time.RFC3339)

	encoded, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return false, fmt.Errorf("encode %s: %w", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, fmt.Errorf("create the plugin state directory: %w", err)
	}
	if err := os.WriteFile(path, append(encoded, '\n'), 0o644); err != nil {
		return false, fmt.Errorf("write %s: %w", path, err)
	}
	return true, nil
}
