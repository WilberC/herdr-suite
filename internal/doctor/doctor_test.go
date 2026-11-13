package doctor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/WilberC/herdr-suite/internal/gsrestore"
	"github.com/WilberC/herdr-suite/internal/layout"
)

// harness is one installation the doctor reads: a config home, an agent
// directory, a plugin root with its manifest, and the herdr it talks to. Every
// piece is a file under one temp directory and every call is a function, so a
// test can break exactly one thing.
type harness struct {
	t           *testing.T
	root        string
	cwd         string
	herdrPath   string
	env         map[string]string
	herdrOut    string
	herdrErr    error
	sessions    int
	pingErr     error
	pingBlocks  bool
	gentleShell string
	d           Deps
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	root := t.TempDir()
	h := &harness{
		t:         t,
		root:      root,
		cwd:       filepath.Join(root, "work"),
		herdrPath: filepath.Join(root, "bin", "herdr"),
		sessions:  3,
		env: map[string]string{
			"XDG_CONFIG_HOME":     filepath.Join(root, "config"),
			"PI_CODING_AGENT_DIR": filepath.Join(root, "agent"),
		},
	}
	h.mkdirs(h.cwd, filepath.Join(h.env["PI_CODING_AGENT_DIR"], "extensions"))
	h.writeManifest("0.2.0", "0.7.5")
	h.herdrOut = "herdr 0.9.5\n"

	h.d = Deps{
		Version:    "0.2.0",
		Executable: filepath.Join(root, "bin", "hs"),
		Cwd:        h.cwd,
		Getenv:     func(name string) string { return h.env[name] },
		LookPath: func(name string) (string, error) {
			if name == "gentle-shell" {
				if h.gentleShell == "" {
					return "", exec.ErrNotFound
				}
				return h.gentleShell, nil
			}
			return h.herdrPath, nil
		},
		RunHerdr: func(ctx context.Context, bin string, args ...string) ([]byte, []byte, error) {
			if h.herdrErr != nil {
				return nil, nil, h.herdrErr
			}
			return []byte(h.herdrOut), nil, nil
		},
		Ping: func(ctx context.Context) (int, error) {
			if h.pingBlocks {
				<-ctx.Done()
				return 0, ctx.Err()
			}
			if h.pingErr != nil {
				return 0, h.pingErr
			}
			return h.sessions, nil
		},
		Timeout: 50 * time.Millisecond,
	}
	return h
}

func (h *harness) mkdirs(paths ...string) {
	h.t.Helper()
	for _, path := range paths {
		if err := os.MkdirAll(path, 0o755); err != nil {
			h.t.Fatalf("create %s: %v", path, err)
		}
	}
}

func (h *harness) manifestPath() string { return filepath.Join(h.root, "herdr-plugin.toml") }

func (h *harness) writeManifest(version, minHerdr string) {
	h.t.Helper()
	body := "id = \"herdr-suite\"\nname = \"Herd Suite\"\nversion = \"" + version + "\"\nmin_herdr_version = \"" + minHerdr + "\"\n"
	h.writeFile(h.manifestPath(), body)
}

func (h *harness) configPath() string {
	return filepath.Join(h.env["XDG_CONFIG_HOME"], "herdr", "plugins", "config", "herdr-suite", "config.json")
}

func (h *harness) writeConfig(path, body string) {
	h.t.Helper()
	h.mkdirs(filepath.Dir(path))
	h.writeFile(path, body)
}

func (h *harness) globalDir() string {
	h.t.Helper()
	dir, err := layout.Dir(h.d.Getenv)
	if err != nil {
		h.t.Fatalf("resolve the layout directory: %v", err)
	}
	return dir
}

func (h *harness) writeLayout(dir, name string) {
	h.t.Helper()
	h.mkdirs(dir)
	body := "{\n  \"schema_version\": 1,\n  \"name\": \"" + name + "\",\n  \"workspace\": {\n    \"label\": \"project\",\n    \"tabs\": []\n  }\n}\n"
	h.writeFile(filepath.Join(dir, name+".json"), body)
}

