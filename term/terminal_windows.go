//go:build windows

package term

import (
	"errors"
	"os"
	"sync"

	"github.com/serkanalgur/termmosaic"
)

// WindowsTerminal is a stub.
//
// ADR 0001 is explicit that Windows is late and that the Windows console's
// handling is "a distinct and fiddly problem", listing it as the
// highest-probability source of v1 slippage with a build-tagged tcell backend as
// the v1.0 escape hatch. Rather than ship a half-working Windows console that
// passes its own tests on a developer machine, this stub fails every operation
// loudly so the gap stays visible.
//
// The EscapeHatch note: if Windows support is required before v1.0, the
// mitigation ADR 0001 proposes is a build-tagged tcell backend for Windows only,
// keeping termmosaic.Terminal and termmosaic.Sink intact. The interfaces are the
// seam that makes that possible, and they are unchanged.
type WindowsTerminal struct {
	mu      sync.Mutex
	in, out *os.File
	caps    termmosaic.Caps
	resizes chan termmosaic.Size
	closed  bool
}

var _ termmosaic.Terminal = (*WindowsTerminal)(nil)

// errWindows is what every unsupported operation reports.
var errWindows = errors.New("termmosaic: the Windows console backend is not implemented; see ADR 0001")

// ErrWindowsStub returns the error the Windows stub reports, so callers can
// detect it with errors.Is rather than by string.
func ErrWindowsStub() error { return errWindows }

// Open returns a stub terminal. It succeeds so that a program can start and
// report a readable error, rather than failing with a message the user cannot
// act on.
func Open(in, out *os.File, getenv func(string) string) (*WindowsTerminal, error) {
	// The same nil checks as the Unix backend. Open is one API with two
	// implementations, so it must reject the same inputs on both; the Windows
	// stub is a stub for console *operations*, not for argument validation, and
	// validating nothing here made the shared test in term_test.go fail on
	// Windows while passing everywhere else.
	if in == nil {
		return nil, errors.New("termmosaic: nil input file")
	}
	if out == nil {
		return nil, errors.New("termmosaic: nil output file")
	}
	if getenv == nil {
		getenv = os.Getenv
	}
	return &WindowsTerminal{
		in:      in,
		out:     out,
		caps:    DetectCaps(getenv),
		resizes: make(chan termmosaic.Size, 1),
	}, nil
}

// Size returns a conventional size; the stub cannot query the console.
func (t *WindowsTerminal) Size() (w, h int) { return 80, 25 }

// EnterRawMode is unsupported.
func (t *WindowsTerminal) EnterRawMode() error { return errWindows }

// LeaveRawMode is unsupported.
func (t *WindowsTerminal) LeaveRawMode() error { return errWindows }

// EnterAltScreen is unsupported.
func (t *WindowsTerminal) EnterAltScreen() error { return errWindows }

// LeaveAltScreen is unsupported.
func (t *WindowsTerminal) LeaveAltScreen() error { return errWindows }

// Capabilities returns the detected capabilities, which are still meaningful
// even though nothing can be drawn with them.
func (t *WindowsTerminal) Capabilities() termmosaic.Caps { return t.caps }

// Read is unsupported.
func (t *WindowsTerminal) Read(p []byte) (int, error) { return 0, errWindows }

// ResizeEvents returns a channel that is never written to.
func (t *WindowsTerminal) ResizeEvents() <-chan termmosaic.Size { return t.resizes }

// Close is supported, and closes the resize channel.
func (t *WindowsTerminal) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return nil
	}
	t.closed = true
	close(t.resizes)
	return nil
}
