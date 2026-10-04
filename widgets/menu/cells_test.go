package menu

import (
	"strings"
	"testing"

	"github.com/serkanalgur/termmosaic/buffer"
)

// rect is the menu rectangle used throughout the tests.
func rect(w, h int) buffer.Rect { return buffer.Rect{W: w, H: h} }

// edge returns the vertical border glyph of the catalog's ONLY border table, as a
// string.
//
// Every expectation in this file that needs a border asks the table for its rune
// rather than spelling the rune out. That is not stylistic: vocabulary_test.go
// scans every .go file in the module, tests included, and fails on a box-drawing
// literal anywhere but buffer/border.go and buffer/border_test.go. Asking the table
// also keeps these assertions honest about what they are testing — the menu's
// content rather than the frame's glyphs, which block's own tests already cover.
func edge() string { return string(buffer.BorderPlain.Glyphs(false).Vertical) }

// assertPrefix fails unless row begins with want.
func assertPrefix(t *testing.T, row, want string) {
	t.Helper()
	if !strings.HasPrefix(row, want) {
		t.Errorf("row\n got %q\nwant it to begin with %q", row, want)
	}
}

// framed returns an open, focused, bordered menu of the given size.
func framed(t *testing.T, w, h int, items ...Item) *Menu {
	t.Helper()
	m := newMenu(t, w, h, items...)
	m.Block().SetBorder(buffer.BorderPlain)
	return m
}

// ---------------------------------------------------------------------------
// what is drawn
// ---------------------------------------------------------------------------

// TestMenuDrawsOneColumnPerOpenLevel is the layout in one picture: each open level
// is its own column, side by side, so a two-level menu reads as two columns rather
// than as a rewritten parent.
//
// The border is asked for from the catalog's own glyph table rather than spelled
// out in box-drawing runes, because buffer/border.go owns that table and
// vocabulary_test.go enforces it over every file in the module, tests included.
func TestMenuDrawsOneColumnPerOpenLevel(t *testing.T) {
	m := framed(t, 60, 10, tree()...)
	got := rows(t, 60, 10, m)

	// One column, so its header occupies the whole interior: the level number, the
	// opening bracket at the column's first cell and the closing one at its last.
	// The number is what makes the LEVEL readable; the brackets are what make the
	// column the ACTIVE one.
	if !strings.Contains(got[1], "1") {
		t.Errorf("the header does not show the level number: %q", got[1])
	}
	if !strings.HasPrefix(got[1], edge()+"[") || !strings.HasSuffix(got[1], "]"+edge()) {
		t.Errorf("the only column's header is not bracketed across its interior: %q", got[1])
	}
	// The root's items follow it, one per row, marker gutter first.
	// The marker gutter is one cell, then the check gutter, then the label. A
	// non-checkable item's check cell is a SPACE rather than an absent column,
	// because the column's presence is decided for the whole column and the rows
	// must line up with each other.
	assertPrefix(t, got[2], edge()+"› New")
	// The hint sits before the branch marker, so the pair reads as one annotation.
	if !strings.Contains(got[2], "^N") {
		t.Errorf("row 2 = %q, want the keybinding hint", got[2])
	}
	// Save is the disabled item: drawn, dimmed, and carrying no marker.
	assertPrefix(t, got[3], edge()+"  Save")
	// And there is exactly ONE column for one open level, spanning the interior.
	if len(m.lay.cols) != 1 {
		t.Fatalf("one open level built %d columns, want 1", len(m.lay.cols))
	}
	if got, want := m.lay.cols[0].rect.W, m.blk.Interior().W; got != want {
		t.Errorf("the single column is %d cells wide, want the interior's %d", got, want)
	}
}

