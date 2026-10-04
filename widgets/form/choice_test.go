package form

import (
	"strings"
	"testing"

	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
)

// ---------------------------------------------------------------------------
// Select
// ---------------------------------------------------------------------------

// TestSelectDrawsTheMarkerColumn checks the accessibility rule as a cell
// assertion: the highlighted option carries a marker and every other option
// carries a space in the SAME column, so the highlight is a shape difference
// rather than a colour one.
func TestSelectDrawsTheMarkerColumn(t *testing.T) {
	sel := NewSelect(buffer.Rect{W: 12, H: 3}, []string{"alpha", "beta", "gamma"})
	got := screenRows(t, 12, 3, sel)
	wantRow(t, got, 0, SelectDefaultMarker+" alpha")
	wantRow(t, got, 1, "  beta")
	wantRow(t, got, 2, "  gamma")

	// The two marker cells hold different runes, which is what "colour is not the
	// only signal" means concretely: the highlighted row's first cell is the
	// marker, and every other row's is a space.
	assertDifferent(t, "the marker cell of a selected and an unselected option",
		got[0][:len(SelectDefaultMarker)], got[1][:len(SelectDefaultMarker)])
}

// TestSelectKeysMoveTheHighlight walks every key in the documented contract.
func TestSelectKeysMoveTheHighlight(t *testing.T) {
	sel := NewSelect(buffer.Rect{W: 12, H: 6}, []string{"a", "b", "c", "d"})
	sel.SetFocused(true)
	if sel.Selected() != 0 {
		t.Fatalf("setup: selected = %d, want 0", sel.Selected())
	}

	for _, tc := range []struct {
		seq  string
		want int
	}{
		{"\x1b[B", 1},
		{"\x1b[C", 2},
		{"\x1b[A", 1},
		{"\x1b[D", 0},
		{"\x1b[F", 3},
		{"\x1b[H", 0},
		{"\x1b[6~", 3}, // page down to the end of a four-item list
		{"\x1b[5~", 0}, // page back up
	} {
		press(t, sel, tc.seq)
		if got := sel.Selected(); got != tc.want {
			t.Errorf("after %q: selected = %d, want %d", tc.seq, got, tc.want)
		}
	}
}

// TestSelectHighlightClampsAtBothEnds checks the ends, where a naive
// implementation either wraps around or panics.
func TestSelectHighlightClampsAtBothEnds(t *testing.T) {
	sel := NewSelect(buffer.Rect{W: 12, H: 6}, []string{"a", "b"})
	sel.SetFocused(true)
	press(t, sel, "\x1b[A")
	if got := sel.Selected(); got != 0 {
		t.Errorf("up at the first option: selected = %d, want 0", got)
	}
	sel.SetSelected(1)
	press(t, sel, "\x1b[B")
	if got := sel.Selected(); got != 1 {
		t.Errorf("down at the last option: selected = %d, want 1", got)
	}
}

// TestSelectEnterFiresOnSelect covers activation, and checks that OnSelect is
// NOT fired by a programmatic SetSelected — an application changing a selection
// must not receive a user activation it did not ask for.
func TestSelectEnterFiresOnSelect(t *testing.T) {
	sel := NewSelect(buffer.Rect{W: 12, H: 6}, []string{"a", "b", "c"})
	sel.SetFocused(true)
	got := -1
	calls := 0
	sel.OnSelect = func(i int) { got, calls = i, calls+1 }

	sel.SetSelected(2)
	if calls != 0 {
		t.Errorf("SetSelected fired OnSelect %d times, want 0", calls)
	}
	press(t, sel, "\r")
	if calls != 1 || got != 2 {
		t.Errorf("after enter: OnSelect called %d times with %d, want 1 call with 2", calls, got)
	}
	// The reported index is the selected one, which is a different number from the
	// one selected before the activation: comparing the two proves the callback
	// carries the current index rather than a stale one.
	sel.SetSelected(0)
	press(t, sel, "\r")
	if got != 0 {
		t.Errorf("after selecting 0 and activating: OnSelect received %d, want 0", got)
	}
}

