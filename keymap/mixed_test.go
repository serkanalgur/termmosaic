package keymap_test

// This file is the observation ADR 0009 §"Risks to revisit at v1.0" item 1 says
// nobody has made: an application where a global binding and a widget's own
// switch in Handle both answer keys.
//
// The risk is named precisely in §Consequences: "An application that binds `q`
// globally and also has a `switch` case for `q` in a widget will find the
// widget's case unreachable for the focused context." That is argued at length
// in the ADR and, until now, measured by nobody — docs/STATUS.md lists it as
// risk 2 for v1.0 and says it is "mitigated by building a mixed-mechanism
// example before the tag".
//
// So this is that observation, in a test rather than in examples/. A test earns
// its place here for three reasons: it runs in CI on every push, it asserts
// rather than prints, and an example program would be a fourth thing to keep
// correct. What it demonstrates is below, in the comments on each test, and the
// summary is the file's closing comment.

import (
	"strings"
	"testing"

	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/keymap"
)

// searchBox is a widget in the oldest style there is: its own switch on ev.Key,
// no Commandable, no Clickable. It is the shape 24 widgets in the catalog have,
// and the shape ADR 0009 was written so that it does NOT have to change.
//
// It deliberately handles `q` as well as two keys nothing binds globally,
// because the collision is the point.
type searchBox struct {
	bounds termmosaic.Rect
	// handled records, in order, the keys this widget's own switch acted on.
	handled []string
	// text is the field's contents, so a test can assert the rune reached it.
	text string
}

func (w *searchBox) Bounds() termmosaic.Rect { return w.bounds }
func (w *searchBox) Draw(*buffer.Buffer)     {}
func (w *searchBox) Invalidate()             {}

func (w *searchBox) Handle(ev termmosaic.Event) bool {
	if ev.Kind != termmosaic.EventKey {
		return false
	}
	switch {
	case ev.Key == termmosaic.KeyEnter:
		w.handled = append(w.handled, "submit")
		return true
	case ev.Rune == '/':
		w.handled = append(w.handled, "open-search")
		return true
	case ev.Rune == 'q':
		w.handled = append(w.handled, "close")
		return true
	case ev.Rune != 0:
		// A text field's whole job: every printable rune, bound or not.
		w.text += string(ev.Rune)
		w.handled = append(w.handled, "insert")
		return true
	default:
		return false
	}
}

// keyLogger is the application-level fallback: the root widget the tree offers
// an unconsumed event to, which is where a real application puts its own
// routing.
type keyLogger struct {
	bounds  termmosaic.Rect
	handled []string
}

func (w *keyLogger) Bounds() termmosaic.Rect { return w.bounds }
func (w *keyLogger) Draw(*buffer.Buffer)     {}
func (w *keyLogger) Invalidate()             {}
func (w *keyLogger) Handle(ev termmosaic.Event) bool {
	if ev.Kind == termmosaic.EventKey {
		w.handled = append(w.handled, "root:"+chordOfEvent(ev))
		return true
	}
	return false
}

func chordOfEvent(ev termmosaic.Event) string {
	c, ok := keymap.ChordOf(ev)
	if !ok {
		return "?"
	}
	return c.String()
}

// application is the whole of §2.2's loop, as a helper so every test in this
// file runs the SAME loop an application would. If the loop is where the
// behaviour lives, the loop is what the test has to use.
type application struct {
	km     *keymap.Registry
	focus  termmosaic.Widget
	root   termmosaic.Widget
	counts *counts
	// log is every step the loop took, in order, which is what makes the
	// precedence observable rather than inferred.
	log []string
}

// step is §2.2's loop body for one event, transcribed. The four branches are the
// ADR's four branches and the order is the ADR's order.
func (a *application) step(ev termmosaic.Event) {
	switch ev.Kind {
	case termmosaic.EventKey, termmosaic.EventMouse:
		if id, ok := a.km.Dispatch(ev, a.focus); ok {
			a.log = append(a.log, "keymap:"+string(id))
			return
		}
		a.log = append(a.log, "unconsumed")
		if a.focus != nil && a.focus.Handle(ev) {
			a.log = append(a.log, "focus")
			return
		}
		if a.root.Handle(ev) {
			a.log = append(a.log, "root")
		}
	case termmosaic.EventResize:
		a.log = append(a.log, "resize")
	default:
		// Paste, focus, compose: straight to the tree, never Dispatch.
		a.log = append(a.log, "bypass")
		if a.focus != nil && a.focus.Handle(ev) {
			a.log = append(a.log, "focus")
			return
		}
		a.root.Handle(ev)
	}
}

