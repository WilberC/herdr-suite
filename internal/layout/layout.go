package layout

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/WilberC/herdr-suite/internal/herdrmock"
)

// Node types a split tree entry can hold.
const (
	TypePane  = "pane"
	TypeSplit = "split"
)

// CurrentSchema is the layout schema version this build writes. It is stored as
// the top-level schema_version of every saved document, so a later build can
// tell which shape it is reading.
const CurrentSchema = 1

// Layout is one stored space. The single-workspace form holds the workspace that
// was active when it was snapshotted; the session form holds every workspace of
// the session a fork captured. SchemaVersion is the version the document was
// read at, which is CurrentSchema for a freshly captured layout and for a file
// written before the field existed.
type Layout struct {
	Name          string
	Session       string
	SchemaVersion int
	// CapturedAt is the timestamp a stored document records for the moment it
	// was snapshotted. This build reads it and prints it in the show view; it
	// does not write one, so a document saved here carries none.
	CapturedAt string
	Workspace  Workspace
	Workspaces []Workspace
}

// storedDocument is the file form of a layout: exactly one of the two shapes is
// present, so a single-workspace file keeps the shape earlier versions wrote and
// a session file carries a workspace list instead. SchemaVersion is read as raw
// JSON so a value that is not an integer is refused with a message of our own
// instead of a decoder error about a Go type.
type storedDocument struct {
	SchemaVersion json.RawMessage `json:"schema_version,omitempty"`
	Name          string          `json:"name"`
	Session       string          `json:"session,omitempty"`
	CapturedAt    string          `json:"captured_at,omitempty"`
	Workspace     *Workspace      `json:"workspace,omitempty"`
	Workspaces    []Workspace     `json:"workspaces,omitempty"`
}

// MarshalJSON encodes the form the layout holds with the schema version this
// build writes.
func (l Layout) MarshalJSON() ([]byte, error) {
	document := storedDocument{
		SchemaVersion: json.RawMessage(strconv.Itoa(CurrentSchema)),
		Name:          l.Name,
		Session:       l.Session,
		CapturedAt:    l.CapturedAt,
	}
	if len(l.Workspaces) > 0 {
		document.Workspaces = l.Workspaces
	} else {
		workspace := l.Workspace
		document.Workspace = &workspace
	}
	return json.Marshal(document)
}

// UnmarshalJSON reads either form and refuses a document this build cannot
// read: a newer schema first, then a document that carries both shapes or
// neither.
func (l *Layout) UnmarshalJSON(data []byte) error {
	var document storedDocument
	if err := json.Unmarshal(data, &document); err != nil {
		return err
	}
	schemaVersion, err := readSchemaVersion(document.SchemaVersion)
	if err != nil {
		return err
	}
	if document.Workspace != nil && len(document.Workspaces) > 0 {
		return errors.New("layout document holds both workspace and workspaces")
	}
	if document.Workspace == nil && len(document.Workspaces) == 0 {
		return errors.New("layout document holds no workspace")
	}

	l.Name = document.Name
	l.Session = document.Session
	l.SchemaVersion = schemaVersion
	l.CapturedAt = document.CapturedAt
	l.Workspace = Workspace{}
	l.Workspaces = document.Workspaces
	if document.Workspace != nil {
		l.Workspace = *document.Workspace
	}
	return nil
}

// readSchemaVersion reads the document's schema version. A document that names
// none predates versioning (v0.1.0 wrote single-workspace files without it) and
// is read as CurrentSchema; a version this build cannot read is refused instead
// of guessed at.
func readSchemaVersion(raw json.RawMessage) (int, error) {
	if len(raw) == 0 {
		return CurrentSchema, nil
	}
	version, err := strconv.Atoi(string(raw))
	if err != nil {
		return 0, fmt.Errorf("layout schema_version %s is not an integer", raw)
	}
	if version < 1 {
		return 0, fmt.Errorf("layout schema_version %d is invalid; it must be a positive integer", version)
	}
	if version > CurrentSchema {
		return 0, UnsupportedSchemaError{Version: version}
	}
	return version, nil
}

// UnsupportedSchemaError reports a document a newer herdr-suite wrote: this
// build does not know every field it holds, so it refuses the document instead
// of reading the part it understands.
type UnsupportedSchemaError struct {
	Version int
}

func (e UnsupportedSchemaError) Error() string {
	return fmt.Sprintf("layout schema_version %d was written by a newer herdr-suite; update hs", e.Version)
}

// Multi reports whether the layout holds a session: every workspace of one
// session, instead of a single workspace.
func (l Layout) Multi() bool { return len(l.Workspaces) > 0 }

// Spaces returns the workspaces the layout holds: the stored session, or the one
// stored workspace.
func (l Layout) Spaces() []Workspace {
	if len(l.Workspaces) > 0 {
		return l.Workspaces
	}
	return []Workspace{l.Workspace}
}

// Workspace is the snapshotted workspace.
type Workspace struct {
	Label string `json:"label"`
	Tabs  []Tab  `json:"tabs"`
}

// Tab is one tab of the workspace with its split tree.
type Tab struct {
	Label         string `json:"label,omitempty"`
	Focused       bool   `json:"focused"`
	FocusedPaneID string `json:"focused_pane_id,omitempty"`
	Root          Node   `json:"root"`
}

// Node is one node of a tab's split tree: a pane leaf, or a binary split with
// an ordered pair of children.
type Node struct {
	Type string `json:"type"`

	PaneID string `json:"pane_id,omitempty"`
	Label  string `json:"label,omitempty"`
	Cwd    string `json:"cwd,omitempty"`
	Agent  *Agent `json:"agent,omitempty"`

	Direction string  `json:"direction,omitempty"`
	Ratio     float64 `json:"ratio,omitempty"`
	First     *Node   `json:"first,omitempty"`
	Second    *Node   `json:"second,omitempty"`
}

