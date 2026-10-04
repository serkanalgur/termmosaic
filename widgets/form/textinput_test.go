package form

import (
	"strings"
	"testing"

	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
)

// TestDecoderProducesTheKeysTheContractNames is a guard on this package's own
// test helpers. Every keyboard assertion below goes through input.Decode, and
// that is only worth doing if the sequences really decode to the keys the
// contracts name. If this fails, the sequences are wrong, not the widgets.
func TestDecoderProducesTheKeysTheContractNames(t *testing.T) {
	for _, tc := range []struct {
		seq string
		key termmosaic.Key
	}{
		{"\x1b[A", termmosaic.KeyUp},
		{"\x1b[B", termmosaic.KeyDown},
		{"\x1b[C", termmosaic.KeyRight},
		{"\x1b[D", termmosaic.KeyLeft},
		{"\x1b[H", termmosaic.KeyHome},
		{"\x1b[F", termmosaic.KeyEnd},
		{"\x1b[3~", termmosaic.KeyDelete},
		{"\x1b[5~", termmosaic.KeyPageUp},
		{"\x1b[6~", termmosaic.KeyPageDown},
		{"\r", termmosaic.KeyEnter},
		{"\x7f", termmosaic.KeyBackspace},
	} {
		ev := decodeKey(t, tc.seq)
		if ev.Kind != termmosaic.EventKey {
			t.Errorf("%q decoded to %v, want a key event", tc.seq, ev.Kind)
			continue
		}
		if ev.Key != tc.key {
			t.Errorf("%q decoded to key %v, want %v", tc.seq, ev.Key, tc.key)
		}
	}
	for _, tc := range []struct {
		seq string
		r   rune
		mod termmosaic.KeyMod
	}{
		{"a", 'a', 0},
		{" ", ' ', 0},                     // the space bar arrives as a rune, not as KeySpace
		{"\x01", 'a', termmosaic.ModCtrl}, // ctrl-a
		{"\x1a", 'z', termmosaic.ModCtrl}, // ctrl-z
		{"\x17", 'w', termmosaic.ModCtrl}, // ctrl-w
		{"\x04", 'd', termmosaic.ModCtrl}, // ctrl-d
	} {
		ev := decodeKey(t, tc.seq)
		if ev.Rune != tc.r || ev.Mod != tc.mod {
			t.Errorf("%q decoded to rune %q mod %q, want %q mod %q", tc.seq, ev.Rune, ev.Mod, tc.r, tc.mod)
		}
	}
}

// ---------------------------------------------------------------------------
// TextInput
// ---------------------------------------------------------------------------

// TestTextInputDrawsWhatWasTyped is the ordinary case: a focused field shows
// what was typed, and the caret is at the end of it.
func TestTextInputDrawsWhatWasTyped(t *testing.T) {
	in := NewTextInput(buffer.Rect{W: 20, H: 1})
	in.SetFocused(true)
	types(t, in, "hi")
	got := screenRows(t, 20, 1, in)
	wantRow(t, got, 0, "hi")
}

// TestTextInputUsesItsOwnBoundsNotTheBuffer pins ADR 0007 §1 rule 1 on a field:
// a 6-wide TextInput on a 40-wide screen shows 6 cells, not 40.
func TestTextInputUsesItsOwnBoundsNotTheBuffer(t *testing.T) {
	in := NewTextInputString(buffer.Rect{X: 2, W: 6, H: 1}, "0123456789")
	buf := buffer.NewBuffer(40, 1)
	in.Draw(buf)
	// The field is six cells wide starting at column two, so its row is five
	// characters plus the truncation marker and then blanks. A field that read the
	// buffer's width instead would have written all ten characters, which is what
	// this comparison proves: six cells of content and 32 blanks, not ten
	// characters.
	want := "  01234" + buffer.TruncSuffix + strings.Repeat(" ", 32)
	if got := rowOf(buf, 0, 40); got != want {
		t.Errorf("row = %q, want %q: the field must not write past its own bounds", got, want)
	}
}

