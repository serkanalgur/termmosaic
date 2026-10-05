package keymap

import (
	"testing"

	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
)

// rec is a minimal Widget for the allocation table: it has bounds, so a mouse
// press can hit-test against it, and it records what it was asked.
type rec struct {
	name    string
	bounds  termmosaic.Rect
	handled []string
}

func (w *rec) Bounds() termmosaic.Rect { return w.bounds }
func (w *rec) Draw(*buffer.Buffer)     {}
func (w *rec) Invalidate()             {}
func (w *rec) Handle(ev termmosaic.Event) bool {
	w.handled = append(w.handled, ev.Kind.String())
	return false
}

// clickable is a rec that also answers a click, for the mouse rows. It is a
// value rather than a pointer to the rec so that a test can name the hit point
// and the command inline.
type clickable struct {
	rec
	// cmd is the command a press at the hit point means. A press anywhere else
	// on the widget means nothing — a click on padding is a click on the widget
	// and not a command, which is the row the allocation table calls out.
	cmd  CommandID
	hitX int
	hitY int
}

func (w *clickable) Command(x, y int) (CommandID, bool) {
	if x == w.hitX && y == w.hitY {
		return w.cmd, true
	}
	return "", false
}

// allocSink keeps the optimiser from deleting the work under test.
var allocSink any

