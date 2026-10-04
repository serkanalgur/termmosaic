// Package diff implements TermMosaic's two-tier cell diff: a per-row skip on
// top of a per-cell pass.
//
// This is the single most important code in the project. ADR 0003's argument for
// the hybrid renderer is entirely this package's numbers, and ADR 0002's are a
// direct measurement of it: on a 200x60 scene that is 99% static chrome, the
// diff writes 141 bytes against 19,979 for a full repaint, a ~141x reduction.
//
// The two tiers:
//
//	Tier 1 — compare a whole row's cells as bytes with bytes.Equal. If the row
//	         is unchanged, skip it entirely. This is what makes static chrome
//	         free, and it depends on Cell being 16 bytes with no padding
//	         (see buffer.Cell and TestCellHasNoPadding).
//	Tier 2 — for a dirty row, walk cells. An SGR sequence is emitted only when
//	         fg, bg or attr actually differs from the previous cell written, and
//	         an unchanged cell breaks the cursor-movement run.
//
// The pass is zero-allocation: it appends into a byte slice owned by the
// Differ and reused across frames, and it allocates no per-cell or per-row
// temporaries. BenchmarkDiffDefaultScene asserts 0 allocs/op.
package diff

import (
	"bytes"
	"fmt"

	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/internal/ansi"
)

// Cursor is the cursor state diffed separately from the cell grid. A frame with
// no cell changes at all still has to move the cursor if it moved.
type Cursor struct {
	X, Y    int
	Visible bool
	Style   ansi.Style
	// Valid reports whether the cursor has ever been positioned. On the first
	// frame everything is emitted unconditionally, so this distinguishes
	// "cursor at 0,0" from "no cursor yet".
	Valid bool
}

// Frame is the input to one diff: the freshly drawn cells, the cells currently
// on screen, and the regions that changed.
type Frame struct {
	// Cur and Prev are the new and previously-rendered cell grids. Both must be
	// top-level buffers (stride == Width) of exactly Width by Height: Diff panics
	// otherwise, and buffer.RowBytes panics on a view as the backstop. Carrying
	// the buffers rather than flat []Cell slices is what makes that rule
	// enforceable — a slice carries no stride to check the flat index against,
	// which is how ADR 0002's original unsound byte compare shipped (ADR 0006).
	Cur, Prev *buffer.Buffer
	// Width and Height are the grid dimensions in cells. They are redundant with
	// Cur, deliberately: two sources of truth would be its own footgun, so Diff
	// checks them against both buffers at entry and panics on a mismatch.
	Width, Height int
	// Rects are the regions that changed. A nil or empty Rects means nothing
	// changed and Diff writes nothing.
	Rects []buffer.Rect
	// Cursor and PrevCursor are the new and previous cursor states.
	Cursor, PrevCursor Cursor
	// ForceFull ignores Rects and diffs every row. The renderer sets it after a
	// resize, where the previous frame is meaningless.
	ForceFull bool
	// Encoder selects the colour encoding.
	Encoder ansi.Encoder
}

// Differ renders Frames into byte slices, reusing its scratch buffer across
// frames. It is not safe for concurrent use: one Differ belongs to one renderer
// on one goroutine. That is the single-goroutine rule ADR 0003 documents, and
// here it is structural rather than advisory.
type Differ struct {
	// out is the byte scratch. It is handed to the caller, who must write it
	// to the Sink before the next Diff call. Callers who need to retain it
	// must copy.
	out []byte
	// style is the rendition state the diff believes the terminal is in,
	// carried across frames so the first changed cell of a frame only pays for
	// what actually changed. It is invalidated whenever the frame forces a
	// full repaint or changes depth.
	style ansi.Style
	// haveStyle reports whether style is meaningful.
	haveStyle bool
}

// NewDiffer returns a Differ with a scratch buffer of at least n bytes.
func NewDiffer(n int) *Differ {
	if n < 1024 {
		n = 1024
	}
	return &Differ{out: make([]byte, 0, n)}
}

// Scratch returns the Differ's byte buffer, for sizing.
func (d *Differ) Scratch() []byte { return d.out }

