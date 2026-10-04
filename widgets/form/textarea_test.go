package form

import (
	"strings"
	"testing"

	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
)

// TestTextAreaWrapsAtItsOwnWidth is the wrapping half of the contract: the area
// uses buffer's wrap width, not the buffer's width, and it uses its own bounds.
func TestTextAreaWrapsAtItsOwnWidth(t *testing.T) {
	area := NewTextAreaString(buffer.Rect{X: 1, W: 6, H: 3}, "alpha beta gamma")
	got := screenRows(t, 20, 3, area)
	wantRow(t, got, 0, " alpha")
	wantRow(t, got, 1, " beta")
	wantRow(t, got, 2, " gamma")
}

// TestTextAreaDrawsOnlyWhatFits is the information-budget case: more lines than
// rows shows the top of the content and clips the rest, which is ADR 0007's
// "clip, never blank" for a text area.
func TestTextAreaDrawsOnlyWhatFits(t *testing.T) {
	area := NewTextAreaString(buffer.Rect{W: 5, H: 2}, "one two three four")
	got := screenRows(t, 10, 2, area)
	if n := len(got); n != 2 {
		t.Fatalf("rendered %d rows, want 2", n)
	}
	wantRow(t, got, 0, "one")
	wantRow(t, got, 1, "two")
}

// TestTextAreaEnterInsertsANewline covers the key a single-line field refuses.
func TestTextAreaEnterInsertsANewline(t *testing.T) {
	area := NewTextArea(buffer.Rect{W: 10, H: 3})
	area.SetFocused(true)
	types(t, area, "ab")
	press(t, area, "\r")
	types(t, area, "cd")
	if got := area.Text(); got != "ab\ncd" {
		t.Errorf("after enter: text = %q, want %q", got, "ab\ncd")
	}
	got := screenRows(t, 10, 3, area)
	wantRow(t, got, 0, "ab")
	wantRow(t, got, 1, "cd")
}

// TestTextAreaMovesByVisualLineNotByNewline is the property that makes wrapping
// usable: Up and Down follow the WRAPPED lines, so from the second visual line of
// a long single-line paragraph the caret lands on the same column one line up,
// rather than jumping to the start of the text.
func TestTextAreaMovesByVisualLineNotByNewline(t *testing.T) {
	area := NewTextAreaString(buffer.Rect{W: 5, H: 4}, "alpha beta gamma")
	area.SetFocused(true)
	area.Draw(buffer.NewBuffer(10, 4)) // build the wrap cache

	// Wrapped as "alpha", "beta", " gamma" at width 5. Put the caret four cells
	// into the last line and step up.
	area.SetCursor(15)
	if got := area.Cursor(); got != 15 {
		t.Fatalf("setup: cursor = %d, want 15", got)
	}
	press(t, area, "\x1b[A") // up
	// The column is preserved: four cells into the line above, which is four
	// cells long, is its end at rune 10 — not the start of the text, which is
	// what a newline-based implementation would give.
	if got := area.Cursor(); got != 10 {
		t.Errorf("after up: cursor = %d, want 10 (the same column on the line above)", got)
	}
	press(t, area, "\x1b[A") // up again, onto the first line
	if got := area.Cursor(); got != 4 {
		t.Errorf("after a second up: cursor = %d, want 4", got)
	}
	press(t, area, "\x1b[A") // up past the first line
	if got := area.Cursor(); got != 0 {
		t.Errorf("up at the first line: cursor = %d, want 0", got)
	}
	press(t, area, "\x1b[A")
	if got := area.Cursor(); got != 0 {
		t.Errorf("up at the first line again: cursor = %d, want 0", got)
	}
}

// TestTextAreaHomeAndEndArePerVisualLine distinguishes the two pairs: Home and
// End work on the visual line, Ctrl-Home and Ctrl-End on the whole text.
func TestTextAreaHomeAndEndArePerVisualLine(t *testing.T) {
	area := NewTextAreaString(buffer.Rect{W: 5, H: 4}, "alpha beta gamma")
	area.SetFocused(true)
	area.Draw(buffer.NewBuffer(10, 4))
	area.SetCursor(13) // inside " gamma", the third line

	press(t, area, "\x1b[F") // end
	if got := area.Cursor(); got != 16 {
		t.Errorf("after end: cursor = %d, want 16 (the end of the line)", got)
	}
	press(t, area, "\x1b[H") // home
	if got := area.Cursor(); got != 11 {
		t.Errorf("after home: cursor = %d, want 11 (the start of the visual line)", got)
	}
	press(t, area, "\x1b[1;5F") // ctrl-end
	if got := area.Cursor(); got != 16 {
		t.Errorf("after ctrl-end: cursor = %d, want 16", got)
	}
	press(t, area, "\x1b[1;5H") // ctrl-home
	if got := area.Cursor(); got != 0 {
		t.Errorf("after ctrl-home: cursor = %d, want 0", got)
	}
}