// TestDispatchIsZeroAllocation is the measurable claim of ADR 0009 §2, pinned
// as a test in the shape of input's TestDecodeIsZeroAllocation rather than
// asserted in a comment.
//
// The input path is on the latency path, and ADR 0005 already set the bar at
// zero allocations per decoded key. This package sits immediately downstream of
// it and inherits the bar: a TUI that allocates per keystroke has its input
// latency governed by the garbage collector rather than by how much work the
// dispatch does.
//
// EVERY ROW of §2's allocation table is a subtest, because each row is a
// different mechanism and a single number would not say which one regressed:
//
//	key, no match                            — the miss path, and the only row a
//	                                           naive implementation gets right
//	key, match, Enabled == nil               — the run path
//	key, match, Enabled != nil               — a func field read from the
//	                                           registry, which is NOT a boxing
//	                                           allocation
//	key, match, Run declines, next candidate  — the candidate walk over a
//	                                           pre-built sealed slice
//	key, non-key event                       — step 1's unconditional return, and
//	                                           ADR 0005 §4's paste rule enforced
//	                                           one layer up
//	mouse, press, no Clickable at the point  — a Bounds().Contains walk
//
// Describe, Chords and Warnings are deliberately NOT measured here. They
// allocate, by design and by documentation, and calling them from Dispatch
// would be a bug; the test that would catch it is this one, and it does not
// call them.
func TestDispatchIsZeroAllocation(t *testing.T) {
	// always and never are top-level so the compiler cannot devirtualise the
	// call and hide an allocation inside the closure.
	var (
		always  = true
		_       = always
		counter int
		runs    int
	)

	cases := []struct {
		name  string
		setup func(r *Registry, w *rec)
		ev    termmosaic.Event
		// after runs the dispatch once so the test can assert the path was
		// actually taken and the zero is not a zero because nothing happened.
		after func(t *testing.T, r *Registry, w *rec)
	}{
		{
			name: "key, no match",
			setup: func(r *Registry, w *rec) {
				r.Register(yes("app.quit", "quit", &runs))
				c, err := ParseChord("q")
				if err != nil {
					t.Fatal(err)
				}
				r.Bind(Binding{Chord: c, ID: "app.quit"})
			},
			ev: termmosaic.KeyEvent('z', 0),
			after: func(t *testing.T, r *Registry, w *rec) {
				if _, ok := r.Dispatch(termmosaic.KeyEvent('z', 0), w); ok {
					t.Error("an unbound chord must not be consumed")
				}
			},
		},
		{
			name: "key, match, Enabled == nil",
			setup: func(r *Registry, w *rec) {
				r.Register(yes("app.quit", "quit", &runs))
				c, err := ParseChord("q")
				if err != nil {
					t.Fatal(err)
				}
				r.Bind(Binding{Chord: c, ID: "app.quit"})
			},
			ev: termmosaic.KeyEvent('q', 0),
			after: func(t *testing.T, r *Registry, w *rec) {
				id, ok := r.Dispatch(termmosaic.KeyEvent('q', 0), w)
				if !ok || id != "app.quit" {
					t.Errorf("Dispatch(q) = %q, %v, want app.quit, true", id, ok)
				}
			},
		},
		{
			name: "key, match, Enabled != nil",
			setup: func(r *Registry, w *rec) {
				r.Register(Command{
					ID:      "app.save",
					Desc:    "save",
					Enabled: func() bool { return always },
					Run:     func(Ctx) bool { runs++; return true },
				})
				// "Ctrl+s", not "Ctrl+S": ADR 0009 §3 folds Shift into the
				// rune, so Shift is never a bit on a printable chord and
				// Ctrl+Shift+S is the separate chord spelled "Ctrl+S".
				if err := r.BindString("Ctrl+s", "app.save", ScopeGlobal, nil); err != nil {
					t.Fatal(err)
				}
			},
			ev: termmosaic.KeyEvent('s', termmosaic.ModCtrl),
			after: func(t *testing.T, r *Registry, w *rec) {
				id, ok := r.Dispatch(termmosaic.KeyEvent('s', termmosaic.ModCtrl), w)
				if !ok || id != "app.save" {
					t.Errorf("Dispatch(Ctrl+s) = %q, %v, want app.save, true", id, ok)
				}
			},
		},
		{
			name: "key, match, Enabled false",
			setup: func(r *Registry, w *rec) {
				r.Register(Command{
					ID:      "app.save",
					Desc:    "save",
					Enabled: func() bool { return false },
					Run:     func(Ctx) bool { runs++; return true },
				})
				if err := r.BindString("Ctrl+S", "app.save", ScopeGlobal, nil); err != nil {
					t.Fatal(err)
				}
			},
			ev: termmosaic.KeyEvent('s', termmosaic.ModCtrl),
			after: func(t *testing.T, r *Registry, w *rec) {
				if _, ok := r.Dispatch(termmosaic.KeyEvent('s', termmosaic.ModCtrl), w); ok {
					t.Error("an unavailable command must not be consumed")
				}
			},
		},
		{
			name: "key, match, Run declines, next candidate runs",
			setup: func(r *Registry, w *rec) {
				// Two commands on the same chord in the same rank. The first
				// declines, the second runs. This is the only fallthrough
				// mechanism in TermMosaic and it is a Go return value.
				r.Register(
					Command{ID: "app.first", Desc: "first", Run: func(Ctx) bool { return false }},
					Command{ID: "app.second", Desc: "second", Run: func(Ctx) bool { runs++; return true }},
				)
				c, err := ParseChord("x")
				if err != nil {
					t.Fatal(err)
				}
				r.Bind(
					Binding{Chord: c, ID: "app.first"},
					Binding{Chord: c, ID: "app.second"},
				)
			},
			ev: termmosaic.KeyEvent('x', 0),
			after: func(t *testing.T, r *Registry, w *rec) {
				id, ok := r.Dispatch(termmosaic.KeyEvent('x', 0), w)
				if !ok || id != "app.second" {
					t.Errorf("Dispatch(x) = %q, %v, want app.second, true (the first candidate declined)", id, ok)
				}
			},
		},
		{
			name: "key, match, Run declines and nothing else is bound",
			setup: func(r *Registry, w *rec) {
				r.Register(declines("app.only", "only", &counter))
				if err := r.BindString("x", "app.only", ScopeGlobal, nil); err != nil {
					t.Fatal(err)
				}
			},
			ev: termmosaic.KeyEvent('x', 0),
			after: func(t *testing.T, r *Registry, w *rec) {
				if _, ok := r.Dispatch(termmosaic.KeyEvent('x', 0), w); ok {
					t.Error("a declined Run must leave the event unconsumed so it reaches the tree")
				}
			},
		},
		{
			name: "paste, never dispatched",
			setup: func(r *Registry, w *rec) {
				r.Register(yes("app.quit", "quit", &runs))
				if err := r.BindString("q", "app.quit", ScopeGlobal, nil); err != nil {
					t.Fatal(err)
				}
			},
			ev: termmosaic.Event{Kind: termmosaic.EventPaste, Text: "qqqqqq"},
			after: func(t *testing.T, r *Registry, w *rec) {
				if _, ok := r.Dispatch(termmosaic.Event{Kind: termmosaic.EventPaste, Text: "qqqqqq"}, w); ok {
					t.Error("a paste must never be consumed by the command layer (ADR 0005 §4)")
				}
			},
		},
		{
			name: "resize, never dispatched",
			setup: func(r *Registry, w *rec) {
				r.Register(yes("app.quit", "quit", &runs))
				if err := r.BindString("q", "app.quit", ScopeGlobal, nil); err != nil {
					t.Fatal(err)
				}
			},
			ev: termmosaic.ResizeEvent(80, 24),
			after: func(t *testing.T, r *Registry, w *rec) {
				if _, ok := r.Dispatch(termmosaic.ResizeEvent(80, 24), w); ok {
					t.Error("a resize is a fact about the world, not an intent, and must never be a command")
				}
			},
		},
		{
			name: "mouse, press, no Clickable widget at the point",
			setup: func(r *Registry, w *rec) {
				r.Register(yes("app.quit", "quit", &runs))
				if err := r.BindString("q", "app.quit", ScopeGlobal, nil); err != nil {
					t.Fatal(err)
				}
				// A Clickable widget is attached and covers the point, but its
				// Command answers false there: a click on padding is a click on
				// the widget and not a command. The walk is a Bounds().Contains
				// per attached widget and one interface call.
				btn := &clickable{rec: rec{name: "button", bounds: termmosaic.Rect{X: 0, Y: 0, W: 10, H: 3}}, cmd: "app.quit", hitX: 99, hitY: 99}
				r.Attach(btn, w)
			},
			ev: termmosaic.Event{Kind: termmosaic.EventMouse, Mouse: termmosaic.Mouse{X: 5, Y: 1, Button: termmosaic.MouseLeft, Action: termmosaic.MousePress}},
			after: func(t *testing.T, r *Registry, w *rec) {
				ev := termmosaic.Event{Kind: termmosaic.EventMouse, Mouse: termmosaic.Mouse{X: 5, Y: 1, Button: termmosaic.MouseLeft, Action: termmosaic.MousePress}}
				if _, ok := r.Dispatch(ev, w); ok {
					t.Error("a press on a widget's padding must not be consumed")
				}
			},
		},
		{
			name: "mouse, press, Clickable names a command",
			setup: func(r *Registry, w *rec) {
				r.Register(yes("app.open", "open", &runs))
				btn := &clickable{rec: rec{name: "button", bounds: termmosaic.Rect{X: 0, Y: 0, W: 10, H: 3}}, cmd: "app.open", hitX: 5, hitY: 1}
				r.Attach(btn, w)
			},
			ev: termmosaic.Event{Kind: termmosaic.EventMouse, Mouse: termmosaic.Mouse{X: 5, Y: 1, Button: termmosaic.MouseLeft, Action: termmosaic.MousePress}},
			after: func(t *testing.T, r *Registry, w *rec) {
				ev := termmosaic.Event{Kind: termmosaic.EventMouse, Mouse: termmosaic.Mouse{X: 5, Y: 1, Button: termmosaic.MouseLeft, Action: termmosaic.MousePress}}
				id, ok := r.Dispatch(ev, w)
				if !ok || id != "app.open" {
					t.Errorf("Dispatch(press) = %q, %v, want app.open, true", id, ok)
				}
			},
		},
		{
			name: "mouse, drag, not a command",
			setup: func(r *Registry, w *rec) {
				r.Register(yes("app.open", "open", &runs))
				btn := &clickable{rec: rec{name: "button", bounds: termmosaic.Rect{X: 0, Y: 0, W: 10, H: 3}}, cmd: "app.open", hitX: 5, hitY: 1}
				r.Attach(btn, w)
			},
			ev: termmosaic.Event{Kind: termmosaic.EventMouse, Mouse: termmosaic.Mouse{X: 5, Y: 1, Button: termmosaic.MouseLeft, Action: termmosaic.MouseDrag}},
			after: func(t *testing.T, r *Registry, w *rec) {
				ev := termmosaic.Event{Kind: termmosaic.EventMouse, Mouse: termmosaic.Mouse{X: 5, Y: 1, Button: termmosaic.MouseLeft, Action: termmosaic.MouseDrag}}
				if _, ok := r.Dispatch(ev, w); ok {
					t.Error("a drag is a three-phase gesture, not an instantaneous intent (ADR 0009 §6)")
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := New()
			w := &rec{name: "focus", bounds: termmosaic.Rect{X: 0, Y: 0, W: 20, H: 5}}
			tc.setup(r, w)
			r.Seal()

			// Prove the path is real before measuring it: a zero-allocation
			// assertion over a dispatch that never dispatches is a green test
			// that proves nothing.
			before := runs
			tc.after(t, r, w)

			ev := tc.ev
			for i := 0; i < 8; i++ {
				r.Dispatch(ev, w)
			}
			var sink CommandID
			allocs := testing.AllocsPerRun(200, func() {
				id, _ := r.Dispatch(ev, w)
				sink = id
			})
			allocSink = sink
			if allocs != 0 {
				t.Errorf("Dispatch allocated %v times per run, want 0 (ADR 0009 §2)", allocs)
			}
			_ = before
		})
	}
}

// BenchmarkDispatchKey measures the hot path so the zero-allocation claim has a
// number next to it, in the spirit of input's BenchmarkDecodeKey.
func BenchmarkDispatchKey(b *testing.B) {
	r := New()
	r.Register(Command{ID: "app.quit", Desc: "quit", Run: func(Ctx) bool { return true }})
	c, _ := ParseChord("q")
	r.Bind(Binding{Chord: c, ID: "app.quit"})
	r.Seal()
	w := &rec{bounds: termmosaic.Rect{X: 0, Y: 0, W: 20, H: 5}}
	ev := termmosaic.KeyEvent('q', 0)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		r.Dispatch(ev, w)
	}
}

// BenchmarkDispatchMiss measures the path an unbound key takes, which is the
// one every keystroke nobody bound a command for goes through.
func BenchmarkDispatchMiss(b *testing.B) {
	r := New()
	r.Register(Command{ID: "app.quit", Desc: "quit", Run: func(Ctx) bool { return true }})
	c, _ := ParseChord("q")
	r.Bind(Binding{Chord: c, ID: "app.quit"})
	r.Seal()
	w := &rec{bounds: termmosaic.Rect{X: 0, Y: 0, W: 20, H: 5}}
	ev := termmosaic.KeyEvent('z', 0)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		r.Dispatch(ev, w)
	}
}

// yes is a command that always runs.
func yes(id CommandID, desc string, runs *int) Command {
	return Command{ID: id, Desc: desc, Run: func(Ctx) bool { *runs++; return true }}
}

// declines is a command whose Run always declines.
func declines(id CommandID, desc string, n *int) Command {
	return Command{ID: id, Desc: desc, Run: func(Ctx) bool { *n++; return false }}
}