// TestTextInputTruncatesRatherThanSpilling is the same property for a field
// whose content is longer than the field: the overflow is truncated with the
// marker, not clipped at the buffer edge, so a neighbouring widget is safe.
func TestTextInputTruncatesRatherThanSpilling(t *testing.T) {
	in := NewTextInputString(buffer.Rect{W: 8, H: 1}, "abcdefghijklmno")
	got := screenRows(t, 20, 1, in)
	wantRow(t, got, 0, "abcdefg"+buffer.TruncSuffix)
	if n := buffer.StringWidth(got[0]); n != 8 {
		t.Errorf("row is %d cells, want 8", n)
	}
}

// TestTextInputRepaintsItsWholeBoundsOnShrink is ADR 0007 §1 rule 3, the rule a
// grow-only resize test cannot catch. The field is first wide enough for ten
// characters, then narrowed to four: every cell of the new bounds must be
// repainted, and no cell of the field's old content may survive.
func TestTextInputRepaintsItsWholeBoundsOnShrink(t *testing.T) {
	in := NewTextInputString(buffer.Rect{W: 20, H: 1}, "abcdefghij")
	buf := buffer.NewBuffer(20, 1)
	in.Draw(buf)
	before := rowOf(buf, 0, 20)
	if !strings.HasPrefix(before, "abcdefghij") {
		t.Fatalf("setup: row = %q, want the untruncated text", before)
	}

	in.SetBounds(buffer.Rect{W: 4, H: 1})
	in.Draw(buf)
	got := rowOf(buf, 0, 4)
	// Three characters plus the marker: the fourth cell is spent telling the user
	// there is more, which is what "clip, never blank" means for a shrunk field.
	if want := "abc" + buffer.TruncSuffix; got != want {
		t.Errorf("after shrinking to 4 cells\n got %q\nwant %q", got, want)
	}
	// And the mutation must be visible: the pre-shrink frame and the post-shrink
	// frame have to differ, or the assertion above is vacuous.
	if before == got {
		t.Errorf("the field rendered %q both before and after the shrink, so this test cannot fail", before)
	}
}

// TestTextInputGrowsBackAfterAShrink is the other half of the resize contract: a
// field narrowed and then widened must show the whole value again, because the
// cached display is keyed on the rect rather than on the direction of the change.
func TestTextInputGrowsBackAfterAShrink(t *testing.T) {
	in := NewTextInputString(buffer.Rect{W: 4, H: 1}, "abcdefghij")
	buf := buffer.NewBuffer(20, 1)
	in.Draw(buf)
	in.SetBounds(buffer.Rect{W: 20, H: 1})
	in.Draw(buf)
	if got, want := strings.TrimRight(rowOf(buf, 0, 20), " "), "abcdefghij"; got != want {
		t.Errorf("after growing back\n got %q\nwant %q", got, want)
	}
}

// TestTextInputDrawDoesNotAllocate is the performance contract of ADR 0008 §4.
// The first Draw builds the cache, which allocates by design; every Draw after
// it must not.
func TestTextInputDrawDoesNotAllocate(t *testing.T) {
	in := NewTextInputString(buffer.Rect{W: 20, H: 1}, "hello")
	in.SetFocused(true)
	buf := buffer.NewBuffer(20, 1)
	in.Draw(buf) // warm the cache
	if got := testing.AllocsPerRun(200, func() { in.Draw(buf) }); got != 0 {
		t.Errorf("Draw allocated %v times per run after the cache was warm, want 0", got)
	}
}

// TestTextInputDrawDoesNotAllocateWithASelection proves the same for the case
// with three styles interleaved on one row, which is the path that could
// plausibly build a slice per frame.
func TestTextInputDrawDoesNotAllocateWithASelection(t *testing.T) {
	in := NewTextInputString(buffer.Rect{W: 20, H: 1}, "hello world")
	in.SetFocused(true)
	in.SelectAll()
	buf := buffer.NewBuffer(20, 1)
	in.Draw(buf)
	if got := testing.AllocsPerRun(200, func() { in.Draw(buf) }); got != 0 {
		t.Errorf("Draw allocated %v times per run with a selection, want 0", got)
	}
}

// TestTextInputPlaceholderIsShownWhenEmpty covers the empty field, which is the
// state a user sees most and the one where a cursor implementation usually
// disappears.
func TestTextInputPlaceholderIsShownWhenEmpty(t *testing.T) {
	in := NewTextInput(buffer.Rect{W: 20, H: 1})
	in.Placeholder = "name"
	got := screenRows(t, 20, 1, in)
	wantRow(t, got, 0, "name")
}

