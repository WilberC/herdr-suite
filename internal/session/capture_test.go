package session

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/WilberC/herdr-suite/internal/herdrmock"
	"github.com/WilberC/tui-kit/text"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// This file is the visual capture harness: it renders the switcher with
// realistic sample data at fixed window sizes and hands each frame to freeze,
// so a redesign can be looked at instead of reasoned about. It only runs when
// HS_CAPTURE is set, and it is not a user-facing command:
//
//	HS_CAPTURE=1 HS_CAPTURE_DIR=/tmp/hs-shots HS_CAPTURE_PREFIX=after \
//	  go test ./internal/session -run TestCaptureSwitcher -count=1

// captureSwitcherSize is the window a captured frame is rendered for.
type captureSwitcherSize struct {
	name string
	w, h int
}

// resizeSwitcher hands the model the window size a terminal would report.
func resizeSwitcher(m *switcher, w, h int) {
	m.width, m.height = w, h
}

// captureSessions is the sample list every shot renders, in the order the
// manager lists it: running sessions first, each group by name.
func captureSessions() []herdrmock.Session {
	return []herdrmock.Session{
		{Name: "api-gateway", Running: true, Socket: "/home/ana/.local/share/herdr/sessions/api-gateway.sock"},
		{Name: "gentle-shell", Running: true, Socket: "/home/ana/.local/share/herdr/sessions/gentle-shell.sock"},
		{Name: "herdr-suite", Running: true, Socket: "/home/ana/.local/share/herdr/sessions/herdr-suite.sock"},
		{Name: "work", Running: true, Default: true, Socket: "/home/ana/.local/share/herdr/sessions/work.sock"},
		{Name: "scratch", Socket: "/home/ana/.local/share/herdr/sessions/scratch.sock"},
		{Name: "research", Socket: "/home/ana/.local/share/herdr/sessions/research.sock"},
		{Name: "demo", Socket: "/home/ana/.local/share/herdr/sessions/demo.sock"},
	}
}

// captureWorkspaces is the preview of the highlighted session.
func captureWorkspaces() []WorkspacePreview {
	return []WorkspacePreview{
		{Label: "project", Tabs: 3, Panes: 5, Agent: "working"},
		{Label: "docs", Tabs: 1, Panes: 1, Agent: "idle"},
		{Label: "ci", Tabs: 2, Panes: 3, Agent: "working"},
		{Label: "notes", Tabs: 1, Panes: 2},
	}
}

// captureModel builds a switcher over the sample data, highlighted on the first
// session and with its preview already in.
func captureModel(sessions []herdrmock.Session, workspaces []WorkspacePreview) *switcher {
	model := newSwitcher(SwitcherConfig{Preview: capturePreviewer{}})
	model.setSessions(sessions)
	// The shots highlight the default session, the way a user opens the list.
	for i, session := range model.matches {
		if session.Default {
			model.cursor = i
		}
	}
	if session, ok := model.highlighted(); ok {
		model.previewName = session.Name
		model.previewWorkspaces = workspaces
		model.previewDone = true
	}
	return model
}

// capturePreviewer stands in for the topology client: the harness writes the
// preview the switcher would have received.
type capturePreviewer struct{}

func (capturePreviewer) Preview(context.Context, herdrmock.Session) ([]WorkspacePreview, error) {
	return captureWorkspaces(), nil
}

func TestCaptureSwitcher(t *testing.T) {
	if os.Getenv("HS_CAPTURE") == "" {
		t.Skip("visual capture harness; set HS_CAPTURE=1 to run it")
	}
	lipgloss.SetColorProfile(termenv.TrueColor)
	dir := captureDir(t)
	prefix := capturePrefix()

	frames := map[string]string{}
	for _, size := range []captureSwitcherSize{
		{name: "180", w: 180, h: 50},
		{name: "140", w: 140, h: 40},
		{name: "80", w: 80, h: 24},
	} {
		model := captureModel(captureSessions(), captureWorkspaces())
		resizeSwitcher(model, size.w, size.h)
		frames[prefix+"-switcher-"+size.name] = captureCanvas(model.View(), size.w, size.h)
	}

	filtered := captureModel(captureSessions(), captureWorkspaces())
	for _, r := range "her" {
		filtered.query += string(r)
	}
	filtered.refilter()
	if session, ok := filtered.highlighted(); ok {
		filtered.previewName = session.Name
	}
	resizeSwitcher(filtered, 140, 40)
	frames[prefix+"-switcher-filtered"] = captureCanvas(filtered.View(), 140, 40)

	empty := captureModel(nil, nil)
	resizeSwitcher(empty, 140, 40)
	frames[prefix+"-switcher-empty"] = captureCanvas(empty.View(), 140, 40)

	status := captureModel(captureSessions(), captureWorkspaces())
	status.status = "stopped 'scratch'"
	resizeSwitcher(status, 140, 40)
	frames[prefix+"-switcher-status"] = captureCanvas(status.View(), 140, 40)

	pending := captureModel(captureSessions(), nil)
	pending.previewDone = false
	resizeSwitcher(pending, 140, 40)
	frames[prefix+"-switcher-pending"] = captureCanvas(pending.View(), 140, 40)

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
