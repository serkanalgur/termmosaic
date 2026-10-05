package main

// The input tests: the key and mouse contract, the pair switch, the pause, and
// the mouse-capture guarantee.
//
// Every keyboard and mouse assertion goes through the REAL decoder, following the
// idiom widgets/data/eventtest_test.go established: a hand-built Event can describe
// something no terminal would ever send, and a test that passes on one proves
// nothing about the program. Decoding "\x1b[C" gives the same KeyRight a real
// arrow key gives, and it fails the moment the decoder's mapping changes.
//
// The events that do NOT decode from bytes are built directly, and each says why:
// a bare ESC is ambiguous with a sequence prefix until the decoder has waited out
// its delay, and a 0x03 is only the interrupt once the decoder has reported it as
// Ctrl+'c'.

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/headless"
	"github.com/serkanalgur/termmosaic/input"
	"github.com/serkanalgur/termmosaic/render"
	"github.com/serkanalgur/termmosaic/widgets/widgettest"
)

// The escape sequences for the keys this example binds, as the ACTUAL bytes a
// terminal sends rather than as decoder calls, because the point is that the
// decoder agrees with the board about what those bytes mean.
const (
	seqUp     = "\x1b[A"
	seqDown   = "\x1b[B"
	seqRight  = "\x1b[C"
	seqLeft   = "\x1b[D"
	seqHome   = "\x1b[H"
	seqEnd    = "\x1b[F"
	seqPgUp   = "\x1b[5~"
	seqPgDn   = "\x1b[6~"
	seqTab    = "\t"
	seqBackTb = "\x1b[Z"
)

// shortTableH is a screen height at which the table's viewport is smaller than the
// fixture's fourteen rows, so scrolling is observable AND several body rows are
// visible, so a click can be aimed at one that is not the first.
//
// It is asserted against a real dashboard in TestShortTableHIsScrollableAndClickable,
// so a change to any band's declared height moves this test's subject with it
// rather than quietly making every scrolling assertion here vacuous.
const shortTableH = 26

// The SGR 1006 button codes, which is the encoding this example ENABLES, so every
// mouse assertion below goes through the same decoding path a real click takes
// rather than through a hand-built Mouse struct.
//
// 64 and 65 are wheel up and down with no modifiers, 0 is a left-button press.
const (
	sgrWheelUp   = 64
	sgrWheelDown = 65
	sgrLeftPress = 0
)

// sgr builds an SGR mouse sequence for a button code at a cell, as a terminal sends
// it: the coordinates are ONE-BASED, which is the encoding's own convention and the
// reason a hand-built event with zero-based coordinates is not the same thing.
func sgr(button, x, y int) string {
	return "\x1b[<" + strconv.Itoa(button) + ";" + strconv.Itoa(x+1) + ";" + strconv.Itoa(y+1) + "M"
}

// wheelAt is one wheel notch at a cell, up or down.
func wheelAt(x, y int, up bool) string {
	if up {
		return sgr(sgrWheelUp, x, y)
	}
	return sgr(sgrWheelDown, x, y)
}

// clickAt is a left-button press at a cell.
func clickAt(x, y int) string { return sgr(sgrLeftPress, x, y) }

// decode runs one byte sequence through the real decoder and returns the single
// event it produced.
func decode(t *testing.T, seq string) termmosaic.Event {
	t.Helper()
	ev, n, status := input.Decode([]byte(seq), input.DefaultConfig())
	if status != input.StatusOK {
		t.Fatalf("decoding %q: status %v, want ok", seq, status)
	}
	if n != len(seq) {
		t.Errorf("decoding %q consumed %d bytes of %d", seq, n, len(seq))
	}
	if ev.Kind == termmosaic.EventNone {
		t.Fatalf("decoding %q produced no event", seq)
	}
	return ev
}

// key decodes a sequence and asserts it is a key.
func key(t *testing.T, seq string) termmosaic.Event {
	t.Helper()
	ev := decode(t, seq)
	if ev.Kind != termmosaic.EventKey {
		t.Fatalf("decoding %q produced a %v, want a key", seq, ev.Kind)
	}
	return ev
}

// press offers one decoded sequence to the board and reports whether it consumed
// it.
func press(t *testing.T, d *dashboard, seq string) bool {
	t.Helper()
	return d.Handle(key(t, seq))
}

// click offers one decoded mouse sequence to the board.
func click(t *testing.T, d *dashboard, seq string) bool {
	t.Helper()
	ev := decode(t, seq)
	if ev.Kind != termmosaic.EventMouse {
		t.Fatalf("decoding %q produced a %v, want a mouse event", seq, ev.Kind)
	}
	return d.Handle(ev)
}

// sampleSnapshot is the bundled capture as a snapshot, at the frozen instant every
// golden uses so the footer reads the same in every test.
func sampleSnapshot(t testing.TB) *snapshot {
	t.Helper()
	m, _ := newOfflineSource().Fetch(context.Background())
	return &snapshot{m: m, at: at, source: "offline", ok: true, gen: 1}
}

// live returns a board fed the bundled sample at the wide size, which is where the
// chooser has room for three tabs and every panel is drawn.
func live(t testing.TB) *dashboard {
	t.Helper()
	return boardAt(t, goldenWideW, goldenWideH)
}

// boardAt returns a board of the given size fed the bundled sample.
func boardAt(t testing.TB, w, h int) *dashboard {
	t.Helper()
	d := newDashboard(w, h)
	d.SetFrame(buildFrame(sampleSnapshot(t)))
	return d
}

// focusedTable moves the focus onto the pairs table and renders once, so the table
// has an interior and a hit-testable body before any click is aimed at it.
//
// Rendering first is not cosmetic: data.Table hit-tests against a body rectangle
// derived in adapt, and a click that arrives before the first Draw would land
// against a zero rect — which the widget handles, but which would make every mouse
// assertion here pass vacuously.
func focusedTable(t *testing.T, d *dashboard) {
	t.Helper()
	renderAt(t, goldenWideW, goldenWideH, 1, d)
	for d.FocusLabel() != "pairs table" {
		if !press(t, d, seqTab) {
			t.Fatalf("tab stopped being consumed at focus %d", d.FocusIndex())
		}
	}
}

// ---------------------------------------------------------------------------
// the focus ring
// ---------------------------------------------------------------------------

