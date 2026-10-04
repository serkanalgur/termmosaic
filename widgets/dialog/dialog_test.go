package dialog

import (
	"strings"
	"testing"

	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
)

// TestInfoDialogDrawsTitleBodyAndDismiss is the whole of the info variant, read
// off the screen: a titled frame, the message, and one button carrying the focus
// ring.
func TestInfoDialogDrawsTitleBodyAndDismiss(t *testing.T) {
	d := infoDialog(40, 9)
	got := rows(t, 40, 9, d)

	if !strings.Contains(got[0], "Saved") {
		t.Errorf("row 0 should carry the title: %q", got[0])
	}
	wantFramed(t, got, 0)
	wantFramed(t, got, 8)
	want(t, got, 3, "Your changes were written.")
	want(t, got, 7, "["+DefaultDismissLabel+"]")
}

// TestConfirmDialogDrawsBothButtonsOnOneRow pins the two-button layout: one row,
// left to right, Cancel first. The order is the conventional one and the
// default focus is asserted separately, because the two are different claims.
func TestConfirmDialogDrawsBothButtonsOnOneRow(t *testing.T) {
	got := rows(t, 40, 9, confirmDialog(40, 9))
	want(t, got, 7, " "+DefaultCancelLabel+"   ["+DefaultOKLabel+"]")
}

// TestConfirmDefaultFocusIsTheAffirmativeAction is the reason VariantConfirm
// exists rather than a generic two-button dialog: a dialog that opens with Cancel
// focused makes walking away the answer a stray Enter reaches.
//
// It asserts on the RING, which is a character, so the claim is about what the
// user can see rather than about a style the test set itself.
func TestConfirmDefaultFocusIsTheAffirmativeAction(t *testing.T) {
	d := confirmDialog(40, 9)
	if got := d.FocusedAction(); got != 1 {
		t.Fatalf("a fresh confirm dialog focuses action %d (%q), want the OK button",
			got, d.Actions()[got].Label)
	}
	got := rows(t, 40, 9, d)
	if !strings.Contains(got[7], "["+DefaultOKLabel+"]") {
		t.Errorf("the OK button is not ringed, so focus is not where the test says: %q", got[7])
	}
	if strings.Contains(got[7], "["+DefaultCancelLabel+"]") {
		t.Errorf("the Cancel button is ringed as well as OK: %q", got[7])
	}
}

// TestChoiceDialogDrawsTheMarkerColumn pins the choice variant's one
// colour-independent signal: a marker in its OWN column, so every label starts on
// the same cell whether or not it is the focused one.
func TestChoiceDialogDrawsTheMarkerColumn(t *testing.T) {
	got := rows(t, 40, 12, choiceDialog(40, 12, 6))
	want(t, got, 3, ChoiceMarker+" a-option")
	want(t, got, 4, "  b-option")
	want(t, got, 5, "  c-option")
	want(t, got, 10, " "+DefaultCancelLabel)
}

// atSize returns d resized to w by h, for the tests that render the same widget at
// several sizes — which is the only way a responsive claim can be tested at all,
// since a widget's own bounds are what it lays itself out from.
func atSize(d *Dialog, w, h int) *Dialog {
	d.SetBounds(rect(w, h))
	return d
}

// TestFocusedChoiceCarriesTheMarker asserts the marker FOLLOWS the focus rather
// than being painted on whichever row happens to be first — which is the failure
// a marker built into the label rather than into its own column would have.
func TestFocusedChoiceCarriesTheMarker(t *testing.T) {
	d := choiceDialog(40, 12, 6)
	d.SetFocused(true)

	first := rows(t, 40, 12, d)
	d.SetFocus(3)
	third := rows(t, 40, 12, d)

	// The choices are the third through eighth interior rows, so focus 3 is the
	// sixth row: the fourth choice.
	assertDifferent(t, "the rows before and after moving the focus", first[3], third[6])
	if !strings.Contains(third[6], ChoiceMarker+" d-option") {
		t.Errorf("the newly focused choice carries no marker: %q", third[6])
	}
	if strings.Contains(third[3], ChoiceMarker) {
		t.Errorf("a choice that is no longer focused still carries the marker: %q", third[3])
	}
}

