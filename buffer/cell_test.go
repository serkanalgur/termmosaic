package buffer

import (
	"strings"
	"testing"
	"unsafe"
)

// TestCellHasNoPadding is the mandatory guard ADR 0002 requires. It is
// deliberately named and placed so that anyone adding a field to Cell meets it
// immediately: if sizeof(Cell) changes, this fails and the byte-wise row skip
// in the diff is no longer sound.
//
// ADR 0002 notes this invariant is a footgun and accepts it on the grounds
// that a failing test is better than a silently flickering row. It is.
func TestCellHasNoPadding(t *testing.T) {
	if got := unsafe.Sizeof(Cell{}); got != 16 {
		t.Fatalf("sizeof(Cell) = %d, want 16; ADR 0002 requires a padding-free 16-byte cell", got)
	}
}

// TestCellHasNoInteriorPadding checks the field offsets individually, so a
// future reordering that happens to keep the total at 16 is caught too. Go
// inserts padding between fields to satisfy alignment; a layout like
// {rune, uint16, Colour} would silently waste bytes and reintroduce the
// hazard even though the total size might still be 16.
func TestCellHasNoInteriorPadding(t *testing.T) {
	var c Cell
	checks := []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{"Ch", unsafe.Offsetof(c.Ch), 0},
		{"FG", unsafe.Offsetof(c.FG), 4},
		{"BG", unsafe.Offsetof(c.BG), 8},
		{"Attr", unsafe.Offsetof(c.Attr), 12},
		{"flags", unsafe.Offsetof(c.flags), 14},
	}
	for _, tc := range checks {
		if tc.got != tc.want {
			t.Errorf("offsetof(Cell.%s) = %d, want %d (indicates interior padding)", tc.name, tc.got, tc.want)
		}
	}
}

// TestRowBytesAreContiguous is the property the row skip actually depends on:
// consecutive cells in the backing slice are adjacent in memory with no gaps.
func TestRowBytesAreContiguous(t *testing.T) {
	b := NewBuffer(4, 1)
	b.Set(0, 0, 'a', DefaultStyle)
	b.Set(1, 0, 'b', DefaultStyle)
	got := b.RowBytes(0, 0, 4)
	if len(got) != 64 {
		t.Fatalf("rowBytes len = %d, want 64", len(got))
	}
	if unsafe.Sizeof(Cell{}) != 16 {
		t.Fatal("cell size changed")
	}
}

// TestRowBytesPanicsOnSubBuffer is ADR 0006's structural refusal: RowBytes is a
// method on Buffer so that the old free-function call no longer compiles, and it
// panics on a view because a strided row has no correct byte range to hand back.
// A nil return would let bytes.Equal(nil, nil) report two different strided rows
// as equal.
func TestRowBytesPanicsOnSubBuffer(t *testing.T) {
	parent := NewBuffer(10, 10)
	sub := parent.SubBuffer(2, 3, 4, 5)

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("RowBytes on a sub-buffer must panic: its rows are not contiguous")
		}
		msg, ok := r.(string)
		if !ok {
			t.Fatalf("panic value is %T, want a string naming the alternative", r)
		}
		if !strings.Contains(msg, "Row") || !strings.Contains(msg, "SubBuffer") {
			t.Errorf("panic message does not name Row/SubBuffer as the alternative: %q", msg)
		}
	}()
	sub.RowBytes(0, 0, sub.Width())
}

// TestRowBytesOutOfRangeIsNil keeps the panic narrow: only a sub-buffer panics.
// An empty or out-of-range range on a top-level buffer is a normal answer,
// because the diff clips its rectangles before asking.
func TestRowBytesOutOfRangeIsNil(t *testing.T) {
	b := NewBuffer(4, 2)
	for _, tc := range []struct{ y, x0, n int }{
		{-1, 0, 4}, {0, -1, 4}, {2, 0, 4}, {0, 0, 0}, {0, 0, -1}, {0, 3, 2}, {0, 4, 1},
	} {
		if got := b.RowBytes(tc.y, tc.x0, tc.n); got != nil {
			t.Errorf("RowBytes(%d,%d,%d) = %d bytes, want nil", tc.y, tc.x0, tc.n, len(got))
		}
	}
	if got := b.RowBytes(1, 1, 3); len(got) != 48 {
		t.Errorf("RowBytes(1,1,3) = %d bytes, want 48", len(got))
	}
}