// TestFocusRingCyclesAndNoWidgetSwallowsTab is the composition contract in
// miniature, and it is the test that would fail if a widget grew a Tab binding.
//
// The application's focus routing is "Tab moves focus because nothing else claims
// it". That is an ASSUMPTION about every widget in the ring, and an assumption
// about someone else's widget is exactly the kind that rots silently. So it is
// asserted: a full turn returns to where it started, which cannot happen if any
// member consumed a tab.
func TestFocusRingCyclesAndNoWidgetSwallowTab(t *testing.T) {
	d := live(t)
	if got := d.FocusIndex(); got != 0 {
		t.Fatalf("focus starts at %d, want 0", got)
	}
	// The labels are a parallel list to the ring, and a shorter one would be a
	// panic waiting for a reader who tabs far enough.
	if len(d.focusLabels) != len(d.focusables) {
		t.Fatalf("the ring has %d widgets and %d labels; they must be the same length",
			len(d.focusables), len(d.focusLabels))
	}
	for i := 0; i < len(d.focusables); i++ {
		if !press(t, d, seqTab) {
			t.Fatalf("tab was not consumed at step %d", i)
		}
		if got, want := d.FocusIndex(), (i+1)%len(d.focusables); got != want {
			t.Fatalf("after %d tabs focus is %d, want %d", i+1, got, want)
		}
	}
	// Backwards, so shift-tab is not a one-way door.
	if !press(t, d, seqBackTb) {
		t.Fatal("shift-tab was not consumed")
	}
	if got, want := d.FocusIndex(), len(d.focusables)-1; got != want {
		t.Errorf("after one shift-tab focus is %d, want %d", got, want)
	}
}

// TestOnlyTheFocusedWidgetTakesTheKeys is the other half of the contract: focus is
// not decoration.
//
// An unfocused table must NOT consume the arrows, because then tabbing to the pair
// chooser would leave the arrows silently scrolling a table the reader cannot see
// being scrolled — the failure mode of a screen that claims to be focusable.
func TestOnlyTheFocusedWidgetTakesTheKeys(t *testing.T) {
	d := live(t)
	renderAt(t, goldenWideW, goldenWideH, 1, d)

	// The chooser has focus by default, and the arrows change the PAIR rather than
	// the table's selection.
	before := d.table.Selected()
	press(t, d, seqRight)
	if got := d.table.Selected(); got != before {
		t.Errorf("an arrow moved the table's selection from %d to %d while the chooser had focus", before, got)
	}
	if !d.pair.Focused() {
		t.Error("the chooser is not focused after an arrow while it had focus")
	}
	if d.table.Focused() {
		t.Error("the table is focused while the chooser is")
	}

	// With the table focused, the arrows move the selection.
	focusedTable(t, d)
	if !d.table.Focused() {
		t.Fatal("the table did not take focus")
	}
	if d.pair.Focused() {
		t.Error("the chooser kept focus after the table took it")
	}
	before = d.table.Selected()
	if !press(t, d, seqDown) {
		t.Fatal("down was not consumed by the focused table")
	}
	if got := d.table.Selected(); got <= before {
		t.Errorf("down did not move the selection: %d then %d", before, got)
	}
}

// ---------------------------------------------------------------------------
// the table
// ---------------------------------------------------------------------------

// TestTableScrollKeysReachTheTable walks the whole documented table contract, one
// key at a time, asserting the SELECTION moves rather than merely that the key was
// consumed.
//
// "Consumed" is the weak assertion and it is what a form's own tests use, because
// a form cannot say what consuming means. Here the board can: the selection is
// visible state, so every key is checked against it.
func TestTableScrollKeysReachTheTable(t *testing.T) {
	d := live(t)
	focusedTable(t, d)
	rows := d.table.Rows()
	if rows < 4 {
		t.Fatalf("the fixture has %d rows; these keys need more to be distinguishable", rows)
	}

	if !press(t, d, seqEnd) {
		t.Fatal("end was not consumed")
	}
	if got := d.table.Selected(); got != rows-1 {
		t.Errorf("end selected %d, want the last row %d", got, rows-1)
	}
	if !press(t, d, seqHome) {
		t.Fatal("home was not consumed")
	}
	if got := d.table.Selected(); got != 0 {
		t.Errorf("home selected %d, want the first row 0", got)
	}

	// Page down moves more than one row, which is what distinguishes it from a
	// repeated down — the reason it is asserted rather than skipped.
	press(t, d, seqDown)
	afterOne := d.table.Selected()
	if !press(t, d, seqPgDn) {
		t.Fatal("page down was not consumed")
	}
	afterPage := d.table.Selected()
	if afterPage <= afterOne+1 {
		t.Errorf("page down moved from %d to %d; a page is more than a row", afterOne, afterPage)
	}
	if !press(t, d, seqPgUp) {
		t.Fatal("page up was not consumed")
	}
	if got := d.table.Selected(); got >= afterPage {
		t.Errorf("page up did not move backwards: %d then %d", afterPage, got)
	}

	// Up from the first row stays at the first row rather than wrapping or going
	// negative: the table's own contract clamps, and the board must not change that.
	if !press(t, d, seqHome) {
		t.Fatal("home was not consumed")
	}
	press(t, d, seqUp)
	if got := d.table.Selected(); got != 0 {
		t.Errorf("up from the first row selected %d, want 0", got)
	}
}

