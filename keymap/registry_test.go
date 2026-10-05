package keymap

import (
	"strings"
	"testing"

	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
)

// log records the order commands ran in, so a precedence test asserts which one
// ran rather than only that something did.
type log struct{ order []CommandID }

func (l *log) cmd(id CommandID, desc string) Command {
	return Command{ID: id, Desc: desc, Run: func(Ctx) bool { l.order = append(l.order, id); return true }}
}

// plain is a Widget that is neither Commandable nor Clickable, which is the
// norm for the whole v0.2 catalog.
type plain struct {
	bounds termmosaic.Rect
	seen   int
}

func (w *plain) Bounds() termmosaic.Rect      { return w.bounds }
func (w *plain) Draw(*buffer.Buffer)          {}
func (w *plain) Invalidate()                  {}
func (w *plain) Handle(termmosaic.Event) bool { w.seen++; return false }

// publishing is a Widget that implements Commandable, which is how a widget
// contributes its own keys.
type publishing struct {
	plain
	bindings []Binding
	// bindingCalls counts Bindings invocations, so a test can assert it is never
	// called on the dispatch path.
	bindingCalls int
}

func (w *publishing) Bindings() []Binding {
	w.bindingCalls++
	return w.bindings
}

// TestPrecedenceFollowsTheFourRanks is §2.1's table as a test. Each row is a
// subtest because each row is a different claim, and a precedence bug that
// only shows up between two adjacent rows would be invisible in a single
// combined assertion.
func TestPrecedenceFollowsTheFourRanks(t *testing.T) {
	focus := &plain{bounds: termmosaic.Rect{W: 10, H: 3}}
	other := &plain{bounds: termmosaic.Rect{W: 10, H: 3}}
	screen := &plain{bounds: termmosaic.Rect{W: 80, H: 24}}

	// build wires one binding per scope plus a user override, all on one chord,
	// and returns the registry and the log.
	build := func(l *log) *Registry {
		r := New()
		r.Register(
			l.cmd("global.cmd", "global"),
			l.cmd("screen.cmd", "screen"),
			l.cmd("focus.cmd", "focus"),
			l.cmd("override.cmd", "override"),
		)
		c, err := ParseChord("x")
		if err != nil {
			t.Fatal(err)
		}
		r.Bind(
			Binding{Chord: c, ID: "global.cmd", Scope: ScopeGlobal},
			Binding{Chord: c, ID: "screen.cmd", Scope: ScopeScreen, Owner: screen},
			Binding{Chord: c, ID: "focus.cmd", Scope: ScopeFocus, Owner: focus},
		)
		return r
	}

	t.Run("focus beats screen beats global", func(t *testing.T) {
		l := &log{}
		r := build(l)
		r.SetScreen(screen)
		r.Seal()
		id, ok := r.Dispatch(termmosaic.KeyEvent('x', 0), focus)
		if !ok || id != "focus.cmd" {
			t.Errorf("Dispatch = %q, %v, want focus.cmd (rank 2 beats rank 3 beats rank 4)", id, ok)
		}
	})

	t.Run("screen beats global when nothing is focused", func(t *testing.T) {
		l := &log{}
		r := build(l)
		r.SetScreen(screen)
		r.Seal()
		id, ok := r.Dispatch(termmosaic.KeyEvent('x', 0), other)
		if !ok || id != "screen.cmd" {
			t.Errorf("Dispatch = %q, %v, want screen.cmd (the focus binding's owner is not the focused widget)", id, ok)
		}
	})

	t.Run("global when the focus owner is not focused and no screen is set", func(t *testing.T) {
		l := &log{}
		r := build(l)
		r.Seal()
		// `other` is neither the focus binding's owner nor the screen, so the
		// only live binding on this chord is the global one.
		id, ok := r.Dispatch(termmosaic.KeyEvent('x', 0), other)
		if !ok || id != "global.cmd" {
			t.Errorf("Dispatch = %q, %v, want global.cmd (neither the focus nor the screen binding is live)", id, ok)
		}
	})

	t.Run("a user override in the same scope outranks the default there", func(t *testing.T) {
		l := &log{}
		r := build(l)
		r.SetScreen(screen)
		r.Seal()
		c, err := ParseChord("x")
		if err != nil {
			t.Fatal(err)
		}
		// Bound AFTER Seal, which is what makes it an override rather than a
		// default, and global rather than focus, so it is in the same rank as
		// the existing global binding. It replaces that one outright, and both
		// are below the live focus binding.
		r.Bind(Binding{Chord: c, ID: "override.cmd", Scope: ScopeGlobal})
		id, ok := r.Dispatch(termmosaic.KeyEvent('x', 0), focus)
		if !ok || id != "focus.cmd" {
			t.Errorf("Dispatch = %q, %v, want focus.cmd; specificity outranks the override flag (ADR 0009 §2.1)", id, ok)
		}
		// With the screen popped, the focus binding is dead and the global one
		// is the only live candidate — and it is the override, because the
		// override replaced the default in its own scope rather than losing to
		// it.
		r.SetScreen(nil)
		id, ok = r.Dispatch(termmosaic.KeyEvent('x', 0), other)
		if !ok || id != "override.cmd" {
			t.Errorf("Dispatch = %q, %v, want override.cmd; the override replaced the default global binding", id, ok)
		}
		if len(l.order) != 2 {
			t.Errorf("ran %v, want exactly one command per dispatch", l.order)
		}
	})

	t.Run("a user override in a weaker scope does NOT outrank a stronger default", func(t *testing.T) {
		// This is the case §2.1 argues at length: a user binding Esc globally
		// must not steal a dialog's Esc. So the override here is GLOBAL and the
		// default is FOCUS, and the focus default wins.
		l := &log{}
		r := New()
		r.Register(l.cmd("focus.cmd", "focus"), l.cmd("override.cmd", "override"))
		c, _ := ParseChord("Esc")
		r.Bind(Binding{Chord: c, ID: "focus.cmd", Scope: ScopeFocus, Owner: focus})
		r.Seal()
		r.Bind(Binding{Chord: c, ID: "override.cmd", Scope: ScopeGlobal})
		id, ok := r.Dispatch(termmosaic.SpecialKeyEvent(termmosaic.KeyEscape, 0), focus)
		if !ok || id != "focus.cmd" {
			t.Errorf("Dispatch = %q, %v, want focus.cmd; a dialog's own Esc cannot be stolen by a user's global Esc (ADR 0009 §2.1)", id, ok)
		}
		// And with the dialog gone, the user's binding applies.
		id, ok = r.Dispatch(termmosaic.SpecialKeyEvent(termmosaic.KeyEscape, 0), other)
		if !ok || id != "override.cmd" {
			t.Errorf("Dispatch = %q, %v, want override.cmd once the dialog is no longer focused", id, ok)
		}
	})

	t.Run("two global bindings on one chord collapse to the later one", func(t *testing.T) {
		// ADR 0009 §2.1 says ties are "resolved by registration order, and a
		// later registration replaces an earlier one". The second half is the
		// operative one and it is what makes an override an override: two
		// bindings for the same chord in the same scope with the same owner are
		// the same binding, declared twice. The first half has nothing to
		// resolve in practice, because a rank is defined by a scope and only
		// one owner per scope can be live at a time — so a genuine tie within
		// one rank cannot be constructed, and the ordering is a determinism
		// guarantee rather than a user-visible choice.
		l := &log{}
		r := New()
		r.Register(l.cmd("first", "first"), l.cmd("second", "second"))
		c, _ := ParseChord("x")
		r.Bind(Binding{Chord: c, ID: "first"}, Binding{Chord: c, ID: "second"})
		r.Seal()
		if id, _ := r.Dispatch(termmosaic.KeyEvent('x', 0), focus); id != "second" {
			t.Errorf("Dispatch = %q, want second; the later registration replaces the earlier one", id)
		}
		if got := len(r.bindings); got != 1 {
			t.Errorf("the registry holds %d bindings, want 1", got)
		}
	})

	t.Run("a later Bind of the same chord, scope and owner replaces", func(t *testing.T) {
		l := &log{}
		r := New()
		r.Register(l.cmd("old", "old"), l.cmd("new", "new"))
		c, _ := ParseChord("x")
		r.Bind(Binding{Chord: c, ID: "old"})
		r.Seal()
		r.Bind(Binding{Chord: c, ID: "new"})
		if id, _ := r.Dispatch(termmosaic.KeyEvent('x', 0), focus); id != "new" {
			t.Errorf("Dispatch = %q, want new; replacing is what makes an override an override, not an error", id)
		}
		if got := len(r.bindings); got != 1 {
			t.Errorf("the registry holds %d bindings, want 1; a replaced binding must not linger as a second row", got)
		}
	})
}

