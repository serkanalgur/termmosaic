package buffer

import "github.com/serkanalgur/termmosaic/geometry"

// Rect is an axis-aligned rectangle in cell coordinates. Aliased from geometry
// so that buffer users get one type rather than two.
type Rect = geometry.Rect

// Size is a terminal width and height in cells. Aliased from geometry for the
// same reason as Rect.
type Size = geometry.Size

// Buffer is a width-by-height grid of Cell, row-major, plus the dirty-rectangle
// accumulator the renderer consumes.
//
// Cells live in a single flat slice so a row is one contiguous span of memory.
// That contiguity is what makes the diff's byte-wise row skip sound and fast
// (ADR 0002).
type Buffer struct {
	w, h int
	// stride is the number of cells between the start of consecutive rows in
	// cx. It equals w for a top-level buffer. A sub-buffer views a rectangle of
	// a wider parent, whose rows are strided, so its cells are NOT contiguous
	// in memory — which means the byte-wise row skip is only sound for a
	// top-level buffer. The renderer only ever diffs top-level buffers.
	stride int
	cx     []Cell // cells, row-major, stride*h long

	// dirty accumulates invalidation rectangles. A sub-buffer shares its
	// parent's set so invalidation raised inside a composed region is not lost.
	dirty *dirtySet
	// dx, dy translate this buffer's coordinates into the owner's when marking
	// dirty. Non-zero only for sub-buffers.
	dx, dy int

	// scratch is a reusable rectangle slice so draining dirty state does not
	// allocate per frame.
	scratch []Rect
}

// NewBuffer returns a buffer of w by h cells, all set to DefaultCell.
func NewBuffer(w, h int) *Buffer {
	if w < 0 {
		w = 0
	}
	if h < 0 {
		h = 0
	}
	b := &Buffer{w: w, h: h, stride: w, dirty: &dirtySet{}}
	b.cx = make([]Cell, w*h)
	b.Clear()
	b.MarkAllDirty()
	return b
}

// Size returns the buffer's dimensions in cells.
func (b *Buffer) Size() (w, h int) { return b.w, b.h }

// Width returns the buffer's width in cells.
func (b *Buffer) Width() int { return b.w }

// Height returns the buffer's height in cells.
func (b *Buffer) Height() int { return b.h }

// Cells returns the backing cell slice, row-major. It is exposed for the diff
// and for tests; callers must not resize the buffer while holding it.
func (b *Buffer) Cells() []Cell { return b.cx }

// Clear resets every cell to DefaultCell without marking anything dirty. It is
// the buffer-level reset; use ClearRect for a region.
func (b *Buffer) Clear() {
	for i := range b.cx {
		b.cx[i] = DefaultCell
	}
}

// ClearRect resets every cell in r to DefaultCell and marks r dirty.
func (b *Buffer) ClearRect(r Rect) {
	r = r.Clip(b.w, b.h)
	for y := r.Y; y < r.Bottom(); y++ {
		row := b.row(y)
		for x := r.X; x < r.Right(); x++ {
			row[x] = DefaultCell
		}
	}
	b.MarkDirty(r)
}

// Resize changes the buffer to w by h, discarding all cells. It marks the whole
// buffer dirty: the old contents are gone, so the region is not comparable
// against the previous frame.
func (b *Buffer) Resize(w, h int) {
	if w < 0 {
		w = 0
	}
	if h < 0 {
		h = 0
	}
	if w == b.w && h == b.h {
		return
	}
	b.w, b.h = w, h
	b.stride = w
	b.cx = make([]Cell, w*h)
	b.Clear()
	b.MarkAllDirty()
}

// row returns the y-th row as a slice aliasing the backing store. For a
// sub-buffer the returned slice has length w but consecutive rows are stride
// apart in memory.
func (b *Buffer) row(y int) []Cell {
	return b.cx[y*b.stride : y*b.stride+b.w]
}

// at returns a pointer to the cell at (x, y), honouring the stride. Callers
// must have already bounds-checked.
func (b *Buffer) at(x, y int) *Cell {
	return &b.cx[y*b.stride+x]
}

