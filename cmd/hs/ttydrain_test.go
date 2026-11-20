package main

import (
	"errors"
	"io"
	"os"
	"testing"
	"time"
)

// fakeTty feeds drainTerminalReplies chunk by chunk and then reports the
// silence a real poll-backed file reports: os.ErrDeadlineExceeded.
type fakeTty struct {
	chunks [][]byte
}

func (f *fakeTty) Read(p []byte) (int, error) {
	if len(f.chunks) == 0 {
		return 0, os.ErrDeadlineExceeded
	}
	chunk := f.chunks[0]
	f.chunks = f.chunks[1:]
	n := copy(p, chunk)
	if n < len(chunk) {
		panic("test chunk larger than the read buffer")
	}
	return n, nil
}

func (f *fakeTty) SetReadDeadline(time.Time) error { return nil }

func TestDrainTerminalRepliesConsumesWholeReplies(t *testing.T) {
	f := &fakeTty{chunks: [][]byte{
		[]byte("\x1b]10;rgb:c7c7/c7c7/c7c7\x1b\\"),
		[]byte("\x1b]4;7;rgb:8080/8080/8080\x1b\\"),
		[]byte("\x1b[?62;c"),
		[]byte("\x1b[1;1R"),
	}}
	got := drainTerminalReplies(f, time.Millisecond, time.Second)
	if want := 25 + 26 + 7 + 6; got != want {
		t.Fatalf("drainTerminalReplies consumed %d bytes, want %d", got, want)
	}
}

func TestDrainTerminalRepliesConsumesSplitReply(t *testing.T) {
	f := &fakeTty{chunks: [][]byte{
		[]byte("\x1b]4;7;rgb:80"),
		[]byte("80/8080/8080\x1b\\"),
	}}
	got := drainTerminalReplies(f, time.Millisecond, time.Second)
	if want := 26; got != want {
		t.Fatalf("drainTerminalReplies consumed %d bytes, want %d", got, want)
	}
}

func TestDrainTerminalRepliesStopsAtUserInput(t *testing.T) {
	f := &fakeTty{chunks: [][]byte{
		[]byte("\x1b]11;rgb:1c1c/1c1c/1c1c\x1b\\"),
		[]byte("ls -la\r"),
	}}
	got := drainTerminalReplies(f, time.Millisecond, time.Second)
	// The reply is consumed, and the first non-reply byte stops the drain:
	// its whole read chunk leaves the queue, the price of clearing what the
	// queries polluted.
	if want := 25 + len("ls -la\r"); got != want {
		t.Fatalf("drainTerminalReplies consumed %d bytes, want %d (reply + the read that stopped it)", got, want)
	}
}

func TestDrainTerminalRepliesEmptyQueueReturnsAtOnce(t *testing.T) {
	f := &fakeTty{}
	done := make(chan int, 1)
	go func() { done <- drainTerminalReplies(f, time.Millisecond, time.Second) }()
	select {
	case got := <-done:
		if got != 0 {
			t.Fatalf("drainTerminalReplies consumed %d bytes from an empty queue, want 0", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("drainTerminalReplies hung on an empty queue")
	}
}

func TestDrainTerminalRepliesStopsOnHardError(t *testing.T) {
	f := &errTty{}
	got := drainTerminalReplies(f, time.Millisecond, time.Second)
	if got != 0 {
		t.Fatalf("drainTerminalReplies consumed %d bytes before a hard error, want 0", got)
	}
}

type errTty struct{}

func (errTty) Read([]byte) (int, error)        { return 0, errors.New("gone") }
func (errTty) SetReadDeadline(time.Time) error { return nil }

var _ io.Reader = errTty{}
