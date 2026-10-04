package buffer

import (
	"testing"
	"unsafe"
)

func TestBufferBasics(t *testing.T) {
	b := NewBuffer(3, 2)
	if w, h := b.Size(); w != 3 || h != 2 {
		t.Fatalf("Size() = %d,%d want 3,2", w, h)
	}
	if got := b.CellAt(0, 0); got != DefaultCell {
		t.Fatalf("fresh cell = %+v, want DefaultCell", got)
	}
	b.Set(1, 1, 'x', NewStyle(NewColour(1, 2, 3), NewColour(4, 5, 6), AttrBold))
	got := b.CellAt(1, 1)
	want := NewCell('x', NewStyle(NewColour(1, 2, 3), NewColour(4, 5, 6), AttrBold))
	if got != want {
		t.Fatalf("CellAt = %+v, want %+v", got, want)
	}
	// Out of range must not panic or write.
	b.Set(-1, 0, 'a', DefaultStyle)
	b.Set(0, -1, 'a', DefaultStyle)
	b.Set(99, 0, 'a', DefaultStyle)
	if got := b.CellAt(-1, 0); got != DefaultCell {
		t.Errorf("out-of-range CellAt = %+v, want DefaultCell", got)
	}
}

func TestFillAndClearRect(t *testing.T) {
	b := NewBuffer(4, 4)
	c := NewCell('#', NewStyle(NewColour(9, 9, 9), DefaultColour, 0))
	b.FillRect(Rect{X: 1, Y: 1, W: 2, H: 2}, c)
	if b.CellAt(1, 1) != c || b.CellAt(2, 2) != c {
		t.Error("FillRect did not fill")
	}
	if b.CellAt(0, 0) != DefaultCell || b.CellAt(3, 3) != DefaultCell {
		t.Error("FillRect wrote outside the rect")
	}
	// Clipping: a rect straddling the edge must be clipped, not panic.
	b.FillRect(Rect{X: -2, Y: -2, W: 4, H: 4}, c)
	if b.CellAt(0, 0) != c || b.CellAt(1, 1) != c {
		t.Error("clipped FillRect did not fill the overlap")
	}
	b.ClearRect(Rect{X: 1, Y: 1, W: 2, H: 2})
	if b.CellAt(1, 1) != DefaultCell {
		t.Error("ClearRect did not clear")
	}
	b.Fill(c)
	if b.CellAt(3, 3) != c {
		t.Error("Fill did not fill")
	}
	b.Clear()
	if b.CellAt(3, 3) != DefaultCell {
		t.Error("Clear did not clear")
	}
}