// TestEnabledAndRunAreTheOnlyTwoWaysToSayNo is §2 step 4's two exits, stated
// as one mechanism. There is no fallthrough flag and no preventDefault flag, and
// this is the test that would notice if someone added one.
func TestEnabledAndRunAreTheOnlyTwoWaysToSayNo(t *testing.T) {
	focus := &plain{}
	t.Run("Enabled false continues to the next candidate", func(t *testing.T) {
		ran := false
		r := New()
		r.Register(
			Command{ID: "unavailable", Desc: "u", Enabled: func() bool { return false }, Run: func(Ctx) bool { return true }},
			Command{ID: "available", Desc: "a", Run: func(Ctx) bool { ran = true; return true }},
		)
		c, _ := ParseChord("x")
		r.Bind(Binding{Chord: c, ID: "unavailable"}, Binding{Chord: c, ID: "available"})
		r.Seal()
		if id, ok := r.Dispatch(termmosaic.KeyEvent('x', 0), focus); !ok || id != "available" {
			t.Errorf("Dispatch = %q, %v, want available", id, ok)
		}
		if !ran {
			t.Error("the available candidate did not run")
		}
	})

	t.Run("Run false continues to the next candidate", func(t *testing.T) {
		ran := false
		r := New()
		r.Register(
			Command{ID: "declines", Desc: "d", Run: func(Ctx) bool { return false }},
			Command{ID: "accepts", Desc: "a", Run: func(Ctx) bool { ran = true; return true }},
		)
		c, _ := ParseChord("x")
		r.Bind(Binding{Chord: c, ID: "declines"}, Binding{Chord: c, ID: "accepts"})
		r.Seal()
		if id, ok := r.Dispatch(termmosaic.KeyEvent('x', 0), focus); !ok || id != "accepts" {
			t.Errorf("Dispatch = %q, %v, want accepts", id, ok)
		}
		if !ran {
			t.Error("the accepting candidate did not run")
		}
	})

	t.Run("neither means the event reaches the tree", func(t *testing.T) {
		r := New()
		r.Register(Command{ID: "declines", Desc: "d", Run: func(Ctx) bool { return false }})
		c, _ := ParseChord("x")
		r.Bind(Binding{Chord: c, ID: "declines"})
		r.Seal()
		if _, ok := r.Dispatch(termmosaic.KeyEvent('x', 0), focus); ok {
			t.Error("a declined Run must leave the event unconsumed, or TextInput would stop receiving runes nothing declared a command for")
		}
	})

	t.Run("Enabled is called at most once per candidate", func(t *testing.T) {
		calls := 0
		r := New()
		r.Register(Command{ID: "c", Desc: "c", Enabled: func() bool { calls++; return true }, Run: func(Ctx) bool { return true }})
		ch, _ := ParseChord("x")
		r.Bind(Binding{Chord: ch, ID: "c"})
		r.Seal()
		r.Dispatch(termmosaic.KeyEvent('x', 0), focus)
		if calls != 1 {
			t.Errorf("Enabled was called %d times for one dispatch, want 1; it is on the hot path", calls)
		}
	})
}

