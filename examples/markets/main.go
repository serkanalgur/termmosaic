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
	"flag"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
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
func run(offline bool, interval time.Duration) error {
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
	// leaves a terminal in raw mode has broken the user's shell.
	defer t.Close()

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

	// The fetch goroutine. It owns every network call in this program, which is the
	// statement the whole design rests on: nothing on the frame path can block,
	// because nothing on the frame path can reach the network.
	go func() {
		defer stop()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			refresh()
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
	src2 := input.NewSource(t, withProbe(input.DefaultConfig(), term.NewSink(os.Stdout)))
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
					switch {
					case isQuitKey(ev):
						stop()
						return
					case ev.Rune == 'r':
						// A manual refetch, so the failure and staleness paths are
						// reachable without waiting out the interval.
						go refresh()
					}
				case termmosaic.EventResize:
					// ADR 0007 §6's rule, and it needs no code beyond these two
					// calls: resize for EVERY event so Renderer.Size stays
					// truthful, then recompute the root's rect, then let the pacer
					// decide when to paint. Resize already forces a full repaint, so
					// the InvalidateAll that would sit here is redundant.
					r.Resize(ev.Size.W, ev.Size.H)
					board.SetBounds(buffer.Rect{W: ev.Size.W, H: ev.Size.H})
					r.Post(func() {
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
	// Closing the Source restores the terminal modes it enabled and stops its
	// goroutines. It does not close the terminal, which the deferred t.Close()
	// above owns.
	_ = src2.Close()
	_ = r.LeaveAltScreen()
	_ = t.LeaveRawMode()
	return err
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

// isQuitKey reports whether ev should end the program, in the decoder's vocabulary
// rather than as raw bytes.
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