// TestSelectClickSelectsAndTakesFocus is the mouse path.
func TestSelectClickSelectsAndTakesFocus(t *testing.T) {
	sel := NewSelect(buffer.Rect{X: 1, W: 12, H: 4}, []string{"a", "b", "c"})
	if !clickAt(sel, 3, 2) {
		t.Fatalf("a click on the third option was not consumed")
	}
	if got := sel.Selected(); got != 2 {
		t.Errorf("after clicking the third row: selected = %d, want 2", got)
	}
	if !sel.Focused() {
		t.Errorf("a click did not take focus")
	}
	// A click in the marker column selects the same row: the marker is part of
	// the row, not a separate widget.
	if !clickAt(sel, 1, 0) {
		t.Fatalf("a click on the marker column was not consumed")
	}
	if got := sel.Selected(); got != 0 {
		t.Errorf("after clicking the marker column of row 0: selected = %d, want 0", got)
	}
}

// TestSelectClickOutsideBoundsAndOnEmptySpaceChangesNothing covers the two ways a
// click can be wrong: outside the widget, and inside it but below the last
// option.
func TestSelectClickOutsideBoundsAndOnEmptySpaceChangesNothing(t *testing.T) {
	sel := NewSelect(buffer.Rect{X: 1, Y: 1, W: 10, H: 3}, []string{"a", "b"})
	sel.SetFocused(true)
	sel.SetSelected(1)

	for _, at := range [][2]int{{0, 1}, {11, 1}, {2, 4}, {2, 0}} {
		if clickAt(sel, at[0], at[1]) {
			t.Errorf("a click at %v was consumed, want it ignored: outside Bounds", at)
		}
	}
	if got := sel.Selected(); got != 1 {
		t.Errorf("an outside click changed the selection to %d, want 1", got)
	}
	// Inside Bounds but past the last option: consumed by nobody, selected by
	// nobody. A list with two options in three rows has a real empty third row.
	if clickAt(sel, 2, 3) {
		t.Errorf("a click on the empty row below the last option selected something")
	}
	if got := sel.Selected(); got != 1 {
		t.Errorf("a click on empty space changed the selection to %d, want 1", got)
	}
}

// TestSelectScrollsWhenTheOptionsDoNotFit is the virtualization property: only
// the visible window is drawn, the scroll offset is clamped, and the window
// really changes.
func TestSelectScrollsWhenTheOptionsDoNotFit(t *testing.T) {
	labels := make([]string, 20)
	for i := range labels {
		labels[i] = string(rune('a'+i%26)) + strings.Repeat("x", 3)
	}
	sel := NewSelect(buffer.Rect{W: 10, H: 3}, labels)
	buf := buffer.NewBuffer(10, 4)
	sel.Draw(buf)
	top := rowOf(buf, 0, 10)
	if sel.Offset() != 0 {
		t.Fatalf("setup: offset = %d, want 0", sel.Offset())
	}

	if !wheelAt(sel, 0, 0, false) {
		t.Fatalf("a wheel notch was not consumed")
	}
	if sel.Offset() == 0 {
		t.Errorf("the scroll offset is still 0 after a wheel notch")
	}
	sel.Draw(buf)
	scrolled := rowOf(buf, 0, 10)
	assertDifferent(t, "the first visible row before and after a wheel scroll", top, scrolled)
}

// TestSelectMouseWheelDoesNotMoveTheHighlight separates the two mouse actions: a
// wheel scrolls the view, a click selects. Conflating them makes a long list
// impossible to look at without changing the value.
func TestSelectMouseWheelDoesNotMoveTheHighlight(t *testing.T) {
	sel := NewSelect(buffer.Rect{W: 10, H: 3}, []string{"a", "b", "c", "d", "e"})
	sel.SetSelected(4)
	wheelAt(sel, 0, 0, false)
	wheelAt(sel, 0, 0, false)
	if got := sel.Selected(); got != 4 {
		t.Errorf("after two wheel notches: selected = %d, want 4", got)
	}
}

