// Command search is TermMosaic's form example: a query field, a table of results
// from Wikipedia's search API, and a detail pane fed by the article summary
// endpoint.
//
// It exists because every other example in this repository proves that a widget
// draws correctly and that a screen routes its keys. This one proves the thing
// neither of those can: that the widget catalog and the keymap hold together when
// something ACTUALLY holds focus and there is a network in the loop.
//
// # What is new here, and why it is the hard case
//
// hello and markets compose screens from widgets that hold no keyboard focus.
// Their "focus ring" is an integer the screen owns. Here form.TextInput and
// data.Table both implement termmosaic.Focusable and both have key contracts of
// their own, so:
//
//   - A widget owns its keys. The field consumes printable runes, backspace,
//     delete, left, right, home and end; the table consumes up, down, page up and
//     down, home, end, left, right, enter and space. Not one of them is bound at
//     ScopeScreen here, because a screen-scoped binding outranks the focused widget
//     and would make the field untypable and the table unnavigable.
//   - The keys the widgets DECLINE are the screen's. TextInput declines Up, Down,
//     Enter and Tab on purpose, "because the key belongs to the form". The form
//     here owns exactly those four and nothing else, and each one's SCOPE is argued
//     at its own declaration in search.go.
//
// # The event loop
//
// It is ADR 0009 §2's four-step order, verbatim: key and mouse events go to
// km.Dispatch first and fall through to the tree's Handle only when nothing
// consumed them, a resize is not a command and goes straight to the resize path,
// and a paste goes to the tree whole.
//
// Everything that mutates widget state is POSTED rather than run on the input
// goroutine, because the input goroutine and the render goroutine are different
// goroutines and Handle writes the focus index and the table's selection, which
// Draw reads. Post is the framework's own answer to that (ADR 0003), and it is
// the pattern an application is expected to copy.
//
// # Nothing on the frame path can block
//
// The network runs on its own goroutine and hands its results over through
// render.Renderer.Post. A search that took two seconds would freeze a TUI that
// fetched on its frame path, and a search box that freezes cannot be cancelled —
// and this example's most interesting property is precisely that Enter starts a
// request, the screen says "searching…" on the next frame, and the reader keeps
// typing.
//
// There is deliberately NO heartbeat here, and markets has one. A heartbeat exists
// to keep a clock on screen honest; this screen's footer carries the time of the
// last RESPONSE, which only changes when a response arrives — so the republish that
// would keep it ticking is the same republish the response already triggers.
//
// # Offline
//
// --offline runs the whole screen on a captured response with no network at all.
// That is what the golden tests render, so CI never depends on the internet and a
// golden failure is never a network failure; it is also what a person on a plane
// runs, and it exercises exactly the same decode, layout and rendering code as the
// live path rather than a parallel one. See sample.go for the capture's provenance.
//
// # Keys
//
//	q, esc, ctrl-c    quit
//	enter             run the search, or open the selected article
//	tab / shift-tab   next / previous pane
//	up / down         leave the query field for the results, and back
//	ctrl+home / end   first / last pane
//
// None of that is written twice: it is a keymap.Registry, and the hint line is
// rendered from DescribeGrouped.

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"sync"
	"sync/atomic"

	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/input"
	"github.com/serkanalgur/termmosaic/render"
	"github.com/serkanalgur/termmosaic/term"
)

// store is the one piece of shared mutable state, and it exists because the fetch
// goroutine and the render goroutine are different goroutines.
//
// It is a mutex rather than a channel for a reason worth stating: a channel would
// work for handing data over, but the screen needs to be able to ask "what is the
// current response?" from the render goroutine at any moment without blocking,
// because Draw cannot block. A mutex-guarded read of a pointer is that. The
// snapshot itself is immutable once stored, so the lock is held for a pointer copy
// and nothing more, and no widget state is ever touched under it.
//
// The race test drives this with a concurrent writer and a concurrent reader under
// -race, which is the only way to be sure the claim holds.
type store struct {
	mu   sync.RWMutex
	snap *snapshot
	gen  uint64
}

// publish stores a new snapshot and returns its generation.
//
// The generation is what lets the screen tell "new data" from "the same data
// again", which matters because SetFrame skips a republish of a generation it has
// already applied.
func (s *store) publish(snap *snapshot) uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gen++
	snap.gen = s.gen
	s.snap = snap
	return s.gen
}