// TestWheelScrollsTheTableWithoutTakingFocus is the mouse's most useful gesture
// here, and it is the one whose focus behaviour is worth pinning.
//
// A wheel notch over the table scrolls it, and the focus does NOT move: a reader
// scrolling a list with the wheel is reading, not committing to a panel, and
// yanking focus to the table under them would make the key hint change under the
// pointer. data.Table consumes the wheel whether or not it is focused, which is
// what makes this true, and the assertion is here so a change to it is deliberate.
func TestWheelScrollsTheTableWithoutTakingFocus(t *testing.T) {
	// A SHORT screen, so the table's viewport is smaller than its fourteen rows and
	// there is something to scroll. At the full height the table shows every row and
	// a wheel notch correctly does nothing — which would make this test pass
	// vacuously, and a test that cannot fail is worse than no test.
	d := boardAt(t, goldenWideW, shortTableH)
	renderAt(t, goldenWideW, shortTableH, 1, d)
	if d.table.RowOffset() != 0 {
		t.Fatalf("the table starts scrolled to %d; this test needs it at the top", d.table.RowOffset())
	}
	if d.table.Rows() <= d.table.Bounds().H {
		t.Fatalf("the table has %d rows in %d rows of height; there is nothing to scroll",
			d.table.Rows(), d.table.Bounds().H)
	}

	before := d.table.RowOffset()
	if !click(t, d, wheelAt(10, d.table.Bounds().Y+2, false)) {
		t.Fatal("a wheel notch over the table was not consumed")
	}
	scrolled := d.table.RowOffset()
	if scrolled <= before {
		t.Errorf("the wheel did not scroll the table: offset %d then %d", before, scrolled)
	}
	if d.FocusIndex() != 0 {
		t.Errorf("the wheel moved the focus to %d; it should leave it alone", d.FocusIndex())
	}

	if !click(t, d, wheelAt(10, d.table.Bounds().Y+2, true)) {
		t.Fatal("the upward wheel notch was not consumed")
	}
	if got := d.table.RowOffset(); got >= scrolled {
		t.Errorf("the wheel up did not scroll back: offset %d then %d", scrolled, got)
	}
	// And focus is STILL unmoved after the second notch, because the rule is about
	// the gesture rather than about the first event.
	if d.FocusIndex() != 0 {
		t.Errorf("the wheel moved the focus to %d; it should leave it alone", d.FocusIndex())
	}
}

// TestClickingARowSelectsItAndFocusesTheTable is the mouse's other gesture, and it
// is the composition step no widget can do alone.
//
// The click selects the row — the table's job — and the APPLICATION takes focus
// from the widget that consumed it, because a reader who clicked a row intends to
// use the arrow keys on it. Asserting both halves is what distinguishes "the mouse
// works" from "the mouse and the keyboard agree afterwards".
func TestClickingARowSelectsItAndFocusesTheTable(t *testing.T) {
	d := boardAt(t, goldenWideW, shortTableH)
	renderAt(t, goldenWideW, shortTableH, 1, d)

	if d.FocusLabel() != "pair" {
		t.Fatalf("focus starts on %q, want the chooser", d.FocusLabel())
	}
	// The FIRST body row, which is inside the table's own frame. A cell well below
	// the table would be outside every widget and would test nothing.
	body := d.table.Bounds().Y + 2
	if !click(t, d, clickAt(10, body)) {
		t.Fatal("a click inside the table was not consumed")
	}
	if d.FocusLabel() != "pairs table" {
		t.Errorf("a click on the table left the focus on %q; the click should focus it", d.FocusLabel())
	}
	if !d.table.Focused() {
		t.Error("the table is not focused after a click on it")
	}
	if got := d.table.Selected(); got != 0 {
		t.Errorf("the click on the first body row selected row %d, want 0", got)
	}
	// And the click did not stop there: the arrow keys now act on the table, which
	// is the property the focus take was for. At this height there are rows below the
	// viewport, so the selection has somewhere to go.
	before := d.table.Selected()
	press(t, d, seqDown)
	if got := d.table.Selected(); got <= before {
		t.Errorf("after clicking row %d the down arrow did not move the selection (%d)", before, got)
	}
	// Clicking a DIFFERENT row moves the selection to that row, which is what
	// distinguishes "the click hit the row the pointer was on" from "the click
	// focused the table and happened to select something". The row clicked is the
	// last VISIBLE one, so this does not depend on how many rows the viewport has:
	// at a taller screen it is a later row, at this one it is the second.
	press(t, d, seqHome)
	// The LAST visible body row, which is derived from the table's own rectangle —
	// two borders and one header come off the height — rather than written down. The
	// last row rather than the second because the last is the one a reader's
	// pointer is most likely to be over, and a click just below the viewport must
	// NOT be consumed: a hit-test that leaked into the border would select a row the
	// reader cannot see.
	visible := d.table.Bounds().H - 3
	last := visible - 1
	// Not a skip: the table's height at the golden size is the code's own output,
	// not a property of this environment. A layout change that squeezed the body
	// to one row would silently stop testing the click that must NOT be consumed,
	// which is the half of this test that has no other coverage. That is a
	// regression, and it fails here rather than skipping.
	if last < 1 {
		t.Fatalf("the table shows %d body rows at the golden size; this test needs two, "+
			"and a body that cannot show two rows cannot show the off-screen-click guard either", visible)
	}
	if !click(t, d, clickAt(10, body+last)) {
		t.Fatal("the second click was not consumed")
	}
	if got := d.table.Selected(); got != last {
		t.Errorf("a click on body row %d selected row %d", last, got)
	}
	// And the cell just below the body — the panel's own bottom border — selects
	// nothing, which is what makes the row the reader aimed at the row they got.
	if click(t, d, clickAt(10, d.table.Bounds().Bottom()-1)) {
		t.Error("a click on the table's bottom border was consumed as a row")
	}
	if got := d.table.Selected(); got != last {
		t.Errorf("the click on the border changed the selection to %d, want %d", got, last)
	}
}

// TestShortTableHIsActuallyShort keeps the constant this file's scrolling tests
// depend on honest.
//
// Without it, a change to any band's declared height could leave shortTableH tall
// enough to show every row — and then every scrolling assertion above would pass
// vacuously, which is the failure mode a test that cannot fail has.
func TestShortTableHIsScrollableAndClickable(t *testing.T) {
	d := boardAt(t, goldenWideW, shortTableH)
	renderAt(t, goldenWideW, shortTableH, 1, d)
	// Above the minimum, or the screen would be drawing the diagnostic and the table
	// would not exist at all.
	if !strings.Contains(screen(t, goldenWideW, shortTableH, 1, d), "pairs") {
		t.Fatalf("at %dx%d the screen is not drawing the table at all", goldenWideW, shortTableH)
	}
	if got, rows := d.table.Bounds().H, d.table.Rows(); got >= rows {
		t.Errorf("the table has %d rows in %d rows of height; there is nothing to scroll", rows, got)
	}
	// And at least two body rows are visible, which is what lets a click be aimed at
	// a row other than the first. Two borders and one header come off the height.
	if body := d.table.Bounds().H - 3; body < 2 {
		t.Errorf("the table shows %d body rows at %dx%d; these tests need two",
			body, goldenWideW, shortTableH)
	}
}

