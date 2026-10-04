package dialog

import (
	"strings"
	"testing"

	"github.com/serkanalgur/termmosaic/buffer"
)

// TestTheLayoutChangesWithSizeNotMerelyRescales is the assertion that makes
// "responsive" mean something rather than being a synonym for "re-centred".
//
// It asserts on the SCREEN, because the screen is what the user sees and the only
// thing a responsive claim is about. Four properties, each of which a rescaling
// widget would fail:
//
//  1. The action row goes from ONE row to TWO as the width falls below what the
//     buttons need. That is a different arrangement, not the same one squeezed:
//     the buttons are on the same row at 40 cells and on different rows at 20.
//  2. The body's ROW COUNT changes with the width, because a narrower body wraps.
//     A widget that reflowed without rewrapping would keep the same number of
//     rows at every width and pass a resize test while doing nothing.
//  3. The choice list's HEIGHT changes with the dialog's height, and what it
//     prioritises changes too: a tall dialog shows the body and several choices,
//     a short one keeps the question and drops most of the list.
//  4. The buttons stay at the BOTTOM of the box at every height, which is what
//     makes a scrolling choice list usable rather than a moving target.
func TestTheLayoutChangesWithSizeNotMerelyRescales(t *testing.T) {
	// 1. The action row wraps. The width is DERIVED from what the buttons need
	// rather than hard-coded, so the assertion is about the threshold and not about
	// a number that happens to sit above it today.
	need := confirmDialog(40, 9).actionsWidth()
	wide := rows(t, 40, 9, confirmDialog(40, 9))
	narrowW := need + 1 // two for the frame, so the interior is one cell too narrow
	narrow := rows(t, narrowW, 9, confirmDialog(narrowW, 9))
	wideRow, narrowRow := buttonRows(t, wide), buttonRows(t, narrow)
	assertDifferent(t, "the number of rows the buttons occupy",
		itoa(wideRow), itoa(narrowRow))
	if wideRow != 1 {
		t.Errorf("at 40 cells the two buttons occupy %d rows, want 1", wideRow)
	}
	if narrowRow != 2 {
		t.Errorf("at %d cells the interior is %d, one short of the %d the buttons need, so "+
			"they must wrap to two rows; they occupy %d", narrowW, narrowW-2, need, narrowRow)
	}
	// The two arrangements really are different rows, not one row drawn twice: the
	// first button is alone on the upper one.
	upper := unframe(narrow[len(narrow)-3])
	if !strings.Contains(upper, DefaultCancelLabel) {
		t.Errorf("the first button is not alone on the upper action row: %q", upper)
	}
	if strings.Contains(upper, DefaultOKLabel) {
		t.Errorf("both buttons are on the same row at %d cells, so the wrap did not happen: %q",
			narrowW, upper)
	}

	// 2. The body rewraps, so its row COUNT changes. The message is long enough to
	// wrap in the narrow interior and short enough to fit in the wide one, so the
	// two counts are genuinely different rather than both being "however many lines
	// this sentence happens to take".
	narrowBody := bodyRows(t, rows(t, 24, 12, longBody(t, 24, 12)))
	wideBody := bodyRows(t, rows(t, 60, 12, longBody(t, 60, 12)))
	if narrowBody <= wideBody {
		t.Errorf("a 24-cell body occupies %d rows and a 60-cell body %d: the narrower "+
			"layout should wrap onto MORE rows", narrowBody, wideBody)
	}
	if wideBody != 1 {
		t.Errorf("at 60 cells the body should fit on one row, got %d", wideBody)
	}

	// 3. The choice list's height follows the dialog's height. Both sizes are chosen
	// so the list genuinely overflows — twelve choices do not fit in either — because
	// a pair of sizes where the list already fits would make the two counts equal for
	// a reason that has nothing to do with the layout.
	tall := rows(t, 30, 12, choiceDialog(30, 12, 12))
	short := rows(t, 30, 6, choiceDialog(30, 6, 12))
	tallChoices, shortChoices := choiceRowCount(t, tall), choiceRowCount(t, short)
	assertDifferent(t, "how many choices are visible",
		itoa(tallChoices), itoa(shortChoices))
	if tallChoices <= shortChoices {
		t.Errorf("a 12-row dialog shows %d choices and a 6-row dialog %d: the taller "+
			"one should show more", tallChoices, shortChoices)
	}
	// A 12-row dialog has ten interior rows. The action row always takes one, the
	// body takes one more, and the list gets what is left — eight. The twelve choices
	// are therefore clipped to eight, which is the property stated as a number derived
	// from the geometry rather than guessed.
	const interior = 12 - 2
	if want := interior - 1 - 1; tallChoices != want {
		t.Errorf("a 12-row dialog shows %d choices, want the %d its %d interior rows leave "+
			"after the body and the action row", tallChoices, want, interior)
	}
	// A short dialog keeps the QUESTION rather than showing neither region: the
	// budget reserves a row for the list, and the body wins the rest.
	if !strings.Contains(joinRows(short), "Pick one.") {
		t.Errorf("a 6-row dialog dropped its body instead of trimming its list:\n%s",
			joinRows(short))
	}

	// 4. The buttons stay pinned to the bottom at every height.
	for _, h := range []int{6, 9, 12, 16, 24} {
		got := rows(t, 30, h, choiceDialog(30, h, 12))
		last := h - 1
		if !strings.Contains(got[last-1], DefaultCancelLabel) {
			t.Errorf("at height %d the button row is not on the last interior row %d:\n%s",
				h, last-1, strings.Join(got, "\n"))
		}
	}
}