// TestUnbindRemovesRatherThanFallsBack is §5's rule, and it is the one users
// notice: pressing "unbind this key" must leave the key dead, not resurrect the
// default it replaced.
func TestUnbindRemovesRatherThanFallsBack(t *testing.T) {
	focus := &plain{}
	r := New()
	r.Register(Command{ID: "c", Desc: "c", Run: func(Ctx) bool { return true }})
	c, _ := ParseChord("x")
	r.Bind(Binding{Chord: c, ID: "c"})
	r.Seal()
	r.Unbind(c, ScopeGlobal, nil)
	if _, ok := r.Dispatch(termmosaic.KeyEvent('x', 0), focus); ok {
		t.Error("the chord is still bound after Unbind")
	}
	// And an override is removed too, not just the default.
	r.Bind(Binding{Chord: c, ID: "c"})
	r.Bind(Binding{Chord: c, ID: "c"}) // the second is the override
	r.Unbind(c, ScopeGlobal, nil)
	if _, ok := r.Dispatch(termmosaic.KeyEvent('x', 0), focus); ok {
		t.Error("Unbind left a binding behind; it says it removes every binding for the chord in scope, including user overrides")
	}
}

// TestBindStringSurfacesAParseError is the property that makes a rebinding UI
// possible: a key the application cannot read must be an error at the point the
// user typed it, not a binding that silently never fires.
func TestBindStringSurfacesAParseError(t *testing.T) {
	r := New()
	r.Register(Command{ID: "c", Desc: "c", Run: func(Ctx) bool { return true }})
	if err := r.BindString("Ctrl+", "c", ScopeGlobal, nil); err == nil {
		t.Error(`BindString("Ctrl+") reported no error`)
	}
	if err := r.BindString("nope", "c", ScopeGlobal, nil); err == nil {
		t.Error(`BindString("nope") reported no error`)
	}
	if err := r.BindString("Ctrl+k", "c", ScopeGlobal, nil); err != nil {
		t.Errorf(`BindString("Ctrl+k"): %v`, err)
	}
	if got := len(r.bindings); got != 1 {
		t.Errorf("the registry holds %d bindings, want 1; a failed parse must not bind anything", got)
	}
}

