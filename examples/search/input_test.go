package main

// The key tests: the key contract, driven through the REAL decoder and through the
// REAL registry.
//
// Every keyboard assertion here goes through input.Decode rather than a hand-built
// termmosaic.Event, which is the idiom widgets/data/eventtest_test.go established
// and the reason it is worth copying: a hand-built Event can describe something no
// terminal would ever send, and a test that passes on it proves nothing about the
// program. Decoding "\x1b[C" gives the same KeyRight the decoder gives a real arrow
// key, and it fails the moment the decoder changes.
//
// The mouse and resize cases are hand-built, because there is no byte sequence this
// example acts on: the widgets hit-test themselves, and a resize is the
// application's job rather than the widget's.
//
// Every key goes through km.Dispatch and then the screen's Handle — which is
// exactly what run does — because a test that called only one of them would be
// testing half the program. And half the program is precisely what this example is
// about: a key the registry declines is a key the focused widget gets, and the tests
// below assert both halves of that.

import (
	"strings"
	"testing"

	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/headless"
	"github.com/serkanalgur/termmosaic/input"
	"github.com/serkanalgur/termmosaic/keymap"
	"github.com/serkanalgur/termmosaic/render"
	"github.com/serkanalgur/termmosaic/widgets/form"
)

// app is the example as run: the screen plus the callbacks main would wire. It
// embeds the screen so the tests read as they did when the screen owned everything.
type app struct {
	*search
	submits []string
	opens   []string
	quits   int
}

// newApp builds the screen at (w, h) fed the bundled capture, with recording
// callbacks.
func newApp(t *testing.T, w, h int) *app {
	t.Helper()
	a := &app{}
	a.search = newSearch(screenBounds(w, h))
	a.search.onSubmit = func(q string) { a.submits = append(a.submits, q) }
	a.search.onOpen = func(title string) { a.opens = append(a.opens, title) }
	a.search.quitFn = func() { a.quits++ }
	// The field is pre-filled, exactly as --offline fills it. Without it a test that
	// pressed Enter would be refused for an empty query, and the refusal would read
	// as a broken binding rather than as what it is.
	a.search.query.SetText(sampleQuery)
	apply(t, a.search, newOfflineSource(), sampleQuery)
	return a
}

// newBlankApp builds the screen with an EMPTY field, for the tests that type.
//
// It exists because newApp pre-fills the field the way --offline does, and a test
// that types into a pre-filled field has to know where the caret lands: SetText
// resets it to the start, so "abc" appended after "terminal user interface" reads as
// a bug in the field when it is really a fixture the test did not clear.
func newBlankApp(t *testing.T, w, h int) *app {
	t.Helper()
	a := newApp(t, w, h)
	a.search.query.SetText("")
	return a
}

// press offers one decoded key sequence to the program and reports whether it was
// consumed.
//
// It is ADR 0009 §2's loop reduced to the two lines that can change anything: the
// keymap first, and the tree only if nothing claimed it. The focus argument is the
// FOCUSED WIDGET, which is what run passes — see the note in main.go about why
// passing the root would quietly break any focus-scoped binding.
func (a *app) press(t testing.TB, seq string) bool {
	t.Helper()
	ev := decodeKey(t, seq)
	if _, consumed := a.search.km.Dispatch(ev, a.search.FocusWidget()); consumed {
		return true
	}
	return a.search.Handle(ev)
}

// tap presses a key and reports whether the program consumed it, failing the test if
// it did not.
//
// It exists because a keystroke the program ignores is a BUG in almost every test in
// this file, and a helper that fails makes each of them one line instead of three.
// It takes a testing.TB because the golden builders in search_test.go reach for it
// too, and a helper usable from only one file would be duplicated.
func tap(t testing.TB, a *app, seq string) bool {
	t.Helper()
	if !a.press(t, seq) {
		t.Errorf("the program did not consume %q", seq)
	}
	return true
}

// typeText presses each rune of s as a separate keystroke.
//
// Separate keystrokes rather than one EventText, because a real terminal delivers
// typing as keys and a test that pasted the whole query in one event would not
// exercise the per-rune path at all.
func typeText(t testing.TB, a *app, s string) {
	t.Helper()
	for _, r := range s {
		tap(t, a, string(r))
	}
}