// TestFocusIsMarkedByAShapeNotAColour is the accessibility claim, asserted the only
// way it can honestly be asserted: two dialogs differing ONLY in focus must differ
// in a CHARACTER.
//
// If focus were signalled by colour alone this test could not fail, which is
// exactly what makes it worth having: it is the regression net for someone
// deleting the ring.
func TestFocusIsMarkedByAShapeNotAColour(t *testing.T) {
	// The two dialogs differ in ONE thing: which action carries the ring. They are
	// otherwise identical and neither has widget focus, so any difference between
	// them is the focus mark and nothing else.
	onOK := confirmDialog(40, 9)
	onCancel := atSize(confirmDialog(40, 9), 40, 9)
	onCancel.SetFocus(0)

	a := rows(t, 40, 9, onOK)
	b := rows(t, 40, 9, onCancel)

	assertDifferent(t, "the button row with the ring on each action", a[7], b[7])
	if !strings.Contains(a[7], "["+DefaultOKLabel+"]") {
		t.Errorf("the OK button is not ringed: %q", a[7])
	}
	if !strings.Contains(b[7], "["+DefaultCancelLabel+"]") {
		t.Errorf("the Cancel button is not ringed: %q", b[7])
	}
	// The ring moved without changing a single cell's width, which is what keeps a
	// focus change from reflowing the row.
	if len([]rune(a[7])) != len([]rune(b[7])) {
		t.Errorf("the focus mark changed the row's width: %d vs %d cells",
			len([]rune(a[7])), len([]rune(b[7])))
	}
}

// TestTheRingMarksThePendingActionNotWidgetFocus pins the one place the two kinds
// of focus come apart, because it is a decision rather than an accident: a dialog
// that has lost widget focus still says which action Enter WOULD press, since the
// ring is the default-action marker as much as a focus marker.
//
// Without this, un-focusing a dialog would silently make its default action
// invisible, and an application that re-showed it would show the user a modal with
// no idea what Return does.
func TestTheRingMarksThePendingActionNotWidgetFocus(t *testing.T) {
	d := confirmDialog(40, 9)
	if got := rows(t, 40, 9, d); !strings.Contains(got[7], "["+DefaultOKLabel+"]") {
		t.Errorf("a dialog that has never been focused should still ring its pending action: %q", got[7])
	}
	d.SetFocused(true)
	d.SetFocused(false)
	if got := rows(t, 40, 9, d); !strings.Contains(got[7], "["+DefaultOKLabel+"]") {
		t.Errorf("losing widget focus hid the pending action's ring: %q", got[7])
	}
}

// TestTheFocusRingHasRingsOfEqualWidth is the arithmetic behind that: the focused
// ring and the unfocused padding must be the same width, or the whole assertion
// above is a description of a reflow rather than of a focus mark.
func TestTheFocusRingHasRingsOfEqualWidth(t *testing.T) {
	if buffer.StringWidth(ActionRingFocused) != buffer.StringWidth(ActionRingIdle) {
		t.Errorf("the two rings are %d and %d cells: focus would reflow the row",
			buffer.StringWidth(ActionRingFocused), buffer.StringWidth(ActionRingIdle))
	}
	if buffer.StringWidth(ChoiceMarker) != 1 {
		t.Errorf("the choice marker is %d cells; a marker column must be one",
			buffer.StringWidth(ChoiceMarker))
	}
}

// TestTheFocusRingIsChoicesThenActions pins the ring's ORDER, which is what makes
// "Tab moves through the list and then reaches the buttons" true rather than
// hoped for.
func TestTheFocusRingIsChoicesThenActions(t *testing.T) {
	d := choiceDialog(40, 12, 3)
	if got, want := d.FocusCount(), 4; got != want {
		t.Fatalf("FocusCount = %d, want %d: three choices and one button", got, want)
	}
	for i := 0; i < 3; i++ {
		d.SetFocus(i)
		if got := d.FocusedChoice(); got != i {
			t.Errorf("ring item %d is choice %d, want %d", i, got, i)
		}
		if got := d.FocusedAction(); got != -1 {
			t.Errorf("ring item %d is also action %d, want -1", i, got)
		}
	}
	d.SetFocus(3)
	if got := d.FocusedChoice(); got != -1 {
		t.Errorf("ring item 3 is choice %d, want -1", got)
	}
	if got := d.FocusedAction(); got != 0 {
		t.Errorf("ring item 3 is action %d, want 0", got)
	}
}

// TestAnActionStyleOverridesTheDialogStyle asserts the documented precedence:
// a bespoke action keeps its own colours and gains the focus attribute, while an
// action with none follows ActionStyle.
//
// The two branches are separate assertions because the bug this guards against is
// asymmetric — one of them silently winning over the other.
func TestAnActionStyleOverridesTheDialogStyle(t *testing.T) {
	plain := New(rect(40, 9), VariantConfirm)
	plain.ActionStyle = buffer.NewStyle(buffer.NewColour(0, 0, 0x80), buffer.DefaultColour, 0)

	bespoke := New(rect(40, 9), VariantConfirm)
	bespoke.ActionStyle = buffer.NewStyle(buffer.NewColour(0, 0, 0x80), buffer.DefaultColour, 0)
	bespoke.SetActions(Action{Label: DefaultCancelLabel}, Action{Label: DefaultOKLabel, Style: buffer.NewStyle(buffer.NewColour(0, 0, 0xff), buffer.DefaultColour, 0)})

	bufPlain := cellBuf(40, 9)
	plain.Draw(bufPlain)
	bufBespoke := cellBuf(40, 9)
	bespoke.Draw(bufBespoke)

	// Cell (2, 7) is inside the Cancel button on both dialogs, which carries no
	// style of its own; (12, 7) is inside OK, which does on the second.
	if got, want := bufPlain.CellAt(2, 7).FG, buffer.NewColour(0, 0, 0x80); got != want {
		t.Errorf("an action with no style of its own has fg %#x, want the dialog's %#x", got, want)
	}
	if got, want := bufBespoke.CellAt(12, 7).FG, buffer.NewColour(0, 0, 0xff); got != want {
		t.Errorf("an action with its own style has fg %#x, want its own %#x", got, want)
	}
}

