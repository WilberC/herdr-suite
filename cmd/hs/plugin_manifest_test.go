package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

type manifestAction struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Contexts    []string `json:"contexts"`
	Command     []string `json:"command"`
	Platforms   []string `json:"platforms"`
}

type manifestPane struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Placement string   `json:"placement"`
	Command   []string `json:"command"`
}

type pluginManifest struct {
	ID              string           `json:"id"`
	Name            string           `json:"name"`
	Version         string           `json:"version"`
	MinHerdrVersion string           `json:"min_herdr_version"`
	Description     string           `json:"description"`
	Platforms       []string         `json:"platforms"`
	Actions         []manifestAction `json:"actions"`
	Panes           []manifestPane   `json:"panes"`
	Startup         []map[string]any `json:"startup"`
	Events          []map[string]any `json:"events"`
}

// manifestPath is the plugin manifest this repository ships.
func manifestPath() string {
	return filepath.Join("..", "..", "herdr-plugin.toml")
}

func readManifestBytes(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(manifestPath())
	if err != nil {
		t.Fatalf("read the plugin manifest: %v", err)
	}
	return data
}

// parseManifest reads the manifest with the TOML parser the ecosystem already
// uses, so the test agrees with what herdr reads instead of reimplementing TOML.
func parseManifest(t *testing.T, data []byte) pluginManifest {
	t.Helper()
	interpreter, err := exec.LookPath("python3")
	if err != nil {
		interpreter, err = exec.LookPath("python")
	}
	if err != nil {
		t.Skip("reading the plugin manifest needs a python with tomllib (3.11+)")
	}

	script := "import json,sys,tomllib;json.dump(tomllib.loads(sys.stdin.read()),sys.stdout)"
	cmd := exec.Command(interpreter, "-c", script)
	cmd.Stdin = bytes.NewReader(data)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("tomllib refused the plugin manifest: %v\n%s", err, stderr.String())
	}

	var document pluginManifest
	if err := json.Unmarshal(stdout.Bytes(), &document); err != nil {
		t.Fatalf("decode the parsed manifest: %v\n%s", err, stdout.String())
	}
	return document
}

func TestPluginManifestParses(t *testing.T) {
	document := parseManifest(t, readManifestBytes(t))

	if document.ID != "herdr-suite" {
		t.Fatalf("manifest id = %q, want %q", document.ID, "herdr-suite")
	}
	if document.Name == "" || document.Version == "" || document.Description == "" {
		t.Fatalf("manifest = %#v, want a name, a version and a description", document)
	}
	for _, want := range []string{"linux", "macos"} {
		if !strings.Contains(strings.Join(document.Platforms, ","), want) {
			t.Fatalf("manifest platforms = %v, want %q", document.Platforms, want)
		}
	}
}

// TestPluginManifestRequiresSnapshotCapableHerdr pins the verified finding behind
// min_herdr_version: "herdr api snapshot" first shipped in herdr 0.7.2, where
// src/cli/api.rs gained the snapshot subcommand (absent in 0.7.1) together with
// the session.snapshot socket method. 0.7.5 satisfies it, so the declared
// minimum stays where the later layout work put it.
func TestPluginManifestRequiresSnapshotCapableHerdr(t *testing.T) {
	document := parseManifest(t, readManifestBytes(t))

	if document.MinHerdrVersion != "0.7.5" {
		t.Fatalf("min_herdr_version = %q, want %q", document.MinHerdrVersion, "0.7.5")
	}
	if compareVersions(document.MinHerdrVersion, "0.7.2") < 0 {
		t.Fatalf("min_herdr_version = %q, want at least 0.7.2, the release that added 'herdr api snapshot'",
			document.MinHerdrVersion)
	}
}

func compareVersions(left, right string) int {
	parse := func(version string) []int {
		parts := strings.Split(version, ".")
		numbers := make([]int, 0, len(parts))
		for _, part := range parts {
			number, err := strconv.Atoi(part)
			if err != nil {
				return nil
			}
			numbers = append(numbers, number)
		}
		return numbers
	}
	leftParts, rightParts := parse(left), parse(right)
	if leftParts == nil || rightParts == nil {
		return -1
	}
	for index := 0; index < len(leftParts) || index < len(rightParts); index++ {
		got, want := 0, 0
		if index < len(leftParts) {
			got = leftParts[index]
		}
		if index < len(rightParts) {
			want = rightParts[index]
		}
		if got != want {
			if got < want {
				return -1
			}
			return 1
		}
	}
	return 0
}