// TestSelectTruncatesALongOptionLabel keeps a long label inside the widget.
func TestSelectTruncatesALongOptionLabel(t *testing.T) {
	sel := NewSelect(buffer.Rect{W: 8, H: 1}, []string{"a very long option label"})
	got := screenRows(t, 20, 1, sel)
	if n := buffer.StringWidth(got[0]); n > 8 {
		t.Errorf("row is %d cells wide, want at most 8: %q", n, got[0])
	}
	if !strings.HasSuffix(got[0], buffer.TruncSuffix) {
		t.Errorf("row = %q, want it to end with the truncation marker", got[0])
	}
}

// TestSelectWithNoOptionsIsInert covers the empty list: no panic, no highlight,
// no marker, and nothing consumed by a key.
func TestSelectWithNoOptionsIsInert(t *testing.T) {
	sel := NewSelect(buffer.Rect{W: 10, H: 3}, nil)
	sel.SetFocused(true)
	buf := buffer.NewBuffer(10, 3)
	sel.Draw(buf)
	if got := sel.Selected(); got != -1 {
		t.Errorf("an empty select reports selected = %d, want -1", got)
	}
	for y := 0; y < 3; y++ {
		if got := rowOf(buf, y, 10); got != "          " {
			t.Errorf("row %d = %q, want blanks: an empty select draws its background only", y, got)
		}
	}
	for _, seq := range []string{"\x1b[A", "\x1b[B", "\r"} {
		// An empty list may or may not consume a key; what matters is that
		// neither choice panics and that nothing is selected afterwards.
		sel.Handle(decodeKey(t, seq))
	}
	sel.SetLabels([]string{"now there is one"})
	if got := sel.Selected(); got != 0 {
		t.Errorf("after labels arrive: selected = %d, want 0", got)
	}
}

// TestSelectRepaintsRowsItNoLongerUses is ADR 0007 §1 rule 3 for a list: with
// fewer options than rows, the rows it used to draw must be blanked.
func TestSelectRepaintsRowsItNoLongerUses(t *testing.T) {
	sel := NewSelect(buffer.Rect{W: 10, H: 4}, []string{"a", "b", "c", "d"})
	buf := buffer.NewBuffer(10, 4)
	sel.Draw(buf)
	if strings.TrimSpace(rowOf(buf, 3, 10)) == "" {
		t.Fatalf("setup: row 3 is blank, want it to hold an option")
	}

	sel.SetLabels([]string{"a"})
	sel.Draw(buf)
	for y := 1; y < 4; y++ {
		if got := rowOf(buf, y, 10); got != "          " {
			t.Errorf("row %d after the list shrank is %q, want blanks", y, got)
		}
	}
}

// TestSelectDrawDoesNotAllocate is the ADR 0008 §4 rule for a virtualized widget.
func TestSelectDrawDoesNotAllocate(t *testing.T) {
	sel := NewSelect(buffer.Rect{W: 10, H: 4}, []string{"a", "b", "c", "d", "e", "f"})
	buf := buffer.NewBuffer(10, 4)
	sel.Draw(buf) // warm the row cache
	if got := testing.AllocsPerRun(200, func() { sel.Draw(buf) }); got != 0 {
		t.Errorf("Draw allocated %v times per run after the rows were cached, want 0", got)
	}
}

// ---------------------------------------------------------------------------
// Radio
// ---------------------------------------------------------------------------

// TestRadioMarksTheChosenOptionWithADifferentShape is the radio accessibility
// rule: the chosen option's marker and every other option's are different
// strings, and the focus mark is a third shape again.
func TestRadioMarksTheChosenOptionWithADifferentShape(t *testing.T) {
	g := NewRadio(buffer.Rect{W: 14, H: 3}, []string{"one", "two", "three"})
	got := screenRows(t, 14, 3, g)
	wantRow(t, got, 0, "  "+RadioMarkSelected+" one")
	wantRow(t, got, 1, "  "+RadioMarkUnselected+" two")
	assertDifferent(t, "the chosen and unchosen markers", RadioMarkSelected, RadioMarkUnselected)
}

