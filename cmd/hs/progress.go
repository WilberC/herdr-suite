package main

import (
	"fmt"
	"io"
	"time"

	"github.com/WilberC/herdr-suite/internal/layout"
)

// progressBudget is how long a replay or an install may run before it starts
// reporting progress. Under it the command stays as silent as it always was,
// which keeps the scriptable path unchanged.
const progressBudget = 2 * time.Second

// clock is the time source a command measures progress with: the clock the deps
// carry, or the wall clock when nothing pinned one.
func (d deps) clock() func() time.Time {
	if d.now != nil {
		return d.now
	}
	return time.Now
}

// stepProgress reports replay steps to stderr. Nothing prints while the replay
// is under the budget; the first step that crosses it makes the steps that
// already ran print too, so a slow replay always names every step, and a replay
// that only ends over budget backfills them at finish. verbose skips the budget
// and prints each step as it starts. Stdout is never touched: a script reads the
// same summary it read before.
type stepProgress struct {
	out     io.Writer
	verbose bool
	budget  time.Duration
	clock   func() time.Time
	started time.Time
	lines   []string
	printed bool
}

// newStepProgress starts measuring the replay that is about to run.
func newStepProgress(out io.Writer, verbose bool, budget time.Duration, clock func() time.Time) *stepProgress {
	if clock == nil {
		clock = time.Now
	}
	return &stepProgress{out: out, verbose: verbose, budget: budget, clock: clock, started: clock()}
}

// report is the layout.StepObserver a load hands to Apply: it tells the user
// which step is starting, before the herdr call it makes.
func (p *stepProgress) report(step layout.Step) {
	line := fmt.Sprintf("[%d/%d] %s", step.Index, step.Total, step.Describe())
	if p.verbose {
		p.print(line)
		return
	}
	p.lines = append(p.lines, line)
	if p.printed {
		p.print(line)
		return
	}
	if p.clock().Sub(p.started) > p.budget {
		p.flush()
	}
}

// finish reports a replay whose total passed the budget even when no single
// step did, and stays silent when the whole replay was fast.
func (p *stepProgress) finish() {
	if p.verbose || p.printed || len(p.lines) == 0 {
		return
	}
	if p.clock().Sub(p.started) > p.budget {
		p.flush()
	}
}

func (p *stepProgress) flush() {
	for _, line := range p.lines {
		p.print(line)
	}
	p.printed = true
}

func (p *stepProgress) print(line string) {
	fmt.Fprintln(p.out, line)
}

// slowSuffix names how long an operation took when it passed the budget, and is
// empty when it was fast, so a normal run prints the line it always printed.
func slowSuffix(elapsed time.Duration) string {
	if elapsed <= progressBudget {
		return ""
	}
	return fmt.Sprintf(" (took %.1fs)", elapsed.Seconds())
}