// TestClicksOutsideTheTableDoNotTakeFocus is the negative case, which is what stops
// the click-through from being "any click focuses the table".
func TestClicksOutsideTheTableDoNotTakeFocus(t *testing.T) {
	d := live(t)
	renderAt(t, goldenWideW, goldenWideH, 1, d)

	// Row 2 is inside the KPI band, which nothing in the ring claims.
	if click(t, d, clickAt(10, 2)) {
		t.Error("a click in the KPI row was consumed by something")
	}
	if d.FocusIndex() != 0 {
		t.Errorf("a click outside the ring moved the focus to %d", d.FocusIndex())
	}
}

// TestClickingATabChangesThePair is the chooser's mouse contract, and it is the
// reason the chooser is a Tabs widget rather than three application-invented keys.
func TestClickingATabChangesThePair(t *testing.T) {
	d := live(t)
	renderAt(t, goldenWideW, goldenWideH, 1, d)

	// The chooser row is the one above the status line, and the tabs start at its
	// left edge. The second tab is EUR's label plus the first tab's width, so the
	// click is placed by asking the widget rather than by counting cells — a test
	// that counted would break silently when a pair's name changed length.
	tabs := d.pair.Bounds()
	second := tabs.X + len(d.pair.Labels()[0]) + 2
	if !click(t, d, clickAt(second, tabs.Y)) {
		t.Fatal("a click on the second tab was not consumed")
	}
	if !d.pair.Focused() {
		t.Error("the chooser is not focused after a click on it")
	}
	if got := d.pair.SelectedLabel(); got != d.pair.Labels()[1] {
		t.Errorf("the clicked tab is %q, want %q", got, d.pair.Labels()[1])
	}
	// And the screen followed: the pair the whole dashboard is about changed.
	if got := d.market.PrimaryPair(); got != selectablePairs[1] {
		t.Errorf("the dashboard's primary pair is %q after clicking the %q tab, want %q",
			got, d.pair.Labels()[1], selectablePairs[1])
	}
}

// ---------------------------------------------------------------------------
// the pair switch
// ---------------------------------------------------------------------------

// TestPairSwitchChangesEveryPanelThatNamesThePair is the correctness test for the
// chooser, and it is the reason the chooser exists.
//
// A control that changed one label and left the sparkline drawing the old pair's
// shape would be worse than no control: the screen would be confidently wrong. So
// every panel that names or draws the pair is checked — the KPI tile's label and
// value, the sparkline's title, the meter's figure and the gauge's.
func TestPairSwitchChangesEveryPanelThatNamesThePair(t *testing.T) {
	d := live(t)
	renderAt(t, goldenWideW, goldenWideH, 1, d)

	eur := screen(t, goldenWideW, goldenWideH, 1, d)
	if !strings.Contains(eur, "EUR/USD spot") {
		t.Fatalf("the screen does not start on EUR:\n%s", eur)
	}
	if !strings.Contains(eur, "EUR/USD, last") {
		t.Errorf("the sparkline's title does not name EUR:\n%s", eur)
	}

	// Move to GBP through the KEYBOARD, not a setter, because the keyboard path is
	// the one a reader uses and it goes through the widget's own contract.
	if !press(t, d, seqRight) {
		t.Fatal("right was not consumed by the focused chooser")
	}
	if got := d.pair.SelectedLabel(); got != "GBP/USD" {
		t.Fatalf("the chooser selected %q, want GBP/USD", got)
	}

	gbp := screen(t, goldenWideW, goldenWideH, 1, d)
	if !strings.Contains(gbp, "GBP/USD spot") {
		t.Errorf("the spot tile was not relabelled:\n%s", gbp)
	}
	if strings.Contains(gbp, "EUR/USD spot") {
		t.Errorf("the spot tile still names EUR after the switch:\n%s", gbp)
	}
	if !strings.Contains(gbp, "GBP/USD, last") {
		t.Errorf("the sparkline's title was not relabelled:\n%s", gbp)
	}
	if strings.Contains(gbp, "EUR/USD, last") {
		t.Errorf("the sparkline's title still names EUR:\n%s", gbp)
	}
	// The FIGURES changed too, which is the half a label-only test would miss.
	//
	// They are asserted against the CAPTURED rate rather than a literal, so a change
	// to the fixture moves the expectation with it. And the assertion is deliberately
	// NOT "the EUR rate is gone from the screen": the table lists every tracked pair,
	// so EUR's rate legitimately stays. Asserting its absence would be asserting that
	// the table does not exist.
	gbpSpot := rateText(d.market.rates["GBP"])
	if got := rowWith(gbp, gbpSpot); got == "" {
		t.Errorf("the GBP spot rate %s is not on screen:\n%s", gbpSpot, gbp)
	}
	// And the window extremes followed, because they come from the pair's OWN window
	// rather than from the primary's — which is the whole reason market.withPair
	// swaps the series rather than only the label.
	if gbp == eur {
		t.Error("switching the pair produced an identical screen; nothing but the label moved")
	}
	// The EUR row is still in the TABLE, because the table lists every tracked pair
	// and switching the primary is not filtering it. Asserting this is what stops a
	// future edit from "simplifying" the table to follow the selection.
	if !strings.Contains(gbp, "EUR/USD ") {
		t.Errorf("the EUR row left the table when GBP became the primary:\n%s", gbp)
	}
}

