package buffer

import (
	"strings"
	"testing"
)

// spanText concatenates a line's text and SpansWidth reports its width, so a
// table-driven wrap test can assert both without re-implementing either.
func spanText(spans []Span) string {
	var b strings.Builder
	for _, s := range spans {
		b.WriteString(s.Text)
	}
	return b.String()
}

func lineTexts(w Wrapped) []string {
	out := make([]string, w.Height())
	for i := range out {
		out[i] = spanText(w.Line(i))
	}
	return out
}

// TestSpansWidthCountsCellsNotRunes is table-driven because the whole point is
// that SpansWidth and a naive rune count disagree, and every caller of
// Truncate/Wrap does its arithmetic in cells.
func TestSpansWidthCountsCellsNotRunes(t *testing.T) {
	cases := []struct {
		name  string
		spans []Span
		want  int
	}{
		{"nil", nil, 0},
		{"empty slice", []Span{}, 0},
		{"empty text", []Span{NewSpan("", DefaultStyle)}, 0},
		{"ascii", []Span{NewSpan("hello", DefaultStyle)}, 5},
		{"two spans", []Span{NewSpan("ab", DefaultStyle), NewSpan("cde", DefaultStyle)}, 5},
		{"one wide rune", []Span{NewSpan("漢", DefaultStyle)}, 2},
		{"wide plus narrow", []Span{NewSpan("a漢b", DefaultStyle)}, 4},
		{"all wide", []Span{NewSpan("漢字", DefaultStyle)}, 4},
		{"wide split across spans", []Span{NewSpan("漢", DefaultStyle), NewSpan("字", DefaultStyle)}, 4},
		{"combining mark is zero width", []Span{NewSpan("é", DefaultStyle)}, 1},
		{"combining mark alone", []Span{NewSpan("́", DefaultStyle)}, 0},
		{"wide adjacent to zero width", []Span{NewSpan("漢́", DefaultStyle)}, 2},
		{"mixed", []Span{NewSpan("日本", DefaultStyle), NewSpan(" ok ", DefaultStyle), NewSpan("漢", DefaultStyle)}, 10},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := SpansWidth(tc.spans); got != tc.want {
				t.Errorf("SpansWidth(%q) = %d, want %d", spanText(tc.spans), got, tc.want)
			}
		})
	}
}

