package basic

import (
	"strings"
	"testing"

	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/geometry"
	"github.com/serkanalgur/termmosaic/widgets/widgettest"
)

// screen renders root on a w-by-h screen and returns the rows with trailing
// spaces trimmed.
func screen(t *testing.T, w, h int, root termmosaic.Widget) []string {
	t.Helper()
	sink := widgettest.Render(t, w, h, 1, root)
	out := make([]string, h)
	for y := 0; y < h; y++ {
		out[y] = widgettest.Row(sink, y)
	}
	return out
}

// wantRows asserts the rendered screen equals the given rows. Both sides are
// compared with trailing spaces trimmed, so a test need not count the background
// blanks a widget paints.
func wantRows(t *testing.T, got []string, expect ...string) {
	t.Helper()
	for i := range got {
		w := ""
		if i < len(expect) {
			w = strings.TrimRight(expect[i], " ")
		}
		if got[i] != w {
			t.Errorf("row %d\n got %q\nwant %q", i, got[i], w)
		}
	}
	if len(expect) > len(got) {
		t.Errorf("expected %d rows, rendered %d", len(expect), len(got))
	}
}

// ---------------------------------------------------------------------------
// Text
// ---------------------------------------------------------------------------

// TestTextDrawsOneLineOfSpansInEachStyle is the ordinary case: styled runs, each
// in its own style, on the top row of Bounds.
func TestTextDrawsOneLineOfSpansInEachStyle(t *testing.T) {
	red := buffer.NewColour(0xff, 0, 0)
	blue := buffer.NewColour(0, 0, 0xff)
	txt := NewText(buffer.Rect{W: 20, H: 1}, []buffer.Span{
		buffer.NewSpan("ab", buffer.NewStyle(red, buffer.DefaultColour, 0)),
		buffer.NewSpan("cd", buffer.NewStyle(blue, buffer.DefaultColour, 0)),
	})
	sink := widgettest.Render(t, 20, 1, 1, txt)

	if got := widgettest.Row(sink, 0); got != "abcd" {
		t.Errorf("row 0 = %q, want %q", got, "abcd")
	}
	if got := sink.CellAt(0, 0).FG; got != red {
		t.Errorf("cell 0 fg = %#x, want %#x", got, red)
	}
	if got := sink.CellAt(3, 0).FG; got != blue {
		t.Errorf("cell 3 fg = %#x, want %#x: each span keeps its own style", got, blue)
	}
}

// TestTextUsesItsOwnBoundsNotTheBuffer pins ADR 0007 §1 rule 1, the single most
// likely mistake in the catalog: the widget's space is Bounds(), never buf.Size().
// A 40-wide screen with a 10-wide Text must show 10 cells of text.
func TestTextUsesItsOwnBoundsNotTheBuffer(t *testing.T) {
	txt := NewTextString(buffer.Rect{X: 2, W: 10, H: 1}, "0123456789", buffer.PlainStyle)
	wantRows(t, screen(t, 40, 1, txt), "  0123456789")
}

// TestTextTruncatesRatherThanSpilling checks that an over-long label is truncated
// inside the widget's own rect.
//
// It matters because SetSpans bounds-checks against the BUFFER, not against the
// widget's rect: without an explicit truncation a 10-cell Text beside a neighbour
// in a 40-cell screen would write straight over it.
func TestTextTruncatesRatherThanSpilling(t *testing.T) {
	txt := NewTextString(buffer.Rect{W: 8, H: 1}, "abcdefghijklmno", buffer.PlainStyle)
	got := screen(t, 20, 1, txt)
	wantRows(t, got, "abcdefg"+buffer.TruncSuffix)

	if n := buffer.StringWidth(got[0]); n != 8 {
		t.Errorf("row is %d cells, want 8: the text must not exceed Bounds", n)
	}
}