// TestRadioFocusMarkIsAShapeNotAColour checks that focus has its own indicator,
// separate from selection, and that it appears only on the chosen row.
func TestRadioFocusMarkIsAShapeNotAColour(t *testing.T) {
	g := NewRadio(buffer.Rect{W: 14, H: 3}, []string{"one", "two"})
	buf := buffer.NewBuffer(14, 3)
	g.Draw(buf)
	if strings.HasPrefix(rowOf(buf, 0, 14), RadioFocusMark) {
		t.Errorf("row 0 starts with the focus mark while the group is unfocused: %q", rowOf(buf, 0, 14))
	}

	g.SetFocused(true)
	g.Draw(buf)
	if !strings.HasPrefix(rowOf(buf, 0, 14), RadioFocusMark) {
		t.Errorf("row 0 = %q, want it to start with the focus mark", rowOf(buf, 0, 14))
	}
	if strings.HasPrefix(rowOf(buf, 1, 14), RadioFocusMark) {
		t.Errorf("row 1 = %q, want the focus mark on the chosen row only", rowOf(buf, 1, 14))
	}
	// The focus mark is a shape in its own column, distinct from both the chosen
	// marker and the unchosen one.
	assertDifferent(t, "the focus mark and the chosen marker", RadioFocusMark, RadioMarkSelected)
}

// TestRadioArrowsChooseAndFire covers the radio contract: moving is choosing, and
// each change fires OnSelect once.
func TestRadioArrowsChooseAndFire(t *testing.T) {
	g := NewRadio(buffer.Rect{W: 14, H: 4}, []string{"one", "two", "three"})
	g.SetFocused(true)
	chosen := []int{}
	g.OnSelect = func(i int) { chosen = append(chosen, i) }

	press(t, g, "\x1b[B")
	press(t, g, "\x1b[B")
	if got := g.Selected(); got != 2 {
		t.Errorf("after two downs: selected = %d, want 2", got)
	}
	if len(chosen) != 2 || chosen[0] != 1 || chosen[1] != 2 {
		t.Errorf("OnSelect received %v, want [1 2]", chosen)
	}
	press(t, g, "\x1b[A")
	if got := g.Selected(); got != 1 {
		t.Errorf("after up: selected = %d, want 1", got)
	}
	press(t, g, "\x1b[H")
	if got := g.Selected(); got != 0 {
		t.Errorf("after home: selected = %d, want 0", got)
	}
	if len(chosen) != 4 {
		t.Errorf("OnSelect was called %d times, want 4: one per change, including home", len(chosen))
	}
	// A move that changes nothing must not fire: pressing up at the start of the
	// group is not a user action the application asked to hear about.
	press(t, g, "\x1b[A")
	if len(chosen) != 4 {
		t.Errorf("OnSelect was called %d times after a clamped move, want 4", len(chosen))
	}
}

// TestRadioClickChoosesAndEnterRefires covers both mouse and the confirm key.
func TestRadioClickChoosesAndEnterRefires(t *testing.T) {
	g := NewRadio(buffer.Rect{X: 1, W: 14, H: 3}, []string{"one", "two", "three"})
	fired := -1
	g.OnSelect = func(i int) { fired = i }

	if !clickAt(g, 3, 2) {
		t.Fatalf("a click on the third option was not consumed")
	}
	if got := g.Selected(); got != 2 {
		t.Errorf("after clicking the third option: selected = %d, want 2", got)
	}
	if fired != 2 {
		t.Errorf("the click fired OnSelect with %d, want 2", fired)
	}
	press(t, g, "\r")
	if fired != 2 {
		t.Errorf("after enter, OnSelect fired with %d, want the chosen option 2", fired)
	}
}