// TestWrapNeverSplitsWideGlyphOrSpanBoundaryWord is the wrap contract's hardest
// case, and the reason Wrap flattens before it breaks anything: a span boundary
// is not a word boundary.
//
// "Se" in one style and "lected" in another must come out as one line if it
// fits, and must never be cut between the two spans even when it does not.
func TestWrapNeverSplitsWideGlyphOrSpanBoundaryWord(t *testing.T) {
	se := NewSpan("Se", NewStyle(NewColour(1, 1, 1), DefaultColour, AttrBold))

	t.Run("a straddling word is not split at the span boundary", func(t *testing.T) {
		w := Wrap([]Span{se, NewSpan("lected", DefaultStyle)}, 8)
		if got := lineTexts(w); len(got) != 1 || got[0] != "Selected" {
			t.Errorf("lines = %q, want [Selected] on one line", got)
		}
		// Both halves must survive, each in its own style.
		line := w.Line(0)
		if len(line) != 2 || line[0].Text != "Se" || line[1].Text != "lected" {
			t.Errorf("line spans = %+v, want two spans preserving the split", line)
		}
		if line[0].Style != se.Style || line[1].Style != DefaultStyle {
			t.Errorf("per-rune styles were not preserved: %+v", line)
		}
	})

	t.Run("a straddling word is hard-cut, not split at the span boundary", func(t *testing.T) {
		// "Selected" holds no space, so there is no break opportunity anywhere and
		// the word is cut at the cell boundary with no marker. What must not
		// happen is a cut BETWEEN the two spans: at width 5 the cut lands at
		// index 5, which is inside the second span, not at the boundary.
		w := Wrap([]Span{se, NewSpan("lected", DefaultStyle)}, 5)
		got := lineTexts(w)
		if len(got) != 2 || got[0] != "Selec" || got[1] != "ted" {
			t.Errorf("lines = %q, want [Selec ted]", got)
		}
		if joined := strings.Join(got, ""); joined != "Selected" {
			t.Errorf("rejoining the lines gave %q, want %q", joined, "Selected")
		}
	})

	t.Run("a straddling word moves whole when a break opportunity follows it", func(t *testing.T) {
		// "Selected items": the space after the straddling word is the break, so
		// the whole word moves down even though its first two cells fit.
		w := Wrap([]Span{se, NewSpan("lected items", DefaultStyle)}, 9)
		got := lineTexts(w)
		if len(got) != 2 || got[0] != "Selected" || got[1] != "items" {
			t.Errorf("lines = %q, want [Selected items]", got)
		}
	})

	t.Run("a wide glyph is never split", func(t *testing.T) {
		w := Wrap([]Span{NewSpan("ab漢cd", DefaultStyle)}, 4)
		got := lineTexts(w)
		if len(got) != 2 || got[0] != "ab漢" || got[1] != "cd" {
			t.Errorf("lines = %q, want [ab漢 cd]: 漢 needs two cells and only one remained", got)
		}
		for i, line := range w.Lines {
			for _, s := range line {
				if StringWidth(s.Text) > 4 {
					t.Errorf("line %d holds %q, wider than 4 cells", i, s.Text)
				}
			}
		}
	})

	t.Run("a wide glyph straddling two spans is kept whole", func(t *testing.T) {
		// Width 3 holds 'a' and 漢 exactly; 'b' moves down. The point is that 漢
		// is never cut in half by the span boundary it straddles.
		w := Wrap([]Span{NewSpan("a漢", DefaultStyle), NewSpan("b", DefaultStyle)}, 3)
		if got := lineTexts(w); len(got) != 2 || got[0] != "a漢" || got[1] != "b" {
			t.Errorf("lines = %q, want [a漢 b]", got)
		}
	})

	t.Run("a wide glyph too wide for the area is dropped and its line omitted", func(t *testing.T) {
		// Width 1: 漢 cannot fit at all, so it is dropped and the line holding
		// nothing else disappears entirely rather than becoming an empty line.
		w := Wrap([]Span{NewSpan("漢", DefaultStyle)}, 1)
		if w.Height() != 0 {
			t.Errorf("Height() = %d, want 0: the line was empty and must be omitted", w.Height())
		}
		if len(w.Lines) != 0 {
			t.Errorf("Lines = %+v, want none", w.Lines)
		}

		// Mixed: the wide glyph is dropped and the surrounding text still wraps.
		w = Wrap([]Span{NewSpan("a漢b", DefaultStyle)}, 1)
		if got := lineTexts(w); len(got) != 2 || got[0] != "a" || got[1] != "b" {
			t.Errorf("lines = %q, want [a b]", got)
		}
	})
}

// TestWrapConsumesTheBreakSpaceAndPreservesOthers pins the rule that makes column
// alignment safe: the space that CAUSES a break disappears, and no other space
// is touched. Trimming a whole line would silently move every column after it.
func TestWrapConsumesTheBreakSpaceAndPreservesOthers(t *testing.T) {
	cases := []struct {
		name  string
		text  string
		width int
		want  []string
	}{
		{"no wrap needed", "short", 10, []string{"short"}},
		{"exact fit", "abcde", 5, []string{"abcde"}},

		{"break space consumed at exactly the right width", "aaa bbb", 4, []string{"aaa", "bbb"}},
		{"break space consumed when it does not fit", "aaa bbb", 3, []string{"aaa", "bbb"}},
		{"interior spaces preserved", "a  b", 10, []string{"a  b"}},
		{"leading space preserved", " a", 10, []string{" a"}},
		{"trailing space preserved", "a ", 10, []string{"a "}},
		// At width 3 the space at index 3 caused the break and is consumed; the
		// one at index 4 did not and survives at the head of the next line. That
		// is the whole rule: exactly one space dies per break, never a run.
		{"a second space survives at the head of the next line", "aaa  bbb", 3, []string{"aaa", " bb", "b"}},
		{"the earliest break wins, later spaces stay put", "aaa   bbb", 3, []string{"aaa", " ", "bbb"}},
		{"only the break space is consumed", "a b c", 1, []string{"a", "b", "c"}},
		{"word longer than width is cut with no marker", "abcdefgh", 3, []string{"abc", "def", "gh"}},
		{"hyphen is not a break opportunity", "ab-cd", 2, []string{"ab", "-c", "d"}},
		{"slash is not a break opportunity", "ab/cd", 2, []string{"ab", "/c", "d"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := Wrap([]Span{NewSpan(tc.text, DefaultStyle)}, tc.width)
			got := lineTexts(w)
			if len(got) != len(tc.want) {
				t.Fatalf("lines = %q (%d), want %q (%d)", got, len(got), tc.want, len(tc.want))
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("line %d = %q, want %q", i, got[i], tc.want[i])
				}
			}
			// No line may exceed the width, and no line may be empty.
			for i, line := range w.Lines {
				if len(line) == 0 {
					t.Errorf("line %d is empty; empty lines must be omitted", i)
				}
				if got := SpansWidth(line); got > tc.width {
					t.Errorf("line %d is %d cells, wider than %d", i, got, tc.width)
				}
			}
		})
	}
}

