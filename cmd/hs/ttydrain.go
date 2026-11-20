package main

import (
	"io"
	"os"
	"time"

	"github.com/charmbracelet/x/term"
	"github.com/mattn/go-isatty"
	"golang.org/x/sys/unix"
)

// A TUI asks the terminal about itself — the palette queries the theme makes,
// and the capability probes the TUI library makes — and every answer that
// arrives after its reader stopped waits in the tty input queue. The shell
// that resumes when hs exits then reads those bytes as if the user had typed
// them. settleStdin drains that residue: after an interactive picker closes,
// it briefly owns the terminal again and discards the pending replies, so
// nothing the queries provoked ever reaches the shell.
//
// The drain stops at the first byte that is not part of a reply, because that
// byte is a keystroke: the read that carried it is lost, which is the price
// of clearing the queue the queries polluted.
const (
	replySilence  = 20 * time.Millisecond
	replyDrainCap = 500 * time.Millisecond
)

// settleStdin drains pending terminal replies from stdin, nothing more. It
// touches the terminal only when stdin is one, and leaves it as it found it.
func settleStdin() {
	f := os.Stdin
	if !isatty.IsTerminal(f.Fd()) {
		return
	}
	state, err := term.MakeRaw(f.Fd())
	if err != nil {
		return
	}
	defer term.Restore(f.Fd(), state)
	drainTerminalReplies(&pollReader{f: f, fd: int(f.Fd())}, replySilence, replyDrainCap)
}

// pollReader reads a tty through poll(2): the kernel bounds the wait, because
// the Go poller cannot — a deadline set on an inherited fd is refused, and no
// deadline interrupts a read already blocked on a tty. Polling for readiness
// before each read makes "no data within the deadline" a return, not a hang.
type pollReader struct {
	f        *os.File
	fd       int
	deadline time.Time
}

// SetReadDeadline records the deadline the drain loop asks for; the poll
// timeout enforces it.
func (p *pollReader) SetReadDeadline(t time.Time) error {
	p.deadline = t
	return nil
}

// Read waits for the tty to hold data until the recorded deadline, then reads
// what is there. A deadline passed with no data reports os.ErrDeadlineExceeded,
// the same silence the drain loop understands.
func (p *pollReader) Read(b []byte) (int, error) {
	ms := time.Until(p.deadline).Milliseconds()
	if ms < 0 {
		ms = 0
	}
	if ms > 1000 {
		ms = 1000
	}
	ready, err := unix.Poll([]unix.PollFd{{Fd: int32(p.fd), Events: unix.POLLIN}}, int(ms))
	if err != nil {
		return 0, err
	}
	if ready == 0 {
		return 0, os.ErrDeadlineExceeded
	}
	return p.f.Read(b)
}

// drainTerminalReplies reads from r and discards terminal replies — OSC
// sequences closed by BEL or ST, and CSI sequences closed by their final
// byte — until the reader goes silent for silence, until cap has elapsed, or
// until a byte arrives that no reply would hold. It returns how many bytes
// it took off the queue, including the whole read that carried the first
// keystroke byte, if one did.
func drainTerminalReplies(r interface {
	io.Reader
	SetReadDeadline(time.Time) error
}, silence, cap time.Duration) int {
	const (
		idle = iota
		inOSC
		inCSI
	)
	start := time.Now()
	state := idle
	consumed := 0
	buf := make([]byte, 256)
	for time.Since(start) < cap {
		r.SetReadDeadline(time.Now().Add(silence))
		n, err := r.Read(buf)
		for i := 0; i < n; i++ {
			b := buf[i]
			switch state {
			case inOSC:
				if b == 0x07 {
					state = idle
				} else if b == 0x1b && i+1 < n && buf[i+1] == '\\' {
					state = idle
					i++
				}
			case inCSI:
				if b >= 0x40 && b <= 0x7e {
					state = idle
				}
			default:
				if b == 0x1b {
					if i+1 < n {
						if buf[i+1] == ']' {
							state = inOSC
							i++
						} else if buf[i+1] == '[' {
							state = inCSI
							i++
						} else {
							return consumed + n
						}
					}
					// A lone ESC at the end of the chunk may continue in
					// the next read; hold the state and let it decide.
				} else {
					return consumed + n
				}
			}
		}
		consumed += n
		if err != nil {
			return consumed
		}
	}
	return consumed
}