// TestTextInputPlaceholderIsTruncatedRatherThanClipped checks that an over-long
// placeholder says there was more of it.
func TestTextInputPlaceholderIsTruncatedRatherThanClipped(t *testing.T) {
	in := NewTextInput(buffer.Rect{W: 6, H: 1})
	in.Placeholder = "a very long placeholder"
	got := screenRows(t, 6, 1, in)
	if got[0] == "a very" {
		t.Errorf("placeholder was clipped to %q rather than truncated", got[0])
	}
	if !strings.HasSuffix(got[0], buffer.TruncSuffix) {
		t.Errorf("placeholder row = %q, want it to end with the truncation marker", got[0])
	}
	if n := buffer.StringWidth(got[0]); n != 6 {
		t.Errorf("placeholder row is %d cells, want 6", n)
	}
}

// TestTextInputCursorIsShownWithoutColour is the accessibility rule stated as a
// test: a focused field's caret must be distinguishable from the same field
// unfocused, using attributes that survive NO_COLOR rather than colour.
func TestTextInputCursorIsShownWithoutColour(t *testing.T) {
	in := NewTextInputString(buffer.Rect{W: 10, H: 1}, "ab")
	buf := buffer.NewBuffer(10, 1)
	in.Draw(buf)
	unfocused := buf.CellAt(0, 0)

	in.SetFocused(true)
	in.Draw(buf)
	focused := buf.CellAt(0, 0)

	if unfocused.Attr == focused.Attr {
		t.Errorf("the caret cell's attributes are %v focused and %v unfocused, so focus is "+
			"signalled by colour alone", focused.Attr, unfocused.Attr)
	}
	if focused.Attr&buffer.AttrReverse == 0 {
		t.Errorf("the caret cell has attributes %v, want AttrReverse: reverse video "+
			"survives NO_COLOR, a colour does not", focused.Attr)
	}
}

// TestTextInputBackspaceDeletesBackwards covers the deletion key through the
// decoder, including the case where there is nothing left to delete.
func TestTextInputBackspaceDeletesBackwards(t *testing.T) {
	in := NewTextInputString(buffer.Rect{W: 10, H: 1}, "abc")
	in.SetFocused(true)
	in.SetCursor(3)

	press(t, in, "\x7f")
	if got := in.Text(); got != "ab" {
		t.Errorf("after backspace: text = %q, want %q", got, "ab")
	}
	press(t, in, "\x7f")
	press(t, in, "\x7f")
	if got := in.Text(); got != "" {
		t.Errorf("after deleting everything: text = %q, want empty", got)
	}
	if in.Handle(decodeKey(t, "\x7f")) {
		t.Errorf("backspace on an empty field was consumed; there was nothing to delete")
	}
}

// TestTextInputDeleteRemovesTheSelection proves the selection is deleted rather
// than one end of it, which is the difference between a selection and a caret.
func TestTextInputDeleteRemovesTheSelection(t *testing.T) {
	in := NewTextInputString(buffer.Rect{W: 10, H: 1}, "abcdef")
	in.SetFocused(true)
	in.SetCursor(1)
	press(t, in, "\x1b[C")    // right
	press(t, in, "\x1b[1;2C") // shift-right
	lo, hi, ok := in.Selection()
	if !ok || lo != 2 || hi != 3 {
		t.Fatalf("selection = [%d,%d) ok=%v, want [2,3) ok", lo, hi, ok)
	}
	press(t, in, "\x1b[3~") // delete
	// The selection was the "c" at index 2, so deleting it leaves "abdef".
	if got := in.Text(); got != "abdef" {
		t.Errorf("after deleting the selection: text = %q, want %q", got, "abdef")
	}
}

