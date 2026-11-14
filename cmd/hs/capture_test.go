package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/WilberC/herdr-suite/internal/herdrmock"
	"github.com/WilberC/herdr-suite/internal/layout"
	"github.com/WilberC/tui-kit/text"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// This file is the visual capture harness for the space pickers: it renders the
// layout picker and the workspace prompt with realistic sample data at fixed
// window sizes and hands each frame to freeze, so a redesign can be looked at
// instead of reasoned about. It only runs when HS_CAPTURE is set:
//
//	HS_CAPTURE=1 HS_CAPTURE_DIR=/tmp/hs-shots HS_CAPTURE_PREFIX=after \
//	  go test ./cmd/hs -run TestCaptureSpacePicker -count=1

// resizeLayoutPicker hands the picker the window size a terminal would report.
func resizeLayoutPicker(p *layoutPicker, w, h int) {
	p.width, p.height = w, h
}

// resizeWorkspacePicker hands the prompt the window size a terminal reports.
func resizeWorkspacePicker(p *workspacePicker, w, h int) {
	p.width, p.height = w, h
}

// captureEntries is the sample layout list every shot renders: both scopes,
// mixed sizes and recent modifications.
func captureEntries() []layout.Entry {
	now := time.Now()
	return []layout.Entry{
		{Name: "api", Scope: layout.ScopeProject, Path: "/home/ana/src/api/.herdr/layouts/api.json", Workspace: "api", Workspaces: 1, Tabs: 3, Panes: 6, Modified: now.Add(-5 * time.Minute)},
		{Name: "web", Scope: layout.ScopeProject, Path: "/home/ana/src/api/.herdr/layouts/web.json", Workspace: "web", Workspaces: 1, Tabs: 2, Panes: 3, Modified: now.Add(-48 * time.Hour)},
		{Name: "work", Scope: layout.ScopeGlobal, Path: "/home/ana/.local/share/herdr-suite/layouts/work.json", Workspace: "project", Workspaces: 1, Tabs: 2, Panes: 4, Modified: now.Add(-2 * time.Hour)},
		{Name: "docs", Scope: layout.ScopeGlobal, Path: "/home/ana/.local/share/herdr-suite/layouts/docs.json", Workspace: "notes", Workspaces: 1, Tabs: 1, Panes: 1, Modified: now.Add(-26 * time.Hour)},
		{Name: "ci-replay", Scope: layout.ScopeGlobal, Path: "/home/ana/.local/share/herdr-suite/layouts/ci-replay.json", Workspace: "ci", Workspaces: 2, Tabs: 3, Panes: 7, Modified: now.Add(-72 * time.Hour)},
	}
}

// captureWorkspaceList is the sample workspace list the load prompt renders.
func captureWorkspaceList() []herdrmock.Workspace {
	return []herdrmock.Workspace{
		{ID: "w1", Label: "project", Focused: true, TabCount: 3, PaneCount: 5, AgentState: "working"},
		{ID: "w2", Label: "docs", TabCount: 1, PaneCount: 1, AgentState: "idle"},
		{ID: "w3", Label: "ci", TabCount: 2, PaneCount: 3, AgentState: "working"},
		{ID: "w4", Label: "notes", TabCount: 1, PaneCount: 2},
	}
}