// SetCell writes c at (x, y). Coordinates outside the buffer, and writes to a
// continuation cell, are silently ignored: the buffer must never be corrupted
// by an out-of-range widget, because a TUI that panics on a layout rounding
// error is unusable.
func (b *Buffer) SetCell(x, y int, c Cell) {
	if x < 0 || y < 0 || x >= b.w || y >= b.h {
		return
	}
	*b.at(x, y) = c
}

// CellAt returns the cell at (x, y). Out-of-range coordinates return
// DefaultCell, and a continuation cell is returned as it is stored (call
// IsContinuation to detect it) so that a test asserting on a wide glyph can see
// the second half.
func (b *Buffer) CellAt(x, y int) Cell {
	if x < 0 || y < 0 || x >= b.w || y >= b.h {
		return DefaultCell
	}
	return *b.at(x, y)
}

// Set is a convenience wrapper for writing a styled rune at (x, y).
func (b *Buffer) Set(x, y int, r rune, fg, bg Colour, attr Attr) {
	b.SetCell(x, y, NewCell(r, fg, bg, attr))
}

// SetString writes s starting at (x, y) and returns the x coordinate just past
// the last cell written.
//
// It is wide-character aware: a double-width rune consumes two cells, and the
// second is written as a continuation cell so that the pair compares equal to
// the previous frame and the row skip still fires. A rune that would straddle
// the right edge is not written at all, because there is no partial glyph.
//
// Writing a continuation cell directly is impossible; SetString owns that
// state.
func (b *Buffer) SetString(x, y int, s string, fg, bg Colour, attr Attr) int {
	if y < 0 || y >= b.h {
		return x
	}
	cx := x
	for _, r := range s {
		w := RuneWidth(r)
		switch w {
		case 0:
			// Zero-width marks carry no cell of their own and are dropped.
			// See RuneWidth: grapheme composition is not implemented.
			continue
		case 2:
			// A double-width glyph must have both of its cells available. If
			// only one remains, the glyph is not written at all: half a glyph
			// is worse than none, and the row would otherwise be left with an
			// unpaired cell that differs from the previous frame forever.
			if cx+1 >= b.w {
				return cx
			}
			if cx >= 0 {
				*bufCell(b, cx, y) = NewCell(r, fg, bg, attr)
				*bufCell(b, cx+1, y) = NewCell(continuationRune, fg, bg, attr).asContinuation()
			}
			cx += 2
		default:
			if cx >= b.w {
				return cx
			}
			if cx >= 0 {
				*bufCell(b, cx, y) = NewCell(r, fg, bg, attr)
			}
			cx++
		}
	}
	return cx
}

// bufCell returns a pointer to (x, y) without bounds checking, for use from
// SetString which has already validated the coordinates. It exists so the wide
// path is stride-correct on sub-buffers too.
func bufCell(b *Buffer, x, y int) *Cell { return b.at(x, y) }

// Fill writes c to every cell in the buffer and marks the whole buffer dirty.
func (b *Buffer) Fill(c Cell) {
	for i := range b.cx {
		b.cx[i] = c
	}
	b.MarkAllDirty()
}

// FillRect writes c to every cell in r and marks r dirty. Coordinates are
// clipped to the buffer.
func (b *Buffer) FillRect(r Rect, c Cell) {
	r = r.Clip(b.w, b.h)
	for y := r.Y; y < r.Bottom(); y++ {
		row := b.row(y)
		for x := r.X; x < r.Right(); x++ {
			row[x] = c
		}
	}
	b.MarkDirty(r)
}

