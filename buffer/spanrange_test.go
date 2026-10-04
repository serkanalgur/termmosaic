package buffer

import (
	"strings"
	"testing"
)

// The tests here are the coverage that used to live beside two private copies of
// these writers, in widgets/data and widgets/viz. They moved with the behaviour:
// a rule that was tested in a widget package is now tested where the rule lives,
// and the widget tests still exercise the rules through their own widgets.

// continuationAt returns the columns of row y holding a continuation cell, so an
// assertion can say WHICH cells are unpaired rather than tripping over the
// legitimate ones.
func continuationAt(b *Buffer, y int) []int {
	var out []int
	for x := 0; x < b.Width(); x++ {
		if b.CellAt(x, y).IsContinuation() {
			out = append(out, x)
		}
	}
	return out
}

// TestSetSpansInClipsToTheRange covers every boundary of the range: content that
// fits, content that fits exactly, content that does not, and the wide glyph that
// straddles the right edge.
func TestSetSpansInClipsToTheRange(t *testing.T) {
	cases := []struct {
		name      string
		spans     []Span
		x0, x1    int
		wantRow   string
		wantAfter int
	}{
		{"fits", []Span{NewSpan("abc", DefaultStyle)}, 0, 5, "abc   ", 3},
		{"exactly at the edge", []Span{NewSpan("abc", DefaultStyle)}, 0, 3, "abc", 3},
		{"one cell too narrow", []Span{NewSpan("abcd", DefaultStyle)}, 0, 3, "abc", 3},
		{"offset range", []Span{NewSpan("abcde", DefaultStyle)}, 2, 4, "  ab", 4},
		{"wide glyph fits", []Span{NewSpan("漢", DefaultStyle)}, 0, 2, "漢>", 2},
		{"wide glyph straddles the right edge", []Span{NewSpan("a漢", DefaultStyle)}, 0, 2, "a ", 1},
		{"only a wide glyph, one cell of room", []Span{NewSpan("漢", DefaultStyle)}, 0, 1, " ", 0},
		{"empty range", []Span{NewSpan("abc", DefaultStyle)}, 3, 3, "   ", 3},
		{"inverted range", []Span{NewSpan("abc", DefaultStyle)}, 4, 2, "    ", 4},
		{"no spans", nil, 0, 4, "    ", 0},
		{"empty span among full ones", []Span{NewSpan("a", DefaultStyle), NewSpan("", DefaultStyle), NewSpan("b", DefaultStyle)}, 0, 4, "ab  ", 2},
		{"only empty spans", []Span{NewSpan("", DefaultStyle), NewSpan("", DefaultStyle)}, 0, 4, "    ", 0},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := NewBuffer(8, 1)
			if got := b.SetSpansIn(c.x0, c.x1, 0, c.spans); got != c.wantAfter {
				t.Errorf("SetSpansIn(%d, %d) returned %d, want %d", c.x0, c.x1, got, c.wantAfter)
			}
			if got := rowDesc(b, 8); !strings.HasPrefix(got, c.wantRow) {
				t.Errorf("row = %q, want the prefix %q", got, c.wantRow)
			}
		})
	}
}

// TestSetSpansInNegativeStartIsIgnoredNotWritten is the negative-x case: the
// cursor advances so the column arithmetic of a partially off-screen widget still
// works, and nothing is written left of the range. SetSpansIn inherits SetSpans'
// rule here, which is why the skipped cells are not an error.
func TestSetSpansInNegativeStartIsIgnoredNotWritten(t *testing.T) {
	b := NewBuffer(6, 1)
	b.FillRect(Rect{W: 6, H: 1}, DefaultStyle.Blank())

	if got := b.SetSpansIn(-2, 4, 0, []Span{NewSpan("abcd", DefaultStyle)}); got != 2 {
		t.Errorf("returned %d, want 2: the two cells left of the range were skipped, not drawn", got)
	}
	if got := rowDesc(b, 6); got != "cd    " {
		t.Errorf("row = %q, want %q: the cells before x0 must stay blank", got, "cd    ")
	}
}

