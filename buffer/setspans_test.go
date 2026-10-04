package buffer

import (
	"strings"
	"testing"
)

// styleSet returns distinguishable styles for the multi-span tests. They differ
// in every channel, so a cell written in the wrong style is unambiguous.
func styleSet() (red, blue, green Style) {
	return NewStyle(NewColour(255, 0, 0), NewColour(1, 1, 1), AttrBold),
		NewStyle(NewColour(0, 0, 255), NewColour(2, 2, 2), AttrItalic),
		NewStyle(NewColour(0, 128, 0), NewColour(3, 3, 3), 0)
}

// TestSetSpansWritesEachSpanInItsOwnStyle is the basic contract, including a
// span boundary landing mid-word: the text is concatenated but the styles are
// not merged across the boundary, because each cell is written independently.
func TestSetSpansWritesEachSpanInItsOwnStyle(t *testing.T) {
	red, blue, _ := styleSet()
	b := NewBuffer(20, 1)

	next := b.SetSpans(0, 0, []Span{NewSpan("ab", red), NewSpan("cde", blue)})
	if next != 5 {
		t.Errorf("returned %d, want 5", next)
	}
	for x, want := range map[int]rune{0: 'a', 1: 'b', 2: 'c', 3: 'd', 4: 'e'} {
		if got := b.CellAt(x, 0).Rune(); got != want {
			t.Errorf("cell %d = %q, want %q", x, got, want)
		}
	}
	for x := 0; x < 2; x++ {
		if got := b.CellAt(x, 0).Style(); got != red {
			t.Errorf("cell %d style = %+v, want %+v", x, got, red)
		}
	}
	for x := 2; x < 5; x++ {
		if got := b.CellAt(x, 0).Style(); got != blue {
			t.Errorf("cell %d style = %+v, want %+v", x, got, blue)
		}
	}

	// A boundary landing mid-word changes nothing about the styles.
	b = NewBuffer(20, 1)
	b.SetSpans(0, 0, []Span{NewSpan("Se", red), NewSpan("lected", blue)})
	if got := b.CellAt(1, 0).Style(); got != red {
		t.Errorf("cell 1 = %+v, want the first span's %+v", got, red)
	}
	if got := b.CellAt(2, 0).Style(); got != blue {
		t.Errorf("cell 2 = %+v, want the second span's %+v", got, blue)
	}
	if got := b.CellAt(7, 0).Style(); got != blue {
		t.Errorf("cell 7 = %+v, want the second span's %+v", got, blue)
	}
}

// TestSetSpansResolvesUnsetStylesOnEntry states that a span carrying the unset
// sentinel is written as DefaultStyle, not as opaque black. It is the reason
// NewSpan's author does not have to resolve anything.
func TestSetSpansResolvesUnsetStylesOnEntry(t *testing.T) {
	b := NewBuffer(4, 1)
	b.SetSpans(0, 0, []Span{
		NewSpan("ab", PlainStyle),
		NewSpan("cd", BoldStyle),
	})
	for x := 0; x < 4; x++ {
		if got := b.CellAt(x, 0).Style(); got.FG != DefaultColour || got.BG != DefaultColour {
			t.Errorf("cell %d = %+v, want the terminal's own colours", x, got)
		}
	}
	if b.CellAt(0, 0).Attr != 0 {
		t.Error("PlainStyle must resolve to no attributes")
	}
	if b.CellAt(3, 0).Attr != AttrBold {
		t.Errorf("BoldStyle lost its attribute: %v", b.CellAt(3, 0).Attr)
	}

	// The zero Style behaves the same way, and identically to PlainStyle.
	b.SetSpans(0, 0, []Span{NewSpan("ab", Style{})})
	if got := b.CellAt(0, 0).Style(); got != DefaultStyle {
		t.Errorf("the zero span style resolved to %+v, want DefaultStyle", got)
	}
}