// SubBuffer returns a view of the region (x, y, w, h) of b, sharing b's cell
// storage rather than copying it. Writes through the view are visible in b and
// vice versa, which is what makes layout composition cheap.
//
// A view's rows are strided rather than contiguous, so the byte-wise row skip
// is not sound over it. The renderer only diffs top-level buffers; compose with
// views, diff at the top.
//
// The view shares b's dirty accumulator with a coordinate offset, so marking
// the view dirty invalidates the right region of the parent. The view must not
// outlive the parent, and Resize on either is undefined while the other is
// live — the same contract as any slice alias.
func (b *Buffer) SubBuffer(x, y, w, h int) *Buffer {
	r := Rect{X: x, Y: y, W: w, H: h}.Clip(b.w, b.h)
	// cx must span whole *strided* rows of the parent, not the tightly packed
	// region a naive slice would give: row r.Y+1 of the parent starts at
	// (r.Y+1)*stride, not at r.Y*stride+r.W.
	sub := &Buffer{
		w:      r.W,
		h:      r.H,
		stride: b.stride,
		cx:     b.cx[r.Y*b.stride+r.X : (r.Y+r.H)*b.stride],
		dirty:  b.dirty,
		dx:     b.dx + r.X,
		dy:     b.dy + r.Y,
	}
	return sub
}

// Clip returns an independent copy of the region r of b. Unlike SubBuffer the
// result is detached: later writes to b are not visible, and the copy has its
// own dirty accumulator. Use it when a widget needs to compose content
// off-screen.
func (b *Buffer) Clip(r Rect) *Buffer {
	r = r.Clip(b.w, b.h)
	out := NewBuffer(r.W, r.H)
	for y := 0; y < r.H; y++ {
		copy(out.row(y), b.row(r.Y + y)[r.X:r.Right()])
	}
	out.dirty.reset()
	return out
}

// MarkDirty records r as needing to be redrawn. It is safe to call from any
// goroutine (ADR 0003).
func (b *Buffer) MarkDirty(r Rect) {
	if r.Empty() {
		return
	}
	b.dirty.mark(Rect{X: r.X + b.dx, Y: r.Y + b.dy, W: r.W, H: r.H})
}

// MarkAllDirty records the whole buffer as needing to be redrawn. It is safe to
// call from any goroutine.
func (b *Buffer) MarkAllDirty() {
	b.dirty.markAll()
}

// Invalidate is an alias for MarkAllDirty, spelled to match Widget.Invalidate.
func (b *Buffer) Invalidate() { b.MarkAllDirty() }

// IsDirty reports whether any region is awaiting a redraw. It is safe to call
// from any goroutine.
func (b *Buffer) IsDirty() bool { return !b.dirty.isEmpty() }

// TakeDirty returns the accumulated dirty rectangles, coalesced and clipped to
// the buffer, and resets the accumulator. The result aliases an internal
// scratch slice and is only valid until the next call to TakeDirty on the same
// buffer. Used by the renderer; widget code normally calls MarkDirty only.
func (b *Buffer) TakeDirty() []Rect {
	return b.dirty.takeInto(b.scratch[:0], b.w, b.h)
}

// swap exchanges the contents of b with other, including the dirty state. The
// renderer uses it to flip between its front and back buffers each frame so
// that no frame ever has to copy a whole screen.
func (b *Buffer) swap(other *Buffer) {
	b.w, b.h, b.stride, b.cx = other.w, other.h, other.stride, other.cx
	b.dx, b.dy = other.dx, other.dy
	b.scratch, other.scratch = other.scratch, b.scratch
	bs, os := b.dirty, other.dirty
	b.dirty, other.dirty = os, bs
}

// TakeDirtyInto moves the accumulated dirty rectangles into dst (which must be
// empty) and resets the accumulator, clipping to the buffer.
//
// dst's backing array is reused across frames by the renderer, so a steady-state
// frame does not allocate here. The returned slice aliases dst and is valid only
// until the next call on the same buffer.
func (b *Buffer) TakeDirtyInto(dst []Rect, w, h int) []Rect {
	if w == 0 || h == 0 {
		// A zero-sized buffer has no cells, so nothing can be dirty.
		b.dirty.reset()
		return dst[:0]
	}
	return b.dirty.takeInto(dst[:0], w, h)
}

// ClearDirty discards any accumulated invalidation without touching cells.
//
// The renderer uses it on the front buffer: the front buffer is by definition
// what the terminal is already showing, so nothing in it is outstanding.
func (b *Buffer) ClearDirty() { b.dirty.reset() }
