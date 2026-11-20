package session

import (
	"fmt"
	"io"
	"strings"
)

// AltscreenAvailable reports whether the host terminal can swap to the
// alternate screen buffer, which is how a popup hands the pane back exactly as
// it found it. Detection stays capability-based: TERM must name a real
// terminal, because an empty or "dumb" value means the pane has no buffer to
// swap.
func AltscreenAvailable(term string) bool {
	term = strings.ToLower(strings.TrimSpace(term))
	return term != "" && term != "dumb"
}

// ClearResidue is the fallback for a terminal AltscreenAvailable refuses: it
// erases what the inline render left printed in the pane, the way ctrl+l
// clears a shell screen. Scrollback survives, matching ctrl+l.
func ClearResidue(out io.Writer) {
	if out == nil {
		return
	}
	fmt.Fprint(out, "\x1b[2J\x1b[H")
}