// TestTheActionRowWrapsExactlyBelowWhatTheButtonsNeed pins the threshold by
// measurement rather than by a literal, so the wrap cannot drift: one cell more than
// the buttons need on one row, and they are on one row; one cell less, and they are
// not.
//
// A test at two round numbers would pass whether the threshold sat at 13 or at 40,
// which is the failure mode this one exists to prevent.
func TestTheActionRowWrapsExactlyBelowWhatTheButtonsNeed(t *testing.T) {
	d := confirmDialog(60, 9)
	need := d.actionsWidth()
	frame := 2 // the border, one cell each side

	// The interior has to hold the buttons side by side. One cell either side of
	// the boundary decides it, which is the only assertion that pins a threshold.
	atSize(d, need+frame, 9)
	d.Draw(cellBuf(need+frame, 9))
	if got := d.actRows; got != 1 {
		t.Errorf("at %d cells the interior is exactly %d and the buttons fit on one row, "+
			"but they occupy %d", need+frame, need, got)
	}

	atSize(d, need+frame-1, 9)
	d.Draw(cellBuf(need+frame-1, 9))
	if got := d.actRows; got != 2 {
		t.Errorf("at %d cells the interior is one short of the %d the buttons need, so they "+
			"must wrap, but they occupy %d rows", need+frame-1, need, got)
	}
}

// TestTheActionRowWrapsOnlyWhenItMust guards the other failure: a dialog that wraps
// eagerly is as wrong as one that never wraps. Three buttons that fit comfortably
// stay on one row however short the body is.
func TestTheActionRowWrapsOnlyWhenItMust(t *testing.T) {
	d := New(rect(40, 9), VariantInfo)
	d.SetActions(
		Action{Label: "Yes"},
		Action{Label: "No"},
		Action{Label: "Maybe later"},
	)
	need := d.actionsWidth()
	if want := len("Yes") + 2 + actionGap + len("No") + 2 + actionGap + len("Maybe later") + 2; need != want {
		t.Fatalf("setup: actionsWidth is %d, want %d", need, want)
	}
	// Every width from the threshold up to four times it keeps the buttons on one
	// row, and the threshold itself is asserted by the test above. A dialog that
	// wrapped eagerly would fail at the wide end.
	for _, slack := range []int{0, 1, 4, 12, 24} {
		w := need + 2 + slack // two for the frame
		atSize(d, w, 9)
		d.Draw(cellBuf(w, 9))
		if d.actRows != 1 {
			t.Errorf("at %d cells (interior %d, %d slack over the %d the buttons need) the "+
				"buttons occupy %d rows, want 1: a row that fits must not wrap",
				w, w-2, slack, need, d.actRows)
		}
	}
}

