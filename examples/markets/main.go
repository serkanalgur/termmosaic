// Command markets is TermMosaic's live-data demonstration: one screen fed by two
// public APIs over the network, composed entirely from the widget catalog.
//
// It exists because every other example in this repository proves that a widget
// draws correctly. This one proves something a widget test cannot: that an
// application can put a real I/O source behind the catalog and keep every promise
// the framework makes while doing it. Specifically:
//
//   - Draw never blocks. The network runs on its own goroutine and hands its
//     results over through render.Renderer.Post, so the render goroutine only ever
//     reads state a fetch has already published. A dashboard that fetched on its
//     frame path would freeze for up to the fetch timeout on every cycle.
//   - Draw allocates nothing. Every number is formatted off the frame path (see
//     view.go), every size-derived value is cached against the rectangle it came
//     from (see dashboard.go's adapt), and the tests assert both under -race.
//   - It degrades visibly. A failed fetch says which source failed and keeps
//     whatever the other one returned; a fetch that has never succeeded replaces
//     the bands with a panel that names the failure. A dashboard that hangs on a
//     dead network, or that renders an absent number as zero, is worse than one
//     that says "no data".
//   - It is responsive. Three width bands, each of which moves panels rather than
//     merely narrowing them, and a one-line diagnostic below the declared minimum.
//
// # Offline
//
// --offline runs the whole screen on a bundled capture of real responses with no
// network at all. That is what the golden tests render, so CI never depends on the
// internet and a golden failure is never a network failure; it is also what a
// person on a plane runs, and it exercises exactly the same layout and rendering
// code as the live path rather than a parallel one.
//
// # Keys
//
// q quits. r refetches now, which is how the staleness and failure states are
// reachable without waiting thirty seconds.

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/input"
	"github.com/serkanalgur/termmosaic/render"
	"github.com/serkanalgur/termmosaic/term"
)

// The store is the one piece of shared mutable state, and it exists because the
// fetch goroutine and the render goroutine are different goroutines.
//
// It is a mutex rather than a channel for a reason worth stating: a channel would
// work for handing data over, but the dashboard needs to be able to ask "what is
// the current snapshot?" from the render goroutine at any moment without blocking,
// because Draw cannot block. A mutex-guarded read of a pointer is that. The
// snapshot itself is immutable once stored, so the lock is held for a pointer copy
// and nothing more, and no widget state is ever touched under it.
//
// The race test in markets_test.go drives this with a concurrent writer and a
// concurrent reader under -race, which is the only way to be sure the claim holds.
type store struct {
	mu   sync.RWMutex
	snap *snapshot
	gen  uint64
}

// publish stores a new snapshot and returns its generation.
//
// The generation is what lets the board tell "new data" from "the same data again",
// which matters because a fetch that fails leaves the previous snapshot in place
// and only the error changes.
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
// fetch completes, and the board renders that as "loading", not as an error.
func (s *store) load() (*snapshot, uint64) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snap, s.gen
}

func main() {
	offline := flag.Bool("offline", false, "run on bundled sample data with no network")
	interval := flag.Duration("interval", refreshInterval, "how often to refetch")
	flag.Parse()

	if err := run(*offline, *interval); err != nil {
		fmt.Fprintln(os.Stderr, "markets:", err)
		os.Exit(1)
	}
}