func TestSetString(t *testing.T) {
	t.Run("ascii", func(t *testing.T) {
		b := NewBuffer(10, 1)
		next := b.SetString(0, 0, "hello", DefaultStyle)
		if next != 5 {
			t.Errorf("returned %d, want 5", next)
		}
		for i, r := range "hello" {
			if got := b.CellAt(i, 0).Rune(); got != r {
				t.Errorf("cell %d = %q, want %q", i, got, r)
			}
		}
	})

	t.Run("zero-width marks are dropped", func(t *testing.T) {
		b := NewBuffer(10, 1)
		// A base 'e' plus combining acute: one grapheme, two runes, one cell.
		next := b.SetString(0, 0, "e\u0301", DefaultStyle)
		if next != 1 {
			t.Errorf("returned %d, want 1 (combining mark must not occupy a cell)", next)
		}
		if b.CellAt(0, 0).Rune() != 'e' || b.CellAt(1, 0) != DefaultCell {
			t.Error("combining mark handling is wrong")
		}
	})

	t.Run("wide glyph occupies two cells", func(t *testing.T) {
		b := NewBuffer(10, 1)
		fg := NewColour(255, 0, 0)
		next := b.SetString(0, 0, "漢", NewStyle(fg, DefaultColour, 0))
		if next != 2 {
			t.Errorf("returned %d, want 2", next)
		}
		left := b.CellAt(0, 0)
		if left.Rune() != '漢' || left.IsContinuation() {
			t.Errorf("left cell = %+v, want the glyph itself", left)
		}
		right := b.CellAt(1, 0)
		if !right.IsContinuation() {
			t.Error("second cell must be a continuation cell")
		}
		if right.FG != fg {
			t.Errorf("continuation FG = %v, want %v (style must match its left half)", right.FG, fg)
		}
		// The stability property: re-writing the same glyph yields an equal
		// second cell, so the row skip fires and nothing flickers.
		first := b.CellAt(1, 0)
		b.SetString(0, 0, "漢", NewStyle(fg, DefaultColour, 0))
		if b.CellAt(1, 0) != first {
			t.Error("continuation cell is not stable across identical writes")
		}
	})

	t.Run("wide glyph straddling the edge is not written", func(t *testing.T) {
		// Width 2: 'a' takes cell 0, leaving one cell for a two-cell glyph.
		b := NewBuffer(2, 1)
		b.SetString(0, 0, "a漢", DefaultStyle)
		if b.CellAt(0, 0).Rune() != 'a' {
			t.Errorf("cell 0 = %q, want 'a'", b.CellAt(0, 0).Rune())
		}
		if b.CellAt(1, 0) != DefaultCell {
			t.Errorf("cell 1 = %+v, want untouched DefaultCell (no half-written glyph)", b.CellAt(1, 0))
		}
	})

	t.Run("out of range rows are no-ops", func(t *testing.T) {
		b := NewBuffer(4, 1)
		if got := b.SetString(0, 5, "x", DefaultStyle); got != 0 {
			t.Errorf("returned %d, want 0", got)
		}
	})

	t.Run("negative start x skips off-screen runes", func(t *testing.T) {
		b := NewBuffer(4, 1)
		b.SetString(-2, 0, "abcd", DefaultStyle)
		if b.CellAt(0, 0).Rune() != 'c' || b.CellAt(1, 0).Rune() != 'd' {
			t.Errorf("got %q,%q want c,d", b.CellAt(0, 0).Rune(), b.CellAt(1, 0).Rune())
		}
	})
}

func TestResizeDiscardsAndMarksDirty(t *testing.T) {
	b := NewBuffer(4, 4)
	b.TakeDirty()
	b.Set(0, 0, 'x', DefaultStyle)
	b.TakeDirty()

	b.Resize(2, 2)
	if w, h := b.Size(); w != 2 || h != 2 {
		t.Fatalf("Size() = %d,%d want 2,2", w, h)
	}
	if got := b.CellAt(3, 3); got != DefaultCell {
		t.Error("resized buffer should be cleared")
	}
	rects := b.TakeDirty()
	if len(rects) != 1 || rects[0] != (Rect{W: 2, H: 2}) {
		t.Fatalf("TakeDirty after resize = %+v, want one full rect", rects)
	}

	// Same-size resize is a no-op and must not invalidate.
	b.TakeDirty()
	b.Resize(2, 2)
	if b.IsDirty() {
		t.Error("no-op resize must not invalidate")
	}

	// Negative dimensions are clamped, not panics.
	b.Resize(-5, -5)
	if w, h := b.Size(); w != 0 || h != 0 {
		t.Fatalf("Size() = %d,%d want 0,0", w, h)
	}
}

func TestSubBufferIsAViewAndForwardsDirty(t *testing.T) {
	parent := NewBuffer(10, 10)
	sub := parent.SubBuffer(2, 3, 4, 5)

	if sub.Width() != 4 || sub.Height() != 5 {
		t.Fatalf("sub size = %dx%d, want 4x5", sub.Width(), sub.Height())
	}
	sub.Set(1, 1, 'z', DefaultStyle)
	if got := parent.CellAt(3, 4); got.Rune() != 'z' {
		t.Error("writes through the sub-buffer must be visible in the parent")
	}

	// Dirty raised in sub-buffer coordinates must land on the parent.
	parent.TakeDirty()
	sub.MarkDirty(Rect{X: 0, Y: 0, W: 2, H: 2})
	rects := parent.TakeDirty()
	if len(rects) != 1 || rects[0] != (Rect{X: 2, Y: 3, W: 2, H: 2}) {
		t.Fatalf("parent dirty = %+v, want {2,3,2,2}", rects)
	}

	// Out-of-bounds sub-requests are clipped, not fatal.
	edge := parent.SubBuffer(8, 8, 100, 100)
	if edge.Width() != 2 || edge.Height() != 2 {
		t.Errorf("clipped sub size = %dx%d, want 2x2", edge.Width(), edge.Height())
	}
}