// load returns the current snapshot and its generation.
//
// A nil snapshot is a legitimate answer: it is what a caller sees before the first
// search completes, and the screen renders that as "type a query", not as an error.
func (s *store) load() (*snapshot, uint64) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snap, s.gen
}

// main parses the flags and runs, reporting any failure on stderr rather than
// leaving a terminal in raw mode.
func main() {
	offline := flag.Bool("offline", false, "run on the bundled capture with no network")
	flag.Parse()

	if err := run(*offline); err != nil {
		fmt.Fprintln(os.Stderr, "search:", err)
		os.Exit(1)
	}
}

// run opens the terminal, wires the renderer, the input source and the fetch
// goroutines, and runs until the user quits.
func run(offline bool) (err error) {
	// Fall back to the headless path when stdout is not a terminal, so the example
	// is runnable in CI and under redirection instead of erroring out.
	if !isTerminal(os.Stdout) {
		fmt.Println("search: stdout is not a terminal, so there is nothing to draw onto.")
		fmt.Println("search: run it in a real terminal, or run the golden tests:")
		fmt.Println("           go test ./examples/search/")
		return nil
	}

	t, err := term.Open(os.Stdin, os.Stdout, os.Getenv)
	if err != nil {
		return err
	}
	// Restoring the terminal is not optional on any exit path: a program that
	// leaves a terminal in raw mode has broken the user's shell. So a failure to do
	// it is itself worth reporting, which is why the deferred close folds its error
	// into the named return rather than dropping it.
	defer func() { err = errors.Join(err, t.Close()) }()

	w, h := t.Size()
	caps := t.Capabilities()
	setRung(caps.Unicode)

	r := render.New(term.NewSink(os.Stdout), render.Config{
		Width:   w,
		Height:  h,
		Caps:    caps,
		NoColor: render.NoColorFromEnv(os.Getenv),
	})

	var src source
	if offline {
		src = newOfflineSource()
	} else {
		src = newLiveSource()
	}

	st := &store{}
	// detailGen numbers article fetches. It is separate from the store's generation
	// because a detail fetch has its own lifecycle — it is triggered by moving the
	// selection, not by running a search — and the detail pane's cache key is
	// (selected row, detail generation), so the two counters must not be one.
	var detailGen atomic.Uint64

	board := newSearch(screenBounds(w, h))
	r.SetRoot(board)

	// --offline primes the screen with the capture, so it opens on a populated
	// result set rather than an empty one waiting for a keystroke. The live path
	// has no equivalent: there is nothing to show before the reader has asked for
	// something, and inventing a placeholder result set would be a search box that
	// lies about what it found.
	if primed, ok := src.(*offlineSource); ok {
		snap := primed.primeSnapshot()
		st.publish(snap)
		// The field is filled too, so the screen shows a COMPLETE search rather than
		// results with an empty box above them — which is also what makes pressing
		// Enter immediately a no-op rather than a surprise.
		board.query.SetText(sampleQuery)
		if snap, _ := st.load(); snap != nil {
			board.SetFrame(buildFrame(snap))
		}
	}

	if err := t.EnterRawMode(); err != nil {
		return err
	}
	if err := t.EnterAltScreen(); err != nil {
		_ = t.LeaveRawMode()
		return err
	}
	if err := r.Reset(); err != nil {
		return err
	}

	// quit is closed by whichever goroutine decides the program is finished. Both
	// the input loop and a signal handler may want to stop the program, and closing
	// a channel twice panics, so the close goes through sync.Once.
	quit := make(chan struct{})
	var quitOnce sync.Once
	stop := func() { quitOnce.Do(func() { close(quit) }) }
	board.quitFn = stop

	// fetchCtx is cancelled when the program ends, so an in-flight request cannot
	// outlive the terminal it might try to write to.
	fetchCtx, cancelFetch := context.WithCancel(context.Background())
	defer cancelFetch()

	// doSearch performs one search.
	//
	// The three steps are separate and the order is the whole of it. The request
	// runs on ITS OWN goroutine and blocks for up to fetchTimeout — that is what
	// putting it here is for, because the caller is the render goroutine, where
	// blocking is the one thing that must never happen (ADR 0002). Publishing takes
	// the store's lock for a pointer copy. The Post queues a callback that runs on
	// the RENDER goroutine before the next frame, and that is where widget state is
	// mutated: Table.SetRows and SetPending are not safe to call while a Draw may be
	// reading them, and Post is the framework's own answer to exactly that (ADR
	// 0003).
	board.onSubmit = func(query string) {
		go func() {
			res, err := src.Search(fetchCtx, query)
			st.publish(&snapshot{res: res, err: err, at: wallClock(), source: src.label()})
			r.Post(func() {
				board.SetSearching(false)
				if snap, _ := st.load(); snap != nil {
					board.SetFrame(buildFrame(snap))
				}
				r.Invalidate(board.Bounds())
			})
		}()
	}

	// openArticle performs one article fetch. Same three steps, and the extra one
	// that matters here: the pane is told a request is IN FLIGHT before it starts,
	// so moving the selection shows "loading the article…" rather than leaving the
	// previous article's extract on screen under the new article's title.
	board.onOpen = func(title string) {
		board.SetPending(title)
		r.Invalidate(board.Bounds())
		go func() {
			sum, err := src.Summary(fetchCtx, title)
			d := &detailSnapshot{title: title, sum: sum, err: err, gen: detailGen.Add(1)}
			r.Post(func() {
				board.SetPending("")
				board.SetDetail(d)
				r.Invalidate(board.Bounds())
			})
		}()
	}

	// Input. The Source is built AFTER raw mode, because the kitty keyboard query
	// it sends is answered on the input stream and is meaningless without raw mode.
	// WriteProbe is wired to the example's own sink rather than to a method on
	// Terminal, which is the point of ADR 0005 not widening ADR 0001's interface.
	src2 := input.NewSource(t, searchInputConfig(term.NewSink(os.Stdout)))

	// Keys and resizes arrive on ONE ordered channel, so a resize can never be
	// delivered between the bytes of a half-read escape sequence, and no input
	// event is ever dropped to make room for a resize.
	go func() {
		for {
			select {
			case <-quit:
				return
			case <-fetchCtx.Done():
				return
			case ev, ok := <-src2.Events():
				if !ok {
					// The terminal reached EOF.
					stop()
					return
				}
				switch ev.Kind {
				case termmosaic.EventKey, termmosaic.EventMouse:
					// ADR 0009 §2's order, all of it. The keymap is asked first; a key
					// it does not claim falls through to the tree. A mouse event is
					// offered to the registry too, because a click is a command when
					// the widget under it says so — nothing here implements
					// Clickable, so a click falls through and the field and the table
					// hit-test it themselves.
					//
					// The whole thing is POSTED rather than run here, because a
					// command's Run writes the focus index and the table's selection
					// and Draw reads both: running it on the input goroutine would be a
					// data race against the frame path. Dispatch is called here and not
					// by the framework because the framework owns decoding and the
					// application owns when its own state may be touched.
					key := ev
					r.Post(func() {
						// The focus argument is the FOCUSED WIDGET, not the root. A
						// ScopeFocus binding's liveness is decided against it, and this
						// example binds none — see search.go — but passing the root here
						// would silently kill the next one anyone adds, so the argument
						// is the widget the ring says has focus.
						if _, consumed := board.km.Dispatch(key, board.FocusWidget()); consumed {
							// A consumed key changed the screen's state, or ended the
							// program, so the frame that described the old state is stale.
							// InvalidateAll rather than a sub-rectangle: the marker moves
							// between panes and the hint line's TEXT changes.
							r.InvalidateAll()
							placeCursor(r, board)
							return
						}
						// Unconsumed: the tree, exactly as ADR 0003's frame pipeline
						// says. This is where the field's printable runes and the table's
						// arrows arrive, and it is the half of the design that exists
						// only because the widgets decline the other half.
						if board.Handle(key) {
							r.InvalidateAll()
							placeCursor(r, board)
						}
					})
				case termmosaic.EventResize:
					// A resize is NOT a command — ADR 0009 §2 rule 1 — and ADR 0007 §5's
					// order is unchanged: recompute the root's rectangle, then resize the
					// renderer. Renderer.Resize never draws, so the whole interval
					// between the event and the next Render is available to recompute
					// bounds, and Resize already forces a full repaint, which is why the
					// InvalidateAll that would sit here is redundant.
					size := ev.Size
					board.SetBounds(screenBounds(size.W, size.H))
					r.Resize(size.W, size.H)
					r.Post(func() {
						board.Handle(termmosaic.ResizeEvent(size.W, size.H))
						placeCursor(r, board)
					})
				}
			}
		}
	}()

	pacer := render.NewPacer(r)
	frameErr := make(chan error, 1)
	go func() { frameErr <- pacer.Run(quit) }()

	err = <-frameErr
	// The single teardown, on the single exit path, in the reverse of the order the
	// modes were turned on: mouse capture off, then the alternate screen, then raw
	// mode. Disabling mouse reporting after leaving raw mode would emit the sequence
	// into a terminal that is line-buffering again, which is how a disable sequence
	// ends up echoed into the user's shell as visible text.
	//
	// Closing the Source is what disables mouse capture and restores bracketed
	// paste; it does not close the terminal, which the deferred t.Close() owns.
	_ = src2.Close()
	_ = r.LeaveAltScreen()
	_ = t.LeaveRawMode()
	return err
}