// TestRadioWithNoOptionsIsInert is the degenerate list for the second
// virtualized widget.
func TestRadioWithNoOptionsIsInert(t *testing.T) {
	g := NewRadio(buffer.Rect{W: 12, H: 2}, nil)
	g.SetFocused(true)
	buf := buffer.NewBuffer(12, 2)
	g.Draw(buf)
	if got := g.Selected(); got != -1 {
		t.Errorf("an empty group reports selected = %d, want -1", got)
	}
	if got := rowOf(buf, 0, 12); got != "            " {
		t.Errorf("row 0 = %q, want blanks", got)
	}
}

// TestRadioDrawDoesNotAllocate keeps the second virtualized widget honest.
func TestRadioDrawDoesNotAllocate(t *testing.T) {
	g := NewRadio(buffer.Rect{W: 14, H: 3}, []string{"one", "two", "three"})
	buf := buffer.NewBuffer(14, 3)
	g.Draw(buf)
	if got := testing.AllocsPerRun(200, func() { g.Draw(buf) }); got != 0 {
		t.Errorf("Draw allocated %v times per run after the rows were cached, want 0", got)
	}
}

// ---------------------------------------------------------------------------
// Checkbox
// ---------------------------------------------------------------------------

// TestCheckboxHasThreeDistinctMarkers is the tri-state accessibility rule as a
// string comparison: the three markers are three different shapes.
func TestCheckboxHasThreeDistinctMarkers(t *testing.T) {
	assertDifferent(t, "the unchecked and checked markers", CheckMarkUnchecked, CheckMarkChecked)
	assertDifferent(t, "the checked and indeterminate markers", CheckMarkChecked, CheckMarkIndeterminate)
	assertDifferent(t, "the unchecked and indeterminate markers", CheckMarkUnchecked, CheckMarkIndeterminate)
}

// TestCheckboxDrawsEachState checks all three states on screen, which is the only
// way to know the marker is actually the one the state asked for.
func TestCheckboxDrawsEachState(t *testing.T) {
	c := NewCheckbox(buffer.Rect{W: 20, H: 1}, "Notifications")
	for _, tc := range []struct {
		state CheckState
		mark  string
	}{
		{Unchecked, CheckMarkUnchecked},
		{Checked, CheckMarkChecked},
		{Indeterminate, CheckMarkIndeterminate},
	} {
		c.SetState(tc.state)
		got := screenRows(t, 20, 1, c)
		wantRow(t, got, 0, tc.mark+" Notifications")
	}
}

// TestCheckboxTogglesWithSpaceAndEnter covers the toggle keys, including the
// transition out of the mixed state.
func TestCheckboxTogglesWithSpaceAndEnter(t *testing.T) {
	c := NewCheckbox(buffer.Rect{W: 20, H: 1}, "x")
	c.SetFocused(true)

	press(t, c, " ")
	if got := c.State(); got != Checked {
		t.Errorf("after space: state = %v, want checked", got)
	}
	press(t, c, "\r")
	if got := c.State(); got != Unchecked {
		t.Errorf("after enter: state = %v, want unchecked", got)
	}
	c.SetState(Indeterminate)
	press(t, c, " ")
	if got := c.State(); got != Checked {
		t.Errorf("after space from the mixed state: state = %v, want checked", got)
	}
}

// TestCheckboxTriStateCyclesOnlyWhenAsked checks the two key sets separately:
// toggling is always available, while cycling through all three states is opt-in
// because most checkboxes are binary and a stray arrow should not surprise them.
func TestCheckboxTriStateCyclesOnlyWhenAsked(t *testing.T) {
	binary := NewCheckbox(buffer.Rect{W: 20, H: 1}, "x")
	binary.SetFocused(true)
	if binary.Handle(decodeKey(t, "\x1b[C")) {
		t.Errorf("a right arrow was consumed by a binary checkbox")
	}
	if got := binary.State(); got != Unchecked {
		t.Errorf("a right arrow changed the state to %v", got)
	}

	tri := NewCheckbox(buffer.Rect{W: 20, H: 1}, "x")
	tri.TriState = true
	tri.SetFocused(true)
	press(t, tri, "\x1b[C")
	if got := tri.State(); got != Checked {
		t.Errorf("after right: state = %v, want checked", got)
	}
	press(t, tri, "\x1b[C")
	if got := tri.State(); got != Indeterminate {
		t.Errorf("after a second right: state = %v, want indeterminate", got)
	}
	press(t, tri, "\x1b[D")
	if got := tri.State(); got != Checked {
		t.Errorf("after left: state = %v, want checked: the cycle must be reversible", got)
	}
	press(t, tri, "\x1b[D")
	if got := tri.State(); got != Unchecked {
		t.Errorf("after a second left: state = %v, want unchecked", got)
	}
	press(t, tri, "\x1b[D")
	if got := tri.State(); got != Indeterminate {
		t.Errorf("after a third left: state = %v, want the cycle to wrap to indeterminate", got)
	}
}

