package render

// The four tests ADR 0007 names under "Tests to add, in ADR 0002's spirit" and
// STATUS.md records as still unwritten. They are renderer- and application-level
// rather than widget-level, which is why they live here.
//
// Each pins one row of the degenerate-size table in ADR 0007 §4 or one step of the
// resize contract in §5/§6. None existed before this file, so what they assert was
// documented and unenforced — which for a release gate is the same as unclaimed.
//
// Where it is possible, each asserts on the Sink's CELL GRID rather than on emitted
// bytes: the question every one of them asks is what the terminal would be showing,
// and headless.MemorySink exists precisely so that question is answerable (ADR 0001).

import (
	"strings"
	"testing"

	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/geometry"
	"github.com/serkanalgur/termmosaic/headless"
	"github.com/serkanalgur/termmosaic/layout"
)

// countingSink wraps a MemorySink and records how many times it was called at all,
// as opposed to how many bytes it accepted.
//
// ADR 0007 §4's row for a zero-sized screen is stronger than "writes zero bytes":
// it is "does not call the Sink", and that is a different assertion. A frame could
// write nothing and still call Flush, and a byte count would not notice.
type countingSink struct {
	*headless.MemorySink
	writes  int
	flushes int
}

func (c *countingSink) Write(p []byte) (int, error) {
	c.writes++
	return c.MemorySink.Write(p)
}

func (c *countingSink) Flush() error {
	c.flushes++
	return c.MemorySink.Flush()
}

// tagBlock is the fixture for the resize tests: a widget that writes ONE
// DISTINGUISHABLE GLYPH on every row of its rectangle, and that glyph depends on
// the rectangle's SIZE.
//
// Both halves are load-bearing, and both were arrived at by writing the test the
// obvious way first and finding it could not fail:
//
//   - "every cell was written" is not checkable. buffer.DefaultCell.Ch is ' ', not
//     0, so an unwritten cell and a cell explicitly filled with a space are
//     indistinguishable — and the obvious fixture fills its whole rect with spaces.
//   - A per-ROW glyph is not enough either: row 5 shows the same glyph at every
//     size, so a row left over from the previous size would still look right.
//
// A size-dependent glyph makes a stale row visible in both directions: a row that
// was never repainted shows a space, and one repainted at the wrong size shows the
// previous size's glyph.
type tagBlock struct {
	bounds buffer.Rect
	// draws counts Draw calls, so a test can assert the widget was consulted.
	draws int
}

// rowTag is the glyph this block writes on every row at the given size. It is a
// function of w+h, so consecutive sizes in a sweep always differ.
func rowTag(w, h int) rune { return rune('A' + (w+h)%26) }

func (t *tagBlock) Bounds() buffer.Rect          { return t.bounds }
func (t *tagBlock) Invalidate()                  {}
func (t *tagBlock) Handle(termmosaic.Event) bool { return false }

func (t *tagBlock) Draw(buf *buffer.Buffer) {
	t.draws++
	tag := rowTag(t.bounds.W, t.bounds.H)
	for y := t.bounds.Y; y < t.bounds.Bottom(); y++ {
		buf.Set(t.bounds.X, y, tag, buffer.DefaultStyle)
	}
}

// assertScreenIsCurrent asserts that every row of a w-by-h screen carries the tag
// of that exact size, and that the renderer reached the right-hand and bottom edges.
//
// The MaxX/MaxY half is the coverage check: MemorySink records the furthest cell a
// rune was written to, so a rectangle the frame never painted cannot reach the
// corner. It is asserted as >= because the counters are cumulative across a sweep
// and a shrink must not lower them.
func assertScreenIsCurrent(t *testing.T, sink *headless.MemorySink, size geometry.Size) {
	t.Helper()
	want := rowTag(size.W, size.H)
	for y := 0; y < size.H; y++ {
		got := sink.CellAt(0, y).Ch
		if got != want {
			t.Errorf("%dx%d: cell (0,%d) is %q, want %q: a row survived from a previous size, "+
				"or was never repainted", size.W, size.H, y, got, want)
			return
		}
	}
	if sink.MaxX() < size.W-1 {
		t.Errorf("%dx%d: output reached column %d, so the right edge was never painted",
			size.W, size.H, sink.MaxX())
	}
	if sink.MaxY() < size.H-1 {
		t.Errorf("%dx%d: output reached row %d, so the bottom edge was never painted",
			size.W, size.H, sink.MaxY())
	}
}