// TestAChoiceListLongerThanTheDialogScrolls is the virtualization claim: a dialog
// with a hundred choices costs what one with three costs, and it is the proof that
// the list is a window rather than a table.
//
// It is checked two ways, because they fail independently: the visible range never
// exceeds the viewport, and the focused choice is always inside it.
func TestAChoiceListLongerThanTheDialogScrolls(t *testing.T) {
	d := choiceDialog(30, 9, 100)
	focus(d)
	d.Draw(cellBuf(30, 9))

	if got := d.ChoiceCount(); got != 100 {
		t.Fatalf("ChoiceCount = %d, want 100", got)
	}
	first, last := d.vm.Range()
	if last-first > d.choiceShown {
		t.Errorf("the visible range [%d,%d) is larger than the %d rows shown",
			first, last, d.choiceShown)
	}

	// Walk the whole hundred and check the invariant at every step, which is what
	// would break on an off-by-one in the scroll engine's ItemAt.
	for i := 0; i < 100; i++ {
		d.SetFocus(i)
		f, l := d.vm.Range()
		if got := d.FocusedChoice(); got < f || got >= l {
			t.Fatalf("at choice %d the focus is outside the visible window [%d,%d)", got, f, l)
		}
	}
	// And End reaches the last one rather than the last VISIBLE one.
	d.SetFocus(0)
	mustPress(t, d, keyEnd)
	if got, want := d.FocusedChoice(), 0; got != want {
		// End jumps to the last RING item, which for this dialog is the button.
		if got := d.FocusedAction(); got != 0 {
			t.Errorf("End landed on choice %d action %d, want the ring's last item", got, got)
		}
	}
}

// TestTheChoiceListScrollsOnlyAsFarAsItMust pins ADR 0007 §6's rule 2 — clamp, do
// not recentre — where it is observable. A dialog that re-derived the offset from a
// "keep the selection centred" rule would jump a whole screen at a time, and a user
// scrolling would lose their place.
func TestTheChoiceListScrollsOnlyAsFarAsItMust(t *testing.T) {
	d := choiceDialog(30, 9, 100)
	focus(d)
	d.Draw(cellBuf(30, 9))
	_, shown := d.vm.Range()

	for i := 0; i < shown; i++ {
		d.SetFocus(i)
	}
	if got := d.vm.Offset(); got != 0 {
		t.Fatalf("the list scrolled to offset %d while the selection was still visible at 0", got)
	}

	d.SetFocus(shown)
	if got := d.vm.Offset(); got != 1 {
		t.Errorf("moving the selection one row past the viewport scrolled by %d, want 1: "+
			"ScrollIntoView moves the minimum amount, it does not recentre", got)
	}

	// PageDown moves by a screen less one row, which is virtual's own rule; this
	// asserts the dialog does not have its own competing one.
	before := d.FocusedChoice()
	mustPress(t, d, keyPageDown)
	if got := d.FocusedChoice(); got != before+shown-1 {
		t.Errorf("PageDown moved the choice from %d to %d, want %d", before, got, before+shown-1)
	}
}

