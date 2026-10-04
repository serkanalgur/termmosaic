package form

import (
	"strings"
	"testing"

	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
)

// ---------------------------------------------------------------------------
// Tabs
// ---------------------------------------------------------------------------

// TestTabsMarksTheSelectedTabWithBrackets is the accessibility requirement in the
// task statement: the selected tab is marked WITHOUT relying on colour. The
// assertion is on cells and on the bracket characters themselves, not on an SGR
// sequence, so it cannot be satisfied by a colour change.
func TestTabsMarksTheSelectedTabWithBrackets(t *testing.T) {
	tabs := NewTabs(buffer.Rect{W: 24, H: 1}, []string{"one", "two", "three"})
	got := screenRows(t, 24, 1, tabs)
	wantRow(t, got, 0, "[one] two  three")

	tabs.SetSelected(1)
	got = screenRows(t, 24, 1, tabs)
	wantRow(t, got, 0, " one [two] three ")

	assertDifferent(t, "the selected and unselected tab marks", TabMarkSelected, TabMarkUnselected)
}

// TestTabsSelectionMovesWithoutChangingTheLayout pins the reason the unselected
// mark is a space rather than nothing: selecting a different tab must not move any
// other tab, or the row jumps under the cursor.
func TestTabsSelectionMovesWithoutChangingTheLayout(t *testing.T) {
	tabs := NewTabs(buffer.Rect{W: 24, H: 1}, []string{"one", "two", "three"})
	first := screenRows(t, 24, 1, tabs)[0]
	tabs.SetSelected(2)
	second := screenRows(t, 24, 1, tabs)[0]

	// The labels sit in the same columns in both frames. Only the positions are
	// compared, because the frames legitimately differ in length by one trailing
	// space: the selected tab's closing bracket is a glyph where an unselected
	// tab's is a blank.
	for _, label := range []string{"one", "two", "three"} {
		if strings.Index(first, label) != strings.Index(second, label) {
			t.Errorf("the label %q is at column %d then %d: selecting a tab must not reflow the row",
				label, strings.Index(first, label), strings.Index(second, label))
		}
	}
}

// TestTabsKeysMoveTheSelection walks the whole key contract.
func TestTabsKeysMoveTheSelection(t *testing.T) {
	tabs := NewTabs(buffer.Rect{W: 30, H: 1}, []string{"a", "b", "c", "d"})
	tabs.SetFocused(true)
	for _, tc := range []struct {
		seq  string
		want int
	}{
		{"\x1b[C", 1},
		{"\x1b[D", 0},
		{"\x1b[F", 3},
		{"\x1b[H", 0},
		{"\x1b[B", 1},
		{"\x1b[A", 0},
		// A one-row viewport has no page to move, so a page key moves one tab:
		// page() clamps to at least one rather than dividing by nothing.
		{"\x1b[6~", 1},
		{"\x1b[5~", 0},
	} {
		press(t, tabs, tc.seq)
		if got := tabs.Selected(); got != tc.want {
			t.Errorf("after %q: selected = %d, want %d", tc.seq, got, tc.want)
		}
	}
}

// TestTabsArrowsSelectAndFire is the tab-bar contract: moving IS selecting, and
// each change fires OnSelect once.
func TestTabsArrowsSelectAndFire(t *testing.T) {
	tabs := NewTabs(buffer.Rect{W: 30, H: 1}, []string{"a", "b", "c"})
	tabs.SetFocused(true)
	chosen := []int{}
	tabs.OnSelect = func(i int) { chosen = append(chosen, i) }

	press(t, tabs, "\x1b[C")
	press(t, tabs, "\x1b[C")
	if got := tabs.Selected(); got != 2 {
		t.Errorf("after two rights: selected = %d, want 2", got)
	}
	if len(chosen) != 2 {
		t.Errorf("OnSelect fired %d times, want 2", len(chosen))
	}
	tabs.SetSelected(1)
	if len(chosen) != 2 {
		t.Errorf("SetSelected fired OnSelect %d times, want 2", len(chosen))
	}
	press(t, tabs, "\r")
	if len(chosen) != 3 || chosen[2] != 1 {
		t.Errorf("after enter, OnSelect saw %v, want one more call with 1", chosen)
	}
}

