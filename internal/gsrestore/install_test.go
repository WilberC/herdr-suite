package gsrestore

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

func testTarget(t *testing.T) Target {
	t.Helper()
	root := t.TempDir()
	agentDir := filepath.Join(root, "agent")
	extensions := filepath.Join(agentDir, "extensions")
	if err := os.MkdirAll(extensions, 0o755); err != nil {
		t.Fatal(err)
	}
	return Target{
		AgentDir:      agentDir,
		ExtensionsDir: extensions,
		Path:          filepath.Join(extensions, ReporterFileName),
	}
}

func reporterFixture() []byte {
	return []byte(ReporterVersionLine + "\n// shipped reporter body\n")
}

func backupsOf(t *testing.T, path string) []string {
	t.Helper()
	matches, err := filepath.Glob(path + ".bak-*")
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(matches)
	return matches
}

func TestInstallWritesTheReporter(t *testing.T) {
	target := testTarget(t)
	reporter := reporterFixture()

	result, err := Install(target, reporter, false, time.Date(2026, 11, 12, 20, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	want := InstallResult{Path: target.Path, Wrote: true}
	if result != want {
		t.Fatalf("result = %#v, want %#v", result, want)
	}

	data, err := os.ReadFile(target.Path)
	if err != nil {
		t.Fatalf("read the installed reporter: %v", err)
	}
	if string(data) != string(reporter) {
		t.Fatalf("installed reporter = %q, want the source bytes", data)
	}
	if backups := backupsOf(t, target.Path); len(backups) != 0 {
		t.Fatalf("backups = %v, want none for a fresh install", backups)
	}
}

func TestInstallIsIdempotentWhenTheFileIsAlreadyOurs(t *testing.T) {
	target := testTarget(t)
	reporter := reporterFixture()
	if err := os.WriteFile(target.Path, reporter, 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := Install(target, reporter, false, time.Date(2026, 11, 12, 20, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if !result.Already || result.Wrote {
		t.Fatalf("result = %#v, want Already with no write", result)
	}
	if backups := backupsOf(t, target.Path); len(backups) != 0 {
		t.Fatalf("backups = %v, want none when nothing changed", backups)
	}
}

func TestInstallRefusesSomeoneElsesFileWithoutForce(t *testing.T) {
	target := testTarget(t)
	foreign := []byte("console.log('another plugin');\n")
	if err := os.WriteFile(target.Path, foreign, 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Install(target, reporterFixture(), false, time.Date(2026, 11, 12, 20, 0, 0, 0, time.UTC))
	if err == nil {
		t.Fatal("Install overwrote a file that is not a herdr reporter without --force")
	}
	if !strings.Contains(err.Error(), "--force") {
		t.Fatalf("error = %q, want it to mention --force", err)
	}
	data, err := os.ReadFile(target.Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(foreign) {
		t.Fatalf("refused install changed the file: %q", data)
	}
	if backups := backupsOf(t, target.Path); len(backups) != 0 {
		t.Fatalf("backups = %v, want none when the install is refused", backups)
	}
}

func TestInstallBacksUpAndReplacesWithForce(t *testing.T) {
	target := testTarget(t)
	foreign := []byte("console.log('another plugin');\n")
	if err := os.WriteFile(target.Path, foreign, 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := Install(target, reporterFixture(), true, time.Date(2026, 11, 12, 20, 0, 1, 0, time.UTC))
	if err != nil {
		t.Fatalf("Install --force: %v", err)
	}
	wantBackup := target.Path + ".bak-20261112T200001"
	if result.Backup != wantBackup {
		t.Fatalf("Backup = %q, want %q", result.Backup, wantBackup)
	}
	backed, err := os.ReadFile(result.Backup)
	if err != nil {
		t.Fatalf("read the backup: %v", err)
	}
	if string(backed) != string(foreign) {
		t.Fatalf("backup = %q, want the replaced bytes", backed)
	}
	data, err := os.ReadFile(target.Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(reporterFixture()) {
		t.Fatalf("installed reporter = %q, want ours", data)
	}
}

func TestInstallBacksUpAReporterLookingFileWithoutForce(t *testing.T) {
	target := testTarget(t)
	vanilla := []byte("// HERDR_INTEGRATION_ID=pi\nfunction report() { method: \"pane.report_agent_session\" }\n")
	if err := os.WriteFile(target.Path, vanilla, 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := Install(target, reporterFixture(), false, time.Date(2026, 11, 12, 20, 0, 2, 0, time.UTC))
	if err != nil {
		t.Fatalf("Install over a herdr reporter: %v", err)
	}
	if result.Backup != target.Path+".bak-20261112T200002" {
		t.Fatalf("Backup = %q, want the vanilla integration backed up", result.Backup)
	}
}

func TestInstallKeepsASingleBackupOnRepeatInstalls(t *testing.T) {
	target := testTarget(t)
	first := []byte("// HERDR_INTEGRATION_ID=pi\nfirst vanilla integration\n")
	if err := os.WriteFile(target.Path, first, 0o644); err != nil {
		t.Fatal(err)
	}
	firstResult, err := Install(target, reporterFixture(), false, time.Date(2026, 11, 12, 20, 0, 3, 0, time.UTC))
	if err != nil {
		t.Fatalf("first Install: %v", err)
	}

	second := []byte("// HERDR_INTEGRATION_ID=pi\nsecond vanilla integration\n")
	if err := os.WriteFile(target.Path, second, 0o644); err != nil {
		t.Fatal(err)
	}
	secondResult, err := Install(target, reporterFixture(), false, time.Date(2026, 11, 12, 21, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("second Install: %v", err)
	}
	if secondResult.Backup != firstResult.Backup {
		t.Fatalf("second Backup = %q, want the existing single backup %q", secondResult.Backup, firstResult.Backup)
	}
	if backups := backupsOf(t, target.Path); len(backups) != 1 {
		t.Fatalf("backups = %v, want exactly one", backups)
	}
	backed, err := os.ReadFile(secondResult.Backup)
	if err != nil {
		t.Fatal(err)
	}
	if string(backed) != string(second) {
		t.Fatalf("backup = %q, want the most recent replaced bytes", backed)
	}
}

func TestInstallRefusesASourceWithoutTheVersionMarker(t *testing.T) {
	target := testTarget(t)
	_, err := Install(target, []byte("console.log('not a reporter');\n"), false, time.Now())
	if err == nil {
		t.Fatal("Install accepted a source without the version marker")
	}
	if !strings.Contains(err.Error(), "HERDR_SUITE_GS_RESTORE_VERSION") {
		t.Fatalf("error = %q, want it to name the missing marker", err)
	}
	if _, statErr := os.Stat(target.Path); statErr == nil {
		t.Fatal("Install wrote a reporter it rejected")
	}
}

func TestRemoveRestoresTheLatestBackup(t *testing.T) {
	target := testTarget(t)
	if err := os.WriteFile(target.Path, reporterFixture(), 0o644); err != nil {
		t.Fatal(err)
	}
	older := target.Path + ".bak-20260101T000000"
	newer := target.Path + ".bak-20260102T000000"
	if err := os.WriteFile(older, []byte("older\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newer, []byte("newer\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := Remove(target)
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if !result.Restored || result.Backup != newer {
		t.Fatalf("result = %#v, want a restore from %q", result, newer)
	}
	data, err := os.ReadFile(target.Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "newer\n" {
		t.Fatalf("restored file = %q, want the latest backup", data)
	}
}

func TestRemoveDeletesTheReporterWhenThereIsNoBackup(t *testing.T) {
	target := testTarget(t)
	if err := os.WriteFile(target.Path, reporterFixture(), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := Remove(target)
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if !result.Removed || result.Restored {
		t.Fatalf("result = %#v, want Removed with no restore", result)
	}
	if _, err := os.Stat(target.Path); !os.IsNotExist(err) {
		t.Fatalf("stat the removed file = %v, want it gone", err)
	}
}

func TestRemoveReportsNothingWhenNoReporterIsInstalled(t *testing.T) {
	target := testTarget(t)
	result, err := Remove(target)
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if result.Removed || result.Restored {
		t.Fatalf("result = %#v, want neither removed nor restored", result)
	}
}