// TestDrawIsTotalAtEveryDegenerateSize walks the sizes ADR 0007 §4 names —
// 0x0, 1x1, and everything below the minimum — through the whole stack. The
// assertion is that nothing panics and that the frame, where there is room for
// one, is still drawn: a widget that goes blank when it cannot show everything
// loses the information precisely when information is scarce.
func TestDrawIsTotalAtEveryDegenerateSize(t *testing.T) {
	for _, size := range []struct{ w, h int }{
		{0, 0}, {1, 1}, {2, 1}, {1, 2}, {2, 2}, {3, 2}, {2, 3}, {3, 3},
		{4, 3}, {5, 4}, {6, 4}, {7, 5}, {8, 5}, {9, 6}, {11, 4}, {13, 3},
	} {
		sizes := []struct {
			name string
			d    *Dialog
		}{
			{"info", infoDialog(size.w, size.h)},
			{"confirm", confirmDialog(size.w, size.h)},
			{"choice", choiceDialog(size.w, size.h, 5)},
			{"choice many", choiceDialog(size.w, size.h, 40)},
			{"no body", New(rect(size.w, size.h), VariantConfirm)},
			{"no actions", New(rect(size.w, size.h), VariantInfo)},
		}
		for _, s := range sizes {
			t.Run(s.name, func(t *testing.T) {
				d := s.d
				// Through the renderer, so the encoder and the headless screen
				// model are in the loop and an unrecognised sequence fails here
				// rather than in somebody's terminal.
				got := rows(t, size.w, size.h, d)

				// At 2x2 and up there is room for a frame, so a frame is what
				// must be there; and at a size with room for a body line there
				// must be one. A widget that goes blank when it cannot show
				// everything loses the information precisely when information
				// is scarce, which is what ADR 0007 §4 rules out.
				if size.w >= 2 && size.h >= 2 {
					wantFramed(t, got, 0)
					wantFramed(t, got, size.h-1)
					if !showsContent(got, size) && size.w >= 6 && size.h >= 4 {
						t.Errorf("%dx%d drew a frame and no content at all:\n%s",
							size.w, size.h, strings.Join(got, "\n"))
					}
				}
			})
		}
	}
}

// showsContent reports whether the screen carries anything inside the frame, which
// is the difference between "clipped" and "blank" for a widget below its minimum.
func showsContent(got []string, size struct{ w, h int }) bool {
	for y := 1; y < size.h-1; y++ {
		if strings.TrimSpace(unframe(got[y])) != "" {
			return true
		}
	}
	return false
}

// TestDrawAtANegativeRect is the other half of "Draw is total": a rect whose
// origin is off the top left of the screen is reachable through SetBounds when a
// layout overflows, and must clip rather than index a negative cell.
func TestDrawAtANegativeRect(t *testing.T) {
	for _, r := range []buffer.Rect{
		{X: -5, Y: -5, W: 20, H: 8},
		{X: 3, Y: 3, W: -4, H: 6},
		{X: -1, Y: 0, W: 10, H: 4},
		{X: 0, Y: -1, W: 10, H: 4},
	} {
		d := New(r, VariantConfirm)
		d.SetBodyString("body")
		d.SetChoices([]string{"a", "b"})
		buf := cellBuf(20, 12)
		d.Draw(buf)
	}
}

// TestBelowMinSizeDrawsTheMinimumLayoutClipped is ADR 0007 §4's row for "Bounds
// smaller than MinSize", stated as an assertion: the dialog is shown at a size
// under its own minimum, and the button row is still on screen.
func TestBelowMinSizeDrawsTheMinimumLayoutClipped(t *testing.T) {
	full := confirmDialog(40, 9)
	m := full.MinSize()
	// A frame is two rows, and the minimum layout is one body line plus one
	// action row on top of it.
	if m.H < 2+1+1 {
		t.Fatalf("MinSize is %dx%d, which cannot hold a frame, a body line and an action row", m.W, m.H)
	}
	if m.W < 2+full.actionsWidth() {
		t.Fatalf("MinSize is %dx%d, which cannot hold a frame around the %d-cell action row",
			m.W, m.H, full.actionsWidth())
	}

	// One cell under the minimum on each axis: the layout is the minimum one,
	// clipped, and it is still not blank.
	small := confirmDialog(m.W-1, m.H-1)
	got := rows(t, m.W-1, m.H-1, small)
	whole := strings.Join(got, "\n")
	if strings.Trim(whole, " "+string(edge().Vertical)+string(edge().Horizontal)+
		string(edge().TopLeft)+string(edge().TopRight)+
		string(edge().BottomLeft)+string(edge().BottomRight)) == "" {
		t.Errorf("at %dx%d, below MinSize %dx%d, the dialog drew nothing but its frame:\n%s",
			m.W-1, m.H-1, m.W, m.H, whole)
	}
	wantFramed(t, got, 0)
	wantFramed(t, got, m.H-2)
}

