package herdrcli

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/WilberC/herdr-suite/internal/herdrmock"
)

// mutator is the Client's write surface bound to one herdr session: every call
// runs the herdr binary with that session's socket in the environment.
type mutator struct {
	client *Client
	target herdrmock.Target
}

// Mutations returns the write surface bound to target.
func (c *Client) Mutations(target herdrmock.Target) herdrmock.Mutator {
	return mutator{client: c, target: target}
}

// CreateWorkspace runs "herdr workspace create" and reports the workspace, tab
// and root pane herdr answered with.
func (m mutator) CreateWorkspace(ctx context.Context, req herdrmock.CreateWorkspaceRequest) (herdrmock.CreatedPane, error) {
	args := []string{"workspace", "create"}
	args = append(args, valueFlag("--cwd", req.Cwd)...)
	args = append(args, valueFlag("--label", req.Label)...)
	args = append(args, focusFlag(req.Focus)...)

	out, err := m.client.run(ctx, m.target, args...)
	if err != nil {
		return herdrmock.CreatedPane{}, err
	}
	document, err := decodeResult(out, args)
	if err != nil {
		return herdrmock.CreatedPane{}, err
	}
	return herdrmock.CreatedPane{
		WorkspaceID: document.Workspace.ID,
		TabID:       document.Tab.ID,
		PaneID:      document.RootPane.ID,
	}, nil
}

// CreateTab runs "herdr tab create" and reports the tab and root pane herdr
// answered with.
func (m mutator) CreateTab(ctx context.Context, req herdrmock.CreateTabRequest) (herdrmock.CreatedTab, error) {
	args := []string{"tab", "create"}
	args = append(args, valueFlag("--workspace", req.WorkspaceID)...)
	args = append(args, valueFlag("--cwd", req.Cwd)...)
	args = append(args, valueFlag("--label", req.Label)...)
	args = append(args, focusFlag(req.Focus)...)

	out, err := m.client.run(ctx, m.target, args...)
	if err != nil {
		return herdrmock.CreatedTab{}, err
	}
	document, err := decodeResult(out, args)
	if err != nil {
		return herdrmock.CreatedTab{}, err
	}
	return herdrmock.CreatedTab{TabID: document.Tab.ID, PaneID: document.RootPane.ID}, nil
}

// SplitPane runs "herdr pane split" and returns the pane it created. The ratio
// is the share the split pane keeps, which is the ratio the snapshot reports for
// the same split, so a stored ratio is passed unchanged.
func (m mutator) SplitPane(ctx context.Context, req herdrmock.SplitRequest) (string, error) {
	args := []string{"pane", "split", req.PaneID}
	args = append(args, valueFlag("--direction", req.Direction)...)
	if req.Ratio > 0 {
		args = append(args, "--ratio", strconv.FormatFloat(req.Ratio, 'f', -1, 64))
	}
	args = append(args, valueFlag("--cwd", req.Cwd)...)
	args = append(args, focusFlag(req.Focus)...)

	out, err := m.client.run(ctx, m.target, args...)
	if err != nil {
		return "", err
	}
	document, err := decodeResult(out, args)
	if err != nil {
		return "", err
	}
	if document.Pane.ID == "" {
		return "", fmt.Errorf("herdr %s: the answer names no pane", joinArgs(args))
	}
	return document.Pane.ID, nil
}

// RenameTab runs "herdr tab rename".
func (m mutator) RenameTab(ctx context.Context, tabID, label string) error {
	_, err := m.client.run(ctx, m.target, "tab", "rename", tabID, label)
	return err
}

// RenamePane runs "herdr pane rename".
func (m mutator) RenamePane(ctx context.Context, paneID, label string) error {
	_, err := m.client.run(ctx, m.target, "pane", "rename", paneID, label)
	return err
}

// FocusWorkspace runs "herdr workspace focus".
func (m mutator) FocusWorkspace(ctx context.Context, workspaceID string) error {
	_, err := m.client.run(ctx, m.target, "workspace", "focus", workspaceID)
	return err
}

// FocusTab runs "herdr tab focus".
func (m mutator) FocusTab(ctx context.Context, tabID string) error {
	_, err := m.client.run(ctx, m.target, "tab", "focus", tabID)
	return err
}

// FocusAgent runs "herdr agent focus".
func (m mutator) FocusAgent(ctx context.Context, target string) error {
	_, err := m.client.run(ctx, m.target, "agent", "focus", target)
	return err
}

// StartAgent runs "herdr agent start", which returns once herdr detects the
// agent ready in that pane.
func (m mutator) StartAgent(ctx context.Context, req herdrmock.StartAgentRequest) error {
	args := []string{"agent", "start", req.Name, "--kind", req.Kind, "--pane", req.PaneID}
	if len(req.Args) > 0 {
		args = append(args, "--")
		args = append(args, req.Args...)
	}
	_, err := m.client.run(ctx, m.target, args...)
	return err
}

// ClosePane runs "herdr pane close".
func (m mutator) ClosePane(ctx context.Context, paneID string) error {
	_, err := m.client.run(ctx, m.target, "pane", "close", paneID)
	return err
}

// CloseTab runs "herdr tab close".
func (m mutator) CloseTab(ctx context.Context, tabID string) error {
	_, err := m.client.run(ctx, m.target, "tab", "close", tabID)
	return err
}

// creation is the part of a herdr creation answer a replay reads: the ids of
// the objects herdr made.
type creation struct {
	Workspace struct {
		ID string `json:"workspace_id"`
	} `json:"workspace"`
	Tab struct {
		ID string `json:"tab_id"`
	} `json:"tab"`
	RootPane struct {
		ID string `json:"pane_id"`
	} `json:"root_pane"`
	Pane struct {
		ID string `json:"pane_id"`
	} `json:"pane"`
}

// decodeResult reads the creation answer herdr prints: one result object that
// names the workspace, tab, root pane or pane the call created.
func decodeResult(out []byte, args []string) (creation, error) {
	var document struct {
		Result *creation `json:"result"`
	}
	if err := json.Unmarshal(out, &document); err != nil {
		return creation{}, fmt.Errorf("parse herdr %s: %w", joinArgs(args), err)
	}
	if document.Result == nil {
		return creation{}, fmt.Errorf("parse herdr %s: the answer has no result", joinArgs(args))
	}
	return *document.Result, nil
}

func valueFlag(name, value string) []string {
	if value == "" {
		return nil
	}
	return []string{name, value}
}

// focusFlag asks herdr to move focus to the created object, or to leave it
// where it is.
func focusFlag(focus bool) []string {
	if focus {
		return []string{"--focus"}
	}
	return []string{"--no-focus"}
}

func joinArgs(args []string) string {
	return strings.Join(args, " ")
}

var _ herdrmock.Mutator = mutator{}