// TestPairSwitchCyclesThroughEveryOfferedPair walks the whole ring, because a
// chooser that works for one step and not the third is the common failure.
func TestPairSwitchCyclesThroughEveryOfferedPair(t *testing.T) {
	d := live(t)
	renderAt(t, goldenWideW, goldenWideH, 1, d)

	// Right steps forward and CLAMPS at the last tab rather than wrapping. That is
	// the chooser widget's documented contract, and it is asserted here because the
	// obvious alternative — wrapping — would be a behaviour change to someone else's
	// widget, and a reader pressing right at the end should be told "you are at the
	// end" by the highlight staying put, not be teleported to the first pair.
	seen := map[string]bool{d.market.PrimaryPair(): true}
	for i := 0; i < len(selectablePairs)-1; i++ {
		if !press(t, d, seqRight) {
			t.Fatalf("right was not consumed at step %d", i)
		}
		got := d.market.PrimaryPair()
		if seen[got] {
			t.Errorf("right produced %q, which has already been shown; the chooser is not stepping", got)
		}
		seen[got] = true
	}
	if len(seen) != len(selectablePairs) {
		t.Errorf("%d of %d pairs were reachable; the ring does not cover the chooser",
			len(seen), len(selectablePairs))
	}
	// Past the end it holds.
	press(t, d, seqRight)
	if got := d.pair.SelectedLabel(); got != d.pair.Labels()[len(d.pair.Labels())-1] {
		t.Errorf("right past the last tab moved to %q; the chooser should clamp", got)
	}
	// And back one step, which is the other direction of the same clamp.
	press(t, d, seqLeft)
	if got, want := d.market.PrimaryPair(), selectablePairs[len(selectablePairs)-2]; got != want {
		t.Errorf("left from the last pair produced %q, want %q", got, want)
	}
	// Left at the first tab holds rather than wrapping, for the same reason.
	press(t, d, seqHome)
	press(t, d, seqLeft)
	if got := d.pair.SelectedLabel(); got != d.pair.Labels()[0] {
		t.Errorf("left past the first tab moved to %q; the chooser should clamp", got)
	}
	// And the chooser's own Home and End reach the ends of ITS list, which is the
	// widget's contract rather than the board's.
	press(t, d, seqHome)
	if got := d.pair.SelectedLabel(); got != d.pair.Labels()[0] {
		t.Errorf("home selected the tab %q, want %q", got, d.pair.Labels()[0])
	}
	press(t, d, seqEnd)
	if got := d.pair.SelectedLabel(); got != d.pair.Labels()[len(d.pair.Labels())-1] {
		t.Errorf("end did not select the last tab: %q", got)
	}
}

// TestEveryOfferedPairIsFetched guards the chooser's list against drifting away
// from trackedFX.
//
// selectablePairs is a separate list from trackedFX because it is a SUBSET chosen
// for the screen, and a subset is exactly the kind of thing that drifts: a currency
// added to the fetch list and forgotten in the chooser is invisible, and one added
// to the chooser and not to the fetch list is a tab that can select a pair with no
// rates at all.
func TestEveryOfferedPairIsFetched(t *testing.T) {
	fetched := map[string]bool{}
	for _, c := range trackedFX {
		fetched[c] = true
	}
	for _, c := range selectablePairs {
		if !fetched[c] {
			t.Errorf("the chooser offers %s but trackedFX does not fetch it", c)
		}
	}
	// And the default pair must be one of them, or the chooser's initial selection
	// would be index 0 pointing at a pair the screen never intended to show.
	if _, ok := pairTabIndex(primaryPair); !ok {
		t.Errorf("the default pair %s is not in selectablePairs %v", primaryPair, selectablePairs)
	}
	if got := d0().FocusIndex(); got != 0 {
		t.Errorf("focus starts at %d, want 0", got)
	}
}

// d0 is a board at the wide size with no data, for the construction-only
// assertions.
func d0() *dashboard { return newDashboard(goldenWideW, goldenWideH) }

// TestPairSwitchWithNoWindowSaysSoRatherThanDrawingAnotherPairsShape is the
// honesty case, and it is the reason market.withPair empties the window rather
// than falling back.
//
// A currency with no series would otherwise keep EUR's history and draw it under a
// "GBP/USD" title. The screen must instead report the absence: the series band is
// dropped by the height budget's own rule (hasSeries is false), and the window
// extremes read as absent.
func TestPairSwitchWithNoWindowSaysSoRatherThanDrawingAnotherPairsShape(t *testing.T) {
	d := live(t)
	renderAt(t, goldenWideW, goldenWideH, 1, d)

	// A pair that IS in the chooser but has no series in the fixture: the offline
	// sample windows EUR, GBP and JPY, so this drives the rule through the market
	// rather than through the board — which is where the rule lives.
	m := d.market.withPair("AUD")
	// A legitimate skip, and the only one of its kind in this file: the condition
	// is a property of the BUNDLED SAMPLE DATA, not of the code's geometry or of
	// the environment running the test. If the fixture ever grows an AUD window
	// then there is no "pair with no window" left to assert against, and the
	// case is genuinely not applicable — the golden would then be what covers it.
	// Unlike a geometry skip, this one cannot be triggered by a layout change.
	if _, ok := m.series["AUD"]; ok {
		t.Skip("the bundled offline fixture now carries an AUD window, so there is no " +
			"series-less pair for this case; the golden covers that instead")
	}
	if m.hasHistory() {
		t.Error("a pair with no window reports a history; it would draw another pair's shape")
	}
	if _, _, ok := m.extremes(); ok {
		t.Error("a pair with no window reports extremes")
	}
	// And the frame built from it says so rather than showing a figure.
	f := buildFrame(&snapshot{m: m, at: at, source: "offline", ok: true, gen: 2})
	if f.series != nil {
		t.Errorf("the frame carries a series for a pair with no window: %v", f.series)
	}
	if f.tiles[tileHigh].value != noDataPlaceholder {
		t.Errorf("the window high reads %q for a pair with no window, want %q",
			f.tiles[tileHigh].value, noDataPlaceholder)
	}
	// The window shape is not the only thing that must not leak: the LABEL follows
	// the pair even when there is no window, so the reader is told which pair the
	// screen is about rather than being left with EUR's name and no data.
	if f.pair != "AUD" {
		t.Errorf("the frame's pair is %q, want AUD", f.pair)
	}
}

// ---------------------------------------------------------------------------
// the pause
// ---------------------------------------------------------------------------

// TestPauseTogglesAndSaysSo covers the whole pause contract: the flag, the
// schedule's dependency on it, and the visible consequence.
//
// The visible consequence is the part worth a test. A dashboard that stops
// updating without saying why is indistinguishable from one that has hung, and the
// only thing standing between the two is the footer's word.
func TestPauseTogglesAndSaysSo(t *testing.T) {
	d := live(t)
	closed := screen(t, goldenWideW, goldenWideH, 1, d)
	if strings.Contains(closed, "PAUSED") {
		t.Fatalf("a running screen says PAUSED:\n%s", closed)
	}

	if !press(t, d, " ") {
		t.Fatal("space was not consumed")
	}
	if !d.Paused() {
		t.Fatal("space did not pause")
	}
	paused := screen(t, goldenWideW, goldenWideH, 1, d)
	if !strings.Contains(paused, "PAUSED") {
		t.Errorf("a paused screen does not say so:\n%s", paused)
	}
	// The hint names the ACTION rather than the key, which is what makes it usable
	// without prior knowledge.
	if !strings.Contains(paused, "resume") {
		t.Errorf("the key hint does not offer to resume:\n%s", paused)
	}
	if strings.Contains(paused, "pause") && !strings.Contains(paused, "[space] resume") {
		t.Logf("the hint still offers to pause:\n%s", paused)
	}

	if !press(t, d, " ") {
		t.Fatal("the second space was not consumed")
	}
	if d.Paused() {
		t.Fatal("the second space did not resume")
	}
	resumed := screen(t, goldenWideW, goldenWideH, 1, d)
	if strings.Contains(resumed, "PAUSED") {
		t.Errorf("a resumed screen still says PAUSED:\n%s", resumed)
	}
}