// TestSpanBoundaryAcrossWideGlyph is THE test in this file. It is the flicker
// invariant ADR 0008 spells out and the one that is cheapest to get wrong.
//
// A double-width glyph owns two cells. The continuation half belongs to the span
// that owns the glyph's FIRST rune, and it must take THAT span's style. If it
// took the next span's style, the two halves of one character would differ from
// each other and from the previous frame, the tier-1 row skip would never fire,
// and the row would flicker on every frame forever.
//
// It is asserted three ways: the value, the equality with its left half, and the
// cross-frame stability that is the actual reason the rule exists.
func TestSpanBoundaryAcrossWideGlyph(t *testing.T) {
	red, blue, _ := styleSet()

	// The boundary falls EXACTLY on the wide glyph's second cell: 'a' in red,
	// then 漢 in blue, so cell 2 is 漢's continuation and its "next span" is
	// whatever comes after.
	spans := []Span{
		NewSpan("a", red),
		NewSpan("漢", blue),
		NewSpan("z", red),
	}
	b := NewBuffer(10, 1)
	next := b.SetSpans(0, 0, spans)
	if next != 4 {
		t.Fatalf("returned %d, want 4", next)
	}

	glyph := b.CellAt(1, 0)
	cont := b.CellAt(2, 0)
	if glyph.Rune() != '漢' || glyph.IsContinuation() {
		t.Fatalf("cell 1 = %+v, want the glyph itself", glyph)
	}
	if !cont.IsContinuation() {
		t.Fatalf("cell 2 = %+v, want a continuation cell", cont)
	}
	if cont.Rune() != 0 {
		t.Errorf("continuation cell carries rune %q; it must carry none", cont.Rune())
	}

	// 1. The value: the owning span's style, blue.
	if cont.Style() != blue {
		t.Errorf("continuation style = %+v, want the OWNING span's %+v", cont.Style(), blue)
	}
	// 2. Equality with its own left half, which is the row-skip precondition.
	if cont.Style() != glyph.Style() {
		t.Errorf("the two halves of one glyph disagree: %+v vs %+v", glyph.Style(), cont.Style())
	}
	// 3. And it is demonstrably NOT the following span's style, so the assertion
	// above cannot pass by accident.
	if cont.Style() == red {
		t.Error("continuation took the following span's style; this is the permanent-flicker bug")
	}
	// The trailing span is written in its own style, unaffected.
	if got := b.CellAt(3, 0).Style(); got != red {
		t.Errorf("cell 3 = %+v, want the third span's %+v", got, red)
	}

	// Cross-frame stability: rewriting the identical span slice must produce a
	// byte-identical pair, or the row never compares equal and never skips.
	firstGlyph, firstCont := b.CellAt(1, 0), b.CellAt(2, 0)
	b.SetSpans(0, 0, spans)
	if b.CellAt(1, 0) != firstGlyph || b.CellAt(2, 0) != firstCont {
		t.Error("an identical rewrite changed the wide glyph's cells; the row skip would never fire")
	}

	// And the same for a wide glyph at the very END of the spans, where there is
	// no following span at all to confuse it with.
	b.SetSpans(0, 0, []Span{NewSpan("a", red), NewSpan("漢", blue)})
	if got, want := b.CellAt(2, 0).Style(), blue; got != want {
		t.Errorf("trailing wide glyph's continuation = %+v, want %+v", got, want)
	}
}

// TestSetSpansWideGlyphContinuationTakesOwningSpanStyle is the ADR's own test
// name for the invariant above, kept separate so the failure message names the
// rule rather than a test that happens to cover it.
//
// It also covers the SetString path, which shares the implementation and must
// therefore share the guarantee.
func TestSetSpansWideGlyphContinuationTakesOwningSpanStyle(t *testing.T) {
	red, blue, _ := styleSet()

	t.Run("SetSpans with a wide glyph mid-slice", func(t *testing.T) {
		b := NewBuffer(10, 1)
		b.SetSpans(0, 0, []Span{NewSpan("x漢y", red), NewSpan("z", blue)})
		if got := b.CellAt(1, 0).Style(); got != red {
			t.Errorf("glyph cell = %+v, want %+v", got, red)
		}
		if got := b.CellAt(2, 0).Style(); got != red {
			t.Errorf("continuation cell = %+v, want the owning span's %+v (not blue)", got, blue)
		}
		if got := b.CellAt(3, 0).Style(); got != red {
			t.Errorf("cell 3 = %+v, want %+v", got, red)
		}
		if got := b.CellAt(4, 0).Style(); got != blue {
			t.Errorf("cell 4 = %+v, want the following span's %+v", got, blue)
		}
	})

	t.Run("SetString takes the same path and the same guarantee", func(t *testing.T) {
		b := NewBuffer(10, 1)
		b.SetString(0, 0, "x漢y", blue)
		if got := b.CellAt(1, 0).Style(); got != blue {
			t.Errorf("glyph cell = %+v, want %+v", got, blue)
		}
		if got := b.CellAt(2, 0).Style(); got != blue {
			t.Errorf("continuation cell = %+v; it must match its own left half", got)
		}
	})

	t.Run("two adjacent wide glyphs keep their own styles", func(t *testing.T) {
		b := NewBuffer(10, 1)
		b.SetSpans(0, 0, []Span{NewSpan("漢", red), NewSpan("漢", blue)})
		if b.CellAt(0, 0).Style() != red || b.CellAt(1, 0).Style() != red {
			t.Error("the first glyph's two halves do not share the first span's style")
		}
		if b.CellAt(2, 0).Style() != blue || b.CellAt(3, 0).Style() != blue {
			t.Error("the second glyph's two halves do not share the second span's style")
		}
	})
}