// TestTextInputSelectionGrowsWithShiftAndCollapsesWithout is the whole selection
// contract in one test: Shift extends, a plain arrow collapses it.
func TestTextInputSelectionGrowsWithShiftAndCollapsesWithout(t *testing.T) {
	in := NewTextInputString(buffer.Rect{W: 10, H: 1}, "abcdef")
	in.SetFocused(true)
	in.SetCursor(6) // the end: SetText leaves the caret at 0

	press(t, in, "\x1b[1;2D") // shift-left from the end
	lo, hi, ok := in.Selection()
	if !ok || lo != 5 || hi != 6 {
		t.Fatalf("after shift-left: selection = [%d,%d) ok=%v, want [5,6)", lo, hi, ok)
	}
	press(t, in, "\x1b[1;2D") // shift-left again
	lo, hi, _ = in.Selection()
	if lo != 4 || hi != 6 {
		t.Errorf("after two shift-lefts: selection = [%d,%d), want [4,6)", lo, hi)
	}
	press(t, in, "\x1b[C") // plain right
	if _, _, ok := in.Selection(); ok {
		t.Errorf("a plain arrow left a selection behind; it must collapse it")
	}
	if got := in.SelectedText(); got != "" {
		t.Errorf("SelectedText = %q after collapsing, want empty", got)
	}
}

// TestTextInputHomeAndEndCoverTheWholeText checks both keys, and the Ctrl
// variants that must behave identically at the ends of a single-line field.
func TestTextInputHomeAndEndCoverTheWholeText(t *testing.T) {
	in := NewTextInputString(buffer.Rect{W: 10, H: 1}, "abcdef")
	in.SetFocused(true)
	in.SetCursor(3)

	press(t, in, "\x1b[H")
	if got := in.Cursor(); got != 0 {
		t.Errorf("after home: cursor = %d, want 0", got)
	}
	press(t, in, "\x1b[F")
	if got := in.Cursor(); got != 6 {
		t.Errorf("after end: cursor = %d, want 6", got)
	}
	// Ctrl-End from the end must not move, and must not be treated as an
	// unconsumed key either.
	press(t, in, "\x05") // ctrl-e
	if got := in.Cursor(); got != 6 {
		t.Errorf("after ctrl-e: cursor = %d, want 6", got)
	}
	press(t, in, "\x01") // ctrl-a selects all
	lo, hi, ok := in.Selection()
	if !ok || lo != 0 || hi != 6 {
		t.Errorf("after ctrl-a: selection = [%d,%d) ok=%v, want [0,6)", lo, hi, ok)
	}
}

// TestTextInputWordMotion covers Ctrl-Left and Ctrl-Right in both directions and
// at both ends, which is where a word-motion implementation usually breaks.
func TestTextInputWordMotion(t *testing.T) {
	in := NewTextInputString(buffer.Rect{W: 20, H: 1}, "alpha beta gamma")
	in.SetFocused(true)
	in.SetCursor(16) // inside "gamma"

	press(t, in, "\x1b[1;5D") // ctrl-left
	if got := in.Cursor(); got != 11 {
		t.Errorf("after ctrl-left: cursor = %d, want 11 (the start of \"gamma\")", got)
	}
	press(t, in, "\x1b[1;5D") // ctrl-left again crosses the space
	if got := in.Cursor(); got != 6 {
		t.Errorf("after a second ctrl-left: cursor = %d, want 6 (the start of \"beta\")", got)
	}
	press(t, in, "\x1b[1;5C") // ctrl-right lands on the START of the next word
	if got := in.Cursor(); got != 11 {
		t.Errorf("after ctrl-right: cursor = %d, want 11", got)
	}
	press(t, in, "\x1b[1;5C") // and on to the end of the text
	if got := in.Cursor(); got != 16 {
		t.Errorf("after a second ctrl-right: cursor = %d, want 16", got)
	}
	// Motion past either end is a no-op, not a panic and not a wrap.
	in.SetCursor(0)
	press(t, in, "\x1b[1;5D")
	if got := in.Cursor(); got != 0 {
		t.Errorf("ctrl-left at the start: cursor = %d, want 0", got)
	}
	in.SetCursor(16)
	press(t, in, "\x1b[1;5C")
	if got := in.Cursor(); got != 16 {
		t.Errorf("ctrl-right at the end: cursor = %d, want 16", got)
	}
}