// TestMenuTwoColumnsShowBothLevelsSideBySide is the submenu layout, asserted on
// cells. Both headers must be present in the SAME frame, which is what makes the
// depth readable without counting key presses.
func TestMenuTwoColumnsShowBothLevelsSideBySide(t *testing.T) {
	m := framed(t, 60, 10, tree()...)
	press(t, m, "\x1b[C") // open New
	got := rows(t, 60, 10, m)

	// The cursor is at level 1 now, so the SUBMENU's header is the bracketed one and
	// the root's is space-padded. Both appear in the same row, and the difference
	// between them is one cell: that is the whole of the non-colour active-level
	// signal.
	if !strings.Contains(got[1], " 1") {
		t.Errorf("the root's header is missing or is not space-padded: %q", got[1])
	}
	if !strings.Contains(got[1], "[2 New") {
		t.Errorf("the active submenu header is not bracketed with its level and parent label: %q", got[1])
	}
	// Both levels' items are in the SAME row, which is what makes the two columns
	// read as one menu rather than as a rewritten parent.
	if !strings.Contains(got[2], "New") || !strings.Contains(got[2], "Deep") {
		t.Errorf("row 2 does not show both levels' first items: %q", got[2])
	}
	if !strings.Contains(got[3], "Save") || !strings.Contains(got[3], "Plain") {
		t.Errorf("row 3 does not show both levels' second items: %q", got[3])
	}
	// The two columns are laid out at different x positions, one per level.
	if len(m.lay.cols) != 2 {
		t.Fatalf("the layout built %d columns for 2 open levels", len(m.lay.cols))
	}
	a, b := m.lay.cols[0], m.lay.cols[1]
	if a.depth != 0 || b.depth != 1 {
		t.Errorf("column depths are %d and %d, want 0 and 1", a.depth, b.depth)
	}
	if a.rect.X >= b.rect.X {
		t.Errorf("column 1 starts at x=%d and column 2 at x=%d: a submenu must sit to the RIGHT", a.rect.X, b.rect.X)
	}
	if !b.active || a.active {
		t.Errorf("column 0 active=%v, column 1 active=%v; the submenu's column must be the active one", a.active, b.active)
	}
}

// TestMenuActiveHeaderIsBracketedAndInactiveIsNot is the accessibility contract in
// its most load-bearing form: two states differing in ONE cell, and that cell
// being a bracket rather than a colour.
//
// It is asserted as a difference in the rendered rows, because a test that checks
// the styles would pass for a widget that got the colours right and the shapes
// wrong — which is the failure the rule exists to prevent.
func TestMenuActiveHeaderIsBracketedAndInactiveIsNot(t *testing.T) {
	// At the root there is one header and it is the active one.
	// This menu is borderless, so the header is ROW 0 rather than row 1. Asking the
	// layout for the header's row rather than hard-coding an index keeps the
	// assertion about the widget's shape rather than about a bordered one.
	root := newMenu(t, 60, 10, tree()...)
	head := rows(t, 60, 10, root)[root.lay.cols[0].headRect.Y]
	if !strings.HasPrefix(head, "[1") {
		t.Errorf("at the root the only header must be bracketed: %q", head)
	}

	// Open a submenu: the bracket MOVES to the new active level. That is the
	// property worth pinning — a bracket that stayed on the root would say "level
	// one is active" while the keys were going to level two.
	m := newMenu(t, 60, 10, tree()...)
	press(t, m, "\x1b[C")
	at1 := rows(t, 60, 10, m)[m.lay.cols[0].headRect.Y]
	if strings.Contains(at1, "[1") {
		t.Errorf("with a submenu open the root header is still bracketed (%q)", at1)
	}
	if !strings.Contains(at1, "[2 New") {
		t.Errorf("the submenu header is not bracketed: %q", at1)
	}

	// And it moves again at depth three.
	press(t, m, "\x1b[C")
	at2 := rows(t, 60, 10, m)[m.lay.cols[0].headRect.Y]
	if !strings.Contains(at2, "[3 Deep") {
		t.Errorf("at depth 3 the deepest header must be the bracketed one: %q", at2)
	}
	if strings.Contains(at2, "[1") || strings.Contains(at2, "[2 ") {
		t.Errorf("at depth 3 an ancestor is still bracketed (%q): the bracket is not following the active level", at2)
	}

	// Coming back out moves it back, so the bracket is a function of the CURSOR and
	// not of how the menu was opened.
	press(t, m, "\x1b[D")
	press(t, m, "\x1b[D")
	back := rows(t, 60, 10, m)[m.lay.cols[0].headRect.Y]
	if !strings.Contains(back, "[1") {
		t.Errorf("back at the root the bracket did not return: %q", back)
	}
}