// TestPauseDoesNotChangeTheData pins what the pause must NOT do.
//
// A pause is about the SCHEDULE. If it also stopped the manual 'r' — the whole
// point of which is to fetch without waiting — then a paused screen could never be
// brought up to date, and the control would be a trap rather than a pause.
func TestPauseDoesNotChangeTheData(t *testing.T) {
	d := live(t)
	renderAt(t, goldenWideW, goldenWideH, 1, d)
	before := screen(t, goldenWideW, goldenWideH, 1, d)

	asked := 0
	d.OnRefresh = func() { asked++ }
	press(t, d, " ")
	if !d.Paused() {
		t.Fatal("space did not pause")
	}
	if !press(t, d, "r") {
		t.Fatal("'r' was not consumed while paused")
	}
	if asked != 1 {
		t.Errorf("'r' asked for %d fetches while paused, want 1", asked)
	}

	after := screen(t, goldenWideW, goldenWideH, 1, d)
	// The BANDS are compared, not the whole screen: the footer gains "PAUSED" and
	// the key hint's word changes from "pause" to "resume", both of which are the
	// pause showing itself and neither of which is the data. Everything above the
	// chrome must be identical.
	if bandsOf(after, goldenWideH) != bandsOf(before, goldenWideH) {
		t.Errorf("pausing changed the data.\n--- before ---\n%s\n--- after ---\n%s", before, after)
	}
}

// bandsOf returns everything above the chrome rows of an h-row screen.
//
// It is the comparison the pause needs: the two chrome rows are SUPPOSED to change,
// and the bands above them are not.
func bandsOf(screen string, h int) string {
	lines := strings.Split(screen, "\n")
	if len(lines) < chromeRows {
		return screen
	}
	return strings.Join(lines[:len(lines)-chromeRows], "\n")
}

// TestSpaceIsNotSwallowedByTheFocusedWidget is the composition rule from the other
// side: 'space' is the APPLICATION's key, so it works with the table focused too.
//
// The table's own contract includes KeySpace as activate, so a routing order that
// offered keys to the focused widget first would let a form's activate key win
// over the pause — and the two would then be mutually exclusive depending on where
// the focus happened to be. This is the test that says they are not.
func TestSpaceIsNotSwallowedByTheFocusedWidget(t *testing.T) {
	d := live(t)
	focusedTable(t, d)
	if !press(t, d, " ") {
		t.Fatal("space was not consumed while the table had focus")
	}
	if !d.Paused() {
		t.Error("space did not pause while the table had focus")
	}
}

// ---------------------------------------------------------------------------
// the help overlay
// ---------------------------------------------------------------------------

// TestQuestionMarkTogglesTheHelpOverlay drives the overlay through the decoder and
// checks it on the SCREEN, including that closing it leaves nothing behind.
//
// The last part is the one worth the test: an overlay drawn over the bands and then
// simply not drawn leaves its cells in the buffer, because the renderer diffs and
// never clears. The block repaints its whole rectangle when it is drawn, so this
// passes only if the bands are fully repainted underneath it on the frame the help
// closes.
func TestQuestionMarkTogglesTheHelpOverlay(t *testing.T) {
	d := live(t)
	buf := buffer.NewBuffer(goldenWideW, goldenWideH)
	d.SetBounds(buffer.Rect{W: goldenWideW, H: goldenWideH})
	d.Draw(buf)
	closed := rowsOf(buf, goldenWideW, goldenWideH)

	if d.HelpOpen() {
		t.Fatal("the help starts open")
	}
	for _, gone := range []string{"page · home / end", "scroll the pairs table"} {
		if strings.Contains(closed, gone) {
			t.Errorf("the help body %q is on screen while the help is closed", gone)
		}
	}

	if !press(t, d, "?") {
		t.Fatal("'?' was not consumed")
	}
	if !d.HelpOpen() {
		t.Fatal("'?' did not open the help")
	}
	d.Draw(buf)
	open := rowsOf(buf, goldenWideW, goldenWideH)
	for _, want := range []string{
		"tab / shift-tab", "space", "quit", "scroll the pairs table", "click a row",
	} {
		if !strings.Contains(open, want) {
			t.Errorf("the open overlay does not mention %q:\n%s", want, open)
		}
	}
	// The overlay is over the bands: the table's own title is hidden behind it,
	// which is what "overlay" means and is asserted so a future edit cannot make it
	// a panel that merely sits below the dashboard.
	if strings.Contains(open, "largest moves, |bps|") {
		t.Logf("the overlay does not cover the whole detail band at this size")
	}

	if !press(t, d, "?") {
		t.Fatal("the second '?' was not consumed")
	}
	d.Draw(buf)
	again := rowsOf(buf, goldenWideW, goldenWideH)
	if again != closed {
		t.Errorf("closing the help left cells behind.\n--- closed ---\n%s\n--- again ---\n%s",
			closed, again)
	}
}

