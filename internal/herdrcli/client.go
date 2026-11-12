// Package herdrcli implements the herdr client by running the herdr binary.
// herdr-suite never opens a herdr socket itself: every read and write goes
// through the CLI, so the binary behaves the same whether it runs outside
// herdr or inside a plugin action.
package herdrcli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"github.com/WilberC/herdr-suite/internal/herdrmock"
)

// EnvBinary names the environment variable that overrides where the herdr
// binary is found. Without it the binary is looked up on PATH.
const EnvBinary = "HERDR_BIN_PATH"

// Runner executes the herdr binary with the environment it should run in and
// returns its standard streams.
type Runner func(ctx context.Context, bin string, env []string, args ...string) (stdout, stderr []byte, err error)

// Client is the herdrmock.SessionClient implementation that shells out to the
// herdr binary.
type Client struct {
	Bin string
	Run Runner
}

// New returns a client that resolves the herdr binary on every call, so a
// HERDR_BIN_PATH exported after start still applies.
func New() *Client {
	return &Client{}
}

// ListSessions returns every named session herdr knows about.
func (c *Client) ListSessions(ctx context.Context) ([]herdrmock.Session, error) {
	out, err := c.run(ctx, herdrmock.Target{}, "session", "list", "--json")
	if err != nil {
		return nil, err
	}

	var document struct {
		Sessions []struct {
			Name    string `json:"name"`
			Running bool   `json:"running"`
			Default bool   `json:"default"`
			Socket  string `json:"socket_path"`
		} `json:"sessions"`
	}
	if err := json.Unmarshal(out, &document); err != nil {
		return nil, fmt.Errorf("parse herdr session list: %w", err)
	}

	sessions := make([]herdrmock.Session, 0, len(document.Sessions))
	for _, session := range document.Sessions {
		sessions = append(sessions, herdrmock.Session{
			Name:    session.Name,
			Running: session.Running,
			Default: session.Default,
			Socket:  session.Socket,
		})
	}
	return sessions, nil
}

// StopSession stops a running session.
func (c *Client) StopSession(ctx context.Context, name string) error {
	_, err := c.run(ctx, herdrmock.Target{}, "session", "stop", name)
	return err
}

// DeleteSession deletes a stopped session. herdr is idempotent here: deleting
// a name that does not exist succeeds.
func (c *Client) DeleteSession(ctx context.Context, name string) error {
	_, err := c.run(ctx, herdrmock.Target{}, "session", "delete", name)
	return err
}

func (c *Client) run(ctx context.Context, target herdrmock.Target, args ...string) ([]byte, error) {
	bin, err := binary(c.Bin)
	if err != nil {
		return nil, err
	}
	run := c.Run
	if run == nil {
		run = execRunner
	}
	stdout, stderr, err := run(ctx, bin, withSocket(os.Environ(), target.Socket), args...)
	if err != nil {
		if message := strings.TrimSpace(string(stderr)); message != "" {
			return nil, fmt.Errorf("herdr %s: %w: %s", strings.Join(args, " "), err, message)
		}
		return nil, fmt.Errorf("herdr %s: %w", strings.Join(args, " "), err)
	}
	return stdout, nil
}

// withSocket returns env with HERDR_SOCKET_PATH pointing at socket, replacing
// any value already there. An empty socket leaves env alone: herdr then talks to
// the session this process already runs against.
func withSocket(env []string, socket string) []string {
	if socket == "" {
		return env
	}
	prefix := EnvSocketPath + "="
	resolved := make([]string, 0, len(env)+1)
	for _, entry := range env {
		if !strings.HasPrefix(entry, prefix) {
			resolved = append(resolved, entry)
		}
	}
	return append(resolved, prefix+socket)
}

func execRunner(ctx context.Context, bin string, env []string, args ...string) ([]byte, []byte, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}

// Execer replaces the current process with "herdr --session <name>", the only
// way to attach: herdr forbids nested launches, so the host shell has to
// become the herdr client.
type Execer struct {
	Bin string
}

// Attach replaces this process with a herdr client attached to name. It
// returns only when the replacement fails.
func (e Execer) Attach(name string) error {
	bin, err := binary(e.Bin)
	if err != nil {
		return err
	}
	return execReplace(bin, []string{bin, "--session", name}, os.Environ())
}

var execReplace = syscall.Exec

func binary(explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	if path := os.Getenv(EnvBinary); path != "" {
		return path, nil
	}
	path, err := exec.LookPath("herdr")
	if err != nil {
		return "", fmt.Errorf("herdr is required; install it or set %s", EnvBinary)
	}
	return path, nil
}