// TestSetSpansDropsWideGlyphThatDoesNotFit pins the other half-glyph rule: a
// wide rune with only one cell of room is not written at all, so the row never
// carries an unpaired cell that differs from the previous frame forever. The
// whole run ends there rather than resuming at the next span.
//
// The row is described as the runes a reader would see, with a '>' standing for
// a continuation half, so a stray continuation cannot hide inside a readable
// string. A trailing ' ' is an untouched DefaultCell, not a written space.
func TestSetSpansDropsWideGlyphThatDoesNotFit(t *testing.T) {
	cases := []struct {
		name    string
		width   int
		spans   []Span
		want    string
		returns int
	}{
		{"fits exactly", 3, []Span{NewSpan("a漢", DefaultStyle)}, "a漢>", 3},
		{"one cell short", 2, []Span{NewSpan("a漢", DefaultStyle)}, "a ", 1},
		{"one cell short, run stops there", 2, []Span{NewSpan("a漢b", DefaultStyle)}, "a ", 1},
		{"one cell short, later span does not resume", 2,
			[]Span{NewSpan("a漢", DefaultStyle), NewSpan("b", DefaultStyle)}, "a ", 1},
		{"one-column buffer drops the glyph", 1, []Span{NewSpan("漢", DefaultStyle)}, " ", 0},
		{"a wide glyph filling the width leaves no room after", 2,
			[]Span{NewSpan("漢b", DefaultStyle)}, "漢>", 2},
		{"room for the glyph but not the rune after", 3,
			[]Span{NewSpan("a漢z", DefaultStyle)}, "a漢>", 3},
		{"room to spare on the right", 5, []Span{NewSpan("a漢b", DefaultStyle)}, "a漢>b ", 4},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := NewBuffer(tc.width, 1)
			if next := b.SetSpans(0, 0, tc.spans); next != tc.returns {
				t.Errorf("returned %d, want %d", next, tc.returns)
			}
			if got := rowDesc(b, tc.width); got != tc.want {
				t.Errorf("row = %q, want %q", got, tc.want)
			}
			// No cell may be a continuation half with no glyph to its left, and no
			// cell may hold the continuation rune without being flagged.
			for x := 0; x < tc.width; x++ {
				c := b.CellAt(x, 0)
				if c.IsContinuation() && x == 0 {
					t.Error("cell 0 is an unpaired continuation half")
				}
				if !c.IsContinuation() && c.Rune() == continuationRune {
					t.Errorf("cell %d holds the continuation rune but is not flagged", x)
				}
			}
		})
	}
}

// rowDesc renders row 0 the way a reader would see it, marking a continuation
// half as '>' so an unpaired one is visible rather than invisible.
func rowDesc(b *Buffer, width int) string {
	var out strings.Builder
	for x := 0; x < width; x++ {
		c := b.CellAt(x, 0)
		if c.IsContinuation() {
			out.WriteByte('>')
			continue
		}
		out.WriteRune(c.Rune())
	}
	return out.String()
}

