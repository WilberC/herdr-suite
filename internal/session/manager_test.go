package session

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/WilberC/herdr-suite/internal/herdrmock"
)

type fakeExec struct {
	names []string
	err   error
}

func (f *fakeExec) Attach(name string) error {
	f.names = append(f.names, name)
	return f.err
}

type managerFixture struct {
	manager *Manager
	client  *herdrmock.Fake
	exec    *fakeExec
	out     *bytes.Buffer
	errOut  *bytes.Buffer
	prompts []string
}

func newManagerFixture(t *testing.T, sessions ...herdrmock.Session) *managerFixture {
	t.Helper()
	fixture := &managerFixture{
		client: &herdrmock.Fake{Sessions: sessions},
		exec:   &fakeExec{},
		out:    &bytes.Buffer{},
		errOut: &bytes.Buffer{},
	}
	fixture.manager = New(Config{
		Client:    fixture.client,
		Exec:      fixture.exec,
		Out:       fixture.out,
		Err:       fixture.errOut,
		Getenv:    func(string) string { return "" },
		AssumeYes: true,
		Confirmer: func(prompt string) (bool, error) {
			fixture.prompts = append(fixture.prompts, prompt)
			return true, nil
		},
	})
	return fixture
}

func sessionFixture() []herdrmock.Session {
	return []herdrmock.Session{
		{Name: "default", Running: true, Default: true},
		{Name: "work", Running: true},
		{Name: "scratch"},
	}
}

func callTargets(fake *herdrmock.Fake, method string) []string {
	var targets []string
	for _, call := range fake.Calls() {
		if call.Method == method {
			targets = append(targets, call.Target)
		}
	}
	return targets
}

func TestListOrdersRunningSessionsFirstThenByName(t *testing.T) {
	fixture := newManagerFixture(t,
		herdrmock.Session{Name: "zeta"},
		herdrmock.Session{Name: "beta", Running: true},
		herdrmock.Session{Name: "alpha"},
		herdrmock.Session{Name: "gamma", Running: true},
	)

	got, err := fixture.manager.List(context.Background())
	if err != nil {
		t.Fatalf("List error = %v, want nil", err)
	}

	var names []string
	for _, session := range got {
		names = append(names, session.Name)
	}
	want := []string{"beta", "gamma", "alpha", "zeta"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("List names = %v, want %v", names, want)
	}
}

func TestStopRunsAfterConfirmationAndReports(t *testing.T) {
	fixture := newManagerFixture(t, sessionFixture()...)

	if err := fixture.manager.Stop(context.Background(), "work"); err != nil {
		t.Fatalf("Stop error = %v, want nil", err)
	}

	if got, want := callTargets(fixture.client, "StopSession"), []string{"work"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("stopped sessions = %v, want %v", got, want)
	}
	if got, want := fixture.out.String(), "stopped session 'work'\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
	if len(fixture.prompts) != 0 {
		t.Fatalf("prompts = %v, want none under AssumeYes", fixture.prompts)
	}
}

func TestStopSkipsAnAlreadyStoppedSession(t *testing.T) {
	fixture := newManagerFixture(t, sessionFixture()...)

	if err := fixture.manager.Stop(context.Background(), "scratch"); err != nil {
		t.Fatalf("Stop error = %v, want nil", err)
	}

	if got := callTargets(fixture.client, "StopSession"); len(got) != 0 {
		t.Fatalf("stopped sessions = %v, want none", got)
	}
	if got, want := fixture.errOut.String(), "session 'scratch' is already stopped\n"; got != want {
		t.Fatalf("stderr = %q, want %q", got, want)
	}
	if fixture.prompts != nil {
		t.Fatalf("prompts = %v, want none for a stopped session", fixture.prompts)
	}
}

func TestStopRefusesWithoutConfirmationInANonInteractiveShell(t *testing.T) {
	fixture := newManagerFixture(t, sessionFixture()...)
	fixture.manager = New(Config{
		Client:    fixture.client,
		Exec:      fixture.exec,
		Out:       fixture.out,
		Err:       fixture.errOut,
		Getenv:    func(string) string { return "" },
		Confirmer: fixture.manager.config.Confirmer,
	})

	err := fixture.manager.Stop(context.Background(), "work")

	var refusal Refusal
	if !errors.As(err, &refusal) {
		t.Fatalf("Stop error = %v, want Refusal", err)
	}
	if want := "stop 'work'? its agents will be killed; pass --yes to run it non-interactively"; refusal.Reason != want {
		t.Fatalf("refusal = %q, want %q", refusal.Reason, want)
	}
	if got := callTargets(fixture.client, "StopSession"); len(got) != 0 {
		t.Fatalf("stopped sessions = %v, want none", got)
	}
}

