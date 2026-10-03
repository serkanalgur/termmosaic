//go:build !windows

package term

import (
	"errors"
	"io"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/internal/ansi"
	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

// UnixTerminal is a termmosaic.Terminal driving a tty file descriptor.
type UnixTerminal struct {
	mu     sync.Mutex
	tty    *os.File
	out    *os.File
	caps   termmosaic.Caps
	getenv func(string) string

	savedState *term.State
	raw        bool
	alt        bool
	closed     bool

	resizes   chan termmosaic.Size
	sigwinch  chan os.Signal
	done      chan struct{}
	closeOnce sync.Once
}

var _ termmosaic.Terminal = (*UnixTerminal)(nil)

// Open opens the controlling terminal.
//
// in is the file to read input from and out the file to write frames to;
// normally both are os.Stdin and os.Stdout, but passing os.Stderr for out is
// common so that shell redirection does not capture the UI. getenv defaults to
// os.Getenv and is injectable so capability detection can be tested.
func Open(in, out *os.File, getenv func(string) string) (*UnixTerminal, error) {
	if in == nil {
		return nil, errors.New("termmosaic: nil input file")
	}
	if out == nil {
		return nil, errors.New("termmosaic: nil output file")
	}
	if getenv == nil {
		getenv = os.Getenv
	}
	t := &UnixTerminal{
		tty:      in,
		out:      out,
		getenv:   getenv,
		resizes:  make(chan termmosaic.Size, 8),
		sigwinch: make(chan os.Signal, 1),
		done:     make(chan struct{}),
	}
	t.caps = DetectCaps(getenv)
	t.resizes <- t.sizeOrDefault()
	signal.Notify(t.sigwinch, syscall.SIGWINCH)
	go t.watchResize()
	return t, nil
}

// sizeOrDefault returns the terminal size, falling back to 80x25 when the ioctl
// fails, which happens when the file is not a tty. A TUI that refuses to start
// because it cannot ask a pipe its size is worse than one that starts at a
// conventional default.
func (t *UnixTerminal) sizeOrDefault() termmosaic.Size {
	w, h, err := t.size()
	if err != nil {
		return termmosaic.Size{W: 80, H: 25}
	}
	return termmosaic.Size{W: w, H: h}
}

// size reads the window size with the TIOCGWINSZ ioctl.
func (t *UnixTerminal) size() (w, h int, err error) {
	ws, err := unix.IoctlGetWinsize(int(t.tty.Fd()), unix.TIOCGWINSZ)
	if err != nil {
		return 0, 0, err
	}
	if ws.Col == 0 || ws.Row == 0 {
		return 0, 0, unix.ENOTTY
	}
	return int(ws.Col), int(ws.Row), nil
}

// Size returns the terminal's current size in cells.
func (t *UnixTerminal) Size() (w, h int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	s := t.sizeOrDefault()
	return s.W, s.H
}

// EnterRawMode puts the tty into raw mode, saving the previous state so
// LeaveRawMode can restore it exactly.
//
// A second call is a no-op. The state is saved once, not twice, because
// x/term's Restore is only meaningful against the state captured before the
// first MakeRaw.
func (t *UnixTerminal) EnterRawMode() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return ErrUnsupported
	}
	if t.raw {
		return nil
	}
	fd := int(t.tty.Fd())
	if !term.IsTerminal(fd) {
		return ErrNotTTY
	}
	st, err := term.MakeRaw(fd)
	if err != nil {
		return err
	}
	t.savedState = st
	t.raw = true
	return nil
}

// LeaveRawMode restores the terminal's mode. It is safe to call when raw mode was
// never entered.
func (t *UnixTerminal) LeaveRawMode() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.raw || t.savedState == nil {
		t.raw = false
		return nil
	}
	err := term.Restore(int(t.tty.Fd()), t.savedState)
	t.raw = false
	t.savedState = nil
	return err
}

// RawMode reports whether raw mode is currently entered.
func (t *UnixTerminal) RawMode() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.raw
}

// EnterAltScreen switches to the alternate screen buffer and hides the cursor.
//
// The cursor is saved first so LeaveAltScreen can put it back where the shell
// left it, rather than wherever the UI happened to leave it.
func (t *UnixTerminal) EnterAltScreen() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return ErrUnsupported
	}
	if t.alt {
		return nil
	}
	if _, err := t.out.WriteString(ansi.SaveCursor + ansi.EnterAltScreen + ansi.EraseDisplay + ansi.HideCursor); err != nil {
		return err
	}
	t.alt = true
	return nil
}

// LeaveAltScreen returns to the main screen buffer and restores the cursor.
func (t *UnixTerminal) LeaveAltScreen() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.alt {
		return nil
	}
	_, err := t.out.WriteString(ansi.ShowCursor + ansi.RestoreCursor + ansi.LeaveAltScreen)
	t.alt = false
	return err
}

// AltScreen reports whether the alternate screen is active.
func (t *UnixTerminal) AltScreen() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.alt
}

// Capabilities returns the detected capabilities.
func (t *UnixTerminal) Capabilities() termmosaic.Caps {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.caps
}

// Read reads input bytes from the tty. It returns io.EOF once closed.
func (t *UnixTerminal) Read(p []byte) (int, error) {
	n, err := t.tty.Read(p)
	if err != nil {
		if errors.Is(err, os.ErrClosed) {
			return 0, io.EOF
		}
		return n, err
	}
	return n, nil
}

// ResizeEvents returns the channel resize events are delivered on.
func (t *UnixTerminal) ResizeEvents() <-chan termmosaic.Size { return t.resizes }

// watchResize converts SIGWINCH notifications into size events.
func (t *UnixTerminal) watchResize() {
	for {
		select {
		case <-t.done:
			return
		case <-t.sigwinch:
			t.mu.Lock()
			s := t.sizeOrDefault()
			t.mu.Unlock()
			select {
			case t.resizes <- s:
			case <-t.done:
				return
			default:
				// The channel is full: the consumer is not keeping up and a
				// stale size is worth less than the latest one, so drop this
				// notification rather than block the signal goroutine.
			}
		}
	}
}

// Close restores raw mode, leaves the alternate screen and stops the resize
// watcher. It is idempotent, because a terminal left in raw mode is a broken
// shell and being called twice must not make that worse.
func (t *UnixTerminal) Close() error {
	var err error
	t.closeOnce.Do(func() {
		t.mu.Lock()
		t.closed = true
		wasRaw, state := t.raw, t.savedState
		wasAlt := t.alt
		t.raw, t.savedState, t.alt = false, nil, false
		t.mu.Unlock()

		if wasAlt {
			_, e := t.out.WriteString(ansi.ShowCursor + ansi.RestoreCursor + ansi.LeaveAltScreen)
			err = errors.Join(err, e)
		}
		if wasRaw && state != nil {
			err = errors.Join(err, term.Restore(int(t.tty.Fd()), state))
		}
		signal.Stop(t.sigwinch)
		close(t.done)
		close(t.resizes)
	})
	return err
}
