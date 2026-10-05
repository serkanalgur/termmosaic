package termmosaic

import "github.com/serkanalgur/termmosaic/buffer"

// Widget is a retained node in the UI tree, exactly as ADR 0003 specifies it.
//
// The four-method surface is the whole contract, and its smallness is the
// point: there is no reconciler, no virtual tree, no incremental-draw interface,
// and no message algebra. ADR 0003 argues each of those away explicitly — most
// importantly that requiring widget authors to implement correct incremental
// invalidation is a discipline Go cannot enforce, and that silent invalidation
// bugs are the worst class of bug a TUI can have.
//
// The trade, recorded in ADR 0003 and accepted: Draw runs for every widget every
// frame, cheap today (~81 µs for 12,000 cells) with no lever to pull if it ever
// stops being cheap. The dirty-rectangle accumulator is the hook an incremental
// API would attach to, when that day comes.
type Widget interface {
	// Bounds returns the widget's rectangle in screen cells. It must be safe to
	// call before the widget has ever been drawn.
	Bounds() Rect
	// Draw writes the widget's cells into buf, which is clipped to Bounds.
	// It is called every frame for every widget and is expected to be cheap
	// and idempotent: writing the same state twice must produce the same cells.
	Draw(buf *buffer.Buffer)
	// Invalidate marks Bounds dirty. It must be safe to call from any
	// goroutine (ADR 0003), which the buffer's dirty accumulator provides.
	Invalidate()
	// Handle offers the event to the widget and reports whether it consumed it.
	// Events reach a widget only after the application's keymap has declined
	// them (ADR 0009), so a binding in a keymap shadows a switch in this
	// widget. A widget's own key handling is therefore the fallback, not the
	// primary path, and a key that matters to both should be a command.
	Handle(Event) bool
}

// Minimizable is an optional interface a Widget implements if it has a smallest
// size at which it can render something meaningful.
//
// This is the existing Focusable pattern exactly: optional, so that adding it
// costs no widget anything, and discoverable by a type assertion. A widget that
// does not implement it is fully supported — there is no framework default
// minimum and no framework reaction (ADR 0007 §4).
//
// MinSize is the size of the WHOLE widget INCLUDING its own chrome — a bordered
// Table with MinSize{20, 5} needs 20x5 cells, not a 20x5 content area. Pinning
// this here is deliberate: three authors would otherwise each decide whether
// their border counts, and every caller's arithmetic would then be wrong for
// some subset of the catalog.
//
// The framework does nothing with a MinSize. What to do when the available
// space is below it is the application's decision, because only the application
// knows whether losing a table is acceptable (ADR 0007, rejected alternative:
// a framework-owned "too small" screen).
type Minimizable interface {
	Widget
	// MinSize returns the widget's smallest meaningful size in cells. It must be
	// pure, must not depend on the current Bounds, and must be safe to call before
	// the widget has ever been drawn.
	MinSize() Size
}

// Focusable is an optional interface a Widget implements if it can take focus.
// The renderer uses it to maintain focus order without requiring every widget to
// carry a focus method.
type Focusable interface {
	Widget
	// Focused reports whether the widget currently has focus.
	Focused() bool
	// SetFocused gives or removes focus. Implementations invalidate themselves.
	SetFocused(bool)
}