// TestMenuSelectedRowIsMarkedAndFilled is the item-level selection signal, asserted
// on two channels at once: the gutter glyph and the row's background attribute.
//
// Both are needed. The glyph survives a monochrome terminal; the fill is what makes
// a row read as selected at a glance. A widget with only one of them fails this
// test, which is the intent.
func TestMenuSelectedRowIsMarkedAndFilled(t *testing.T) {
	m := newMenu(t, 40, 8, tree()...)
	buf := drawInto(m, 40, 8)

	if got := buf.CellAt(0, 1).Ch; got != '›' {
		t.Errorf("the selected row's gutter cell is %q, want %q", got, '›')
	}
	// The fill covers the WHOLE row, not just the text: a highlight that stops at
	// the end of the label is a highlight of a word.
	if got := buf.CellAt(38, 1).Attr; !got.Has(buffer.AttrReverse) {
		t.Errorf("the selected row is not filled to its far edge: attr %v at column 38", got)
	}
	// An unselected row carries neither.
	if got := buf.CellAt(0, 2).Ch; got != ' ' {
		t.Errorf("an unselected row's gutter cell is %q, want a space", got)
	}
	if got := buf.CellAt(38, 2).Attr; got.Has(buffer.AttrReverse) {
		t.Errorf("an unselected row is filled too: attr %v", got)
	}
}

// TestMenuDisabledRowCarriesNoMarker is the disabled signal's non-colour half. A
// disabled item is dimmed by style, and the gutter's emptiness is what says "this
// one cannot be selected" to a reader who sees no difference between two dim
// colours.
func TestMenuDisabledRowCarriesNoMarker(t *testing.T) {
	m := newMenu(t, 40, 8, tree()...)
	buf := drawInto(m, 40, 8)
	c := &m.lay.cols[0]

	// The disabled item is the one at index 1, which the layout places on the row
	// its offset names rather than at a hard-coded row number.
	y := c.itemRect.Y + 1
	if got := buf.CellAt(c.markerX, y).Ch; got != ' ' {
		t.Errorf("the disabled row's gutter cell is %q, want a space: a disabled item must not look selected", got)
	}
	if got := line(buf, y, 40); !strings.Contains(got, "Save") {
		t.Errorf("the disabled item is not drawn at all: %q — a menu that hides what it cannot do cannot be read", got)
	}
	// And its label is faint by default, which is an ATTRIBUTE rather than a colour,
	// so it survives NO_COLOR. The label's own column is read from the layout: the
	// check gutter sits between the marker and the text, and the check cell carries
	// CheckStyle rather than the row's style.
	if got := buf.CellAt(c.labelX, y).Attr; !got.Has(buffer.AttrFaint) {
		t.Errorf("the disabled row's label is not faint: attr %v, want AttrFaint", got)
	}
	// An ENABLED row's label is not faint, so the attribute is doing the work
	// rather than every row simply carrying it.
	if got := buf.CellAt(c.labelX, c.itemRect.Y).Attr; got.Has(buffer.AttrFaint) {
		t.Errorf("an enabled row's label is faint too: attr %v", got)
	}
}