// TestTextTruncatesOnShrink is the half of the resize contract a grow-only test
// cannot catch: the truncation is keyed on the rect, so a label that fit in 20
// cells and is then shown in 5 must be recomputed.
func TestTextTruncatesOnShrink(t *testing.T) {
	txt := NewTextString(buffer.Rect{W: 20, H: 1}, "abcdefghij", buffer.PlainStyle)
	buf := buffer.NewBuffer(20, 1)
	txt.Draw(buf)
	if got := rowOf(buf, 0, 20); got != "abcdefghij" {
		t.Fatalf("setup: got %q, want the untruncated text", got)
	}

	txt.SetBounds(buffer.Rect{W: 5, H: 1})
	txt.Draw(buf)
	// Only the cells inside the new bounds are this widget's business: the ones
	// past column 5 belong to whatever is composed beside it, and repainting them
	// would be a widget writing outside Bounds.
	if got := rowOf(buf, 0, 5); got != "abcd"+buffer.TruncSuffix {
		t.Errorf("after shrinking to 5: got %q, want %q", got, "abcd"+buffer.TruncSuffix)
	}
}

// TestTextAlignmentPlacesTheRun checks all three alignments, and reads the result
// as the column the run starts at: alignment controls leading space, and a padded
// expected string would only be testing the background.
func TestTextAlignmentPlacesTheRun(t *testing.T) {
	for _, tc := range []struct {
		name  string
		align geometry.Align
		at    int
	}{
		{"left", geometry.AlignLeft, 0},
		{"center", geometry.AlignCenter, 4},
		{"right", geometry.AlignRight, 8},
	} {
		t.Run(tc.name, func(t *testing.T) {
			txt := NewTextString(buffer.Rect{W: 10, H: 1}, "ab", buffer.PlainStyle)
			txt.SetAlign(tc.align)
			got := screen(t, 10, 1, txt)[0]
			if at := strings.Index(got, "ab"); at != tc.at {
				t.Errorf("\n got %q (run starts at %d)\nwant the run at column %d", got, at, tc.at)
			}
		})
	}
}

// TestTextRepaintsItsWholeBounds is ADR 0007 §1 rule 3: content shrinking inside
// the same rect must not leave the old cells behind.
func TestTextRepaintsItsWholeBounds(t *testing.T) {
	txt := NewTextString(buffer.Rect{W: 12, H: 1}, "abcdefghij", buffer.PlainStyle)
	buf := buffer.NewBuffer(12, 1)
	txt.Draw(buf)
	if got := rowOf(buf, 0, 12); got != "abcdefghij" {
		t.Fatalf("setup: got %q", got)
	}

	txt.SetText("ab", buffer.PlainStyle)
	txt.Draw(buf)
	if got := rowOf(buf, 0, 12); got != "ab" {
		t.Errorf("got %q, want %q: the rest of the row must have been repainted", got, "ab")
	}
}

// TestTextRepaintsItsBackgroundWhenItChanges is the same rule for style rather than
// content, which is the case a background-less widget cannot satisfy.
func TestTextRepaintsItsBackgroundWhenItChanges(t *testing.T) {
	one := buffer.NewStyle(buffer.DefaultColour, buffer.NewColour(0, 0, 0), 0)
	two := buffer.NewStyle(buffer.DefaultColour, buffer.NewColour(0xff, 0, 0), 0)

	txt := NewTextString(buffer.Rect{W: 6, H: 1}, "ab", one)
	buf := buffer.NewBuffer(6, 1)
	txt.Draw(buf)
	txt.SetBackground(two)
	txt.Draw(buf)

	// Columns 2 and up: a glyph cell wears its SPAN's style, so only the cells the
	// text does not cover can show the widget's background.
	for x := 2; x < 6; x++ {
		if got := buf.CellAt(x, 0).BG; got != two.BG {
			t.Fatalf("cell %d bg = %s, want %s", x, got, two.BG)
		}
	}
}

// TestTextDrawIsTotalAtEverySize: every rect, including empty and 1x1, defined
// and panic-free, for every Align including an out-of-range one.
func TestTextDrawIsTotalAtEverySize(t *testing.T) {
	for w := 0; w <= 8; w++ {
		for h := 0; h <= 4; h++ {
			for _, align := range []geometry.Align{
				geometry.AlignLeft, geometry.AlignCenter, geometry.AlignRight, geometry.Align(9),
			} {
				txt := NewTextString(buffer.Rect{W: w, H: h}, "a long label", buffer.PlainStyle)
				txt.SetAlign(align)
				widgettest.Render(t, atLeast1(w), atLeast1(h), 1, txt)
			}
		}
	}
}