// screenBounds returns the screen's rectangle: the whole terminal.
//
// The other two examples inset their root by a margin. This one does not, because
// its content is a full-height table and a detail pane, and an inset would cost a
// row and a column of the one thing on the screen whose whole job is to be scrolled
// through. There is no clamp either, which is the ADR 0007 anti-pattern both of
// those examples were rewritten to remove: the rect IS the screen, so it grows and
// shrinks with it.
func screenBounds(w, h int) buffer.Rect {
	return buffer.Rect{W: w, H: h}.Clip(w, h)
}

// placeCursor puts the terminal's caret where the query field's caret is.
//
// This is the one thing this example does that the other two cannot: they hide the
// cursor because nothing they draw can be typed into, and this one has a focusable
// text field whose caret is a real cursor. A field with an invisible caret looks
// broken, and one with a caret parked in the last cell drawn looks like a bug in
// the renderer.
//
// The position is APPROXIMATE and honestly so: the field scrolls its own text when
// the query outgrows it, and the widget does not expose the scroll offset, so the
// cell is computed from the caret's RUNE index and clamped to the field's width. For
// a query shorter than the field — which is every query at any width this screen
// draws at above 46 cells — that is exact.
//
// The cursor is hidden whenever the field does not have focus, because a caret
// floating next to a results table is worse than no caret at all.
func placeCursor(r *render.Renderer, s *search) {
	if s.FocusIndex() != paneQuery {
		r.SetCursor(render.Cursor{Valid: true, Visible: false})
		return
	}
	f := s.query.Bounds()
	if f.Empty() {
		r.SetCursor(render.Cursor{Valid: true, Visible: false})
		return
	}
	x := f.X + s.query.Cursor()
	if x >= f.Right() {
		x = f.Right() - 1
	}
	if x < f.X {
		x = f.X
	}
	r.SetCursor(render.Cursor{X: x, Y: f.Y, Visible: true, Valid: true})
}