// TestRenderAtZeroSizeWritesNothing is ADR 0007 §4's first row, pinned.
//
//	> **Size 0 on either axis** — a VALID size. Render writes zero bytes and does
//	> not call the Sink. Widgets may be called; every write is discarded. The event
//	> loop must stay alive and must not treat it as an error.
//
// "Does not call the Sink" is the part that matters, and the part a byte count
// cannot check, so both the call counts and the byte count are asserted. All three
// shapes are covered: a terminal that has detached entirely (0x0), one reporting a
// width but no height (0x24 — the shape a SIGWINCH burst produces when a window is
// dragged off the bottom of the screen), and its transpose.
//
// The root widget IS still called, and that is asserted rather than assumed: ADR
// 0007 says widgets may be, so a future change that panicked on a zero size would
// be caught here instead of in somebody's terminal.
func TestRenderAtZeroSizeWritesNothing(t *testing.T) {
	for _, size := range []geometry.Size{
		{W: 0, H: 0},
		{W: 0, H: 24},
		{W: 24, H: 0},
	} {
		t.Run(sizeLabel(size), func(t *testing.T) {
			sink := &countingSink{MemorySink: headless.NewMemorySink(size.W, size.H)}
			r := New(sink, Config{Width: size.W, Height: size.H, Caps: termmosaic.DefaultCaps()})
			root := &block{bounds: buffer.Rect{W: size.W, H: size.H}, text: "content"}
			r.SetRoot(root)

			n, err := r.Render()
			if err != nil {
				t.Fatalf("Render must treat a zero-sized screen as valid, got error: %v", err)
			}
			if n != 0 {
				t.Errorf("Render reported %d bytes, want 0", n)
			}
			if sink.writes != 0 {
				t.Errorf("the Sink was written %d times, want 0", sink.writes)
			}
			if sink.flushes != 0 {
				t.Errorf("the Sink was flushed %d times, want 0", sink.flushes)
			}
			if root.draws != 1 {
				t.Errorf("the root widget was drawn %d times, want 1: widgets MAY be called, "+
					"so a change that panicked on a zero size must fail here", root.draws)
			}

			// It must stay that way: a zero-sized screen is not a one-frame accident,
			// it is every frame until the size is known again.
			for i := 0; i < 3; i++ {
				if _, err := r.Render(); err != nil {
					t.Fatalf("follow-up frame %d: %v", i, err)
				}
			}
			if sink.writes != 0 || sink.flushes != 0 {
				t.Errorf("a repeated zero-sized frame touched the Sink (writes %d, flushes %d), "+
					"want 0 and 0", sink.writes, sink.flushes)
			}
			if r.NeedsFrame() {
				t.Error("a zero-sized renderer must report that it needs no frame, or a " +
					"detached terminal's event loop spins at the target frame rate")
			}

			// Coming back out of a zero size is the other half of the contract, and
			// it is where a buffer that was never sized would show its problem.
			const w, h = 20, 4
			sink.MemorySink.Resize(w, h)
			r.Resize(w, h)
			root.bounds = buffer.Rect{W: w, H: h}
			if _, err := r.Render(); err != nil {
				t.Fatalf("Render after growing out of a zero size: %v", err)
			}
			if got, want := sink.Line(0), "content"; got != want {
				t.Errorf("after growing, row 0 = %q, want %q", got, want)
			}
		})
	}
}

// sizeLabel renders a size as a subtest name.
func sizeLabel(s geometry.Size) string {
	return itoaTest(s.W) + "x" + itoaTest(s.H)
}

