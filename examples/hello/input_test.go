package main

// The input tests: the key contract, driven through the REAL decoder and through
// the REAL registry.
//
// Every keyboard assertion here goes through input.Decode rather than a
// hand-built termmosaic.Event, which is the idiom widgets/data/eventtest_test.go
// established and the reason it is worth copying: a hand-built Event can describe
// something no terminal would ever send, and a test that passes on it proves
// nothing about the program. Decoding "\x1b[C" gives the same KeyRight the
// decoder gives a real arrow key, and it fails the moment the decoder changes.
//
// The mouse and resize cases are hand-built, because there is no byte sequence
// this example acts on: it claims no mouse events at all, and a resize is the
// application's job rather than the widget's.
//
// Every key goes through km.Dispatch rather than through a switch in the test,
// because that is where the program sends it. A test that called the widget's
// Handle would be testing a method that now claims nothing, which is a test that
// passes for the wrong reason.

import (
	"strings"
	"testing"

	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/input"
	"github.com/serkanalgur/termmosaic/keymap"
	"github.com/serkanalgur/termmosaic/widgets/form"
	"github.com/serkanalgur/termmosaic/widgets/widgettest"
)

// noQuit is the quit handler for a widget under test that must not quit.
//
// It is the default rather than a recorder because most of these tests are about
// the focus ring, and a recorder's field would be one more thing every one of
// them had to know about. TestQuitKeysAreConsumed builds its own harness with a
// recorder, because that is the test where quitting is the subject.
func noQuit() {}

// app is the example as run: the widget plus the registry the event loop
// dispatches through. It embeds the widget so the tests read as they did when the
// widget owned the whole key contract.
type app struct {
	*hello
	// quits counts app.quit runs.
	quits int
}

// newApp builds the example with a counting quit handler.
func newApp(t *testing.T, w, h int) *app {
	t.Helper()
	a := &app{}
	a.hello = newHello(rootBounds(w, h), buffer.DepthTrueColor, func() { a.quits++ })
	return a
}

// press offers one decoded key sequence to the program and reports whether it was
// consumed.
//
// It is ADR 0009 §2.2's loop reduced to the two lines that can change anything:
// the keymap first, the tree only if nothing claimed it. The focus argument is
// the root widget, which is what run passes.
func (a *app) press(t *testing.T, seq string) bool {
	t.Helper()
	ev := decodeKey(t, seq)
	if _, consumed := a.hello.km.Dispatch(ev, a.hello); consumed {
		return true
	}
	return a.hello.Handle(ev)
}

// decodeKey runs one key sequence through the real decoder and returns the event
// it produced, failing the test if the sequence is not a single event.
func decodeKey(t *testing.T, seq string) termmosaic.Event {
	t.Helper()
	ev, n, status := input.Decode([]byte(seq), input.DefaultConfig())
	if status != input.StatusOK {
		t.Fatalf("decoding %q: status %v, want ok", seq, status)
	}
	if n != len(seq) {
		t.Errorf("decoding %q consumed %d bytes of %d", seq, n, len(seq))
	}
	if ev.Kind != termmosaic.EventKey {
		t.Fatalf("decoding %q produced a %v, want a key", seq, ev.Kind)
	}
	return ev
}

// The escape sequences for the keys this example binds. Written as the ACTUAL
// bytes rather than as decoder calls, because the point is that the decoder agrees
// with the registry about what those bytes mean.
const (
	seqUp     = "\x1b[A"
	seqDown   = "\x1b[B"
	seqRight  = "\x1b[C"
	seqLeft   = "\x1b[D"
	seqHome   = "\x1b[H"
	seqEnd    = "\x1b[F"
	seqTab    = "\t"
	seqBackTb = "\x1b[Z"
)