// TestPaddingMakesByteCompareUnsound reproduces the hazard ADR 0002 documents,
// so the reason for TestCellHasNoPadding is on record in code. It scribbles
// undefined values into the would-be padding of a *different* struct layout and
// shows that the byte-wise row compare then reports two logically identical rows
// as different. This is what would happen to Cell if it had tail padding.
func TestPaddingMakesByteCompareUnsound(t *testing.T) {
	// Same logical content, but this layout has 2 bytes of tail padding.
	type paddedCell struct {
		Ch   rune
		FG   Colour
		BG   Colour
		Attr Attr
	}
	if unsafe.Sizeof(paddedCell{}) != 16 {
		t.Fatalf("expected the padded variant to also be 16 bytes, got %d", unsafe.Sizeof(paddedCell{}))
	}

	a := []paddedCell{{Ch: 'x', FG: NewColour(1, 2, 3), BG: NewColour(4, 5, 6), Attr: AttrBold}}
	b := []paddedCell{{Ch: 'x', FG: NewColour(1, 2, 3), BG: NewColour(4, 5, 6), Attr: AttrBold}}

	// Struct equality: the correct answer.
	if a[0] != b[0] {
		t.Fatal("struct compare should say equal")
	}

	// Scribble into the undefined tail padding of b only.
	pa := (*[16]byte)(unsafe.Pointer(&a[0]))
	pb := (*[16]byte)(unsafe.Pointer(&b[0]))
	pb[14], pb[15] = 0xFF, 0xFF

	structEqual := a[0] == b[0]
	byteEqual := *pa == *pb
	if structEqual && byteEqual {
		t.Fatal("expected the padded byte compare to be wrong; Go may now define padding")
	}
	if !structEqual {
		t.Fatal("struct compare should still say equal")
	}
	t.Logf("per-cell struct loop says equal: %v (correct); byte-wise row compare says equal: %v (WRONG)",
		structEqual, byteEqual)
}

// TestContinuationCellRoundTripsEqual is the wide-glyph invariant: a
// continuation cell must compare equal to itself across frames or the row skip
// misses and the glyph flickers. Unmeasured upstream (ADR 0002 risk 2), but the
// property is cheap to assert.
func TestContinuationCellRoundTripsEqual(t *testing.T) {
	wideStyle := NewStyle(NewColour(255, 0, 0), DefaultColour, 0)
	c := NewCell('漢', wideStyle).asContinuation()
	if c == NewCell('漢', wideStyle) {
		t.Fatal("continuation flag must distinguish the cell from its left half")
	}
	// Two DISTINCT instances, built separately: writing the same expression on
	// both sides of the comparison compares an expression with itself, which is
	// always false and so can never fail.
	c1 := NewCell('y', wideStyle).asContinuation()
	c2 := NewCell('y', wideStyle).asContinuation()
	if c1 != c2 {
		t.Errorf("two identical continuation cells do not compare equal: %+v vs %+v", c1, c2)
	}
	if !c.IsContinuation() {
		t.Fatal("IsContinuation must be true")
	}
	if NewCell('x', DefaultStyle).IsContinuation() {
		t.Fatal("IsContinuation must be false for an ordinary cell")
	}
}

func TestAttrBitsAreDistinctAndHas(t *testing.T) {
	all := []Attr{AttrBold, AttrFaint, AttrItalic, AttrUnderline, AttrReverse, AttrStrike}
	seen := Attr(0)
	for _, a := range all {
		if a == 0 {
			t.Fatalf("%v has a zero bit", a)
		}
		if seen&a != 0 {
			t.Fatalf("%v overlaps an earlier attribute", a)
		}
		seen |= a
	}
	if seen != 0x3F {
		t.Fatalf("attribute bits occupy %d of 16; expected the low 6", seen)
	}
	combined := AttrBold | AttrItalic
	if !combined.Has(AttrBold) || !combined.Has(AttrItalic) || combined.Has(AttrUnderline) {
		t.Fatal("Has is wrong for combined attributes")
	}
	if got := Attr(0).String(); got != "none" {
		t.Errorf("Attr(0).String() = %q, want %q", got, "none")
	}
	if got := (AttrBold | AttrStrike).String(); got != "bold|strike" {
		t.Errorf("String() = %q, want %q", got, "bold|strike")
	}
}

func BenchmarkAttrString(b *testing.B) {
	a := AttrBold | AttrUnderline
	for i := 0; i < b.N; i++ {
		_ = a.String()
	}
}