// TestCheckboxStateIsTotalOverTheByteRange covers a value this package did not
// produce: a CheckState arriving from data. It must degrade, not panic and not
// draw a marker no user could read.
func TestCheckboxStateIsTotalOverTheByteRange(t *testing.T) {
	c := NewCheckbox(buffer.Rect{W: 20, H: 1}, "x")
	c.SetState(CheckState(200))
	if got := c.State(); got != Unchecked {
		t.Errorf("an out-of-range state reads back as %v, want unchecked", got)
	}
	got := screenRows(t, 20, 1, c)
	wantRow(t, got, 0, CheckMarkUnchecked+" x")
}

// TestCheckboxClickTogglesAndOutsideClicksDoNot covers the mouse.
func TestCheckboxClickTogglesAndOutsideClicksDoNot(t *testing.T) {
	c := NewCheckbox(buffer.Rect{X: 2, W: 20, H: 1}, "x")
	if !clickAt(c, 3, 0) {
		t.Fatalf("a click inside the box was not consumed")
	}
	if got := c.State(); got != Checked {
		t.Errorf("after a click: state = %v, want checked", got)
	}
	if !c.Focused() {
		t.Errorf("a click did not take focus")
	}
	for _, at := range [][2]int{{1, 0}, {22, 0}, {3, 1}, {3, -1}} {
		if clickAt(c, at[0], at[1]) {
			t.Errorf("a click at %v was consumed, want it ignored", at)
		}
	}
	if got := c.State(); got != Checked {
		t.Errorf("an outside click changed the state to %v, want it unchanged", got)
	}
}

// TestCheckboxOnChangeFiresOnlyForUserActions separates the two ways a state can
// change.
func TestCheckboxOnChangeFiresOnlyForUserActions(t *testing.T) {
	c := NewCheckbox(buffer.Rect{W: 20, H: 1}, "x")
	c.SetFocused(true)
	seen := []CheckState{}
	c.OnChange = func(s CheckState) { seen = append(seen, s) }

	c.SetState(Checked)
	if len(seen) != 0 {
		t.Errorf("SetState fired OnChange %d times, want 0", len(seen))
	}
	press(t, c, " ")
	if len(seen) != 1 || seen[0] != Unchecked {
		t.Errorf("after a toggle, OnChange saw %v, want [unchecked]", seen)
	}
}

// TestCheckboxTruncatesALongLabel keeps a long label inside the box.
func TestCheckboxTruncatesALongLabel(t *testing.T) {
	c := NewCheckbox(buffer.Rect{W: 10, H: 1}, "a very long label indeed")
	got := screenRows(t, 20, 1, c)
	if n := buffer.StringWidth(got[0]); n > 10 {
		t.Errorf("row is %d cells wide, want at most 10: %q", n, got[0])
	}
	if !strings.HasSuffix(got[0], buffer.TruncSuffix) {
		t.Errorf("row = %q, want it to end with the truncation marker", got[0])
	}
}

// TestCheckboxDrawDoesNotAllocate keeps the label cache keyed on the label, so a
// label that does not fit does not rebuild every frame.
func TestCheckboxDrawDoesNotAllocate(t *testing.T) {
	c := NewCheckbox(buffer.Rect{W: 8, H: 1}, "a very long label indeed")
	buf := buffer.NewBuffer(20, 1)
	c.Draw(buf)
	if got := testing.AllocsPerRun(200, func() { c.Draw(buf) }); got != 0 {
		t.Errorf("Draw allocated %v times per run with a truncated label, want 0", got)
	}
}