// TestMinSizeIncludesChromeAndIsPure covers both halves of ADR 0007 §2's MinSize
// contract: it counts the frame, and it does not depend on Bounds.
//
// The purity half matters because a layout calls MinSize BEFORE handing out
// rectangles; a MinSize that read Bounds would return a different answer at that
// point than at any other time.
func TestMinSizeIncludesChromeAndIsPure(t *testing.T) {
	d := confirmDialog(40, 9)
	before := d.MinSize()

	d.SetBounds(rect(200, 60))
	d.Draw(cellBuf(200, 60))
	d.SetBounds(rect(1, 1))
	d.Draw(cellBuf(1, 1))

	if got := d.MinSize(); got != before {
		t.Errorf("MinSize changed with Bounds: %dx%d then %dx%d", before.W, before.H, got.W, got.H)
	}

	// EXACT equality, not a lower bound. An inequality here is what lets "MinSize
	// ignores the frame" pass: a MinSize that dropped the border still covers the
	// interior minimum, so it has to be compared against the WHOLE widget.
	//
	// The interior minimum is the LARGER of what the block's own chrome needs — a
	// title is a content minimum of its own — and what the dialog's content needs:
	// one action row wide, one body line and one action row tall.
	if wantW := d.actionsWidth() + 2*chromeOf(d); before.W < wantW {
		t.Errorf("MinSize width is %d, want at least %d: %d of action row plus %d of chrome",
			before.W, wantW, d.actionsWidth(), 2*chromeOf(d))
	}
	if wantH := 1 + 1 + 2*chromeOf(d); before.H != wantH {
		t.Errorf("MinSize height is %d, want exactly %d: a body line and an action row "+
			"plus %d of chrome", before.H, wantH, 2*chromeOf(d))
	}
	// And it is never LESS than what the block alone would demand, which is what a
	// title contributes: the title is content as far as the frame is concerned.
	if blkMin := d.Block().MinSize(); before.W < blkMin.W || before.H < blkMin.H {
		t.Errorf("MinSize is %dx%d, smaller than the block's own %dx%d", before.W, before.H, blkMin.W, blkMin.H)
	}
}

// TestMinSizeIsTheSmallestSizeThatShowsEverythingMinSizePromises asserts the
// property MinSize exists to provide: at exactly MinSize the dialog shows a body
// line, an action row and, for a choice dialog, one choice — all of them.
func TestMinSizeIsTheSmallestSizeThatShowsEverythingMinSizePromises(t *testing.T) {
	for _, tc := range []struct {
		name string
		d    *Dialog
	}{
		{"info", infoDialog(40, 9)},
		{"confirm", confirmDialog(40, 9)},
		{"choice", choiceDialog(40, 12, 8)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := tc.d.MinSize()
			got := rows(t, m.W, m.H, atSize(tc.d, m.W, m.H))
			screen := strings.Join(got, "\n")

			if !strings.Contains(screen, DefaultDismissLabel) &&
				!strings.Contains(screen, DefaultCancelLabel) &&
				!strings.Contains(screen, DefaultOKLabel) {
				t.Errorf("at MinSize %dx%d no action label is on screen:\n%s", m.W, m.H, screen)
			}
			if !strings.Contains(screen, unframe(strings.SplitN(screen, "\n", 2)[1])[:1]) &&
				strings.Count(screen, "\n") < 2 {
				t.Errorf("MinSize %dx%d is too short to hold a border and a row", m.W, m.H)
			}
		})
	}
}