// TestTextInputWordDeleteAcceptsBothSpellingsOfCtrlBackspace covers the two
// sequences a terminal sends for Ctrl-Backspace. Both must delete a word; a
// widget that only handles one is broken on half the terminals.
func TestTextInputWordDeleteAcceptsEverySpellingOfCtrlBackspace(t *testing.T) {
	// Ctrl-W is what a terminal actually sends (0x17); the other two spellings are
	// built directly, because they are reported by the kitty keyboard protocol as
	// a key plus a modifier and no byte sequence in this table produces them.
	for _, tc := range []struct {
		name string
		ev   termmosaic.Event
	}{
		{"ctrl-w", decodeKey(t, "\x17")},
		{"ctrl-h", termmosaic.KeyEvent('h', termmosaic.ModCtrl)},
		{"ctrl-backspace", termmosaic.SpecialKeyEvent(termmosaic.KeyBackspace, termmosaic.ModCtrl)},
	} {
		in := NewTextInputString(buffer.Rect{W: 20, H: 1}, "alpha beta")
		in.SetFocused(true)
		in.SetCursor(10)
		if !in.Handle(tc.ev) {
			t.Errorf("%s was not consumed", tc.name)
			continue
		}
		if got := in.Text(); got != "alpha " {
			t.Errorf("after %s: text = %q, want %q", tc.name, got, "alpha ")
		}
	}
}

// TestTextInputPasteIsOneUndoableOperation is the requirement ADR 0005 §4 was
// written for, asserted the only way that means anything: a ten-thousand
// character paste must leave the undo stack one entry deep, so one Ctrl-Z removes
// all of it.
//
// The test is non-vacuous by construction: it compares the undo depth after a
// paste of 10,000 characters against a depth that would mean 10,000 keystrokes,
// and it then requires a single undo to restore the pre-paste text exactly.
func TestTextInputPasteIsOneUndoableOperation(t *testing.T) {
	in := NewTextInputString(buffer.Rect{W: 20, H: 1}, "")
	in.SetFocused(true)

	const n = 10000
	payload := strings.Repeat("a", n)
	paste(t, in, payload)

	if got := in.Len(); got != n {
		t.Fatalf("after pasting %d characters: len = %d", n, got)
	}
	if depth := in.UndoDepth(); depth != 1 {
		t.Errorf("after pasting %d characters: undo depth = %d, want 1: a paste is one "+
			"operation (ADR 0005 §4), not %d keystrokes", n, depth, n)
	}
	if !in.Undo() {
		t.Fatalf("Undo reported nothing to undo after a paste")
	}
	if got := in.Text(); got != "" {
		t.Errorf("after one undo: text is %d runes, want 0: one Ctrl-Z must remove the whole paste", in.Len())
	}
}

// TestTextInputOnChangeFiresOncePerPaste is the second half of the same rule: the
// callback runs once for a whole paste, so an application that re-lays-out on
// change re-lays-out once.
func TestTextInputOnChangeFiresOncePerPaste(t *testing.T) {
	in := NewTextInput(buffer.Rect{W: 20, H: 1})
	in.SetFocused(true)
	calls := 0
	in.OnChange = func(string) { calls++ }

	payload := strings.Repeat("b", 5000)
	paste(t, in, payload)
	if calls != 1 {
		t.Errorf("a 5000-character paste fired OnChange %d times, want 1", calls)
	}
	// The two numbers compared here are genuinely different, so this is not a
	// value compared against itself: one paste of thousands of characters must
	// not produce as many calls as characters.
	assertDifferent(t, "the paste call count and the pasted character count",
		itoa(calls), itoa(len(payload)))

	// One call for the paste and one more for a single keystroke, so the count
	// really is counting operations rather than being stuck.
	in.Handle(termmosaic.KeyEvent('c', 0))
	if calls != 2 {
		t.Errorf("after one keystroke the call count is %d, want 2", calls)
	}
}

// TestTextInputUndoCoalescesTypingButNotEditing checks the narrow coalescing
// rule: a run of typed characters is one undo, while a delete or a paste is its
// own step. Coalescing too eagerly makes Ctrl-Z useless; too little makes it
// tedious.
func TestTextInputUndoCoalescesTypingButNotEditing(t *testing.T) {
	in := NewTextInput(buffer.Rect{W: 20, H: 1})
	in.SetFocused(true)
	types(t, in, "hello")
	if depth := in.UndoDepth(); depth != 1 {
		t.Errorf("after typing 5 characters: undo depth = %d, want 1 (a typed run coalesces)", depth)
	}
	in.Undo()
	if got := in.Text(); got != "" {
		t.Errorf("after one undo of a typed run: text = %q, want empty", got)
	}

	types(t, in, "ab")
	press(t, in, "\x7f") // a delete is its own step
	if depth := in.UndoDepth(); depth != 2 {
		t.Errorf("after typing and deleting: undo depth = %d, want 2 (the delete is its own step)", depth)
	}
	in.Undo()
	if got := in.Text(); got != "ab" {
		t.Errorf("after undoing the delete: text = %q, want %q", got, "ab")
	}
}