// TestWrapZeroWidthIsEmptyNotPanic states the degenerate-size contract ADR 0007
// §4 requires of every helper: a size of zero is a VALID size, so it returns an
// empty result rather than panicking or looping.
func TestWrapZeroWidthIsEmptyNotPanic(t *testing.T) {
	for _, width := range []int{0, -1, -100, minInt} {
		for _, spans := range [][]Span{
			nil,
			{},
			{NewSpan("", DefaultStyle)},
			{NewSpan("hello world", DefaultStyle)},
			{NewSpan("漢", DefaultStyle)},
		} {
			w := Wrap(spans, width)
			if w.Height() != 0 {
				t.Errorf("Wrap(%q, %d).Height() = %d, want 0", spanText(spans), width, w.Height())
			}
			if len(w.Lines) != 0 {
				t.Errorf("Wrap(%q, %d).Lines = %+v, want none", spanText(spans), width, w.Lines)
			}
			if w.Width != width {
				t.Errorf("Wrap(.., %d).Width = %d, want the width it was given", width, w.Width)
			}
			if w.Line(0) != nil {
				t.Errorf("Wrap(.., %d).Line(0) = %+v, want nil", width, w.Line(0))
			}
		}
	}
}

// minInt is the most negative int, used to prove the width check is a signed
// comparison rather than an equality against zero.
const minInt = -1 << 62

// TestWrapEmptyInputIsEmpty covers the cases where there is nothing to wrap:
// the result must be indistinguishable from a zero-width wrap.
func TestWrapEmptyInputIsEmpty(t *testing.T) {
	for _, spans := range [][]Span{nil, {}, {NewSpan("", DefaultStyle)}, {NewSpan("", BoldStyle)}} {
		w := Wrap(spans, 20)
		if w.Height() != 0 {
			t.Errorf("Wrap(%+v, 20).Height() = %d, want 0", spans, w.Height())
		}
		if w.Width != 20 {
			t.Errorf("Wrap(.., 20).Width = %d, want 20 echoed back", w.Width)
		}
	}
}

// TestWrapLineOutOfRangeIsNil keeps the degenerate contract for indexing: a
// widget asked for more lines than the text has must get nil and draw nothing,
// not panic and not blank itself (ADR 0007 §4, "clip, never blank").
func TestWrapLineOutOfRangeIsNil(t *testing.T) {
	w := Wrap([]Span{NewSpan("one two", DefaultStyle)}, 3)
	if w.Height() != 2 {
		t.Fatalf("Height = %d, want 2", w.Height())
	}
	for _, i := range []int{-1, -100, 2, 3, 1 << 30} {
		if got := w.Line(i); got != nil {
			t.Errorf("Line(%d) = %+v, want nil", i, got)
		}
	}
	if w.Line(0) == nil || w.Line(1) == nil {
		t.Error("an in-range line must not be nil")
	}
}