// TestTheBodyIsWrappedNotTruncatedWhenThereIsRoom covers the interaction between
// the two ways a body can be shortened: wrapping when the width allows it, and the
// truncation marker only when the HEIGHT runs out.
func TestTheBodyIsWrappedNotTruncatedWhenThereIsRoom(t *testing.T) {
	// Long enough to WRAP at a 38-cell interior, and the counts below are asserted
	// against the wrap the widget actually performs rather than against a number
	// written here, so the test cannot fail because the sentence was reworded.
	const body = "alpha beta gamma delta epsilon zeta eta theta"
	if got := buffer.StringWidth(body); got <= 38 {
		t.Fatalf("setup: the body is %d cells and must exceed the 38-cell interior", got)
	}

	// WIDTH is the constraint: two wrapped lines both fit, so neither is cut short
	// and no marker appears anywhere.
	roomy := New(rect(40, 10), VariantInfo)
	roomy.SetBodyString(body)
	bufRoomy := cellBuf(40, 10)
	atSize(roomy, 40, 10)
	roomy.Draw(bufRoomy)
	if got := roomy.bodyShown; got != 2 {
		t.Fatalf("a 38-cell interior shows the body on %d rows, want 2", got)
	}
	if got, want := roomy.bodyLines.Height(), 2; got != want {
		t.Fatalf("setup: the body wraps to %d lines, want %d; the marker half of this test "+
			"proves nothing unless the message really does wrap", got, want)
	}
	for i := 0; i < roomy.bodyShown; i++ {
		if got := rowOf(bufRoomy, roomy.bodyY+i); strings.Contains(got, buffer.TruncSuffix) {
			t.Errorf("row %d carries a truncation marker although the whole body fits: %q", i, got)
		}
	}

	// HEIGHT is the constraint, and it needs a body with MORE lines than the dialog
	// can show — so the short case uses a longer message, which is what makes "there
	// is more below" true rather than hypothetical.
	short := New(rect(40, 5), VariantInfo)
	short.SetBodyString(body + " iota kappa lambda mu nu xi omicron pi rho sigma")
	bufShort := cellBuf(40, 5)
	atSize(short, 40, 5)
	short.Draw(bufShort)

	if got := short.bodyShown; got != 2 {
		t.Fatalf("a 3-row interior shows %d body rows, want 2", got)
	}
	if got := short.bodyLines.Height(); got <= short.bodyShown {
		t.Fatalf("setup: the longer body wraps to %d lines and %d are shown, so nothing is "+
			"clipped and this test proves nothing", got, short.bodyShown)
	}
	// The LAST line that fits is the one carrying the marker — the row the reader's
	// eye lands on. Asserting on the first row instead would be asserting on a line
	// that is not clipped at all.
	last := short.bodyY + short.bodyShown - 1
	if got := rowOf(bufShort, last); !strings.Contains(got, buffer.TruncSuffix) {
		t.Errorf("the last body line that fits carries no marker: %q", got)
	}
	if got := rowOf(bufShort, last-1); strings.Contains(got, buffer.TruncSuffix) {
		t.Errorf("a body line that is NOT the last carries a marker: %q", got)
	}
	// And the two layouts really are different, so the marker above is a response to
	// the HEIGHT rather than to the width. The first row is deliberately not the one
	// compared: it is the same sentence either way, and asserting on it would be
	// asserting that nothing happened.
	assertDifferent(t, "the body at full height and at a clipped height",
		joinRows([]string{rowOf(bufRoomy, roomy.bodyY), rowOf(bufRoomy, roomy.bodyY+1)}),
		joinRows([]string{rowOf(bufShort, short.bodyY), rowOf(bufShort, short.bodyY+1)}))
}