// newApplication wires the mixed-mechanism shape: a search box with its own
// switch, an application-level `q` quit binding, and a global `/` binding that
// the widget also handles.
func newApplication(t *testing.T) (*application, *searchBox, *keyLogger) {
	t.Helper()
	box := &searchBox{bounds: termmosaic.Rect{X: 0, Y: 0, W: 20, H: 3}}
	root := &keyLogger{bounds: termmosaic.Rect{X: 0, Y: 0, W: 80, H: 24}}

	km := keymap.New()
	quits := 0
	opensSearch := 0
	km.Register(
		keymap.Command{ID: "app.quit", Desc: "quit the program", Group: "app",
			Run: func(keymap.Ctx) bool { quits++; return true }},
		keymap.Command{ID: "app.open-search", Desc: "open the search bar", Group: "app",
			Run: func(keymap.Ctx) bool { opensSearch++; return true }},
		keymap.Command{ID: "search.submit", Desc: "run the search", Group: "search",
			Run: func(keymap.Ctx) bool { return true }},
	)
	// `q` is bound globally AND handled in the widget's own switch. This is the
	// overlap the ADR names as its sharpest edge.
	if err := km.BindString("q", "app.quit", keymap.ScopeGlobal, nil); err != nil {
		t.Fatal(err)
	}
	// `/` is bound globally AND handled in the widget's own switch, and this one
	// is the harder case: the binding SHADOWS the widget's case, so the user
	// loses the widget's behaviour and the keymap's runs instead.
	if err := km.BindString("/", "app.open-search", keymap.ScopeGlobal, nil); err != nil {
		t.Fatal(err)
	}
	km.Seal()
	// The widget is NOT Commandable, so its bindings are not in the registry —
	// which is why `search.submit` is registered with no chord.
	km.Attach(box, root)

	app := &application{km: km, focus: box, root: root}
	app.counts = &counts{quits: &quits, searches: &opensSearch}
	return app, box, root
}

// last returns the most recent log entry, which is what a test asserting one
// dispatch wants; the full trace is for the tests that care about the whole
// path.
func (a *application) last() string {
	if len(a.log) == 0 {
		return ""
	}
	return a.log[len(a.log)-1]
}

// counts lets a test read the handler counters the commands wrote to.
type counts struct {
	quits    *int
	searches *int
}

func (a *application) trace() string { return strings.Join(a.log, " > ") }

