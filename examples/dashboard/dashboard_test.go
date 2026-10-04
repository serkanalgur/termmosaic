package main

import (
	"strings"
	"testing"

	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/widgets/widgettest"
)

// screen renders the board and returns its rows with trailing blanks trimmed.
func screen(t *testing.T, w, h int, board *dashboard) []string {
	t.Helper()
	sink := widgettest.Render(t, w, h, 1, board)
	out := make([]string, h)
	for y := 0; y < h; y++ {
		out[y] = widgettest.Row(sink, y)
	}
	return out
}

func TestDashboardRendersEveryWidgetAtAWorkableSize(t *testing.T) {
	// 120x30 is the size this screen is designed for, and the size at which all nine
	// widgets have room: the assertion is that every one of them is VISIBLE, which
	// is what a catalog screenshot is.
	board := newDashboard(120, 30)
	rows := screen(t, 120, 30, board)
	joined := strings.Join(rows, "\n")

	for _, want := range []string{
		// the three data widgets, by their Block titles
		"requests", "recent requests", "services", "log",
		// the table's header row, as far as the viewport reaches
		"id", "method", "path",
		// the tree's expansion glyphs
		"+", "-",
		// the pager's status readout
		"Ln 1/",
		// the progress bar's label and its number
		"deploy", "%",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("the screen does not show %q:\n%s", want, joined)
		}
	}
	// And something is drawn on most rows: an empty screen would satisfy every
	// assertion above only if the assertions were empty, so this is the sanity check
	// that they are not.
	filled := 0
	for _, r := range rows {
		if strings.TrimSpace(r) != "" {
			filled++
		}
	}
	if filled < 25 {
		t.Errorf("only %d of %d rows have content; the screen is mostly empty", filled, len(rows))
	}
}

func TestDashboardSurvivesEveryDegenerateSize(t *testing.T) {
	// ADR 0007 §4: a widget tree must be total for every rect, including 0x0 and a
	// size below every widget's minimum. The whole screen at once is the case that
	// matters: nine widgets each defensively total can still panic in composition.
	for _, size := range []buffer.Size{
		{W: 0, H: 0}, {W: 0, H: 24}, {W: 80, H: 0},
		{W: 1, H: 1}, {W: 2, H: 2}, {W: 5, H: 3}, {W: 12, H: 4}, {W: 40, H: 6},
		{W: 200, H: 60},
	} {
		board := newDashboard(size.W, size.H)
		buf := buffer.NewBuffer(size.W, size.H)
		board.Draw(buf)
		if size.W > 0 && size.H > 0 {
			// Through the whole stack as well, which is where the diff and the
			// encoder meet the composition.
			board.SetBounds(buffer.Rect{W: size.W, H: size.H})
			widgettest.Render(t, size.W, size.H, 2, board)
		}
	}
}

func TestDashboardGrowsAndShrinksWithoutStaleCells(t *testing.T) {
	// A resize test that only grows cannot catch the stale-cell bug, so this one
	// shrinks from wide to narrow and back, and checks both the screen and the
	// widgets' own cells.
	board := newDashboard(140, 34)
	wide := screen(t, 140, 34, board)

	board.SetBounds(buffer.Rect{W: 60, H: 18})
	narrow := screen(t, 60, 18, board)
	for y := range narrow {
		if len([]rune(narrow[y])) > 60 {
			t.Errorf("row %d is %d cells wide after shrinking to 60: %q", y, len([]rune(narrow[y])), narrow[y])
		}
	}
	if strings.Join(wide, "") == strings.Join(narrow, "") {
		t.Error("shrinking the screen changed nothing; the layout did not adapt")
	}

	// Growing back must restore the wide layout rather than keep the narrow one.
	board.SetBounds(buffer.Rect{W: 140, H: 34})
	again := screen(t, 140, 34, board)
	if strings.Join(again, "") != strings.Join(wide, "") {
		t.Error("growing back did not restore the previous layout")
	}
}