func TestCaptureSpacePicker(t *testing.T) {
	if os.Getenv("HS_CAPTURE") == "" {
		t.Skip("visual capture harness; set HS_CAPTURE=1 to run it")
	}
	lipgloss.SetColorProfile(termenv.TrueColor)
	dir := captureDir(t)
	prefix := capturePrefix()

	frames := map[string]string{}
	for _, size := range []struct {
		name string
		w, h int
	}{{"180", 180, 50}, {"140", 140, 40}, {"80", 80, 24}} {
		picker := newLayoutPicker(captureEntries(), nil)
		resizeLayoutPicker(picker, size.w, size.h)
		frames[prefix+"-space-"+size.name] = captureCanvas(picker.View(), size.w, size.h)

		prompt := &workspacePicker{workspaces: captureWorkspaceList()}
		resizeWorkspacePicker(prompt, size.w, size.h)
		frames[prefix+"-workspace-"+size.name] = captureCanvas(prompt.View(), size.w, size.h)
	}

	// The pane a herdr popup actually hands these prompts: the manifest declares
	// 120x24 cells, and herdr spawns the command in the popup's inner area (one
	// border cell a side, plus one reserved column), which is where the compact
	// frame has to fill the pane with no band around it.
	picker := newLayoutPicker(captureEntries(), nil)
	resizeLayoutPicker(picker, 117, 22)
	frames[prefix+"-space-popup"] = captureCanvas(picker.View(), 117, 22)

	prompt := &workspacePicker{workspaces: captureWorkspaceList()}
	resizeWorkspacePicker(prompt, 117, 22)
	frames[prefix+"-workspace-popup"] = captureCanvas(prompt.View(), 117, 22)

	empty := newLayoutPicker(nil, nil)
	resizeLayoutPicker(empty, 140, 40)
	frames[prefix+"-space-empty"] = captureCanvas(empty.View(), 140, 40)

	// The pointer over the row beside the selection, so both tints can be judged
	// side by side. hover is counted from one, so 2 is row 1.
	hovered := newLayoutPicker(captureEntries(), nil)
	resizeLayoutPicker(hovered, 140, 40)
	hovered.hover = 2
	frames[prefix+"-space-hover"] = captureCanvas(hovered.View(), 140, 40)

	hoveredPrompt := &workspacePicker{workspaces: captureWorkspaceList()}
	resizeWorkspacePicker(hoveredPrompt, 140, 40)
	hoveredPrompt.hover = 2
	frames[prefix+"-workspace-hover"] = captureCanvas(hoveredPrompt.View(), 140, 40)

	restoring := newLayoutPicker(captureEntries(), func(layout.Entry, func(layout.Step)) (string, error) {
		return "", nil
	})
	restoring.phase = pickerRestoring
	restoring.entry = captureEntries()[0]
	restoring.steps = restoreSteps()[:2]
	last := restoreSteps()[2]
	restoring.step = &last
	resizeLayoutPicker(restoring, 140, 40)
	frames[prefix+"-space-restoring"] = captureCanvas(restoring.View(), 140, 40)

	done := newLayoutPicker(captureEntries(), nil)
	done.phase = pickerDone
	done.entry = captureEntries()[0]
	done.steps = restoreSteps()
	resizeLayoutPicker(done, 140, 40)
	frames[prefix+"-space-done"] = captureCanvas(done.View(), 140, 40)

	failed := newLayoutPicker(captureEntries(), nil)
	failed.phase = pickerFailed
	failed.entry = captureEntries()[0]
	failed.restoreErr = layout.StepError{
		Index:  2,
		Total:  3,
		Action: layout.Action{Op: layout.OpRenamePane, Pane: "p-1", Label: "editor"},
		Err:    errors.New("pane limit reached"),
	}
	resizeLayoutPicker(failed, 140, 40)
	frames[prefix+"-space-failed"] = captureCanvas(failed.View(), 140, 40)

	writeCaptureFrames(t, dir, frames)
}

// captureCanvas pads a rendered view to the whole window, the way a terminal
// shows it: freeze crops trailing whitespace, so the frame only reads as
// centered when the empty cells are part of the capture.
func captureCanvas(view string, w, h int) string {
	lines := strings.Split(view, "\n")
	for len(lines) < h {
		lines = append(lines, "")
	}
	for i, line := range lines {
		if pad := w - text.Width(line); pad > 0 {
			lines[i] = line + strings.Repeat(" ", pad)
		}
	}
	return strings.Join(lines, "\n")
}

func captureDir(t *testing.T) string {
	t.Helper()
	dir := os.Getenv("HS_CAPTURE_DIR")
	if dir == "" {
		dir = filepath.Join(os.TempDir(), "hs-shots")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create capture dir: %v", err)
	}
	return dir
}

func capturePrefix() string {
	if prefix := os.Getenv("HS_CAPTURE_PREFIX"); prefix != "" {
		return prefix
	}
	return "after"
}

// writeCaptureFrames writes every frame as .ansi and, when freeze is
// installed, as the PNG beside it.
func writeCaptureFrames(t *testing.T, dir string, frames map[string]string) {
	t.Helper()
	freeze := os.Getenv("HS_FREEZE")
	if freeze == "" {
		freeze = filepath.Join(os.Getenv("HOME"), ".local", "bin", "freeze")
	}
	for name, frame := range frames {
		ansiPath := filepath.Join(dir, name+".ansi")
		if err := os.WriteFile(ansiPath, []byte(strings.TrimRight(frame, "\n")+"\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", ansiPath, err)
		}
		if _, err := os.Stat(freeze); err != nil {
			t.Logf("freeze not installed; %s left as .ansi", name)
			continue
		}
		pngPath := filepath.Join(dir, name+".png")
		cmd := exec.Command(freeze, "--language", "ansi", "--background", "#1a1b26", "-o", pngPath, ansiPath)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("freeze %s: %v\n%s", name, err, out)
		}
		t.Logf("captured %s", pngPath)
	}
}