func (h *harness) reporterPath() string {
	return filepath.Join(h.env["PI_CODING_AGENT_DIR"], "extensions", gsrestore.ReporterFileName)
}

func (h *harness) installReporter(marker string) {
	h.t.Helper()
	h.writeFile(h.reporterPath(), "// HERDR_SUITE_GS_RESTORE_VERSION="+marker+"\n// reporter body\n")
}

func (h *harness) writeFile(path, body string) {
	h.t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		h.t.Fatalf("write %s: %v", path, err)
	}
}

func (h *harness) check(report Report, id string) Check {
	h.t.Helper()
	check, ok := report.Check(id)
	if !ok {
		h.t.Fatalf("the report holds no %q check; ids are %v", id, checkIDs(report))
	}
	return check
}

func (h *harness) assertCheck(report Report, id string, status Status, detail string) {
	h.t.Helper()
	check := h.check(report, id)
	if check.Status != status {
		h.t.Errorf("%s status = %s, want %s (detail: %s)", id, check.Status, status, check.Detail)
	}
	if check.Detail != detail {
		h.t.Errorf("%s detail = %q, want %q", id, check.Detail, detail)
	}
}

func checkIDs(report Report) []string {
	ids := make([]string, 0, len(report.Checks))
	for _, check := range report.Checks {
		ids = append(ids, check.ID)
	}
	return ids
}

// TestDoctorReportsAHealthyInstallation pins every line of a clean report, in
// order: the wording is the contract scripts and humans read.
func TestDoctorReportsAHealthyInstallation(t *testing.T) {
	h := newHarness(t)

	report := Run(h.d)

	if report.ExitCode() != 0 {
		t.Fatalf("exit = %d, want 0; report:\n%s", report.ExitCode(), render(t, report))
	}
	wantIDs := []string{
		"herdr", "socket", "config", "binary", "layouts", "project",
		"gs-restore reporter", "gs-restore version", "gs-restore backup", "gs-restore launcher",
		"manifest",
	}
	if got := checkIDs(report); strings.Join(got, ",") != strings.Join(wantIDs, ",") {
		t.Fatalf("report checks = %v, want %v", got, wantIDs)
	}

	h.assertCheck(report, "herdr", Pass, "herdr 0.9.5 at "+h.herdrPath+" (min_herdr_version 0.7.5)")
	h.assertCheck(report, "socket", Pass, "reachable · 3 sessions")
	h.assertCheck(report, "config", Pass, h.configPath()+" (missing) · gs_restore install off, auto_install off")
	h.assertCheck(report, "binary", Pass, "0.2.0 matches herdr-plugin.toml")
	h.assertCheck(report, "layouts", Pass, h.globalDir()+" · 0 layouts")
	h.assertCheck(report, "project", Pass, "no .herdr declaration above "+h.cwd)
	h.assertCheck(report, "gs-restore reporter", Skip, "not installed (feature default off)")
	h.assertCheck(report, "gs-restore version", Skip, "no reporter installed")
	h.assertCheck(report, "gs-restore backup", Pass, "none")
	h.assertCheck(report, "gs-restore launcher", Skip, "not checked; no reporter installed")
	h.assertCheck(report, "manifest", Pass, h.manifestPath()+" · Herd Suite 0.2.0 (min_herdr_version 0.7.5)")
}

func TestDoctorCountsTheLayoutStore(t *testing.T) {
	h := newHarness(t)
	h.writeLayout(h.globalDir(), "work")
	h.writeLayout(h.globalDir(), "api")

	report := Run(h.d)

	h.assertCheck(report, "layouts", Pass, h.globalDir()+" · 2 layouts")
}

func TestDoctorSummarizesProjectDiscovery(t *testing.T) {
	h := newHarness(t)
	h.writeLayout(filepath.Join(h.cwd, ".herdr", "layouts"), "api")

	report := Run(h.d)

	h.assertCheck(report, "project", Pass, "project "+h.cwd+" · 1 layout")
}