// TestSetSpansEdgeCases covers the three boundaries the ADR calls out plus the
// ones a composed widget tree produces.
func TestSetSpansEdgeCases(t *testing.T) {
	red, blue, _ := styleSet()

	t.Run("an empty span slice writes nothing", func(t *testing.T) {
		for _, spans := range [][]Span{nil, {}} {
			b := NewBuffer(4, 1)
			b.SetCell(0, 0, NewCell('Z', DefaultStyle))
			b.TakeDirty() // NewBuffer marks everything dirty; only new writes count
			if next := b.SetSpans(0, 0, spans); next != 0 {
				t.Errorf("returned %d, want the starting x", next)
			}
			if b.CellAt(0, 0).Rune() != 'Z' {
				t.Error("an empty span slice overwrote a cell")
			}
			if b.IsDirty() {
				t.Error("an empty span slice marked the buffer dirty")
			}
		}
	})

	t.Run("empty text in a span writes nothing", func(t *testing.T) {
		b := NewBuffer(4, 1)
		b.SetCell(1, 0, NewCell('Z', DefaultStyle))
		next := b.SetSpans(0, 0, []Span{NewSpan("", red), NewSpan("ab", blue), NewSpan("", red)})
		if next != 2 {
			t.Errorf("returned %d, want 2: empty spans must not consume cells", next)
		}
		if b.CellAt(0, 0).Rune() != 'a' || b.CellAt(1, 0).Rune() != 'b' {
			t.Error("the non-empty span was not written at x=0")
		}
		if b.CellAt(2, 0).Rune() != ' ' {
			t.Error("an empty span consumed a cell")
		}
	})

	t.Run("a span crossing the clip boundary stops cleanly", func(t *testing.T) {
		// The span runs off the right edge mid-way. Everything up to the edge is
		// written, the run returns, and no cell beyond it is touched.
		b := NewBuffer(5, 1)
		next := b.SetSpans(0, 0, []Span{NewSpan("abcdefghij", red)})
		if next != 5 {
			t.Errorf("returned %d, want 5 (the buffer width)", next)
		}
		for x := 0; x < 5; x++ {
			if b.CellAt(x, 0).Rune() != rune('a'+x) {
				t.Errorf("cell %d = %q, want %q", x, b.CellAt(x, 0).Rune(), rune('a'+x))
			}
			if b.CellAt(x, 0).Style() != red {
				t.Errorf("cell %d lost its style at the boundary", x)
			}
		}
	})

	t.Run("a span crossing the clip boundary on a wide glyph", func(t *testing.T) {
		// 4 cells: 'abc' then 漢 needs 2 and only 1 remains, so it is dropped and
		// cell 3 stays a DefaultCell rather than half a glyph.
		b := NewBuffer(4, 1)
		b.SetSpans(0, 0, []Span{NewSpan("abc漢", red)})
		if b.CellAt(3, 0) != DefaultCell {
			t.Errorf("cell 3 = %+v, want untouched (half a glyph is worse than none)", b.CellAt(3, 0))
		}
		for x := 0; x < 3; x++ {
			if b.CellAt(x, 0).Rune() != rune('a'+x) {
				t.Errorf("cell %d = %q", x, b.CellAt(x, 0).Rune())
			}
		}
	})

	t.Run("a span wider than the buffer", func(t *testing.T) {
		for _, w := range []int{1, 2, 3, 7} {
			b := NewBuffer(w, 1)
			next := b.SetSpans(0, 0, []Span{NewSpan("the quick brown fox jumps", red)})
			if next != w {
				t.Errorf("width %d: returned %d, want %d", w, next, w)
			}
		}
	})

	t.Run("out-of-range rows are no-ops", func(t *testing.T) {
		b := NewBuffer(4, 1)
		b.TakeDirty()
		for _, y := range []int{-1, 1, 1000} {
			if got := b.SetSpans(0, y, []Span{NewSpan("abc", red)}); got != 0 {
				t.Errorf("y=%d returned %d, want 0", y, got)
			}
		}
		if b.IsDirty() {
			t.Error("an out-of-range row marked the buffer dirty")
		}
	})

	t.Run("a negative start x skips off-screen runes", func(t *testing.T) {
		b := NewBuffer(4, 1)
		next := b.SetSpans(-2, 0, []Span{NewSpan("abcd", red)})
		if next != 2 {
			t.Errorf("returned %d, want 2", next)
		}
		if b.CellAt(0, 0).Rune() != 'c' || b.CellAt(1, 0).Rune() != 'd' {
			t.Errorf("got %q,%q want c,d", b.CellAt(0, 0).Rune(), b.CellAt(1, 0).Rune())
		}
		if b.CellAt(2, 0) != DefaultCell {
			t.Error("wrote past the end of the string")
		}
	})

	t.Run("zero-width runes are dropped, not given a cell", func(t *testing.T) {
		// Two spans, each carrying a combining mark, so the test also shows that a
		// mark does not leak the previous span's style onto the next cell: marks
		// are dropped outright rather than attached to the preceding span.
		b := NewBuffer(6, 1)
		next := b.SetSpans(0, 0, []Span{
			NewSpan("e\u0301", red),
			NewSpan("\u0301x", blue),
		})
		if next != 2 {
			t.Errorf("returned %d, want 2: the combining marks occupy no cells", next)
		}
		if got := b.CellAt(0, 0); got.Rune() != 'e' || got.Style() != red {
			t.Errorf("cell 0 = %+v, want 'e' in the first span's style", got)
		}
		if got := b.CellAt(1, 0); got.Rune() != 'x' || got.Style() != blue {
			t.Errorf("cell 1 = %+v, want 'x' in the second span's style", got)
		}
		if got := b.CellAt(2, 0); got != DefaultCell {
			t.Errorf("cell 2 = %+v, want untouched: a mark must not occupy a cell", got)
		}
	})

	t.Run("a span starting past the right edge writes nothing", func(t *testing.T) {
		b := NewBuffer(4, 1)
		b.TakeDirty()
		if got := b.SetSpans(9, 0, []Span{NewSpan("abc", red)}); got != 9 {
			t.Errorf("returned %d, want 9", got)
		}
		if b.IsDirty() {
			t.Error("an off-screen write marked the buffer dirty")
		}
	})
}

