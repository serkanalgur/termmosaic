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
//   - Input decoding is NOT one of them any more. Terminal.Read returns raw bytes
//     and that is the whole of this package's job with input: the
//     input package turns those bytes into termmosaic.Event values, and it is a
//     separate package precisely so that the decoding is a pure function over a
//     byte slice rather than a state machine buried in the one package whose I/O
//     path is untested. Key encoding, legacy xterm modifiers, the kitty keyboard
//     protocol, mouse encoding (SGR 1006, urxvt 1015, legacy X10), bracketed
//     paste, focus events and the escape-delay rule all live there. See ADR 0005.
//   - tmux / screen DCS passthrough (ESC P tmux; ... ESC \). Deferred by ADR 0005
//     and a REAL gap, not an oversight: without it a TermMosaic program under
//     tmux on a modern terminal can lose key and mouse reporting. The input
//     decoder CONSUMES a DCS wrapper so the stream stays in sync, but it does not
//     unwrap one. The fix attaches as a pre-dispatch step in the input parser,
//     which is why the parser's entry point is the byte stream.
//   - Windows. See terminal_windows.go; it returns ErrUnsupported. ADR 0005 does
//     not help: the Windows console delivers KEY_EVENT and MOUSE_EVENT records
//     rather than escape sequences, so Windows input is a second decoder behind
//     the same Event union, not a solved problem.
//   - Capability probing beyond the environment heuristics in caps.go. There is
//     no terminfo, per ADR 0001. Note that Caps.KittyKeyboard means "may
//     support": negotiation state lives on input.Source.
//   - Closing the tty file itself. Close restores raw mode and leaves the
//     alternate screen, but it deliberately does not close os.Stdin, which is not
//     this package's to close. A consequence is that a goroutine blocked in
//     Terminal.Read on a live tty does not return when Close is called. See
//     input.Source.Close, which waits for its reader and says so.
//
// UNVERIFIED: nothing in this package's I/O path is exercised by the test suite,
// because it needs a real tty. INPUT IS NOW DIFFERENT, and the distinction
// matters. Input DECODING is covered exhaustively by the input package's unit
// tests — every sequence form, every split-read case, every mouse encoding — but
// it has still never run against a real tty, because a pure function over bytes
// proves that our decoding is right and proves nothing about what terminals emit.
// The ioctl, signal and raw-mode handling here remain unverified too; what the
// tests in this package do cover is the pure logic (caps.go, the sink, and the
// resize coalescing policy in terminal_unix_test.go). Treat the transport as
// unverified until someone runs examples/hello against several terminals — which
// is exactly why that example exists and why its golden test does not go through
// here.
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