// TestMenuDisabledRowStaysUnmarkedWhenSelectedProgrammatically is the case the
// navigation tests cannot reach.
//
// No key ever selects a disabled item — that is what the skip rule is for — so the
// only way a disabled row can be the selected one is a caller calling into the
// widget directly, which is legal and is what an application restoring a stored
// selection does. The drawing must hold in that case too, and it is a separate
// guard from the skip: the skip makes the state unreachable by key, and this makes
// it unreachable by drawing.
func TestMenuDisabledRowStaysUnmarkedWhenSelectedProgrammatically(t *testing.T) {
	m := newMenu(t, 40, 8, tree()...)
	// Save is the disabled item at index 1.
	m.selectAt(0, 1)
	if got := m.Selected(); got != 1 {
		t.Fatalf("setup: the selection is %d, want 1 — selectAt must accept an index it is handed", got)
	}
	buf := drawInto(m, 40, 8)
	c := &m.lay.cols[0]
	y := c.itemRect.Y + 1
	if got := buf.CellAt(c.markerX, y).Ch; got != ' ' {
		t.Errorf("a programmatically selected DISABLED row wears the marker %q: a disabled item "+
			"must never look selectable, however it came to be selected", got)
	}
	// And it is still drawn and still faint.
	if got := line(buf, y, 40); !strings.Contains(got, "Save") {
		t.Errorf("the disabled row is not drawn at all: %q", got)
	}
	if got := buf.CellAt(c.labelX, y).Attr; !got.Has(buffer.AttrFaint) {
		t.Errorf("a selected disabled row is not faint: attr %v", got)
	}
}

// TestMenuCheckColumnShowsCheckedAndUnchecked is the toggle signal. Two items, one
// checked and one not, and they must differ in a CELL — which is the assertion
// that colour is not the only signal for a checkbox.
func TestMenuCheckColumnShowsCheckedAndUnchecked(t *testing.T) {
	m := newMenu(t, 40, 8,
		Item{Label: "on", Checkable: true, Checked: true},
		Item{Label: "off", Checkable: true, Checked: false},
		Item{Label: "plain"},
	)
	buf := drawInto(m, 40, 8)
	// Column 1 is the check gutter: the marker is column 0.
	if got := buf.CellAt(1, 1).Ch; got != '✓' {
		t.Errorf("the checked item's check cell is %q, want %q", got, '✓')
	}
	if got := buf.CellAt(1, 2).Ch; got != ' ' {
		t.Errorf("the unchecked item's check cell is %q, want a space", got)
	}
	if got := line(buf, 1, 40); !strings.Contains(got, "on") {
		t.Errorf("row 1 does not contain the checked item's label: %q", got)
	}
	// The labels sit to the RIGHT of the check column, so the gutter is between
	// the marker and the text.
	if got := buf.CellAt(2, 1).Ch; got != 'o' {
		t.Errorf("the label starts at the check column: cell (2,1) is %q, want the first letter of 'on'", got)
	}
}

// TestMenuCheckColumnAbsentWhenNothingIsCheckable is the other half, and the
// stronger statement: a check column in a column of plain items would assert that
// these are toggles. Its ABSENCE is the signal, so this test would fail against a
// widget that always drew the gutter.
func TestMenuCheckColumnAbsentWhenNothingIsCheckable(t *testing.T) {
	m := newMenu(t, 40, 6, Item{Label: "one"}, Item{Label: "two"})
	buf := drawInto(m, 40, 6)
	if got := line(buf, 1, 40); !strings.HasPrefix(got, "›one") {
		t.Errorf("row 1 = %q, want it to start with the marker then the label — no check column", got)
	}
	if got := line(buf, 2, 40); !strings.HasPrefix(got, " two") {
		t.Errorf("row 2 = %q, want the label to start at column 1", got)
	}
}

// TestMenuSubmenuMarkerIsDrawnOnlyForBranches is the branch signal, asserted on
// cells so a widget that coloured branches without marking them would fail.
//
// The marker is at the RIGHT gutter of the row, so its position is also part of the
// contract: a marker next to the label would read as part of the text.
func TestMenuSubmenuMarkerIsDrawnOnlyForBranches(t *testing.T) {
	m := newMenu(t, 40, 8, tree()...)
	buf := drawInto(m, 40, 8)
	// The row is 40 wide: the submenu marker is the last cell before the edge.
	if got := buf.CellAt(39, 1).Ch; got != '▸' {
		t.Errorf("the branch row's right gutter is %q, want %q", got, '▸')
	}
	// Sep, a leaf, has nothing there.
	if got := buf.CellAt(39, 3).Ch; got != ' ' {
		t.Errorf("the leaf row's right gutter is %q, want a space", got)
	}
}