// TestSetSpansInWideGlyphContinuationTakesItsOwnSpanStyle is the flicker rule at
// a range edge: both halves of a glyph take the SAME span's style, so the pair
// compares equal next frame and the row skip fires.
func TestSetSpansInWideGlyphContinuationTakesItsOwnSpanStyle(t *testing.T) {
	red, blue, _ := styleSet()
	b := NewBuffer(6, 1)

	b.SetSpansIn(0, 4, 0, []Span{NewSpan("a漢b", red), NewSpan("c", blue)})
	if got := b.CellAt(1, 0).Style(); got != red {
		t.Errorf("the glyph cell's style = %+v, want the first span's %+v", got, red)
	}
	if got := b.CellAt(2, 0).Style(); got != red {
		t.Errorf("the continuation cell's style = %+v, want the OWNING span's %+v, not the next span's %+v",
			got, red, blue)
	}
	if !b.CellAt(2, 0).IsContinuation() {
		t.Errorf("cell 2 is not a continuation cell")
	}
}

// TestSetSpansInNeverLeavesAHalfGlyph is the clipping rule stated from the
// outside: whatever the content and whatever the range, every continuation cell
// in the row must be the right half of a glyph written by this call. A writer that
// placed one half and stopped would leave a cell that renders as junk and never
// compares equal to the previous frame.
func TestSetSpansInNeverLeavesAHalfGlyph(t *testing.T) {
	mark := firstRuneOf(TruncSuffix)
	for _, text := range []string{"a漢b", "漢a漢b", "a漢漢b", "漢漢漢"} {
		for x0 := 0; x0 < 3; x0++ {
			for width := 1; width <= 6; width++ {
				b := NewBuffer(10, 1)
				b.SetSpansIn(x0, x0+width, 0, []Span{NewSpan(text, DefaultStyle)})
				for _, x := range continuationAt(b, 0) {
					if x-1 < x0 || b.CellAt(x-1, 0).IsContinuation() {
						t.Errorf("SetSpansIn(%q, %d, %d): cell %d is a continuation without a glyph beside it",
							text, x0, width, x)
					}
				}
				// The capped and window writers lay a marker over the same cells,
				// so the rule has to hold for them too.
				b.SetSpansCappedIn(x0, x0+width, 0, []Span{NewSpan(text, DefaultStyle)}, mark)
				for _, x := range continuationAt(b, 0) {
					if x-1 < x0 || b.CellAt(x-1, 0).IsContinuation() {
						t.Errorf("SetSpansCappedIn(%q, %d, %d): cell %d is a continuation without a glyph beside it",
							text, x0, width, x)
					}
				}
				b.SetSpansWindowIn(x0, x0+width, 0, []Span{NewSpan(text, DefaultStyle)}, 1, mark)
				for _, x := range continuationAt(b, 0) {
					if x-1 < x0 || b.CellAt(x-1, 0).IsContinuation() {
						t.Errorf("SetSpansWindowIn(%q, %d, %d, skip 1): cell %d is a continuation without a glyph beside it",
							text, x0, width, x)
					}
				}
			}
		}
	}
}

