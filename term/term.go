// Package term implements the real terminal backend: a termmosaic.Terminal
// driving a tty and a termmosaic.Sink writing to it.
//
// It exists because ADR 0001's whole point is that TermMosaic owns the terminal
// layer rather than wrapping one, and that ownership has a price ADR 0001 states
// plainly: "we now own the terminal-quirk surface". What is implemented here is
// the part that cannot be deferred — size, raw mode, the alternate screen,
// resize — because nothing works without them.
//
// What is NOT implemented here, deliberately, and is a visible gap:
//
//   - Input decoding. Terminal.Read returns raw bytes and nothing turns them
//     into termmosaic.Event values. The escape-sequence surface — key encoding,
//     mouse encoding (SGR 1006, urxvt 1015, legacy X10), bracketed paste,
//     paste coalescing, tmux DCS passthrough — is the bulk of the quirk list
//     ADR 0001 warns about, and it wants its own decision and its own tests
//     rather than being sketched in alongside the transport.
//   - Windows. See terminal_windows.go; it returns ErrUnsupported.
//   - Capability probing beyond the environment heuristics in caps.go. There is
//     no terminfo, per ADR 0001.
//
// UNVERIFIED: nothing in this package's I/O path is exercised by the test suite,
// because it needs a real tty. What is tested is the pure logic (caps.go, the
// sink) and the build. Treat the ioctl, signal and raw-mode handling as
// unverified until someone runs an example against a terminal — which is exactly
// why examples/hello exists and why the golden test does not go through here.
package term

import (
	"errors"
	"io"

	"github.com/serkanalgur/termmosaic"
)

// ErrUnsupported is returned by operations a platform or terminal cannot
// support, such as raw mode on a non-tty file.
var ErrUnsupported = errors.New("termmosaic: operation unsupported by this terminal")

// ErrNotTTY is returned when an operation needs a terminal but the file is not
// one, for example when stdout is a pipe.
var ErrNotTTY = errors.New("termmosaic: not a terminal")

// NewSink returns a Sink writing to w.
//
// Writes are unbuffered on purpose. The renderer already produces one complete
// frame per Write, and a further buffer would only add a flush point that has
// to be got right for the latency of a partial frame. It also keeps the byte
// stream a Sink receives and the bytes a terminal sees identical, which is what
// lets a test swap in a MemorySink and get the same result.
func NewSink(w io.Writer) termmosaic.Sink { return &fileSink{w: w} }

type fileSink struct {
	w io.Writer
}

func (s *fileSink) Write(p []byte) (int, error) { return s.w.Write(p) }

// Flush is a no-op: there is no intermediate buffer to push.
func (s *fileSink) Flush() error { return nil }
