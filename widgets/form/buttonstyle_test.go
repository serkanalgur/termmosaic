package form

import (
	"testing"

	"github.com/serkanalgur/termmosaic/buffer"
)

// This file pins the two button renditions reaching the LABEL, not only the
// background and the two ring cells.
//
// FocusStyle and DisabledStyle are documented at button.go as "the style of the
// whole button — background, label and brackets". The code computed the fill in
// Draw and truncated the cached label in rebuild from the unfocused label style,
// so a configured FocusStyle reached the ring and the fill and nothing else: the
// focused button showed `[OK]` in the default style on a red-on-green field, and
// the default (unset) focus attribute — the one that is supposed to make focus
// visible with nothing configured and under NO_COLOR, which suppresses colour but
// not attributes — was visible as two bracket cells.

// buttonCellAt returns the cell at (x, y) of buf.
func buttonCellAt(buf *buffer.Buffer, x, y int) buffer.Cell { return buf.CellAt(x, y) }

// buttonLabelCell returns the cell carrying the first cell of want on row y.
func buttonLabelCell(t *testing.T, buf *buffer.Buffer, want string, y int) buffer.Cell {
	t.Helper()
	for _, c := range buf.Row(y) {
		if c.Ch == []rune(want)[0] {
			return c
		}
	}
	t.Fatalf("no cell holding %q on row %d:\n%q", want, y, rowOf(buf, y, buf.Width()))
	return buffer.Cell{}
}

func TestButtonFocusStyleReachesTheLabel(t *testing.T) {
	b := NewButton(buffer.Rect{W: 20, H: 3}, "OK")
	focus := buffer.NewStyle(buffer.NewColour(255, 0, 0), buffer.NewColour(0, 255, 0), buffer.AttrBold)
	b.FocusStyle = focus
	b.SetFocused(true)

	buf := buffer.NewBuffer(20, 3)
	b.Draw(buf)

	br := b.buttonRect()
	got := buttonLabelCell(t, buf, "OK", br.Y)
	if got.FG != focus.FG || got.BG != focus.BG || got.Attr != focus.Attr {
		t.Errorf("focused label cell = FG %s BG %s Attr %d, want FocusStyle FG %s BG %s Attr %d",
			got.FG, got.BG, got.Attr, focus.FG, focus.BG, focus.Attr)
	}
	// The label and the ring are one region, so they must not disagree about it.
	if ring := buttonCellAt(buf, br.X, br.Y); ring.FG != focus.FG || ring.BG != focus.BG {
		t.Errorf("the left ring cell = FG %s BG %s, want FocusStyle FG %s BG %s",
			ring.FG, ring.BG, focus.FG, focus.BG)
	}
}

func TestButtonFocusAttributeReachesTheLabelWithoutConfiguration(t *testing.T) {
	// The default path. The doc's claim is that focus is "visible with no
	// configuration and under NO_COLOR", which rests on the attribute; with the
	// attribute only on the two bracket cells, a reader who cannot see a colour
	// change and is looking at the middle of the button sees nothing at all.
	b := NewButton(buffer.Rect{W: 20, H: 3}, "OK")
	b.SetFocused(true)

	buf := buffer.NewBuffer(20, 3)
	b.Draw(buf)

	got := buttonLabelCell(t, buf, "OK", b.buttonRect().Y)
	if got.Attr&buffer.AttrReverse == 0 {
		t.Errorf("the focused label carries Attr %d, want the default focus rendition's AttrReverse", got.Attr)
	}
}

func TestButtonDisabledStyleReachesTheLabel(t *testing.T) {
	b := NewButton(buffer.Rect{W: 20, H: 3}, "OK")
	dis := buffer.NewStyle(buffer.NewColour(128, 128, 128), buffer.NewColour(40, 40, 40), buffer.AttrFaint)
	b.DisabledStyle = dis
	b.SetDisabled(true)

	buf := buffer.NewBuffer(20, 3)
	b.Draw(buf)

	got := buttonLabelCell(t, buf, "OK", b.buttonRect().Y)
	if got.FG != dis.FG || got.BG != dis.BG || got.Attr != dis.Attr {
		t.Errorf("disabled label cell = FG %s BG %s Attr %d, want DisabledStyle FG %s BG %s Attr %d",
			got.FG, got.BG, got.Attr, dis.FG, dis.BG, dis.Attr)
	}
}

func TestButtonUnfocusedLabelKeepsLabelStyle(t *testing.T) {
	// The complement: with no focus and no disabled state the label keeps
	// LabelStyle, so "the label style" did not become "the focus style".
	b := NewButton(buffer.Rect{W: 20, H: 3}, "OK")
	lbl := buffer.NewStyle(buffer.NewColour(10, 20, 30), buffer.DefaultColour, 0)
	b.LabelStyle = lbl
	b.FocusStyle = buffer.NewStyle(buffer.NewColour(255, 0, 0), buffer.NewColour(0, 255, 0), buffer.AttrBold)

	buf := buffer.NewBuffer(20, 3)
	b.Draw(buf)

	got := buttonLabelCell(t, buf, "OK", b.buttonRect().Y)
	if got.FG != lbl.FG {
		t.Errorf("unfocused label FG = %s, want LabelStyle %s", got.FG, lbl.FG)
	}
}

func TestButtonLabelRenditionFollowsAFocusChange(t *testing.T) {
	// The cache is keyed on focus and on disabled already, so this is the test that
	// the truncation happens in the right rendition and not only that it happens.
	b := NewButton(buffer.Rect{W: 20, H: 3}, "OK")
	b.FocusStyle = buffer.NewStyle(buffer.NewColour(255, 0, 0), buffer.NewColour(0, 255, 0), buffer.AttrBold)

	buf := buffer.NewBuffer(20, 3)
	b.Draw(buf)
	b.SetFocused(true)
	b.Draw(buf)
	if got := buttonLabelCell(t, buf, "OK", b.buttonRect().Y); got.Attr&buffer.AttrBold == 0 {
		t.Errorf("after focusing, the label carries Attr %d, want FocusStyle's AttrBold", got.Attr)
	}
	b.SetFocused(false)
	b.Draw(buf)
	if got := buttonLabelCell(t, buf, "OK", b.buttonRect().Y); got.Attr&buffer.AttrBold != 0 {
		t.Errorf("after unfocusing, the label still carries AttrBold")
	}
}