// decodeKey runs one key sequence through the real decoder and returns the event it
// produced, failing the test if the sequence is not exactly one event.
func decodeKey(t testing.TB, seq string) termmosaic.Event {
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

// TestTheFieldAcceptsTyping is the first requirement of a form example and the one
// a global binding can break: a query field the user cannot type into.
//
// It is here, before anything else, because every other key test in this file is
// meaningless if a letter does not reach the field.
func TestTheFieldAcceptsTyping(t *testing.T) {
	a := newBlankApp(t, goldenWideW, goldenHighH)
	typeText(t, a, "go")
	if got := a.Query(); got != "go" {
		t.Errorf("the field holds %q after typing \"go\", want %q", got, "go")
	}
}

// TestQuitIsDeclinedWhileTheFieldHasFocus is the dangerous binding in this example,
// and it has one right answer.
//
// 'q' is bound to app.quit at ScopeGlobal, and the registry is asked BEFORE the tree.
// A naive global 'q' therefore swallows every q a reader types into a query — which
// is how a search box ends up untypable. The command DECLINES while the field has
// focus, the declined Run is the keymap's only fallthrough mechanism, and the 'q'
// reaches the field instead.
//
// Both halves are asserted, because either alone would pass for the wrong reason: a
// test that only checked the field would pass on a build where 'q' also quit, and a
// test that only checked the quit count would pass on a build where 'q' was dead.
func TestQuitIsDeclinedWhileTheFieldHasFocus(t *testing.T) {
	a := newBlankApp(t, goldenWideW, goldenHighH)

	tap(t, a, "q")
	if a.quits != 0 {
		t.Error("pressing q while typing a query quit the program")
	}
	if got := a.Query(); got != "q" {
		t.Errorf("the field holds %q after pressing q, want it to have received the q", got)
	}

	// With the field NOT focused — the table is — 'q' is an ordinary quit key again.
	tap(t, a, "\t")
	tap(t, a, "q")
	if a.quits != 1 {
		t.Errorf("pressing q with the results focused ran the quit command %d times, want 1", a.quits)
	}
}

// TestEscAndCtrlCQuitFromEitherPane is the other half of the quit contract: those
// two are not printable, so no field wants them and they work everywhere.
//
// Escape is NOT decoded from a bare ESC byte: the decoder waits out its ambiguity
// with what follows, because a lone ESC and the start of an escape sequence are the
// same three bytes. hello's tests say the same thing, so the event here is built
// rather than decoded — and it is built from the decoder's own vocabulary rather
// than as a raw byte.
func TestEscAndCtrlCQuitFromEitherPane(t *testing.T) {
	for _, ev := range []termmosaic.Event{
		termmosaic.SpecialKeyEvent(termmosaic.KeyEscape, 0),
		// Ctrl-C arrives as Ctrl+'c' rather than as 0x03, because the decoder has
		// already consumed the control byte; writing 0x03 here would be a chord no
		// terminal sends.
		termmosaic.KeyEvent('c', termmosaic.ModCtrl),
	} {
		a := newApp(t, goldenWideW, goldenHighH)
		if !a.dispatch(t, ev) {
			t.Errorf("%v was not consumed with the field focused", ev)
		}
		if a.quits != 1 {
			t.Errorf("%v ran quit %d times with the field focused, want 1", ev, a.quits)
		}
		b := newApp(t, goldenWideW, goldenHighH)
		tap(t, b, "\t")
		if !b.dispatch(t, ev) {
			t.Errorf("%v was not consumed with the results focused", ev)
		}
		if b.quits != 1 {
			t.Errorf("%v ran quit %d times with the results focused, want 1", ev, b.quits)
		}
	}
}

// dispatch offers one already-built event to the program, which is ADR 0009 §2's
// loop for an event there is no byte sequence for.
func (a *app) dispatch(t testing.TB, ev termmosaic.Event) bool {
	t.Helper()
	if _, consumed := a.search.km.Dispatch(ev, a.search.FocusWidget()); consumed {
		return true
	}
	return a.search.Handle(ev)
}

// TestTabMovesTheRingAndIsNotSwallowed is the other binding the widgets gave up.
//
// Neither the TextInput nor the Table consumes KeyTab, so a screen-scoped binding
// is the only thing that can own it — and it has to work from EITHER pane, which is
// why it is not focus-scoped.
func TestTabMovesTheRingAndIsNotSwallowed(t *testing.T) {
	a := newApp(t, goldenWideW, goldenHighH)
	if a.FocusIndex() != paneQuery {
		t.Fatalf("the ring starts on %q, want the query field", a.FocusLabel())
	}

	tap(t, a, "\t")
	if a.FocusIndex() != paneResults {
		t.Errorf("Tab moved the focus to %q, want the results", a.FocusLabel())
	}
	tap(t, a, "\t")
	if a.FocusIndex() != paneQuery {
		t.Errorf("Tab wrapped the focus to %q, want the query field", a.FocusLabel())
	}

	// Shift-Tab, and from the field it must go BACKWARD rather than being a second
	// forward step: the ring wraps, so backward from the first member is the last.
	tap(t, a, "\x1b[Z")
	if a.FocusIndex() != paneResults {
		t.Errorf("shift-tab moved the focus to %q, want the results", a.FocusLabel())
	}
	tap(t, a, "\x1b[Z")
	if a.FocusIndex() != paneQuery {
		t.Errorf("shift-tab wrapped the focus to %q, want the query field", a.FocusLabel())
	}
}

// TestTabKeepsTheQuery is the property a focus ring is judged on: moving the focus
// between panes must not cost the reader their work.
func TestTabKeepsTheQuery(t *testing.T) {
	a := newBlankApp(t, goldenWideW, goldenHighH)
	typeText(t, a, "terminal emulator")

	tap(t, a, "\t")
	tap(t, a, "\t")
	if got := a.Query(); got != "terminal emulator" {
		t.Errorf("the query reads %q after a round trip through the results", got)
	}
}

// TestUpAndDownLeaveTheFieldAndThenBelongToTheTable is the central scope assertion
// of this example, and it is in one test because the two halves are one mechanism.
//
//   - The field DECLINES Up and Down (textinput.go:506), so while the field has
//     focus the screen's arrow bindings are live and step to the results.
//   - The Table CONSUMES Up and Down, so once it has focus the screen's bindings are
//     gated off by their Enabled and the table's own contract receives the key,
//     moving its selection by a row.
//
// The failure this replaces is the one an ungated screen-scoped arrow binding
// causes: a twenty-row result list you can only reach with Tab, because the screen
// took the arrows for itself.
//
// It also pins the step as ONE-WAY, which is a consequence rather than a choice: a
// key the table consumes cannot be taken back by the screen in that pane, so Up from
// the top row moves nothing and the way back to the field is Shift-Tab. A ring whose
// arrows worked in both directions here would need the table to DECLINE Up at its
// first row, which is not a thing a widget can express.
func TestUpAndDownLeaveTheFieldAndThenBelongToTheTable(t *testing.T) {
	a := newApp(t, goldenWideW, goldenHighH)

	// From the field, Down moves to the results rather than doing nothing.
	if !a.search.km.Has(cmdResults) {
		t.Error("focus.results is unavailable while the field has focus, so Down would be dead")
	}
	tap(t, a, "\x1b[B")
	if a.FocusIndex() != paneResults {
		t.Fatalf("Down from the field left the focus on %q, want the results", a.FocusLabel())
	}

	// From the table, the same Down moves the SELECTION rather than the ring.
	before := a.Selected()
	tap(t, a, "\x1b[B")
	if a.FocusIndex() != paneResults {
		t.Errorf("Down with the results focused moved the focus to %q", a.FocusLabel())
	}
	if got := a.Selected(); got != before+1 {
		t.Errorf("Down with the results focused left the selection at %d, want %d", got, before+1)
	}

	// And Up walks back up the list, stopping at the top rather than stealing itself
	// back to the field.
	tap(t, a, "\x1b[A")
	if got := a.Selected(); got != before {
		t.Errorf("Up left the selection at %d, want %d", got, before)
	}
	tap(t, a, "\x1b[A")
	if got := a.Selected(); got != 0 {
		t.Errorf("Up at the top of the results left the selection on row %d, want row 0", got)
	}
	if a.FocusIndex() != paneResults {
		t.Errorf("Up at the top of the results took the focus to %q; a key the table consumes must stay the table's", a.FocusLabel())
	}
	if a.search.km.Has(cmdQuery) {
		t.Error("focus.query is live with the results focused, so Up would shadow the table's own arrow contract")
	}
}

// TestEnterSubmitsFromTheFieldAndIsLeftAloneByTheTable is the second declined key,
// and it is a case where the two panes' Enter mean DIFFERENT things.
//
// While the field has focus, Enter runs the search. While the table has focus, Enter
// is the table's own activation and asks for the selected article. A screen-scoped
// binding with no gate would take Enter in both panes and the second one would be
// dead — which is why the command's Enabled is the field's focus and why the
// fallthrough to the tree is the whole mechanism.
func TestEnterSubmitsFromTheFieldAndIsLeftAloneByTheTable(t *testing.T) {
	a := newApp(t, goldenWideW, goldenHighH)

	tap(t, a, "\r")
	if len(a.submits) != 1 || a.submits[0] != sampleQuery {
		t.Errorf("Enter in the field submitted %v, want one submit of the sample query", a.submits)
	}
	if len(a.opens) != 0 {
		t.Errorf("Enter in the field asked for an article: %v", a.opens)
	}

	// Move to the results and press it again. The submit must NOT run — it is gated
	// off — and the table's activation must.
	a.search.SetSearching(false)
	tap(t, a, "\t")
	if a.search.km.Has(cmdSubmit) {
		t.Error("search.submit is available with the results focused, so Enter would shadow the table's own activation")
	}
	tap(t, a, "\r")
	if len(a.submits) != 1 {
		t.Errorf("Enter in the results submitted again: %v", a.submits)
	}
	if len(a.opens) != 1 || a.opens[0] != "Text-based user interface" {
		t.Errorf("Enter in the results asked for %v, want the selected article", a.opens)
	}
}

// TestHomeAndEndStillMoveTheCaretAndTheSelection is the assertion for the keys this
// screen deliberately does NOT bind.
//
// The bare forms belong to the focused widget: the field moves the caret with them
// and the table selects the first and last row. A screen-scoped binding on Home
// would outrank BOTH by specificity and silently break both — a search field whose
// caret cannot go home and a table whose rows cannot be reached by End.
func TestHomeAndEndStillMoveTheCaretAndTheSelection(t *testing.T) {
	a := newBlankApp(t, goldenWideW, goldenHighH)
	typeText(t, a, "abcdef")
	// The caret is at the end after typing; Home must take it to the start.
	tap(t, a, "\x1b[H")
	if got := a.query.Cursor(); got != 0 {
		t.Errorf("Home left the caret at %d, want 0 — a screen-scoped Home binding would do this", got)
	}
	tap(t, a, "\x1b[F")
	if got := a.query.Cursor(); got != len("abcdef") {
		t.Errorf("End left the caret at %d, want %d", got, len("abcdef"))
	}

	// And in the table, the same two keys select the first and last row.
	tap(t, a, "\t")
	if got := a.Selected(); got != 0 {
		t.Errorf("the results start on row %d, want 0", got)
	}
	tap(t, a, "\x1b[F")
	if got, want := a.Selected(), a.results.Rows()-1; got != want {
		t.Errorf("End left the selection on row %d, want the last row %d", got, want)
	}
	tap(t, a, "\x1b[H")
	if got := a.Selected(); got != 0 {
		t.Errorf("Home left the selection on row %d, want the first row", got)
	}
}

// TestCtrlHomeAndCtrlEndJumpTheRing is what the ring's ends are bound to instead,
// and the test is here so the choice is checked rather than merely argued.
func TestCtrlHomeAndCtrlEndJumpTheRing(t *testing.T) {
	a := newApp(t, goldenWideW, goldenHighH)
	tap(t, a, "\t")
	if a.FocusIndex() != paneResults {
		t.Fatalf("the focus is on %q", a.FocusLabel())
	}
	tap(t, a, "\x1b[1;5H") // ctrl+home
	if a.FocusIndex() != paneQuery {
		t.Errorf("ctrl+home left the focus on %q, want the query field", a.FocusLabel())
	}
	tap(t, a, "\x1b[1;5F") // ctrl+end
	if a.FocusIndex() != paneResults {
		t.Errorf("ctrl+end left the focus on %q, want the results", a.FocusLabel())
	}
}

// TestNoScreenBindingStealsAFocusedWidgetsKey is the general form of the two tests
// above, and it is the property this example exists to demonstrate.
//
// It walks EVERY binding in the registry, dispatches its chord with each pane
// focused, and asserts that the key either runs a command of this screen's OR
// reaches the focused widget. There is no third answer: a chord the registry claims
// is not a chord the widget gets, and the failure mode is a field that will not take
// a letter or a table that will not scroll.
//
// The list of widget-owned chords is PER PANE rather than one list for both, and the
// difference is the whole of the Up/Down case: form.TextInput DECLINES Up and Down
// on purpose, so in the query pane they are not the widget's to lose — while
// data.Table consumes them, so in the results pane they are. A single merged list
// would report the one binding this example deliberately makes as a defect.
//
// It is written against the registry's own descriptions rather than a hand-written
// command list, so a binding added tomorrow is covered without this test being edited.
func TestNoScreenBindingStealsAFocusedWidgetsKey(t *testing.T) {
	owned := map[int]map[string]bool{
		paneQuery: {
			// form.TextInput. Up, Down, Enter and Tab are absent because the widget
			// documents declining all four.
			"a": true, "Backspace": true, "Del": true, "Left": true, "Right": true,
			"Home": true, "End": true, "Ctrl+a": true, "Ctrl+z": true, "Ctrl+w": true,
			"Ctrl+d": true, "Ctrl+e": true, "Space": true,
		},
		paneResults: {
			// data.Table. KeyTab is absent because the widget documents declining it.
			"Up": true, "Down": true, "PgUp": true, "PgDn": true, "Home": true, "End": true,
			"Left": true, "Right": true, "Enter": true, "Space": true,
		},
	}

	for _, pane := range []int{paneQuery, paneResults} {
		a := newApp(t, goldenWideW, goldenHighH)
		a.search.setFocus(pane)
		for _, entry := range a.search.km.Describe(keymap.ScopeScreen) {
			for _, chord := range entry.Chords {
				if !owned[pane][chord.String()] {
					continue
				}
				ev := chordEvent(t, chord)
				if _, consumed := a.search.km.Dispatch(ev, a.search.FocusWidget()); consumed {
					t.Errorf("with %q focused, %s is bound to %s at screen scope and takes a key the "+
						"focused widget owns; either scope it to the widget or gate it on that widget's focus",
						focusLabels[pane], chord, entry.ID)
				}
			}
		}
	}
}

// chordEvent turns a Chord back into an Event, which is the only way to feed
// Dispatch without going through the decoder — and it is the one place in this file
// that does so, because the chords come from the registry rather than from source.
//
// It goes through ChordOf by way of a synthesised event so that the normalisation
// rules are applied once, by keymap, rather than reconstructed here.
func chordEvent(t *testing.T, c keymap.Chord) termmosaic.Event {
	t.Helper()
	ev := termmosaic.KeyEvent(c.Rune, c.Mod)
	if c.Key != termmosaic.KeyNone {
		ev = termmosaic.SpecialKeyEvent(c.Key, c.Mod)
	}
	got, ok := keymap.ChordOf(ev)
	if !ok {
		t.Fatalf("the chord %s normalised away", c)
	}
	if got != c {
		t.Fatalf("the chord %s did not survive a round trip through an event; it became %s", c, got)
	}
	return ev
}

// TestEveryCommandIsReachableByADispatch is the test that replaces the old
// "the bindings are what I wrote" assertion.
//
// It reads the registry's own Describe output and asserts that Dispatch CONSUMES
// every chord it advertises. A binding that Describe reports but Dispatch declines
// is a documented key that does nothing — which is precisely the failure mode a hint
// built from the registry would otherwise publish to the reader.
//
// Both panes are checked, because a command gated on one pane's focus is
// legitimately declined in the other; the test asserts that every chord is consumed
// in AT LEAST one pane, and TestHintMatchesWhatDispatchDoes covers the rest.
func TestEveryCommandIsReachableByADispatch(t *testing.T) {
	for _, entry := range newApp(t, goldenWideW, goldenHighH).search.km.Describe(keymap.ScopeScreen) {
		for _, chord := range entry.Chords {
			ok := false
			for _, pane := range []int{paneQuery, paneResults} {
				a := newApp(t, goldenWideW, goldenHighH)
				a.search.setFocus(pane)
				if _, consumed := a.search.km.Dispatch(chordEvent(t, chord), a.search.FocusWidget()); consumed {
					ok = true
					break
				}
			}
			if !ok {
				t.Errorf("the registry describes %s as %s but Dispatch declines it in every pane",
					chord, entry.ID)
			}
		}
	}
}

// TestHintIsRenderedFromTheRegistry is the anti-drift property.
//
// The hint is compared against a KeyHint fed from the registry alone, so a binding
// renamed or a description reworded changes what the hint says on the next
// construction and there is no second place a key can be spelled.
//
// It is SEPARATE from TestHintNamesOnlyLiveCommands because the two failures are
// different: this one catches a hint that has drifted from the bindings, and that
// one catches a hint that is faithful to the bindings and still wrong about the pane.
func TestHintIsRenderedFromTheRegistry(t *testing.T) {
	for _, pane := range []int{paneQuery, paneResults} {
		a := newApp(t, goldenWideW, goldenHighH)
		a.search.setFocus(pane)

		want := form.NewKeyHint(buffer.Rect{}, nil)
		want.SetEntries(a.search.hintEntries())

		if len(a.search.hint.Bindings) != len(want.Bindings) {
			t.Errorf("with %q focused the hint has %d rows, want %d",
				focusLabels[pane], len(a.search.hint.Bindings), len(want.Bindings))
			continue
		}
		for i, b := range a.search.hint.Bindings {
			if b != want.Bindings[i] {
				t.Errorf("with %q focused, hint row %d is %+v, want %+v — the hint is not what the registry says",
					focusLabels[pane], i, b, want.Bindings[i])
			}
		}
	}
}

// TestHintNamesOnlyLiveCommands is the property that makes the hint honest, and it
// is separate from the anti-drift test because it is a different failure.
//
// A hint built from DescribeGrouped alone advertises every screen-scoped binding,
// including the ones whose Enabled is false in this pane. Enter would be described as
// "run the search" on a screen where the table has focus and Enter opens the
// article — a hint that lies about the key the reader is most likely to press.
func TestHintNamesOnlyLiveCommands(t *testing.T) {
	a := newApp(t, goldenWideW, goldenHighH)

	tap(t, a, "\t") // focus the results
	if a.search.km.Has(cmdSubmit) {
		t.Fatal("search.submit is live with the results focused")
	}
	for _, b := range a.search.hint.Bindings {
		if b.Help == "run the search" {
			t.Errorf("the hint offers %q while the results are focused, where Enter opens the article instead", b.Key)
		}
	}

	tap(t, a, "\x1b[Z") // shift-tab, back to the field
	if !a.search.km.Has(cmdSubmit) {
		t.Fatal("search.submit is not live with the field focused")
	}
	found := false
	for _, b := range a.search.hint.Bindings {
		if b.Help == "run the search" {
			found = true
		}
	}
	if !found {
		t.Error("the hint does not offer the search with the field focused")
	}
}

// TestEveryHintChordDoesSomethingInEveryPane is the guarantee the hint makes to the
// reader, checked exhaustively.
//
// It is the "no dead keys" property in its strongest form: every chord the hint
// names, in the pane whose hint names it, either runs a command or reaches the
// focused widget. A chord that does neither is a key the screen advertises and
// ignores, which is the failure ADR 0009's bad list names as the worst one a TUI
// has.
func TestEveryHintChordDoesSomethingInEveryPane(t *testing.T) {
	for _, pane := range []int{paneQuery, paneResults} {
		a := newApp(t, goldenWideW, goldenHighH)
		a.search.setFocus(pane)
		if len(a.search.hint.Bindings) == 0 {
			t.Fatalf("with %q focused the hint is empty", focusLabels[pane])
		}
		for _, entry := range a.search.hintEntries() {
			for _, chord := range entry.Chords {
				ev := chordEvent(t, chord)
				consumed := false
				if _, ok := a.search.km.Dispatch(ev, a.search.FocusWidget()); ok {
					consumed = true
				}
				if !consumed && !a.search.Handle(ev) {
					t.Errorf("with %q focused, the hint names %s for %s and nothing consumed it",
						focusLabels[pane], chord, entry.ID)
				}
			}
		}
	}
}

// TestTheHintIsOnScreenAndDerived asserts the two halves together: the line is
// actually drawn, and its TEXT is the registry's.
//
// The rendered form is checked rather than the bindings, because what a reader sees
// is the bracketed line, and a bindings check would pass on a widget that never
// painted anything.
func TestTheHintIsOnScreenAndDerived(t *testing.T) {
	a := newApp(t, goldenWideW, goldenHighH)
	got := screenAt(t, goldenWideW, goldenHighH, 2, a.search)

	if !strings.Contains(got, "[Enter] run the search") {
		t.Errorf("the pinned hint line does not read as the registry's\n--- screen ---\n%s", got)
	}
	if !strings.Contains(got, "[Tab] next pane") {
		t.Errorf("the pinned hint line does not name the ring's Tab step\n--- screen ---\n%s", got)
	}
	if !strings.Contains(got, "quit") {
		t.Errorf("the pinned hint line does not say how to quit\n--- screen ---\n%s", got)
	}
}

// TestRegistryIsQuiet is the registry's own diagnostic surface, asserted empty.
//
// A registry that warns about itself is not demonstrating much, and every warning it
// can raise here would be a real finding: a global binding with an owner, a scoped
// one without, a shadowed chord, or a command nobody can reach.
func TestRegistryIsQuiet(t *testing.T) {
	for _, w := range newApp(t, goldenWideW, goldenHighH).search.km.Warnings() {
		t.Errorf("the registry warns: %s", w)
	}
}

// TestFocusWidgetIsTheFocusedWidgetAndNotTheRoot pins the Dispatch argument.
//
// Nothing in this example binds at ScopeFocus, so this test cannot fail through a
// dead binding — which is exactly why it is worth having. It asserts the thing the
// NEXT focus-scoped binding will depend on, and it fails loudly if someone "simplifies"
// the call site to pass the root.
func TestFocusWidgetIsTheFocusedWidgetAndNotTheRoot(t *testing.T) {
	a := newApp(t, goldenWideW, goldenHighH)

	if got := a.search.FocusWidget(); got != termmosaic.Widget(a.search.query) {
		t.Errorf("with the field focused the Dispatch argument is %T, want the TextInput", got)
	}
	tap(t, a, "\t")
	if got := a.search.FocusWidget(); got != termmosaic.Widget(a.search.results) {
		t.Errorf("with the results focused the Dispatch argument is %T, want the Table", got)
	}
}

// TestDescribeScopeFocusCannotNarrowToTheFocusedWidget documents the gap this
// example works around, so the workaround cannot be quietly deleted and the gap
// cannot be quietly forgotten.
//
// keymap.Registry has SetScreen and no SetFocus. inScope short-circuits an
// EXACT-scope match without consulting liveness, so DescribeGrouped(ScopeFocus)
// returns every focus-scoped binding regardless of which widget actually has focus.
// An application that built its hint from that query would advertise the field's
// bindings on a screen where the table has focus.
//
// The test asserts the behaviour as it is, and TestHintNamesOnlyLiveCommands asserts
// the workaround that answers it.
func TestDescribeScopeFocusCannotNarrowToTheFocusedWidget(t *testing.T) {
	a := newApp(t, goldenWideW, goldenHighH)

	// Bind a focus-scoped command to the FIELD, exactly as a caller following
	// keymap's documentation would, and give the table one of its own.
	for _, spec := range []struct {
		id    keymap.CommandID
		owner termmosaic.Widget
	}{
		{"probe.field", a.search.query},
		{"probe.table", a.search.results},
	} {
		if err := a.search.km.BindString("F5", spec.id, keymap.ScopeFocus, spec.owner); err != nil {
			t.Fatal(err)
		}
	}
	a.search.km.Register(
		keymap.Command{ID: "probe.field", Desc: "field probe", Run: func(keymap.Ctx) bool { return true }},
		keymap.Command{ID: "probe.table", Desc: "table probe", Run: func(keymap.Ctx) bool { return true }},
	)

	// Dispatch resolves correctly — the field's binding is live only for the field.
	a.search.setFocus(paneQuery)
	if id, ok := a.search.km.Dispatch(chordEvent(t, mustChord(t, "F5")), a.search.FocusWidget()); !ok || id != "probe.field" {
		t.Errorf("F5 with the field focused ran %q (consumed %v), want probe.field", id, ok)
	}
	a.search.setFocus(paneResults)
	if id, ok := a.search.km.Dispatch(chordEvent(t, mustChord(t, "F5")), a.search.FocusWidget()); !ok || id != "probe.table" {
		t.Errorf("F5 with the results focused ran %q (consumed %v), want probe.table", id, ok)
	}

	// And the describe query cannot tell them apart: both bindings are reported,
	// whichever pane has focus. That is the gap, and it is what the hint's own
	// Has() filter works around rather than inheriting.
	for _, pane := range []int{paneQuery, paneResults} {
		a.search.setFocus(pane)
		var sawField, sawTable bool
		for _, e := range a.search.km.DescribeGrouped(keymap.ScopeFocus) {
			switch e.ID {
			case "probe.field":
				sawField = true
			case "probe.table":
				sawTable = true
			}
		}
		if !sawField || !sawTable {
			t.Errorf("with %q focused, DescribeGrouped(ScopeFocus) reported field=%v table=%v; "+
				"it should report both, because it cannot narrow to the focused widget without a SetFocus",
				focusLabels[pane], sawField, sawTable)
		}
	}
}

// mustChord parses a chord literal or fails, for the two places a test needs one
// outside the registry's own output.
func mustChord(t *testing.T, s string) keymap.Chord {
	t.Helper()
	c, err := keymap.ParseChord(s)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// TestAPasteIsOneUndoStepInTheField is the ADR 0005 §4 property, carried through the
// screen's own paste path.
//
// It is here rather than in the widget's own tests because the screen ROUTES pastes:
// it goes straight to the tree and never through Dispatch, and a routing mistake
// would expand a paste into one undo step per character — invisible in the widget's
// tests and expensive in a real session.
func TestAPasteIsOneUndoStepInTheField(t *testing.T) {
	a := newBlankApp(t, goldenWideW, goldenHighH)
	tap(t, a, "g")

	paste := termmosaic.Event{Kind: termmosaic.EventPaste, Text: "o lang"}
	if a.search.Handle(paste) {
		if got := a.Query(); got != "go lang" {
			t.Errorf("after the paste the field holds %q, want %q", got, "go lang")
		}
	} else {
		t.Error("the screen did not route the paste to the focused field")
	}

	// One undo removes the whole of it.
	if !a.query.Undo() {
		t.Fatal("there was nothing to undo after a paste")
	}
	if got := a.Query(); got != "g" {
		t.Errorf("one undo left the field holding %q, want the single pre-paste character %q", got, "g")
	}
}

// TestResizeReachesTheScreen asserts the one non-key event the screen claims.
//
// A resize is NOT a command — ADR 0009 §2 rule 1 — so it goes straight to the tree,
// and the screen has to turn it into a new rectangle. Without this the screen would
// keep drawing at the old size and the renderer would clip the difference away.
func TestResizeReachesTheScreen(t *testing.T) {
	a := newApp(t, goldenWideW, goldenHighH)
	if !a.search.Handle(termmosaic.ResizeEvent(60, 20)) {
		t.Error("the screen did not claim a resize")
	}
	if got := a.search.Bounds(); got.W != 60 || got.H != 20 {
		t.Errorf("after the resize the screen's rectangle is %v, want 60x20", got)
	}
	if a.search.lay.valid {
		t.Error("the layout cache survived a resize, so the next frame would use the old arrangement")
	}
}

// TestMouseClickTakesFocus is the interaction a mouse-capturing screen owes the
// reader: clicking a row selects it AND makes the table the keyboard's target.
//
// Neither half is something a widget can do on its own, which is why the screen
// exists as a router rather than as a container.
//
// The click is at the SECOND body row, and the offset from the table's rectangle is
// three cells: one for the block's border, one for its padding, and one for the
// header. Counting them here rather than guessing is deliberate — a click test that
// hit the wrong row would still take focus and would pass on the wrong assertion.
func TestMouseClickTakesFocus(t *testing.T) {
	a := newApp(t, goldenWideW, goldenHighH)
	renderAt(t, goldenWideW, goldenHighH, 1, a.search)

	row := a.search.results.Bounds()
	y := row.Y + 2 // border + padding = the first body row
	x := row.X + 4
	click := termmosaic.Event{
		Kind:  termmosaic.EventMouse,
		Mouse: termmosaic.Mouse{X: x, Y: y, Action: termmosaic.MousePress, Button: termmosaic.MouseLeft},
	}
	if !a.search.Handle(click) {
		t.Fatalf("a click at (%d,%d) inside the results was not consumed", x, y)
	}
	if a.FocusIndex() != paneResults {
		t.Errorf("clicking the results left the focus on %q, want the results", a.FocusLabel())
	}
	if got := a.Selected(); got != 0 {
		t.Errorf("clicking the first body row selected %d, want 0", got)
	}

	// A click one row lower selects that row, which is the half of the assertion
	// that the hit test is real rather than a focus switch dressed up as one.
	y = row.Y + 3
	if !a.search.Handle(termmosaic.Event{
		Kind:  termmosaic.EventMouse,
		Mouse: termmosaic.Mouse{X: x, Y: y, Action: termmosaic.MousePress, Button: termmosaic.MouseLeft},
	}) {
		t.Fatalf("a click at (%d,%d) was not consumed", x, y)
	}
	if got := a.Selected(); got != 1 {
		t.Errorf("clicking the second body row selected %d, want 1", got)
	}

	// And a click on the field takes the keyboard back, placing the caret.
	f := a.search.query.Bounds()
	if !a.search.Handle(termmosaic.Event{
		Kind:  termmosaic.EventMouse,
		Mouse: termmosaic.Mouse{X: f.X + 3, Y: f.Y, Action: termmosaic.MousePress, Button: termmosaic.MouseLeft},
	}) {
		t.Fatal("a click inside the query field was not consumed")
	}
	if a.FocusIndex() != paneQuery {
		t.Errorf("clicking the query field left the focus on %q, want the query", a.FocusLabel())
	}
	if got := a.query.Cursor(); got != 3 {
		t.Errorf("clicking the fourth cell of the field put the caret at %d, want 3", got)
	}
}

// TestIdleFrameWritesNothing is the diff's premise stated as a test: a frame on which
// nothing changed writes nothing, which is what makes the pacer's target rate free.
//
// It is the frame-path allocation test's silent twin. One says Draw builds nothing;
// this says the renderer therefore has nothing to send. A screen that re-published
// an unchanged frame would cost the same bandwidth and would not fail the other
// test, because nothing about it allocates.
func TestIdleFrameWritesNothing(t *testing.T) {
	s := screen(t, goldenWideW, goldenHighH)

	sink := headless.NewMemorySink(goldenWideW, goldenHighH)
	r := render.New(sink, render.Config{
		Width: goldenWideW, Height: goldenHighH, Caps: termmosaic.DefaultCaps(),
	})
	r.SetRoot(s)
	if _, err := r.Render(); err != nil {
		t.Fatal(err)
	}
	// Settle: a screen that never settles would make this test pass vacuously.
	if _, err := r.Render(); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 5; i++ {
		n, err := r.Render()
		if err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Fatalf("idle frame %d wrote %d bytes; an unchanged screen must send nothing", i, n)
		}
	}
}

// TestHintSurvivesAResize asserts the hint's cached line is keyed on its own
// rectangle, so a resize rebuilds it rather than leaving the old width's
// truncation on screen.
//
// A hint truncated for 120 cells shown in a 50-cell screen is a hint that names
// bindings past the right edge, which is the failure markets' own comments describe
// for a widget that cached its line against the wrong thing.
func TestHintSurvivesAResize(t *testing.T) {
	a := newApp(t, goldenWideW, goldenHighH)
	renderAt(t, goldenWideW, goldenHighH, 1, a.search)

	narrow := goldenNarrowW
	renderAt(t, narrow, goldenNarrowH, 2, a.search)
	got := screenAt(t, narrow, goldenNarrowH, 2, a.search)

	if !strings.Contains(got, "[Enter] run the search") {
		t.Errorf("after a resize the hint is no longer derived from the registry\n--- screen ---\n%s", got)
	}
}
