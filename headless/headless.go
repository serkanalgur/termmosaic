// Package headless provides a terminal that touches no real device and a sink
// that exposes the resulting cell buffer.
//
// ADR 0001 makes this a first-class v1 deliverable rather than a stretch goal,
// for a specific reason: tcell was rejected partly because its headless backend
// cannot expose the cell buffer, and the testability pillar depends entirely on
// being able to assert on cells. So MemorySink here does not merely log bytes —
// it maintains a real buffer.Buffer by interpreting the SGR, CUP, ED and EL
// sequences the renderer emits.
//
// That is what lets a widget test say `assert CellAt(4, 2) has foreground
// #30c080` instead of pattern-matching an escape-sequence string.
package headless

import (
	"errors"

	"io"
	"sync"

	"github.com/serkanalgur/termmosaic"
)

// Terminal is a terminal with no device behind it.
//
// It performs no syscalls, has no raw mode to enter or leave, and never touches
// stdin or stdout. Input is pushed in with Feed and resizes with SetSize, so a
// test drives the whole input and frame pipeline from Go code with no terminal
// and no platform-specific behaviour.
//
// Terminal satisfies termmosaic.Terminal.
type Terminal struct {
	mu      sync.Mutex
	w, h    int
	caps    termmosaic.Caps
	in      chan []byte
	resizes chan termmosaic.Size

	rawMode   bool
	altScreen bool
	closed    bool
	closeOnce sync.Once
	done      chan struct{}
}

var _ termmosaic.Terminal = (*Terminal)(nil)

// Option configures a headless Terminal.
type Option func(*Terminal)

// WithCaps overrides the capabilities a headless Terminal reports. The default
// is termmosaic.DefaultCaps; a test that wants to exercise the 256- or 16-colour
// degradation ladder sets it here.
func WithCaps(c termmosaic.Caps) Option {
	return func(t *Terminal) { t.caps = c }
}

// New returns a headless Terminal of the given size in cells.
//
// The input channel is buffered so Feed never blocks a test that does not drain
// it, and ResizeEvents is buffered so SetSize never blocks either. A test that
// wants to observe delivery order drains them.
func New(w, h int, opts ...Option) *Terminal {
	t := &Terminal{
		w:       w,
		h:       h,
		caps:    termmosaic.DefaultCaps(),
		in:      make(chan []byte, 64),
		resizes: make(chan termmosaic.Size, 64),
		done:    make(chan struct{}),
	}
	for _, o := range opts {
		o(t)
	}
	return t
}

// Size returns the terminal's current size in cells.
func (t *Terminal) Size() (w, h int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.w, t.h
}

// SetSize changes the terminal's reported size and queues a resize event.
//
// The event is queued rather than sent so SetSize never blocks a test that is
// not draining the channel; a dropped event on a full channel would silently
// lose a resize, so the channel is generously buffered and PushResize is the
// explicit, blocking alternative for tests that care.
func (t *Terminal) SetSize(w, h int) {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return
	}
	t.w, t.h = w, h
	t.mu.Unlock()
	t.PushResize(w, h)
}

// PushResize queues a resize event, blocking until there is room. Use it when a
// test must be certain the event was delivered.
func (t *Terminal) PushResize(w, h int) {
	t.mu.Lock()
	closed := t.closed
	t.mu.Unlock()
	if closed {
		return
	}
	select {
	case t.resizes <- termmosaic.Size{W: w, H: h}:
	case <-t.done:
	}
}

// EnterRawMode records that raw mode was requested. There is nothing to switch,
// but the state is tracked so a test can assert the renderer's setup sequence and
// so LeaveRawMode can be a genuine no-op.
func (t *Terminal) EnterRawMode() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return errClosed
	}
	t.rawMode = true
	return nil
}

// LeaveRawMode records that raw mode was left.
func (t *Terminal) LeaveRawMode() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.rawMode = false
	return nil
}

// RawMode reports whether raw mode is currently entered, so tests can assert the
// setup and teardown sequences.
func (t *Terminal) RawMode() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.rawMode
}

// EnterAltScreen records that the alternate screen was requested.
func (t *Terminal) EnterAltScreen() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return errClosed
	}
	t.altScreen = true
	return nil
}

// LeaveAltScreen records that the alternate screen was left.
func (t *Terminal) LeaveAltScreen() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.altScreen = false
	return nil
}

// AltScreen reports whether the alternate screen is currently active.
func (t *Terminal) AltScreen() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.altScreen
}

// Capabilities returns the configured capabilities.
func (t *Terminal) Capabilities() termmosaic.Caps {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.caps
}

// Read returns queued input bytes, blocking until some arrive or the terminal is
// closed. It returns io.EOF once closed, which is the contract
// termmosaic.Terminal specifies.
func (t *Terminal) Read(p []byte) (int, error) {
	select {
	case b, ok := <-t.in:
		if !ok {
			return 0, io.EOF
		}
		return copy(p, b), nil
	case <-t.done:
		return 0, io.EOF
	}
}

// Feed queues input bytes as though the user had typed them. It never blocks.
func (t *Terminal) Feed(p []byte) {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return
	}
	cp := make([]byte, len(p))
	copy(cp, p)
	t.mu.Unlock()

	select {
	case t.in <- cp:
	case <-t.done:
	}
}

// FeedString queues s as input bytes.
func (t *Terminal) FeedString(s string) { t.Feed([]byte(s)) }

// PendingInput reports how many unread input chunks are queued, so a test can
// wait for its input to be consumed.
func (t *Terminal) PendingInput() int { return len(t.in) }

// ResizeEvents returns the channel resize events are delivered on. It is closed
// when the terminal is closed.
func (t *Terminal) ResizeEvents() <-chan termmosaic.Size { return t.resizes }

// Close shuts the terminal down, unblocking any pending Read and closing the
// event channels. It is idempotent.
func (t *Terminal) Close() error {
	t.closeOnce.Do(func() {
		t.mu.Lock()
		t.closed = true
		t.rawMode = false
		t.altScreen = false
		t.mu.Unlock()
		close(t.done)
		close(t.in)
		close(t.resizes)
	})
	return nil
}

// Closed reports whether Close has been called.
func (t *Terminal) Closed() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.closed
}

// errClosed is returned by operations on a closed terminal.
var errClosed = errors.New("headless: terminal is closed")

// ErrClosed returns the error a closed headless Terminal reports. Exported so a
// test can assert on it with errors.Is rather than by string.
func ErrClosed() error { return errClosed }