// TestSubBufferRowsAreStrided pins the SECOND precondition of ADR 0002's
// byte-wise row comparison.
//
// Padding-free Cell is necessary but not sufficient: the byte range handed to
// bytes.Equal must also be contiguous, and that is a property of the caller,
// not of the type. TestCellHasNoPadding cannot cover it, because no type-level
// test can. This bug shipped once already -- SubBuffer originally returned a
// naively contiguous slice, which satisfied the padding invariant and was still
// an unsound byte compare, because a row-major sub-rectangle is strided.
//
// The rule the code now enforces: byte-wise row comparison is sound on
// top-level buffers only. Sub-buffers are strided and must never be
// byte-compared; the renderer composes with views and diffs at the top.
func TestSubBufferRowsAreStrided(t *testing.T) {
	const cellSize = int(unsafe.Sizeof(Cell{}))

	// A top-level buffer's rows ARE contiguous, so the byte compare is sound.
	top := NewBuffer(8, 4)
	if got, want := rowGap(top, 0, 1), cellSize*top.Width(); got != want {
		t.Errorf("top-level row gap = %d bytes, want %d; top-level rows must be contiguous", got, want)
	}

	// A sub-buffer's rows are NOT contiguous, so the same byte compare would be
	// reading across the parent's row boundary.
	parent := NewBuffer(10, 10)
	sub := parent.SubBuffer(2, 3, 4, 5)
	if got, want := rowGap(sub, 0, 1), cellSize*sub.Width(); got == want {
		t.Errorf("a sub-buffer's rows are contiguous (%d bytes); they should be strided by the parent", got)
	}
	if got, want := rowGap(sub, 0, 1), cellSize*parent.Width(); got != want {
		t.Errorf("sub row gap = %d bytes, want %d (the parent's stride)", got, want)
	}

	// The striding is observable: a cell written at sub-buffer row 1 lands in
	// parent row 4, not immediately after sub-buffer row 0.
	sub.Set(0, 1, 'z', DefaultStyle)
	if got := parent.CellAt(2, 4).Rune(); got != 'z' {
		t.Errorf("parent cell (2,4) = %q, want 'z'; the view is not strided as documented", got)
	}

	// The same property asserted through the PUBLIC accessor callers actually
	// hold. Row is the surface that replaced the removed flat Cells(), so this
	// is the assertion that pins the invariant where it can be violated again.
	for y := 0; y < sub.Height(); y++ {
		if got := len(sub.Row(y)); got != sub.Width() {
			t.Errorf("sub.Row(%d) has length %d, want Width() = %d", y, got, sub.Width())
		}
	}
	if got, want := rowGap(sub, 0, 1), cellSize*parent.Width(); got != want {
		t.Errorf("public Row gap = %d bytes, want %d (the parent's stride)", got, want)
	}
}

// TestRowIsStrideCorrectOnSubBuffer is the correctness half of ADR 0006: Row is
// the only bulk accessor, so it must be right on a view. Reading sub.Row(y)[x]
// must give the parent's cell at (x0+x, y0+y) — not the cell a flat y*w+x index
// would reach, which is the bug the removed Cells() invited.
func TestRowIsStrideCorrectOnSubBuffer(t *testing.T) {
	const parentW, parentH = 11, 7
	parent := NewBuffer(parentW, parentH)
	for y := 0; y < parentH; y++ {
		for x := 0; x < parentW; x++ {
			parent.Set(x, y, rune('a'+x), DefaultStyle)
		}
	}

	const x0, y0, w, h = 3, 2, 5, 4
	sub := parent.SubBuffer(x0, y0, w, h)

	for y := 0; y < h; y++ {
		row := sub.Row(y)
		for x := 0; x < w; x++ {
			want := parent.CellAt(x0+x, y0+y)
			if got := row[x]; got != want {
				t.Fatalf("sub.Row(%d)[%d] = %+v, want the parent's (%d,%d) cell %+v",
					y, x, got, x0+x, y0+y, want)
			}
		}
	}

	// And a write through Row lands in the parent, which is the other half of
	// "a view shares storage rather than copying it".
	sub.Row(1)[2] = NewCell('Z', NewStyle(NewColour(0xff, 0, 0), DefaultColour, AttrBold))
	if got := parent.CellAt(x0+2, y0+1).Rune(); got != 'Z' {
		t.Errorf("write through sub.Row(1)[2] landed at rune %q, want 'Z'", got)
	}
}