func TestDoctorFailsOnALayoutStoreItCannotRead(t *testing.T) {
	h := newHarness(t)
	h.writeLayout(h.globalDir(), "work")
	h.writeFile(filepath.Join(h.globalDir(), "future.json"),
		"{\n  \"schema_version\": 2,\n  \"name\": \"future\",\n  \"workspace\": {\"label\": \"project\", \"tabs\": []}\n}\n")

	report := Run(h.d)

	check := h.check(report, "layouts")
	if check.Status != Fail {
		t.Fatalf("layouts status = %s, want FAIL", check.Status)
	}
	for _, want := range []string{"newer herdr-suite", "update hs"} {
		if !strings.Contains(check.Detail, want) {
			t.Errorf("layouts detail = %q, want it to contain %q", check.Detail, want)
		}
	}
	if report.ExitCode() != 1 {
		t.Fatalf("exit = %d, want 1", report.ExitCode())
	}
}

func TestDoctorFailsWhenHerdrIsMissing(t *testing.T) {
	h := newHarness(t)
	h.d.LookPath = func(string) (string, error) { return "", exec.ErrNotFound }

	report := Run(h.d)

	check := h.check(report, "herdr")
	if check.Status != Fail {
		t.Fatalf("herdr status = %s, want FAIL", check.Status)
	}
	if !strings.Contains(check.Detail, "HERDR_BIN_PATH") {
		t.Fatalf("herdr detail = %q, want the fix clause naming HERDR_BIN_PATH", check.Detail)
	}
	if report.ExitCode() != 1 {
		t.Fatalf("exit = %d, want 1", report.ExitCode())
	}
}

func TestDoctorHonoursTheHerdrBinaryOverride(t *testing.T) {
	h := newHarness(t)
	override := filepath.Join(h.root, "custom-herdr")
	h.env["HERDR_BIN_PATH"] = override
	h.d.LookPath = func(string) (string, error) { return "", exec.ErrNotFound }

	report := Run(h.d)

	h.assertCheck(report, "herdr", Pass, "herdr 0.9.5 at "+override+" (min_herdr_version 0.7.5)")
}

func TestDoctorFailsWhenHerdrDoesNotRun(t *testing.T) {
	h := newHarness(t)
	h.herdrErr = errors.New("exit status 127")

	report := Run(h.d)

	check := h.check(report, "herdr")
	if check.Status != Fail {
		t.Fatalf("herdr status = %s, want FAIL", check.Status)
	}
	if !strings.Contains(check.Detail, "cannot run") || !strings.Contains(check.Detail, h.herdrPath) {
		t.Fatalf("herdr detail = %q, want it to name the binary it could not run", check.Detail)
	}
}

func TestDoctorFailsWhenHerdrReportsNoVersion(t *testing.T) {
	h := newHarness(t)
	h.herdrOut = "herdr (development build)\n"

	report := Run(h.d)

	check := h.check(report, "herdr")
	if check.Status != Fail {
		t.Fatalf("herdr status = %s, want FAIL", check.Status)
	}
	if !strings.Contains(check.Detail, "reported no version") {
		t.Fatalf("herdr detail = %q, want it to say no version was found", check.Detail)
	}
}

func TestDoctorFailsBelowTheManifestFloor(t *testing.T) {
	h := newHarness(t)
	h.herdrOut = "herdr 0.7.4\n"

	report := Run(h.d)

	h.assertCheck(report, "herdr", Fail, "herdr 0.7.4 is older than min_herdr_version 0.7.5; update herdr")
	if report.ExitCode() != 1 {
		t.Fatalf("exit = %d, want 1", report.ExitCode())
	}
}