// TestSetScreenIsAFieldWrite is the claim that a dialog push and pop costs
// nothing, and it is the reason scope liveness is resolved at dispatch time
// rather than baked into the sealed table.
func TestSetScreenIsAFieldWrite(t *testing.T) {
	focus := &plain{}
	dialog := &plain{}
	r := New()
	l := &log{}
	r.Register(l.cmd("dialog.cancel", "cancel"), l.cmd("global.quit", "quit"))
	c, _ := ParseChord("Esc")
	r.Bind(
		Binding{Chord: c, ID: "dialog.cancel", Scope: ScopeScreen, Owner: dialog},
		Binding{Chord: c, ID: "global.quit", Scope: ScopeGlobal},
	)
	r.Seal()
	// Before the dialog: the global binding answers Esc.
	if id, _ := r.Dispatch(termmosaic.SpecialKeyEvent(termmosaic.KeyEscape, 0), focus); id != "global.quit" {
		t.Errorf("Dispatch = %q, want global.quit before the dialog is pushed", id)
	}
	r.SetScreen(dialog)
	if id, _ := r.Dispatch(termmosaic.SpecialKeyEvent(termmosaic.KeyEscape, 0), focus); id != "dialog.cancel" {
		t.Errorf("Dispatch = %q, want dialog.cancel while the dialog is up", id)
	}
	r.SetScreen(nil)
	if id, _ := r.Dispatch(termmosaic.SpecialKeyEvent(termmosaic.KeyEscape, 0), focus); id != "global.quit" {
		t.Errorf("Dispatch = %q, want global.quit after the dialog is popped", id)
	}
}