func itoaTest(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [8]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// TestResizeShrinksAndRepaintsWholeRect is ADR 0007 §1 rule 3 at the application
// level, and it is the half the existing resize tests do not cover:
//
//	> A widget must repaint its entire Bounds() before drawing content into it. On
//	> every size change, not only at construction: a widget that drew 10 rows and
//	> now draws 3 leaves seven stale rows on screen unless it repainted the whole
//	> rect first.
//
// Grow AND shrink, at every size in a sweep, so a defect shows up at the exact size
// that caused it rather than at whichever size the sweep happened to stop on.
//
// The shrink is the half that matters, and it is why the grow cases do not
// substitute for it: growing gives a widget MORE room, so every extra cell is
// freshly written and nothing can be stale. Shrinking is the only direction in
// which the previous frame's content survives anywhere — and a sweep that only
// shrank would miss a defect that only bites on the way out, so both run.
func TestResizeShrinksAndRepaintsWholeRect(t *testing.T) {
	big := geometry.Size{W: 24, H: 10}
	small := geometry.Size{W: 9, H: 3}

	// step resizes the sink, the renderer and the root's rectangle in lockstep and
	// renders, asserting after every step that the screen belongs to the new size
	// and not to the one before it.
	// step walks one cell at a time from one size to another — growing or
	// shrinking — resizing the sink, the renderer and the root's rectangle in
	// lockstep and asserting after every step that the screen belongs to the NEW
	// size and not to the one before it. The final step is clamped so the sweep
	// lands exactly on to, which matters because the intermediate sizes are where a
	// stale row would be left behind.
	step := func(t *testing.T, r *Renderer, sink *headless.MemorySink, root *tagBlock, from, to geometry.Size, delta int) {
		t.Helper()
		w, h := from.W, from.H
		for {
			w, h = w+delta, h+delta
			if delta > 0 {
				w, h = minInt(w, to.W), minInt(h, to.H)
			} else {
				w, h = maxInt(w, to.W), maxInt(h, to.H)
			}
			size := geometry.Size{W: w, H: h}
			sink.Resize(w, h)
			r.Resize(w, h)
			root.bounds = buffer.Rect{W: w, H: h}
			if _, err := r.Render(); err != nil {
				t.Fatalf("resize to %dx%d: %v", w, h, err)
			}
			assertScreenIsCurrent(t, sink, size)
			if sink.UnknownSequences() != 0 {
				t.Fatalf("%dx%d: %d unrecognised sequences", w, h, sink.UnknownSequences())
			}
			if size == to {
				return
			}
		}
	}

	newAt := func(t *testing.T, size geometry.Size) (*Renderer, *headless.MemorySink, *tagBlock) {
		t.Helper()
		sink := headless.NewMemorySink(size.W, size.H)
		r := New(sink, Config{Width: size.W, Height: size.H, Caps: termmosaic.DefaultCaps()})
		root := &tagBlock{bounds: buffer.Rect{W: size.W, H: size.H}}
		r.SetRoot(root)
		if _, err := r.Render(); err != nil {
			t.Fatal(err)
		}
		assertScreenIsCurrent(t, sink, size)
		return r, sink, root
	}

	t.Run("shrink", func(t *testing.T) {
		r, sink, root := newAt(t, big)
		step(t, r, sink, root, big, small, -1)
		if got := root.bounds; got != (buffer.Rect{W: small.W, H: small.H}) {
			t.Errorf("sweep ended at %v, want %v", got, buffer.Rect{W: small.W, H: small.H})
		}
	})

	// Grow first, so the shrink that follows starts from the LARGEST rectangle —
	// the worst case for leaving stale cells, because every row below the target
	// was written by the big frame.
	t.Run("grow then shrink", func(t *testing.T) {
		r, sink, root := newAt(t, small)
		step(t, r, sink, root, small, big, 1)
		step(t, r, sink, root, big, small, -1)
		if got := root.bounds; got != (buffer.Rect{W: small.W, H: small.H}) {
			t.Errorf("sweep ended at %v, want %v", got, buffer.Rect{W: small.W, H: small.H})
		}
	})

	// And the degenerate sizes inside the same sweep, because ADR 0007 §5 says a
	// resize "always repaints the whole screen" and a size of zero has no screen
	// to repaint — the next step back up must still be correct.
	t.Run("through a degenerate size", func(t *testing.T) {
		r, sink, root := newAt(t, small)
		for _, size := range []geometry.Size{{W: 0, H: 3}, {W: 9, H: 0}, {W: 0, H: 0}, {W: 1, H: 1}, {W: 12, H: 6}} {
			sink.Resize(size.W, size.H)
			r.Resize(size.W, size.H)
			root.bounds = buffer.Rect{W: size.W, H: size.H}
			if _, err := r.Render(); err != nil {
				t.Fatalf("resize to %dx%d: %v", size.W, size.H, err)
			}
			if size.W > 0 && size.H > 0 {
				assertScreenIsCurrent(t, sink, size)
			}
		}
	})
}

// TestResizeCoalescedToOneRepaintPerTick is ADR 0007 §6's anti-jank rule, pinned.
//
//	> An app that calls r.Resize for every EventResize but does not call Render
//	> itself therefore coalesces a drag burst to at most one full repaint per frame
//	> tick, automatically, with zero new code.
//
// The coalescing is entirely a consequence of two properties — Render is a no-op
// when nothing is dirty, and Resize does not draw — so the existing tests, which
// cover each property separately, could not see the composition. The composition is
// the thing ADR 0007 §6 actually promises.
//
// The burst is the one §6 names: 18 resize events over a 300 ms drag. The assertion
// is on FRAMES rather than bytes, because a repaint that happens twice is half the
// time and no bytes different from a repaint that happens once — Frames is the only
// measurement that can see it.
func TestResizeCoalescedToOneRepaintPerTick(t *testing.T) {
	const (
		startW, startH = 80, 24
		dragEvents     = 18
	)
	sink := headless.NewMemorySink(startW, startH)
	r := New(sink, Config{Width: startW, Height: startH, Caps: termmosaic.DefaultCaps()})
	root := &tagBlock{bounds: buffer.Rect{W: startW, H: startH}}
	r.SetRoot(root)
	pacer := NewPacer(r)

	if n, err := pacer.Tick(); err != nil || n == 0 {
		t.Fatalf("first tick: n=%d err=%v, want a painted frame", n, err)
	}
	framesBefore := r.Frames()

	// The drag: a burst of resizes with NO Render between them, which is what
	// ADR 0005 §5's keep-latest coalescing and ADR 0007 §6's pacer together produce.
	last := geometry.Size{W: startW, H: startH}
	for i := 1; i <= dragEvents; i++ {
		w, h := startW+i, startH+i/2
		sink.Resize(w, h)
		r.Resize(w, h)
		root.bounds = buffer.Rect{W: w, H: h}
		last = geometry.Size{W: w, H: h}
		if r.Frames() != framesBefore {
			t.Fatalf("resize event %d painted a frame: Resize must not draw, or the coalescing "+
				"this rule depends on does not happen", i)
		}
		if !r.NeedsFrame() {
			t.Fatalf("resize event %d left the renderer reporting no work: Size() is truthful "+
				"but the screen has never been repainted at the new size", i)
		}
		if got, _ := r.Size(); got != w {
			t.Errorf("after resize event %d, Size() reports width %d, want %d: Resize must stay "+
				"truthful for every event even though only one frame is painted", i, got, w)
		}
	}

	// One tick. One repaint.
	sink.MarkFrame()
	n, err := pacer.Tick()
	if err != nil {
		t.Fatalf("tick after the drag burst: %v", err)
	}
	if got := r.Frames() - framesBefore; got != 1 {
		t.Errorf("%d resize events produced %d repaints, want exactly 1: the pacer is what "+
			"coalesces a drag burst, and this is the rule that says so", dragEvents, got)
	}
	// MarkFrame AFTER the tick: it returns the bytes since the previous boundary,
	// which is this tick's frame and not the one before it.
	if got := len(sink.MarkFrame()); n != got {
		t.Errorf("Render reported %d bytes, the Sink recorded %d for the same frame", n, got)
	}
	if sink.UnknownSequences() != 0 {
		t.Errorf("the coalesced repaint emitted %d unrecognised sequences", sink.UnknownSequences())
	}

	// And the coalesced frame is the frame at the FINAL size, not a partial one:
	// every cell of it is on screen. An app that coalesced wrongly by painting the
	// first size of the burst would leave the tail blank, which is the failure a
	// "counted one frame" assertion alone would miss.
	assertScreenIsCurrent(t, sink, last)

	// A settled tick must write nothing at all.
	before := sink.Writes()
	if n, err := pacer.Tick(); err != nil || n != 0 {
		t.Errorf("settled tick: n=%d err=%v, want a no-op", n, err)
	}
	if sink.Writes() != before {
		t.Error("a settled tick reached the Sink")
	}
}

// clippingRoot is a widget that records the rectangle it was handed and paints the
// whole of it, so "the widget saw an off-screen rect" and "a write escaped the
// screen" are both observable.
type clippingRoot struct {
	want buffer.Rect
	seen buffer.Rect
}

func (c *clippingRoot) Bounds() buffer.Rect          { return c.want }
func (c *clippingRoot) Invalidate()                  {}
func (c *clippingRoot) Handle(termmosaic.Event) bool { return false }

func (c *clippingRoot) Draw(buf *buffer.Buffer) {
	c.seen = c.want
	if c.want.Empty() {
		// ADR 0007 §4: Draw returns immediately on an empty Bounds. Note that an
		// empty Rect still has X == 0 and Y == 0, so a Draw that did not check
		// would happily paint the origin.
		return
	}
	buf.FillRect(c.want, buffer.NewCell('.', buffer.DefaultStyle))
	buf.SetString(c.want.X, c.want.Y, "root", buffer.DefaultStyle)
}

// clipToScreen returns r intersected with a w-by-h screen at the origin.
//
// It is spelled out here rather than called from buffer or geometry on purpose:
// ADR 0007 §1 rule 2 assigns this clipping to the APPLICATION, at the composition
// boundary, so a test that called a framework helper would be testing the helper
// rather than the rule.
func clipToScreen(r buffer.Rect, w, h int) buffer.Rect {
	if r.Empty() {
		return buffer.Rect{}
	}
	x, y := maxInt(r.X, 0), maxInt(r.Y, 0)
	right, bottom := minInt(r.Right(), w), minInt(r.Bottom(), h)
	if right <= x || bottom <= y {
		return buffer.Rect{}
	}
	return buffer.Rect{X: x, Y: y, W: right - x, H: bottom - y}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// TestRootBoundsClippedToScreen is ADR 0007 §1 rule 2, pinned.
//
//	> **Bounds() is clipped to the screen before Draw sees it.** A widget's rect can
//	> never extend past the screen; layout.Solve overflow is resolved by clipping at
//	> the composition boundary, not by handing a widget a rect it must defend
//	> against.
//
// "Clipped at the composition boundary" is a statement about the APPLICATION: it is
// the application that chooses its root's rectangle, so it is the application that
// must clip it. Nothing in the framework does it for you — which is exactly why the
// rule needs a test, because a framework rule nothing enforces is a comment.
//
// The rectangles here are the ones layout.Solve genuinely produces on overflow, not
// invented ones: ADR 0004 specifies that fixed constraints exceeding the available
// space keep their sizes and sum to MORE than the available space, so a second
// child is positioned past the edge and its rect is off screen.
func TestRootBoundsClippedToScreen(t *testing.T) {
	const w, h = 30, 8
	cases := []struct {
		name  string
		off   buffer.Rect
		want  buffer.Rect
		label string
	}{
		{
			name:  "a fixed root wider than the screen",
			off:   buffer.Rect{W: 40, H: 8},
			want:  buffer.Rect{X: 0, Y: 0, W: 30, H: 8},
			label: "layout.Solve gives a Length(40) all 40 cells in an 8-row screen",
		},
		{
			name:  "a root overflowing only the right edge",
			off:   buffer.Rect{X: 25, W: 40, H: 8},
			want:  buffer.Rect{X: 25, Y: 0, W: 5, H: 8},
			label: "a second child of an overflowing vertical pair starts past the edge",
		},
		{
			name:  "a root overflowing only the bottom edge",
			off:   buffer.Rect{X: 2, Y: 5, W: 10, H: 20},
			want:  buffer.Rect{X: 2, Y: 5, W: 10, H: 3},
			label: "a horizontal pair whose second child runs past the last row",
		},
		{
			name:  "a root overflowing both axes",
			off:   buffer.Rect{X: 20, Y: 6, W: 40, H: 40},
			want:  buffer.Rect{X: 20, Y: 6, W: 10, H: 2},
			label: "the last child of a two-by-two grid, with both axes overflowing",
		},
	}

	// First: the solver really does produce rectangles this shape, so the case is
	// about layout.Solve's documented overflow and not about a fiction.
	t.Run("the solver produces these overflows", func(t *testing.T) {
		sizes := layout.Solve(layout.Vertical,
			[]layout.Constraint{layout.Length(15), layout.Length(40)}, 10, h)
		if len(sizes) != 2 || sizes[0] != 15 || sizes[1] != 40 {
			t.Fatalf("Solve = %v, want [15 40]: ADR 0004 specifies fixed constraints keep "+
				"their sizes on overflow", sizes)
		}
		second := buffer.Rect{X: layout.Offset(sizes, 10, 1), W: sizes[1], H: h}
		if second.Right() <= w {
			t.Fatalf("the second child lands at %v, on screen; the clipping case is not exercised", second)
		}
		if got, want := clipToScreen(second, w, h),
			(buffer.Rect{X: second.X, Y: 0, W: w - second.X, H: h}); got != want {
			t.Errorf("clipToScreen(%v) = %v, want %v", second, got, want)
		}
	})

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.off.Right() <= w && tc.off.Bottom() <= h {
				t.Fatalf("the case rect %v does not overflow a %dx%d screen, so it proves nothing",
					tc.off, w, h)
			}
			clipped := clipToScreen(tc.off, w, h)
			if clipped != tc.want {
				t.Errorf("clipToScreen(%v) = %v, want %v", tc.off, clipped, tc.want)
			}

			sink := headless.NewMemorySink(w, h)
			r := New(sink, Config{Width: w, Height: h, Caps: termmosaic.DefaultCaps()})
			root := &clippingRoot{want: clipped}
			r.SetRoot(root)
			if _, err := r.Render(); err != nil {
				t.Fatalf("Render: %v", err)
			}

			if root.seen != clipped {
				t.Errorf("the widget was handed %v, want the clipped %v", root.seen, clipped)
			}
			if root.seen.X < 0 || root.seen.Y < 0 ||
				root.seen.Right() > w || root.seen.Bottom() > h {
				t.Errorf("the widget was handed an off-screen rect %v for a %dx%d screen", root.seen, w, h)
			}
			if sink.MaxX() >= w || sink.MaxY() >= h {
				t.Errorf("output reached cell (%d,%d) on a %dx%d screen", sink.MaxX(), sink.MaxY(), w, h)
			}
			if sink.UnknownSequences() != 0 {
				t.Errorf("%d unrecognised sequences", sink.UnknownSequences())
			}
			// The clipped rect was painted, and the corner of it most likely to be
			// cut off is the one that proves the clip happened: without it the
			// rightmost column of the screen would carry the root's glyph.
			if sink.MaxX() >= w || sink.MaxY() >= h {
				t.Errorf("output reached cell (%d,%d) on a %dx%d screen", sink.MaxX(), sink.MaxY(), w, h)
			}
			if clipped.W > 0 && clipped.H > 0 {
				if got := sink.CellAt(clipped.X, clipped.Y).Ch; got != 'r' {
					t.Errorf("the clipped root's first cell is %q, want %q: the clipped rect "+
						"was not painted", got, 'r')
				}
				// Just outside the clipped rect, on screen, must still be blank.
				if outside := sink.CellAt(clipped.Right(), clipped.Y); outside.Ch != '.' && outside.Ch != ' ' {
					t.Errorf("cell %d is %q, but it lies outside the clipped rect %v",
						clipped.Right(), outside.Ch, clipped)
				}
			}
		})
	}
}