func TestStopKeepsTheSessionWhenTheUserDeclines(t *testing.T) {
	fixture := newManagerFixture(t, sessionFixture()...)
	fixture.manager = New(Config{
		Client:      fixture.client,
		Exec:        fixture.exec,
		Out:         fixture.out,
		Err:         fixture.errOut,
		Getenv:      func(string) string { return "" },
		Interactive: true,
		Confirmer: func(prompt string) (bool, error) {
			fixture.prompts = append(fixture.prompts, prompt)
			return false, nil
		},
	})

	err := fixture.manager.Stop(context.Background(), "work")

	if !errors.Is(err, ErrCancelled) {
		t.Fatalf("Stop error = %v, want ErrCancelled", err)
	}
	if got := callTargets(fixture.client, "StopSession"); len(got) != 0 {
		t.Fatalf("stopped sessions = %v, want none", got)
	}
	if want := []string{"stop 'work'? its agents will be killed"}; strings.Join(fixture.prompts, "|") != strings.Join(want, "|") {
		t.Fatalf("prompts = %v, want %v", fixture.prompts, want)
	}
}

func TestStopSurfacesTheClientError(t *testing.T) {
	fixture := newManagerFixture(t, sessionFixture()...)
	fixture.client.StopSessionErr = errors.New("socket unavailable")

	err := fixture.manager.Stop(context.Background(), "work")

	if err == nil || !strings.Contains(err.Error(), "stop 'work': socket unavailable") {
		t.Fatalf("Stop error = %v, want it to wrap the client error", err)
	}
}

func TestDeleteStopsARunningSessionFirst(t *testing.T) {
	fixture := newManagerFixture(t, sessionFixture()...)

	if err := fixture.manager.Delete(context.Background(), "work"); err != nil {
		t.Fatalf("Delete error = %v, want nil", err)
	}

	want := []herdrmock.Call{
		{Method: "ListSessions"},
		{Method: "StopSession", Target: "work"},
		{Method: "DeleteSession", Target: "work"},
	}
	if got := fixture.client.Calls(); strings.Join(callNames(got), ",") != strings.Join(callNames(want), ",") {
		t.Fatalf("calls = %v, want %v", callNames(got), callNames(want))
	}
	if got, want := fixture.out.String(), "deleted session 'work'\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}

func TestDeleteOfAStoppedSessionSkipsTheStop(t *testing.T) {
	fixture := newManagerFixture(t, sessionFixture()...)

	if err := fixture.manager.Delete(context.Background(), "scratch"); err != nil {
		t.Fatalf("Delete error = %v, want nil", err)
	}

	if got := callTargets(fixture.client, "StopSession"); len(got) != 0 {
		t.Fatalf("stopped sessions = %v, want none", got)
	}
	if got, want := callTargets(fixture.client, "DeleteSession"), []string{"scratch"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("deleted sessions = %v, want %v", got, want)
	}
}

func TestDeleteIsIdempotentForAnUnknownSession(t *testing.T) {
	fixture := newManagerFixture(t, sessionFixture()...)

	if err := fixture.manager.Delete(context.Background(), "ghost"); err != nil {
		t.Fatalf("Delete error = %v, want nil", err)
	}

	if got, want := callTargets(fixture.client, "DeleteSession"), []string{"ghost"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("deleted sessions = %v, want %v", got, want)
	}
	if got := callTargets(fixture.client, "StopSession"); len(got) != 0 {
		t.Fatalf("stopped sessions = %v, want none", got)
	}
}

func TestDeleteRefusesTheDefaultSessionBeforeAsking(t *testing.T) {
	fixture := newManagerFixture(t, sessionFixture()...)

	err := fixture.manager.Delete(context.Background(), "default")

	var refusal Refusal
	if !errors.As(err, &refusal) {
		t.Fatalf("Delete error = %v, want Refusal", err)
	}
	if want := "'default' is the default session and cannot be deleted"; refusal.Reason != want {
		t.Fatalf("refusal = %q, want %q", refusal.Reason, want)
	}
	if got := callTargets(fixture.client, "DeleteSession"); len(got) != 0 {
		t.Fatalf("deleted sessions = %v, want none", got)
	}
	if len(fixture.prompts) != 0 {
		t.Fatalf("prompts = %v, want none for the default session", fixture.prompts)
	}
}

func TestRestartWritesTheFlagFileInInteractiveMode(t *testing.T) {
	fixture := newManagerFixture(t, sessionFixture()...)
	flag := filepath.Join(t.TempDir(), "restart")

	if err := fixture.manager.Restart(context.Background(), "work", flag); err != nil {
		t.Fatalf("Restart error = %v, want nil", err)
	}

	data, err := os.ReadFile(flag)
	if err != nil {
		t.Fatalf("restart flag not written: %v", err)
	}
	if got, want := string(data), "work"; got != want {
		t.Fatalf("restart flag = %q, want %q", got, want)
	}
	if got, want := fixture.out.String(), "stopped session 'work'\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
	if got, want := callTargets(fixture.client, "StopSession"), []string{"work"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("stopped sessions = %v, want %v", got, want)
	}
}

func TestRestartInCLIModeStopsAndReportsTheAttachHint(t *testing.T) {
	fixture := newManagerFixture(t, sessionFixture()...)

	if err := fixture.manager.Restart(context.Background(), "work", ""); err != nil {
		t.Fatalf("Restart error = %v, want nil", err)
	}

	want := "stopped session 'work'\nattach it again with: hs attach work\n"
	if got := fixture.out.String(); got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}

func TestRestartSkipsAStoppedSession(t *testing.T) {
	fixture := newManagerFixture(t, sessionFixture()...)

	if err := fixture.manager.Restart(context.Background(), "scratch", ""); err != nil {
		t.Fatalf("Restart error = %v, want nil", err)
	}

	if got := callTargets(fixture.client, "StopSession"); len(got) != 0 {
		t.Fatalf("stopped sessions = %v, want none", got)
	}
	if got, want := fixture.errOut.String(), "session 'scratch' is already stopped\n"; got != want {
		t.Fatalf("stderr = %q, want %q", got, want)
	}
}

func TestAttachRefusesANestedLaunch(t *testing.T) {
	fixture := newManagerFixture(t)
	fixture.manager = New(Config{
		Client: fixture.client,
		Exec:   fixture.exec,
		Out:    fixture.out,
		Err:    fixture.errOut,
		Getenv: func(name string) string {
			if name == "HERDR_ENV" {
				return "inside"
			}
			return ""
		},
	})

	err := fixture.manager.Attach("work")

	var refusal Refusal
	if !errors.As(err, &refusal) {
		t.Fatalf("Attach error = %v, want Refusal", err)
	}
	if want := "run it outside herdr; herdr forbids nested launches"; refusal.Reason != want {
		t.Fatalf("refusal = %q, want %q", refusal.Reason, want)
	}
	if len(fixture.exec.names) != 0 {
		t.Fatalf("exec targets = %v, want none", fixture.exec.names)
	}
	if fixture.out.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", fixture.out.String())
	}
}

