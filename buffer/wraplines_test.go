package buffer

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
)

// The tests here cover the two things Wrap learned after its first release: that
// a newline is structure rather than a zero-width space, and that the mapping
// back into the caller's text is part of the result rather than something a
// widget re-derives from the consumed-space rule.

// TestWrapTreatsNewlineAsAHardBreak is the newline contract stated as a table,
// because each case is a way the old behaviour mangled text and each is silent:
// nothing errored, the paragraph just came back as one unwrapped block.
//
// The blank-line cases are the ones a static renderer and an editable widget both
// depend on. "a\n\nb" is THREE lines, not one and not two, and "a\n" is two: a
// trailing newline opens a final empty logical line, which is where the caret
// sits after the last Enter.
func TestWrapTreatsNewlineAsAHardBreak(t *testing.T) {
	cases := []struct {
		name  string
		text  string
		width int
		want  []string
	}{
		{"one newline", "a\nb", 10, []string{"a", "b"}},
		{"trailing newline", "a\n", 10, []string{"a", ""}},
		{"leading newline", "\na", 10, []string{"", "a"}},
		{"two newlines", "a\n\nb", 10, []string{"a", "", "b"}},
		{"only a newline", "\n", 10, []string{"", ""}},
		{"only newlines", "\n\n", 10, []string{"", "", ""}},
		{"newline wins over a wide area", "a\nb", 40, []string{"a", "b"}},
		{"hard break after a soft one", "one two\nthree", 5, []string{"one", "two", "three"}},
		{"newline inside a wrapped word run", "ab cd\nef gh", 3, []string{"ab", "cd", "ef", "gh"}},
		{"space before a newline is kept", "ab \ncd", 10, []string{"ab ", "cd"}},
		{"newline inside a one-column area", "a\nb", 1, []string{"a", "b"}},
		{"empty text", "", 10, nil},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := Wrap([]Span{NewSpan(c.text, DefaultStyle)}, c.width)
			got := lineTexts(w)
			if len(got) != len(c.want) {
				t.Fatalf("Wrap(%q, %d) produced %d lines %q, want %d %q",
					c.text, c.width, len(got), got, len(c.want), c.want)
			}
			for i := range c.want {
				if got[i] != c.want[i] {
					t.Errorf("Wrap(%q, %d) line %d = %q, want %q", c.text, c.width, i, got[i], c.want[i])
				}
			}
			if len(w.Ranges) != len(w.Lines) {
				t.Errorf("Wrap(%q, %d): %d ranges for %d lines", c.text, c.width, len(w.Ranges), len(w.Lines))
			}
		})
	}
}

// TestWrapMultiLineStringRoundTrips is the load-bearing property of the whole
// entry point: the ranges tile the input, and the runes the lines display are
// exactly the runes the ranges cover that occupy a cell. A caller that keeps the
// ranges can therefore walk back to its own text and know precisely which runes
// it did not see on screen.
func TestWrapMultiLineStringRoundTrips(t *testing.T) {
	cases := []string{
		"a\nb",
		"a\n\nb",
		"a\n",
		"\n",
		"one two three\nfour five six",
		"first\nsecond line long enough to wrap\nthird",
		"trailing spaces   \n   leading spaces",
		"漢字\n漢字wide\nmixed 漢字 text",
		"accented é text\nsecond line",
		"tab\tseparated\nvalues here",
		"漢字b\nc",
	}
	for _, text := range cases {
		for width := 1; width <= 12; width++ {
			checkWrapMapping(t, text, width)
		}
	}
}

// TestWrapRangesSurviveManyGeneratedInputs is the property version of the same
// invariants, over a corpus built from exactly the runes the contract treats
// specially: the space that is a break opportunity, the newline that is a hard
// break, the double-width glyph and the combining mark.
//
// The invariants are the ones an editable widget relies on, and each is a way
// the previous re-derivation in widgets/form was wrong:
//
//   - len(Ranges) == len(Lines), and the ranges run forward without overlapping,
//     with only unplaceable or consumed runes outside them
//   - a line displays exactly the cell-bearing runes of its own range
//   - the runes between two lines are only break runes and runes the wrap could
//     not place — never a character the reader would expect to see
//   - the runes the lines display are exactly the covered runes, in order
func TestWrapRangesSurviveManyGeneratedInputs(t *testing.T) {
	rng := rand.New(rand.NewSource(20261004))
	alphabet := []rune("ab c\n漢́é")

	for iter := 0; iter < 4000; iter++ {
		n := rng.Intn(20)
		var text strings.Builder
		for i := 0; i < n; i++ {
			text.WriteRune(alphabet[rng.Intn(len(alphabet))])
		}
		input := text.String()
		width := rng.Intn(9) // 0..8, including the degenerate 0

		w := Wrap([]Span{NewSpan(input, DefaultStyle)}, width)
		if width <= 0 {
			if w.Height() != 0 {
				t.Fatalf("Wrap(%q, %d).Height() = %d, want 0", input, width, w.Height())
			}
			continue
		}
		checkWrapMapping(t, input, width)
		_ = w
	}
}