// TestSetSpansCappedInMarkerRules is Truncate's contract on the frame path: a
// fitted range is drawn whole, an over-wide one gives its last cell to the marker,
// and the marker wears the last non-empty input span's style.
func TestSetSpansCappedInMarkerRules(t *testing.T) {
	red, blue, _ := styleSet()
	mark := firstRuneOf(TruncSuffix)

	t.Run("fits, no marker", func(t *testing.T) {
		b := NewBuffer(6, 1)
		if got := b.SetSpansCappedIn(0, 5, 0, []Span{NewSpan("abc", DefaultStyle)}, mark); got != 3 {
			t.Errorf("returned %d, want 3", got)
		}
		if got := rowDesc(b, 6); got != "abc   " {
			t.Errorf("row = %q, want no marker", got)
		}
	})

	t.Run("exactly full, no marker", func(t *testing.T) {
		b := NewBuffer(6, 1)
		b.SetSpansCappedIn(0, 3, 0, []Span{NewSpan("abc", DefaultStyle)}, mark)
		if got := rowDesc(b, 6); got != "abc   " {
			t.Errorf("row = %q, want no marker: nothing was cut", got)
		}
	})

	t.Run("over-wide, marker in the last cell", func(t *testing.T) {
		b := NewBuffer(6, 1)
		if got := b.SetSpansCappedIn(0, 5, 0, []Span{NewSpan("abcdefgh", DefaultStyle)}, mark); got != 5 {
			t.Errorf("returned %d, want 5", got)
		}
		if got := rowDesc(b, 6); got != "abcd… " {
			t.Errorf("row = %q, want %q", got, "abcd… ")
		}
	})

	t.Run("one cell is the marker alone", func(t *testing.T) {
		b := NewBuffer(3, 1)
		if got := b.SetSpansCappedIn(0, 1, 0, []Span{NewSpan("abcdefgh", DefaultStyle)}, mark); got != 1 {
			t.Errorf("returned %d, want 1", got)
		}
		if got := rowDesc(b, 3); got != "…  " {
			t.Errorf("row = %q, want the marker alone", got)
		}
	})

	t.Run("marker wears the last non-empty span's style", func(t *testing.T) {
		b := NewBuffer(6, 1)
		b.SetSpansCappedIn(0, 4, 0, []Span{NewSpan("aaaaaaa", red), NewSpan("", blue), NewSpan("bbb", blue)}, mark)
		cell := b.CellAt(3, 0)
		if cell.Rune() != mark {
			t.Fatalf("cell 3 = %q, want the marker", cell.Rune())
		}
		if got := cell.Style(); got != blue {
			t.Errorf("the marker's style = %+v, want the last non-empty span's %+v", got, blue)
		}
	})

	t.Run("empty content fits, so there is no marker", func(t *testing.T) {
		b := NewBuffer(3, 1)
		b.SetSpansCappedIn(0, 1, 0, []Span{NewSpan("", DefaultStyle)}, mark)
		if got := rowDesc(b, 3); got != "   " {
			t.Errorf("row = %q, want nothing drawn: empty content is not truncation", got)
		}
	})

	t.Run("empty range writes nothing", func(t *testing.T) {
		b := NewBuffer(3, 1)
		if got := b.SetSpansCappedIn(2, 2, 0, []Span{NewSpan("abc", DefaultStyle)}, mark); got != 2 {
			t.Errorf("returned %d, want 2", got)
		}
		if got := rowDesc(b, 3); got != "   " {
			t.Errorf("row = %q, want it untouched", got)
		}
	})
}

// TestSetSpansCappedInNeverSplitsAGlyphUnderTheMarker is the wide-glyph half of
// the capped rule: the marker takes the last cell, so content is written into the
// cells before it and a glyph that would reach it is dropped rather than halved.
func TestSetSpansCappedInNeverSplitsAGlyphUnderTheMarker(t *testing.T) {
	mark := firstRuneOf(TruncSuffix)
	b := NewBuffer(8, 1)
	b.SetSpansCappedIn(0, 5, 0, []Span{NewSpan("a漢bcdef", DefaultStyle)}, mark)

	if got := b.CellAt(4, 0).Rune(); got != mark {
		t.Errorf("cell 4 = %q, want the marker", got)
	}
	if b.CellAt(4, 0).IsContinuation() || b.CellAt(3, 0).IsContinuation() {
		t.Errorf("a continuation cell sits under the marker: %q", rowDesc(b, 8))
	}
	if got, want := rowDesc(b, 8), "a漢>b…"; !strings.HasPrefix(got, want) {
		t.Errorf("row = %q, want the prefix %q", got, want)
	}
}