// TestAMechanismThatBindsAKeyTheWidgetAlsoHandles is the observation.
//
// WHAT IT DEMONSTRATES, in the order the log records it:
//
//  1. The keymap wins. `q` reaches app.quit, and the widget's own `case 'q'`
//     never runs — the log shows "keymap:app.quit" with no "focus" step, and the
//     widget's handled list never gains "close".
//
//  2. The widget's case is not merely bypassed, it is UNREACHABLE for as long as
//     the binding exists. This is the sharp edge §Consequences names, now
//     observed: a developer editing searchBox.Handle to change what `q` does
//     will see no effect and no error.
//
//  3. The keymap declining is what makes the fallback work. A key nothing binds
//     reaches the widget, so the widget is the fallback and not dead code —
//     which is the whole justification for Option A in the ADR.
//
//  4. A key the widget handles and the keymap does not reaches the ROOT widget
//     when the focused widget declines, which is ADR 0003's focused-first-then-
//     tree order, unchanged.
//
// The one thing this test cannot show is the failure mode a user would actually
// report, because there is no user here: what it shows is that the precedence
// is total and silent, which is the property that makes the failure mode
// possible.
func TestAMechanismThatBindsAKeyTheWidgetAlsoHandles(t *testing.T) {
	app, box, _ := newApplication(t)

	// `q` is bound globally and also handled by the widget.
	app.step(termmosaic.KeyEvent('q', 0))
	if got := app.trace(); got != "keymap:app.quit" {
		t.Errorf("q produced %q, want %q; the keymap shadows the widget's own case (ADR 0009 §2.3)", got, "keymap:app.quit")
	}
	if got := *app.counts.quits; got != 1 {
		t.Errorf("app.quit ran %d times, want 1", got)
	}
	for _, h := range box.handled {
		if h == "close" {
			t.Fatal("the widget's own `case 'q'` ran; the keymap did not win")
		}
	}
	if len(box.handled) != 0 {
		t.Errorf("the widget handled %v, want nothing; it was never offered the event", box.handled)
	}

	// The shadowing is a property of the BINDING, not of the event: unbind and
	// the same key reaches the same widget case, with no other change. That is
	// the diagnostic a developer needs and the one the ADR's `Handle` doc
	// paragraph points at.
	esc, err := keymap.ParseChord("q")
	if err != nil {
		t.Fatal(err)
	}
	app.km.Unbind(esc, keymap.ScopeGlobal, nil)
	app.step(termmosaic.KeyEvent('q', 0))
	if got := app.trace(); got != "keymap:app.quit > unconsumed > focus" {
		t.Errorf("after Unbind, q produced %q, want the keymap to decline and the widget to answer", got)
	}
	if got := box.handled; len(got) != 1 || got[0] != "close" {
		t.Errorf("the widget handled %v, want [close]; the same key, unbound, reaches the same case", got)
	}
}

// TestTheShadowedKeyIsSilent is the half of the observation that matters most,
// and it is the part no allocation test or type check would catch: nothing
// anywhere reports that searchBox.Handle has a case for a key the keymap eats.
//
// `Warnings` is the mechanism the ADR nominates for exactly this ("the fix, if
// it bites, is Warnings() reporting a chord that is both bound and handled by an
// attached widget, not a Widget change"), and it is NOT implemented here —
// because the registry cannot know what a widget's Handle does. That is not an
// oversight in this implementation; it is a limit on what a Registry can see,
// and this test is what makes the limit concrete rather than theoretical.
func TestTheShadowedKeyIsSilent(t *testing.T) {
	app, box, _ := newApplication(t)
	app.step(termmosaic.KeyEvent('q', 0))
	app.step(termmosaic.KeyEvent('/', 0))

	// The shadowing happened. Both bindings ran.
	if *app.counts.quits != 1 || *app.counts.searches != 1 {
		t.Fatalf("the keymap did not run both commands: quits=%d searches=%d", *app.counts.quits, *app.counts.searches)
	}
	// And the widget's own cases for those two keys did not.
	for _, h := range box.handled {
		if h == "close" || h == "open-search" {
			t.Errorf("the widget handled %q, want the keymap to have shadowed it", h)
		}
	}
	// And nothing in the registry's diagnostic surface mentions it.
	for _, w := range app.km.Warnings() {
		if strings.Contains(w, "shadow") || strings.Contains(w, "unreachable") || strings.Contains(w, "handled") {
			t.Errorf("Warnings reported %q, which would mean the shadowing is detectable; this implementation cannot detect it and does not claim to", w)
		}
	}
	// The registry cannot know, and this is why: the widget is not Commandable,
	// so the only thing the registry was ever told about it is its bounds.
	if _, ok := any(box).(keymap.Commandable); ok {
		t.Fatal("searchBox unexpectedly implements Commandable")
	}
	// The mitigation the ADR names is documentation, and documentation is what
	// widget.go now carries — TestHandleDocumentsPrecedence in handle_doc_test.go
	// reads the file and checks the paragraph is still there, because a sentence
	// lost in a refactor is the whole failure this package's sharpest edge has.
}