// Agent is the agent identification herdr reports for a pane.
type Agent struct {
	Kind  string `json:"kind"`
	Name  string `json:"name,omitempty"`
	State string `json:"state,omitempty"`
}

// Capture turns a live workspace into a layout named name. Space supplies the
// session the topology was read from; it is empty when the read went to the
// ambient session and is then left out of the document.
func Capture(name string, space herdrmock.Space, workspace herdrmock.Workspace) (Layout, error) {
	captured, err := captureWorkspace(workspace)
	if err != nil {
		return Layout{}, err
	}
	return Layout{Name: name, Session: space.Session, SchemaVersion: CurrentSchema, Workspace: captured}, nil
}

// captureWorkspace turns one live workspace into the stored workspace.
func captureWorkspace(workspace herdrmock.Workspace) (Workspace, error) {
	captured := Workspace{
		Label: workspace.Label,
		Tabs:  make([]Tab, 0, len(workspace.Tabs)),
	}
	for _, tab := range workspace.Tabs {
		root, err := captureNode(tab, tab.Root)
		if err != nil {
			return Workspace{}, err
		}
		captured.Tabs = append(captured.Tabs, Tab{
			Label:         tab.Label,
			Focused:       tab.Focused,
			FocusedPaneID: tab.FocusedPaneID,
			Root:          root,
		})
	}
	return captured, nil
}

// CaptureSession turns a live session into a layout named name that holds every
// workspace it reports, in the order herdr reported them.
func CaptureSession(name string, space herdrmock.Space) (Layout, error) {
	if len(space.Workspaces) == 0 {
		label := space.Session
		if label == "" {
			label = name
		}
		return Layout{}, fmt.Errorf("session '%s' reports no workspaces", label)
	}

	document := Layout{
		Name:          name,
		Session:       space.Session,
		SchemaVersion: CurrentSchema,
		Workspaces:    make([]Workspace, 0, len(space.Workspaces)),
	}
	for _, workspace := range space.Workspaces {
		captured, err := captureWorkspace(workspace)
		if err != nil {
			return Layout{}, fmt.Errorf("workspace '%s': %w", workspace.Label, err)
		}
		document.Workspaces = append(document.Workspaces, captured)
	}
	return document, nil
}

func captureNode(tab herdrmock.Tab, node herdrmock.LayoutNode) (Node, error) {
	if len(node.Children) == 0 {
		pane, ok := paneByID(tab.Panes, node.PaneID)
		if !ok {
			return Node{}, fmt.Errorf("tab '%s': pane '%s' is missing from the topology", tabName(tab), node.PaneID)
		}
		captured := Node{
			Type:   TypePane,
			PaneID: pane.ID,
			Label:  pane.Label,
			Cwd:    pane.Cwd,
		}
		if pane.Agent != "" {
			captured.Agent = &Agent{Kind: pane.Agent, Name: pane.AgentName, State: pane.AgentState}
		}
		return captured, nil
	}

	if len(node.Children) != 2 {
		return Node{}, fmt.Errorf("tab '%s': split node has %d children, want 2", tabName(tab), len(node.Children))
	}
	first, err := captureNode(tab, node.Children[0])
	if err != nil {
		return Node{}, err
	}
	second, err := captureNode(tab, node.Children[1])
	if err != nil {
		return Node{}, err
	}
	return Node{
		Type:      TypeSplit,
		Direction: node.Direction,
		Ratio:     node.Ratio,
		First:     &first,
		Second:    &second,
	}, nil
}

func paneByID(panes []herdrmock.Pane, paneID string) (herdrmock.Pane, bool) {
	for _, pane := range panes {
		if pane.ID == paneID {
			return pane, true
		}
	}
	return herdrmock.Pane{}, false
}

func tabName(tab herdrmock.Tab) string {
	if tab.Label != "" {
		return tab.Label
	}
	return tab.ID
}

// Marshal encodes a layout as the document that is stored and printed.
func Marshal(document Layout) ([]byte, error) {
	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode layout: %w", err)
	}
	return append(data, '\n'), nil
}

// Unmarshal decodes a stored layout document.
func Unmarshal(data []byte) (Layout, error) {
	var document Layout
	if err := json.Unmarshal(data, &document); err != nil {
		return Layout{}, fmt.Errorf("decode layout: %w", err)
	}
	return document, nil
}

// Write prints a layout as the document scripts consume.
func Write(w io.Writer, document Layout) error {
	data, err := Marshal(document)
	if err != nil {
		return err
	}
	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("write layout: %w", err)
	}
	return nil
}

// countPanes counts the pane leaves of a split tree.
func countPanes(node Node) int {
	if node.Type == TypePane {
		return 1
	}
	if node.Type != TypeSplit {
		return 0
	}
	count := 0
	if node.First != nil {
		count += countPanes(*node.First)
	}
	if node.Second != nil {
		count += countPanes(*node.Second)
	}
	return count
}

// plural renders a count with its noun, singular at one.
func plural(count int, noun string) string {
	if count == 1 {
		return fmt.Sprintf("%d %s", count, noun)
	}
	return fmt.Sprintf("%d %ss", count, noun)
}

func joinParts(parts ...string) string {
	kept := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			kept = append(kept, part)
		}
	}
	return strings.Join(kept, " · ")
}

var (
	errNoConfigHome = errors.New("neither XDG_CONFIG_HOME nor HOME is set")
)
