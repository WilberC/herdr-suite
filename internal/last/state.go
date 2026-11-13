package last

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// State is the recorded focus history: the previous and current id of each
// pair. A field with no id is written as an explicit null.
type State struct {
	PrevWorkspace *string `json:"prev_workspace"`
	CurWorkspace  *string `json:"cur_workspace"`
	PrevTab       *string `json:"prev_tab"`
	CurTab        *string `json:"cur_tab"`
}

const (
	stateDirName     = "herdr-suite"
	stateFilePrefix  = "last-"
	stateFileSuffix  = ".json"
	socketKeyLength  = 12
	defaultSocketKey = "default"
)

// Key is the file-safe name of one herdr socket: the first twelve hex digits of
// its SHA-256, or "default" when the environment names no socket.
func Key(socket string) string {
	if socket == "" {
		return defaultSocketKey
	}
	sum := sha256.Sum256([]byte(socket))
	return hex.EncodeToString(sum[:])[:socketKeyLength]
}

// Path resolves the state file. herdr injects HERDR_PLUGIN_STATE_DIR pointing
// at this plugin's own state directory; the XDG/HOME fallback mirrors that
// herdr layout (…/herdr/plugins/herdr-suite) so a CLI toggle reads exactly the
// file the event hooks write. One file per socket.
func Path(getenv func(string) string) (string, error) {
	base := getenv("HERDR_PLUGIN_STATE_DIR")
	if base == "" {
		base = getenv("XDG_STATE_HOME")
		if base == "" {
			home := getenv("HOME")
			if home == "" {
				return "", fmt.Errorf("cannot locate the focus-history state: HERDR_PLUGIN_STATE_DIR, XDG_STATE_HOME and HOME are all unset")
			}
			base = filepath.Join(home, ".local", "state")
		}
		base = filepath.Join(base, "herdr", "plugins", stateDirName)
	}
	name := stateFilePrefix + Key(getenv("HERDR_SOCKET_PATH")) + stateFileSuffix
	return filepath.Join(base, name), nil
}

// Load reads one state file. A missing or corrupt file is an empty history; any
// other read failure is an error.
func Load(path string) (State, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return State{}, nil
		}
		return State{}, fmt.Errorf("read %s: %w", path, err)
	}
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return State{}, nil
	}
	return state, nil
}

// Save writes one state file atomically: a temporary file in the same directory
// is renamed over the target, so no reader sees a partial document and no
// temporary file outlives a successful write.
func Save(path string, state State) error {
	encoded, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create the focus-history state directory: %w", err)
	}
	temp, err := os.CreateTemp(dir, ".last-*.tmp")
	if err != nil {
		return fmt.Errorf("create a temporary file in %s: %w", dir, err)
	}
	name := temp.Name()
	defer os.Remove(name)

	if _, err := temp.Write(append(encoded, '\n')); err != nil {
		temp.Close()
		return fmt.Errorf("write %s: %w", name, err)
	}
	if err := temp.Chmod(0o644); err != nil {
		temp.Close()
		return fmt.Errorf("set the mode of %s: %w", name, err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close %s: %w", name, err)
	}
	if err := os.Rename(name, path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}