// TestArrowsAndTabMoveTheFocus is the navigation contract, through the decoder.
//
// It walks all four arrows, Tab and shift-tab, and checks the focus index after
// each. The property under test is a RING: focus wraps, so no arrow is ever a
// no-op. That is asserted by the final step rather than assumed — a focus that
// stopped at the end would pass every intermediate assertion above and leave the
// third fact unreachable by arrow, which is exactly the dead end the wrapping was
// written to avoid.
func TestArrowsAndTabMoveTheFocus(t *testing.T) {
	a := newApp(t, 60, 14)
	if got := a.FocusIndex(); got != 0 {
		t.Fatalf("focus starts at %d, want 0", got)
	}

	for i := 1; i < len(a.facts); i++ {
		if !a.press(t, seqRight) {
			t.Fatalf("right arrow %d was not consumed", i)
		}
		if got := a.FocusIndex(); got != i {
			t.Errorf("after %d right arrows focus is %d, want %d", i, got, i)
		}
	}
	// The wrap. From the last fact, one more right returns to the first.
	if !a.press(t, seqRight) {
		t.Fatal("right arrow at the end was not consumed")
	}
	if got := a.FocusIndex(); got != 0 {
		t.Errorf("focus is %d after wrapping past the last fact, want 0", got)
	}
	// And backwards.
	if !a.press(t, seqLeft) {
		t.Fatal("left arrow was not consumed")
	}
	if got := a.FocusIndex(); got != len(a.facts)-1 {
		t.Errorf("focus is %d after one left arrow, want %d", got, len(a.facts)-1)
	}

	// Tab and shift-tab are the same ring, which is why they are aliases rather
	// than a second mechanism: a reader who tabs forward once per fact must arrive
	// back where they started. Home first, so the count is a full turn from a known
	// place rather than from wherever the arrows left it.
	a.press(t, seqHome)
	for i := 0; i < len(a.facts); i++ {
		if !a.press(t, seqTab) {
			t.Fatalf("tab %d was not consumed", i)
		}
	}
	if got := a.FocusIndex(); got != 0 {
		t.Errorf("focus is %d after a full turn of tabs, want 0", got)
	}
	if !a.press(t, seqBackTb) {
		t.Fatal("shift-tab was not consumed")
	}
	if got := a.FocusIndex(); got != len(a.facts)-1 {
		t.Errorf("focus is %d after one shift-tab, want %d", got, len(a.facts)-1)
	}

	// Up and down are aliases of left and right rather than row movement, and the
	// comment in main.go says so. Asserting it here is what stops a future edit
	// from making them "vertical within the column" and leaving the help's claim
	// ("next fact" / "previous fact") true while the behaviour is not.
	before := a.FocusIndex()
	if !a.press(t, seqUp) {
		t.Fatal("up arrow was not consumed")
	}
	if got, want := a.FocusIndex(), (before-1+len(a.facts))%len(a.facts); got != want {
		t.Errorf("up arrow moved focus to %d, want %d", got, want)
	}
	if !a.press(t, seqDown) {
		t.Fatal("down arrow was not consumed")
	}
	if got := a.FocusIndex(); got != before {
		t.Errorf("down arrow returned focus to %d, want %d", got, before)
	}
}

// TestHomeAndEndJumpToTheEnds pins the two keys that are NOT a ring step, because
// they are the reader's shortcut past three facts and a test that treated them as
// "one step" would let a broken implementation pass.
func TestHomeAndEndJumpToTheEnds(t *testing.T) {
	a := newApp(t, 60, 14)
	if !a.press(t, seqEnd) {
		t.Fatal("end was not consumed")
	}
	if got := a.FocusIndex(); got != len(a.facts)-1 {
		t.Errorf("end put focus at %d, want %d", got, len(a.facts)-1)
	}
	if !a.press(t, seqHome) {
		t.Fatal("home was not consumed")
	}
	if got := a.FocusIndex(); got != 0 {
		t.Errorf("home put focus at %d, want 0", got)
	}
}

