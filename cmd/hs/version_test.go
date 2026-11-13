package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type versionRun struct {
	d              deps
	root           string
	stdout, stderr bytes.Buffer
}

func newVersionRun(t *testing.T, buildVersion string) *versionRun {
	t.Helper()
	root := t.TempDir()
	run := &versionRun{root: root}
	run.d = deps{
		version:    buildVersion,
		getenv:     func(string) string { return "" },
		cwd:        func() (string, error) { return root, nil },
		executable: func() (string, error) { return filepath.Join(root, "bin", "hs"), nil },
		out:        &run.stdout,
		err:        &run.stderr,
	}
	return run
}

func (r *versionRun) writeManifest(t *testing.T, version string) string {
	t.Helper()
	path := filepath.Join(r.root, "herdr-plugin.toml")
	body := `id = "herdr-suite"
name = "Herd Suite"
version = "` + version + `"
min_herdr_version = "0.7.5"
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write the manifest: %v", err)
	}
	return path
}

func (r *versionRun) run(t *testing.T, args ...string) int {
	t.Helper()
	return runWith(r.d, args, &r.stdout, &r.stderr)
}

// TestBuildVersionDefaultsToDev pins the build contract: a working tree reports
// "dev", and only a release build passes -X main.version=<tag>.
func TestBuildVersionDefaultsToDev(t *testing.T) {
	if version != "dev" {
		t.Fatalf("the build version defaults to %q, want %q; only the release pipeline sets it", version, "dev")
	}
}

func TestVersionPrintsTheEmbeddedVersion(t *testing.T) {
	run := newVersionRun(t, "0.2.0")

	if code := run.run(t, "version"); code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", code, run.stderr.String())
	}
	if got, want := run.stdout.String(), "hs version 0.2.0\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
	if run.stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", run.stderr.String())
	}
}

func TestVersionPrintsJSON(t *testing.T) {
	run := newVersionRun(t, "dev")

	if code := run.run(t, "version", "--json"); code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", code, run.stderr.String())
	}
	var document struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(run.stdout.Bytes(), &document); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, run.stdout.String())
	}
	if document.Version != "dev" {
		t.Fatalf("json version = %q, want %q", document.Version, "dev")
	}
	if lines := strings.Count(run.stdout.String(), "\n"); lines != 1 {
		t.Fatalf("stdout holds %d lines, want one JSON document:\n%s", lines, run.stdout.String())
	}
}

func TestVersionRejectsAnUnknownFlag(t *testing.T) {
	run := newVersionRun(t, "0.2.0")

	if code := run.run(t, "version", "--bogus"); code != 2 {
		t.Fatalf("exit = %d, want 2\nstdout:\n%s", code, run.stdout.String())
	}
	if !strings.Contains(run.stderr.String(), `unknown flag "--bogus"`) {
		t.Fatalf("stderr = %q, want the unknown flag named", run.stderr.String())
	}
	if !strings.Contains(run.stderr.String(), "usage: hs version") {
		t.Fatalf("stderr = %q, want the usage printed", run.stderr.String())
	}
}

// TestVersionCheckMatrix pins the stale-binary contract: 0 when the embedded
// version matches the manifest, 1 when it does not, and always 0 for a dev
// build, which carries no release version to compare.
func TestVersionCheckMatrix(t *testing.T) {
	tests := []struct {
		name       string
		build      string
		manifest   string
		writeIt    bool
		wantExit   int
		wantStdout []string
		wantStderr []string
	}{
		{
			name:       "a matching release version",
			build:      "0.2.0",
			manifest:   "0.2.0",
			writeIt:    true,
			wantExit:   0,
			wantStdout: []string{"hs version 0.2.0", "manifest 0.2.0", "ok"},
		},
		{
			name:       "a stale binary",
			build:      "0.1.0",
			manifest:   "0.2.0",
			writeIt:    true,
			wantExit:   1,
			wantStdout: []string{"hs version 0.1.0", "manifest 0.2.0", "stale", "run make hs or reinstall the plugin"},
		},
		{
			name:       "a dev build never fails",
			build:      "dev",
			writeIt:    false,
			wantExit:   0,
			wantStdout: []string{"hs version dev", "dev build; no release version embedded"},
		},
		{
			name:       "a release build without a manifest",
			build:      "0.2.0",
			writeIt:    false,
			wantExit:   1,
			wantStderr: []string{"no herdr-plugin.toml", "pass --manifest"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			run := newVersionRun(t, tt.build)
			if tt.writeIt {
				run.writeManifest(t, tt.manifest)
			}

			code := run.run(t, "version", "--check")

			if code != tt.wantExit {
				t.Fatalf("exit = %d, want %d\nstdout:\n%s\nstderr:\n%s", code, tt.wantExit, run.stdout.String(), run.stderr.String())
			}
			for _, want := range tt.wantStdout {
				if !strings.Contains(run.stdout.String(), want) {
					t.Errorf("stdout does not contain %q; got:\n%s", want, run.stdout.String())
				}
			}
			for _, want := range tt.wantStderr {
				if !strings.Contains(run.stderr.String(), want) {
					t.Errorf("stderr does not contain %q; got:\n%s", want, run.stderr.String())
				}
			}
			if tt.wantExit == 0 && run.stderr.Len() != 0 {
				t.Errorf("stderr = %q, want empty", run.stderr.String())
			}
		})
	}
}

func TestVersionCheckReadsAnExplicitManifest(t *testing.T) {
	run := newVersionRun(t, "0.2.0")
	explicit := filepath.Join(t.TempDir(), "other.toml")
	body := "id = \"herdr-suite\"\nname = \"Herd Suite\"\nversion = \"0.2.0\"\nmin_herdr_version = \"0.7.5\"\n"
	if err := os.WriteFile(explicit, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	code := run.run(t, "version", "--check", "--manifest", explicit)

	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstdout:\n%s\nstderr:\n%s", code, run.stdout.String(), run.stderr.String())
	}
	if !strings.Contains(run.stdout.String(), "manifest 0.2.0") {
		t.Fatalf("stdout = %q, want the explicit manifest's version", run.stdout.String())
	}
}

func TestVersionCheckPrintsJSON(t *testing.T) {
	run := newVersionRun(t, "0.1.0")
	run.writeManifest(t, "0.2.0")

	code := run.run(t, "version", "--check", "--json")

	if code != 1 {
		t.Fatalf("exit = %d, want 1\nstdout:\n%s", code, run.stdout.String())
	}
	var document struct {
		Version  string `json:"version"`
		Manifest string `json:"manifest"`
		OK       bool   `json:"ok"`
		Detail   string `json:"detail"`
	}
	if err := json.Unmarshal(run.stdout.Bytes(), &document); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, run.stdout.String())
	}
	if document.Version != "0.1.0" || document.Manifest != "0.2.0" {
		t.Fatalf("json = %#v, want both versions reported", document)
	}
	if document.OK {
		t.Fatalf("json ok = true, want false for a mismatch")
	}
	if !strings.Contains(document.Detail, "stale") {
		t.Fatalf("json detail = %q, want it to explain the mismatch", document.Detail)
	}
}

// TestReleaseWorkflowEmbedsTheTagAsTheVersion pins the pipeline: a published
// binary self-reports, and a local build keeps the dev default.
func TestReleaseWorkflowEmbedsTheTagAsTheVersion(t *testing.T) {
	build, ok := workflowJobs(t, workflowSource(t, "release.yml"))["build"]
	if !ok {
		t.Fatal("release.yml declares no build job")
	}
	for _, want := range []string{"-ldflags", "-X main.version=", "${GITHUB_REF_NAME#v}"} {
		if !strings.Contains(build, want) {
			t.Errorf("the build job does not pass %q; got:\n%s", want, build)
		}
	}

	makefile, err := os.ReadFile(filepath.Join("..", "..", "Makefile"))
	if err != nil {
		t.Fatalf("read the Makefile: %v", err)
	}
	if strings.Contains(string(makefile), "-ldflags") {
		t.Errorf("the Makefile passes ldflags, so a local build would stop reporting %q:\n%s", "dev", makefile)
	}
}

// TestEnsureBinaryVerifiesADownloadedBinaryWithVersion pins the provisioning
// sanity check: a downloaded binary has to answer "version", which is what a
// binary from this release line does and what a mismatched or foreign asset
// does not.
func TestEnsureBinaryVerifiesADownloadedBinaryWithVersion(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "bin", "ensure-binary.sh"))
	if err != nil {
		t.Fatalf("read bin/ensure-binary.sh: %v", err)
	}
	script := string(data)

	if strings.Contains(script, `"$tmp" --help`) {
		t.Error("bin/ensure-binary.sh still checks a downloaded binary with --help")
	}
	if !strings.Contains(script, `"$tmp" version`) {
		t.Error(`bin/ensure-binary.sh does not run "$tmp" version as its sanity check`)
	}
	for _, want := range []string{`sanity="$("$tmp" version`, `"hs version "*`} {
		if !strings.Contains(script, want) {
			t.Errorf("bin/ensure-binary.sh does not compose the check with %q", want)
		}
	}
}