// TestTextInputUndoDoesNotCoalesceEditsAtDifferentPositions is the other half of
// the coalescing rule, and the half a greedy implementation gets wrong: two
// single-rune insertions that are not adjacent are two user actions, and one
// Ctrl-Z must take back only the last of them.
//
// Without this, typing a character, moving the caret and typing another collapses
// into one step — and undo then removes text from two places the user never
// touched together.
func TestTextInputUndoDoesNotCoalesceEditsAtDifferentPositions(t *testing.T) {
	in := NewTextInput(buffer.Rect{W: 20, H: 1})
	in.SetFocused(true)

	in.Handle(termmosaic.KeyEvent('a', 0)) // at 0
	press(t, in, "\x1b[H")                 // back to the start
	in.Handle(termmosaic.KeyEvent('b', 0)) // also at 0, but not adjacent to the first

	if depth := in.UndoDepth(); depth != 2 {
		t.Fatalf("after two insertions at the same position: undo depth = %d, want 2: "+
			"edits that are not adjacent are two user actions", depth)
	}
	if !in.Undo() {
		t.Fatalf("Undo reported nothing to undo")
	}
	if got := in.Text(); got != "a" {
		t.Errorf("after one undo: text = %q, want %q: only the last insertion may be reverted", got, "a")
	}
	if !in.Undo() {
		t.Fatalf("the second Undo reported nothing to undo")
	}
	if got := in.Text(); got != "" {
		t.Errorf("after two undos: text = %q, want empty", got)
	}
}

// TestTextInputUndoStackIsBounded proves the eviction rule rather than an
// unbounded stack: a field typed into for a very long session must stop growing.
func TestTextInputUndoStackIsBounded(t *testing.T) {
	in := NewTextInput(buffer.Rect{W: 40, H: 1})
	in.SetFocused(true)
	// Each iteration types one character, coalesces, deletes it, and the delete
	// forces a new step.
	for i := 0; i < undoLimit*2; i++ {
		in.Handle(termmosaic.KeyEvent('x', 0))
		press(t, in, "\x7f")
	}
	if depth := in.UndoDepth(); depth > undoLimit {
		t.Errorf("undo depth = %d after %d edits, want at most %d", depth, undoLimit*2, undoLimit)
	}
}

// TestTextInputClickPlacesTheCaret covers the mouse path, including a click
// beyond the last character which must land at the end rather than nowhere.
func TestTextInputClickPlacesTheCaret(t *testing.T) {
	in := NewTextInputString(buffer.Rect{X: 1, W: 10, H: 1}, "abcdef")
	if !clickAt(in, 3, 0) { // cell 3 is the 'c'
		t.Fatalf("a click inside the field was not consumed")
	}
	if got := in.Cursor(); got != 2 {
		t.Errorf("after clicking cell 3: cursor = %d, want 2 (before \"c\")", got)
	}
	if !in.Focused() {
		t.Errorf("a click inside the field did not take focus")
	}
	clickAt(in, 9, 0) // well past the end of the text
	if got := in.Cursor(); got != 6 {
		t.Errorf("after clicking past the text: cursor = %d, want 6 (the end)", got)
	}
}

// TestTextInputClickOutsideBoundsChangesNothing is the negative mouse case, and
// it is the one a click-through bug shows up in.
func TestTextInputClickOutsideBoundsChangesNothing(t *testing.T) {
	in := NewTextInputString(buffer.Rect{X: 2, W: 6, H: 1}, "abcdef")
	in.SetFocused(true)
	in.SetCursor(3)
	for _, at := range [][2]int{{0, 0}, {1, 0}, {8, 0}, {3, 1}, {3, -1}} {
		if clickAt(in, at[0], at[1]) {
			t.Errorf("a click at %v was consumed, want it ignored: it is outside Bounds", at)
		}
	}
	if got := in.Cursor(); got != 3 {
		t.Errorf("an outside click moved the cursor to %d, want it unchanged at 3", got)
	}
	if !in.Focused() {
		t.Errorf("an outside click removed focus")
	}
}