// Diff appends the byte stream that turns Prev into Cur to the Differ's scratch
// buffer and returns it. The returned slice aliases the Differ's storage and is
// only valid until the next Diff call.
//
// Writing zero bytes is a normal, expected outcome: a frame in which nothing is
// dirty must not touch the terminal at all, which is the property the frame
// pacer depends on to stay idle.
//
// It panics if the Frame is not internally consistent — a nil Cur or Prev, or a
// Width or Height that disagrees with either buffer. This is framework-internal
// wiring, so a mismatch means the renderer handed the diff two different grids,
// which is a bug worth failing loudly rather than silently mis-indexing. The
// check runs once per call, not per cell or per row.
func (d *Differ) Diff(f Frame) []byte {
	checkFrame(f)
	d.out = d.out[:0]

	rects := f.Rects
	if f.ForceFull {
		rects = []buffer.Rect{{W: f.Width, H: f.Height}}
	}
	cellsChanged := false
	if len(rects) == 0 || f.Width <= 0 || f.Height <= 0 {
		// Nothing changed. Still handle a cursor-only change below.
		return d.diffCursor(f, false)
	}

	// A full repaint or a change of colour depth invalidates our idea of the
	// terminal's current style; the next emitted cell must restate it.
	if f.ForceFull {
		d.haveStyle = false
	}

	enc := f.Encoder

	// When the previous frame is available, seed the tracked style from the
	// cursor style the previous frame left behind: that is genuinely the
	// terminal's state, since the previous frame ended by positioning the
	// cursor. On the first frame, or after a resize, we know nothing and the
	// first written cell must restate its style absolutely.
	if !f.ForceFull && f.PrevCursor.Valid {
		d.style = f.PrevCursor.Style
		d.haveStyle = true
	} else {
		d.haveStyle = false
	}

	// Track the last cell position written, so a run of adjacent cells costs
	// nothing instead of a cursor move per cell.
	lastX, lastY := -2, -2

	for _, r := range rects {
		r = r.Clip(f.Width, f.Height)
		if r.Empty() {
			continue
		}
		for y := r.Y; y < r.Bottom(); y++ {
			// ---- Tier 1: per-row skip -------------------------------------
			// One bytes.Equal over the row's contiguous memory. This is the
			// whole reason Cell is padding-free. RowBytes refuses a sub-buffer,
			// so this is also where "the frame's buffers must be top-level" is
			// enforced rather than assumed.
			if !f.ForceFull && bytes.Equal(
				f.Cur.RowBytes(y, 0, f.Width),
				f.Prev.RowBytes(y, 0, f.Width),
			) {
				continue
			}

			// ---- Tier 2: per-cell -----------------------------------------
			// The rows are hoisted out of the inner loop: one slice expression
			// per row instead of a multiply-add and a bounds check per cell
			// (ADR 0006).
			curRow, prevRow := f.Cur.Row(y), f.Prev.Row(y)
			for x := r.X; x < r.Right(); x++ {
				cur := curRow[x]

				if !f.ForceFull && cur == prevRow[x] {
					continue
				}

				// A continuation cell is written as part of its wide glyph.
				// The cell to its left already emitted the rune; emitting the
				// continuation's zero rune would clear the glyph. So skip it
				// and let the row scan continue.
				if cur.IsContinuation() {
					continue
				}

				// A run of adjacent cells needs no cursor move. The position
				// is tracked as lastX/lastY rather than assumed from the loop
				// index so that skipping an unchanged or continuation cell
				// correctly breaks the run.
				if x != lastX+1 || y != lastY {
					d.out = ansi.AppendCursorPosition(d.out, y+1, x+1)
				}

				style := cur.Style()
				if !d.haveStyle {
					// We do not know what the terminal is in, so restate the
					// whole style rather than guessing a delta.
					d.out = enc.AppendSGR(d.out, style)
					d.haveStyle = true
				} else if style != d.style {
					d.out = enc.AppendSGRDelta(d.out, d.style, style)
				}
				d.style = style

				ch := cur.Ch
				if ch == 0 {
					ch = ' '
				}
				d.out = ansi.AppendRune(d.out, ch)
				lastX, lastY = x, y
				cellsChanged = true
			}
		}
	}

	return d.diffCursor(f, cellsChanged)
}

// checkFrame panics unless f's two buffers are non-nil and both agree with
// f.Width and f.Height. It exists so the diff's precondition is checked once per
// call rather than assumed: a mismatch is a renderer bug, and a renderer bug that
// mis-indexes two different grids produces wrong pixels rather than a crash.
func checkFrame(f Frame) {
	if f.Cur == nil || f.Prev == nil {
		panic("diff: Frame.Cur and Frame.Prev must both be non-nil buffers")
	}
	if f.Cur.Width() != f.Width || f.Cur.Height() != f.Height {
		panic(fmt.Sprintf("diff: Frame is %dx%d but Cur is %dx%d",
			f.Width, f.Height, f.Cur.Width(), f.Cur.Height()))
	}
	if f.Prev.Width() != f.Width || f.Prev.Height() != f.Height {
		panic(fmt.Sprintf("diff: Frame is %dx%d but Prev is %dx%d",
			f.Width, f.Height, f.Prev.Width(), f.Prev.Height()))
	}
}

// diffCursor appends the cursor change, if any, and returns the result. It runs
// even for a no-op cell diff, because a frame can move or restyle the cursor
// without touching a single cell.
//
// The cursor is diffed separately from the cell grid because its state is not
// part of any row: there is no row to skip it with, and a cursor move on an
// otherwise-empty frame must still reach the terminal.
func (d *Differ) diffCursor(f Frame, cellsChanged bool) []byte {
	cur := f.Cursor
	prev := f.PrevCursor
	if !cur.Valid {
		// No cursor is being managed. If the previous frame had one visible,
		// leave the terminal as we found it.
		if prev.Valid && prev.Visible {
			d.out = ansi.AppendString(d.out, ansi.HideCursor)
		}
		return d.out
	}
	if cur == prev {
		return d.out
	}

	// Visibility is emitted before the position: hiding first means a frame
	// that both moves and hides cannot flash the cursor at the new position.
	if cur.Visible != prev.Visible || !prev.Valid {
		if cur.Visible {
			d.out = ansi.AppendString(d.out, ansi.ShowCursor)
		} else {
			d.out = ansi.AppendString(d.out, ansi.HideCursor)
		}
	}
	// The cursor's own style is terminal state and must be restated whenever
	// it differs, including when the frame wrote no cells, or the glyph would
	// be drawn in whatever style the last cell left behind.
	if cur.Style != prev.Style || !prev.Valid {
		if cellsChanged && d.haveStyle {
			d.out = f.Encoder.AppendSGRDelta(d.out, d.style, cur.Style)
		} else {
			d.out = f.Encoder.AppendSGR(d.out, cur.Style)
			d.haveStyle = true
		}
		d.style = cur.Style
	}
	if cur.X != prev.X || cur.Y != prev.Y || !prev.Valid {
		d.out = ansi.AppendCursorPosition(d.out, cur.Y+1, cur.X+1)
	}
	return d.out
}

// ResetStyle forgets the tracked terminal style, forcing the next emitted cell
// to restate its own. The renderer calls it when the terminal's colour depth
// changes.
func (d *Differ) ResetStyle() {
	d.haveStyle = false
	d.style = ansi.Style{}
}