// TestSetSpansWindowInSkipsAndMarks is the horizontally scrolled region: the
// window shows the TAIL of the content, and says so with a marker whenever
// anything was cut — including when the content filled the window exactly.
func TestSetSpansWindowInSkipsAndMarks(t *testing.T) {
	mark := firstRuneOf(TruncSuffix)
	text := NewSpan("state0", DefaultStyle)

	cases := []struct {
		name      string
		x0, x1    int
		skip      int
		wantRow   string
		wantAfter int
	}{
		{"no skip, content fits", 0, 6, 0, "state0", 6},
		{"no skip, content cut", 0, 4, 0, "sta…", 4},
		{"skip the head, visible part fits exactly", 0, 4, 2, "ate0", 4},
		{"window filled exactly still marked", 0, 5, 0, "stat…", 5},
		{"skip past the content draws nothing", 0, 5, 99, "     ", 0},
		{"negative skip is zero", 0, 4, -3, "sta…", 4},
		{"one cell shows content, no marker", 0, 1, 3, "t", 1},
		{"one cell cut shows content, no marker", 0, 1, 0, "s", 1},
		{"offset range, tail fits", 2, 6, 3, "  te0", 5},
		{"offset range, cut", 2, 6, 0, "  sta…", 6},
		{"empty range", 3, 3, 0, "     ", 3},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := NewBuffer(8, 1)
			if got := b.SetSpansWindowIn(c.x0, c.x1, 0, []Span{text}, c.skip, mark); got != c.wantAfter {
				t.Errorf("SetSpansWindowIn(%d, %d, skip=%d) returned %d, want %d",
					c.x0, c.x1, c.skip, got, c.wantAfter)
			}
			if got := rowDesc(b, 8); !strings.HasPrefix(got, c.wantRow) {
				t.Errorf("row = %q, want the prefix %q", got, c.wantRow)
			}
		})
	}
}

// TestSetSpansWindowInDropsAGlyphStraddlingTheLeftEdge is the left-edge half of
// the wide-glyph rule: a window that starts in the middle of a double-width glyph
// shows neither half of it, and the window ends there rather than shifting the
// rest of the content by a cell.
func TestSetSpansWindowInDropsAGlyphStraddlingTheLeftEdge(t *testing.T) {
	mark := firstRuneOf(TruncSuffix)
	b := NewBuffer(8, 1)
	b.SetSpansWindowIn(0, 6, 0, []Span{NewSpan("a漢b", DefaultStyle)}, 2, mark)

	got := rowDesc(b, 8)
	if strings.Contains(got, "漢") || len(continuationAt(b, 0)) != 0 {
		t.Errorf("row = %q: the straddled glyph was half-drawn", got)
	}
	if want := "     …"; !strings.HasPrefix(got, want) {
		t.Errorf("row = %q, want the prefix %q: the window ends at the straddled glyph", got, want)
	}
}

// TestSetSpansWindowInMarkerNeverLandsOnAContinuationCell is the case both private
// copies got wrong in the same way. The window writer can fill its range EXACTLY,
// so the cell before the marker can be the right half of a glyph: overwriting only
// that half would leave a glyph cell whose other half says something else, which
// renders as junk and never compares equal to the previous frame.
func TestSetSpansWindowInMarkerNeverLandsOnAContinuationCell(t *testing.T) {
	mark := firstRuneOf(TruncSuffix)
	b := NewBuffer(8, 1)
	// Five cells: the glyph at 3-4 fits exactly, and the next rune does not, so
	// the marker is due at cell 4 — the glyph's continuation.
	if got := b.SetSpansWindowIn(0, 5, 0, []Span{NewSpan("漢a漢b", DefaultStyle)}, 0, mark); got != 5 {
		t.Errorf("returned %d, want 5", got)
	}

	if got := continuationAt(b, 0); len(got) != 1 || got[0] != 1 {
		t.Errorf("continuation cells are at %v, want only the untouched glyph's at [1]", got)
	}
	if got := b.CellAt(4, 0).Rune(); got != mark {
		t.Errorf("cell 4 = %q, want the marker", got)
	}
	// The glyph cell the marker's neighbour belonged to must be blank rather than
	// a stranded glyph: a blank is invisible against the row the caller painted.
	if got := b.CellAt(3, 0).Rune(); got != ' ' {
		t.Errorf("cell 3 = %q, want it blanked: the glyph it belonged to lost its other half", got)
	}
}