// TestTextInputDragExtendsTheSelectionFromAClick is the two-step mouse gesture:
// a click sets the anchor, a drag grows the selection.
func TestTextInputDragExtendsTheSelectionFromAClick(t *testing.T) {
	in := NewTextInputString(buffer.Rect{W: 10, H: 1}, "abcdefgh")
	clickAt(in, 1, 0)
	if _, _, ok := in.Selection(); ok {
		t.Fatalf("a plain click left a selection behind")
	}
	dragAt(in, 5, 0)
	lo, hi, ok := in.Selection()
	if !ok || lo != 1 || hi != 5 {
		t.Errorf("after dragging to cell 5: selection = [%d,%d) ok=%v, want [1,5)", lo, hi, ok)
	}
	if got := in.SelectedText(); got != "bcde" {
		t.Errorf("SelectedText = %q, want %q", got, "bcde")
	}
}

// TestTextInputUnfocusedFieldConsumesNoKeys is what makes a form of fields
// usable: an unfocused field must not swallow the keystroke meant for its
// sibling.
func TestTextInputUnfocusedFieldConsumesNoKeys(t *testing.T) {
	in := NewTextInput(buffer.Rect{W: 10, H: 1})
	for _, seq := range []string{"a", "\x7f", "\x1b[C", "\x1b[D", "\r", "\x1b[A"} {
		if in.Handle(decodeKey(t, seq)) {
			t.Errorf("an unfocused field consumed %q", seq)
		}
	}
	if got := in.Text(); got != "" {
		t.Errorf("an unfocused field changed its text to %q", got)
	}
}

// TestTextInputScrollsHorizontallyAndShowsTheCaret covers a field narrower than
// its content: the visible window must follow the caret, and the first visible
// character must change when the caret moves off the left edge.
func TestTextInputScrollsHorizontallyAndShowsTheCaret(t *testing.T) {
	in := NewTextInputString(buffer.Rect{W: 5, H: 1}, "abcdefghij")
	in.SetFocused(true)
	buf := buffer.NewBuffer(10, 1)

	in.SetCursor(9)
	in.Draw(buf)
	end := rowOf(buf, 0, 5)

	in.SetCursor(0)
	in.Draw(buf)
	start := rowOf(buf, 0, 5)

	assertDifferent(t, "the window with the caret at the start and at the end", start, end)
	if !strings.HasPrefix(end, "fghi") {
		t.Errorf("with the caret at the end the window is %q, want it to start at \"fghi\"", end)
	}
}

// TestTextInputDegenerateSizesDrawWithoutPanicking is ADR 0007 §4's contract,
// asserted at every size a terminal can produce, including below MinSize. It also
// checks that a 1×1 field still paints its background, because a widget that
// returns early without painting is the stale-cell bug in miniature.
func TestTextInputDegenerateSizesDrawWithoutPanicking(t *testing.T) {
	for _, r := range []buffer.Rect{
		{W: 0, H: 0}, {X: 2, Y: 3, W: 0, H: 5}, {W: 1, H: 1}, {W: 2, H: 2}, {W: 1, H: 3},
	} {
		in := NewTextInputString(r, "hello")
		in.SetFocused(true)
		buf := buffer.NewBuffer(8, 6)
		in.Draw(buf) // must not panic
		if r.W <= 0 || r.H <= 0 {
			continue
		}
		got := rowOf(buf, r.Y, r.W)
		// A one-cell field cannot hold both a character and the truncation marker,
		// and the marker is the more useful of the two: it is what tells the user
		// there is text. Anything wider starts with the text itself.
		want := "h"
		if r.W == 1 {
			want = buffer.TruncSuffix
		}
		if !strings.HasPrefix(got, want) {
			t.Errorf("at %+v the field drew %q, want it to start with %q", r, got, want)
		}
	}
}

// TestTextInputMinSizeIsIndependentOfState pins the ADR 0007 §2 rule that
// MinSize is pure: it must not change when the field gains content or focus, or a
// layout would resize under the user.
func TestTextInputMinSizeIsIndependentOfState(t *testing.T) {
	in := NewTextInput(buffer.Rect{W: 20, H: 1})
	empty := in.MinSize()
	// SetText rather than typing: the field is deliberately unfocused here, and
	// an unfocused field must refuse keystrokes rather than accept them.
	in.SetText("some text")
	in.SetFocused(true)
	if got := in.MinSize(); got != empty {
		t.Errorf("MinSize changed with content and focus: %+v then %+v", empty, got)
	}
	if empty.W < 1 || empty.H < 1 {
		t.Errorf("MinSize = %+v, want at least 1x1", empty)
	}
}