// TestUnboundKeysStillReachTheTree is the property that makes Option A safe, and
// it is the one a full command layer would have broken: a text field must still
// receive every printable rune, including the ones no command is bound to.
func TestUnboundKeysStillReachTheTree(t *testing.T) {
	app, box, _ := newApplication(t)

	// A rune nothing binds reaches the widget and lands in its text.
	app.step(termmosaic.KeyEvent('h', 0))
	app.step(termmosaic.KeyEvent('i', 0))
	if box.text != "hi" {
		t.Errorf("the field holds %q, want %q; a key no command consumes must still reach a text field", box.text, "hi")
	}
	if got := app.trace(); got != "unconsumed > focus > unconsumed > focus" {
		t.Errorf("two unbound runes produced %q, want each to fall through to the focused widget", got)
	}

	// Enter is handled by the widget and bound to nothing.
	app.step(termmosaic.SpecialKeyEvent(termmosaic.KeyEnter, 0))
	if got := box.handled[len(box.handled)-1]; got != "submit" {
		t.Errorf("the widget handled %q last, want submit", got)
	}
}

// TestTheKeymapNeverSeesAPaste is the rule that would be cheapest to get wrong
// and most expensive to discover, and a mixed-mechanism application is exactly
// where it would be got wrong: the widget's switch is right there, one rune at a
// time, and it would be easy to route a paste through it.
//
// The assertion is that a paste reaches the tree WHOLE and the keymap's
// resolution loop does not run per character. Ten thousand characters must cost
// one trip through the tree, not ten thousand through the keymap.
func TestTheKeymapNeverSeesAPaste(t *testing.T) {
	app, box, _ := newApplication(t)
	before := len(app.log)
	paste := strings.Repeat("q", 10_000)
	app.step(termmosaic.Event{Kind: termmosaic.EventPaste, Text: paste})

	if got := app.log[before]; got != "bypass" {
		t.Errorf("a paste took the %q branch, want %q; Resize, Paste, Focus and Compose never enter a command layer (ADR 0009 §2 step 1)", got, "bypass")
	}
	// The widget's rune case saw it as ONE event, not ten thousand runes: its
	// switch is on ev.Rune, and a paste carries Text, so nothing per-rune ran.
	for _, h := range box.handled {
		if h == "close" {
			t.Error("the widget's per-rune case ran over a paste; the paste must reach the tree whole, once (ADR 0005 §4)")
		}
	}
	// And no command ran.
	if *app.counts.quits != 0 {
		t.Errorf("app.quit ran %d times over a paste of 'q' characters, want 0", *app.counts.quits)
	}
}

// TestResizeIsNotACommand is §2.2's rule 1, and it is here because a
// mixed-mechanism application is where someone would be tempted to bind
// "resize" to a command for symmetry with everything else.
//
// A resize is a fact about the world, not an intent, and ADR 0007 §5 already
// gives it its place in the loop.
func TestResizeIsNotACommand(t *testing.T) {
	app, _, _ := newApplication(t)
	before := len(app.log)
	app.step(termmosaic.ResizeEvent(120, 40))
	if got := app.log[before]; got != "resize" {
		t.Errorf("a resize took the %q branch, want %q; there is no resize command and there will not be one", got, "resize")
	}
}

