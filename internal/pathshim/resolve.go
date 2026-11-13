package pathshim

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

// Plugin is the checkout the script would exec.
type Plugin struct {
	Root   string
	Binary string
}

// pluginRootField is the field Script reads with grep and sed, by the same rule:
// the first "plugin_root" value in "herdr plugin list --json". The colon and the
// spacing are tolerated so a pretty-printed herdr keeps working.
var pluginRootField = regexp.MustCompile(`"plugin_root"[ \t\n\v\f\r]*:[ \t\n\v\f\r]*"([^"]*)"`)

// ParsePluginRoot is the read side of the shim: it returns the first
// "plugin_root" value in a plugin list, or "" when the output carries none.
func ParsePluginRoot(output []byte) string {
	match := pluginRootField.FindSubmatch(output)
	if match == nil {
		return ""
	}
	return string(match[1])
}

// Resolve runs the plugin list and checks that the binary the shim would exec is
// there. A herdr that fails is an error; a missing plugin or a checkout without
// an executable bin/hs is a refusal naming the command that fixes it.
func Resolve(run func() ([]byte, error)) (Plugin, error) {
	output, err := run()
	if err != nil {
		return Plugin{}, fmt.Errorf("read herdr plugin list: %w", err)
	}
	root := ParsePluginRoot(output)
	if root == "" {
		return Plugin{}, Refusal{Reason: "the herdr-suite plugin is not installed; run: herdr plugin install WilberC/herdr-suite"}
	}
	binary := filepath.Join(root, "bin", FileName)
	info, err := os.Stat(binary)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return Plugin{}, Refusal{Reason: fmt.Sprintf(
			"%s is missing or not executable; run: herdr plugin update herdr-suite", binary)}
	}
	return Plugin{Root: root, Binary: binary}, nil
}