// TestWrapMergesSameStyledRunes checks that a wrapped line is a handful of spans
// rather than one per rune, and — the part that matters for correctness — that
// spans sharing a style are merged only when they really do share it.
func TestWrapMergesSameStyledRunes(t *testing.T) {
	w := Wrap([]Span{NewSpan("abcdef", DefaultStyle)}, 3)
	if len(w.Line(0)) != 1 || w.Line(0)[0].Text != "abc" {
		t.Errorf("line 0 = %+v, want a single span of %q", w.Line(0), "abc")
	}

	// Two spans with different styles must stay separate even though they are
	// adjacent text.
	a := NewStyle(NewColour(1, 1, 1), DefaultColour, 0)
	b := NewStyle(NewColour(2, 2, 2), DefaultColour, 0)
	w = Wrap([]Span{NewSpan("ab", a), NewSpan("cd", b)}, 4)
	if len(w.Line(0)) != 2 {
		t.Errorf("line 0 has %d spans, want 2: differing styles must not merge", len(w.Line(0)))
	}
}

// TestTruncateAppendsMarkerInLastSpanStyle pins the marker contract from both
// ends: the marker wears the style of the last span kept, and both suffix
// constants are exactly one cell wide so the two rungs are interchangeable.
func TestTruncateAppendsMarkerInLastSpanStyle(t *testing.T) {
	red := NewStyle(NewColour(255, 0, 0), DefaultColour, AttrBold)
	blue := NewStyle(NewColour(0, 0, 255), DefaultColour, 0)
	spans := []Span{NewSpan("hello ", red), NewSpan("world", blue)}

	for _, tc := range []struct {
		name  string
		trunc func([]Span, int) []Span
		mark  string
	}{
		{"unicode", Truncate, TruncSuffix},
		{"ascii", TruncateASCII, AscTruncSuffix},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := StringWidth(tc.mark); got != 1 {
				t.Fatalf("%s is %d cells wide, want 1; the two rungs must not shift a layout", tc.mark, got)
			}

			// Keep "hello " (6 cells) plus "w" from the second span, then the
			// marker: 8 cells.
			got := tc.trunc(spans, 8)
			if w := SpansWidth(got); w != 8 {
				t.Errorf("truncated width = %d, want 8", w)
			}
			last := got[len(got)-1]
			if last.Text != tc.mark {
				t.Errorf("marker = %q, want %q", last.Text, tc.mark)
			}
			if last.Style != blue {
				t.Errorf("marker style = %+v, want the last KEPT span's %+v", last.Style, blue)
			}
			if text := spanText(got); text != "hello w"+tc.mark {
				t.Errorf("text = %q, want %q", text, "hello w"+tc.mark)
			}

			// The full text fits: unchanged, and the same backing array.
			if got := tc.trunc(spans, 11); len(got) != 2 {
				t.Errorf("a fitting input came back with %d spans, want the 2 it was given", len(got))
			}
		})
	}
}

// TestTruncSuffixesAreOneCellWide is its own test because it is the property the
// pair of constants exists for, and a future "smarter" ASCII marker (three dots,
// or "-") would break the equivalence silently.
func TestTruncSuffixesAreOneCellWide(t *testing.T) {
	for _, m := range []string{TruncSuffix, AscTruncSuffix} {
		if got := RuneWidth([]rune(m)[0]); got != 1 {
			t.Errorf("%q is %d cells wide, want 1", m, got)
		}
		if StringWidth(m) != 1 {
			t.Errorf("StringWidth(%q) = %d, want 1", m, StringWidth(m))
		}
	}
	if TruncSuffix == AscTruncSuffix {
		t.Error("the two markers are identical; the ASCII rung would have no point")
	}
}

