package session

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/WilberC/herdr-suite/internal/herdrmock"
)

// Execer replaces the current process with a herdr client attached to a
// session. The real implementation calls syscall.Exec; tests fake it.
type Execer interface {
	Attach(name string) error
}

// Confirmer asks a yes/no question and reports the answer.
type Confirmer func(prompt string) (bool, error)

// Config carries the collaborators a Manager needs. Getenv, Out and Err fall
// back to the process environment and to discarding writes.
type Config struct {
	Client      herdrmock.SessionClient
	Exec        Execer
	Out         io.Writer
	Err         io.Writer
	Getenv      func(string) string
	Interactive bool
	AssumeYes   bool
	Confirmer   Confirmer
}

// Manager owns herdr session management: listing, attaching, creating,
// stopping, restarting and deleting sessions.
type Manager struct {
	config Config
}

// New returns a Manager over cfg.
func New(cfg Config) *Manager {
	if cfg.Getenv == nil {
		cfg.Getenv = os.Getenv
	}
	if cfg.Out == nil {
		cfg.Out = io.Discard
	}
	if cfg.Err == nil {
		cfg.Err = io.Discard
	}
	return &Manager{config: cfg}
}

// List returns the sessions herdr knows about, running ones first and each
// group ordered by name, so every surface shows the same stable order.
func (m *Manager) List(ctx context.Context) ([]herdrmock.Session, error) {
	sessions, err := m.config.Client.ListSessions(ctx)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	sort.SliceStable(sessions, func(i, j int) bool {
		if sessions[i].Running != sessions[j].Running {
			return sessions[i].Running
		}
		return sessions[i].Name < sessions[j].Name
	})
	return sessions, nil
}

// Stop stops a running session. A session that is already stopped, or that
// herdr does not know, is reported and left alone: herdr has nothing to stop.
func (m *Manager) Stop(ctx context.Context, raw string) error {
	name, err := Sanitize(raw)
	if err != nil {
		return err
	}
	running, err := m.running(ctx, name)
	if err != nil {
		return err
	}
	if !running {
		fmt.Fprintf(m.config.Err, "session '%s' is already stopped\n", name)
		return nil
	}
	if err := m.confirm(stopPrompt(name)); err != nil {
		return err
	}
	if err := m.config.Client.StopSession(ctx, name); err != nil {
		return fmt.Errorf("stop '%s': %w", name, err)
	}
	fmt.Fprintf(m.config.Out, "stopped session '%s'\n", name)
	return nil
}

// Delete stops the session when it is running, then deletes it: herdr only
// deletes stopped sessions. The default session cannot be deleted.
func (m *Manager) Delete(ctx context.Context, raw string) error {
	name, err := Sanitize(raw)
	if err != nil {
		return err
	}
	sessions, err := m.List(ctx)
	if err != nil {
		return err
	}
	found, known := find(sessions, name)
	if known && found.Default {
		return Refusal{Reason: fmt.Sprintf("'%s' is the default session and cannot be deleted", name)}
	}
	if err := m.confirm(deletePrompt(name)); err != nil {
		return err
	}
	if known && found.Running {
		if err := m.config.Client.StopSession(ctx, name); err != nil {
			return fmt.Errorf("stop '%s': %w", name, err)
		}
	}
	if err := m.config.Client.DeleteSession(ctx, name); err != nil {
		return fmt.Errorf("delete '%s': %w", name, err)
	}
	fmt.Fprintf(m.config.Out, "deleted session '%s'\n", name)
	return nil
}

// Restart stops a running session so the next attach starts it fresh: herdr
// has no session start command, and a fresh server re-reads its config and
// plugin manifests. When flagPath is set, the interactive switcher's restart
// handoff, the name is written there and the caller attaches it after the
// switcher exits. Otherwise the stop is all a CLI can do, so it reports how to
// attach the session again.
func (m *Manager) Restart(ctx context.Context, raw, flagPath string) error {
	name, err := Sanitize(raw)
	if err != nil {
		return err
	}
	running, err := m.running(ctx, name)
	if err != nil {
		return err
	}
	if !running {
		fmt.Fprintf(m.config.Err, "session '%s' is already stopped\n", name)
		return nil
	}
	if err := m.confirm(restartPrompt(name)); err != nil {
		return err
	}
	if err := m.config.Client.StopSession(ctx, name); err != nil {
		return fmt.Errorf("stop '%s': %w", name, err)
	}
	if flagPath != "" {
		if err := os.WriteFile(flagPath, []byte(name), 0o600); err != nil {
			return fmt.Errorf("write restart flag: %w", err)
		}
	}
	fmt.Fprintf(m.config.Out, "stopped session '%s'\n", name)
	if flagPath == "" {
		fmt.Fprintf(m.config.Out, "attach it again with: hs attach %s\n", name)
	}
	return nil
}

// Attach replaces this process with a herdr client attached to raw, sanitized
// first. Herdr creates the session on attach, so a session that does not exist
// yet is the same call. Herdr forbids nested launches, so this refuses inside
// a herdr pane.
func (m *Manager) Attach(raw string) error {
	if m.config.Getenv("HERDR_ENV") != "" {
		return Refusal{Reason: "run it outside herdr; herdr forbids nested launches"}
	}
	name, err := Sanitize(raw)
	if err != nil {
		return err
	}
	if m.config.Exec == nil {
		return errors.New("attach is unavailable")
	}

	fmt.Fprintf(m.config.Out, "\033]2;herdr:%s\007", name)
	return m.config.Exec.Attach(name)
}

func (m *Manager) running(ctx context.Context, name string) (bool, error) {
	sessions, err := m.List(ctx)
	if err != nil {
		return false, err
	}
	found, known := find(sessions, name)
	return known && found.Running, nil
}

func (m *Manager) confirm(prompt string) error {
	if m.config.AssumeYes {
		return nil
	}
	if !m.config.Interactive || m.config.Confirmer == nil {
		return Refusal{Reason: prompt + "; pass --yes to run it non-interactively"}
	}
	answer, err := m.config.Confirmer(prompt)
	if err != nil {
		return err
	}
	if !answer {
		return ErrCancelled
	}
	return nil
}

// TerminalConfirmer asks on the terminal and accepts y or yes in either case.
// Anything else, including a bare enter, cancels.
func TerminalConfirmer(in io.Reader, out io.Writer) Confirmer {
	return func(prompt string) (bool, error) {
		if _, err := fmt.Fprintf(out, "%s [y/N] ", prompt); err != nil {
			return false, err
		}
		line, err := bufio.NewReader(in).ReadString('\n')
		if err != nil && line == "" {
			return false, nil
		}
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "y", "yes":
			return true, nil
		}
		return false, nil
	}
}

func find(sessions []herdrmock.Session, name string) (herdrmock.Session, bool) {
	for _, session := range sessions {
		if session.Name == name {
			return session, true
		}
	}
	return herdrmock.Session{}, false
}

func stopPrompt(name string) string {
	return fmt.Sprintf("stop '%s'? its agents will be killed", name)
}

func restartPrompt(name string) string {
	return fmt.Sprintf("restart '%s'? its agents will be killed", name)
}

func deletePrompt(name string) string {
	return fmt.Sprintf("delete '%s'? it will be stopped first", name)
}
