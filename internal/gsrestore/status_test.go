package gsrestore

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func noGentleShell(string) (string, error) {
	return "", exec.ErrNotFound
}

func TestInspectReportsAMissingReporter(t *testing.T) {
	target := testTarget(t)
	getenv := envFrom(map[string]string{"PI_CODING_AGENT_DIR": target.AgentDir})

	status := Inspect(getenv, noGentleShell, nil)
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
	}, nil)
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

	status := Inspect(getenv, noGentleShell, nil)
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
	status := Inspect(envFrom(map[string]string{}), noGentleShell, nil)
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

	status := Inspect(getenv, noGentleShell, nil)
	if status.Backup != result.Backup {
		t.Fatalf("Backup = %q, want %q", status.Backup, result.Backup)
	}
}

func TestDocumentCarriesEveryFieldTheReportPrints(t *testing.T) {
	target := testTarget(t)
	if _, err := Install(target, reporterFixture(), false, time.Now()); err != nil {
		t.Fatalf("Install: %v", err)
	}
	getenv := envFrom(map[string]string{"PI_CODING_AGENT_DIR": target.AgentDir})

	status := Inspect(getenv, noGentleShell, nil)
	document := status.Document()

	if document.AgentDir != target.AgentDir || document.AgentDirError != "" {
		t.Fatalf("agent dir fields = %#v, want %q", document, target.AgentDir)
	}
	if document.ReporterPath != target.Path || !document.ReporterPresent || !document.ReporterInstalled {
		t.Fatalf("reporter fields = %#v, want the installed reporter", document)
	}
	if document.ReporterVersion != "v1" || !document.VersionMatches {
		t.Fatalf("version fields = %#v, want the shipped marker", document)
	}
	if document.ShippedVersion != ReporterVersion() {
		t.Fatalf("ShippedVersion = %q, want %q", document.ShippedVersion, ReporterVersion())
	}
	if document.IntegrationVersion != ShippedIntegrationVersion {
		t.Fatalf("IntegrationVersion = %d, want %d", document.IntegrationVersion, ShippedIntegrationVersion)
	}
	if document.IntegrationExpected != 0 || document.IntegrationDiffers {
		t.Fatalf("integration fields = %#v, want no expected version and no difference", document)
	}
	if document.IntegrationShipped != ShippedIntegrationVersion {
		t.Fatalf("IntegrationShipped = %d, want %d", document.IntegrationShipped, ShippedIntegrationVersion)
	}
	if document.Install || document.AutoInstall {
		t.Fatalf("toggles = %#v, want both off", document)
	}
}

func TestDocumentRecordsAnUnresolvableAgentDirAsAField(t *testing.T) {
	status := Inspect(envFrom(map[string]string{}), noGentleShell, nil)

	document := status.Document()
	if document.AgentDirError == "" {
		t.Fatal("AgentDirError = empty, want the missing agent dir reported instead of dropped")
	}
	if document.AgentDir != "" || document.ReporterInstalled {
		t.Fatalf("document = %#v, want an unknown agent dir without a reporter", document)
	}
}

func TestWriteJSONPrintsOneDocument(t *testing.T) {
	var buf bytes.Buffer
	status := Inspect(envFrom(map[string]string{}), noGentleShell, nil)

	if err := WriteJSON(&buf, status); err != nil {
		t.Fatalf("WriteJSON error = %v", err)
	}
	if got := strings.Count(strings.TrimSpace(buf.String()), "\n"); got != 0 {
		t.Fatalf("WriteJSON printed %d newlines, want one document:\n%s", got, buf.String())
	}
	var document map[string]any
	if err := json.Unmarshal(buf.Bytes(), &document); err != nil {
		t.Fatalf("WriteJSON output is not JSON: %v\n%s", err, buf.String())
	}
	if _, ok := document["shipped_version"]; !ok {
		t.Fatalf("document has no shipped_version:\n%s", buf.String())
	}
}

func TestInspectReportsTheHerdrIntegrationMarker(t *testing.T) {
	target := testTarget(t)
	reporter := IntegrationIDLine + "\n" + IntegrationVersionLine(10) + "\n" + ReporterVersionLine + "\n"
	if err := os.WriteFile(target.Path, []byte(reporter), 0o644); err != nil {
		t.Fatal(err)
	}
	getenv := envFrom(map[string]string{"PI_CODING_AGENT_DIR": target.AgentDir})

	status := Inspect(getenv, noGentleShell, func() (string, bool) {
		return "pi: current (v10) (/tmp/extensions/herdr-agent-state.ts)\n", true
	})

	if status.IntegrationVersion != 10 {
		t.Fatalf("IntegrationVersion = %d, want the installed marker 10", status.IntegrationVersion)
	}
	if status.IntegrationExpected != 10 {
		t.Fatalf("IntegrationExpected = %d, want herdr's 10", status.IntegrationExpected)
	}
	if !status.IntegrationDiffers() {
		t.Fatal("IntegrationDiffers = false, want herdr's 10 reported as a difference from the shipped 9")
	}
}

func TestInspectReportsAnOutdatedIntegrationMarkerAsInformation(t *testing.T) {
	target := testTarget(t)
	reporter := IntegrationIDLine + "\n" + IntegrationVersionLine(ShippedIntegrationVersion) + "\n" + ReporterVersionLine + "\n"
	if err := os.WriteFile(target.Path, []byte(reporter), 0o644); err != nil {
		t.Fatal(err)
	}
	getenv := envFrom(map[string]string{"PI_CODING_AGENT_DIR": target.AgentDir})

	status := Inspect(getenv, noGentleShell, func() (string, bool) {
		return "pi: outdated (v9 < v10) (/tmp/extensions/herdr-agent-state.ts)\n", true
	})

	if status.IntegrationVersion != ShippedIntegrationVersion || status.IntegrationExpected != 10 {
		t.Fatalf("integration = installed %d, expected %d, want %d and 10",
			status.IntegrationVersion, status.IntegrationExpected, ShippedIntegrationVersion)
	}
	if !status.IntegrationDiffers() {
		t.Fatal("IntegrationDiffers = false, want herdr's 10 reported as information")
	}
}

func TestInspectLeavesTheIntegrationUnknownWithoutHerdr(t *testing.T) {
	target := testTarget(t)
	if err := os.WriteFile(target.Path, reporterFixture(), 0o644); err != nil {
		t.Fatal(err)
	}
	getenv := envFrom(map[string]string{"PI_CODING_AGENT_DIR": target.AgentDir})

	for _, run := range []struct {
		name  string
		probe Probe
	}{
		{name: "no probe"},
		{name: "herdr refused", probe: func() (string, bool) { return "", false }},
		{name: "text does not parse", probe: func() (string, bool) { return "garbage", true }},
	} {
		t.Run(run.name, func(t *testing.T) {
			status := Inspect(getenv, noGentleShell, run.probe)
			if status.IntegrationVersion != ShippedIntegrationVersion {
				t.Fatalf("IntegrationVersion = %d, want the installed %d", status.IntegrationVersion, ShippedIntegrationVersion)
			}
			if status.IntegrationExpected != 0 {
				t.Fatalf("IntegrationExpected = %d, want 0 when herdr did not report one", status.IntegrationExpected)
			}
			if status.IntegrationDiffers() {
				t.Fatal("IntegrationDiffers = true, want no information without herdr's answer")
			}
		})
	}
}
