package main

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// workflowSource reads one shipped workflow, so these tests pin the pipeline
// this repository actually publishes instead of a description of it.
func workflowSource(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join("..", "..", ".github", "workflows", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

// matrixList matches the inline matrix lists the workflows declare. They are
// written inline, and only there, so reading them back needs no YAML parser and
// therefore no dependency: what this test compares is exactly what GitHub reads.
var matrixList = regexp.MustCompile(`(?m)^\s+(os|arch): \[([^\]]*)\]$`)

// matrixLists returns the "os: [..]" and "arch: [..]" entries of a workflow,
// refusing duplicates so a second matrix cannot hide from the comparison.
func matrixLists(t *testing.T, source string) map[string][]string {
	t.Helper()
	lists := map[string][]string{}
	for _, match := range matrixList.FindAllStringSubmatch(source, -1) {
		key := match[1]
		if _, again := lists[key]; again {
			t.Fatalf("the workflow declares %q more than once", key)
		}
		lists[key] = []string{}
		for _, value := range strings.Split(match[2], ",") {
			if value = strings.TrimSpace(value); value != "" {
				lists[key] = append(lists[key], value)
			}
		}
	}
	return lists
}

// manifestPlatform names the plugin manifest platform a Go GOOS ships for. A
// target without an entry here cannot be added to the release matrix without
// naming the platform herdr should enable it on.
var manifestPlatform = map[string]string{
	"linux":   "linux",
	"darwin":  "macos",
	"windows": "windows",
}

// jobHeader matches a job id of the jobs section, which is the only block of a
// workflow indented by exactly two spaces.
var jobHeader = regexp.MustCompile(`(?m)^  ([a-z][a-z0-9-]*):$`)

// workflowJobs splits the jobs section into one block per job, so a test can
// assert what one job does instead of matching the whole file.
func workflowJobs(t *testing.T, source string) map[string]string {
	t.Helper()
	_, jobs, ok := strings.Cut(source, "\njobs:\n")
	if !ok {
		t.Fatalf("the workflow declares no jobs section:\n%s", source)
	}
	headers := jobHeader.FindAllStringSubmatchIndex(jobs, -1)
	if len(headers) == 0 {
		t.Fatalf("the workflow declares no jobs:\n%s", source)
	}
	blocks := make(map[string]string, len(headers))
	for index, header := range headers {
		end := len(jobs)
		if index+1 < len(headers) {
			end = headers[index+1][0]
		}
		blocks[jobs[header[2]:header[3]]] = jobs[header[0]:end]
	}
	return blocks
}

func jobNames(jobs map[string]string) []string {
	names := make(map[string]bool, len(jobs))
	for name := range jobs {
		names[name] = true
	}
	return sortedKeys(names)
}

func sortedKeys(values map[string]bool) []string {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// TestReleaseWorkflowShipsTheManifestPlatforms keeps the manifest, the release
// matrix and the README honest about where the plugin runs: a target added to
// the matrix without its platform in herdr-plugin.toml fails here, and so does a
// platform the release never builds.
func TestReleaseWorkflowShipsTheManifestPlatforms(t *testing.T) {
	manifest := parseManifest(t, readManifestBytes(t))
	lists := matrixLists(t, workflowSource(t, "release.yml"))

	if _, ok := lists["arch"]; !ok {
		t.Fatalf("release.yml declares no arch matrix list; got %v", lists)
	}
	if want := []string{"amd64", "arm64"}; !reflect.DeepEqual(lists["arch"], want) {
		t.Fatalf("release.yml builds arches %v, want %v", lists["arch"], want)
	}

	declared := map[string]bool{}
	for _, platform := range manifest.Platforms {
		declared[platform] = true
	}
	shipped := map[string]bool{}
	for _, goos := range lists["os"] {
		platform, ok := manifestPlatform[goos]
		if !ok {
			t.Fatalf("release.yml builds GOOS %q, which no manifest platform names; add the platform to herdr-plugin.toml or drop the target", goos)
		}
		shipped[platform] = true
	}
	if got, want := sortedKeys(shipped), sortedKeys(declared); !reflect.DeepEqual(got, want) {
		t.Fatalf("release.yml ships platforms %v, herdr-plugin.toml declares %v", got, want)
	}
}

// TestReleaseWorkflowPublishesVerifiableAssets pins what an installer needs: a
// static binary per target, built after the checks pass, named as the manifest's
// bin/ replacement expects, and one SHA256SUMS file that covers every one of
// them.
func TestReleaseWorkflowPublishesVerifiableAssets(t *testing.T) {
	source := workflowSource(t, "release.yml")
	jobs := workflowJobs(t, source)

	verify, ok := jobs["verify"]
	if !ok {
		t.Fatalf("release.yml declares the jobs %v, want a verify job", jobNames(jobs))
	}
	for _, want := range []string{"go vet ./...", "go test -count=1 ./...", "gofmt -l"} {
		if !strings.Contains(verify, want) {
			t.Errorf("the verify job does not run %q:\n%s", want, verify)
		}
	}

	build, ok := jobs["build"]
	if !ok {
		t.Fatalf("release.yml declares the jobs %v, want a build job", jobNames(jobs))
	}
	for _, want := range []string{"needs: verify", `CGO_ENABLED: "0"`, "hs-${GOOS}-${GOARCH}", "./cmd/hs", "actions/upload-artifact"} {
		if !strings.Contains(build, want) {
			t.Errorf("the build job does not contain %q:\n%s", want, build)
		}
	}

	publish, ok := jobs["publish"]
	if !ok {
		t.Fatalf("release.yml declares the jobs %v, want a publish job", jobNames(jobs))
	}
	for _, want := range []string{"needs: build", "actions/download-artifact", "sha256sum hs-* > SHA256SUMS", "dist/SHA256SUMS", "gh release create"} {
		if !strings.Contains(publish, want) {
			t.Errorf("the publish job does not contain %q:\n%s", want, publish)
		}
	}
	if !strings.Contains(publish, "actions/checkout") {
		t.Errorf("the publish job does not check out the repository, which gh release create needs to resolve it:\n%s", publish)
	}

	for _, want := range []string{`- "v*"`, "contents: write"} {
		if !strings.Contains(source, want) {
			t.Errorf("release.yml does not contain %q; got:\n%s", want, source)
		}
	}
}

// TestCIWorkflowGatesBuildFormatAndTests pins the checks every push and pull
// request has to pass, on both runners the plugin claims support for.
func TestCIWorkflowGatesBuildFormatAndTests(t *testing.T) {
	source := workflowSource(t, "ci.yml")

	lists := matrixLists(t, source)
	want := []string{"ubuntu-latest", "macos-latest"}
	if got := append([]string{}, lists["os"]...); !reflect.DeepEqual(got, want) {
		t.Fatalf("ci.yml runs on %v, want %v", got, want)
	}

	head, _, ok := strings.Cut(source, "\njobs:\n")
	if !ok {
		t.Fatalf("ci.yml declares no jobs section:\n%s", source)
	}
	for _, want := range []string{"\n  push:", "\n  pull_request:"} {
		if !strings.Contains(head, want) {
			t.Errorf("ci.yml does not run on %q; got:\n%s", strings.TrimSpace(want), head)
		}
	}

	jobs := workflowJobs(t, source)
	test, ok := jobs["test"]
	if !ok {
		t.Fatalf("ci.yml declares the jobs %v, want a test job", jobNames(jobs))
	}
	for _, want := range []string{
		"runs-on: ${{ matrix.os }}",
		"go build ./...",
		"go vet ./...",
		"gofmt -l",
		"go test -count=1 ./...",
	} {
		if !strings.Contains(test, want) {
			t.Errorf("the test job does not run %q:\n%s", want, test)
		}
	}

	gofmt, fail := strings.Index(test, "gofmt -l"), strings.Index(test, "exit 1")
	if gofmt < 0 || fail < 0 || fail < gofmt {
		t.Fatalf("the test job does not fail on unformatted files:\n%s", test)
	}
}
