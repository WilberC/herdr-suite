package session

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
)

// Refusal reports an operation herdr-suite declines to perform: invalid input,
// a protected session, or a confirmation it cannot ask for. Callers report the
// reason and exit with the usage status.
type Refusal struct {
	Reason string
}

func (r Refusal) Error() string { return r.Reason }

// ErrCancelled reports a destructive action the user declined at the
// confirmation prompt.
var ErrCancelled = errors.New("cancelled")

// Sanitize turns a typed name into a herdr session name: whitespace runs
// collapse to one '-', surrounding '-' are dropped, and the result may only
// hold letters, digits, '.', '_' and '-'. The old herdr-sessionizer applied the
// same two rules before a name reached herdr.
func Sanitize(raw string) (string, error) {
	var name strings.Builder
	pendingDash := false
	for _, r := range raw {
		if unicode.IsSpace(r) {
			pendingDash = name.Len() > 0
			continue
		}
		if pendingDash {
			name.WriteByte('-')
			pendingDash = false
		}
		name.WriteRune(r)
	}

	sanitized := strings.Trim(name.String(), "-")
	if sanitized == "" {
		return "", Refusal{Reason: "session name is empty"}
	}
	for _, r := range sanitized {
		if !validNameRune(r) {
			return "", Refusal{Reason: fmt.Sprintf(
				"invalid session name '%s'; use letters, digits, '.', '_' and '-'",
				sanitized,
			)}
		}
	}
	return sanitized, nil
}

func validNameRune(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z':
		return true
	case r >= 'A' && r <= 'Z':
		return true
	case r >= '0' && r <= '9':
		return true
	}
	return r == '.' || r == '_' || r == '-'
}
