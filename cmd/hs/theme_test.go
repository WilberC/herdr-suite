package main

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/WilberC/tui-kit/theme"
)

// The switcher is the interactive surface, so it installs the theme before it
// renders: the setting names a file in the themes directory, and a theme that
// fails to load leaves the built-in in force.
func TestDefaultSwitcherInstallsTheConfiguredTheme(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "gruvbox.json"), []byte(`{"name": "gruvbox", "colors": {"text": "#ebdbb2"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	previous := hsThemeDir
	hsThemeDir = func() string { return dir }
	t.Cleanup(func() { hsThemeDir = previous })

	d := deps{getenv: func(string) string { return "gruvbox" }}
	_ = defaultSwitcher(d, io.Discard)
	t.Cleanup(func() { _ = theme.Use(theme.TokyoNight()) })

	if got := theme.Active().Name; got != "gruvbox" {
		t.Fatalf("the switcher rendered with the theme %q, want \"gruvbox\"", got)
	}
}

func TestDefaultSwitcherWithoutASettingKeepsTheBuiltIn(t *testing.T) {
	previous := hsThemeDir
	hsThemeDir = func() string { return t.TempDir() }
	t.Cleanup(func() { hsThemeDir = previous })

	d := deps{getenv: func(string) string { return "" }}
	_ = defaultSwitcher(d, io.Discard)
	t.Cleanup(func() { _ = theme.Use(theme.TokyoNight()) })

	if got := theme.Active().Name; got != "tokyo-night" && got != "system" {
		t.Fatalf("the switcher rendered with the theme %q, want the built-in or the system theme", got)
	}
}
