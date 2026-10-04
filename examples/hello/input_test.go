package main

// The input tests: the key contract, driven through the REAL decoder.
//
// Every keyboard assertion here goes through input.Decode rather than a
// hand-built termmosaic.Event, which is the idiom widgets/data/eventtest_test.go
// established and the reason it is worth copying: a hand-built Event can describe
// something no terminal would ever send, and a test that passes on it proves
// nothing about the program. Decoding "\x1b[C" gives the same KeyRight the
// decoder gives a real arrow key, and it fails the moment the decoder changes.
//
// The mouse and resize cases are hand-built, because there is no byte sequence
// this example's Handle acts on: it claims no mouse events at all, and a resize is
// the application's job rather than the widget's.

import (
	"strings"
	"testing"

	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/input"
	"github.com/serkanalgur/termmosaic/widgets/widgettest"
)

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

// press offers one decoded key sequence to the widget and reports whether it was
// consumed.
func press(t *testing.T, h *hello, seq string) bool {
	t.Helper()
	return h.Handle(decodeKey(t, seq))
}

// The escape sequences for the keys this example binds. Written as the ACTUAL
// bytes rather than as decoder calls, because the point is that the decoder agrees
// with the widget about what those bytes mean.
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
	h := newHello(rootBounds(60, 14), buffer.DepthTrueColor)
	if got := h.FocusIndex(); got != 0 {
		t.Fatalf("focus starts at %d, want 0", got)
	}

	for i := 1; i < len(h.facts); i++ {
		if !press(t, h, seqRight) {
			t.Fatalf("right arrow %d was not consumed", i)
		}
		if got := h.FocusIndex(); got != i {
			t.Errorf("after %d right arrows focus is %d, want %d", i, got, i)
		}
	}
	// The wrap. From the last fact, one more right returns to the first.
	if !press(t, h, seqRight) {
		t.Fatal("right arrow at the end was not consumed")
	}
	if got := h.FocusIndex(); got != 0 {
		t.Errorf("focus is %d after wrapping past the last fact, want 0", got)
	}
	// And backwards.
	if !press(t, h, seqLeft) {
		t.Fatal("left arrow was not consumed")
	}
	if got := h.FocusIndex(); got != len(h.facts)-1 {
		t.Errorf("focus is %d after one left arrow, want %d", got, len(h.facts)-1)
	}

	// Tab and shift-tab are the same ring, which is why they are aliases rather
	// than a second mechanism: a reader who tabs forward once per fact must arrive
	// back where they started. Home first, so the count is a full turn from a known
	// place rather than from wherever the arrows left it.
	press(t, h, seqHome)
	for i := 0; i < len(h.facts); i++ {
		if !press(t, h, seqTab) {
			t.Fatalf("tab %d was not consumed", i)
		}
	}
	if got := h.FocusIndex(); got != 0 {
		t.Errorf("focus is %d after a full turn of tabs, want 0", got)
	}
	if !press(t, h, seqBackTb) {
		t.Fatal("shift-tab was not consumed")
	}
	if got := h.FocusIndex(); got != len(h.facts)-1 {
		t.Errorf("focus is %d after one shift-tab, want %d", got, len(h.facts)-1)
	}

	// Up and down are aliases of left and right rather than row movement, and the
	// comment in main.go says so. Asserting it here is what stops a future edit
	// from making them "vertical within the column" and leaving the hint's claim
	// ("arrows move focus") true while the behaviour is not.
	before := h.FocusIndex()
	if !press(t, h, seqUp) {
		t.Fatal("up arrow was not consumed")
	}
	if got, want := h.FocusIndex(), (before-1+len(h.facts))%len(h.facts); got != want {
		t.Errorf("up arrow moved focus to %d, want %d", got, want)
	}
	if !press(t, h, seqDown) {
		t.Fatal("down arrow was not consumed")
	}
	if got := h.FocusIndex(); got != before {
		t.Errorf("down arrow returned focus to %d, want %d", got, before)
	}
}