// TestHelpOverlayClosesOnTheKeysItNames pins the dismiss keys, because a modal
// that a reader cannot get out of with the keyboard is a modal they solve by killing
// the terminal.
//
// Escape and Enter both dismiss, and so does '?' — and 'q' does NOT, because 'q'
// quits the program and swallowing it here would turn "quit" into "close a panel" for
// as long as the help was open. That distinction is the reason the test says so.
func TestHelpOverlayClosesOnTheKeysItNames(t *testing.T) {
	d := live(t)
	renderAt(t, goldenWideW, goldenWideH, 1, d)

	// '?' and Enter decode from bytes. Escape does NOT: a lone ESC is ambiguous with
	// a sequence prefix until the decoder has waited out its delay, so the event a
	// running program receives after that delay is built directly. The reason is
	// stated at each case rather than once here, because it is the decoder's
	// documented behaviour and a reader of this test should know which half of it is
	// a shortcut.
	for _, seq := range []string{"?", "\r"} {
		open := live(t)
		renderAt(t, goldenWideW, goldenWideH, 1, open)
		if !press(t, open, "?") {
			t.Fatalf("'?' was not consumed")
		}
		if !open.HelpOpen() {
			t.Fatalf("'?' did not open the help")
		}
		if !open.Handle(decode(t, seq)) {
			t.Errorf("%q was not consumed while the help was open", seq)
		}
		if open.HelpOpen() {
			t.Errorf("the help stayed open after %q", seq)
		}
	}

	esc := live(t)
	renderAt(t, goldenWideW, goldenWideH, 1, esc)
	press(t, esc, "?")
	escEv := termmosaic.SpecialKeyEvent(termmosaic.KeyEscape, 0)
	if !esc.Handle(escEv) {
		t.Error("escape was not consumed while the help was open")
	}
	if esc.HelpOpen() {
		t.Error("the help stayed open after escape")
	}

	// 'q' while the help is open still reaches the caller: the board does not
	// consume it as a dismissal, so isQuitKey sees it and the program exits.
	open := live(t)
	renderAt(t, goldenWideW, goldenWideH, 1, open)
	press(t, open, "?")
	quitEv := key(t, "q")
	if open.Handle(quitEv) != true {
		t.Error("'q' was not consumed while the help was open")
	}
	if !isQuitKey(quitEv) {
		t.Error("'q' with the help open is not a quit key, so the program would not exit")
	}
	if !open.HelpOpen() {
		t.Error("'q' closed the help; it should have quit the program instead")
	}
}

// TestTheMouseIsNotModalUnderTheHelp is the overlay's mouse rule, and it is a
// design decision rather than an accident.
//
// The help swallows KEYS but not MOUSE, because a reader who has opened a help
// panel and then clicks a table row expects the row to be selected. An overlay that
// took the mouse too would be strictly worse than no overlay at all for the case
// where a reader opened it by accident.
func TestTheMouseIsNotModalUnderTheHelp(t *testing.T) {
	d := live(t)
	renderAt(t, goldenWideW, goldenWideH, 1, d)
	if !press(t, d, "?") {
		t.Fatal("'?' was not consumed")
	}
	if !d.HelpOpen() {
		t.Fatal("'?' did not open the help")
	}

	if !click(t, d, "\x1b[<0;10;32M") {
		t.Error("a click on the table was not consumed while the help was open")
	}
	if d.FocusLabel() != "pairs table" {
		t.Errorf("the click did not take focus while the help was open; focus is on %q", d.FocusLabel())
	}
	// And the help is STILL open, because taking focus is not dismissing it.
	if !d.HelpOpen() {
		t.Error("the click closed the help; the overlay is meant to be non-modal for the mouse")
	}
}

// ---------------------------------------------------------------------------
// mouse capture
// ---------------------------------------------------------------------------

// TestMouseCaptureIsEnabledForThisExample is the positive half of the guarantee,
// and it asserts the CONFIG rather than the effect.
//
// The effect — a click scrolling the table — is asserted elsewhere and would pass
// with capture off, because these tests build their events directly. Only the
// config says whether the terminal is actually reporting the mouse, so this is the
// assertion that would catch the example quietly dropping the one line that takes
// the user's selection away.
func TestMouseCaptureIsEnabledForThisExample(t *testing.T) {
	if got := marketsInputConfig(headless.NewMemorySink(80, 24)).MouseMode; got != input.MouseClick {
		t.Errorf("the example's mouse mode is %v, want MouseClick; ADR 0005 leaves capture off "+
			"by default and this example is the one that opts in", got)
	}
	// And the brackets-paste and kitty defaults are NOT disturbed by opting in:
	// turning mouse reporting on should not quietly change anything else.
	cfg := marketsInputConfig(headless.NewMemorySink(80, 24))
	def := input.DefaultConfig()
	if cfg.BracketedPaste != def.BracketedPaste || cfg.ProbeKitty != def.ProbeKitty {
		t.Errorf("opting into mouse capture changed another mode: %+v against the defaults %+v", cfg, def)
	}
}

// TestMouseCaptureIsRestoredOnExit is the half that matters to the user's SHELL,
// and it is the reason the teardown is a named function.
//
// It drives the real input.Source over a headless terminal with a recording sink,
// exactly as marketsInputConfig wires it in run, and asserts that:
//
//  1. capture was enabled — the terminal was asked to report the mouse, and
//  2. teardown disabled it again.
//
// A program that enables mouse capture and quits without disabling it leaves the
// user's shell with no text selection, no middle-click paste and no scrollback
// copying, and nothing on screen to say why. That is the single worst thing this
// example could do, so it is asserted against the byte stream rather than against
// a struct field.
func TestMouseCaptureIsRestoredOnExit(t *testing.T) {
	rec := headless.NewMemorySink(80, 24)
	ht := headless.New(80, 24)
	src := input.NewSource(ht, marketsInputConfig(rec))
	// The terminal must be closed BEFORE the Source: Close deliberately does not
	// close a terminal it was only reading, so the reader would otherwise still be
	// blocked in Read when the test ends.
	t.Cleanup(func() { _ = ht.Close() })

	enabled := rec.RawString()
	for _, want := range []string{"?1000h", "?1006h"} {
		if !strings.Contains(enabled, want) {
			t.Fatalf("mouse capture was not enabled: %q is missing from %q", want, enabled)
		}
	}
	for _, unwanted := range []string{"?1000l", "?1006l"} {
		if strings.Contains(enabled, unwanted) {
			t.Errorf("the disable sequence %q was written while the program was still running", unwanted)
		}
	}

	teardown(render.New(rec, render.Config{Width: 80, Height: 24, Caps: termmosaic.DefaultCaps()}), ht, src)

	restored := rec.RawString()
	for _, want := range []string{"?1000l", "?1006l", "?2004l"} {
		if !strings.Contains(restored, want) {
			t.Errorf("the disable sequence %q is missing from %q; a TUI that leaves the terminal "+
				"in a mode it turned on has broken the user's shell", want, restored)
		}
	}
	// And the order: the disable must come AFTER the enable, which is the only
	// thing that makes it a restoration rather than a coincidence.
	if strings.Index(restored, "?1000h") > strings.Index(restored, "?1000l") {
		t.Error("mouse capture was disabled before it was enabled")
	}
}