// TestSetStringInIsSetSpansInOfOneString is the uniform-text entry point, and the
// two must agree cell for cell including the wide-glyph rules.
func TestSetStringInIsSetSpansInOfOneString(t *testing.T) {
	red, _, _ := styleSet()
	cases := []struct {
		s         string
		x0, x1    int
		wantRow   string
		wantAfter int
	}{
		{"abc", 0, 5, "abc   ", 3},
		{"abcdef", 0, 3, "abc   ", 3},
		{"a漢b", 0, 4, "a漢>b  ", 4},
		{"a漢b", 0, 2, "a     ", 1},
		{"", 0, 4, "      ", 0},
		{"abc", 3, 3, "        ", 3},
		{"abc", -1, 2, "bc      ", 2},
	}
	for _, c := range cases {
		b := NewBuffer(8, 1)
		one := NewBuffer(8, 1)
		got := b.SetStringIn(c.x0, c.x1, 0, c.s, red)
		want := one.SetSpansIn(c.x0, c.x1, 0, []Span{NewSpan(c.s, red)})
		if got != want || got != c.wantAfter {
			t.Errorf("SetStringIn(%q, %d, %d) returned %d, SetSpansIn returned %d, want %d",
				c.s, c.x0, c.x1, got, want, c.wantAfter)
		}
		if row, other := rowDesc(b, 8), rowDesc(one, 8); row != other {
			t.Errorf("SetStringIn and SetSpansIn disagree: %q vs %q", row, other)
		}
		if row := rowDesc(b, 8); !strings.HasPrefix(row, c.wantRow) {
			t.Errorf("SetStringIn(%q, %d, %d): row = %q, want the prefix %q", c.s, c.x0, c.x1, row, c.wantRow)
		}
	}
}

// TestSpanRangeWritersDoNotAllocate is the reason these exist rather than a call
// to Truncate: they are on the frame path, so a single allocation per visible row
// per frame is the regression ADR 0008 §4 forbids.
func TestSpanRangeWritersDoNotAllocate(t *testing.T) {
	red, blue, _ := styleSet()
	spans := []Span{NewSpan("漢a漢b some content here", red), NewSpan(" and more", blue)}
	mark := firstRuneOf(TruncSuffix)

	b := NewBuffer(80, 2)
	calls := map[string]func(){
		"SetSpansIn":            func() { b.SetSpansIn(3, 40, 0, spans) },
		"SetSpansCappedIn":      func() { b.SetSpansCappedIn(3, 40, 0, spans, mark) },
		"SetSpansWindowIn":      func() { b.SetSpansWindowIn(3, 40, 0, spans, 7, mark) },
		"SetStringIn":           func() { b.SetStringIn(3, 40, 0, "some content", red) },
		"SetSpansCappedIn/one":  func() { b.SetSpansCappedIn(3, 4, 0, spans, mark) },
		"SetSpansWindowIn/skip": func() { b.SetSpansWindowIn(3, 40, 0, spans, 99, mark) },
	}
	for name, call := range calls {
		call() // warm any lazily initialised state
		if got := testing.AllocsPerRun(200, call); got != 0 {
			t.Errorf("%s allocated %v times per run, want 0", name, got)
		}
	}
}

// firstRuneOf returns the first rune of s, for a one-cell marker string.
func firstRuneOf(s string) rune {
	for _, r := range s {
		return r
	}
	return 0
}