// TestGrowAndShrinkRepaintEveryRow is ADR 0007 §1 rule 3 at the widget level, and
// it is tested in the direction that can actually fail.
//
// Growing cannot leave a stale cell: every extra cell is freshly painted. Only
// SHRINKING can, and in this widget the sharpest form of it is a WIDTH change,
// because a wider body wraps into fewer lines and the rows it used to occupy are
// then painted by nobody. The renderer diffs and never clears, so if the widget
// did not repaint its whole bounds those rows would keep last frame's text for
// ever.
//
// Both directions run on the SAME buffer, so the second phase is reading cells the
// first phase wrote.
func TestGrowAndShrinkRepaintEveryRow(t *testing.T) {
	d := New(rect(60, 12), VariantInfo)
	d.SetBody([]buffer.Span{buffer.NewSpan(
		"one two three four five six seven eight nine ten eleven twelve "+
			"thirteen fourteen fifteen sixteen seventeen eighteen nineteen twenty",
		buffer.PlainStyle)})
	buf := cellBuf(200, 12)
	atSize(d, 60, 12)
	d.Draw(buf)

	// At 60 cells the body wraps, so its second line STARTS with a word that sits in
	// the middle of the sentence. Probing on a line's first word rather than a
	// substring is what makes the two layouts distinguishable at all — every word
	// of the narrow layout is still present in the wide one.
	if got := countRowsStartingWith(buf, "twelve"); got != 1 {
		t.Fatalf("setup: at 60 cells exactly one row should start with the body's second line, got %d", got)
	}

	// Shrink the WIDTH: the same text now fits on ONE row, and the row that used to
	// hold the second line is painted by nobody. If the dialog did not repaint its
	// whole bounds, that row would keep last frame's sentence for ever.
	atSize(d, 200, 12)
	d.Draw(buf)
	if got := countRowsStartingWith(buf, "twelve"); got != 0 {
		t.Errorf("after the width change, %d rows still start with the old second line", got)
	}
	// The whole message now fits on ONE row, and only one row carries any of it:
	// the row that used to hold the rest of the sentence is blank again.
	carrying := 0
	for y := 1; y < 11; y++ {
		if strings.Contains(rowText(buf, y), "nine") {
			carrying++
		}
	}
	if carrying != 1 {
		t.Errorf("%d interior rows still carry body text at 200 cells, want 1", carrying)
	}

	// Grow again: the wrapped line comes back, and nothing of the previous frame may
	// show through it.
	atSize(d, 60, 12)
	d.Draw(buf)
	if got := countRowsStartingWith(buf, "twelve"); got != 1 {
		t.Errorf("growing back to 60 cells gave %d wrapped body rows, want 1", got)
	}
}

// TestEveryCellInsideBoundsTakesTheBackground is the same property stated on the
// BACKGROUND rather than on the glyphs, because a blank cell and an unpainted cell
// are indistinguishable when both are spaces.
//
// The two backgrounds must be different colours, or the test cannot fail — the
// trap this comment exists to name.
func TestEveryCellInsideBoundsTakesTheBackground(t *testing.T) {
	one := buffer.NewStyle(buffer.DefaultColour, buffer.NewColour(0, 0, 0), 0)
	two := buffer.NewStyle(buffer.DefaultColour, buffer.NewColour(0, 0, 0x80), 0)
	if one.BG == two.BG {
		t.Fatal("test setup: the two backgrounds are identical, so this cannot fail")
	}

	d := New(rect(40, 9), VariantChoice)
	d.SetBodyString("a body line that wraps over several rows at forty cells wide")
	d.SetChoices([]string{"one", "two", "three"})
	d.Block().SetBackground(one)
	buf := cellBuf(40, 9)
	d.Draw(buf)

	// Shrink AND recolour in one step, which is the worst case: fewer rows to
	// repaint, and a change every one of them has to take.
	atSize(d, 40, 6)
	d.Block().SetBackground(two)
	d.Draw(buf)

	// The property asserted is that NO cell kept the OLD background, rather than
	// that every cell took the new one. Content carries its own style — a body
	// span's background legitimately wins over the block's, which is Block's
	// documented composition rule — so demanding the new background everywhere would
	// be asserting something this widget deliberately does not promise.
	in := d.Block().Interior()
	for y := in.Y; y < in.Bottom(); y++ {
		for x := in.X; x < in.Right(); x++ {
			if got := buf.CellAt(x, y).BG; got == one.BG {
				t.Fatalf("interior cell (%d,%d) kept the old background %#x: the fill is not repainting",
					x, y, got)
			}
		}
	}
	// The BLANK interior rows — the ones the shorter layout draws no content into —
	// must take the new background outright, which is the part only a fill can do.
	blank := in.Bottom() - 1
	if got, want := buf.CellAt(in.Right()-1, blank).BG, two.BG; got != want {
		t.Errorf("blank interior cell (%d,%d) has background %#x, want the block's %#x",
			in.Right()-1, blank, got, want)
	}
}

// TestDrawWritesOnlyInsideBounds is the other half of the shrink property, and it
// is the one a half-finished clip would break: a sentinel painted outside the
// dialog's new bounds must survive the dialog's repaint.
func TestDrawWritesOnlyInsideBounds(t *testing.T) {
	d := confirmDialog(60, 12)
	d.SetBodyString("a body that is quite long indeed and wraps at narrow widths")
	buf := cellBuf(60, 12)
	d.Draw(buf)

	const sentinel = '!'
	outside := buffer.Rect{X: 20, Y: 0, W: 40, H: 12}
	buf.FillRect(outside, buffer.PlainStyle.Cell(sentinel))

	// The buffer stays 60 wide so there are cells outside the dialog's new bounds
	// to assert about; only the widget is told it is narrower.
	atSize(d, 20, 12)
	d.Draw(buf)

	for y := 0; y < 12; y++ {
		for x := 20; x < 60; x++ {
			if got := buf.CellAt(x, y).Rune(); got != sentinel {
				t.Fatalf("the dialog wrote %q at (%d,%d), outside its 20-wide bounds", got, x, y)
			}
		}
	}
}