// TestCommandablePublishesWithoutDispatching is §7's contract: a widget
// publishes bindings and keeps handling its own keys. The two mechanisms are
// independent by design, and this is the test that says so.
func TestCommandablePublishesWithoutDispatching(t *testing.T) {
	widget := &publishing{plain: plain{bounds: termmosaic.Rect{W: 10, H: 3}}}
	widget.bindings = []Binding{
		{Chord: Chord{Key: termmosaic.KeyUp}, ID: "list.up", Desc: "previous row"},
		{Chord: Chord{Key: termmosaic.KeyDown}, ID: "list.down", Desc: "next row"},
	}
	l := &log{}
	r := New()
	r.Register(l.cmd("list.up", "up"), l.cmd("list.down", "down"))
	r.Attach(widget)
	if !r.IsAttached(widget) {
		t.Fatal("IsAttached is false right after Attach")
	}

	// The Registry filled in Scope and Owner, which an implementation does not
	// have to set and which Bind ignores if it does.
	for _, b := range r.bindings {
		if b.Scope != ScopeFocus {
			t.Errorf("binding %v has scope %v, want focus", b.Chord, b.Scope)
		}
		if b.Owner != termmosaic.Widget(widget) {
			t.Error("Attach did not set the owner to the widget")
		}
	}

	// A key the widget published is resolved by the keymap...
	if id, ok := r.Dispatch(termmosaic.SpecialKeyEvent(termmosaic.KeyUp, 0), widget); !ok || id != "list.up" {
		t.Errorf("Dispatch(Up) = %q, %v, want list.up", id, ok)
	}
	// ...and a key it did not is not, so the widget's own switch still runs.
	if _, ok := r.Dispatch(termmosaic.SpecialKeyEvent(termmosaic.KeyPageDown, 0), widget); ok {
		t.Error("an unpublished key must reach the widget's own Handle")
	}
	// Bindings is pulled at Attach and never on the dispatch path: a widget
	// that counted its calls would see exactly one, from the Attach above, and
	// none per keystroke. This is the property that keeps Commandable off the
	// hot path by construction rather than by convention.
	before := widget.bindingCalls
	if id, ok := r.Dispatch(termmosaic.SpecialKeyEvent(termmosaic.KeyDown, 0), widget); !ok || id != "list.down" {
		t.Errorf("Dispatch(Down) = %q, %v, want list.down", id, ok)
	}
	if got := widget.bindingCalls - before; got != 0 {
		t.Errorf("Bindings was called %d times by Dispatch, want 0; it is pulled at Attach and Seal, never on the dispatch path", got)
	}
}

// TestAttachIsIdempotent is the property that makes "call Attach after any
// structural change" safe advice rather than a warning: a second Attach for the
// same widget replaces its rows rather than duplicating them, so help does not
// grow a row every time the tree is rebuilt.
func TestAttachIsIdempotent(t *testing.T) {
	widget := &publishing{plain: plain{bounds: termmosaic.Rect{W: 10, H: 3}}}
	widget.bindings = []Binding{{Chord: Chord{Key: termmosaic.KeyUp}, ID: "list.up"}}
	r := New()
	r.Register(Command{ID: "list.up", Desc: "up", Run: func(Ctx) bool { return true }})
	r.Attach(widget)
	first := len(r.bindings)
	r.Attach(widget)
	if got := len(r.bindings); got != first {
		t.Errorf("after two Attaches the registry holds %d bindings, want %d; a re-attach is a re-declaration", got, first)
	}
	if got := len(r.attached); got != 1 {
		t.Errorf("the registry tracks %d instances, want 1", got)
	}
}