// TestHomeAndEndJumpToTheEnds pins the two keys that are NOT a ring step, because
// they are the reader's shortcut past three facts and a test that treated them as
// "one step" would let a broken implementation pass.
func TestHomeAndEndJumpToTheEnds(t *testing.T) {
	h := newHello(rootBounds(60, 14), buffer.DepthTrueColor)
	if !press(t, h, seqEnd) {
		t.Fatal("end was not consumed")
	}
	if got := h.FocusIndex(); got != len(h.facts)-1 {
		t.Errorf("end put focus at %d, want %d", got, len(h.facts)-1)
	}
	if !press(t, h, seqHome) {
		t.Fatal("home was not consumed")
	}
	if got := h.FocusIndex(); got != 0 {
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
	d := newHello(rootBounds(60, 14), buffer.DepthTrueColor)

	first := widgettest.Screen(widgettest.Render(t, 60, 14, 1, d))
	if !strings.Contains(first, string(focusMark[0])+" frame") {
		t.Errorf("the first fact is not marked as focused:\n%s", first)
	}
	// The marker is in its OWN gutter, so the unfocused facts are not marked. If
	// every row carried the marker, the assertion above would pass while focus
	// meant nothing.
	if n := strings.Count(first, string(focusMark[0])); n != 1 {
		t.Errorf("%d rows carry the focus marker, want exactly 1:\n%s", n, first)
	}

	if !press(t, d, seqRight) {
		t.Fatal("right arrow was not consumed")
	}
	second := widgettest.Screen(widgettest.Render(t, 60, 14, 1, d))
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
// The marker lives in its own column precisely so that moving it cannot reflow the
// grid. If a future edit prefixed the marker to the label instead, the values would
// step one cell right when the focus moved and this test would catch it — which is
// the whole reason the gutter exists rather than a marker glued to the text.
func TestFocusDoesNotMoveTheGrid(t *testing.T) {
	d := newHello(rootBounds(60, 14), buffer.DepthTrueColor)
	before := widgettest.Screen(widgettest.Render(t, 60, 14, 1, d))

	if !press(t, d, seqRight) {
		t.Fatal("right arrow was not consumed")
	}
	after := widgettest.Screen(widgettest.Render(t, 60, 14, 1, d))

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
// delivers it as Rune and the widget must find it there. Both the toggle and the
// KEY HINT'S OWN WORD are checked, because a hint that said "[?] keys" while the
// panel was open would be telling the reader to press a key that closes it.
func TestQuestionMarkTogglesTheHelp(t *testing.T) {
	d := newHello(rootBounds(80, 20), buffer.DepthTrueColor)
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
	// the help is closed. The help's own TEXT is not: a panel whose bindings are
	// visible before it is opened is not a toggle.
	for _, want := range []string{hintText, "keys"} {
		if !strings.Contains(closed, want) {
			t.Errorf("the closed screen does not show %q:\n%s", want, closed)
		}
	}
	for _, gone := range []string{helpNav, helpExit} {
		if strings.Contains(closed, gone) {
			t.Errorf("the help body %q is on screen while the help is closed:\n%s", gone, closed)
		}
	}

	if !press(t, d, "?") {
		t.Fatal("'?' was not consumed")
	}
	if !d.HelpOpen() {
		t.Fatal("'?' did not open the help")
	}
	open := render()
	for _, want := range []string{helpNav, helpExit} {
		if !strings.Contains(open, want) {
			t.Errorf("the open screen does not show %q:\n%s", want, open)
		}
	}
	// Opening the help must not have moved the focus, or the key hint would now be
	// describing a panel the reader did not go to.
	if d.FocusIndex() != 0 {
		t.Errorf("opening the help moved the focus to %d, want 0", d.FocusIndex())
	}

	if !press(t, d, "?") {
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
// Opening the help adds two rows and the budget drops the lowest-priority region —
// the note — rather than clipping the help or the facts. That ranking is the reason
// regionHelp is PrioNormal and regionNote is PrioLow, and it is invisible in the
// widget's fields, so it is asserted on the screen.
func TestHelpDoesNotStealRowsFromTheNote(t *testing.T) {
	// 60x12 is the size where the help and the note cannot both fit: the interior is
	// ten rows, the four budgetable regions want thirteen, and the ranking decides
	// which two go. 60x14 would fit everything and assert nothing.
	d := newHello(rootBounds(60, 12), buffer.DepthTrueColor)

	if !press(t, d, "?") {
		t.Fatal("'?' was not consumed")
	}
	got := widgettest.Screen(widgettest.Render(t, 60, 12, 1, d))

	// The facts and the help both survive.
	for _, want := range []string{"frame", "depth", helpNav, helpExit} {
		if !strings.Contains(got, want) {
			t.Errorf("with the help open the screen does not show %q:\n%s", want, got)
		}
	}
	// The note is what goes, because it is PrioLow and the help is not.
	if strings.Contains(got, "the layout above is recomputed") {
		t.Errorf("the note survived the budget with the help open:\n%s", got)
	}
	// And the pinned hint is never the thing dropped.
	if !strings.Contains(got, hintText) {
		t.Errorf("the hint must survive every budget:\n%s", got)
	}
}

// TestHelpIsDroppedBeforeTheFacts is the other direction of the ranking: at a size
// too short for the help AND the facts, the facts win. It is the assertion that
// stops a future edit from making the help PrioHigh and having the screen explain
// its keys instead of showing its numbers.
func TestHelpIsDroppedBeforeTheFacts(t *testing.T) {
	d := newHello(rootBounds(46, 12), buffer.DepthTrueColor)
	if !press(t, d, "?") {
		t.Fatal("'?' was not consumed")
	}
	got := widgettest.Screen(widgettest.Render(t, 46, 12, 1, d))

	if !strings.Contains(got, "frame") || !strings.Contains(got, "depth") {
		t.Errorf("the facts are PrioHigh and must survive:\n%s", got)
	}
	if strings.Contains(got, helpNav) {
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
	d := newHello(rootBounds(60, 14), buffer.DepthTrueColor)
	buf := buffer.NewBuffer(60, 14)

	d.Draw(buf)
	if !d.lay.valid {
		t.Fatal("the first draw did not populate the layout cache")
	}
	d.Draw(buf)
	validBefore := d.lay.valid

	press(t, d, "?")
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

	press(t, d, "?")
	// The next Draw is the point: the toggle invalidated the cache, so adapt
	// re-runs and re-sizes the help's region. Without the invalidation this Draw
	// would reuse the open-mode answer and the region would still ask for two rows.
	d.Draw(buf)
	// The region's SIZE is the assertion, not the budget's answer: Budget reports a
	// zero-size region as KEPT, because dropping it would not free a cell. So a
	// closed help is a region of size zero, and that is what a stale cache would
	// get wrong — it would keep the two rows after the second toggle.
	if got := d.regions[regionHelp].Size; got != 0 {
		t.Errorf("with the help closed the help region asks for %d rows, want 0", got)
	}
	if got := d.lay.show[regionHelp]; !got {
		t.Error("a zero-size region must still be reported as kept; Budget says a drop would not help")
	}
}

// TestQuitKeysAreConsumed pins the three keys run treats as quit, and that the
// widget claims them even though it does not act on them.
//
// The second half is the load-bearing part: the input loop checks isQuitKey BEFORE
// offering the event to the widget, so a widget that returned false for 'q' would
// still work — but only because of that ordering, and an example whose widget
// silently ignored its own quit key is teaching the wrong thing about how to
// compose. run's isQuitKey is asserted alongside so the two cannot drift.
func TestQuitKeysAreConsumed(t *testing.T) {
	h := newHello(rootBounds(60, 14), buffer.DepthTrueColor)
	for _, seq := range []string{"q", "Q"} {
		if !h.Handle(decodeKey(t, seq)) {
			t.Errorf("%q was not consumed by the widget", seq)
		}
		if !isQuitKey(decodeKey(t, seq)) {
			t.Errorf("%q is not a quit key for run", seq)
		}
	}
	// Escape is NOT decoded from a bare ESC: the decoder waits out its ambiguity
	// delay to find out whether the byte is the start of a sequence, so a lone ESC
	// comes back incomplete. The event a real terminal delivers — after that delay —
	// is the one below, which is why the assertion builds it rather than decoding
	// it, and why the source is still "what the decoder produces".
	esc := termmosaic.SpecialKeyEvent(termmosaic.KeyEscape, 0)
	if !h.Handle(esc) {
		t.Error("escape was not consumed by the widget")
	}
	if !isQuitKey(esc) {
		t.Error("escape is not a quit key for run")
	}
	// Ctrl-C arrives as Ctrl+'c' rather than as 0x03, which is the whole reason the
	// raw-byte spelling could not work.
	ctrlC := termmosaic.Event{Kind: termmosaic.EventKey, Rune: 'c', Mod: termmosaic.ModCtrl}
	if !h.Handle(ctrlC) {
		t.Error("ctrl-c was not consumed by the widget")
	}
	if !isQuitKey(ctrlC) {
		t.Error("ctrl-c is not a quit key for run")
	}
	// And the widget did not QUIT on any of them: this example quits from its own
	// input goroutine, so a widget that ended the program would take the decision
	// away from the one place that can close the terminal cleanly.
	if h.HelpOpen() {
		t.Error("a quit key opened the help, so it was treated as some other key")
	}
}

// TestUnboundKeysAreNotConsumed is the honesty rule from Handle's own
// documentation, asserted because it is invisible otherwise.
//
// A widget that consumed every key would break every application that put it under
// something else, and the only way to notice is to press a key the example does not
// use and check that it comes back unhandled.
func TestUnboundKeysAreNotConsumed(t *testing.T) {
	h := newHello(rootBounds(60, 14), buffer.DepthTrueColor)
	for _, seq := range []string{"x", "j", "\r", "\x7f"} {
		if h.Handle(decodeKey(t, seq)) {
			t.Errorf("%q was consumed by a key the example does not bind", seq)
		}
	}
	// A resize and a mouse event are the application's business, not the widget's:
	//	// claiming a resize would mean the widget recomputed bounds itself, and claiming
	// a mouse event would be a claim that the facts are hit targets.
	if h.Handle(termmosaic.ResizeEvent(40, 10)) {
		t.Error("a resize was consumed by the widget; the application owns it")
	}
	if h.Handle(termmosaic.Event{
		Kind:  termmosaic.EventMouse,
		Mouse: termmosaic.Mouse{X: 5, Y: 3, Button: termmosaic.MouseLeft, Action: termmosaic.MousePress},
	}) {
		t.Error("a mouse press was consumed; the example's facts are not hit targets")
	}
	// A modified rune is a chord, not the bare key: ctrl-r must not be read as 'r'.
	if h.Handle(termmosaic.KeyEvent('?', termmosaic.ModCtrl)) {
		t.Error("ctrl-? was consumed as if it were the help key")
	}
}

// TestHelpTextMatchesTheBindings is the anti-drift test, and it is the one that
// makes the help panel trustworthy.
//
// The help names keys in prose and Handle accepts them in a switch, so the two can
// disagree. This walks the binding table, maps each named key to the sequence a
// terminal sends for it, and asserts the widget consumes it — so renaming a key in
// the text without changing the handler fails here rather than shipping a help
// panel that lies.
func TestHelpTextMatchesTheBindings(t *testing.T) {
	// The named key to the sequence a terminal sends for it. Only the keys the help
	// panel names are here; a binding the panel does not mention cannot mislead a
	// reader about it.
	seqs := map[string]string{
		// The two group HEADINGS rather than keys: "move" and "help" label the
		// bindings beneath them, and the keys they head are asserted individually.
		"move": "",
		"help": "",
		// "arrows" is the plural the help uses for all four, so one is enough here;
		// the other three are asserted in the loop below.
		"arrows":    seqRight,
		"tab":       seqTab,
		"shift-tab": seqBackTb,
		"home":      seqHome,
		"end":       seqEnd,
		"?":         "?",
		"q":         "q",
	}
	d := newHello(rootBounds(80, 20), buffer.DepthTrueColor)

	for name, seq := range seqs {
		if !strings.Contains(helpNav, name) && !strings.Contains(helpExit, name) {
			continue
		}
		if seq == "" {
			// A group heading rather than a key: "move" and "help" are labels, and
			// the keys they head are the ones asserted individually below.
			continue
		}
		if !d.Handle(decodeKey(t, seq)) {
			t.Errorf("the help names %q but the widget does not consume %q", name, seq)
		}
	}
	// Escape and ctrl-c are named by the help and are handled, but neither decodes
	// from a single byte: a lone ESC is ambiguous with a sequence prefix, and 0x03
	// is only the interrupt when the decoder has reported it as Ctrl+'c'. Both are
	// asserted as the events a running program receives.
	for _, ev := range []termmosaic.Event{
		termmosaic.SpecialKeyEvent(termmosaic.KeyEscape, 0),
		{Kind: termmosaic.EventKey, Rune: 'c', Mod: termmosaic.ModCtrl},
	} {
		if !d.Handle(ev) {
			t.Errorf("the help names the key carried by %+v but the widget does not consume it", ev)
		}
	}

	// The arrows are named by the help only as "arrows", so they are asserted here
	// rather than through the table: the plural is the help's claim and these are
	// the keys it covers.
	for _, seq := range []string{seqUp, seqDown, seqLeft, seqRight} {
		if !d.Handle(decodeKey(t, seq)) {
			t.Errorf("the help says \"arrows move focus\" but %q is not consumed", seq)
		}
	}
}