// TestTextDrawIsAllocationFree: the truncation happens on the size change, not per
// frame.
func TestTextDrawIsAllocationFree(t *testing.T) {
	for _, w := range []int{20, 8, 3} {
		txt := NewTextString(buffer.Rect{W: w, H: 1}, "a label much wider than the rect", buffer.PlainStyle)
		buf := buffer.NewBuffer(w, 1)
		txt.Draw(buf) // warm the truncation cache

		if got := testing.AllocsPerRun(200, func() { txt.Draw(buf) }); got != 0 {
			t.Errorf("width %d: Draw allocated %.1f objects per run, want 0", w, got)
		}
	}
}

// TestTextDrawIsAllocationFreeWithShortContent is the same guard when nothing is
// truncated, where there is no cache to warm and so nothing that could allocate.
func TestTextDrawIsAllocationFreeWithShortContent(t *testing.T) {
	txt := NewTextString(buffer.Rect{W: 20, H: 1}, "short", buffer.PlainStyle)
	buf := buffer.NewBuffer(20, 1)
	txt.Draw(buf)
	if got := testing.AllocsPerRun(200, func() { txt.Draw(buf) }); got != 0 {
		t.Errorf("Draw allocated %.1f objects per run, want 0", got)
	}
}

// TestTextSetSpansCopiesTheCallersSlice: a caller that reuses a buffer for its
// spans must not be able to change what a drawn frame means after the fact.
func TestTextSetSpansCopiesTheCallersSlice(t *testing.T) {
	spans := []buffer.Span{buffer.NewSpan("abc", buffer.PlainStyle)}
	txt := NewText(buffer.Rect{W: 10, H: 1}, spans)
	spans[0].Text = "zzz"

	buf := buffer.NewBuffer(10, 1)
	txt.Draw(buf)
	if got := rowOf(buf, 0, 10); got != "abc" {
		t.Errorf("got %q, want %q: the widget aliased the caller's slice", got, "abc")
	}
}

// TestTextEmptyContentIsNotAnError: an empty span list is a valid label.
func TestTextEmptyContentIsNotAnError(t *testing.T) {
	txt := NewText(buffer.Rect{W: 6, H: 1}, nil)
	if got := screen(t, 6, 1, txt); got[0] != "" {
		t.Errorf("row 0 = %q, want empty", got[0])
	}
	txt.Draw(buffer.NewBuffer(6, 1)) // must not panic
}

// TestTextConsumesNoEvents: a label is not focusable, and a label that swallowed a
// keypress would make every composed form unusable.
func TestTextConsumesNoEvents(t *testing.T) {
	txt := NewTextString(buffer.Rect{W: 6, H: 1}, "label", buffer.PlainStyle)
	if txt.Handle(termmosaic.Event{Kind: termmosaic.EventKey, Rune: 'q'}) {
		t.Error("Text consumed a key event")
	}
	if txt.Handle(termmosaic.Event{Kind: termmosaic.EventMouse}) {
		t.Error("Text consumed a mouse event")
	}
	txt.Invalidate()
}

// ---------------------------------------------------------------------------
// Paragraph
// ---------------------------------------------------------------------------

// TestParagraphWrapsToTheRectWidth is the ordinary case: the words wrap at the
// widget's width and the space that caused each break is consumed.
func TestParagraphWrapsToTheRectWidth(t *testing.T) {
	p := NewParagraphString(buffer.Rect{W: 10, H: 4}, "one two three four", buffer.PlainStyle)
	wantRows(t, screen(t, 10, 4, p),
		"one two",
		"three four",
		"",
		"",
	)
}

// TestParagraphUsesItsOwnBoundsNotTheBuffer is rule 1 again, and it is where a
// paragraph is most likely to get it wrong: wrapping to the screen's width would
// put one long line in a narrow column.
func TestParagraphUsesItsOwnBoundsNotTheBuffer(t *testing.T) {
	p := NewParagraphString(buffer.Rect{X: 1, W: 8, H: 3}, "aaa bbb ccc", buffer.PlainStyle)
	wantRows(t, screen(t, 40, 3, p),
		" aaa bbb",
		" ccc",
		"",
	)
}