func TestDashboardDrawIsAllocationFree(t *testing.T) {
	// The frame path of the whole screen: nine widgets, one buffer. If any of them
	// rebuilt something per frame this is where it would show.
	board := newDashboard(120, 30)
	buf := buffer.NewBuffer(120, 30)
	for i := 0; i < 3; i++ {
		board.Tick()
		board.Draw(buf)
	}
	if got := testing.AllocsPerRun(50, func() { board.Draw(buf) }); got != 0 {
		t.Errorf("the dashboard frame allocated %v times; the frame path must be free", got)
	}
	// And with the live widgets moving, because a Tick that made Draw allocate would
	// be the worst possible place for it.
	if got := testing.AllocsPerRun(50, func() {
		board.Tick()
		board.Draw(buf)
	}); got != 0 {
		t.Errorf("a ticking frame allocated %v times", got)
	}
}

func TestDashboardTickMovesTheLiveWidgets(t *testing.T) {
	// The two frames must differ: a heartbeat that changed nothing would make every
	// frame of the running example identical, and the pacer would have nothing to
	// draw.
	board := newDashboard(120, 30)
	before := screen(t, 120, 30, board)
	board.Tick()
	board.Tick()
	after := screen(t, 120, 30, board)
	if strings.Join(before, "") == strings.Join(after, "") {
		t.Error("two ticks produced an identical frame; nothing on the screen is live")
	}
}

func TestDashboardTabMovesFocusAndNoWidgetSwallowsIt(t *testing.T) {
	// The focus ring is the application's, and it works only because none of the
	// four focusable widgets consumes KeyTab. If one did, this would be the test
	// that said so.
	board := newDashboard(120, 30)
	if board.FocusIndex() != 0 {
		t.Fatalf("focus starts at %d, want 0", board.FocusIndex())
	}
	for i := 0; i < 4; i++ {
		if !board.Handle(termmosaic.SpecialKeyEvent(termmosaic.KeyTab, 0)) {
			t.Fatalf("tab was not consumed by the application at step %d", i)
		}
		if got := board.FocusIndex(); got != (i+1)%4 {
			t.Fatalf("after %d tabs focus is %d, want %d", i+1, got, (i+1)%4)
		}
	}
	// Four tabs from the list is a full turn of the ring, so the list is focused
	// again — which is what makes the cycle observable rather than a dead end.
	if !board.list.Focused() {
		t.Error("the list is not focused after four tabs")
	}
	// The focused widget consumes its own keys.
	if !board.Handle(termmosaic.SpecialKeyEvent(termmosaic.KeyDown, 0)) {
		t.Error("the focused widget did not consume its own navigation key")
	}
	if !board.Handle(termmosaic.SpecialKeyEvent(termmosaic.KeyTab, 0)) {
		t.Error("tab stopped being consumed once a widget had focus")
	}
}

func TestDashboardSearchGoesToThePagerNotIntoIt(t *testing.T) {
	// The read-only widget cannot receive typed text, so the application owns the
	// mode and hands the pager a finished string. This is the composition contract
	// the Pager's SetQuery implies, and it is what this test pins.
	board := newDashboard(120, 30)
	if !board.Handle(termmosaic.KeyEvent('/', 0)) {
		t.Fatal("'/' was not consumed by the application")
	}
	for _, r := range "503" {
		if !board.Handle(termmosaic.KeyEvent(r, 0)) {
			t.Fatalf("rune %q was not consumed while searching", r)
		}
	}
	if !board.Handle(termmosaic.SpecialKeyEvent(termmosaic.KeyEnter, 0)) {
		t.Fatal("enter was not consumed while searching")
	}
	if got := board.pager.Query(); got != "503" {
		t.Errorf("the pager's query is %q, want %q", got, "503")
	}
	if board.pager.Matches() == 0 {
		t.Error("the query matches nothing in the log")
	}
	// Escape clears the mode and the query.
	if !board.Handle(termmosaic.KeyEvent('/', 0)) {
		t.Fatal("'/' was not consumed the second time")
	}
	if !board.Handle(termmosaic.SpecialKeyEvent(termmosaic.KeyEscape, 0)) {
		t.Fatal("escape was not consumed")
	}
	if board.pager.Query() != "" {
		t.Errorf("escape left the query as %q", board.pager.Query())
	}
}