// TestTabsClickSelectsAndTakesFocus is the mouse path, including a click on a
// bracket, which is part of the tab.
func TestTabsClickSelectsAndTakesFocus(t *testing.T) {
	tabs := NewTabs(buffer.Rect{X: 1, W: 30, H: 1}, []string{"aa", "bb", "cc"})
	// Cell 0 is the first tab's opening bracket, so the second tab occupies
	// cells 4 to 7 and cell 5 is unambiguously inside it.
	if !clickAt(tabs, 5, 0) {
		t.Fatalf("a click on the second tab was not consumed")
	}
	if got := tabs.Selected(); got != 1 {
		t.Errorf("after clicking the second tab: selected = %d, want 1", got)
	}
	if !tabs.Focused() {
		t.Errorf("a click did not take focus")
	}
}

// TestTabsClickOutsideBoundsChangesNothing is the negative mouse case.
func TestTabsClickOutsideBoundsChangesNothing(t *testing.T) {
	tabs := NewTabs(buffer.Rect{X: 2, Y: 1, W: 20, H: 1}, []string{"aa", "bb"})
	tabs.SetFocused(true)
	tabs.SetSelected(1)
	for _, at := range [][2]int{{0, 1}, {22, 1}, {3, 0}, {3, 2}} {
		if clickAt(tabs, at[0], at[1]) {
			t.Errorf("a click at %v was consumed, want it ignored: outside Bounds", at)
		}
	}
	if got := tabs.Selected(); got != 1 {
		t.Errorf("an outside click changed the selection to %d, want 1", got)
	}
}

// TestTabsScrollsWhenTheyDoNotFit is the virtualization property for a tab row.
func TestTabsScrollsWhenTheyDoNotFit(t *testing.T) {
	labels := make([]string, 12)
	for i := range labels {
		labels[i] = "tab" + string(rune('a'+i))
	}
	tabs := NewTabs(buffer.Rect{W: 10, H: 1}, labels)
	buf := buffer.NewBuffer(10, 1)
	tabs.Draw(buf)
	first := rowOf(buf, 0, 10)

	if !wheelAt(tabs, 0, 0, false) {
		t.Fatalf("a wheel notch was not consumed")
	}
	tabs.Draw(buf)
	after := rowOf(buf, 0, 10)
	assertDifferent(t, "the tab row before and after a wheel scroll", first, after)
	if tabs.Offset() == 0 {
		t.Errorf("the tab row did not scroll")
	}
}

// TestTabsIsOneRowTall checks the row that stays a background even when the
// widget is given more height than a tab bar needs: a tab bar that grew a second
// row of tabs would stop being a tab bar.
func TestTabsIsOneRowTall(t *testing.T) {
	tabs := NewTabs(buffer.Rect{W: 24, H: 3}, []string{"one", "two"})
	got := screenRows(t, 10, 3, tabs)
	wantRow(t, got, 0, "[one] two ")
	for y := 1; y < 3; y++ {
		if got[y] != "" {
			t.Errorf("row %d = %q, want the tab row to be one row tall", y, got[y])
		}
	}
}

// TestTabsTruncatesALongLabelRatherThanLosingTheBrackets keeps the non-colour
// indicator at any width: a tab too narrow for its label loses the label, never
// the brackets.
func TestTabsTruncatesALongLabelRatherThanLosingTheBrackets(t *testing.T) {
	tabs := NewTabs(buffer.Rect{W: 6, H: 1}, []string{"a very long tab label"})
	got := screenRows(t, 20, 1, tabs)
	if !strings.HasPrefix(got[0], TabMarkSelected) {
		t.Errorf("row = %q, want it to start with the selection bracket", got[0])
	}
	if !strings.HasSuffix(got[0], TabMarkSelectedEnd) {
		t.Errorf("row = %q, want it to end with the selection bracket", got[0])
	}
}

