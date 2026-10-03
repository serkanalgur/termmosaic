package buffer

import "testing"

func TestBufferBasics(t *testing.T) {
	b := NewBuffer(3, 2)
	if w, h := b.Size(); w != 3 || h != 2 {
		t.Fatalf("Size() = %d,%d want 3,2", w, h)
	}
	if got := b.CellAt(0, 0); got != DefaultCell {
		t.Fatalf("fresh cell = %+v, want DefaultCell", got)
	}
	b.Set(1, 1, 'x', NewColour(1, 2, 3), NewColour(4, 5, 6), AttrBold)
	got := b.CellAt(1, 1)
	want := NewCell('x', NewColour(1, 2, 3), NewColour(4, 5, 6), AttrBold)
	if got != want {
		t.Fatalf("CellAt = %+v, want %+v", got, want)
	}
	// Out of range must not panic or write.
	b.Set(-1, 0, 'a', DefaultColour, DefaultColour, 0)
	b.Set(0, -1, 'a', DefaultColour, DefaultColour, 0)
	b.Set(99, 0, 'a', DefaultColour, DefaultColour, 0)
	if got := b.CellAt(-1, 0); got != DefaultCell {
		t.Errorf("out-of-range CellAt = %+v, want DefaultCell", got)
	}
}

func TestFillAndClearRect(t *testing.T) {
	b := NewBuffer(4, 4)
	c := NewCell('#', NewColour(9, 9, 9), DefaultColour, 0)
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
		next := b.SetString(0, 0, "hello", DefaultColour, DefaultColour, 0)
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
		next := b.SetString(0, 0, "e\u0301", DefaultColour, DefaultColour, 0)
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
		next := b.SetString(0, 0, "漢", fg, DefaultColour, 0)
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
		b.SetString(0, 0, "漢", fg, DefaultColour, 0)
		if b.CellAt(1, 0) != first {
			t.Error("continuation cell is not stable across identical writes")
		}
	})

	t.Run("wide glyph straddling the edge is not written", func(t *testing.T) {
		// Width 2: 'a' takes cell 0, leaving one cell for a two-cell glyph.
		b := NewBuffer(2, 1)
		b.SetString(0, 0, "a漢", DefaultColour, DefaultColour, 0)
		if b.CellAt(0, 0).Rune() != 'a' {
			t.Errorf("cell 0 = %q, want 'a'", b.CellAt(0, 0).Rune())
		}
		if b.CellAt(1, 0) != DefaultCell {
			t.Errorf("cell 1 = %+v, want untouched DefaultCell (no half-written glyph)", b.CellAt(1, 0))
		}
	})

	t.Run("out of range rows are no-ops", func(t *testing.T) {
		b := NewBuffer(4, 1)
		if got := b.SetString(0, 5, "x", DefaultColour, DefaultColour, 0); got != 0 {
			t.Errorf("returned %d, want 0", got)
		}
	})

	t.Run("negative start x skips off-screen runes", func(t *testing.T) {
		b := NewBuffer(4, 1)
		b.SetString(-2, 0, "abcd", DefaultColour, DefaultColour, 0)
		if b.CellAt(0, 0).Rune() != 'c' || b.CellAt(1, 0).Rune() != 'd' {
			t.Errorf("got %q,%q want c,d", b.CellAt(0, 0).Rune(), b.CellAt(1, 0).Rune())
		}
	})
}

func TestResizeDiscardsAndMarksDirty(t *testing.T) {
	b := NewBuffer(4, 4)
	b.TakeDirty()
	b.Set(0, 0, 'x', DefaultColour, DefaultColour, 0)
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
	sub.Set(1, 1, 'z', DefaultColour, DefaultColour, 0)
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

func TestClipIsDetached(t *testing.T) {
	parent := NewBuffer(4, 4)
	parent.Set(1, 1, 'q', DefaultColour, DefaultColour, 0)
	c := parent.Clip(Rect{X: 1, Y: 1, W: 2, H: 2})
	if c.CellAt(0, 0).Rune() != 'q' {
		t.Error("Clip must copy the region")
	}
	c.Set(0, 0, 'w', DefaultColour, DefaultColour, 0)
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
	a.Set(0, 0, 'a', DefaultColour, DefaultColour, 0)
	b.Set(1, 1, 'b', DefaultColour, DefaultColour, 0)
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