func TestDashboardRequestListIsVirtualised(t *testing.T) {
	// The list holds a hundred thousand items, and the screen still renders. This is
	// the claim the whole virtual/ package exists for, exercised through a composed
	// screen rather than a single widget.
	board := newDashboard(120, 30)
	if got := board.list.Items(); got != requestCount {
		t.Fatalf("the list holds %d items, want %d", got, requestCount)
	}
	rows := screen(t, 120, 30, board)
	joined := strings.Join(rows, "\n")
	if !strings.Contains(joined, "000000 GET") {
		t.Errorf("the first request is not on screen:\n%s", joined)
	}
	// End takes the list to the far end of a hundred thousand items, which is the
	// arithmetic virtual/ owns.
	board.Handle(termmosaic.SpecialKeyEvent(termmosaic.KeyTab, 0))
	board.Handle(termmosaic.SpecialKeyEvent(termmosaic.KeyTab, 0))
	board.Handle(termmosaic.SpecialKeyEvent(termmosaic.KeyTab, 0))
	board.Handle(termmosaic.SpecialKeyEvent(termmosaic.KeyTab, 0))
	if !board.Handle(termmosaic.SpecialKeyEvent(termmosaic.KeyEnd, 0)) {
		t.Fatal("end was not consumed by the focused pager")
	}
	board.Handle(termmosaic.SpecialKeyEvent(termmosaic.KeyTab, 0))
	if !board.Handle(termmosaic.SpecialKeyEvent(termmosaic.KeyEnd, 0)) {
		t.Fatal("end was not consumed by the focused list")
	}
	if got := board.list.Selected(); got != requestCount-1 {
		t.Errorf("after End the list selected %d, want %d", got, requestCount-1)
	}
}

func TestDashboardTableScrollsHorizontallyThroughTheScreen(t *testing.T) {
	// The table's columns are wider than the column it is given, so the last two are
	// off-screen until it scrolls. Proving that through a COMPOSED screen — rather
	// than by calling the table directly — is what shows that the horizontal
	// scrolling survives being laid out.
	board := newDashboard(120, 30)
	before := strings.Join(screen(t, 120, 30, board), "\n")
	if strings.Contains(before, "status") {
		t.Skip("every column fits at this size; there is nothing to scroll")
	}
	if !board.Handle(termmosaic.SpecialKeyEvent(termmosaic.KeyTab, 0)) {
		t.Fatal("tab was not consumed")
	}
	// Shift+End is the documented "jump to the last column", and it is the honest
	// thing to test: pressing Right once moves by one column, which is three cells,
	// and asserting a specific column came into view would be asserting the widths.
	if !board.Handle(termmosaic.SpecialKeyEvent(termmosaic.KeyRight, 0)) {
		t.Fatal("right was not consumed by the focused table")
	}
	stepped := strings.Join(screen(t, 120, 30, board), "\n")
	if stepped == before {
		t.Fatal("one step of horizontal scrolling changed nothing")
	}
	if !board.Handle(termmosaic.SpecialKeyEvent(termmosaic.KeyEnd, 0x1)) {
		t.Fatal("shift+end was not consumed by the focused table")
	}
	after := strings.Join(screen(t, 120, 30, board), "\n")
	if after == stepped {
		t.Fatal("jumping to the last column changed nothing")
	}
	for _, col := range []string{"status", "ms"} {
		if !strings.Contains(after, col) {
			t.Errorf("column %q did not come into view:\n%s", col, after)
		}
	}
}