// TestTabsWithNoLabelsIsInert is the empty tab bar.
func TestTabsWithNoLabelsIsInert(t *testing.T) {
	tabs := NewTabs(buffer.Rect{W: 10, H: 1}, nil)
	tabs.SetFocused(true)
	buf := buffer.NewBuffer(10, 1)
	tabs.Draw(buf)
	if got := tabs.Selected(); got != -1 {
		t.Errorf("an empty tab row reports selected = %d, want -1", got)
	}
	if got := rowOf(buf, 0, 10); got != "          " {
		t.Errorf("row = %q, want blanks", got)
	}
	tabs.Handle(termmosaic.ResizeEvent(3, 3))
}

// TestTabsDrawDoesNotAllocate keeps the cached tab rows honest.
func TestTabsDrawDoesNotAllocate(t *testing.T) {
	tabs := NewTabs(buffer.Rect{W: 24, H: 1}, []string{"one", "two", "three"})
	buf := buffer.NewBuffer(24, 1)
	tabs.Draw(buf)
	if got := testing.AllocsPerRun(200, func() { tabs.Draw(buf) }); got != 0 {
		t.Errorf("Draw allocated %v times per run after the rows were cached, want 0", got)
	}
}

// ---------------------------------------------------------------------------
// Button
// ---------------------------------------------------------------------------

// TestButtonRingsItselfOnlyWhileFocused is the focus accessibility rule: focus is
// brackets, and the button is the same width in both states so that tabbing
// around a form does not reflow it.
func TestButtonRingsItselfOnlyWhileFocused(t *testing.T) {
	b := NewButton(buffer.Rect{W: 12, H: 1}, "Save")
	buf := buffer.NewBuffer(12, 1)
	b.Draw(buf)
	// Six cells of button — a space, the label, a space — centred in twelve, so it
	// starts at column three.
	unfocused := cells(buf, 0, 3, 6)
	if unfocused != " Save " {
		t.Fatalf("the unfocused button's own six cells are %q, want %q", unfocused, " Save ")
	}

	b.SetFocused(true)
	b.Draw(buf)
	focused := cells(buf, 0, 3, 6)
	if focused != ButtonRingFocused[:1]+"Save"+ButtonRingFocused[1:] {
		t.Errorf("the focused button's own six cells are %q, want %q", focused, "[Save]")
	}
	if len(focused) != len(unfocused) {
		t.Errorf("the button is %d cells unfocused and %d focused: the rings must not change the width",
			len(unfocused), len(focused))
	}
}

// TestButtonIsCentredInItsBounds covers the placement arithmetic, which has to
// agree with geometry.Align's own convention rather than rounding differently.
func TestButtonIsCentredInItsBounds(t *testing.T) {
	b := NewButton(buffer.Rect{W: 11, H: 1}, "Go")
	got := screenRows(t, 11, 1, b)
	// Three cells of button — a space, the label, a space — in eleven cells,
	// starting at column three, which is the reading geometry.Align uses for an
	// odd remainder.
	wantRow(t, got, 0, "    Go")

	// A taller bounds still gives a one-row button, centred vertically with any
	// odd row left at the bottom.
	tall := NewButton(buffer.Rect{W: 11, H: 3}, "Go")
	got = screenRows(t, 11, 3, tall)
	wantRow(t, got, 0, "")
	wantRow(t, got, 1, "    Go")
	wantRow(t, got, 2, "")
}

// TestButtonActivatesWithBothKeysAndAClick covers the whole contract, and checks
// that the callback fires once per activation.
func TestButtonActivatesWithBothKeysAndAClick(t *testing.T) {
	b := NewButton(buffer.Rect{X: 2, W: 10, H: 1}, "Go")
	b.SetFocused(true)
	fired := 0
	b.OnActivate = func() { fired++ }

	for _, seq := range []string{"\r", " "} {
		press(t, b, seq)
	}
	if fired != 2 {
		t.Errorf("enter and space fired OnActivate %d times, want 2", fired)
	}
	if !clickAt(b, 4, 0) {
		t.Fatalf("a click inside the button was not consumed")
	}
	if fired != 3 {
		t.Errorf("a click fired OnActivate %d times in total, want 3", fired)
	}
	if !b.Focused() {
		t.Errorf("a click did not take focus")
	}
}