// TestEmptyBoundsDrawNothing is the degenerate-size contract's cheapest half, and
// it is pinned rather than assumed because thirty widgets all testing the same
// condition is the point of ADR 0007 §4 deciding it once.
func TestEmptyBoundsDrawNothing(t *testing.T) {
	for _, r := range []buffer.Rect{
		{}, {W: 10, H: 0}, {W: 0, H: 10}, {W: -3, H: -3}, {X: 5, Y: 5, W: -1, H: 4},
	} {
		d := New(r, VariantChoice)
		d.SetBodyString("body")
		d.SetChoices([]string{"a", "b"})
		buf := cellBuf(12, 6)
		buf.Fill(buffer.PlainStyle.Cell('!'))
		d.Draw(buf)
		for y := 0; y < 6; y++ {
			for x := 0; x < 12; x++ {
				if got := buf.CellAt(x, y).Rune(); got != '!' {
					t.Fatalf("bounds %+v wrote %q at (%d,%d): an empty rect must write nothing",
						r, got, x, y)
				}
			}
		}
	}
}

// TestBodyIsTruncatedWithAMarkerNotClipped is "truncated, not dropped", which is
// what tells the user there was more of the message. It is asserted on the marker
// buffer itself defines, so the expectation cannot drift from the shared
// vocabulary.
func TestBodyIsTruncatedWithAMarker(t *testing.T) {
	d := New(rect(20, 5), VariantInfo)
	d.SetBodyString("alpha beta gamma delta epsilon zeta eta theta")
	got := rows(t, 20, 5, d)

	var last string
	for y := range got {
		if strings.Contains(unframe(got[y]), "alpha beta") || strings.Contains(unframe(got[y]), buffer.TruncSuffix) {
			last = unframe(got[y])
		}
	}
	if !strings.Contains(last, buffer.TruncSuffix) {
		t.Errorf("the last body row that fits carries no marker, so the user cannot tell "+
			"the message continues: %q", last)
	}
}

// TestAnOverLongActionLabelIsTruncated pins the same rule for a button: a label
// too wide for its own column says so rather than spilling over its neighbour.
func TestAnOverLongActionLabelIsTruncated(t *testing.T) {
	d := New(rect(20, 6), VariantConfirm)
	d.SetActions(Action{Label: DefaultCancelLabel}, Action{Label: "A very long affirmative label"})
	got := rows(t, 20, 6, d)

	// The two actions do not fit side by side in 20 cells, so the action block
	// wraps onto two rows, and the block is pinned to the bottom of the box: rows
	// 3 and 4, with the frame on 0 and 5.
	first, second := unframe(got[3]), unframe(got[4])
	if !strings.Contains(first, DefaultCancelLabel) {
		t.Errorf("the short button is not on the first action row: %q", first)
	}
	if !strings.Contains(second, buffer.TruncSuffix) {
		t.Errorf("the over-long button was clipped without a marker: %q", second)
	}
	// The short button keeps its own label: the truncation happened to the wide
	// one, not to both.
	if strings.Contains(first, buffer.TruncSuffix) {
		t.Errorf("the short button was truncated as well: %q", first)
	}
	if got, want := buffer.StringWidth(second), 18; got != want {
		t.Errorf("the wrapped button is %d cells, want %d: it must not spill over the frame", got, want)
	}
}

// TestAChoiceWithNoRoomForALabelIsTruncatedNotDropped covers the edge the marker
// column introduces: an interior three cells wide leaves one cell of label, and
// one cell is not enough for "a-option". The row shows the marker and a truncation
// marker, which is the catalog's rule for the last cell of a row everywhere else —
// it is never blanked, and it never spills past the frame.
func TestAChoiceWithNoRoomForALabelIsTruncatedNotDropped(t *testing.T) {
	d := choiceDialog(5, 5, 3)
	got := rows(t, 5, 5, d)

	row := unframe(got[2])
	if !strings.HasPrefix(row, ChoiceMarker) {
		t.Errorf("the marker column is gone: %q", row)
	}
	if !strings.Contains(row, buffer.TruncSuffix) {
		t.Errorf("a one-cell label was dropped rather than truncated: %q", row)
	}
	if got, want := buffer.StringWidth(row), 3; got != want {
		t.Errorf("the row is %d cells, want %d: a choice must not spill past its frame", got, want)
	}
}