// run opens the terminal, wires the renderer, the input source and the fetch
// goroutine, and runs until the user quits.
func run(offline bool, interval time.Duration) (err error) {
	// Refuse early rather than half-way through setup: the alt screen is already
	// active by this point, and returning from it after an error is one more thing
	// to get right on the way out.
	if !isTerminal(os.Stdout) {
		fmt.Println("markets: stdout is not a terminal, so there is nothing to draw onto.")
		fmt.Println("markets: run it in a real terminal, or run the golden tests:")
		fmt.Println("           go test ./examples/markets/")
		return nil
	}

	t, err := term.Open(os.Stdin, os.Stdout, os.Getenv)
	if err != nil {
		return err
	}
	// Restoring the terminal is not optional on any exit path: a program that
	// leaves a terminal in raw mode has broken the user's shell. So a failure to
	// do it is itself worth reporting, which is why the deferred close folds its
	// error into the named return rather than dropping it.
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
	r.SetCursor(render.Cursor{Valid: true, Visible: false})

	var src source
	if offline {
		src = newOfflineSource()
	} else {
		src = newLiveSource()
	}

	st := &store{}
	board := newDashboard(w, h)
	r.SetRoot(board)

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

	quit := make(chan struct{})
	var quitOnce sync.Once
	stop := func() { quitOnce.Do(func() { close(quit) }) }

	// fetchCtx is cancelled when the program ends, so an in-flight request cannot
	// outlive the terminal it might try to write to.
	fetchCtx, cancelFetch := context.WithCancel(context.Background())
	defer cancelFetch()

	// refresh performs one cycle and publishes it.
	//
	// The three steps are separate and the order matters. The cycle runs on this
	// goroutine and blocks for up to fetchTimeout — that is the whole point of
	// putting it here. Publishing takes the store's lock for a pointer copy. The
	// Post queues a callback that runs on the RENDER goroutine before the next
	// frame, and that is where widget state is mutated: Sparkline.SetValues and
	// Table.SetRows are not safe to call while a Draw may be reading them, and Post
	// is the framework's own answer to exactly that (ADR 0003).
	refresh := func() {
		m, err := src.Fetch(fetchCtx)
		at := wallClock()
		snap := &snapshot{m: m, err: err, at: at, source: src.label()}
		if m != nil && m.anyData() {
			snap.ok = true
		}
		if snap.err != nil && m != nil {
			snap.ok = m.anyData()
		}
		st.publish(snap)
		r.Post(func() {
			s, _ := st.load()
			if s == nil {
				return
			}
			board.SetFrame(buildFrame(s))
			r.Invalidate(board.Bounds())
		})
	}

	// 'r' asks for one cycle without waiting out the interval, which is how the
	// staleness and failure states are reachable without thirty seconds of patience.
	//
	// It runs on its OWN goroutine because a cycle blocks for up to fetchTimeout and
	// this is called from Handle, which the input loop runs on the render goroutine —
	// where blocking is the one thing that must never happen (ADR 0002). The refresh
	// itself ends in a Post, so the widgets are still only ever mutated on the render
	// goroutine.
	board.OnRefresh = func() { go refresh() }

	// The fetch goroutine. It owns every network call in this program, which is the
	// statement the whole design rests on: nothing on the frame path can block,
	// because nothing on the frame path can reach the network.
	//
	// The PAUSE check is here rather than inside refresh because pausing is about
	// the SCHEDULE, not about the cycle: a paused dashboard must still republish the
	// heartbeat and must still honour a manual 'r', and folding the check into the
	// cycle would suppress both.
	go func() {
		defer stop()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			if !board.Paused() {
				refresh()
			}
			select {
			case <-quit:
				return
			case <-fetchCtx.Done():
				return
			case <-ticker.C:
			}
		}
	}()

	// The heartbeat. The status line shows the fetch time to the second, so
	// something has to invalidate the screen for that number to stay honest. It is
	// a Post rather than an InvalidateAll so that it republishes the CURRENT frame
	// rather than leaving the widgets holding whatever they last held.
	beat := time.NewTicker(time.Second)
	defer beat.Stop()
	go func() {
		for {
			select {
			case <-quit:
				return
			case <-fetchCtx.Done():
				return
			case <-beat.C:
				r.Post(func() {
					s, gen := st.load()
					if s == nil || gen != board.FrameGen() {
						return
					}
					board.SetFrame(buildFrame(s))
					r.Invalidate(board.Bounds())
				})
			}
		}
	}()

	// Input. One goroutine over the ordered event stream, so a resize can never be
	// delivered between the bytes of a half-read escape sequence.
	//
	// MOUSE CAPTURE IS ON, and this is the one example in the repository that turns
	// it on. ADR 0005 leaves capture off by default because enabling it takes text
	// selection, middle-click paste and scrollback copying away from the user's
	// shell with no terminal-side indication — a framework that did that by default
	// would break something users rely on. This screen needs the wheel and clicks, so
	// it opts in explicitly, and the cost is paid back on the way out: every exit
	// path below goes through the one teardown, which closes the Source — whose
	// Close writes the disable sequences — before leaving the alternate screen and
	// raw mode.
	//
	// The order matters and is the reverse of the order the modes were turned on:
	// mouse capture off, then the alternate screen, then raw mode. Disabling mouse
	// reporting after leaving raw mode would emit it into a terminal that is
	// line-buffering again, which is how a disable sequence ends up echoed into the
	// user's shell as visible text.
	src2 := input.NewSource(t, marketsInputConfig(term.NewSink(os.Stdout)))
	go func() {
		for {
			select {
			case <-quit:
				return
			case ev, ok := <-src2.Events():
				if !ok {
					stop()
					return
				}
				switch ev.Kind {
				case termmosaic.EventKey:
					if isQuitKey(ev) {
						stop()
						return
					}
					// Everything else goes to the board, ON THE RENDER GOROUTINE.
					// Handle writes the focus index, the pause flag, the pair and the
					// overlay flag, all of which Draw reads, so calling it here would
					// be a data race against the frame path. Post is the framework's
					// own handoff for exactly this (ADR 0003), and routing through it
					// is the pattern the other two examples follow.
					key := ev
					r.Post(func() {
						if board.Handle(key) {
							r.InvalidateAll()
						}
					})
				case termmosaic.EventMouse:
					// The same handoff, and for the same reason: a click mutates the
					// table's selection and the focus ring.
					click := ev
					r.Post(func() {
						if board.Handle(click) {
							r.InvalidateAll()
						}
					})
				case termmosaic.EventResize:
					// ADR 0007 §6's rule, and it needs no code beyond these calls:
					// resize for EVERY event so Renderer.Size stays truthful, then
					// recompute the root's rect, then let the pacer decide when to
					// paint. Resize already forces a full repaint, so the InvalidateAll
					// that would sit here is redundant.
					//
					// The SetBounds goes through Handle — on the render goroutine —
					// for the same reason as the key: Bounds is read by the frame that
					// is drawing right now.
					size := ev.Size
					r.Resize(size.W, size.H)
					r.Post(func() {
						board.Handle(termmosaic.ResizeEvent(size.W, size.H))
						r.Invalidate(board.Bounds())
					})
				}
			}
		}
	}()

	pacer := render.NewPacer(r)
	frameErr := make(chan error, 1)
	go func() { frameErr <- pacer.Run(quit) }()

	err = <-frameErr
	// The single teardown, on the single exit path. Anything that returns early
	// above — an error from EnterRawMode, EnterAltScreen or Reset — has NOT built a
	// Source yet, so it has not enabled mouse capture and has nothing to undo; the
	// deferred t.Close() above owns raw mode and the alternate screen for those
	// paths, which is why there is no second teardown to forget to call.
	teardown(r, t, src2)
	return err
}