// TestTeardownIsSafeToCallTwice is the idempotence the deferred-plus-explicit
// pattern in run depends on.
//
// teardown is called on the normal path and could be reached again by a future edit
// that adds a defer, and a second call must not write the disable sequences twice
// or, worse, leave raw mode twice. input.Source.Close is documented as idempotent
// and Terminal.LeaveRawMode tracks its own state; this test is what says the
// COMPOSITION is, rather than each half being individually fine.
func TestTeardownIsSafeToCallTwice(t *testing.T) {
	rec := headless.NewMemorySink(80, 24)
	ht := headless.New(80, 24)
	src := input.NewSource(ht, marketsInputConfig(rec))
	r := render.New(rec, render.Config{Width: 80, Height: 24, Caps: termmosaic.DefaultCaps()})
	if err := ht.EnterRawMode(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ht.Close() })

	teardown(r, ht, src)
	first := strings.Count(rec.RawString(), "?1000l")
	teardown(r, ht, src)
	second := strings.Count(rec.RawString(), "?1000l")

	if first != 1 {
		t.Errorf("the first teardown wrote the mouse disable %d times, want 1", first)
	}
	if second != first {
		t.Errorf("a second teardown wrote the mouse disable again: %d then %d", first, second)
	}
}

// ---------------------------------------------------------------------------
// the unbindable
// ---------------------------------------------------------------------------

// TestQuitKeysAreConsumedAndNotSwallowedByTheOverlay pins the three quit keys on
// the board, alongside the function that actually exits.
//
// isQuitKey is checked in the same loop on purpose: the board consumes 'q' so a
// form never sees it, and the input loop tests isQuitKey BEFORE offering the event
// to the board. If either half changed order the other would break, and asserting
// both together is what keeps the pair honest.
func TestQuitKeysAreConsumedAndNotSwallowedByTheOverlay(t *testing.T) {
	for _, seq := range []string{"q", "Q"} {
		d := live(t)
		ev := key(t, seq)
		if !d.Handle(ev) {
			t.Errorf("%q was not consumed by the board", seq)
		}
		if !isQuitKey(ev) {
			t.Errorf("%q is not a quit key for run", seq)
		}
	}
	// Escape does not decode from a lone ESC: the decoder waits out its ambiguity
	// delay to find out whether the byte begins a sequence, so a bare ESC comes back
	// incomplete. The event a real terminal delivers after that delay is the one
	// below.
	d := live(t)
	esc := termmosaic.SpecialKeyEvent(termmosaic.KeyEscape, 0)
	if !d.Handle(esc) {
		t.Error("escape was not consumed by the board")
	}
	if !isQuitKey(esc) {
		t.Error("escape is not a quit key for run")
	}
	// Ctrl-C arrives as Ctrl+'c' rather than as 0x03, which is the entire reason the
	// raw-byte spelling could not work.
	d = live(t)
	ctrlC := termmosaic.Event{Kind: termmosaic.EventKey, Rune: 'c', Mod: termmosaic.ModCtrl}
	if !d.Handle(ctrlC) {
		t.Error("ctrl-c was not consumed by the board")
	}
	if !isQuitKey(ctrlC) {
		t.Error("ctrl-c is not a quit key for run")
	}
}

// TestUnboundKeysAreNotConsumed is the honesty rule, asserted because it is
// invisible otherwise.
//
// A board that consumed everything would sit at the root of every application and
// silently eat the application's own keys. The only way to notice is to press a key
// the example does not bind and check that it comes back unhandled.
func TestUnboundKeysAreNotConsumed(t *testing.T) {
	d := live(t)
	focusedTable(t, d)
	for _, seq := range []string{"x", "j", "z", "\x7f"} {
		if press(t, d, seq) {
			t.Errorf("%q was consumed by a key the example does not bind", seq)
		}
	}
	// A modified rune is a chord rather than the bare key.
	if d.Handle(termmosaic.KeyEvent('r', termmosaic.ModCtrl)) {
		t.Error("ctrl-r was consumed as if it were the fetch key")
	}
	// A paste is not consumed either: this example has no text field, and claiming a
	// paste would mean swallowing a reader's clipboard with nothing to show for it.
	if d.Handle(termmosaic.Event{Kind: termmosaic.EventPaste, Text: "hello"}) {
		t.Error("a paste was consumed by a screen with no text field")
	}
}

// TestResizeIsHandledAndKeepsTheLayout is the resize path through the board rather
// than through the caller, because main.go now routes it through Handle.
//
// The assertion is that the bands still draw after the resize — a resize that
// reached SetBounds but not the renderer would leave a board whose layout is right
// and whose screen is not.
func TestResizeIsHandledAndKeepsTheLayout(t *testing.T) {
	d := live(t)
	renderAt(t, goldenWideW, goldenWideH, 1, d)
	wide := screen(t, goldenWideW, goldenWideH, 1, d)

	if !d.Handle(termmosaic.ResizeEvent(goldenNarrowW, goldenNarrowH)) {
		t.Fatal("a resize was not consumed")
	}
	narrow := renderAt(t, goldenNarrowW, goldenNarrowH, 1, d)
	got := widgettest.Screen(narrow)
	if got == wide {
		t.Error("the resize did not change the screen")
	}
	if !strings.Contains(got, "EUR/USD spot") {
		t.Errorf("the narrow screen lost the KPI band:\n%s", got)
	}
	// The chrome survives the resize, which is the part a band-only assertion misses.
	if !strings.Contains(got, "[EUR/USD]") {
		t.Errorf("the pair chooser is gone at the narrow size:\n%s", got)
	}
	if !strings.Contains(got, "markets") {
		t.Errorf("the status line is gone at the narrow size:\n%s", got)
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// rowsOf renders a bare buffer's rows with trailing blanks trimmed, so two frames
// can be compared exactly.
//
// A bare buffer rather than a renderer, because the property under test is what
// the WIDGET wrote: the renderer's diff would legitimately skip an unchanged cell
// and the overlay's "nothing left behind" question is about the widget's output,
// not about the encoder's.
func rowsOf(buf *buffer.Buffer, w, h int) string {
	rows := make([]string, h)
	for y := 0; y < h; y++ {
		var b strings.Builder
		row := buf.Row(y)
		for x := 0; x < w && x < len(row); x++ {
			b.WriteRune(row[x].Ch)
		}
		rows[y] = strings.TrimRight(b.String(), " ")
	}
	return strings.Join(rows, "\n")
}
