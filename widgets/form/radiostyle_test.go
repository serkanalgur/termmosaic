package form

import (
	"testing"

	"github.com/serkanalgur/termmosaic/buffer"
)

// This file pins the Radio focus column being styled as a focus MARK only on the
// row that carries the mark.
//
// radio.go rebuilds every row's two-cell lead with the first cell always in
// FocusStyle. On the rows where the focus mark is a blank pad, that cell still
// wore it — and the resolved FocusStyle defaults to "the selected style with
// AttrUnderline", which inherits SelectedStyle's AttrReverse. So an unfocused
// group painted a two-cell reverse-video gutter down every row, on the rows that
// exist to say which option is NOT chosen.
//
// The COLUMN has to exist on every row for the options to line up, and that is
// layout and is not what is at issue here; the rendition of the blank cell is.

// radioCell returns the cell at (x, y).
func radioCell(buf *buffer.Buffer, x, y int) buffer.Cell { return buf.CellAt(x, y) }

func TestRadioFocusMarkIsStyledOnlyOnTheFocusedRow(t *testing.T) {
	g := NewRadio(buffer.Rect{W: 20, H: 4}, []string{"one", "two", "three"})
	g.SetSelected(1)
	g.SetFocused(true)
	buf := buffer.NewBuffer(20, 4)
	g.Draw(buf)

	// Row 1 is the selected row and the focused one, so it carries the mark.
	mark := radioCell(buf, 0, 1)
	if mark.Ch != []rune(RadioFocusMark)[0] {
		t.Fatalf("cell 0 of row 1 holds %q, want the focus mark %q", mark.Ch, RadioFocusMark)
	}
	want := g.styles().focus
	if mark.Attr != want.Attr {
		t.Errorf("the focus mark carries Attr %d, want FocusStyle's %d", mark.Attr, want.Attr)
	}

	// Row 0 is not focused and has no mark, so its gutter must be the ROW's style:
	// no AttrReverse inherited from SelectedStyle.
	for _, y := range []int{0, 2} {
		c := radioCell(buf, 0, y)
		if c.Ch != ' ' {
			t.Errorf("cell 0 of row %d holds %q, want a blank pad", y, c.Ch)
		}
		if c.Attr&buffer.AttrReverse != 0 {
			t.Errorf("the blank focus cell on row %d carries Attr %d, which inherits AttrReverse from SelectedStyle", y, c.Attr)
		}
	}
}

func TestRadioUnfocusedGroupHasNoReverseVideoGutter(t *testing.T) {
	// The whole-frame statement of the same defect: with nothing focused, no cell
	// of any row may carry the focus rendition, so nothing on screen is reverse
	// video by accident.
	g := NewRadio(buffer.Rect{W: 20, H: 4}, []string{"one", "two", "three"})
	g.SetSelected(1) // row 1 legitimately wears SelectedStyle; the others do not.
	buf := buffer.NewBuffer(20, 4)
	g.Draw(buf)

	for _, y := range []int{0, 2, 3} {
		for x := 0; x < 2; x++ {
			if c := radioCell(buf, x, y); c.Attr != 0 {
				t.Errorf("cell (%d,%d) of an unselected row in an unfocused group carries Attr %d, want none", x, y, c.Attr)
			}
		}
	}
}

func TestRadioFocusMarkKeepsFocusStyleOnAFocusedSelectedRow(t *testing.T) {
	// The complement of the first test in the direction that matters: the fix is not
	// "the focus column is always the row style", which would make focus invisible.
	sel := buffer.NewStyle(buffer.NewColour(255, 255, 255), buffer.NewColour(0, 0, 128), buffer.AttrReverse)
	foc := buffer.NewStyle(buffer.NewColour(0, 255, 0), buffer.NewColour(0, 0, 128), buffer.AttrUnderline)
	g := NewRadio(buffer.Rect{W: 20, H: 4}, []string{"one", "two"})
	g.SelectedStyle = sel
	g.FocusStyle = foc
	g.SetSelected(0)
	g.SetFocused(true)
	buf := buffer.NewBuffer(20, 4)
	g.Draw(buf)

	mark := radioCell(buf, 0, 0)
	if mark.FG != foc.FG || mark.Attr != foc.Attr {
		t.Errorf("the focus mark = FG %s Attr %d, want FocusStyle FG %s Attr %d",
			mark.FG, mark.Attr, foc.FG, foc.Attr)
	}
	if mark.Attr == buffer.AttrReverse {
		t.Errorf("the focus mark wears AttrReverse, which belongs to SelectedStyle, not FocusStyle")
	}
}

func TestRadioMarkerColumnIsUnchangedByTheFocusFix(t *testing.T) {
	// The marker cell is the SELECTED option's business and always was: the fix
	// touched the focus column beside it, and this says the two did not merge.
	g := NewRadio(buffer.Rect{W: 20, H: 3}, []string{"one", "two"})
	g.SetSelected(1)
	buf := buffer.NewBuffer(20, 3)
	g.Draw(buf)

	mark := radioCell(buf, 2, 1)
	if mark.Ch != []rune(RadioMarkSelected)[0] {
		t.Errorf("the marker cell on the selected row holds %q, want %q", mark.Ch, RadioMarkSelected)
	}
	if mark.Attr&buffer.AttrReverse == 0 {
		t.Errorf("the selected marker carries Attr %d, want SelectedStyle's AttrReverse", mark.Attr)
	}
}