// TestRowOnSubBufferReturnsNonAdjacentSlices pins the residual hole ADR 0006
// records as risk 2: Row is correct per row, and there is no type-level canary
// for a caller that concatenates a view's rows as though they were adjacent.
// This is the bug that already shipped once (SubBuffer's original contiguous
// slice), so the property is asserted rather than left to a doc comment.
func TestRowOnSubBufferReturnsNonAdjacentSlices(t *testing.T) {
	const cellSize = int(unsafe.Sizeof(Cell{}))
	parent := NewBuffer(10, 6)
	sub := parent.SubBuffer(1, 1, 4, 3)

	gap := int(uintptr(unsafe.Pointer(&sub.Row(1)[0])) - uintptr(unsafe.Pointer(&sub.Row(0)[0])))
	if want := cellSize * parent.Width(); gap != want {
		t.Errorf("consecutive view rows are %d bytes apart, want %d: they must not be adjacent", gap, want)
	}
	// The distance a caller would wrongly assume: back-to-back cells.
	if gap == cellSize*sub.Width() {
		t.Error("a view's rows are contiguous; the striding that RowBytes refuses no longer exists")
	}

	// On a top-level buffer the same two rows ARE adjacent, which is what makes
	// the byte-wise row compare sound there.
	top := NewBuffer(10, 6)
	if gap := int(uintptr(unsafe.Pointer(&top.Row(1)[0])) - uintptr(unsafe.Pointer(&top.Row(0)[0]))); gap != cellSize*top.Width() {
		t.Errorf("top-level row gap = %d, want %d", gap, cellSize*top.Width())
	}
}

// TestRowOutOfRangeIsNil pins the property Row's doc comment promises, because
// it is what makes `for _, c := range buf.Row(y)` safe for any y.
func TestRowOutOfRangeIsNil(t *testing.T) {
	b := NewBuffer(4, 3)
	for _, y := range []int{-1, -100, 3, 4, 1 << 30} {
		if got := b.Row(y); got != nil {
			t.Errorf("Row(%d) = %v, want nil for an out-of-range row", y, got)
		}
	}
	if got := b.Row(2); got == nil || len(got) != b.Width() {
		t.Errorf("Row(Height()-1) = %v, want a full row of %d cells", got, b.Width())
	}
}

// rowGap returns the byte distance between the first cell of row a and the
// first cell of row b.
func rowGap(b *Buffer, a, c int) int {
	return int(uintptr(unsafe.Pointer(&b.Row(c)[0])) - uintptr(unsafe.Pointer(&b.Row(a)[0])))
}

func TestClipIsDetached(t *testing.T) {
	parent := NewBuffer(4, 4)
	parent.Set(1, 1, 'q', DefaultStyle)
	c := parent.Clip(Rect{X: 1, Y: 1, W: 2, H: 2})
	if c.CellAt(0, 0).Rune() != 'q' {
		t.Error("Clip must copy the region")
	}
	c.Set(0, 0, 'w', DefaultStyle)
	if parent.CellAt(1, 1).Rune() != 'q' {
		t.Error("Clip must be detached from the parent")
	}
	if c.IsDirty() {
		t.Error("a fresh Clip starts clean")
	}
}

func TestSwapExchangesContents(t *testing.T) {
	a := NewBuffer(2, 2)
	b := NewBuffer(2, 2)
	a.Set(0, 0, 'a', DefaultStyle)
	b.Set(1, 1, 'b', DefaultStyle)
	// swap moves other's cell storage into the receiver; the argument keeps its
	// own cells, because the renderer swaps by exchanging its own references.
	// Dirty state is exchanged, so invalidation follows the cells.
	a.swap(b)
	if a.CellAt(1, 1).Rune() != 'b' {
		t.Error("swap did not move b's content into a")
	}
	if a.CellAt(0, 0) != DefaultCell {
		t.Error("a still holds content that belonged to b's slot")
	}
	if b.CellAt(0, 0) != DefaultCell || b.CellAt(1, 1).Rune() != 'b' {
		t.Error("swap must not disturb the argument's cells")
	}
}