// TestParagraphShowsOnlyWhatFits is the information-budget case: the rest does not
// fit, so it is not drawn, and the rows that are drawn are not blanked.
func TestParagraphShowsOnlyWhatFits(t *testing.T) {
	p := NewParagraphString(buffer.Rect{W: 6, H: 2}, "one two three four", buffer.PlainStyle)
	wantRows(t, screen(t, 6, 2, p), "one", "two")
}

// TestParagraphRewrapsOnResize covers BOTH directions, because only one of them
// catches a stale cache:
//
//	grow:   wrapped at 7 and shown at 11, it must wrap at 11, or the lines
//	        overflow into the next column.
//	shrink: wrapped at 11 and shown at 7, it must wrap at 7, or the lines are cut
//	        at the column boundary and read as truncated text.
func TestParagraphRewrapsOnResize(t *testing.T) {
	buf := buffer.NewBuffer(20, 4)
	p := NewParagraphString(buffer.Rect{W: 11, H: 4}, "aaa bbb ccc ddd", buffer.PlainStyle)
	p.Draw(buf)
	if got := rowOf(buf, 0, 20); got != "aaa bbb ccc" {
		t.Fatalf("setup at width 11: row 0 = %q, want %q", got, "aaa bbb ccc")
	}
	if got := rowOf(buf, 1, 20); got != "ddd" {
		t.Fatalf("setup at width 11: row 1 = %q, want %q", got, "ddd")
	}

	// The assertions read only the cells inside the paragraph's CURRENT bounds.
	// The ones past it are not the widget's to paint or to clear: repainting them
	// would be a widget writing outside Bounds, and whatever component owns that
	// part of the screen (a block, a pane) is what repaints it.
	p.SetBounds(buffer.Rect{W: 7, H: 4})
	p.Draw(buf)
	if got := rowOf(buf, 0, 7); got != "aaa bbb" {
		t.Errorf("shrunk to 7: row 0 = %q, want %q: the cache was not keyed on the rect", got, "aaa bbb")
	}
	if got := rowOf(buf, 1, 7); got != "ccc ddd" {
		t.Errorf("shrunk to 7: row 1 = %q, want %q", got, "ccc ddd")
	}

	p.SetBounds(buffer.Rect{W: 11, H: 4})
	p.Draw(buf)
	if got := rowOf(buf, 0, 11); got != "aaa bbb ccc" {
		t.Errorf("grown back to 11: row 0 = %q, want %q", got, "aaa bbb ccc")
	}
	if got := rowOf(buf, 1, 11); got != "ddd" {
		t.Errorf("grown back to 11: row 1 = %q, want %q", got, "ddd")
	}
}

// TestParagraphRewrapsWhenTheTextChangesAtTheSameSize: the cache is keyed on the
// rect AND invalidated by SetSpans, so new text at an unchanged size is not
// rendered from the old wrap.
func TestParagraphRewrapsWhenTheTextChangesAtTheSameSize(t *testing.T) {
	p := NewParagraphString(buffer.Rect{W: 10, H: 3}, "one two", buffer.PlainStyle)
	buf := buffer.NewBuffer(10, 3)
	p.Draw(buf)
	if got := rowOf(buf, 0, 10); got != "one two" {
		t.Fatalf("setup: row 0 = %q", got)
	}

	p.SetText("aaa bbb ccc", buffer.PlainStyle)
	p.Draw(buf)
	if got := rowOf(buf, 0, 10); got != "aaa bbb" {
		t.Errorf("row 0 = %q, want %q after SetText at an unchanged size", got, "aaa bbb")
	}
}

// TestParagraphAlignmentIsPerLine checks that alignment moves each line without
// changing where the lines break. Width 11 puts the content on two lines, 7 and 10
// cells wide, which is enough to tell all three alignments apart.
func TestParagraphAlignmentIsPerLine(t *testing.T) {
	for _, tc := range []struct {
		name  string
		align geometry.Align
		first string
		other string
	}{
		// Width 11 puts the content on two lines, 7 and 10 cells wide, so the three
		// alignments give three different offsets for the first line and two of
		// them move the second.
		{"left", geometry.AlignLeft, "one two", "three four"},
		{"center", geometry.AlignCenter, "  one two", "three four"},
		{"right", geometry.AlignRight, "    one two", " three four"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := NewParagraphString(buffer.Rect{W: 11, H: 2}, "one two three four", buffer.PlainStyle)
			p.SetAlign(tc.align)
			got := screen(t, 11, 2, p)
			if got[0] != tc.first {
				t.Errorf("row 0\n got %q\nwant %q", got[0], tc.first)
			}
			if got[1] != tc.other {
				t.Errorf("row 1\n got %q\nwant %q", got[1], tc.other)
			}
		})
	}
}