// TestIsAttachedDetectsARebuiltWidget is the rebuild hazard from §5, and it is
// the only mechanism that catches it: a widget reconstructed each frame has a
// new pointer identity, so its old ScopeFocus bindings silently stop matching
// and the failure mode a user reports is "my key stopped working".
func TestIsAttachedDetectsARebuiltWidget(t *testing.T) {
	first := &publishing{plain: plain{bounds: termmosaic.Rect{W: 10, H: 3}}}
	// The first instance publishes two keys; the rebuilt one publishes only
	// one of them. That asymmetry is what makes the hazard observable: if the
	// stale binding still matched, the dropped key would keep working, and the
	// user would never learn that the widget it belonged to is gone.
	first.bindings = []Binding{
		{Chord: Chord{Key: termmosaic.KeyUp}, ID: "list.up"},
		{Chord: Chord{Key: termmosaic.KeyDelete}, ID: "list.delete"},
	}
	r := New()
	r.Register(
		Command{ID: "list.up", Desc: "up", Run: func(Ctx) bool { return true }},
		Command{ID: "list.delete", Desc: "delete", Run: func(Ctx) bool { return true }},
	)
	r.Attach(first)
	if _, ok := r.Dispatch(termmosaic.SpecialKeyEvent(termmosaic.KeyDelete, 0), first); !ok {
		t.Fatal("the published key did not resolve before the rebuild")
	}

	// The frame loop rebuilds it and re-attaches the new instance, which is the
	// mitigation §7 rule 2 names. The predecessor is no longer current.
	second := &publishing{plain: plain{bounds: termmosaic.Rect{W: 10, H: 3}}}
	second.bindings = []Binding{{Chord: Chord{Key: termmosaic.KeyUp}, ID: "list.up"}}
	r.Attach(second)
	if r.IsAttached(first) {
		t.Error("the rebuilt widget's predecessor is still reported as attached after a re-Attach that did not include it")
	}
	if !r.IsAttached(second) {
		t.Error("IsAttached is false for the widget just attached")
	}

	// The surviving key resolves through the new instance...
	if _, ok := r.Dispatch(termmosaic.SpecialKeyEvent(termmosaic.KeyUp, 0), second); !ok {
		t.Error("after re-Attach the re-published key resolves again")
	}
	// ...and the dropped one is inert, because its owner is a pointer the
	// focused widget is not. Owner is identity, not a name.
	if _, ok := r.Dispatch(termmosaic.SpecialKeyEvent(termmosaic.KeyDelete, 0), second); ok {
		t.Error("a binding orphaned by a rebuild still matched")
	}
	// Warnings is the only mechanism that reports it, which is why the stale
	// binding is kept rather than swept up.
	if !containsWarning(r.Warnings(), "no longer attached") {
		t.Errorf("Warnings did not report the orphaned owner; got %v", r.Warnings())
	}
}

// TestInvokeBypassesEnabledAndDispatch is what a palette row calls, and its
// report is "found", not "succeeded".
func TestInvokeBypassesEnabledAndDispatch(t *testing.T) {
	ran := 0
	var got Ctx
	r := New()
	r.Register(Command{
		ID:      "file.save",
		Desc:    "save",
		Enabled: func() bool { return false },
		Run:     func(c Ctx) bool { ran++; got = c; return true },
	})
	widget := &plain{}
	if !r.Invoke("file.save", termmosaic.Event{}, widget) {
		t.Error("Invoke reported the command was not found, but it is registered")
	}
	if ran != 1 {
		t.Errorf("Run was called %d times, want 1; Invoke bypasses Enabled", ran)
	}
	if !got.Synthesised {
		t.Error("Synthesised is false for an Invoke; a palette activation is not a key press")
	}
	if !got.Chord.IsZero() {
		t.Errorf("Chord = %v, want the zero Chord; there is no key behind a palette row", got.Chord)
	}
	if got.Focus != termmosaic.Widget(widget) {
		t.Error("Focus was not passed through")
	}
	if r.Invoke("no.such.command", termmosaic.Event{}, widget) {
		t.Error("Invoke reported finding a command that is not registered")
	}
}