// withProbe returns cfg with its WriteProbe wired to sink.
//
// The sink is what the renderer already writes frames through, so the enable
// sequences and the kitty query go out on the same path as the UI rather than
// through a second writer that could interleave with it.
func withProbe(cfg input.Config, sink termmosaic.Sink) input.Config {
	cfg.WriteProbe = func(p []byte) error {
		if _, err := sink.Write(p); err != nil {
			return err
		}
		return sink.Flush()
	}
	return cfg
}

// searchInputConfig is this example's input configuration: the recommended
// defaults, the probe wired to the frame sink, and MOUSE CAPTURE ON.
//
// Mouse capture is the one thing this example takes from markets. It needs the
// wheel and clicks — a reader scrolls a nine-row table with the wheel and clicks
// the field to put the caret where they want it — and ADR 0005 leaves capture off
// by default because enabling it takes text selection, middle-click paste and
// scrollback copying away from the user's shell with no terminal-side indication.
// The cost is paid back on the way out through the one teardown.
//
// MouseClick rather than MouseDrag or MouseAll: this screen needs the wheel and
// clicks, and reporting MOTION would multiply the event volume for a screen with
// nothing to do with a drag.
func searchInputConfig(sink termmosaic.Sink) input.Config {
	cfg := withProbe(input.DefaultConfig(), sink)
	cfg.MouseMode = input.MouseClick
	return cfg
}

func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}