// TestTruncateBoundaries walks every boundary of the width arithmetic,
// including the degenerate ones, because the marker's cell has to come from
// somewhere and maxWidth-1 is where off-by-one bugs live.
func TestTruncateBoundaries(t *testing.T) {
	a := NewStyle(NewColour(1, 1, 1), DefaultColour, 0)
	b := NewStyle(NewColour(2, 2, 2), DefaultColour, AttrBold)
	input := []Span{NewSpan("abcdef", a), NewSpan("ghij", b)}

	cases := []struct {
		name     string
		maxWidth int
		wantText string
		wantNil  bool
	}{
		{"negative width", -1, "", true},
		{"large negative width", -1000, "", true},
		{"zero width", 0, "", true},
		{"width 1 is the marker alone", 1, TruncSuffix, false},
		{"width 2 keeps one cell", 2, "a" + TruncSuffix, false},
		{"width 3 keeps two cells", 3, "ab" + TruncSuffix, false},
		{"width 7 keeps a whole span", 7, "abcdef" + TruncSuffix, false},
		{"width 8 splits across the span boundary", 8, "abcdefg" + TruncSuffix, false},
		{"width 10 exact fit is unchanged", 10, "abcdefghij", false},
		{"width 11 past the fit", 11, "abcdefghij", false},
		{"width 4 cuts inside the second span", 4, "abc" + TruncSuffix, false},
	}
	for _, tc := range cases {
		for _, variant := range []struct {
			name  string
			trunc func([]Span, int) []Span
			mark  string
		}{
			{"Truncate", Truncate, TruncSuffix},
			{"TruncateASCII", TruncateASCII, AscTruncSuffix},
		} {
			t.Run(tc.name+"/"+variant.name, func(t *testing.T) {
				got := variant.trunc(input, tc.maxWidth)
				if tc.wantNil {
					if len(got) != 0 {
						t.Fatalf("maxWidth %d returned %+v, want an empty slice", tc.maxWidth, got)
					}
					return
				}
				want := tc.wantText
				if strings.Contains(want, TruncSuffix) {
					want = strings.ReplaceAll(want, TruncSuffix, variant.mark)
				}
				if text := spanText(got); text != want {
					t.Errorf("maxWidth %d gave %q, want %q", tc.maxWidth, text, want)
				}
				if w := SpansWidth(got); w > tc.maxWidth {
					t.Errorf("maxWidth %d produced %d cells, wider than the budget", tc.maxWidth, w)
				}
			})
		}
	}
}

// TestTruncateReturnsInputUnchangedWhenItFits pins the no-allocation fast path,
// because it is what makes Truncate cheap enough to call from a size-change
// check on every frame of a resize.
func TestTruncateReturnsInputUnchangedWhenItFits(t *testing.T) {
	input := []Span{NewSpan("abc", DefaultStyle), NewSpan("def", DefaultStyle)}
	before := &input[0]

	for _, maxWidth := range []int{6, 7, 1000} {
		got := Truncate(input, maxWidth)
		if len(got) != 2 {
			t.Fatalf("maxWidth %d returned %d spans, want 2", maxWidth, len(got))
		}
		if &got[0] != before {
			t.Errorf("maxWidth %d copied a fitting input; the fast path must share the backing array", maxWidth)
		}
	}
}

// TestTruncateDoesNotAllocateWhenItFits is the measurement behind the fast path.
// A fitting input is the common case on a widget whose content fits, and it must
// not cost a heap object.
func TestTruncateDoesNotAllocateWhenItFits(t *testing.T) {
	input := []Span{NewSpan("abc", DefaultStyle), NewSpan("def", DefaultStyle)}
	if got := testing.AllocsPerRun(100, func() { Truncate(input, 6) }); got != 0 {
		t.Errorf("Truncate on a fitting input allocated %.1f objects per run, want 0", got)
	}
}

// TestTruncateNeverSplitsAWideGlyph keeps truncation consistent with SetString,
// SetSpans and Wrap: half a glyph is never emitted.
func TestTruncateNeverSplitsAWideGlyph(t *testing.T) {
	// "a漢bcd" is 6 cells, so a width of 5 truncates and the marker competes
	// with 漢 for the last two cells.
	input := []Span{NewSpan("a漢bcd", DefaultStyle)}

	// Budget 2 after the marker: 'a' fits, 漢 needs 2 more and is dropped rather
	// than half-taken.
	got := Truncate(input, 3)
	if text := spanText(got); text != "a"+TruncSuffix {
		t.Errorf("got %q, want %q", text, "a"+TruncSuffix)
	}
	if w := SpansWidth(got); w != 2 {
		t.Errorf("width = %d, want 2", w)
	}

	// Budget 3: 'a' plus 漢 both fit; the trailing 'c' is what is lost.
	got = Truncate(input, 4)
	if text := spanText(got); text != "a漢"+TruncSuffix {
		t.Errorf("got %q, want %q", text, "a漢"+TruncSuffix)
	}
	if w := SpansWidth(got); w != 4 {
		t.Errorf("width = %d, want 4", w)
	}

	// Budget 4: the whole run is kept and the marker follows it.
	got = Truncate(input, 5)
	if text := spanText(got); text != "a漢b"+TruncSuffix {
		t.Errorf("got %q, want %q", text, "a漢b"+TruncSuffix)
	}

	// A width the text fits exactly takes the no-allocation fast path and adds
	// no marker: the input already fit, so nothing was truncated.
	got = Truncate(input, 6)
	if text := spanText(got); text != "a漢bcd" {
		t.Errorf("a fitting input was marked: %q", text)
	}
}