// TestHasIsTheToolbarPredicate is the cheap question: registered AND available.
func TestHasIsTheToolbarPredicate(t *testing.T) {
	available := true
	r := New()
	r.Register(
		Command{ID: "always", Desc: "a", Run: func(Ctx) bool { return true }},
		Command{ID: "never", Desc: "n", Enabled: func() bool { return available }, Run: func(Ctx) bool { return true }},
	)
	if !r.Has("always") {
		t.Error("Has(always) = false")
	}
	if !r.Has("never") {
		t.Error("Has(never) = false while it is available")
	}
	available = false
	if r.Has("never") {
		t.Error("Has(never) = true while it is unavailable; Has gates a button on every frame")
	}
	if r.Has("absent") {
		t.Error("Has(absent) = true for an unregistered command")
	}
}

// TestRegisterReplacesByID is the one place a duplicate is not an error,
// because a command re-registered after a state change is the same command.
func TestRegisterReplacesByID(t *testing.T) {
	r := New()
	r.Register(Command{ID: "c", Desc: "first", Run: func(Ctx) bool { return true }})
	r.Register(Command{ID: "c", Desc: "second", Run: func(Ctx) bool { return true }})
	cmd, ok := r.Command("c")
	if !ok {
		t.Fatal("Command(c) not found")
	}
	if cmd.Desc != "second" {
		t.Errorf("Desc = %q, want %q; a second Register with the same ID replaces the first", cmd.Desc, "second")
	}
	if _, ok := r.Command("absent"); ok {
		t.Error("Command(absent) reported found")
	}
}

// TestAWidgetNeedsNeitherInterface is the claim that participation is optional,
// asserted rather than described. If a future change made Commandable or
// Clickable mandatory, this stops compiling — which is the point.
func TestAWidgetNeedsNeitherInterface(t *testing.T) {
	var w termmosaic.Widget = &plain{}
	if _, ok := w.(Commandable); ok {
		t.Error("a plain widget unexpectedly implements Commandable")
	}
	if _, ok := w.(Clickable); ok {
		t.Error("a plain widget unexpectedly implements Clickable")
	}
	// And it is still fully usable: Attach accepts it and it contributes
	// nothing, which is not an error.
	r := New()
	r.Attach(w)
	if got := len(r.bindings); got != 0 {
		t.Errorf("attaching a non-participating widget added %d bindings, want 0", got)
	}
}

// TestDispatchWithoutSealStillWorks is a convenience, not a contract: a
// registry used without an explicit Seal pays the rebuild once rather than
// breaking.
func TestDispatchWithoutSealStillWorks(t *testing.T) {
	ran := 0
	r := New()
	r.Register(Command{ID: "c", Desc: "c", Run: func(Ctx) bool { ran++; return true }})
	c, _ := ParseChord("x")
	r.Bind(Binding{Chord: c, ID: "c"})
	if _, ok := r.Dispatch(termmosaic.KeyEvent('x', 0), nil); !ok {
		t.Error("Dispatch failed on a registry that was never sealed")
	}
	if ran != 1 {
		t.Errorf("Run was called %d times, want 1", ran)
	}
}

// TestAttachSkipsNilWidgets is a robustness property, not a nicety: an
// application building a tree conditionally passes a nil interface for a
// widget it decided not to create, and a nil Widget must not become a
// registered zero-width hit target.
func TestAttachSkipsNilWidgets(t *testing.T) {
	r := New()
	r.Attach(nil)
	if len(r.attached) != 0 {
		t.Errorf("Attach(nil) recorded %d widgets, want 0", len(r.attached))
	}
	if r.IsAttached(nil) {
		t.Error("IsAttached(nil) = true after Attach(nil)")
	}
}

// containsWarning reports whether any warning contains sub.
func containsWarning(warnings []string, sub string) bool {
	for _, w := range warnings {
		if strings.Contains(w, sub) {
			return true
		}
	}
	return false
}