func TestAttachPrintsTheWindowTitleAndReplacesTheProcess(t *testing.T) {
	fixture := newManagerFixture(t)

	if err := fixture.manager.Attach("work"); err != nil {
		t.Fatalf("Attach error = %v, want nil", err)
	}

	if got, want := fixture.out.String(), "\033]2;herdr:work\007"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
	if got, want := fixture.exec.names, []string{"work"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("exec targets = %v, want %v", got, want)
	}
}

func TestAttachSanitizesTheTypedName(t *testing.T) {
	fixture := newManagerFixture(t)

	if err := fixture.manager.Attach(" my  project "); err != nil {
		t.Fatalf("Attach error = %v, want nil", err)
	}

	if got, want := fixture.exec.names, []string{"my-project"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("exec targets = %v, want %v", got, want)
	}
	if got, want := fixture.out.String(), "\033]2;herdr:my-project\007"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}

func TestAttachRefusesAnInvalidName(t *testing.T) {
	fixture := newManagerFixture(t)

	err := fixture.manager.Attach("work!")

	var refusal Refusal
	if !errors.As(err, &refusal) {
		t.Fatalf("Attach error = %v, want Refusal", err)
	}
	if len(fixture.exec.names) != 0 {
		t.Fatalf("exec targets = %v, want none", fixture.exec.names)
	}
}

func TestTerminalConfirmerAcceptsYAndYesInAnyCase(t *testing.T) {
	tests := []struct {
		answer string
		want   bool
	}{
		{answer: "y\n", want: true},
		{answer: "Y\n", want: true},
		{answer: "yes\n", want: true},
		{answer: "YES\n", want: true},
		{answer: "Yes\n", want: true},
		{answer: "n\n", want: false},
		{answer: "\n", want: false},
		{answer: "nope\n", want: false},
	}

	for _, tt := range tests {
		t.Run(strings.TrimSpace(tt.answer), func(t *testing.T) {
			var out bytes.Buffer
			confirm := TerminalConfirmer(strings.NewReader(tt.answer), &out)

			got, err := confirm("stop 'work'? its agents will be killed")
			if err != nil {
				t.Fatalf("confirm error = %v, want nil", err)
			}
			if got != tt.want {
				t.Fatalf("confirm(%q) = %v, want %v", tt.answer, got, tt.want)
			}
			if want := "stop 'work'? its agents will be killed [y/N] "; out.String() != want {
				t.Fatalf("prompt = %q, want %q", out.String(), want)
			}
		})
	}
}

func TestTerminalConfirmerTreatsAClosedInputAsNo(t *testing.T) {
	var out bytes.Buffer

	got, err := TerminalConfirmer(strings.NewReader(""), &out)("stop 'work'?")

	if err != nil {
		t.Fatalf("confirm error = %v, want nil", err)
	}
	if got {
		t.Fatalf("confirm = true, want false on closed input")
	}
}

func callNames(calls []herdrmock.Call) []string {
	var names []string
	for _, call := range calls {
		if call.Target == "" {
			names = append(names, call.Method)
			continue
		}
		names = append(names, call.Method+":"+call.Target)
	}
	return names
}