// TestButtonClickOutsideBoundsChangesNothing is the negative mouse case.
func TestButtonClickOutsideBoundsChangesNothing(t *testing.T) {
	b := NewButton(buffer.Rect{X: 3, Y: 1, W: 6, H: 1}, "Go")
	fired := 0
	b.OnActivate = func() { fired++ }
	for _, at := range [][2]int{{2, 1}, {9, 1}, {4, 0}, {4, 2}} {
		if clickAt(b, at[0], at[1]) {
			t.Errorf("a click at %v was consumed, want it ignored: outside Bounds", at)
		}
	}
	if fired != 0 {
		t.Errorf("outside clicks fired OnActivate %d times, want 0", fired)
	}
	if b.Focused() {
		t.Errorf("an outside click took focus")
	}
}

// TestButtonDisabledConsumesNothing is the behaviour half of Disabled: a disabled
// button is inert, not merely grey.
func TestButtonDisabledConsumesNothing(t *testing.T) {
	b := NewButton(buffer.Rect{W: 10, H: 1}, "Go")
	b.SetFocused(true)
	b.SetDisabled(true)
	fired := 0
	b.OnActivate = func() { fired++ }

	if b.Handle(decodeKey(t, "\r")) {
		t.Errorf("a disabled button consumed enter")
	}
	if clickAt(b, 2, 0) {
		t.Errorf("a disabled button consumed a click")
	}
	if fired != 0 {
		t.Errorf("a disabled button fired OnActivate %d times, want 0", fired)
	}
}

// TestButtonTruncatesALongLabel covers the narrow case, where the label must give
// way rather than spilling over whatever is composed beside it.
func TestButtonTruncatesALongLabel(t *testing.T) {
	b := NewButton(buffer.Rect{X: 1, W: 6, H: 1}, "a very long button label")
	buf := buffer.NewBuffer(12, 1)
	b.Draw(buf)
	got := rowOf(buf, 0, 6)
	if !strings.HasSuffix(got, buffer.TruncSuffix) {
		t.Errorf("the button's own six cells are %q, want them to end with the truncation marker", got)
	}
	// Nothing outside the button's rect may be touched: the five cells to the
	// right of it belong to whatever is composed beside it.
	if tail := cells(buf, 0, 7, 5); strings.TrimSpace(tail) != "" {
		t.Errorf("the cells right of the button are %q, want blanks: it wrote outside its bounds", tail)
	}
}

// TestButtonFocusIsNotAColourOnlySignal asserts the focus ring through the cells
// rather than through an SGR sequence.
func TestButtonFocusIsNotAColourOnlySignal(t *testing.T) {
	b := NewButton(buffer.Rect{W: 10, H: 1}, "Go")
	buf := buffer.NewBuffer(10, 1)
	b.Draw(buf)
	unfocused := rowOf(buf, 0, 10)
	b.SetFocused(true)
	b.Draw(buf)
	focused := rowOf(buf, 0, 10)

	assertDifferent(t, "the button row focused and unfocused", focused, unfocused)
	if !strings.Contains(focused, ButtonRingFocused[:1]+"Go"+ButtonRingFocused[1:]) {
		t.Errorf("focused row = %q, want the label between the ring's brackets", focused)
	}
}

// TestButtonDrawDoesNotAllocate keeps the label cache honest.
func TestButtonDrawDoesNotAllocate(t *testing.T) {
	b := NewButton(buffer.Rect{W: 10, H: 3}, "Go")
	buf := buffer.NewBuffer(10, 3)
	b.Draw(buf)
	if got := testing.AllocsPerRun(200, func() { b.Draw(buf) }); got != 0 {
		t.Errorf("Draw allocated %v times per run after the label was cached, want 0", got)
	}
}

// ---------------------------------------------------------------------------
// KeyHint
// ---------------------------------------------------------------------------

// TestKeyHintBracketsTheKeyIsTheAccessibilityRule: the key is a shape distinct
// from its description, so a hint is readable with no colour at all.
func TestKeyHintBracketsTheKey(t *testing.T) {
	k := NewKeyHint(buffer.Rect{W: 40, H: 1}, []Binding{
		NewBinding("tab", "next field"),
		NewBinding("enter", "select"),
	})
	got := screenRows(t, 40, 1, k)
	wantRow(t, got, 0, "[tab] next field"+KeyHintSep+"[enter] select")
}