// TestFocusMarkerIsOnScreenAndMoves is the focus marker at the SCREEN level.
//
// The marker is the non-colour signal, so asserting on the widget's field would
// assert on the mechanism rather than on what a reader sees. Two positions are
// compared: with focus on the first fact and with focus on the second, and the two
// screens must differ on the row that changed.
func TestFocusMarkerIsOnScreenAndMoves(t *testing.T) {
	d := newApp(t, 60, 14)

	first := widgettest.Screen(widgettest.Render(t, 60, 14, 1, d.hello))
	if !strings.Contains(first, string(focusMark[0])+" frame") {
		t.Errorf("the first fact is not marked as focused:\n%s", first)
	}
	// The marker is in its OWN gutter, so the unfocused facts are not marked. If
	// every row carried the marker, the assertion above would pass while focus
	// meant nothing.
	if n := strings.Count(first, string(focusMark[0])); n != 1 {
		t.Errorf("%d rows carry the focus marker, want exactly 1:\n%s", n, first)
	}

	if !d.press(t, seqRight) {
		t.Fatal("right arrow was not consumed")
	}
	second := widgettest.Screen(widgettest.Render(t, 60, 14, 1, d.hello))
	if second == first {
		t.Fatal("moving the focus did not change the screen")
	}
	if !strings.Contains(second, string(focusMark[0])+" depth") {
		t.Errorf("the second fact is not the marked one:\n%s", second)
	}
	if strings.Contains(second, string(focusMark[0])+" frame") {
		t.Errorf("the first fact is still marked after the focus moved:\n%s", second)
	}
}

// TestFocusDoesNotMoveTheGrid is the layout-stability half of the same idea, and
// it is the assertion that makes the gutter worth two cells.
//
// The marker lives in its own column precisely so that moving it cannot reflow
// the grid. If a future edit prefixed the marker to the label instead, the values
// would step one cell right when the focus moved and this test would catch it —
// which is the whole reason the gutter exists rather than a marker glued to the
// text.
func TestFocusDoesNotMoveTheGrid(t *testing.T) {
	d := newApp(t, 60, 14)
	before := widgettest.Screen(widgettest.Render(t, 60, 14, 1, d.hello))

	if !d.press(t, seqRight) {
		t.Fatal("right arrow was not consumed")
	}
	after := widgettest.Screen(widgettest.Render(t, 60, 14, 1, d.hello))

	// The marker is replaced by a SPACE rather than deleted, and the frame counter's
	// digits are deleted.
	//
	// The space is the load-bearing part: the marker lives in a gutter cell that is
	// always drawn, so an unfocused fact has a space there and a focused one has the
	// marker. Deleting the marker would shorten one row by a cell and every
	// comparison below would fail for the right reason in the wrong way.
	//
	// The digits go because each render draws another frame, so the counter changes —
	// which is the example working, and is covered by the counter's own test.
	strip := func(s string) string {
		return strings.Map(func(r rune) rune {
			switch {
			case r == rune(focusMark[0]):
				return ' '
			case r >= '0' && r <= '9':
				return -1
			default:
				return r
			}
		}, s)
	}
	if strip(before) != strip(after) {
		t.Errorf("moving the focus moved something other than the marker.\n--- before ---\n%s\n--- after ---\n%s",
			before, after)
	}
}

