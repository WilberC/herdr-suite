package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/WilberC/herdr-suite/internal/layout"
)

// manualClock is the time source a progress test controls: nothing advances
// unless the test says so, so a slow replay is pinned without sleeping.
type manualClock struct{ at time.Time }

func (c *manualClock) now() time.Time { return c.at }

// jumpClock answers the first read with one time and every later read with
// another, which is how a test says "this operation took 2.5s" without sleeping
// and without depending on how many times the code reads the clock.
type jumpClock struct {
	first time.Time
	then  time.Time
	calls int
}

func (c *jumpClock) now() time.Time {
	c.calls++
	if c.calls == 1 {
		return c.first
	}
	return c.then
}

// steppingClock advances by step on every read, which is how a test crosses the
// budget at a known step.
type steppingClock struct {
	at   time.Time
	step time.Duration
}

func (c *steppingClock) now() time.Time {
	at := c.at
	c.at = c.at.Add(c.step)
	return at
}

func sampleSteps(total int) []layout.Step {
	steps := make([]layout.Step, 0, total)
	for index := 1; index <= total; index++ {
		steps = append(steps, layout.Step{Index: index, Total: total, Action: layout.Action{Op: layout.OpRenamePane, Pane: "p-1", Label: "editor"}})
	}
	return steps
}

func progressLines(out *bytes.Buffer) []string {
	trimmed := strings.TrimSpace(out.String())
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "\n")
}

// TestStepProgressStaysSilentWhenTheReplayIsFast keeps the scriptable path
// exactly as quiet as it is today: a replay under the budget writes nothing to
// stderr.
func TestStepProgressStaysSilentWhenTheReplayIsFast(t *testing.T) {
	clock := &manualClock{at: time.Date(2026, 11, 12, 20, 0, 0, 0, time.UTC)}
	out := &bytes.Buffer{}
	progress := newStepProgress(out, false, progressBudget, clock.now)

	for _, step := range sampleSteps(3) {
		progress.report(step)
		clock.at = clock.at.Add(200 * time.Millisecond)
	}
	progress.finish()

	if out.Len() != 0 {
		t.Fatalf("a fast replay wrote %q, want nothing", out.String())
	}
}

// TestStepProgressPrintsEveryStepOnceTheBudgetPasses pins what a slow replay
// says: one stderr line per step, the steps that already ran included.
func TestStepProgressPrintsEveryStepOnceTheBudgetPasses(t *testing.T) {
	clock := &steppingClock{at: time.Date(2026, 11, 12, 20, 0, 0, 0, time.UTC), step: time.Second}
	out := &bytes.Buffer{}
	progress := newStepProgress(out, false, progressBudget, clock.now)

	for _, step := range sampleSteps(4) {
		progress.report(step)
	}
	progress.finish()

	lines := progressLines(out)
	want := []string{
		"[1/4] rename p-1 'editor'",
		"[2/4] rename p-1 'editor'",
		"[3/4] rename p-1 'editor'",
		"[4/4] rename p-1 'editor'",
	}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Fatalf("stderr lines =\n%s\nwant:\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
}

// TestStepProgressBackfillsTheStepsThatRanBeforeTheBudget pins the finish
// report: a replay whose steps all ran fast, but whose total passed the budget,
// still names every step instead of nothing.
func TestStepProgressBackfillsTheStepsThatRanBeforeTheBudget(t *testing.T) {
	clock := &manualClock{at: time.Date(2026, 11, 12, 20, 0, 0, 0, time.UTC)}
	out := &bytes.Buffer{}
	progress := newStepProgress(out, false, progressBudget, clock.now)

	for _, step := range sampleSteps(2) {
		progress.report(step)
	}
	clock.at = clock.at.Add(2500 * time.Millisecond)
	progress.finish()

	if got, want := len(progressLines(out)), 2; got != want {
		t.Fatalf("stderr holds %d lines, want %d:\n%s", got, want, out.String())
	}
}

// TestStepProgressPrintsImmediatelyWhenVerbose pins --verbose: the steps show
// as they happen, whatever the budget says.
func TestStepProgressPrintsImmediatelyWhenVerbose(t *testing.T) {
	clock := &manualClock{at: time.Date(2026, 11, 12, 20, 0, 0, 0, time.UTC)}
	out := &bytes.Buffer{}
	progress := newStepProgress(out, true, progressBudget, clock.now)

	progress.report(sampleSteps(1)[0])

	if got, want := out.String(), "[1/1] rename p-1 'editor'\n"; got != want {
		t.Fatalf("stderr = %q, want %q", got, want)
	}
}

// TestSlowSuffixNamesTheElapsedTimeOnlyWhenSlow pins the installer suffix: a
// fast operation adds nothing to its line, a slow one says how long it took.
func TestSlowSuffixNamesTheElapsedTimeOnlyWhenSlow(t *testing.T) {
	tests := []struct {
		name    string
		elapsed time.Duration
		want    string
	}{
		{name: "instant", elapsed: 0, want: ""},
		{name: "under the budget", elapsed: 1500 * time.Millisecond, want: ""},
		{name: "at the budget", elapsed: progressBudget, want: ""},
		{name: "over the budget", elapsed: 2500 * time.Millisecond, want: " (took 2.5s)"},
		{name: "much longer", elapsed: 12*time.Second + 300*time.Millisecond, want: " (took 12.3s)"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := slowSuffix(tt.elapsed); got != tt.want {
				t.Fatalf("slowSuffix(%v) = %q, want %q", tt.elapsed, got, tt.want)
			}
		})
	}
}
