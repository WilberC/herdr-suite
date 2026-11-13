package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/WilberC/herdr-suite/internal/herdrmock"
)

type doctorRun struct {
	d              deps
	root           string
	stdout, stderr bytes.Buffer
}

func newDoctorRun(t *testing.T) *doctorRun {
	t.Helper()
	root := t.TempDir()
	run := &doctorRun{root: root}
	cwd := filepath.Join(root, "work")
	agentDir := filepath.Join(root, "agent")
	for _, dir := range []string{cwd, filepath.Join(agentDir, "extensions")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("create %s: %v", dir, err)
		}
	}
	run.writeManifest(t, "0.2.0")

	env := map[string]string{
		"XDG_CONFIG_HOME":     filepath.Join(root, "config"),
		"PI_CODING_AGENT_DIR": agentDir,
	}
	run.d = deps{
		client:     &herdrmock.Fake{Sessions: []herdrmock.Session{{Name: "default", Running: true}, {Name: "work", Running: true}}},
		version:    "0.2.0",
		getenv:     func(name string) string { return env[name] },
		cwd:        func() (string, error) { return cwd, nil },
		executable: func() (string, error) { return filepath.Join(root, "bin", "hs"), nil },
		lookPath:   func(name string) (string, error) { return filepath.Join(root, "bin", name), nil },
		herdrRun: func(context.Context, string, ...string) ([]byte, []byte, error) {
			return []byte("herdr 0.9.5\n"), nil, nil
		},
		socketTimeout: 50 * time.Millisecond,
		out:           &run.stdout,
		err:           &run.stderr,
	}
	return run
}

func (r *doctorRun) manifestPath() string { return filepath.Join(r.root, "herdr-plugin.toml") }

func (r *doctorRun) writeManifest(t *testing.T, version string) {
	t.Helper()
	body := "id = \"herdr-suite\"\nname = \"Herd Suite\"\nversion = \"" + version + "\"\nmin_herdr_version = \"0.7.5\"\n"
	if err := os.WriteFile(r.manifestPath(), []byte(body), 0o644); err != nil {
		t.Fatalf("write the manifest: %v", err)
	}
}

func (r *doctorRun) run(t *testing.T, args ...string) int {
	t.Helper()
	return runWith(r.d, args, &r.stdout, &r.stderr)
}

func TestDoctorPrintsTheReport(t *testing.T) {
	run := newDoctorRun(t)

	code := run.run(t, "doctor")

	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstdout:\n%s\nstderr:\n%s", code, run.stdout.String(), run.stderr.String())
	}
	for _, want := range []string{"PASS  herdr", "PASS  socket", "PASS  binary", "PASS  manifest"} {
		if !strings.Contains(run.stdout.String(), want) {
			t.Errorf("stdout does not contain %q; got:\n%s", want, run.stdout.String())
		}
	}
	if run.stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", run.stderr.String())
	}
}

func TestDoctorPrintsJSON(t *testing.T) {
	run := newDoctorRun(t)

	if code := run.run(t, "doctor", "--json"); code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", code, run.stderr.String())
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
	if err := json.Unmarshal(run.stdout.Bytes(), &document); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, run.stdout.String())
	}
	if document.Version != "0.2.0" || !document.OK {
		t.Fatalf("report = %#v, want the build version and ok true", document)
	}
	if len(document.Checks) == 0 {
		t.Fatal("the json report holds no checks")
	}
	for _, check := range document.Checks {
		if check.ID == "" || check.Status == "" || check.Detail == "" {
			t.Fatalf("check %#v, want an id, a status and a detail", check)
		}
	}
}

func TestDoctorExitsOneWhenALineFails(t *testing.T) {
	run := newDoctorRun(t)
	run.writeManifest(t, "0.3.0")

	code := run.run(t, "doctor")

	if code != 1 {
		t.Fatalf("exit = %d, want 1\nstdout:\n%s", code, run.stdout.String())
	}
	if !strings.Contains(run.stdout.String(), "FAIL") {
		t.Fatalf("stdout = %q, want a FAIL line", run.stdout.String())
	}
	if !strings.Contains(run.stdout.String(), "stale binary") {
		t.Fatalf("stdout = %q, want the stale binary explained", run.stdout.String())
	}
}

func TestDoctorExitsZeroWhenItOnlyWarns(t *testing.T) {
	run := newDoctorRun(t)
	run.d.version = "dev"

	if code := run.run(t, "doctor"); code != 0 {
		t.Fatalf("exit = %d, want 0 for a dev tree\nstdout:\n%s", code, run.stdout.String())
	}
	if !strings.Contains(run.stdout.String(), "SKIP") {
		t.Fatalf("stdout = %q, want the stale check skipped", run.stdout.String())
	}
}