// checkWrapMapping asserts the mapping invariants for one input and width. It is
// shared by the table-driven round-trip test and the generated property test
// because they assert the SAME thing, and two copies of an invariant are two
// invariants.
func checkWrapMapping(t *testing.T, input string, width int) {
	t.Helper()
	runes := []rune(input)
	w := Wrap([]Span{NewSpan(input, DefaultStyle)}, width)

	if len(w.Ranges) != w.Height() {
		t.Fatalf("Wrap(%q, %d): %d ranges for %d lines", input, width, len(w.Ranges), w.Height())
	}
	if w.Height() == 0 {
		// No lines is correct only when there was nothing that could go on one:
		// input of zero-width runes alone displays nothing, and a double-width
		// glyph in a one-column area is dropped with its line omitted. Both are
		// the wrap's documented behaviour rather than a lost line.
		for _, r := range runes {
			if RuneWidth(r) > 0 && !(width < 2 && RuneWidth(r) == 2) {
				t.Fatalf("Wrap(%q, %d) produced no lines although %q occupies a cell", input, width, string(r))
			}
		}
		return
	}
	// skipped asserts that a run of input runes between two lines — or after the
	// last one — is only made of runes the wrap is allowed to skip: the one break
	// rune it consumed, the zero-width marks it dropped, and a double-width glyph
	// a one-column area cannot place at all. It is the whole of "nothing visible
	// was lost", and it is one function because the gap and the tail are the same
	// question.
	skipped := func(gap []rune, where string) {
		t.Helper()
		breaks := 0
		for _, g := range gap {
			switch {
			case g == ' ' || g == '\n':
				breaks++
			case RuneWidth(g) == 0:
			case width < 2 && RuneWidth(g) == 2:
			default:
				t.Fatalf("Wrap(%q, %d): %q is %s, so the wrap dropped visible content", input, width, string(g), where)
			}
		}
		if breaks > 1 {
			t.Fatalf("Wrap(%q, %d) consumed %d break runes %s, want at most one", input, width, breaks, where)
		}
	}

	// displayed collects every rune the lines show, in order. It is compared with
	// the runes the ranges cover, which is the whole content of the mapping: the
	// ranges say what was skipped, and what was skipped must be exactly the runes
	// the contract allows to go.
	var displayed strings.Builder
	for i := 0; i < w.Height(); i++ {
		r := w.Range(i)
		if r.Start > r.End || r.End > len(runes) {
			t.Fatalf("Wrap(%q, %d) line %d has range %d..%d, outside 0..%d",
				input, width, i, r.Start, r.End, len(runes))
		}
		if i > 0 && r.Start < w.Range(i-1).End {
			t.Fatalf("Wrap(%q, %d) line %d starts at %d, before the previous line's end %d",
				input, width, i, r.Start, w.Range(i-1).End)
		}

		// Everything displayed must come from this line's own range, and must be
		// the range's cell-bearing runes in order.
		var fromRange strings.Builder
		for _, rr := range runes[r.Start:r.End] {
			if RuneWidth(rr) > 0 {
				fromRange.WriteRune(rr)
			}
		}
		if got, want := spanText(w.Line(i)), fromRange.String(); got != want {
			t.Fatalf("Wrap(%q, %d) line %d = %q, want the display of %q", input, width, i, got, want)
		}
		displayed.WriteString(spanText(w.Line(i)))

		if i+1 < w.Height() {
			skipped(runes[r.End:w.Range(i+1).Start],
				fmt.Sprintf("between lines %d and %d", i, i+1))
		}
	}
	// The runes after the last line are the same question as a gap: a trailing
	// space that caused a break is consumed by the contract, and a wide glyph at
	// the end of a one-column area is dropped along with its omitted line.
	skipped(runes[w.Range(w.Height()-1).End:], "after the last line")
	// And so are the runes before the first one, which only a dropped glyph at the
	// very front of the text can leave there.
	skipped(runes[:w.Range(0).Start], "before the first line")

	// And nothing outside the ranges is displayed: rebuild the covered runes and
	// compare with what the lines showed.
	var covered strings.Builder
	for i := 0; i < w.Height(); i++ {
		for _, rr := range runes[w.Range(i).Start:w.Range(i).End] {
			if RuneWidth(rr) > 0 {
				covered.WriteRune(rr)
			}
		}
	}
	if got, want := displayed.String(), covered.String(); got != want {
		t.Fatalf("Wrap(%q, %d) displayed %q but its ranges cover %q", input, width, got, want)
	}
}