func TestDoctorFailsWhenTheSocketIsUnreachable(t *testing.T) {
	h := newHarness(t)
	h.pingErr = errors.New("dial unix /tmp/herdr.sock: connect: connection refused")

	report := Run(h.d)

	check := h.check(report, "socket")
	if check.Status != Fail {
		t.Fatalf("socket status = %s, want FAIL", check.Status)
	}
	for _, want := range []string{"unreachable", "connection refused", "HERDR_SOCKET_PATH"} {
		if !strings.Contains(check.Detail, want) {
			t.Errorf("socket detail = %q, want it to contain %q", check.Detail, want)
		}
	}
}

// TestDoctorDoesNotBlockOnAHungSocket pins the timeout: a session listed that
// never answers becomes a FAIL line instead of a hanging report.
func TestDoctorDoesNotBlockOnAHungSocket(t *testing.T) {
	h := newHarness(t)
	h.pingBlocks = true

	report := Run(h.d)

	h.assertCheck(report, "socket", Fail, "no answer within 50ms; start a herdr session, or check HERDR_SOCKET_PATH")
	if report.ExitCode() != 1 {
		t.Fatalf("exit = %d, want 1", report.ExitCode())
	}
}

func TestDoctorReportsTheConfigToggles(t *testing.T) {
	h := newHarness(t)
	h.writeConfig(h.configPath(), `{"gs_restore": {"install": true, "auto_install": false}}`)

	report := Run(h.d)

	h.assertCheck(report, "config", Pass, h.configPath()+" · gs_restore install on, auto_install off")
}

// TestDoctorReadsTheConfigThePluginDirOverrideNames pins that the check follows
// the same override the plugin does, instead of only the XDG default.
func TestDoctorReadsTheConfigThePluginDirOverrideNames(t *testing.T) {
	h := newHarness(t)
	dir := filepath.Join(h.root, "plugin-config")
	h.env["HERDR_PLUGIN_CONFIG_DIR"] = dir
	h.writeConfig(filepath.Join(dir, "config.json"), `{"gs_restore": {"install": false, "auto_install": true}}`)

	report := Run(h.d)

	h.assertCheck(report, "config", Pass, filepath.Join(dir, "config.json")+" · gs_restore install off, auto_install on")
}

func TestDoctorFailsOnAConfigItCannotParse(t *testing.T) {
	h := newHarness(t)
	h.writeConfig(h.configPath(), `{"gs_restore": "on"}`)

	report := Run(h.d)

	check := h.check(report, "config")
	if check.Status != Fail {
		t.Fatalf("config status = %s, want FAIL", check.Status)
	}
	for _, want := range []string{"invalid", h.configPath(), "fix or delete"} {
		if !strings.Contains(check.Detail, want) {
			t.Errorf("config detail = %q, want it to contain %q", check.Detail, want)
		}
	}
}

func TestDoctorSkipsTheStaleCheckOnADevBuild(t *testing.T) {
	h := newHarness(t)
	h.d.Version = "dev"

	report := Run(h.d)

	h.assertCheck(report, "binary", Skip, "dev build; no release version embedded")
	if report.ExitCode() != 0 {
		t.Fatalf("exit = %d, want 0 for a dev tree", report.ExitCode())
	}
}

func TestDoctorFailsOnAStaleBinary(t *testing.T) {
	h := newHarness(t)
	h.d.Version = "0.1.0"

	report := Run(h.d)

	h.assertCheck(report, "binary", Fail, "stale binary: embedded 0.1.0, manifest 0.2.0; run make hs or reinstall the plugin")
	if report.ExitCode() != 1 {
		t.Fatalf("exit = %d, want 1", report.ExitCode())
	}
}

