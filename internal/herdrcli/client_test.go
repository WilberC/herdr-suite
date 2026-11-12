package herdrcli

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/WilberC/herdr-suite/internal/herdrmock"
)

func writeStub(t *testing.T, script string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "herdr")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	return path
}

const listStub = `#!/bin/sh
if [ "$1 $2" != "session list" ] || [ "$3" != "--json" ]; then
  echo "unexpected call: $*" >&2
  exit 9
fi
printf '%s' '{"sessions":[{"default":true,"name":"default","running":true,"socket_path":"/tmp/herdr.sock"},{"name":"work","running":false}]}'
`

func TestListSessionsRunsTheHerdrBinaryAndParsesIt(t *testing.T) {
	t.Setenv(EnvBinary, writeStub(t, listStub))

	sessions, err := New().ListSessions(context.Background())
	if err != nil {
		t.Fatalf("ListSessions error = %v, want nil", err)
	}

	want := []herdrmock.Session{
		{Name: "default", Running: true, Default: true},
		{Name: "work"},
	}
	if !reflect.DeepEqual(sessions, want) {
		t.Fatalf("ListSessions = %#v, want %#v", sessions, want)
	}
}

func TestListSessionsRejectsUnparsableOutput(t *testing.T) {
	t.Setenv(EnvBinary, writeStub(t, "#!/bin/sh\nprintf 'not json'\n"))

	_, err := New().ListSessions(context.Background())

	if err == nil || !strings.Contains(err.Error(), "parse herdr session list") {
		t.Fatalf("ListSessions error = %v, want a parse error", err)
	}
}

func TestStopSessionReportsTheHerdrError(t *testing.T) {
	t.Setenv(EnvBinary, writeStub(t, "#!/bin/sh\necho 'session is busy' >&2\nexit 3\n"))

	err := New().StopSession(context.Background(), "work")

	if err == nil {
		t.Fatalf("StopSession error = nil, want a failure")
	}
	for _, want := range []string{"herdr session stop work", "exit status 3", "session is busy"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("StopSession error = %q, want it to contain %q", err, want)
		}
	}
}

func TestDeleteSessionPassesTheName(t *testing.T) {
	t.Setenv(EnvBinary, writeStub(t, `#!/bin/sh
if [ "$1 $2 $3" != "session delete ghost" ]; then
  echo "unexpected call: $*" >&2
  exit 9
fi
`))

	if err := New().DeleteSession(context.Background(), "ghost"); err != nil {
		t.Fatalf("DeleteSession error = %v, want nil", err)
	}
}

func TestBinaryResolutionExplainsHowToFindHerdr(t *testing.T) {
	t.Setenv(EnvBinary, "")
	t.Setenv("PATH", "")

	_, err := New().ListSessions(context.Background())

	if err == nil || !strings.Contains(err.Error(), EnvBinary) {
		t.Fatalf("ListSessions error = %v, want it to name %s", err, EnvBinary)
	}
}

func TestBinaryPrefersTheClientPathOverTheEnvironment(t *testing.T) {
	explicit := writeStub(t, listStub)
	other := writeStub(t, "#!/bin/sh\nexit 7\n")
	t.Setenv(EnvBinary, other)

	client := &Client{Bin: explicit}
	if _, err := client.ListSessions(context.Background()); err != nil {
		t.Fatalf("ListSessions error = %v, want nil", err)
	}
}

func TestExecerReplacesTheProcessWithTheHerdrClient(t *testing.T) {
	original := execReplace
	t.Cleanup(func() { execReplace = original })

	var gotName string
	var gotArgv, gotEnv []string
	execReplace = func(name string, argv, env []string) error {
		gotName, gotArgv, gotEnv = name, argv, env
		return nil
	}
	t.Setenv(EnvBinary, "/opt/herdr")

	if err := (Execer{}).Attach("work"); err != nil {
		t.Fatalf("Attach error = %v, want nil", err)
	}

	if got, want := gotName, "/opt/herdr"; got != want {
		t.Fatalf("exec path = %q, want %q", got, want)
	}
	wantArgv := []string{"/opt/herdr", "--session", "work"}
	if !reflect.DeepEqual(gotArgv, wantArgv) {
		t.Fatalf("exec argv = %v, want %v", gotArgv, wantArgv)
	}
	if !reflect.DeepEqual(gotEnv, os.Environ()) {
		t.Fatalf("exec environment was not inherited")
	}
}

func TestExecerSurfacesTheReplacementFailure(t *testing.T) {
	original := execReplace
	t.Cleanup(func() { execReplace = original })
	execReplace = func(string, []string, []string) error { return os.ErrPermission }
	t.Setenv(EnvBinary, "/opt/herdr")

	err := (Execer{}).Attach("work")

	if err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("Attach error = %v, want the exec failure", err)
	}
}