// TestMenuHintIsRightAlignedAndGivesWayToALongLabel is the hint column's whole
// story: shown when there is room, dropped before a label is made unreadable, and
// dropped before a branch's marker — which must never go, because a branch that
// does not look like a branch cannot be navigated by reading it.
func TestMenuHintIsRightAlignedAndGivesWayToALongLabel(t *testing.T) {
	m := newMenu(t, 40, 6, tree()...)
	wide := drawInto(m, 40, 6)
	if got := line(wide, 1, 40); !strings.Contains(got, "^N") {
		t.Errorf("at 40 cells the hint column is missing: %q", got)
	}
	// The gap between the hint and the branch marker is exactly hintGap cells, and
	// the marker follows it. Both positions come from the cached layout, which is
	// where the arithmetic actually lives.
	c := &m.lay.cols[0]
	if !c.showHint {
		t.Fatalf("at 40 cells the hint column was dropped; the layout is %+v", c)
	}
	if got := c.submenuX - c.hintX; got != hintGap+2 {
		t.Errorf("the gap between the hint and the submenu marker is %d cells, want %d (the hint's width plus the %d-cell gap)", got, hintGap+2, hintGap)
	}
	if got := wide.CellAt(c.submenuX, 1).Ch; got != '▸' {
		t.Errorf("cell %d is %q, want the submenu marker", c.submenuX, got)
	}

	// Narrow enough that the hint cannot fit beside a legible label. The label's
	// floor is minLabelW, so the threshold is a computed one rather than a magic
	// width, and the test asks the layout where it is rather than guessing.
	m.SetBounds(rect(10, 6))
	narrow := drawInto(m, 10, 6)
	nc := &m.lay.cols[0]
	if nc.showHint {
		t.Errorf("at 10 cells the hint is still shown: %q — the hint is dropped before the label is squeezed", line(narrow, 1, 10))
	}
	if nc.labelW < minLabelW {
		t.Errorf("at 10 cells the label is %d cells wide, below the %d-cell floor it is dropped for", nc.labelW, minLabelW)
	}
	// The submenu marker survives every width: a branch that does not look like a
	// branch cannot be navigated by reading it.
	if got := narrow.CellAt(nc.submenuX, 1).Ch; got != '▸' {
		t.Errorf("the submenu marker was dropped at 10 cells: %q", got)
	}
}

// TestMenuAsciiRungGivesOneCellMarkers is the degradation contract: every marker
// is one cell wide on both rungs, so a menu's ARITHMETIC is identical on a
// terminal that cannot do Unicode.
//
// It is asserted on the drawn columns, not on the constants, because the failure it
// guards is a layout that shifts by a cell between terminals.
func TestMenuAsciiRungGivesOneCellMarkers(t *testing.T) {
	uni := newMenu(t, 40, 8, tree()...)
	ascii := newMenu(t, 40, 8, tree()...)
	ascii.Ascii = true

	u, a := drawInto(uni, 40, 8), drawInto(ascii, 40, 8)
	if got := u.CellAt(39, 1).Ch; got != '▸' {
		t.Errorf("Unicode submenu marker is %q, want %q", got, '▸')
	}
	if got := a.CellAt(39, 1).Ch; got != '>' {
		t.Errorf("ASCII submenu marker is %q, want %q", got, '>')
	}
	// Same label COLUMN on both rungs: the marker widths match. The comparison is on
	// the cached column rather than on a string index, because a byte index into a
	// row containing a three-byte marker is three further right than the cell it
	// names — which is exactly the confusion this assertion exists to rule out.
	if got, want := ascii.lay.cols[0].labelX, uni.lay.cols[0].labelX; got != want {
		t.Errorf("the label column is %d on ASCII and %d on Unicode: the marker widths must match", got, want)
	}
	// And the check column agrees.
	if got := a.CellAt(1, 4).Ch; got != 'x' {
		t.Errorf("ASCII check mark is %q, want %q", got, 'x')
	}
	if got := u.CellAt(1, 4).Ch; got != '✓' {
		t.Errorf("Unicode check mark is %q, want %q", got, '✓')
	}
}