// TestTruncateMarksInAReasonableStyleWhenNothingFits covers maxWidth == 1,
// where there is no "last span kept" to take the marker's style from. Falling
// back to DefaultStyle there would render the marker as opaque black — the exact
// surprise the Style sentinel exists to prevent.
func TestTruncateMarksInAReasonableStyleWhenNothingFits(t *testing.T) {
	// The first rune is wide and the budget is one cell, so nothing is kept.
	input := []Span{
		NewSpan("", DefaultStyle),
		NewSpan("漢", NewStyle(NewColour(9, 9, 9), DefaultColour, AttrItalic)),
	}
	got := Truncate(input, 1)
	if len(got) != 1 {
		t.Fatalf("got %+v, want just the marker", got)
	}
	if got[0].Text != TruncSuffix {
		t.Errorf("marker = %q", got[0].Text)
	}
	if got[0].Style != input[1].Style {
		t.Errorf("marker style = %+v, want the first non-empty input span's %+v", got[0].Style, input[1].Style)
	}

	// An entirely empty input has no style to borrow and falls back to
	// DefaultStyle rather than the zero Style.
	got = Truncate([]Span{NewSpan("", DefaultStyle)}, 1)
	if len(got) != 1 || got[0].Style != DefaultStyle {
		t.Errorf("empty input gave %+v, want one marker in DefaultStyle", got)
	}
}

// TestWrapAndTruncateAreAllocatingAndThusNotForDraw pins ADR 0008 §4's rule
// from the side that makes it enforceable: the helpers allocate, which is WHY
// they must not be called inside Draw, and a draw-path caller would therefore
// show up in a benchmark's allocs/op column.
//
// The widget-side half of the contract is that a cached Wrap read in Draw costs
// nothing, which TestWrapResultCanBeReadWithoutAllocating covers.
func TestWrapAndTruncateAreAllocatingAndThusNotForDraw(t *testing.T) {
	input := []Span{NewSpan("the quick brown fox jumps over the lazy dog", DefaultStyle)}

	if got := testing.AllocsPerRun(50, func() { Wrap(input, 12) }); got == 0 {
		t.Error("Wrap allocated nothing; the doc comment's whole argument is that it allocates")
	}
	if got := testing.AllocsPerRun(50, func() { Truncate(input, 12) }); got == 0 {
		t.Error("Truncate allocated nothing; it must, or a caller could not tell it is the expensive one")
	}
}

// TestWrapResultCanBeReadWithoutAllocating is the other half: once wrapped and
// cached, reading a line inside Draw must be free. A widget that caches Wrapped
// and indexes it therefore satisfies ADR 0008 §4, and this is what that looks
// from the buffer's side.
func TestWrapResultCanBeReadWithoutAllocating(t *testing.T) {
	cached := Wrap([]Span{NewSpan("the quick brown fox", DefaultStyle)}, 10)
	if cached.Height() == 0 {
		t.Fatal("nothing wrapped")
	}
	// Everything a Draw would do with the cached value.
	if got := testing.AllocsPerRun(100, func() {
		for i := 0; i < cached.Height(); i++ {
			_ = cached.Line(i)
			_ = SpansWidth(cached.Line(i))
		}
	}); got != 0 {
		t.Errorf("reading a cached Wrapped allocated %.1f objects per run, want 0", got)
	}
}

// TestNewSpanKeepsTheAuthoredStyle checks that NewSpan does not resolve, so a
// cached []Span keeps the author's intent and Patch still has UnsetColour
// channels to work with.
func TestNewSpanKeepsTheAuthoredStyle(t *testing.T) {
	st := Style{FG: NewColour(1, 2, 3), BG: UnsetColour}
	s := NewSpan("x", st)
	if s.Style != st {
		t.Errorf("NewSpan resolved the style: %+v, want %+v", s.Style, st)
	}
	if s.Text != "x" {
		t.Errorf("Text = %q", s.Text)
	}
}