// TestParagraphCentreLeavesTheOddCellOnTheRight pins the rounding rule: centring a
// 10-cell line in 11 puts the single leftover cell after the text. It is geometry's
// rule rather than this package's, and it is pinned here because a title and a
// paragraph that rounded differently would be visibly misaligned by a column in a
// composed screen.
func TestParagraphCentreLeavesTheOddCellOnTheRight(t *testing.T) {
	p := NewParagraphString(buffer.Rect{W: 11, H: 1}, "three four", buffer.PlainStyle)
	p.SetAlign(geometry.AlignCenter)
	if got := screen(t, 11, 1, p)[0]; got != "three four" {
		t.Errorf("centred: got %q, want %q: the leftover cell goes on the right", got, "three four")
	}
	p.SetAlign(geometry.AlignRight)
	if got := screen(t, 11, 1, p)[0]; got != " three four" {
		t.Errorf("right aligned: got %q, want %q", got, " three four")
	}
}

// TestParagraphBreaksInsideAWordWithoutAMarker is Wrap's rule reaching the screen:
// a word longer than the width is cut at the cell boundary with nothing added,
// because Truncate owns the marker.
func TestParagraphBreaksInsideAWordWithoutAMarker(t *testing.T) {
	p := NewParagraphString(buffer.Rect{W: 4, H: 3}, "abcdefgh", buffer.PlainStyle)
	wantRows(t, screen(t, 4, 3, p), "abcd", "efgh", "")
}

// TestParagraphNeverSplitsAWideGlyph checks the rule at both extremes: in a
// two-cell line a double-width rune still fits and takes both cells, and the
// narrow runes around it wrap one per line. A one-cell area would drop it entirely
// rather than half-write it, which is what TestParagraphDrawIsTotalAtEverySize
// walks without asserting.
func TestParagraphNeverSplitsAWideGlyph(t *testing.T) {
	p := NewParagraphString(buffer.Rect{W: 2, H: 4}, "a漢b", buffer.PlainStyle)
	wantRows(t, screen(t, 2, 4, p), "a", "漢", "b", "")

	sink := widgettest.Render(t, 2, 4, 1, p)
	if !sink.CellAt(0, 1).IsContinuation() && sink.CellAt(0, 1).Rune() == '漢' {
		if got := sink.CellAt(1, 1).IsContinuation(); !got {
			t.Error("a wide glyph was written without its continuation cell")
		}
	}
}

// TestParagraphRepaintsRowsItNoLongerUses is rule 3 with a shrinking wrap: the
// paragraph wrapped into two lines and now wraps into one, and the second row must
// not keep the old text.
func TestParagraphRepaintsRowsItNoLongerUses(t *testing.T) {
	p := NewParagraphString(buffer.Rect{W: 4, H: 3}, "abcdefgh", buffer.PlainStyle)
	buf := buffer.NewBuffer(8, 3)
	p.Draw(buf)
	if got := rowOf(buf, 0, 8); got != "abcd" {
		t.Fatalf("setup: row 0 = %q, want %q", got, "abcd")
	}
	if got := rowOf(buf, 1, 8); got != "efgh" {
		t.Fatalf("setup: row 1 = %q, want %q", got, "efgh")
	}

	p.SetBounds(buffer.Rect{W: 8, H: 3})
	p.Draw(buf)
	if got := rowOf(buf, 0, 8); got != "abcdefgh" {
		t.Errorf("row 0 = %q, want %q", got, "abcdefgh")
	}
	if got := rowOf(buf, 1, 8); got != "" {
		t.Errorf("row 1 = %q, want empty: the row the old wrap used must be repainted", got)
	}
}

