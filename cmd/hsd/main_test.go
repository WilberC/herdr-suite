package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunReportsUsageAndExitCode(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantExit   int
		wantStdout []string
		wantStderr []string
	}{
		{
			name:       "no arguments prints usage on stderr and exits 2",
			args:       nil,
			wantExit:   2,
			wantStderr: []string{"usage: hsd", "session", "space", "layout"},
		},
		{
			name:       "help prints usage on stdout and exits 0",
			args:       []string{"--help"},
			wantExit:   0,
			wantStdout: []string{"usage: hsd", "session", "space", "layout"},
		},
		{
			name:       "help command prints usage on stdout and exits 0",
			args:       []string{"help"},
			wantExit:   0,
			wantStdout: []string{"usage: hsd", "session"},
		},
		{
			name:       "unknown command reports the name and exits 2",
			args:       []string{"bogus"},
			wantExit:   2,
			wantStderr: []string{`unknown command "bogus"`, "usage: hsd"},
		},
		{
			name:       "session scaffold prints its usage and exits 2",
			args:       []string{"session"},
			wantExit:   2,
			wantStderr: []string{"session: not implemented", "usage: hsd session"},
		},
		{
			name:       "space scaffold prints its usage and exits 2",
			args:       []string{"space"},
			wantExit:   2,
			wantStderr: []string{"space: not implemented", "usage: hsd space"},
		},
		{
			name:       "layout scaffold prints its usage and exits 2",
			args:       []string{"layout"},
			wantExit:   2,
			wantStderr: []string{"layout: not implemented", "usage: hsd layout"},
		},
		{
			name:       "scaffold help prints its usage on stdout and exits 0",
			args:       []string{"session", "--help"},
			wantExit:   0,
			wantStdout: []string{"usage: hsd session"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer

			gotExit := run(tt.args, &stdout, &stderr)

			if gotExit != tt.wantExit {
				t.Fatalf("run(%q) exit = %d, want %d", tt.args, gotExit, tt.wantExit)
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
		})
	}
}