func TestDoctorFailsOnAManifestItCannotParse(t *testing.T) {
	h := newHarness(t)
	h.writeFile(h.manifestPath(), "id = \"herdr-suite\"\nname = \"Herd Suite\"\nmin_herdr_version = \"0.7.5\"\n")

	report := Run(h.d)

	check := h.check(report, "manifest")
	if check.Status != Fail {
		t.Fatalf("manifest status = %s, want FAIL", check.Status)
	}
	for _, want := range []string{h.manifestPath(), "version", "--manifest"} {
		if !strings.Contains(check.Detail, want) {
			t.Errorf("manifest detail = %q, want it to contain %q", check.Detail, want)
		}
	}
	h.assertCheck(report, "binary", Skip, "no manifest version to compare against")
	h.assertCheck(report, "herdr", Pass, "herdr 0.9.5 at "+h.herdrPath+" (min_herdr_version unknown)")
}

func TestDoctorFailsWithoutAManifest(t *testing.T) {
	h := newHarness(t)
	if err := os.Remove(h.manifestPath()); err != nil {
		t.Fatal(err)
	}

	report := Run(h.d)

	check := h.check(report, "manifest")
	if check.Status != Fail {
		t.Fatalf("manifest status = %s, want FAIL", check.Status)
	}
	if !strings.Contains(check.Detail, "no herdr-plugin.toml") {
		t.Fatalf("manifest detail = %q, want it to name the missing file", check.Detail)
	}
	if report.ExitCode() != 1 {
		t.Fatalf("exit = %d, want 1", report.ExitCode())
	}
}

func TestDoctorHonoursAnExplicitManifest(t *testing.T) {
	h := newHarness(t)
	explicit := filepath.Join(h.root, "elsewhere", "plugin.toml")
	h.mkdirs(filepath.Dir(explicit))
	h.writeFile(explicit, "id = \"herdr-suite\"\nname = \"Herd Suite\"\nversion = \"0.2.0\"\nmin_herdr_version = \"0.7.5\"\n")
	h.d.ManifestPath = explicit

	report := Run(h.d)

	h.assertCheck(report, "manifest", Pass, explicit+" · Herd Suite 0.2.0 (min_herdr_version 0.7.5)")
}

func TestDoctorReportsTheInstalledGSRestore(t *testing.T) {
	h := newHarness(t)
	h.installReporter("1")
	h.writeFile(h.reporterPath()+".bak-20261112", "// previous integration\n")
	h.gentleShell = filepath.Join(h.root, "bin", "gentle-shell")

	report := Run(h.d)

	h.assertCheck(report, "gs-restore reporter", Pass, "installed at "+h.reporterPath())
	h.assertCheck(report, "gs-restore version", Pass, "matches (v1)")
	h.assertCheck(report, "gs-restore backup", Pass, "present at "+h.reporterPath()+".bak-20261112")
	h.assertCheck(report, "gs-restore launcher", Pass, "gentle-shell at "+h.gentleShell)
	if report.ExitCode() != 0 {
		t.Fatalf("exit = %d, want 0", report.ExitCode())
	}
}

func TestDoctorWarnsOnAReporterFromAnotherBuild(t *testing.T) {
	h := newHarness(t)
	h.installReporter("2")
	h.gentleShell = filepath.Join(h.root, "bin", "gentle-shell")

	report := Run(h.d)

	h.assertCheck(report, "gs-restore version", Warn, "differs (installed v2, shipped v1); run hs gs-restore install")
	if report.ExitCode() != 0 {
		t.Fatalf("exit = %d, want 0: a warning is not a failure", report.ExitCode())
	}
}

func TestDoctorWarnsWhenTheLauncherIsNotResolvable(t *testing.T) {
	h := newHarness(t)
	h.installReporter("1")

	report := Run(h.d)

	h.assertCheck(report, "gs-restore launcher", Warn,
		"gentle-shell is not on PATH; the installed reporter would report a launcher herdr cannot run")
	if report.ExitCode() != 0 {
		t.Fatalf("exit = %d, want 0: a warning is not a failure", report.ExitCode())
	}
}