// TestParagraphDrawIsTotalAtEverySize, including width 1 where a double-width glyph
// cannot fit at all.
func TestParagraphDrawIsTotalAtEverySize(t *testing.T) {
	for w := 0; w <= 8; w++ {
		for h := 0; h <= 4; h++ {
			for _, s := range []string{"", "a", "a long paragraph of words", "wide 漢字 glyph"} {
				p := NewParagraphString(buffer.Rect{W: w, H: h}, s, buffer.PlainStyle)
				widgettest.Render(t, atLeast1(w), atLeast1(h), 1, p)
			}
		}
	}
}

// TestParagraphDrawIsAllocationFreeInSteadyState is the §4 guard. The first Draw at
// a new size wraps, which allocates; every Draw after that must not, and that is
// what a frame loop actually does sixty times a second.
func TestParagraphDrawIsAllocationFreeInSteadyState(t *testing.T) {
	p := NewParagraphString(buffer.Rect{W: 20, H: 5}, "one two three four five six seven", buffer.PlainStyle)
	buf := buffer.NewBuffer(20, 5)
	p.Draw(buf) // wrap once

	if got := testing.AllocsPerRun(200, func() { p.Draw(buf) }); got != 0 {
		t.Errorf("Draw allocated %.1f objects per run, want 0. Wrap belongs in the size-change check "+
			"(ADR 0008 §4)", got)
	}
}

// TestParagraphCostsOneAllocatingDrawPerRectChange makes the price explicit rather
// than leaving it implied: exactly one Draw per rect change pays for the wrap.
func TestParagraphCostsOneAllocatingDrawPerRectChange(t *testing.T) {
	p := NewParagraphString(buffer.Rect{W: 20, H: 5}, "one two three four five six seven", buffer.PlainStyle)
	buf := buffer.NewBuffer(40, 5)
	p.Draw(buf)

	p.SetBounds(buffer.Rect{W: 30, H: 5})
	p.Draw(buf) // the one draw that wraps

	if got := testing.AllocsPerRun(100, func() { p.Draw(buf) }); got != 0 {
		t.Errorf("Draw allocated %.1f objects per run after one rewrap, want 0", got)
	}
}

// TestParagraphLinesReportsTheWrap is the accessor a layout would use to ask
// whether the content overflowed.
func TestParagraphLinesReportsTheWrap(t *testing.T) {
	p := NewParagraphString(buffer.Rect{W: 10, H: 4}, "one two three four", buffer.PlainStyle)
	if got := p.Lines(); got != 0 {
		t.Errorf("Lines before the first Draw = %d, want 0: the wrap depends on the width", got)
	}
	p.Draw(buffer.NewBuffer(10, 4))
	if got := p.Lines(); got != 2 {
		t.Errorf("Lines = %d, want 2", got)
	}
	if got := p.Wrapped().Width; got != 10 {
		t.Errorf("Wrapped().Width = %d, want 10", got)
	}
}

// TestParagraphConsumesNoEvents: scrolling a paragraph is Pager's job, so a
// Paragraph that scrolled itself would be a second scroll model.
func TestParagraphConsumesNoEvents(t *testing.T) {
	p := NewParagraphString(buffer.Rect{W: 10, H: 3}, "one two three", buffer.PlainStyle)
	if p.Handle(termmosaic.Event{Kind: termmosaic.EventKey, Key: termmosaic.KeyDown}) {
		t.Error("Paragraph consumed a scroll key")
	}
	p.Invalidate()
}

// rowOf renders the first n cells of row y of buf as a string with trailing spaces
// trimmed. Passing a width narrower than the buffer's is how a test says "only
// these cells are inside the widget's bounds".
func rowOf(buf *buffer.Buffer, y, n int) string {
	row := buf.Row(y)
	if n > len(row) {
		n = len(row)
	}
	var sb strings.Builder
	for _, c := range row[:n] {
		sb.WriteRune(c.Rune())
	}
	return strings.TrimRight(sb.String(), " ")
}

// atLeast1 clamps a degenerate dimension so a zero-sized screen, which has no cells
// to draw into, is not asked to prove anything.
func atLeast1(v int) int {
	if v < 1 {
		return 1
	}
	return v
}