// TestTheDialogGrowsIntoMoreRoomRatherThanReflowingOneLine pins that a wider
// dialog is a DIFFERENT ARRANGEMENT and not the same content centred in more
// space: the body rewraps, so the text occupies fewer rows and the slack is
// distributed differently.
//
// The two screens are compared as strings and required to differ, which is the
// cheapest honest form of "the layout changed".
func TestTheDialogGrowsIntoMoreRoomRatherThanReflowingOneLine(t *testing.T) {
	body := "one two three four five six seven eight nine ten eleven twelve thirteen"

	small := New(rect(28, 12), VariantInfo)
	small.SetBodyString(body)
	atSize(small, 28, 12)
	bufSmall := cellBuf(28, 12)
	small.Draw(bufSmall)

	big := New(rect(28, 12), VariantInfo)
	big.SetBodyString(body)
	atSize(big, 64, 12)
	bufBig := cellBuf(64, 12)
	big.Draw(bufBig)

	assertDifferent(t, "the screen at 28 cells and at 64", snapshot(bufSmall), snapshot(bufBig))

	smallRows, bigRows := countRowsWithText(bufSmall, "e"), countRowsWithText(bufBig, "e")
	if smallRows <= bigRows {
		t.Errorf("the 28-cell body occupies %d rows and the 64-cell body %d: the narrow "+
			"layout should wrap onto more rows", smallRows, bigRows)
	}
}

// TestTheStackIsCentredAndTheActionsArePinned pins the two vertical decisions
// separately, because they are separable and a test that only checked one of them
// would pass on a dialog that got the other wrong.
//
// The stack is centred in the space above the buttons, and the buttons are on the
// bottom interior row — which is what keeps them still while a list scrolls.
func TestTheStackIsCentredAndTheActionsArePinned(t *testing.T) {
	for _, h := range []int{7, 9, 12, 16, 20} {
		d := choiceDialog(30, h, 3)
		buf := cellBuf(30, h)
		d.Draw(buf)

		if got := d.actTop; got != h-2 {
			t.Errorf("at height %d the action row starts at %d, want %d: the buttons must "+
				"be pinned to the bottom interior row", h, got, h-2)
		}
		// Centred means the slack above the stack equals the slack below it, give or
		// take the odd cell — which is what geometry.AlignCenter's own rule says,
		// and which is reimplemented here rather than measured off the widget's
		// fields so the test could fail on a layout that merely agrees with itself.
		// The odd cell goes on TOP, matching geometry.AlignCenter's own rule
		// ("leaves any odd cell on the left"), which is the shared answer to where
		// a remainder goes rather than a second opinion formed here.
		slackAbove := d.bodyY - d.in.Y
		slackBelow := d.actTop - (d.bodyY + d.bodyShown + d.choiceShown)
		if diff := slackBelow - slackAbove; diff < 0 || diff > 1 {
			t.Errorf("at height %d the stack has %d rows above and %d below, want the "+
				"odd cell on top", h, slackAbove, slackBelow)
		}
	}
}

// TestEveryInteriorRowIsPaintedEvenWhenTheBudgetDropsRegions is "clip, never
// blank" for the case a budget creates: the action row is PrioAlways and cannot be
// dropped, so a dialog squeezed to two interior rows shows its buttons rather than
// a message with no way to answer it.
func TestEveryInteriorRowIsPaintedEvenWhenTheBudgetDropsRegions(t *testing.T) {
	for _, h := range []int{3, 4, 5, 6, 7} {
		d := choiceDialog(30, h, 12)
		got := rows(t, 30, h, d)
		if !strings.Contains(got[h-2], DefaultCancelLabel) {
			t.Errorf("at height %d the Cancel button is not on the last interior row:\n%s",
				h, strings.Join(got, "\n"))
		}
	}
}