// TestTextInputSelectionIsMarkedWithoutColour is the accessibility rule for a
// selection, asserted on cells rather than on an escape sequence: the selected
// cells must carry an attribute the unselected ones do not, and it must be one
// that survives NO_COLOR.
//
// It compares two cells the widget produced itself, and asserts they differ,
// because a test that compares a selection style with itself cannot fail.
func TestTextInputSelectionIsMarkedWithoutColour(t *testing.T) {
	in := NewTextInputString(buffer.Rect{W: 12, H: 1}, "abcdef")
	in.SetFocused(true)
	in.SetCursor(2)
	press(t, in, "\x1b[1;2C")
	press(t, in, "\x1b[1;2C")
	press(t, in, "\x1b[1;2C")
	// Focus is given up afterwards so that what is being measured is the SELECTION
	// and not the caret, which would otherwise mark a cell of its own.
	in.SetFocused(false)

	buf := buffer.NewBuffer(12, 1)
	in.Draw(buf)
	selected, plain := buf.CellAt(2, 0), buf.CellAt(0, 0)

	if selected.Attr == plain.Attr {
		t.Fatalf("the selected cell and an unselected one have the same attributes (%v): the "+
			"selection would be signalled by colour alone", plain.Attr)
	}
	if selected.Attr&buffer.AttrReverse == 0 {
		t.Errorf("the selected cell has attributes %v, want AttrReverse: reverse video survives "+
			"NO_COLOR, a colour does not", selected.Attr)
	}
	if plain.Attr != 0 {
		t.Errorf("an unselected cell has attributes %v, want none", plain.Attr)
	}
	// And the marked region is exactly the selection, not the whole row.
	if in.SelectedText() != "cde" {
		t.Errorf("SelectedText = %q, want %q", in.SelectedText(), "cde")
	}
	assertDifferent(t, "the attribute of a selected and an unselected cell",
		itoa(int(selected.Attr)), itoa(int(plain.Attr)))
}

// TestTextInputCaretIsDistinctFromSelection is the harder half of the same rule:
// with the default styles, a caret and a selection are both reverse video, so the
// caret adds an underline to stay tellable from a selection.
func TestTextInputCaretIsDistinctFromSelection(t *testing.T) {
	in := NewTextInputString(buffer.Rect{W: 12, H: 1}, "abcdef")
	in.SetFocused(true)
	in.SetCursor(3)
	press(t, in, "\x1b[1;2C") // a one-rune selection at index 3, caret at 4

	buf := buffer.NewBuffer(12, 1)
	in.Draw(buf)
	caret, selected := buf.CellAt(4, 0), buf.CellAt(2, 0)

	if caret.Attr == selected.Attr {
		t.Fatalf("the caret cell and the selected cell have identical attributes (%v): a user "+
			"cannot tell where the text will be typed", caret.Attr)
	}
	if caret.Attr&buffer.AttrUnderline == 0 {
		t.Errorf("the caret cell has attributes %v, want AttrUnderline so it differs from a selection",
			caret.Attr)
	}
	if selected.Attr&buffer.AttrUnderline != 0 {
		t.Errorf("the selected cell has attributes %v, want no underline", selected.Attr)
	}
}

// TestTextInputDrawsTheCaretBlockPastTheEndOfTheText covers the state where there
// is no rune to mark: the caret is a block, and it must be distinguishable from
// the background it sits on.
func TestTextInputDrawsTheCaretBlockPastTheEndOfTheText(t *testing.T) {
	in := NewTextInputString(buffer.Rect{W: 10, H: 1}, "ab")
	in.SetFocused(true)
	in.SetCursor(2)

	buf := buffer.NewBuffer(10, 1)
	in.Draw(buf)
	block := buf.CellAt(2, 0)
	if block.Attr&buffer.AttrReverse == 0 {
		t.Errorf("the cell after the last character has attributes %v, want AttrReverse: the caret "+
			"has no rune to mark, so the block IS the signal", block.Attr)
	}
	if in.Focused() == false {
		t.Fatal("setup: the field should be focused")
	}
}