// TestQuestionMarkTogglesTheHelp drives the overlay through the decoder.
//
// '?' is 0x3f, which is a printable rune rather than a special key, so the decoder
// delivers it as Rune and the registry must find it there. Both the toggle and the
// KEY HINT'S OWN WORD are checked, because a hint that said "[?] toggle the keys"
// while the panel was open would be telling the reader to press a key that closes
// it.
func TestQuestionMarkTogglesTheHelp(t *testing.T) {
	d := newApp(t, 80, 20)
	// One buffer for the whole test, so the frame counter is the only thing that
	// differs between two renders and the comparison below can be exact.
	buf := buffer.NewBuffer(80, 20)
	render := func() string {
		d.Draw(buf)
		return bufToScreen(buf, 80, 20)
	}

	closed := render()
	if d.HelpOpen() {
		t.Fatal("the help starts open")
	}
	// The hint is the affordance that says the help exists, so it is on screen while
	// the help is closed. The help's own rows are not: a panel whose bindings are
	// visible before it is opened is not a toggle.
	for _, want := range hintLabels(d.km) {
		if !strings.Contains(closed, want) {
			t.Errorf("the closed screen does not show %q:\n%s", want, closed)
		}
	}
	for _, absent := range helpRowsText(d) {
		if strings.Contains(closed, absent) {
			t.Errorf("the help row %q is on screen while the help is closed:\n%s", absent, closed)
		}
	}

	if !d.press(t, "?") {
		t.Fatal("'?' was not consumed")
	}
	if !d.HelpOpen() {
		t.Fatal("'?' did not open the help")
	}
	open := render()
	for _, want := range helpRowsText(d) {
		if !strings.Contains(open, want) {
			t.Errorf("the open screen does not show %q:\n%s", want, open)
		}
	}
	// Opening the help must not have moved the focus, or the key hints would now
	// be describing a panel the reader did not go to.
	if d.FocusIndex() != 0 {
		t.Errorf("opening the help moved the focus to %d, want 0", d.FocusIndex())
	}

	if !d.press(t, "?") {
		t.Fatal("the second '?' was not consumed")
	}
	if d.HelpOpen() {
		t.Fatal("the second '?' did not close the help")
	}
	// Digits are compared out because the frame counter advances with every draw —
	// three renders have happened by now — and the counter's own behaviour is not
	// what this test is about. Everything else must match EXACTLY, which is what
	// makes it the stale-cell check: a help panel that left its border behind would
	// differ on a border row.
	stripDigits := func(s string) string {
		return strings.Map(func(r rune) rune {
			if r >= '0' && r <= '9' {
				return -1
			}
			return r
		}, s)
	}
	if again, want := stripDigits(render()), stripDigits(closed); again != want {
		t.Errorf("closing the help did not restore the screen.\n--- closed ---\n%s\n--- again ---\n%s",
			want, again)
	}
}