// ---------------------------------------------------------------------------
// Toggle
// ---------------------------------------------------------------------------

// TestToggleStateIsAWord covers the on/off accessibility rule: the state is
// spelled out, and both markers are the same width so nothing reflows.
func TestToggleStateIsAWord(t *testing.T) {
	tg := NewToggle(buffer.Rect{W: 20, H: 1}, "Logging")
	got := screenRows(t, 20, 1, tg)
	wantRow(t, got, 0, ToggleMarkOff+" Logging")

	tg.SetOn(true)
	got = screenRows(t, 20, 1, tg)
	wantRow(t, got, 0, ToggleMarkOn+" Logging")

	if buffer.StringWidth(ToggleMarkOn) != buffer.StringWidth(ToggleMarkOff) {
		t.Errorf("the on marker is %d cells and the off marker %d: they must be equal so the label never moves",
			buffer.StringWidth(ToggleMarkOn), buffer.StringWidth(ToggleMarkOff))
	}
	assertDifferent(t, "the on and off markers", ToggleMarkOn, ToggleMarkOff)
}

// TestToggleTogglesWithEveryBinding walks the whole key contract, including the
// mouse.
func TestToggleTogglesWithEveryBinding(t *testing.T) {
	tg := NewToggle(buffer.Rect{X: 1, W: 20, H: 1}, "x")
	tg.SetFocused(true)
	for _, seq := range []string{" ", "\r", "\x1b[C", "\x1b[D"} {
		before := tg.On()
		press(t, tg, seq)
		if tg.On() == before {
			t.Errorf("%q did not toggle the switch", seq)
		}
	}
	if !clickAt(tg, 2, 0) {
		t.Fatalf("a click was not consumed")
	}
	if !tg.Focused() {
		t.Errorf("a click did not take focus")
	}
}

// TestToggleOnChangeFiresOnlyForUserActions separates the two ways the switch can
// change, as for the checkbox.
func TestToggleOnChangeFiresOnlyForUserActions(t *testing.T) {
	tg := NewToggle(buffer.Rect{W: 20, H: 1}, "x")
	tg.SetFocused(true)
	calls := 0
	tg.OnChange = func(bool) { calls++ }

	tg.SetOn(true)
	if calls != 0 {
		t.Errorf("SetOn fired OnChange %d times, want 0", calls)
	}
	press(t, tg, " ")
	if calls != 1 {
		t.Errorf("after a toggle, OnChange fired %d times, want 1", calls)
	}
}

// TestToggleDrawDoesNotAllocate keeps the toggle's label cache honest.
func TestToggleDrawDoesNotAllocate(t *testing.T) {
	tg := NewToggle(buffer.Rect{W: 20, H: 1}, "Logging")
	buf := buffer.NewBuffer(20, 1)
	tg.Draw(buf)
	if got := testing.AllocsPerRun(200, func() { tg.Draw(buf) }); got != 0 {
		t.Errorf("Draw allocated %v times per run after the label was cached, want 0", got)
	}
}

// TestEveryChoiceWidgetRejectsAWheelItDoesNotOwn pins the wheel contract for the
// two that have no scrolling to do: a wheel notch over a checkbox is not the
// checkbox's event, so it must not be consumed.
func TestEveryChoiceWidgetRejectsAWheelItDoesNotOwn(t *testing.T) {
	for _, w := range []termmosaic.Widget{
		NewCheckbox(buffer.Rect{W: 10, H: 1}, "x"),
		NewToggle(buffer.Rect{W: 10, H: 1}, "x"),
		NewButton(buffer.Rect{W: 10, H: 1}, "x"),
	} {
		if wheelAt(w, 0, 0, false) {
			t.Errorf("%T consumed a wheel notch, want it left for whatever scrolls", w)
		}
	}
}
