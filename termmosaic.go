// Package termmosaic is the root of the TermMosaic TUI framework.
//
// It holds the two narrow interfaces every other package is written against —
// Terminal and Sink (ADR 0001) — plus the geometry, capability, event and widget
// types they traffic in. Nothing here talks to a real terminal: the concrete
// backends live in the headless and term packages, and the core depends only on
// these interfaces. That is what makes every widget CI-testable with no
// terminal, which ADR 0001 records as a first-class deliverable rather than a
// stretch goal.
//
// The import graph is a DAG with no cycles:
//
//	buffer   leaf: Cell, Colour, Attr, Buffer, geometry
//	layout   leaf: the pure constraint solver (ADR 0004)
//	termmosaic        this package: Terminal, Sink, Caps, Event, Widget
//	headless  headless Terminal + MemorySink
//	render    the hybrid renderer (ADR 0003)
package termmosaic

import "github.com/serkanalgur/termmosaic/buffer"

// Rect is an axis-aligned rectangle in cell coordinates. Aliased from buffer so
// that a layout result can be handed to a widget's Bounds() without conversion.
type Rect = buffer.Rect

// Size is a terminal width and height in cells. Aliased from buffer for the same
// reason as Rect.
type Size = buffer.Size

// Terminal controls the physical terminal.
//
// Implementations are the real x/sys-backed terminal and a headless fake for
// CI. Nothing in the core depends on which: the renderer depends only on Sink,
// and widget tests depend only on Terminal plus Sink (ADR 0001).
type Terminal interface {
	// Size returns the terminal's current size in cells.
	Size() (w, h int)
	// EnterRawMode disables line buffering and echo. It is not idempotent:
	// implementations must track their own state and make a second call a
	// no-op, because an unbalanced LeaveRawMode leaves the user's shell broken.
	EnterRawMode() error
	// LeaveRawMode restores the terminal's mode on entry.
	LeaveRawMode() error
	// EnterAltScreen switches to the alternate screen buffer.
	EnterAltScreen() error
	// LeaveAltScreen switches back to the main screen buffer.
	LeaveAltScreen() error
	// Capabilities reports what the terminal supports, which selects both the
	// colour-degradation rung and the box-drawing character set.
	Capabilities() Caps
	// Read reads input bytes. It returns io.EOF when the terminal is closed.
	Read(p []byte) (int, error)
	// ResizeEvents yields a value whenever the terminal is resized. The channel
	// is closed when the terminal is.
	ResizeEvents() <-chan Size
	// Close releases the terminal. It must leave raw mode and the alternate
	// screen even if the caller forgot to.
	Close() error
}

// Sink consumes the bytes the diff produces.
//
// The renderer writes here and knows nothing else about its output. Two
// implementations exist in v1: the real terminal's writer, and a MemorySink that
// records frames for tests (ADR 0001).
type Sink interface {
	// Write appends bytes to the sink. A short write with a nil error is
	// treated as an error by the renderer.
	Write(p []byte) (int, error)
	// Flush pushes buffered bytes to the underlying device.
	Flush() error
}

// Caps reports what a terminal supports.
//
// Detection is a single point of truth so the truecolor->256->16 and
// Unicode->ASCII degradation ladders have one home, which ADR 0001 calls out as
// a direct benefit of owning the terminal layer. TermMosaic has no terminfo
// database; it sniffs a fixed, well-chosen subset, which ADR 0001 records as a
// deliberate accepted limitation.
type Caps struct {
	// TrueColor reports support for SGR 38;2 and 48;2.
	TrueColor bool
	// Color256 reports support for the xterm-256 palette.
	Color256 bool
	// Unicode reports whether box-drawing and block glyphs render correctly,
	// as opposed to mojibake or blank cells on a misconfigured locale.
	Unicode bool
	// Mouse reports support for mouse reporting.
	Mouse bool
	// KittyKeyboard reports support for the kitty keyboard protocol, which
	// gives unambiguous key events for keys xterm cannot encode.
	KittyKeyboard bool
	// BracketedPaste reports support for bracketed paste, which is what makes
	// a multi-line paste one event instead of several keystrokes.
	BracketedPaste bool
}

// ColourDepth returns the rung of the degradation ladder these capabilities
// select.
func (c Caps) ColourDepth() buffer.ColourDepth {
	switch {
	case c.TrueColor:
		return buffer.DepthTrueColor
	case c.Color256:
		return buffer.Depth256
	default:
		return buffer.Depth16
	}
}

// DefaultCaps returns the capabilities of a modern, well-configured terminal.
// A headless Terminal starts here; a real one starts from probing and degrades.
func DefaultCaps() Caps {
	return Caps{
		TrueColor:      true,
		Color256:       true,
		Unicode:        true,
		Mouse:          true,
		KittyKeyboard:  true,
		BracketedPaste: true,
	}
}