// TestKeyHintHonoursACustomSeparator covers the configurable separator and its
// documented default.
func TestKeyHintHonoursACustomSeparator(t *testing.T) {
	k := NewKeyHint(buffer.Rect{W: 40, H: 1}, []Binding{
		NewBinding("a", "one"), NewBinding("b", "two"),
	})
	if got := screenRows(t, 40, 1, k)[0]; !strings.Contains(got, KeyHintSep) {
		t.Errorf("row = %q, want the default separator %q", got, KeyHintSep)
	}
	k.SetSep(" | ")
	if got := screenRows(t, 40, 1, k)[0]; !strings.Contains(got, " | ") {
		t.Errorf("row = %q, want the custom separator", got)
	}
}

// TestKeyHintTruncatesRatherThanClips is the information-budget case for the
// discoverability widget: a narrow pane says there was more rather than cutting a
// key name in half.
func TestKeyHintTruncatesRatherThanClips(t *testing.T) {
	k := NewKeyHint(buffer.Rect{W: 10, H: 1}, []Binding{
		NewBinding("pgdn", "scroll down the option list"),
	})
	got := screenRows(t, 20, 1, k)
	if !strings.HasSuffix(got[0], buffer.TruncSuffix) {
		t.Errorf("row = %q, want it to end with the truncation marker", got[0])
	}
	if n := buffer.StringWidth(got[0]); n != 10 {
		t.Errorf("row is %d cells, want 10", n)
	}
}

// TestKeyHintReactsToChangedBindings is the reason it is a widget: the hints a
// form shows change with its state, and the cached line must follow.
func TestKeyHintReactsToChangedBindings(t *testing.T) {
	k := NewKeyHint(buffer.Rect{W: 40, H: 1}, []Binding{NewBinding("enter", "select")})
	first := screenRows(t, 40, 1, k)[0]

	k.SetBindings([]Binding{
		NewBinding("up", "previous option"),
		NewBinding("enter", "select"),
	})
	second := screenRows(t, 40, 1, k)[0]

	assertDifferent(t, "the hint line before and after the bindings changed", first, second)
	if !strings.HasPrefix(second, "[up]") {
		t.Errorf("row = %q, want the new binding first", second)
	}
}

// TestKeyHintWithNoBindingsDrawsNothing is the empty case: a form with nothing to
// suggest shows a blank line, not a stray separator.
func TestKeyHintWithNoBindingsDrawsNothing(t *testing.T) {
	k := NewKeyHint(buffer.Rect{W: 20, H: 1}, nil)
	buf := buffer.NewBuffer(20, 1)
	k.Draw(buf)
	if got := rowOf(buf, 0, 20); got != "                    " {
		t.Errorf("row = %q, want blanks", got)
	}
	k.SetBindings([]Binding{NewBinding("x", "y")})
	k.Draw(buf)
	if got := rowOf(buf, 0, 20); strings.TrimSpace(got) == "" {
		t.Errorf("after bindings arrive the row is still blank: %q", got)
	}
}

// TestKeyHintConsumesNothing pins the label contract: a key pressed while reading
// the hints belongs to whatever the hints describe.
func TestKeyHintConsumesNothing(t *testing.T) {
	k := NewKeyHint(buffer.Rect{W: 20, H: 1}, []Binding{NewBinding("enter", "select")})
	for _, seq := range []string{"\r", " ", "\x1b[A"} {
		if k.Handle(decodeKey(t, seq)) {
			t.Errorf("the hint consumed %q, want it left for the widget the hint describes", seq)
		}
	}
}

// TestKeyHintDrawDoesNotAllocate keeps the cached line honest.
func TestKeyHintDrawDoesNotAllocate(t *testing.T) {
	k := NewKeyHint(buffer.Rect{W: 40, H: 1}, []Binding{
		NewBinding("tab", "next field"), NewBinding("enter", "select"),
	})
	buf := buffer.NewBuffer(40, 1)
	k.Draw(buf)
	if got := testing.AllocsPerRun(200, func() { k.Draw(buf) }); got != 0 {
		t.Errorf("Draw allocated %v times per run after the line was cached, want 0", got)
	}
}