// TestAFocusBindingOutranksTheGlobalOne is the scope chain doing its job in the
// mixed setting, and it is the case a real application hits the moment it gives
// a widget its own keys: the widget's own binding for `x` must beat the
// application's global `x` while that widget has focus, and must NOT once
// something else does.
//
// This is also the answer to the risk the ADR records: a global binding
// shadowing a widget's key is a sharp edge, but the remedy is not a weaker
// global binding — it is a focus binding, which is one Attach and one
// Commandable away and needs no Widget change.
func TestAFocusBindingOutranksTheGlobalOne(t *testing.T) {
	// The widget keeps its own switch (it embeds searchBox) AND publishes one
	// binding. This is the mixed-mechanism shape with the fix applied, and the
	// fix is the whole participation surface: one optional interface, one
	// method, and no change to Widget.
	pub := &xPublisher{searchBox: &searchBox{bounds: termmosaic.Rect{X: 0, Y: 0, W: 20, H: 3}}}
	elsewhere := &searchBox{bounds: termmosaic.Rect{X: 0, Y: 0, W: 20, H: 3}}
	root := &keyLogger{bounds: termmosaic.Rect{X: 0, Y: 0, W: 80, H: 24}}

	globalRuns, boxRuns := 0, 0
	km := keymap.New()
	km.Register(
		keymap.Command{ID: "app.pick", Desc: "the application's own answer", Group: "app",
			Run: func(keymap.Ctx) bool { globalRuns++; return true }},
		keymap.Command{ID: "box.pick", Desc: "the widget's own answer", Group: "box",
			Run: func(keymap.Ctx) bool { boxRuns++; return true }},
	)
	x, err := keymap.ParseChord("x")
	if err != nil {
		t.Fatal(err)
	}
	km.Bind(keymap.Binding{Chord: x, ID: "app.pick", Scope: keymap.ScopeGlobal})
	pub.chord, pub.id = x, "box.pick"
	km.Attach(pub, root)
	km.Seal()

	app := &application{km: km, focus: pub, root: root}

	// Focused on the publisher: its own answer. The global binding for the same
	// chord is live too and is outranked.
	app.step(termmosaic.KeyEvent('x', 0))
	if got := app.last(); got != "keymap:box.pick" {
		t.Errorf("x produced %q, want %q; the focused widget's own binding is the most specific thing that can want the key", got, "keymap:box.pick")
	}

	// Focused elsewhere: the application's answer, and the widget's binding is
	// inert because its owner is not focused. This is the shadowing in the
	// other direction, and it is a feature — the same registry answers two
	// contexts differently without either answer being wrong.
	app.focus = elsewhere
	app.step(termmosaic.KeyEvent('x', 0))
	if got := app.last(); got != "keymap:app.pick" {
		t.Errorf("x produced %q once focus moved away, want %q; a focus binding is live only while its owner has focus", got, "keymap:app.pick")
	}

	if globalRuns != 1 {
		t.Errorf("app.pick ran %d times, want 1", globalRuns)
	}
	if boxRuns != 1 {
		t.Errorf("box.pick ran %d times, want 1", boxRuns)
	}

	// And both answers are discoverable, which is the other half of the fix: a
	// binding that overrides a global one has to be visible in help, or the
	// override is as silent as the shadowing was.
	entries := km.Describe(keymap.ScopeFocus)
	var ids []string
	for _, e := range entries {
		ids = append(ids, string(e.ID))
	}
	joined := strings.Join(ids, ",")
	if !strings.Contains(joined, "box.pick") || !strings.Contains(joined, "app.pick") {
		t.Errorf("Describe(ScopeFocus) = %v, want both answers listed; help cannot drift from the bindings, and the override is a binding", entries)
	}
}

// xPublisher is a widget that keeps its own switch AND publishes one binding,
// which is the mixed-mechanism shape with the fix applied.
type xPublisher struct {
	*searchBox
	chord keymap.Chord
	id    keymap.CommandID
}

func (p *xPublisher) Bindings() []keymap.Binding {
	return []keymap.Binding{{Chord: p.chord, ID: p.id, Desc: "the widget's own answer"}}
}

// WHAT THIS FILE ESTABLISHES, and what it does not.
//
// It retires ADR 0009's risk 1 — the two-mechanism overlap — as an OBSERVED
// property rather than an argued one. The keymap wins a key it consumes; a
// widget's own case for that key becomes unreachable while the binding exists
// and comes back the moment it is unbound; a key nothing binds still reaches the
// tree, so a text field keeps working; a paste and a resize never enter the
// layer at all. All five are asserted above, through the §2.2 loop, and the
// first two are the ones the ADR was most worried about.
//
// It does NOT retire the risk, and the distinction matters. The risk as written
// is "the two-mechanism overlap has never been observed UNDER LOAD", and this
// observes it in a test with a deterministic key sequence, not under load. What
// the test removes is the "no application in the tree mixes both" half: there is
// now a worked example, and the shadowing is a known, reproducible consequence
// rather than a prediction.
//
// It also shows the fix costs one optional interface and one method. The remedy
// for a shadowed widget key is a focus binding, not a Widget change and not a
// weaker global binding — TestAFocusBindingOutranksTheGlobalOne demonstrates
// both halves of that, and shows the two answers coexist in help.
//
// What remains genuinely open is the one the ADR names as its fix: Warnings
// reporting a chord that is both bound and handled by an attached widget. That
// is not implementable here, and TestTheShadowedKeyIsSilent says why rather than
// leaving it as an omission — the Registry is told a widget's bounds and, at
// most, its published chords; what a widget's Handle does with a key is
// invisible to it by construction.