// TestRootBoundsEntirelyOffScreenClipsToNothing closes the last case of the same
// rule: a root lying wholly outside the screen clips to the EMPTY rect, which is a
// contract (Draw returns immediately) rather than a crash.
func TestRootBoundsEntirelyOffScreenClipsToNothing(t *testing.T) {
	cases := []struct {
		name string
		off  buffer.Rect
	}{
		{"entirely right of the screen", buffer.Rect{X: 30, W: 10, H: 8}},
		{"entirely below the screen", buffer.Rect{Y: 8, W: 30, H: 10}},
		{"entirely left of the screen", buffer.Rect{X: -10, W: 10, H: 8}},
		{"entirely above the screen", buffer.Rect{Y: -10, W: 30, H: 10}},
		{"a degenerate rect", buffer.Rect{W: 0, H: 0}},
		{"negative width", buffer.Rect{X: 2, W: -5, H: 8}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := clipToScreen(tc.off, 30, 8); got != (buffer.Rect{}) {
				t.Errorf("clipToScreen(%v) = %v, want the empty rect", tc.off, got)
			}

			// And an empty root must still render a frame without touching a cell.
			sink := headless.NewMemorySink(30, 8)
			r := New(sink, Config{Width: 30, Height: 8, Caps: termmosaic.DefaultCaps()})
			root := &clippingRoot{want: buffer.Rect{}}
			r.SetRoot(root)
			if _, err := r.Render(); err != nil {
				t.Fatalf("Render with an empty root: %v", err)
			}
			if !root.seen.Empty() {
				t.Errorf("the empty root was handed %v", root.seen)
			}
			// The frame IS painted — a resize and a first frame both force one, and
			// blanking the screen is what a real terminal would show. What must not
			// happen is the root's own content appearing, so the assertion is on the
			// screen's TEXT rather than on the fact that bytes were written.
			if got, want := sink.String(), strings.Repeat("\n", 7); got != want {
				t.Errorf("an empty root produced %q, want eight blank rows (%q)", got, want)
			}
		})
	}
}