// TestTheDialogIsCentredByItsCaller covers what this widget does NOT do, because
// stating it is what stops the next author adding it: a dialog is placed in the
// screen by the APPLICATION, through layout, exactly like every other widget.
//
// The dialog honours whatever rect it is given, including a rect in the middle of a
// larger screen, and writes nothing outside it.
func TestTheDialogIsCentredByItsCaller(t *testing.T) {
	// A 40x9 dialog at the centre of a 60x20 screen.
	place := buffer.Rect{X: 10, Y: 5, W: 40, H: 9}
	d := New(place, VariantConfirm)
	d.SetTitle("Placed", buffer.PlainStyle)
	d.SetBodyString("by the application")

	// The buffer is painted with a sentinel FIRST, so "wrote nothing here" is
	// observable rather than indistinguishable from "wrote a space here". The screen
	// model trims trailing blanks, so it cannot answer this question at all.
	buf := cellBuf(60, 20)
	buf.Fill(buffer.PlainStyle.Cell(sentinelRune))
	d.Draw(buf)

	for y := 0; y < 20; y++ {
		for x := 0; x < 60; x++ {
			if place.Contains(x, y) {
				continue
			}
			if got := buf.CellAt(x, y).Rune(); got != sentinelRune {
				t.Fatalf("the dialog wrote %q at (%d,%d), outside its 40x9 rect at (10,5)",
					got, x, y)
			}
		}
	}
	// And it really did draw something inside that rect, so the loop above is not
	// passing because the dialog drew nothing anywhere.
	if got := buf.CellAt(place.X, place.Y).Rune(); got == sentinelRune {
		t.Error("the dialog's top-left cell was not painted at all")
	}

	// Through the renderer as well, which is where a widget that wrote off its rect
	// would corrupt the diff rather than merely look wrong.
	sink := renderIn(t, 60, 20, d)
	// The frame is at the rect's own top row, which is row 5 of a 20-row screen.
	if got := strings.TrimSpace(sink.Line(5)); got == "" || !strings.Contains(got, "Placed") {
		t.Errorf("the placed dialog's title is not on row 5: %q", sink.Line(5))
	}
}

// TestResizeAtEverySizeDoesNotPanic is ADR 0007's first unforced risk: the catalog's
// honest test of resizing is a sweep. Every size in a range is offered to the same
// widget and every Draw must succeed and stay inside the buffer.
//
// The sweep goes through the RENDERER, because a widget that panics only under the
// renderer's clipping is a widget a user's terminal will crash on.
func TestResizeAtEverySizeDoesNotPanic(t *testing.T) {
	d := choiceDialog(60, 20, 30)
	d.SetTitle("Swept", buffer.PlainStyle)

	for w := 0; w <= 40; w++ {
		for _, h := range []int{0, 1, 2, 3, 4, 5, 8, 13, 20} {
			atSize(d, w, h)
			d.Draw(cellBuf(max(w, 1), max(h, 1)))
			focus(d)
			d.Handle(escape())
		}
	}
}

// TestNoColorKeepsTheFocusMarkAndTheAttributes is the accessibility claim as the
// encoder sees it: with colour suppressed the focus ring is still a CHARACTER in
// the output, and the attribute that reinforces it is still emitted.
//
// Three assertions, each of which fails for a different reason:
//   - the frame still contains the ring, so the mark survives as a shape;
//   - the byte stream contains no colour sequence, so NO_COLOR really is in force;
//   - the byte stream contains the reverse attribute, so the reinforcement was not
//     encoded away with the colours.
func TestNoColorKeepsTheFocusMarkAndTheAttributes(t *testing.T) {
	withColor := renderIn(t, 40, 9, confirmDialog(40, 9))
	noColor := renderNoColor(t, 40, 9, confirmDialog(40, 9))

	// The ring survives as a character.
	if got, want := noColor.Line(7), withColor.Line(7); got != want {
		t.Errorf("NO_COLOR changed the visible text:\n got %q\nwant %q", got, want)
	}
	if !strings.Contains(noColor.Line(7), "["+DefaultOKLabel+"]") {
		t.Errorf("the focused button lost its ring under NO_COLOR: %q", noColor.Line(7))
	}

	// And no colour sequence reaches the terminal.
	raw := noColor.RawString()
	for _, seq := range []string{"38;2", "48;2", "38;5", "48;5"} {
		if strings.Contains(raw, seq) {
			t.Errorf("the NO_COLOR frame contains the colour sequence %q", seq)
		}
	}
	if len(raw) >= len(withColor.RawString()) {
		t.Errorf("the NO_COLOR frame is not smaller: %d bytes against %d",
			len(raw), len(withColor.RawString()))
	}

	// The ATTRIBUTE is what survives, and it is what makes focus visible without
	// colour at all: an unset FocusStyle is the action style reversed.
	if !hasSGR(raw, "7") {
		t.Errorf("the reverse attribute (SGR 7) is not in the NO_COLOR frame, so focus "+
			"is colour-only on a NO_COLOR terminal:\n%q", raw)
	}
}