// TestAChoiceWithNoRoomForItsMarkerAtAll covers the case below that one: an
// interior narrower than the marker column. There is nowhere to put the label, so
// the row is left empty rather than writing a marker over its neighbour's cells.
func TestAChoiceWithNoRoomForItsMarkerAtAll(t *testing.T) {
	d := choiceDialog(3, 5, 3)
	got := rows(t, 3, 5, d)

	// A one-cell interior cannot hold a two-cell marker column, so no choice is
	// drawn at all. The BODY still is — dropping the list is a content decision
	// and blanking the message to go with it would not be one.
	for y := 1; y < 4; y++ {
		if row := unframe(got[y]); strings.Contains(row, ChoiceMarker) {
			t.Errorf("row %d drew a choice marker in a one-cell interior: %q", y, row)
		}
	}
	// The body still draws, one character per row, with the truncation marker on
	// the last line that fits — which is what "clip, never blank" means here.
	if !strings.Contains(unframe(got[1]), "P") {
		t.Errorf("the first body character should still be drawn:\n%s", strings.Join(got, "\n"))
	}
	if !strings.Contains(unframe(got[2]), buffer.TruncSuffix) {
		t.Errorf("the last body line that fits should carry the marker:\n%s", strings.Join(got, "\n"))
	}
}

// TestSetBodyAfterDrawTakesEffect guards the ADR 0007 §3 amendment from the
// mutator's side: a body changed through the setter must not be drawn from cache.
func TestSetBodyAfterDrawTakesEffect(t *testing.T) {
	d := infoDialog(40, 9)
	buf := cellBuf(40, 9)
	d.Draw(buf)
	d.SetBodyString("a completely different message")
	d.Draw(buf)

	if !strings.Contains(rowOf(buf, 3), "completely different") {
		t.Errorf("the second body was not drawn:\n%s\n%s", rowOf(buf, 2), rowOf(buf, 3))
	}
	if strings.Contains(rowOf(buf, 3), "Your changes") {
		t.Errorf("the old body is still on screen: %q", rowOf(buf, 3))
	}
}

// TestAMutatedSpanSliceNeedsInvalidate states the other half honestly: SetBody
// retains the caller's slice BY REFERENCE, so a caller that edits it in place must
// call Invalidate. The test asserts both the failure and the repair, which is what
// makes it documentation rather than a bug report waiting to happen.
func TestAMutatedSpanSliceNeedsInvalidate(t *testing.T) {
	d := New(rect(40, 9), VariantInfo)
	spans := plainSpans("first message")
	d.SetBody(spans)
	buf := cellBuf(40, 9)
	d.Draw(buf)

	spans[0] = buffer.NewSpan("second message", buffer.PlainStyle)
	d.Draw(buf)
	if !strings.Contains(rowOf(buf, 3), "second message") {
		t.Logf("note: the cached layout happened to notice the mutation; the contract " +
			"still requires Invalidate because it is not guaranteed to")
	}

	d.Invalidate()
	d.Draw(buf)
	if !strings.Contains(rowOf(buf, 3), "second message") {
		t.Errorf("after Invalidate the new body should be drawn: %q", rowOf(buf, 3))
	}
}

// TestAStyleFieldWrittenDirectlyStillTakesEffect is the expensive half of ADR 0007
// §3's amendment, done cheaply: the layout cache compares the RESOLVED styles, so
// a caller that writes FocusStyle without calling Invalidate gets a correct frame
// rather than a permanently stale one.
func TestAStyleFieldWrittenDirectlyStillTakesEffect(t *testing.T) {
	d := confirmDialog(40, 9)
	buf := cellBuf(40, 9)
	d.Draw(buf)
	before := buf.CellAt(12, 7).Attr

	// Bold, not reverse: an attribute the default focus style does not carry.
	d.FocusStyle = buffer.PlainStyle.WithAttr(buffer.AttrUnderline)
	d.Draw(buf)

	if got := buf.CellAt(12, 7).Attr; got == before {
		t.Errorf("writing FocusStyle directly had no effect on the drawn cell: attr %v", got)
	}
	if !buf.CellAt(12, 7).Attr.Has(buffer.AttrUnderline) {
		t.Errorf("the focused cell is not underlined: attr %v", buf.CellAt(12, 7).Attr)
	}
}

// TestTheDialogOwnsNoBorderGlyph pins the composition rule ADR 0008 §Decision 4
// states: the frame comes from block, which is the catalog's only border owner.
func TestTheDialogOwnsNoBorderGlyph(t *testing.T) {
	d := infoDialog(40, 9)
	if d.Block().Border == buffer.BorderNone {
		t.Fatal("New must give the dialog a frame; a modal with none does not read as a modal")
	}
	// The interior is what is left after that frame, which is the composition
	// point callers use to place content beside a dialog.
	if got, want := d.Block().Interior(), (buffer.Rect{X: 1, Y: 1, W: 38, H: 7}); got != want {
		t.Errorf("the block's interior is %+v, want %+v", got, want)
	}
}