// TestTextAreaScrollsToFollowTheCaret is the vertical budget: moving the caret
// below the viewport scrolls the minimum amount, and moving back up scrolls it
// back, so the caret is always visible without the view jumping.
func TestTextAreaScrollsToFollowTheCaret(t *testing.T) {
	area := NewTextAreaString(buffer.Rect{W: 5, H: 2}, "one two three four five")
	area.SetFocused(true)
	buf := buffer.NewBuffer(10, 4)
	area.Draw(buf)
	if got := area.ScrollOffset(); got != 0 {
		t.Fatalf("setup: scroll offset = %d, want 0", got)
	}
	top := rowOf(buf, 0, 5)

	area.SetCursor(20) // the end of the text, several lines down
	area.Draw(buf)
	scrolled := rowOf(buf, 0, 5)
	assertDifferent(t, "the first visible line before and after scrolling", top, scrolled)
	if got := area.ScrollOffset(); got == 0 {
		t.Errorf("scroll offset is still 0 with the caret off screen")
	}
	if !strings.Contains(rowOf(buf, 0, 5)+rowOf(buf, 1, 5), "five") {
		t.Errorf("after scrolling the two visible lines are %q and %q, want the caret's line among them",
			rowOf(buf, 0, 5), rowOf(buf, 1, 5))
	}

	area.SetCursor(0)
	area.Draw(buf)
	if got := area.ScrollOffset(); got != 0 {
		t.Errorf("after moving the caret back to the top: scroll offset = %d, want 0", got)
	}
	if got := rowOf(buf, 0, 5); got != top {
		t.Errorf("the first visible line is %q after scrolling back, want %q", got, top)
	}
}

// TestTextAreaPasteKeepsNewlinesAndIsOneOperation is the two halves of the paste
// contract for a multi-line field: the newlines survive, and the whole paste is
// one undo step.
func TestTextAreaPasteKeepsNewlinesAndIsOneOperation(t *testing.T) {
	area := NewTextArea(buffer.Rect{W: 10, H: 4})
	area.SetFocused(true)
	payload := "one\ntwo\nthree"
	paste(t, area, payload)

	if got := area.Text(); got != payload {
		t.Errorf("after pasting: text = %q, want %q: an area keeps the newlines verbatim", got, payload)
	}
	if depth := area.UndoDepth(); depth != 1 {
		t.Errorf("undo depth = %d after a three-line paste, want 1", depth)
	}
	if !area.Undo() {
		t.Fatalf("Undo reported nothing to undo after a paste")
	}
	if got := area.Text(); got != "" {
		t.Errorf("after one undo: text = %q, want empty", got)
	}
}

// TestTextAreaClickPlacesTheCaretOnTheVisualLine covers the mouse path, including
// a click in the second row landing on the second wrapped line.
func TestTextAreaClickPlacesTheCaretOnTheVisualLine(t *testing.T) {
	area := NewTextAreaString(buffer.Rect{X: 2, W: 6, H: 3}, "alpha beta gamma")
	area.Draw(buffer.NewBuffer(12, 3))

	if !clickAt(area, 3, 1) { // cell 1 of the second row
		t.Fatalf("a click inside the area was not consumed")
	}
	if got := area.Cursor(); got != 7 {
		t.Errorf("after clicking cell 1 of row 1: cursor = %d, want 7", got)
	}
	if !area.Focused() {
		t.Errorf("a click inside the area did not take focus")
	}
}

// TestTextAreaClickOutsideBoundsChangesNothing is the negative mouse case.
func TestTextAreaClickOutsideBoundsChangesNothing(t *testing.T) {
	area := NewTextAreaString(buffer.Rect{X: 1, Y: 1, W: 6, H: 2}, "alpha beta")
	area.SetFocused(true)
	area.SetCursor(3)
	for _, at := range [][2]int{{0, 1}, {7, 1}, {2, 3}, {2, 0}} {
		if clickAt(area, at[0], at[1]) {
			t.Errorf("a click at %v was consumed, want it ignored: it is outside Bounds", at)
		}
	}
	if got := area.Cursor(); got != 3 {
		t.Errorf("an outside click moved the cursor to %d, want it unchanged at 3", got)
	}
}

// TestTextAreaTabIsNotConsumed is what makes a form of areas navigable: tab moves
// between fields, so an area must not swallow it.
func TestTextAreaTabIsNotConsumed(t *testing.T) {
	area := NewTextArea(buffer.Rect{W: 10, H: 3})
	area.SetFocused(true)
	if area.Handle(decodeKey(t, "\t")) {
		t.Errorf("the area consumed tab; in a form, tab moves between fields")
	}
}