// bufToScreen renders a bare buffer's rows as one string, with trailing blanks
// trimmed, so two renders can be compared exactly.
//
// A bare buffer rather than a renderer on purpose: this test is about the WIDGET's
// output, and going through the renderer would put a diff and an encoder between
// the two renders and make a difference ambiguous between "the widget changed" and
// "the diff chose differently".
func bufToScreen(buf *buffer.Buffer, w, h int) string {
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

// TestHelpDoesNotStealRowsFromTheNote is the budget's behaviour on the toggle,
// which is the only thing the help costs.
//
// Opening the help adds its rows and the budget drops the lowest-priority region —
// the note — rather than clipping the help or the facts. That ranking is the reason
// regionHelp is PrioNormal and regionNote is PrioLow, and it is invisible in the
// widget's fields, so it is asserted on the screen.
func TestHelpDoesNotStealRowsFromTheNote(t *testing.T) {
	// 60x14 is the size where the help and the note cannot both fit: the interior is
	// eight rows and the budgetable regions want nine of them, so the ranking decides
	// which two go. A taller screen would fit everything and assert nothing, and a
	// shorter one drops the help too — which is the other direction, and has its own
	// test below.
	d := newApp(t, 60, 14)

	if !d.press(t, "?") {
		t.Fatal("'?' was not consumed")
	}
	got := widgettest.Screen(widgettest.Render(t, 60, 14, 1, d.hello))

	// The facts and the help both survive.
	for _, want := range append([]string{"frame", "depth"}, helpRowsText(d)...) {
		if !strings.Contains(got, want) {
			t.Errorf("with the help open the screen does not show %q:\n%s", want, got)
		}
	}
	// The note is what goes, because it is PrioLow and the help is not.
	if strings.Contains(got, "the layout above is recomputed") {
		t.Errorf("the note survived the budget with the help open:\n%s", got)
	}
	// And the pinned hint is never the thing dropped.
	for _, want := range hintLabels(d.km) {
		if !strings.Contains(got, want) {
			t.Errorf("the hint must survive every budget:\n%s", got)
		}
	}
}

// TestHelpIsDroppedBeforeTheFacts is the other direction of the ranking: at a size
// too short for the help AND the facts, the facts win. It is the assertion that
// stops a future edit from making the help PrioHigh and having the screen explain
// its keys instead of showing its numbers.
func TestHelpIsDroppedBeforeTheFacts(t *testing.T) {
	d := newApp(t, 46, 12)
	if !d.press(t, "?") {
		t.Fatal("'?' was not consumed")
	}
	got := widgettest.Screen(widgettest.Render(t, 46, 12, 1, d.hello))

	if !strings.Contains(got, "frame") || !strings.Contains(got, "depth") {
		t.Errorf("the facts are PrioHigh and must survive:\n%s", got)
	}
	if strings.Contains(got, helpRowsText(d)[0]) {
		t.Errorf("the help was shown where there was no room for it:\n%s", got)
	}
}

// TestToggleHelpInvalidatesTheLayoutCache is the caching rule, stated directly.
//
// The budget's answer depends on whether the help is open — adapt sizes its region
// from the flag — and the cache is keyed on the interior rectangle alone. Without
// the invalidation the second Draw would reuse the answer computed for the other
// mode, and TestHelpDoesNotStealRowsFromTheNote would pass on the first frame and
// fail on the second.
func TestToggleHelpInvalidatesTheLayoutCache(t *testing.T) {
	d := newApp(t, 60, 14)
	buf := buffer.NewBuffer(60, 14)

	d.Draw(buf)
	if !d.lay.valid {
		t.Fatal("the first draw did not populate the layout cache")
	}
	d.Draw(buf)
	validBefore := d.lay.valid

	d.press(t, "?")
	if validBefore != d.lay.valid {
		// The flag flipped, so this comparison says nothing; re-warm and compare the
		// ANSWER instead, which is the thing a stale cache would get wrong.
		d.Draw(buf)
	}
	openShow := d.lay.show[regionHelp]
	if !d.lay.valid {
		t.Fatal("the cache was invalidated but the next draw did not repopulate it")
	}
	if !openShow {
		t.Error("with the help open the budget did not keep the help region")
	}

	d.press(t, "?")
	// The next Draw is the point: the toggle invalidated the cache, so adapt
	// re-runs and re-sizes the help's region. Without the invalidation this Draw
	// would reuse the open-mode answer and the region would still ask for its rows.
	d.Draw(buf)
	// The region's SIZE is the assertion, not the budget's answer: Budget reports a
	// zero-size region as KEPT, because dropping it would not free a cell. So a
	// closed help is a region of size zero, and that is what a stale cache would
	// get wrong — it would keep the rows after the second toggle.
	if got := d.regions[regionHelp].Size; got != 0 {
		t.Errorf("with the help closed the help region asks for %d rows, want 0", got)
	}
	if got := d.lay.show[regionHelp]; !got {
		t.Error("a zero-size region must still be reported as kept; Budget says a drop would not help")
	}
}

// TestQuitKeysAreConsumed pins the three keys the program treats as quit, and
// that they reach the command rather than falling through to the tree.
//
// The second half is the load-bearing part. The quit keys used to be a hand-written
// isQuitKey in the input loop AND a case in the widget's Handle, and a test that
// only checked the widget would have passed while the loop did something else. Now
// there is one binding and this asserts that Dispatch resolves it and that the
// handler the application handed in is what runs.
func TestQuitKeysAreConsumed(t *testing.T) {
	a := newApp(t, 60, 14)
	for i, seq := range []string{"q", "Q"} {
		if !a.press(t, seq) {
			t.Errorf("%q was not consumed by the registry", seq)
		}
		// One run per key, cumulatively: a binding that fired twice would be a
		// chord bound to the same command twice, which is precisely the sort of
		// thing a hand-written table got wrong silently.
		if want := i + 1; a.quits != want {
			t.Fatalf("%q ran app.quit %d times in total, want %d", seq, a.quits, want)
		}
	}
	// Escape is NOT decoded from a bare ESC: the decoder waits out its ambiguity
	// delay to find out whether the byte is the start of a sequence, so a lone ESC
	// comes back incomplete. The event a real terminal delivers — after that delay —
	// is the one below, which is why the assertion builds it rather than decoding
	// it, and why the source is still "what the decoder produces".
	esc := termmosaic.SpecialKeyEvent(termmosaic.KeyEscape, 0)
	if _, consumed := a.km.Dispatch(esc, a.hello); !consumed {
		t.Error("escape was not consumed by the registry")
	}
	if a.quits != 3 {
		t.Errorf("escape ran app.quit %d times in total, want 3", a.quits)
	}
	// Ctrl-C arrives as Ctrl+'c' rather than as 0x03, which is the whole reason the
	// raw-byte spelling could not work — and the reason the binding is written
	// "Ctrl+c" rather than "Ctrl+C".
	ctrlC := termmosaic.Event{Kind: termmosaic.EventKey, Rune: 'c', Mod: termmosaic.ModCtrl}
	if _, consumed := a.km.Dispatch(ctrlC, a.hello); !consumed {
		t.Error("ctrl-c was not consumed by the registry")
	}
	if a.quits != 4 {
		t.Errorf("ctrl-c ran app.quit %d times in total, want 4", a.quits)
	}
	// And the widget did not QUIT on any of them, did not open the help, and did
	// not move the focus: this example quits from its own command handler, so a
	// widget that ended the program would take the decision away from the one place
	// that can close the terminal cleanly.
	if a.HelpOpen() {
		t.Error("a quit key opened the help, so it was treated as some other key")
	}
	if a.FocusIndex() != 0 {
		t.Error("a quit key moved the focus, so it was treated as some other key")
	}
}

// TestUnboundKeysAreNotConsumed is the honesty rule, asserted twice over: the
// registry declines a key the example does not bind, and the widget declines it
// too, so an application that put this block under something else sees it come back
// unhandled.
func TestUnboundKeysAreNotConsumed(t *testing.T) {
	a := newApp(t, 60, 14)
	for _, seq := range []string{"x", "j", "\r", "\x7f"} {
		ev := decodeKey(t, seq)
		if _, consumed := a.km.Dispatch(ev, a.hello); consumed {
			t.Errorf("the registry consumed %q, which the example does not bind", seq)
		}
		if a.hello.Handle(ev) {
			t.Errorf("%q was consumed by the widget, which claims nothing", seq)
		}
	}
	// A resize and a mouse event are the application's business, not the widget's:
	// claiming a resize would mean the widget recomputed bounds itself, and claiming
	// a mouse event would be a claim that the facts are hit targets.
	if _, consumed := a.km.Dispatch(termmosaic.ResizeEvent(40, 10), a.hello); consumed {
		t.Error("a resize was dispatched as a command; ADR 0009 §2 rule 1 forbids it")
	}
	if a.hello.Handle(termmosaic.ResizeEvent(40, 10)) {
		t.Error("a resize was consumed by the widget; the application owns it")
	}
	mouse := termmosaic.Event{
		Kind:  termmosaic.EventMouse,
		Mouse: termmosaic.Mouse{X: 5, Y: 3, Button: termmosaic.MouseLeft, Action: termmosaic.MousePress},
	}
	if _, consumed := a.km.Dispatch(mouse, a.hello); consumed {
		t.Error("a mouse press became a command; the example's facts are not hit targets")
	}
	if a.hello.Handle(mouse) {
		t.Error("a mouse press was consumed; the example's facts are not hit targets")
	}
	// A modified rune is a chord, not the bare key: ctrl-? must not be read as '?'.
	ctrlQ := termmosaic.KeyEvent('?', termmosaic.ModCtrl)
	if _, consumed := a.km.Dispatch(ctrlQ, a.hello); consumed {
		t.Error("ctrl-? was consumed as if it were the help key")
	}
}

// TestEveryCommandIsReachableByADispatch is the test that replaces the old
// "the help names it, Handle accepts it" test.
//
// It walks Describe's output — the same rows the hint and the help are rendered
// from — and dispatches every chord it finds, asserting the command that runs is
// the one the row names. A command that is not reachable by any chord it is
// described with fails here; a chord bound to a command nobody describes fails
// TestEveryBindingIsDescribed; and a chord the program answers to which is not in
// the registry fails TestUnboundKeysAreNotConsumed.
//
// The event is built from the chord rather than decoded from bytes, because the
// chord IS what Dispatch consumes and the decoder's agreement with it is already
// pinned by keymap's own round-trip test and by the tests above.
func TestEveryCommandIsReachableByADispatch(t *testing.T) {
	a := newApp(t, 60, 14)
	rows := a.km.Describe(keymap.ScopeScreen)
	if len(rows) == 0 {
		t.Fatal("Describe returned nothing; the example's bindings are unreachable")
	}

	for _, row := range rows {
		if len(row.Chords) != 1 {
			// One row per chord is what Describe promises, and this example's
			// hint and help read DescribeGrouped instead. A Describe that
			// started returning several chords in one row would make this loop
			// dispatch only the first of them, so it is worth a failure rather
			// than a shrug.
			t.Errorf("%s: Describe returned %d chords in one row, want 1", row.ID, len(row.Chords))
			continue
		}
		ev := eventFor(row.Chords[0])
		id, consumed := a.km.Dispatch(ev, a.hello)
		if !consumed {
			t.Errorf("the registry describes %s as %s but Dispatch declined it",
				row.ID, row.Chords[0])
			continue
		}
		if id != row.ID {
			t.Errorf("the row %s (%s) dispatched as %s", row.ID, row.Chords[0], id)
		}
	}
}

// TestEveryBindingIsDescribed is the other direction of the same property: a chord
// the program answers to must appear in what the program prints, or the hint is
// quietly lying about the key contract.
//
// It is stated over Chords rather than over Describe's rows because Chords is the
// un-widened query: Describe(ScopeScreen) asks what this SCREEN can do, which is
// the right question for the hint and the help and the wrong one for "is this chord
// bound at all".
func TestEveryBindingIsDescribed(t *testing.T) {
	a := newApp(t, 60, 14)
	described := make(map[keymap.CommandID]bool)
	for _, row := range a.km.Describe(keymap.ScopeScreen) {
		described[row.ID] = true
	}
	for _, row := range a.km.Describe(keymap.ScopeGlobal) {
		described[row.ID] = true
	}

	// The bindings, as chords rather than as text: this is the one place a chord is
	// spelled out, and it is spelled out in the SAME notation the registry was built
	// from, which is the point ParseChord round-trips.
	for _, spec := range []struct {
		id     keymap.CommandID
		scope  keymap.Scope
		chords []string
	}{
		{cmdQuit, keymap.ScopeGlobal, []string{"q", "Q", "Esc", "Ctrl+c"}},
		{cmdHelp, keymap.ScopeGlobal, []string{"?"}},
		{cmdBack, keymap.ScopeScreen, []string{"Left", "Up", "Backtab"}},
		{cmdNext, keymap.ScopeScreen, []string{"Right", "Down", "Tab"}},
		{cmdFirst, keymap.ScopeScreen, []string{"Home"}},
		{cmdLast, keymap.ScopeScreen, []string{"End"}},
	} {
		for _, s := range spec.chords {
			c, err := keymap.ParseChord(s)
			if err != nil {
				t.Fatalf("%s is not a chord: %v", s, err)
			}
			got := a.km.Chords(spec.id, spec.scope)
			found := false
			for _, have := range got {
				if have == c {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("%s is bound to %s but Chords reports %v", c, spec.id, got)
			}
			if !described[spec.id] {
				t.Errorf("%s is bound but Describe does not report it, so nothing on screen names %s",
					spec.id, c)
			}
		}
	}
}

// TestHintIsRenderedFromTheRegistry is the anti-drift property the old golden
// string was reaching for, stated so that it holds by construction.
//
// The pinned hint used to be a constant, and the net against it was a golden file
// plus a test that checked the constant named the keys Handle accepted. Both are
// gone: the hint's bindings ARE the merged Describe output for hintCommands, and
// this test renders that same output through a second, independent KeyHint and
// compares the two widgets' rows. A hint that stopped being derived from the
// registry — someone setting Bindings directly — fails here, because the widget
// under test and the widget built from Describe would then differ.
//
// The expected text is built from the registry, never written out: that is what
// makes "changing a binding changes the hint" true rather than aspirational.
func TestHintIsRenderedFromTheRegistry(t *testing.T) {
	a := newApp(t, 60, 14)

	want := form.NewKeyHint(buffer.Rect{}, nil)
	want.SetEntries(entriesFor(a.km.DescribeGrouped(keymap.ScopeScreen), hintCommands))
	want.Sep = a.hint.Sep

	gotRow := func(k *form.KeyHint) string {
		b := buffer.NewBuffer(120, 1)
		k.SetBounds(buffer.Rect{W: 120, H: 1})
		k.Draw(b)
		return strings.TrimRight(rowText(b, 120), " ")
	}
	if got, expect := gotRow(a.hint), gotRow(want); got != expect {
		t.Errorf("the hint is not what Describe renders.\n--- on screen ---\n%s\n--- derived ---\n%s",
			got, expect)
	}
	if gotRow(a.hint) == "" {
		t.Fatal("the hint rendered nothing")
	}
	// Every chord the hint shows must be one Dispatch answers to, which is the
	// property a literal string could never have.
	for _, label := range hintLabels(a.km) {
		if !strings.Contains(gotRow(a.hint), label) {
			t.Errorf("the hint does not show %q, which Describe reports", label)
		}
	}
}

// TestHelpRowsMatchTheRegionSize keeps the help overlay and the budget honest with
// each other: helpLines decides what is on each row and helpRows decides what the
// budget asks for, and the two are separate constants for no reason.
func TestHelpRowsMatchTheRegionSize(t *testing.T) {
	if got, want := len(helpLines), helpRows; got != want {
		t.Errorf("helpLines has %d rows and helpRows asks for %d; the overlay would be "+
			"clipped or would leave a gap", got, want)
	}
	a := newApp(t, 60, 14)
	if got := len(a.help.lines); got != len(helpLines) {
		t.Errorf("the panel has %d KeyHints for %d help lines", got, len(helpLines))
	}
}

// TestRegistryIsQuiet is the registry's own diagnostic surface, asserted empty.
//
// It is worth a test because every entry Warnings can produce is a mistake this
// example could plausibly make: a binding naming an unregistered command, a
// command with no description, a scoped binding with no owner, a global binding
// WITH one, a chord bound twice, or — the one this wiring could actually have hit
// — a screen-scoped binding whose owner was never attached.
func TestRegistryIsQuiet(t *testing.T) {
	a := newApp(t, 60, 14)
	for _, w := range a.km.Warnings() {
		t.Errorf("the example's registry warns: %s", w)
	}
}

// eventFor builds the key event a chord describes. It is the inverse of
// keymap.ChordOf for every chord a terminal can produce, which is what makes it a
// fair way to feed the dispatch loop from the registry's own data.
func eventFor(c keymap.Chord) termmosaic.Event {
	return termmosaic.Event{Kind: termmosaic.EventKey, Key: c.Key, Rune: c.Rune, Mod: c.Mod}
}

// hintLabels is what the hint line shows for each of its commands: the bracketed
// chord column KeyHint builds from the entry, and nothing else.
//
// It is a second reader of the same data rather than a copy of it: no key is
// spelled here, so a test using it cannot go stale when a binding changes.
func hintLabels(km *keymap.Registry) []string {
	var out []string
	for _, e := range entriesFor(km.DescribeGrouped(keymap.ScopeScreen), hintCommands) {
		label := make([]string, 0, len(e.Chords))
		for _, c := range e.Chords {
			label = append(label, c.String())
		}
		out = append(out, "["+strings.Join(label, " ")+"] "+e.Desc)
	}
	return out
}

// helpRowsText is each help line as it reads on screen: every command's bracketed
// chord column followed by its description, in the order the panel draws them.
func helpRowsText(a *app) []string {
	entries := a.km.DescribeGrouped(keymap.ScopeScreen)
	out := make([]string, 0, len(helpLines))
	for _, ids := range helpLines {
		rows := entriesFor(entries, ids)
		if len(rows) == 0 {
			out = append(out, "")
			continue
		}
		var b strings.Builder
		for _, e := range rows {
			if b.Len() > 0 {
				b.WriteString(form.KeyHintSep)
			}
			b.WriteString("[" + chordLabel(e.Chords) + "] " + e.Desc)
		}
		out = append(out, b.String())
	}
	return out
}

// chordLabel is the key column KeyHint builds for one entry's chords.
func chordLabel(chords []keymap.Chord) string {
	parts := make([]string, 0, len(chords))
	for _, c := range chords {
		parts = append(parts, c.String())
	}
	return strings.Join(parts, " ")
}
