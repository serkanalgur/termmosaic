package dialog

import (
	"testing"

	"github.com/serkanalgur/termmosaic/buffer"
)

// This file pins ChoiceFocusStyle reaching the focused choice's LABEL.
//
// The defect it exists for is the style-application class the release history
// records: drawChoices computed the row style for the fill and the ">" marker and
// then wrote the label from a cache built with the UNFOCUSED style, because focus
// is not known when that cache is built. The focused row therefore showed its
// marker in the focus rendition, its label in the unfocused one, and the rest of
// the row in the focus rendition — and with the default styles that is
// attr=none text on an attr=reverse row, on exactly the row the reader is meant
// to be looking at.

// cellOfRune returns the cell holding r in row y of buf, or false.
func cellOfRune(buf *buffer.Buffer, r rune, y int) (buffer.Cell, bool) {
	for _, c := range buf.Row(y) {
		if c.Ch == r {
			return c, true
		}
	}
	return buffer.Cell{}, false
}

func TestChoiceFocusStyleReachesTheChoiceLabel(t *testing.T) {
	d := New(rect(30, 7), VariantChoice)
	d.SetChoices([]string{"alpha", "bravo", "charlie"})
	// Two renditions that differ in every field the cell carries, so a label
	// written in the wrong one cannot be mistaken for the right one.
	focus := buffer.NewStyle(buffer.NewColour(0, 255, 0), buffer.NewColour(0, 0, 128), buffer.AttrBold)
	d.ChoiceFocusStyle = focus
	d.SetFocus(1)

	buf := cellBuf(30, 7)
	d.Draw(buf)

	label, ok := cellOfRune(buf, 'b', d.choiceTop+1)
	if !ok {
		t.Fatalf("the focused label is not on its row:\n%s", snapshot(buf))
	}
	if label.FG != focus.FG || label.BG != focus.BG || label.Attr != focus.Attr {
		t.Errorf("focused choice label cell = FG %s BG %s Attr %d, want ChoiceFocusStyle FG %s BG %s Attr %d",
			label.FG, label.BG, label.Attr, focus.FG, focus.BG, focus.Attr)
	}
	// The marker and the label must now agree, which is the whole point: before the
	// fix the marker carried the focus rendition and the label did not.
	marker, ok := cellOfRune(buf, []rune(ChoiceMarker)[0], d.choiceTop+1)
	if !ok {
		t.Fatalf("the focused marker is not on its row:\n%s", snapshot(buf))
	}
	if marker.Attr != label.Attr {
		t.Errorf("marker Attr %d and label Attr %d disagree on the focused row", marker.Attr, label.Attr)
	}
}

func TestUnfocusedChoiceKeepsTheUnfocusedLabelStyle(t *testing.T) {
	// The complement of the test above, and the reason the fix is not "always use
	// the focus style": a row that is not focused must not wear it.
	d := New(rect(30, 7), VariantChoice)
	d.SetChoices([]string{"alpha", "bravo"})
	d.ChoiceFocusStyle = buffer.NewStyle(buffer.NewColour(0, 255, 0), buffer.NewColour(0, 0, 128), buffer.AttrBold)
	d.SetFocus(0)

	buf := cellBuf(30, 7)
	d.Draw(buf)

	label, ok := cellOfRune(buf, 'b', d.choiceTop+1)
	if !ok {
		t.Fatalf("the unfocused label is not on its row:\n%s", snapshot(buf))
	}
	if label.Attr == buffer.AttrBold {
		t.Errorf("the unfocused label wears AttrBold from ChoiceFocusStyle")
	}
}

func TestChoiceFocusStyleReachesTheLabelWithoutConfiguration(t *testing.T) {
	// The default path, which is the one that is actually unreadable: ChoiceStyle
	// resolves to no attribute at all and the unset ChoiceFocusStyle means "the
	// choice style with AttrReverse". Before the fix the focused label kept the
	// unfocused style, so it was plain text sitting on a reversed row.
	d := New(rect(30, 7), VariantChoice)
	d.SetChoices([]string{"alpha", "bravo"})
	d.SetFocus(0)

	buf := cellBuf(30, 7)
	d.Draw(buf)

	label, ok := cellOfRune(buf, 'a', d.choiceTop)
	if !ok {
		t.Fatalf("the focused label is not on its row:\n%s", snapshot(buf))
	}
	if label.Attr&buffer.AttrReverse == 0 {
		t.Errorf("the focused label carries Attr %d, want the default focus rendition's AttrReverse", label.Attr)
	}
}

func TestChoiceFocusStyleSurvivesAMovedFocus(t *testing.T) {
	// Moving the focus must not leave the old label in the old rendition: the
	// second cached slice is rebuilt only on a size or style change, so this is the
	// test that the two slices are keyed to the right rows and are both live.
	d := New(rect(30, 7), VariantChoice)
	d.SetChoices([]string{"alpha", "bravo", "charlie"})
	d.ChoiceFocusStyle = buffer.NewStyle(buffer.NewColour(0, 255, 0), buffer.NewColour(0, 0, 128), buffer.AttrBold)
	d.SetFocus(0)

	buf := cellBuf(30, 7)
	d.Draw(buf)
	d.SetFocus(2)
	d.Draw(buf)

	first, ok := cellOfRune(buf, 'a', d.choiceTop)
	if !ok {
		t.Fatalf("choice 0 is not on its row:\n%s", snapshot(buf))
	}
	if first.Attr == buffer.AttrBold {
		t.Errorf("the label of the row that LOST focus still wears ChoiceFocusStyle")
	}
	last, ok := cellOfRune(buf, 'c', d.choiceTop+2)
	if !ok {
		t.Fatalf("choice 2 is not on its row:\n%s", snapshot(buf))
	}
	if last.Attr != buffer.AttrBold {
		t.Errorf("the newly focused label carries Attr %d, want ChoiceFocusStyle's AttrBold", last.Attr)
	}
}

func TestChoiceLabelsAreCachedInBothRenditions(t *testing.T) {
	// The two slices must stay the same length as the visible window, or a focused
	// row near the bottom of a scrolled list would index off the end of the focused
	// cache while an unfocused one would not — a defect that only appears once the
	// list is longer than the dialog.
	d := New(rect(24, 6), VariantChoice)
	d.SetChoices([]string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"})
	d.SetFocus(9) // scrolled into view by SetFocus

	buf := cellBuf(24, 6)
	d.Draw(buf)

	if len(d.choiceLabels) != len(d.choiceFocusLabels) {
		t.Fatalf("cached %d labels and %d focused labels", len(d.choiceLabels), len(d.choiceFocusLabels))
	}
	if len(d.choiceLabels) == 0 {
		t.Fatalf("no choice labels were cached for a dialog with ten choices")
	}
	if len(d.choiceLabels) >= 10 {
		t.Errorf("%d labels were cached, so the visible window is not what was prepared", len(d.choiceLabels))
	}
}