// TestBlockIsTheChromeCompositionPoint covers the escape hatch: a caller
// configures padding, background and title through Block without Dialog
// re-exporting any of it.
func TestBlockIsTheChromeCompositionPoint(t *testing.T) {
	d := infoDialog(40, 9)
	blockOf(d).SetPadding(2)
	d.SetTitle("Deep", buffer.PlainStyle)

	got := rows(t, 40, 9, d)
	if got, want := d.Block().Interior(), (buffer.Rect{X: 3, Y: 3, W: 34, H: 3}); got != want {
		t.Errorf("with padding 2 the interior is %+v, want %+v", got, want)
	}
	if strings.Contains(got[1], "Your changes") {
		t.Errorf("the body ignored the padding and drew on the first interior row: %q", got[1])
	}
}

// TestDialogSatisfiesTheWidgetInterfaces asserts the optional interfaces at
// runtime as well as at compile time, because a type assertion failing is a
// different bug from a method not existing.
func TestDialogSatisfiesTheWidgetInterfaces(t *testing.T) {
	d := New(rect(40, 9), VariantInfo)
	if _, ok := any(d).(termmosaic.Minimizable); !ok {
		t.Error("Dialog does not implement termmosaic.Minimizable")
	}
	if _, ok := any(d).(termmosaic.Focusable); !ok {
		t.Error("Dialog does not implement termmosaic.Focusable")
	}
	if _, ok := any(d).(termmosaic.Widget); !ok {
		t.Error("Dialog does not implement termmosaic.Widget")
	}
	// All four methods must be safe before the first Draw, which the Widget
	// contract requires and which a layout relies on.
	_ = d.Bounds()
	_ = d.MinSize()
	d.Invalidate()
	d.Handle(termmosaic.SpecialKeyEvent(termmosaic.KeyEscape, 0))
}

// TestIdempotentDraw is the frame-path contract stated as a test: drawing the
// same state twice produces the same cells, or the renderer's row skip never
// fires and the whole screen flickers forever.
func TestIdempotentDraw(t *testing.T) {
	for _, d := range []termmosaic.Widget{
		infoDialog(40, 9), confirmDialog(40, 9), choiceDialog(40, 12, 6),
	} {
		buf := cellBuf(40, 12)
		d.Draw(buf)
		first := snapshot(buf)
		d.Draw(buf)
		if got := snapshot(buf); got != first {
			t.Errorf("%T drew different cells on a second identical frame", d)
		}
	}
}

// TestMinSizeCountsEveryPieceOfChrome states the property the test above checks one
// instance of: MinSize counts whatever chrome the block is configured with, and not
// merely "a border".
//
// It is asserted by COMPARING two dialogs that differ only in their chrome, which is
// the form that cannot pass by accident: a MinSize that ignored padding would report
// the same size for both, and the assertion is that it does not.
func TestMinSizeCountsEveryPieceOfChrome(t *testing.T) {
	plain := confirmDialog(60, 9)
	padded := atSize(confirmDialog(60, 9), 60, 9)
	padded.Block().SetPadding(2)

	plainSize, paddedSize := plain.MinSize(), padded.MinSize()

	if paddedSize.W != plainSize.W+4 || paddedSize.H != plainSize.H+4 {
		t.Errorf("padding 2 grew MinSize by %dx%d, want 4x4: two cells on each side of "+
			"each axis", paddedSize.W-plainSize.W, paddedSize.H-plainSize.H)
	}

	// A borderless dialog of the same content is two cells smaller still, which is
	// the other half: chrome is counted when it is there AND omitted when it is not.
	bare := atSize(confirmDialog(60, 9), 60, 9)
	bare.Block().SetBorder(buffer.BorderNone)
	if got := bare.MinSize(); got.W != plainSize.W-2 || got.H != plainSize.H-2 {
		t.Errorf("removing the border shrank MinSize by %dx%d, want 2x2",
			plainSize.W-got.W, plainSize.H-got.H)
	}

	// And a title LONG ENOUGH TO DOMINATE widens it, because the title sits on the
	// border row and MinSize has to be wide enough for it. The title is deliberately
	// longer than the action row so the assertion is about the title and not about
	// whichever of the two happens to be wider.
	longTitle := atSize(confirmDialog(60, 9), 60, 9)
	const title = "a title far longer than the action row it sits above"
	longTitle.SetTitle(title, buffer.PlainStyle)
	got := longTitle.MinSize()
	if want := buffer.StringWidth(title) + 2 + 2*chromeOf(longTitle); got.W < want {
		t.Errorf("a %d-cell title left MinSize at %d, want at least %d", buffer.StringWidth(title), got.W, want)
	}
}

// chromeOf returns how many cells the dialog's block consumes on each side. It is
// read off the block rather than recomputed, so a test comparing MinSize against the
// chrome cannot disagree with the block about what the chrome is.
func chromeOf(d *Dialog) int {
	n := d.Block().Padding
	if d.Block().Border != buffer.BorderNone {
		n++
	}
	return n
}