// TestSetSpansDoesNotAllocate is the test that keeps this API on the frame path.
// ADR 0008 states SetSpans is 0 allocations because it takes a slice and never
// builds one; this is where that claim is checked rather than asserted in prose.
//
// The wide-glyph and multi-span cases are measured too, because a helper that
// allocated only on the interesting path would be the easiest way for the claim
// to rot.
func TestSetSpansDoesNotAllocate(t *testing.T) {
	red, blue, _ := styleSet()
	b := NewBuffer(200, 1)

	cases := []struct {
		name  string
		spans []Span
	}{
		{"uniform", []Span{NewSpan("hello", red)}},
		{"three spans", []Span{NewSpan("ab", red), NewSpan("cd", blue), NewSpan("ef", red)}},
		{"wide glyphs", []Span{NewSpan("漢漢漢", red)}},
		{"wide glyph at a span boundary", []Span{NewSpan("a漢", red), NewSpan("漢b", blue)}},
		{"empty", nil},
		{"unresolved style", []Span{NewSpan("xyz", BoldStyle)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := testing.AllocsPerRun(100, func() { b.SetSpans(0, 0, tc.spans) }); got != 0 {
				t.Errorf("SetSpans allocated %.1f objects per run, want 0", got)
			}
		})
	}

	// SetString shares the implementation, so it shares the guarantee. This is
	// also the path a widget's label takes on every frame.
	long := "the quick brown fox jumps over the lazy dog and keeps going for a while"
	if got := testing.AllocsPerRun(100, func() { b.SetString(0, 0, long, red) }); got != 0 {
		t.Errorf("SetString allocated %.1f objects per run, want 0", got)
	}
}

// TestSetSpansOnSubBufferIsStrideCorrect re-checks ADR 0006's rule through the
// new write path: a view's rows are strided, so a wide glyph written through a
// view must land in the parent at the parent's stride, not adjacently.
func TestSetSpansOnSubBufferIsStrideCorrect(t *testing.T) {
	parent := NewBuffer(10, 6)
	sub := parent.SubBuffer(3, 2, 5, 4)
	red := NewStyle(NewColour(1, 2, 3), DefaultColour, 0)

	sub.SetSpans(0, 1, []Span{NewSpan("a漢b", red)})

	// parent row 2+1 == 3, columns 3..5.
	if got := parent.CellAt(3, 3).Rune(); got != 'a' {
		t.Errorf("parent (3,3) = %q, want 'a'", got)
	}
	if got := parent.CellAt(4, 3).Rune(); got != '漢' {
		t.Errorf("parent (4,3) = %q, want '漢'", got)
	}
	if got := parent.CellAt(5, 3); !got.IsContinuation() {
		t.Errorf("parent (5,3) = %+v, want the continuation half", got)
	}
	if got := parent.CellAt(5, 3).Style(); got != red {
		t.Errorf("continuation style = %+v, want %+v", got, red)
	}
	if got := parent.CellAt(6, 3).Rune(); got != 'b' {
		t.Errorf("parent (6,3) = %q, want 'b': the view is strided and cell 3 of it is parent column 6", got)
	}
	if got := parent.CellAt(4, 4); got != DefaultCell {
		t.Errorf("the write leaked onto the next view row: parent (4,4) = %+v", got)
	}

	// A view too narrow for the glyph: the run stops at the view's own right
	// edge, so the rune after the pair is never written.
	narrow := parent.SubBuffer(3, 4, 3, 1)
	narrow.SetSpans(0, 0, []Span{NewSpan("a漢b", red)})
	if got := parent.CellAt(3, 4).Rune(); got != 'a' {
		t.Errorf("parent (3,4) = %q, want 'a'", got)
	}
	if got := parent.CellAt(4, 4).Rune(); got != '漢' {
		t.Errorf("parent (4,4) = %q, want '漢'", got)
	}
	if got := parent.CellAt(5, 4); !got.IsContinuation() {
		t.Errorf("parent (5,4) = %+v, want the continuation half", got)
	}
	if got := parent.CellAt(6, 4); got != DefaultCell {
		t.Errorf("the run crossed the view's right edge: parent (6,4) = %+v", got)
	}
}