// teardown restores everything the program turned on, in the reverse of the order
// it turned them on.
//
// It is a function rather than four lines at the end of run because the ORDER is
// the whole of it and a reader — or a future edit that adds an early return — cannot
// check four scattered statements. It is what makes the mouse-capture guarantee
// checkable: there is exactly one place where capture is disabled, and every exit
// path reaches it.
//
// Closing the Source is what disables mouse capture, restores bracketed paste, and
// pops the kitty keyboard flags: input.Source owns the modes IT enabled and its
// Close undoes them (ADR 0005 §6). It does not close the terminal, which the
// deferred t.Close() owns.
func teardown(r *render.Renderer, t termmosaic.Terminal, src *input.Source) {
	_ = src.Close()
	_ = r.LeaveAltScreen()
	_ = t.LeaveRawMode()
}

// FrameGen reports the generation of the frame the widgets currently hold, which
// is what the heartbeat compares against so it never republishes a frame the
// board has already applied.
func (d *dashboard) FrameGen() uint64 {
	if d.cur == nil {
		return 0
	}
	return d.curGen
}

// withProbe returns cfg with its WriteProbe wired to sink, so the enable
// sequences and the keyboard query go out on the same path as the UI rather than
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

// marketsInputConfig is this example's input configuration: the recommended
// defaults, the probe wired to the frame sink, and MOUSE CAPTURE ON.
//
// It is a named function rather than an inline call because the capture decision is
// the most consequential line in this file — it takes the user's text selection,
// middle-click paste and scrollback away for as long as this program runs — and a
// named function is something a test can name and assert on and a reader can find by
// searching for "mouse".
//
// MouseClick rather than MouseDrag or MouseAll: this screen needs the wheel and
// clicks, and reporting MOTION would multiply the event volume for a dashboard with
// nothing to do with a drag. That difference is exactly what the three modes are
// for, and choosing the cheapest one that does the job is the whole of the decision.
func marketsInputConfig(sink termmosaic.Sink) input.Config {
	cfg := withProbe(input.DefaultConfig(), sink)
	cfg.MouseMode = input.MouseClick
	return cfg
}

// isQuitKey reports whether ev should end the program, in the decoder's vocabulary
// rather than as raw bytes.
//
// Ctrl-C is included here and NOT in the board's own key handling, so the quit
// decision lives in exactly one place for every exit path: a key the board also
// claims (it consumes ctrl-c so a form never sees it) does not also have to decide
// whether to exit.
func isQuitKey(ev termmosaic.Event) bool {
	switch {
	case ev.Key == termmosaic.KeyEscape:
		return true
	case ev.Rune == 'q' || ev.Rune == 'Q':
		return ev.Mod == 0
	case ev.Rune == 'c':
		return ev.Mod == termmosaic.ModCtrl
	}
	return false
}

func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}
