package main

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/WilberC/herdr-suite/internal/herdrmock"
)

type fakeExecer struct {
	names []string
	err   error
}

func (f *fakeExecer) Attach(name string) error {
	f.names = append(f.names, name)
	return f.err
}

func newTestDeps() (deps, *herdrmock.Fake, *fakeExecer) {
	client := &herdrmock.Fake{Sessions: []herdrmock.Session{
		{Name: "default", Running: true, Default: true},
		{Name: "work", Running: true},
		{Name: "scratch"},
	}}
	exec := &fakeExecer{}
	return deps{
		client: client,
		spaces: client,
		writer: client,
		exec:   exec,
		getenv: func(string) string { return "" },
		switcher: func(context.Context) (string, error) {
			return "", nil
		},
	}, client, exec
}

func TestRunContract(t *testing.T) {
	tests := []struct {
		name          string
		args          []string
		setup         func(d *deps)
		interactive   bool
		confirmAnswer bool
		switcherOn    bool
		switcherName  string
		switcherErr   error
		wantExit      int
		wantStdout    []string
		wantStderr    []string
		wantCalls     []herdrmock.Call
		wantExec      []string
		wantPrompts   []string
		wantSwitcher  bool
	}{
		{
			name:       "the switcher needs a terminal",
			args:       nil,
			wantExit:   2,
			wantStderr: []string{"hs: the session switcher needs a terminal", "usage: hs"},
		},
		{
			name:         "no arguments opens the switcher",
			interactive:  true,
			wantExit:     0,
			wantSwitcher: true,
		},
		{
			name:         "the switcher attaches the picked session",
			interactive:  true,
			switcherName: "work",
			wantExit:     0,
			wantStdout:   []string{"\033]2;herdr:work\007"},
			wantExec:     []string{"work"},
			wantSwitcher: true,
		},
		{
			name:        "the switcher refuses a nested launch",
			interactive: true,
			setup: func(d *deps) {
				d.getenv = func(name string) string {
					if name == "HERDR_ENV" {
						return "inside"
					}
					return ""
				}
			},
			wantExit:   2,
			wantStderr: []string{"hs: run it outside herdr; herdr forbids nested launches"},
		},
		{
			name:         "the switcher surfaces its failure",
			interactive:  true,
			switcherErr:  errors.New("terminal closed"),
			wantExit:     1,
			wantStderr:   []string{"hs: terminal closed"},
			wantSwitcher: true,
		},
		{
			name:       "help prints usage on stdout and exits 0",
			args:       []string{"--help"},
			wantExit:   0,
			wantStdout: []string{"usage: hs", "attach", "session", "space", "layout"},
		},
		{
			name:       "the help command prints usage on stdout",
			args:       []string{"help"},
			wantExit:   0,
			wantStdout: []string{"usage: hs", "session"},
		},
		{
			name:       "an unknown command reports the name and exits 2",
			args:       []string{"bogus"},
			wantExit:   2,
			wantStderr: []string{`unknown command "bogus"`, "usage: hs"},
		},
		{
			name:       "bare space prints its usage on stderr",
			args:       []string{"space"},
			wantExit:   2,
			wantStderr: []string{"usage: hs space", "save <name>"},
		},
		{
			name:       "the layout command still reports not implemented",
			args:       []string{"layout"},
			wantExit:   2,
			wantStderr: []string{"layout: not implemented", "usage: hs layout"},
		},
		{
			name:       "session help prints its usage on stdout",
			args:       []string{"session", "--help"},
			wantExit:   0,
			wantStdout: []string{"usage: hs session"},
		},
		{
			name:       "bare session prints its usage on stderr",
			args:       []string{"session"},
			wantExit:   2,
			wantStderr: []string{"usage: hs session", "list", "create", "stop", "restart", "delete"},
		},
		{
			name:     "session list prints the stable table",
			args:     []string{"session", "list"},
			wantExit: 0,
			wantStdout: []string{
				"NAME",
				"default  running (default)",
				"work     running",
				"scratch  stopped",
			},
			wantCalls: []herdrmock.Call{{Method: "ListSessions"}},
		},
		{
			name:       "session list prints json on request",
			args:       []string{"session", "list", "--json"},
			wantExit:   0,
			wantStdout: []string{`"sessions"`, `"name": "default"`, `"default": true`},
			wantCalls:  []herdrmock.Call{{Method: "ListSessions"}},
		},
		{
			name:       "session list rejects an unknown flag",
			args:       []string{"session", "list", "--bogus"},
			wantExit:   2,
			wantStderr: []string{`unknown flag "--bogus"`, "usage: hs session"},
		},
		{
			name:       "session list rejects an extra argument",
			args:       []string{"session", "list", "extra"},
			wantExit:   2,
			wantStderr: []string{"list takes no arguments", "usage: hs session"},
		},
		{
			name:       "session list surfaces the client failure",
			args:       []string{"session", "list"},
			setup:      func(d *deps) { d.client.(*herdrmock.Fake).ListSessionsErr = errors.New("socket unavailable") },
			wantExit:   1,
			wantStderr: []string{"hs: list sessions: socket unavailable"},
			wantCalls:  []herdrmock.Call{{Method: "ListSessions"}},
		},
		{
			name:       "session stop stops with yes",
			args:       []string{"session", "stop", "work", "--yes"},
			wantExit:   0,
			wantStdout: []string{"stopped session 'work'\n"},
			wantCalls:  []herdrmock.Call{{Method: "ListSessions"}, {Method: "StopSession", Target: "work"}},
		},
		{
			name:       "session stop refuses without a confirmation",
			args:       []string{"session", "stop", "work"},
			wantExit:   2,
			wantStderr: []string{"hs: stop 'work'? its agents will be killed; pass --yes to run it non-interactively"},
			wantCalls:  []herdrmock.Call{{Method: "ListSessions"}},
		},
		{
			name:          "session stop confirms interactively",
			args:          []string{"session", "stop", "work"},
			interactive:   true,
			confirmAnswer: true,
			wantExit:      0,
			wantStdout:    []string{"stopped session 'work'\n"},
			wantCalls:     []herdrmock.Call{{Method: "ListSessions"}, {Method: "StopSession", Target: "work"}},
			wantPrompts:   []string{"stop 'work'? its agents will be killed"},
		},
		{
			name:          "session stop honours a declined confirmation",
			args:          []string{"session", "stop", "work"},
			interactive:   true,
			confirmAnswer: false,
			wantExit:      0,
			wantStderr:    []string{"cancelled"},
			wantCalls:     []herdrmock.Call{{Method: "ListSessions"}},
			wantPrompts:   []string{"stop 'work'? its agents will be killed"},
		},
		{
			name:       "session stop rejects an invalid name",
			args:       []string{"session", "stop", "work!", "--yes"},
			wantExit:   2,
			wantStderr: []string{"hs: invalid session name 'work!'; use letters, digits, '.', '_' and '-'"},
		},
		{
			name:       "session stop requires a name",
			args:       []string{"session", "stop", "--yes"},
			wantExit:   2,
			wantStderr: []string{"expected one session name", "usage: hs session"},
		},
		{
			name:       "session delete stops first and deletes",
			args:       []string{"session", "delete", "work", "--yes"},
			wantExit:   0,
			wantStdout: []string{"deleted session 'work'\n"},
			wantCalls: []herdrmock.Call{
				{Method: "ListSessions"},
				{Method: "StopSession", Target: "work"},
				{Method: "DeleteSession", Target: "work"},
			},
		},
		{
			name:       "session delete refuses the default session",
			args:       []string{"session", "delete", "default", "--yes"},
			wantExit:   2,
			wantStderr: []string{"hs: 'default' is the default session and cannot be deleted"},
			wantCalls:  []herdrmock.Call{{Method: "ListSessions"}},
		},
		{
			name:     "session restart stops and reports the attach hint",
			args:     []string{"session", "restart", "work", "--yes"},
			wantExit: 0,
			wantStdout: []string{
				"stopped session 'work'\n",
				"attach it again with: hs attach work\n",
			},
			wantCalls: []herdrmock.Call{{Method: "ListSessions"}, {Method: "StopSession", Target: "work"}},
		},
		{
			name:       "session create sanitizes the typed name",
			args:       []string{"session", "create", "my project"},
			wantExit:   0,
			wantStdout: []string{"\033]2;herdr:my-project\007"},
			wantExec:   []string{"my-project"},
		},
		{
			name:       "session create refuses a nested launch",
			args:       []string{"session", "create", "work"},
			setup:      func(d *deps) { d.getenv = func(string) string { return "inside" } },
			wantExit:   2,
			wantStderr: []string{"hs: run it outside herdr; herdr forbids nested launches"},
		},
		{
			name:       "attach replaces the process",
			args:       []string{"attach", "work"},
			wantExit:   0,
			wantStdout: []string{"\033]2;herdr:work\007"},
			wantExec:   []string{"work"},
		},
		{
			name:       "attach refuses a nested launch",
			args:       []string{"attach", "work"},
			setup:      func(d *deps) { d.getenv = func(string) string { return "inside" } },
			wantExit:   2,
			wantStderr: []string{"hs: run it outside herdr; herdr forbids nested launches"},
		},
		{
			name:       "attach requires a name",
			args:       []string{"attach"},
			wantExit:   2,
			wantStderr: []string{"expected one session name", "usage: hs attach"},
		},
		{
			name:       "an unknown session subcommand exits 2",
			args:       []string{"session", "bogus"},
			wantExit:   2,
			wantStderr: []string{`unknown session command "bogus"`, "usage: hs session"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			d, client, exec := newTestDeps()
			d.out = &stdout
			d.err = &stderr
			d.interactive = tt.interactive
			var prompts []string
			d.confirm = func(prompt string) (bool, error) {
				prompts = append(prompts, prompt)
				return tt.confirmAnswer, nil
			}
			var switcherRuns int
			d.switcher = func(context.Context) (string, error) {
				switcherRuns++
				return tt.switcherName, tt.switcherErr
			}
			if tt.setup != nil {
				tt.setup(&d)
			}

			gotExit := runWith(d, tt.args, &stdout, &stderr)

			if gotExit != tt.wantExit {
				t.Fatalf("run(%q) exit = %d, want %d\nstdout:\n%s\nstderr:\n%s", tt.args, gotExit, tt.wantExit, stdout.String(), stderr.String())
			}
			for _, want := range tt.wantStdout {
				if !strings.Contains(stdout.String(), want) {
					t.Errorf("stdout does not contain %q; got:\n%s", want, stdout.String())
				}
			}
			for _, want := range tt.wantStderr {
				if !strings.Contains(stderr.String(), want) {
					t.Errorf("stderr does not contain %q; got:\n%s", want, stderr.String())
				}
			}
			if len(tt.wantStdout) == 0 && stdout.Len() != 0 {
				t.Errorf("stdout is not empty; got:\n%s", stdout.String())
			}
			if len(tt.wantStderr) == 0 && stderr.Len() != 0 {
				t.Errorf("stderr is not empty; got:\n%s", stderr.String())
			}
			if got := client.Calls(); !reflect.DeepEqual(got, tt.wantCalls) {
				t.Errorf("client calls = %v, want %v", got, tt.wantCalls)
			}
			if !reflect.DeepEqual(exec.names, tt.wantExec) {
				t.Errorf("exec targets = %v, want %v", exec.names, tt.wantExec)
			}
			if !reflect.DeepEqual(prompts, tt.wantPrompts) {
				t.Errorf("prompts = %v, want %v", prompts, tt.wantPrompts)
			}
			if tt.wantSwitcher != (switcherRuns == 1) {
				t.Errorf("switcher runs = %d, wantSwitcher %v", switcherRuns, tt.wantSwitcher)
			}
		})
	}
}