func TestDoctorSkipsGSRestoreWithoutAnAgentDirectory(t *testing.T) {
	h := newHarness(t)
	delete(h.env, "PI_CODING_AGENT_DIR")

	report := Run(h.d)

	for _, id := range []string{"gs-restore reporter", "gs-restore version", "gs-restore backup", "gs-restore launcher"} {
		check := h.check(report, id)
		if check.Status != Skip {
			t.Errorf("%s status = %s, want SKIP", id, check.Status)
		}
		if !strings.Contains(check.Detail, "PI_CODING_AGENT_DIR") {
			t.Errorf("%s detail = %q, want it to name the unset variable", id, check.Detail)
		}
	}
}

func TestDoctorJSONReportsTheWholeReport(t *testing.T) {
	h := newHarness(t)
	h.d.Version = "0.1.0"

	var out bytes.Buffer
	if err := WriteJSON(&out, Run(h.d)); err != nil {
		t.Fatalf("WriteJSON error = %v", err)
	}
	var document struct {
		Version string `json:"version"`
		OK      bool   `json:"ok"`
		Checks  []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
			Detail string `json:"detail"`
		} `json:"checks"`
	}
	if err := json.Unmarshal(out.Bytes(), &document); err != nil {
		t.Fatalf("the report is not JSON: %v\n%s", err, out.String())
	}
	if document.Version != "0.1.0" {
		t.Errorf("json version = %q, want %q", document.Version, "0.1.0")
	}
	if document.OK {
		t.Error("json ok = true, want false when a line failed")
	}
	if len(document.Checks) != len(Run(h.d).Checks) {
		t.Fatalf("json holds %d checks, want %d", len(document.Checks), len(Run(h.d).Checks))
	}
	found := false
	for _, check := range document.Checks {
		if check.ID == "binary" {
			found = true
			if check.Status != "FAIL" {
				t.Errorf("json binary status = %q, want FAIL", check.Status)
			}
			if check.Detail == "" {
				t.Error("json binary detail is empty")
			}
		}
	}
	if !found {
		t.Fatal("the json report holds no binary check")
	}
}

// TestWritePrintsStableColumns pins the text shape: a four-column status, the
// check id, and its detail.
func TestWritePrintsStableColumns(t *testing.T) {
	report := Report{Version: "0.2.0", Checks: []Check{
		{ID: "herdr", Status: Pass, Detail: "herdr 0.9.5 at /usr/bin/herdr"},
		{ID: "socket", Status: Fail, Detail: "unreachable; start a herdr session"},
	}}

	var out bytes.Buffer
	if err := Write(&out, report); err != nil {
		t.Fatalf("Write error = %v", err)
	}
	want := "PASS  herdr   herdr 0.9.5 at /usr/bin/herdr\n" +
		"FAIL  socket  unreachable; start a herdr session\n"
	if out.String() != want {
		t.Fatalf("report =\n%s\nwant\n%s", out.String(), want)
	}
}

// TestDoctorWritesNothing pins that the doctor is read-only: it reports, and
// never creates, rewrites or restores a file.
func TestDoctorWritesNothing(t *testing.T) {
	h := newHarness(t)
	h.writeLayout(h.globalDir(), "work")
	h.writeConfig(h.configPath(), `{"gs_restore": {"install": true}}`)
	h.installReporter("2")
	before := treeState(t, h.root)

	report := Run(h.d)
	var out bytes.Buffer
	if err := Write(&out, report); err != nil {
		t.Fatalf("Write error = %v", err)
	}

	if after := treeState(t, h.root); after != before {
		t.Fatalf("the doctor changed the tree:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// treeState renders every file under root with its size, so a read-only report
// cannot pass by leaving an edited file the same length.
func treeState(t *testing.T, root string) string {
	t.Helper()
	var state strings.Builder
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		data := ""
		if !info.IsDir() {
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			data = string(content)
		}
		state.WriteString(path + "|" + info.Mode().String() + "|" + data + "\n")
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return state.String()
}

func render(t *testing.T, report Report) string {
	t.Helper()
	var out bytes.Buffer
	if err := Write(&out, report); err != nil {
		t.Fatalf("Write error = %v", err)
	}
	return out.String()
}
