package gsrestore

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func noGentleShell(string) (string, error) {
	return "", exec.ErrNotFound
}

func TestInspectReportsAMissingReporter(t *testing.T) {
	target := testTarget(t)
	getenv := envFrom(map[string]string{"PI_CODING_AGENT_DIR": target.AgentDir})

	status := Inspect(getenv, noGentleShell)
	if status.TargetErr != nil {
		t.Fatalf("TargetErr = %v, want a resolved target", status.TargetErr)
	}
	if status.Installed {
		t.Fatal("Installed = true, want false")
	}
	if status.Backup != "" {
		t.Fatalf("Backup = %q, want none", status.Backup)
	}
	if status.GentleShell != "" {
		t.Fatalf("GentleShell = %q, want unresolved", status.GentleShell)
	}
	if status.Config.Install || status.Config.AutoInstall {
		t.Fatalf("Config = %#v, want both toggles off", status.Config)
	}
}

func TestInspectReportsTheInstalledReporter(t *testing.T) {
	target := testTarget(t)
	if _, err := Install(target, reporterFixture(), false, time.Now()); err != nil {
		t.Fatalf("Install: %v", err)
	}
	gentleShell := filepath.Join(t.TempDir(), "gentle-shell")
	getenv := envFrom(map[string]string{"PI_CODING_AGENT_DIR": target.AgentDir})

	status := Inspect(getenv, func(name string) (string, error) {
		if name != "gentle-shell" {
			t.Fatalf("lookPath(%q), want gentle-shell", name)
		}
		return gentleShell, nil
	})
	if !status.Installed {
		t.Fatal("Installed = false, want true")
	}
	if status.InstalledVersion != "v1" {
		t.Fatalf("InstalledVersion = %q, want v1", status.InstalledVersion)
	}
	if !status.VersionMatches {
		t.Fatal("VersionMatches = false, want the shipped marker")
	}
	if status.GentleShell != gentleShell {
		t.Fatalf("GentleShell = %q, want %q", status.GentleShell, gentleShell)
	}
}

func TestInspectReportsADifferentReporterVersion(t *testing.T) {
	target := testTarget(t)
	older := []byte("// HERDR_SUITE_GS_RESTORE_VERSION=0\n")
	if err := os.WriteFile(target.Path, older, 0o644); err != nil {
		t.Fatal(err)
	}
	getenv := envFrom(map[string]string{"PI_CODING_AGENT_DIR": target.AgentDir})

	status := Inspect(getenv, noGentleShell)
	if !status.Installed {
		t.Fatal("Installed = false, want the marker to be found")
	}
	if status.InstalledVersion != "v0" {
		t.Fatalf("InstalledVersion = %q, want v0", status.InstalledVersion)
	}
	if status.VersionMatches {
		t.Fatal("VersionMatches = true, want a mismatch against the shipped marker")
	}
}

func TestInspectRecordsAnUnresolvableAgentDir(t *testing.T) {
	status := Inspect(envFrom(map[string]string{}), noGentleShell)
	if status.TargetErr == nil {
		t.Fatal("TargetErr = nil, want the missing PI_CODING_AGENT_DIR reported")
	}
	if status.Installed {
		t.Fatal("Installed = true, want false when the agent dir is unknown")
	}
}

func TestInspectFindsTheLatestBackup(t *testing.T) {
	target := testTarget(t)
	if _, err := Install(target, reporterFixture(), false, time.Date(2026, 11, 12, 20, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target.Path, []byte("// HERDR_INTEGRATION_ID=pi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := Install(target, reporterFixture(), false, time.Date(2026, 11, 12, 20, 0, 5, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	getenv := envFrom(map[string]string{"PI_CODING_AGENT_DIR": target.AgentDir})

	status := Inspect(getenv, noGentleShell)
	if status.Backup != result.Backup {
		t.Fatalf("Backup = %q, want %q", status.Backup, result.Backup)
	}
}