// TestWrapRangesAreRuneIndicesNotCellsAndNotBytes is the offset-basis test, and
// it is the one a widget that moves a caret depends on: a double-width glyph is
// two cells and one rune, a combining mark occupies no cell at all and is still a
// rune in the caller's buffer. Counting cells, or counting only the runes that
// survived, addresses the wrong rune from the first one of either onwards.
//
// The input carries one of each on purpose:
//
//	index  0 1  2   3    4 5    6  7     8
//	rune   a b 漢 ' '  c  d  U+0301 \n    e
//
// Line 1 is "cd" and its range is 4..7, so the combining mark at 6 is INSIDE the
// line it belongs to. A range that counted only the runes that reached a cell
// would end at 6 and address the mark as the line's first rune.
func TestWrapRangesAreRuneIndicesNotCellsAndNotBytes(t *testing.T) {
	const text = "ab漢 cd́\ne"
	runes := []rune(text)

	w := Wrap([]Span{NewSpan(text, DefaultStyle)}, 4)
	if got, want := lineTexts(w), []string{"ab漢", "cd", "e"}; len(got) != len(want) {
		t.Fatalf("Wrap(%q, 4) produced %q, want %q", text, got, want)
	} else {
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("Wrap(%q, 4) line %d = %q, want %q", text, i, got[i], want[i])
			}
		}
	}

	want := []LineRange{{0, 3}, {4, 7}, {8, 9}}
	for i, exp := range want {
		if got := w.Range(i); got != exp {
			t.Errorf("line %d range = %d..%d, want %d..%d (rune indices into %q)",
				i, got.Start, got.End, exp.Start, exp.End, text)
		}
	}
	// The mapping has to be usable, not merely plausible: indexing the caller's
	// own rune slice with it must produce the line's text.
	if got := string(runes[w.Range(0).Start:w.Range(0).End]); got != "ab漢" {
		t.Errorf("runes[0:3] = %q, want %q: the offsets are rune indices, not cells", got, "ab漢")
	}
	if got := string(runes[w.Range(1).Start:w.Range(1).End]); got != "cd́" {
		t.Errorf("runes[4:7] = %q, want the combining mark to be inside its line's range", got)
	}
}

// TestWrapRangesHoldWhenTheWideGlyphHasNowhereToGo is the omitted-line case: a
// one-column area cannot hold a double-width glyph, so Wrap drops the glyph and
// emits no line for it. Lines and Ranges must still be in step, or every index a
// caller derives from one is off against the other.
func TestWrapRangesHoldWhenTheWideGlyphHasNowhereToGo(t *testing.T) {
	w := Wrap([]Span{NewSpan("a漢b", DefaultStyle)}, 1)
	if w.Height() != 2 {
		t.Fatalf("Wrap(%q, 1) produced %q, want the two narrow runes and nothing for the glyph",
			"a漢b", lineTexts(w))
	}
	if len(w.Ranges) != w.Height() {
		t.Fatalf("%d ranges for %d lines", len(w.Ranges), w.Height())
	}
	if got := w.Range(1); got != (LineRange{Start: 2, End: 3}) {
		t.Errorf("line 1 range = %+v, want {2 3}: the dropped glyph's rune belongs to no line", got)
	}
}

// TestWrapRangeIsSafeOutOfRange is the "never panics" half of Line's contract,
// applied to the new accessor: a widget asking about a line it cannot show gets a
// zero range, not a crash.
func TestWrapRangeIsSafeOutOfRange(t *testing.T) {
	w := Wrap([]Span{NewSpan("a\nb", DefaultStyle)}, 10)
	for _, i := range []int{-1, 2, 99} {
		if got := w.Range(i); got != (LineRange{}) {
			t.Errorf("Range(%d) = %+v, want the zero range", i, got)
		}
	}
}

// TestWrapPreservesStyleAcrossAHardBreak is the styling half of the newline rule:
// a break is a break of TEXT, not of style, so the newline itself must not merge
// two differently styled runs into one span on either side of it.
//
// Each span ends at a newline, so every line belongs to exactly one style and a
// merged span across the break would be obvious.
func TestWrapPreservesStyleAcrossAHardBreak(t *testing.T) {
	red := DefaultStyle.WithAttr(AttrBold)
	blue := DefaultStyle.WithAttr(AttrItalic)
	green := DefaultStyle.WithAttr(AttrUnderline)
	w := Wrap([]Span{NewSpan("a\n", red), NewSpan("b\n", blue), NewSpan("c", green)}, 10)

	want := []string{"a", "b", "c"}
	got := lineTexts(w)
	if len(got) != len(want) {
		t.Fatalf("Wrap produced %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("line %d = %q, want %q", i, got[i], want[i])
		}
		line := w.Line(i)
		if len(line) != 1 {
			t.Fatalf("line %d = %+v, want one span: a newline must not merge the runs beside it", i, line)
		}
	}
	for i, want := range []Style{red, blue, green} {
		if line := w.Line(i); len(line) == 1 && line[0].Style != want {
			t.Errorf("line %d style = %+v, want %+v", i, line[0].Style, want)
		}
	}
}