// slowClient never answers a session list: it waits for the context the doctor
// gives it. A doctor that did not bound the probe would hang here.
type slowClient struct{}

func (slowClient) ListSessions(ctx context.Context) ([]herdrmock.Session, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (slowClient) StopSession(context.Context, string) error { return nil }

func (slowClient) DeleteSession(context.Context, string) error { return nil }

func TestDoctorDoesNotHangOnAnUnresponsiveSocket(t *testing.T) {
	run := newDoctorRun(t)
	run.d.client = slowClient{}
	run.d.socketTimeout = 20 * time.Millisecond

	done := make(chan int, 1)
	go func() { done <- run.run(t, "doctor") }()

	select {
	case code := <-done:
		if code != 1 {
			t.Fatalf("exit = %d, want 1\nstdout:\n%s", code, run.stdout.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("hs doctor did not return: the socket probe is not bounded")
	}
	if !strings.Contains(run.stdout.String(), "no answer within") {
		t.Fatalf("stdout = %q, want the timeout reported", run.stdout.String())
	}
}

func TestDoctorRejectsAnUnknownFlag(t *testing.T) {
	run := newDoctorRun(t)

	if code := run.run(t, "doctor", "--bogus"); code != 2 {
		t.Fatalf("exit = %d, want 2\nstdout:\n%s", code, run.stdout.String())
	}
	if !strings.Contains(run.stderr.String(), `unknown flag "--bogus"`) {
		t.Fatalf("stderr = %q, want the unknown flag named", run.stderr.String())
	}
	if !strings.Contains(run.stderr.String(), "usage: hs doctor") {
		t.Fatalf("stderr = %q, want the usage printed", run.stderr.String())
	}
}

func TestDoctorHonoursTheManifestFlag(t *testing.T) {
	run := newDoctorRun(t)
	run.writeManifest(t, "0.3.0")
	explicit := filepath.Join(t.TempDir(), "plugin.toml")
	body := "id = \"herdr-suite\"\nname = \"Herd Suite\"\nversion = \"0.2.0\"\nmin_herdr_version = \"0.7.5\"\n"
	if err := os.WriteFile(explicit, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	code := run.run(t, "doctor", "--manifest", explicit)

	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstdout:\n%s\nstderr:\n%s", code, run.stdout.String(), run.stderr.String())
	}
	if !strings.Contains(run.stdout.String(), "PASS  binary") {
		t.Fatalf("stdout = %q, want the explicit manifest compared", run.stdout.String())
	}
}

func TestDoctorNeedsAPathAfterManifest(t *testing.T) {
	run := newDoctorRun(t)

	if code := run.run(t, "doctor", "--manifest"); code != 2 {
		t.Fatalf("exit = %d, want 2\nstdout:\n%s", code, run.stdout.String())
	}
	if !strings.Contains(run.stderr.String(), "--manifest needs a path") {
		t.Fatalf("stderr = %q, want the missing value reported", run.stderr.String())
	}
}

// TestDoctorAndVersionAreDispatched pins that both commands are real
// subcommands, with usage a caller can read.
func TestDoctorAndVersionAreDispatched(t *testing.T) {
	for _, name := range []string{"doctor", "version"} {
		cmd, ok := lookup(name)
		if !ok {
			t.Fatalf("lookup(%q) = not found, want the command dispatched", name)
		}
		if !strings.Contains(cmd.usage, "usage: hs "+name) {
			t.Errorf("%s usage = %q, want it to start with its own usage line", name, cmd.usage)
		}
		if !strings.Contains(usage, name) {
			t.Errorf("the top-level usage does not name %q", name)
		}
	}
}

func TestDoctorIsNotFoundOnTheHerdrSurface(t *testing.T) {
	run := newDoctorRun(t)
	run.d.lookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	run.d.getenv = func(name string) string {
		if name == "HERDR_BIN_PATH" {
			return ""
		}
		return ""
	}

	if code := run.run(t, "doctor"); code != 1 {
		t.Fatalf("exit = %d, want 1 when herdr cannot be found\nstdout:\n%s", code, run.stdout.String())
	}
	if !strings.Contains(run.stdout.String(), "HERDR_BIN_PATH") {
		t.Fatalf("stdout = %q, want the fix clause naming HERDR_BIN_PATH", run.stdout.String())
	}
}