// TestTextAreaDrawDoesNotAllocate is the ADR 0008 §4 rule for the one widget in
// this package that calls buffer.Wrap.
func TestTextAreaDrawDoesNotAllocate(t *testing.T) {
	area := NewTextAreaString(buffer.Rect{W: 12, H: 4}, "some text that wraps over a few lines here")
	area.SetFocused(true)
	buf := buffer.NewBuffer(12, 4)
	area.Draw(buf) // warm the wrap cache
	if got := testing.AllocsPerRun(200, func() { area.Draw(buf) }); got != 0 {
		t.Errorf("Draw allocated %v times per run after the wrap cache was warm, want 0", got)
	}
}

// TestTextAreaReWrapsOnAResizeBothWays is the resize contract in both directions:
// narrowing must re-wrap (which changes the lines) and widening must re-wrap back.
func TestTextAreaReWrapsOnAResizeBothWays(t *testing.T) {
	area := NewTextAreaString(buffer.Rect{W: 20, H: 3}, "alpha beta gamma")
	buf := buffer.NewBuffer(20, 4)
	area.Draw(buf)
	wide := area.LineCount()
	if wide != 1 {
		t.Fatalf("setup: %d lines at width 20, want 1", wide)
	}

	area.SetBounds(buffer.Rect{W: 6, H: 3})
	area.Draw(buf)
	narrow := area.LineCount()
	if narrow <= wide {
		t.Errorf("at width 6 there are %d lines, want more than the %d at width 20: the wrap must be recomputed", narrow, wide)
	}
	wantRow(t, []string{rowOf(buf, 0, 6)}, 0, "alpha")

	area.SetBounds(buffer.Rect{W: 20, H: 3})
	area.Draw(buf)
	if got := area.LineCount(); got != wide {
		t.Errorf("back at width 20 there are %d lines, want the original %d", got, wide)
	}
}

// TestTextAreaRepaintsRowsItsContentNoLongerUses covers ADR 0007 §1 rule 3 at
// the level it actually bites: the rect has NOT changed, but the content shrank
// from three lines to one, so the two rows the area no longer draws must still be
// repainted blank. A widget that only fills the rows it draws leaves the old text
// on screen forever, because the renderer diffs and never clears.
func TestTextAreaRepaintsRowsItsContentNoLongerUses(t *testing.T) {
	area := NewTextAreaString(buffer.Rect{X: 0, Y: 0, W: 6, H: 3}, "one two three")
	buf := buffer.NewBuffer(6, 3)
	area.Draw(buf)
	if strings.TrimSpace(rowOf(buf, 2, 6)) == "" {
		t.Fatalf("setup: row 2 is blank, want it to hold wrapped content")
	}

	area.SetText("one")
	area.Draw(buf)
	for _, y := range []int{1, 2} {
		if got := rowOf(buf, y, 6); got != "      " {
			t.Errorf("row %d after the content shrank to one line is %q, want six spaces: "+
				"the area must repaint the whole of Bounds", y, got)
		}
	}
	wantRow(t, []string{rowOf(buf, 0, 6)}, 0, "one")
}

// TestTextAreaDegenerateSizesDrawWithoutPanicking is ADR 0007 §4 at every size a
// terminal can produce, including below MinSize and with no wrap possible at all.
func TestTextAreaDegenerateSizesDrawWithoutPanicking(t *testing.T) {
	for _, r := range []buffer.Rect{
		{W: 0, H: 0}, {X: 1, Y: 2, W: 3, H: 0}, {W: 1, H: 1}, {W: 2, H: 1}, {W: 1, H: 4},
	} {
		area := NewTextAreaString(r, "alpha beta gamma")
		area.SetFocused(true)
		buf := buffer.NewBuffer(8, 6)
		area.Draw(buf)                            // must not panic
		area.Handle(termmosaic.ResizeEvent(1, 1)) // a resize event is not a key
	}
}

// TestTextAreaMinSizeIsIndependentOfState pins the purity of MinSize for the area
// as well as the field.
func TestTextAreaMinSizeIsIndependentOfState(t *testing.T) {
	area := NewTextArea(buffer.Rect{W: 20, H: 5})
	empty := area.MinSize()
	area.SetText("a lot of text\nacross\nmany\nlines")
	if got := area.MinSize(); got != empty {
		t.Errorf("MinSize changed with content: %+v then %+v", empty, got)
	}
	if empty.W < 1 || empty.H < 1 {
		t.Errorf("MinSize = %+v, want at least 1x1", empty)
	}
}
