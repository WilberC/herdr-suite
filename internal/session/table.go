package session

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/WilberC/herdr-suite/internal/herdrmock"
)

// SessionRow is one entry of the JSON session list. The field names mirror
// what "herdr session list --json" reports, so a consumer can treat both
// documents the same way.
type SessionRow struct {
	Name    string `json:"name"`
	Running bool   `json:"running"`
	Default bool   `json:"default"`
}

// ListDocument is the document "hs session list --json" prints.
type ListDocument struct {
	Sessions []SessionRow `json:"sessions"`
}

// WriteTable prints the session table: a header row and one row per session,
// in the order given, with the name column padded to the widest name. The
// table is the stable, scriptable form: names are never colored and the state
// column always starts after the widest name.
func WriteTable(w io.Writer, sessions []herdrmock.Session) error {
	width := len("NAME")
	for _, session := range sessions {
		if n := len([]rune(session.Name)); n > width {
			width = n
		}
	}

	if _, err := fmt.Fprintf(w, "%s  %s\n", padName("NAME", width), "STATE"); err != nil {
		return err
	}
	for _, session := range sessions {
		if _, err := fmt.Fprintf(w, "%s  %s\n", padName(session.Name, width), state(session)); err != nil {
			return err
		}
	}
	return nil
}

// WriteJSON prints the session list as JSON, indented for reading.
func WriteJSON(w io.Writer, sessions []herdrmock.Session) error {
	document := ListDocument{Sessions: make([]SessionRow, 0, len(sessions))}
	for _, session := range sessions {
		document.Sessions = append(document.Sessions, SessionRow{
			Name:    session.Name,
			Running: session.Running,
			Default: session.Default,
		})
	}

	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(document)
}

func state(session herdrmock.Session) string {
	label := "stopped"
	if session.Running {
		label = "running"
	}
	if session.Default {
		label += " (default)"
	}
	return label
}

func padName(name string, width int) string {
	padding := width - len([]rune(name))
	if padding < 0 {
		padding = 0
	}
	return name + strings.Repeat(" ", padding)
}