func TestPluginActionsCoverSaveLoadAndList(t *testing.T) {
	document := parseManifest(t, readManifestBytes(t))

	actions := make(map[string]manifestAction, len(document.Actions))
	for _, action := range document.Actions {
		if action.ID == "" || action.Title == "" || action.Description == "" {
			t.Fatalf("action %#v, want an id, a title and a description", action)
		}
		if len(action.Contexts) == 0 {
			t.Fatalf("action %q declares no contexts", action.ID)
		}
		if len(action.Command) == 0 {
			t.Fatalf("action %q declares no command", action.ID)
		}
		if _, again := actions[action.ID]; again {
			t.Fatalf("action %q is declared twice", action.ID)
		}
		actions[action.ID] = action
	}

	for _, want := range []string{"save", "load", "list"} {
		if _, ok := actions[want]; !ok {
			t.Fatalf("manifest actions = %v, want the %q action", document.Actions, want)
		}
	}
}

// hsSubcommands mirrors the subcommand table the CLI dispatches on. The test
// fails when the manifest names something this table does not hold.
var hsSubcommands = map[string]map[string]bool{
	"space":   {"save": true, "load": true, "list": true},
	"session": {"list": true, "create": true, "stop": true, "restart": true, "delete": true, "fork": true},
}

func usageFor(key string) string {
	switch key {
	case "attach":
		return attachUsage
	case "space save":
		return spaceSaveUsage
	case "space load":
		return spaceLoadUsage
	case "space list":
		return spaceListUsage
	case "session fork":
		return sessionForkUsage
	case "session":
		return sessionUsage
	}
	return ""
}

func TestPluginCommandsNameSubcommandsTheCLIHolds(t *testing.T) {
	document := parseManifest(t, readManifestBytes(t))
	panes := make(map[string]bool, len(document.Panes))
	for _, pane := range document.Panes {
		panes[pane.ID] = true
	}

	commands := make([][]string, 0, len(document.Actions)+len(document.Panes))
	for _, action := range document.Actions {
		commands = append(commands, action.Command)
	}
	for _, pane := range document.Panes {
		commands = append(commands, pane.Command)
	}

	for _, command := range commands {
		switch command[0] {
		case "bin/hs":
			checkHerdrSuiteCommand(t, command)
		case "bash":
			checkHerdrInvocation(t, command, document.ID, panes)
		default:
			t.Fatalf("command %v starts neither bin/hs nor bash", command)
		}
	}
}

// checkHerdrSuiteCommand asserts a command invokes a subcommand of this binary
// and only flags that subcommand documents.
func checkHerdrSuiteCommand(t *testing.T, command []string) {
	t.Helper()
	if len(command) < 2 {
		t.Fatalf("command %v names no subcommand", command)
	}
	if _, ok := lookup(command[1]); !ok {
		t.Fatalf("command %v names the unknown command %q", command, command[1])
	}

	key := command[1]
	if subcommands, ok := hsSubcommands[key]; ok {
		if len(command) < 3 || !subcommands[command[2]] {
			t.Fatalf("command %v names no known %q subcommand, want one of %v", command, key, keys(subcommands))
		}
		key += " " + command[2]
	}
	usage := usageFor(key)
	if usage == "" {
		t.Fatalf("command %v has no documented usage to check", command)
	}
	for _, token := range command[2:] {
		if !strings.HasPrefix(token, "-") {
			continue
		}
		if !strings.Contains(usage, token) {
			t.Fatalf("command %v passes %q, which %q does not document", command, token, key)
		}
	}
}

// checkHerdrInvocation asserts a shell command opens a pane this manifest
// declares, for this plugin.
func checkHerdrInvocation(t *testing.T, command []string, pluginID string, panes map[string]bool) {
	t.Helper()
	script := strings.Join(command, " ")
	if !strings.Contains(script, "plugin pane open") {
		t.Fatalf("command %v does not open a plugin pane", command)
	}
	fields := strings.Fields(script)
	if value := flagValue(fields, "--plugin"); value != pluginID {
		t.Fatalf("command %v opens plugin %q, want %q", command, value, pluginID)
	}
	entrypoint := flagValue(fields, "--entrypoint")
	if !panes[entrypoint] {
		t.Fatalf("command %v opens the undeclared pane %q, want one of %v", command, entrypoint, keys(panes))
	}
}

func flagValue(fields []string, flag string) string {
	for index, field := range fields {
		if field == flag && index+1 < len(fields) {
			return fields[index+1]
		}
	}
	return ""
}

func keys(values map[string]bool) []string {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	return names
}