// ---------------------------------------------------------------------------
// mouse
// ---------------------------------------------------------------------------

// TestMenuClickSelectsTakesFocusAndOpensOnASecondClick is the mouse contract. A
// click is a complete interaction without the application writing a handler, and
// the second click on an already-selected row is what distinguishes "let me look
// at this" from "let me go in here".
func TestMenuClickSelectsTakesFocusAndOpensOnASecondClick(t *testing.T) {
	m := newMenu(t, 40, 10, tree()...)
	m.SetFocused(false)

	// Row 3 is Help (index 4): header on row 0, items on rows 1..5.
	if !clickAt(m, 3, 5) {
		t.Fatal("a press inside the menu was not consumed")
	}
	if !m.Focused() {
		t.Error("a press inside the menu did not take focus")
	}
	if got := m.Selected(); got != 4 {
		t.Errorf("the pressed row selected %d, want 4", got)
	}
	if d := m.Depth(); d != 1 {
		t.Errorf("the first press on a row opened a level: depth %d, want 1", d)
	}
	// A second press on the same row opens it. Help is a leaf in this fixture, so
	// use the New row instead to observe the opening half.
	if !clickAt(m, 3, 1) {
		t.Fatal("the press on row 1 was not consumed")
	}
	if !clickAt(m, 3, 1) {
		t.Fatal("the second press on row 1 was not consumed")
	}
	if d := m.Depth(); d != 2 {
		t.Errorf("a second press on a branch did not open it: depth %d, want 2", d)
	}
}

// TestMenuClickInANewColumnSelectsAtThatLevel pins the multi-level half: a press
// on a submenu's row puts the CURSOR there rather than at the root.
func TestMenuClickInANewColumnSelectsAtThatLevel(t *testing.T) {
	m := newMenu(t, 60, 10, tree()...)
	press(t, m, "\x1b[C") // two columns, each 30 wide
	if d := m.Depth(); d != 2 {
		t.Fatalf("setup: depth %d, want 2", d)
	}
	// The submenu's second item is at x=30..59, row 2.
	if !clickAt(m, 40, 2) {
		t.Fatal("a press in the submenu column was not consumed")
	}
	if got := m.SelectedAt(1); got != 1 {
		t.Errorf("the press selected %d at level 1, want 1", got)
	}
	if m.active() != 1 {
		t.Errorf("the active level after a press in that column is %d, want 1", m.active())
	}
}

// TestMenuClickOutsideIsNotConsumed is what lets a Split route a press to a
// sibling: a widget that consumed presses anywhere on the screen would break every
// container that composes it.
func TestMenuClickOutsideIsNotConsumed(t *testing.T) {
	m := newMenu(t, 20, 6, tree()...)
	if clickAt(m, 25, 1) {
		t.Error("a press outside the menu's rectangle was consumed")
	}
	if clickAt(m, -1, -1) {
		t.Error("a press at negative coordinates was consumed")
	}
}

// TestMenuWheelScrollsWithoutMovingTheSelection is the wheel contract, and the
// distinction from navigation is the whole point of it.
func TestMenuWheelScrollsWithoutMovingTheSelection(t *testing.T) {
	var items []Item
	for i := range 40 {
		items = append(items, Item{Label: itoa(i)})
	}
	m := newMenu(t, 20, 6, items...) // 5 item rows under one header
	press(t, m, "\x1b[F")            // End: selection at the tail, offset at the bottom
	sel := m.Selected()
	before := m.Offset()
	if before == 0 {
		t.Fatalf("setup: End left the offset at 0, so there is nothing to scroll away from")
	}
	if !wheelAt(m, 3, 3, true) {
		t.Error("a wheel notch up over the menu was not consumed")
	}
	if got := m.Offset(); got >= before {
		t.Errorf("a wheel notch up moved the offset from %d to %d", before, got)
	}
	if got := m.Selected(); got != sel {
		t.Errorf("a wheel notch moved the selection from %d to %d; the wheel scrolls, it does not navigate", sel, got)
	}
}