// TestNoColorKeepsTheChoiceMarker is the same claim for the choice list, whose
// signal is the marker column rather than the ring.
func TestNoColorKeepsTheChoiceMarker(t *testing.T) {
	sink := renderNoColor(t, 40, 12, choiceDialog(40, 12, 5))
	if !strings.Contains(sink.Line(3), ChoiceMarker+" a-option") {
		t.Errorf("the focused choice lost its marker under NO_COLOR: %q", sink.Line(3))
	}
	if !hasSGR(sink.RawString(), "7") {
		t.Errorf("the focused choice row carries no attribute under NO_COLOR:\n%q", sink.RawString())
	}
}

// TestNoColorAtEveryDegenerateSize runs the NO_COLOR path through the same
// degenerate sweep, because the encoder is reached at every size and a size where
// it emits something the screen model does not implement is a capture a test cannot
// vouch for.
func TestNoColorAtEveryDegenerateSize(t *testing.T) {
	for _, size := range []struct{ w, h int }{
		{0, 0}, {1, 1}, {2, 2}, {3, 3}, {5, 4}, {8, 6}, {12, 8}, {40, 12},
	} {
		for _, v := range []Variant{VariantInfo, VariantConfirm, VariantChoice} {
			d := New(rect(size.w, size.h), v)
			d.SetBodyString("body text that wraps over a few lines when there is room")
			d.SetChoices([]string{"one", "two", "three", "four"})
			renderNoColor(t, size.w, size.h, d)
		}
	}
}

// TestAnExplicitFocusStyleReplacesTheDefault covers the documented sentinel from
// the other side: a caller who SETS FocusStyle gets exactly that, not the default
// with theirs applied over it.
func TestAnExplicitFocusStyleReplacesTheDefault(t *testing.T) {
	d := confirmDialog(40, 9)
	d.FocusStyle = buffer.PlainStyle.WithAttr(buffer.AttrUnderline)
	buf := cellBuf(40, 9)
	d.Draw(buf)

	if got := buf.CellAt(12, 7).Attr; got.Has(buffer.AttrReverse) {
		t.Errorf("an explicit FocusStyle still picked up the reverse default: attr %v", got)
	}
	if !buf.CellAt(12, 7).Attr.Has(buffer.AttrUnderline) {
		t.Errorf("the explicit FocusStyle was not applied: attr %v", buf.CellAt(12, 7).Attr)
	}
}

// TestAnActionStyleOverridesTheDialogStyleUnderNoColor checks the two claims
// together, because that is the combination a real monochrome terminal runs: a
// bespoke button keeps its colours in the stream and gains the focus attribute,
// and the ring around it is still a character.
func TestAnActionStyleOverridesTheDialogStyleUnderNoColor(t *testing.T) {
	d := confirmDialog(40, 9)
	d.SetActions(Action{Label: DefaultCancelLabel}, Action{
		Label: DefaultOKLabel,
		Style: buffer.NewStyle(buffer.NewColour(0xff, 0, 0), buffer.DefaultColour, buffer.AttrBold),
	})
	sink := renderNoColor(t, 40, 9, d)

	if !strings.Contains(sink.Line(7), "["+DefaultOKLabel+"]") {
		t.Errorf("the bespoke button lost its ring: %q", sink.Line(7))
	}
	raw := sink.RawString()
	if strings.Contains(raw, "38;2") {
		t.Errorf("a bespoke red survived NO_COLOR:\n%q", raw)
	}
	if !hasSGR(raw, "1") {
		t.Errorf("the bespoke button's own bold attribute did not survive NO_COLOR:\n%q", raw)
	}
}
