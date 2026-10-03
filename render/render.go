// Package render implements TermMosaic's hybrid renderer.
//
// ADR 0003 fixes the shape: a retained widget tree invalidated by rectangle,
// drawn through the two-tier cell diff from ADR 0002 into a Sink. There is no
// reconciler, no message algebra and no incremental-draw interface, and this
// package is where that decision becomes concrete.
//
// The frame pipeline is exactly:
//
//	events -> widget tree -> Draw into the back buffer -> take the dirty
//	rectangles -> two-tier diff -> Sink
//
// Two properties are load-bearing and both are tested:
//
//   - A frame in which nothing is dirty writes zero bytes. The renderer does not
//     walk the tree, does not run the diff, and does not call the Sink. This is
//     what makes an idle application free rather than a 60 Hz CPU burner.
//   - A resize forces a full repaint, because the previous frame's cells
//     describe a screen that no longer exists.
//
// The renderer is single-goroutine: it owns the buffers and is mutated only from
// the goroutine calling Render or Run. Widget state must be mutated from the
// same goroutine, or inside Post. ADR 0003 accepts that this is documentation
// and not a type, and calls the resulting silent data races its top risk.
package render

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/internal/ansi"
	"github.com/serkanalgur/termmosaic/internal/diff"
)

// DefaultTargetFPS is the frame rate the pacer aims for when none is set.
const DefaultTargetFPS = 60

// DefaultScratchBytes is the initial size of the renderer's byte scratch. It is
// sized for a 200x60 full repaint's worth of escape sequences so that a steady
// state never grows it.
const DefaultScratchBytes = 8 << 10

// Renderer draws a widget tree into a Sink.
type Renderer struct {
	mu sync.Mutex

	sink    termmosaic.Sink
	encoder ansi.Encoder
	noColor bool

	// front is what the terminal is showing; back is what widgets draw into.
	// They are swapped each frame, so no frame ever copies a whole screen.
	front, back *buffer.Buffer

	differ *diff.Differ
	// cur is the cursor the renderer manages, in the diff's representation.
	cur diff.Cursor
	// prevCursor is the cursor state the previous frame left in the terminal.
	prevCursor diff.Cursor

	root     termmosaic.Widget
	w, h     int
	dirty    []buffer.Rect
	forceAll bool

	targetFPS int
	frames    uint64
	// lastPaintN is the byte count of the most recent frame, read by
	// BytesWritten.
	lastPaintN int
	// posted holds callbacks queued by Post.
	posted []func()
}

// Config configures a Renderer.
type Config struct {
	// Width and Height are the initial screen size in cells.
	Width, Height int
	// Caps selects the colour-degradation rung and the character set.
	Caps termmosaic.Caps
	// NoColor suppresses colour regardless of Caps, honouring the NO_COLOR
	// environment convention. Read from the environment by NoColorFromEnv.
	NoColor bool
	// Quantiser overrides colour degradation. A nil value uses the default
	// redmean quantiser. This is the hook a Lab-space mapping would attach to.
	Quantiser buffer.Quantiser
	// TargetFPS is the frame-pacing budget. Zero means DefaultTargetFPS; values
	// below 1 are clamped.
	TargetFPS int
}

// New returns a Renderer that draws into sink.
func New(sink termmosaic.Sink, cfg Config) *Renderer {
	w, h := cfg.Width, cfg.Height
	if w < 0 {
		w = 0
	}
	if h < 0 {
		h = 0
	}
	fps := cfg.TargetFPS
	if fps <= 0 {
		fps = DefaultTargetFPS
	}
	r := &Renderer{
		sink:      sink,
		encoder:   ansi.Encoder{Depth: cfg.Caps.ColourDepth(), NoColor: cfg.NoColor, Quantiser: cfg.Quantiser},
		noColor:   cfg.NoColor,
		front:     buffer.NewBuffer(w, h),
		back:      buffer.NewBuffer(w, h),
		differ:    diff.NewDiffer(DefaultScratchBytes),
		w:         w,
		h:         h,
		targetFPS: fps,
		dirty:     make([]buffer.Rect, 0, 8),
	}
	// The first frame has no previous state, so everything must be written.
	r.forceAll = true
	r.back.MarkAllDirty()
	// The front buffer is what the terminal is already showing, so it has
	// nothing outstanding. Leaving it dirty would make every frame look like
	// there was work to do and the renderer would never go idle.
	r.front.ClearDirty()
	return r
}

// NoColorFromEnv reports whether the NO_COLOR environment convention applies.
// See https://no-color.org: any non-empty value disables colour output.
func NoColorFromEnv(getenv func(string) string) bool {
	return getenv("NO_COLOR") != ""
}

// SetRoot installs the widget tree to draw. Passing nil clears it.
//
// The new root is invalidated in full, because there is no previous content for
// it to be compared against.
func (r *Renderer) SetRoot(w termmosaic.Widget) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.root = w
	r.forceAll = true
	if r.back != nil {
		r.back.MarkAllDirty()
	}
}

// Root returns the installed widget tree.
func (r *Renderer) Root() termmosaic.Widget {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.root
}

// Size returns the screen size in cells.
func (r *Renderer) Size() (w, h int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.w, r.h
}

// Resize changes the screen size, recomputes the widget layout and forces a full
// repaint on the next frame.
//
// This is the SIGWINCH path. A full repaint is not an optimisation choice: the
// previous frame's cells describe a screen that no longer exists, so the diff
// has nothing valid to compare against and every cell must be written.
func (r *Renderer) Resize(w, h int) {
	if w < 0 {
		w = 0
	}
	if h < 0 {
		h = 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if w == r.w && h == r.h {
		return
	}
	r.w, r.h = w, h
	r.front.Resize(w, h)
	r.back.Resize(w, h)
	r.dirty = r.dirty[:0]
	r.forceAll = true
	r.differ.ResetStyle()
	// The cursor may now be outside the screen; hide it until it is restated.
	r.cur.X = clamp(r.cur.X, 0, max(w-1, 0))
	r.cur.Y = clamp(r.cur.Y, 0, max(h-1, 0))
	r.back.MarkAllDirty()
}

// Caps returns the colour depth the renderer is encoding at.
func (r *Renderer) Depth() buffer.ColourDepth {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.encoder.Depth
}

// SetDepth changes the colour depth, for example after capability re-probing.
// The next frame restates absolute style because the terminal's state is no
// longer what the renderer thinks it is.
func (r *Renderer) SetDepth(d buffer.ColourDepth) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.encoder.Depth == d {
		return
	}
	r.encoder.Depth = d
	r.differ.ResetStyle()
	r.forceAll = true
}

// SetCursor sets the cursor position, visibility and style for subsequent
// frames. A zero Cursor with Valid false leaves the cursor unmanaged.
func (r *Renderer) SetCursor(c Cursor) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cur = diff.Cursor{
		X: c.X, Y: c.Y, Visible: c.Visible, Valid: c.Valid,
		Style: ansi.Style{FG: c.FG, BG: c.BG, Attr: c.Attr},
	}
}

// Cursor is the renderer's view of the cursor.
type Cursor struct {
	X, Y    int
	Visible bool
	FG      buffer.Colour
	BG      buffer.Colour
	Attr    buffer.Attr
	// Valid reports whether the cursor is managed at all. False means the
	// renderer will not touch the cursor, which is what a full-screen
	// application usually wants.
	Valid bool
}

// CursorPos returns the renderer's cursor position and visibility.
func (r *Renderer) CursorPos() (x, y int, visible bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cur.X, r.cur.Y, r.cur.Visible
}

// Invalidate marks a region of the screen dirty. It is safe to call from any
// goroutine (ADR 0003).
func (r *Renderer) Invalidate(rct buffer.Rect) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.back != nil {
		r.back.MarkDirty(rct)
	}
}

// InvalidateAll marks the whole screen dirty. Safe from any goroutine.
func (r *Renderer) InvalidateAll() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.back != nil {
		r.back.MarkAllDirty()
	}
}

// NeedsFrame reports whether anything is dirty. It is the frame pacer's test: a
// false result means the next frame must write zero bytes, so the pacer can
// sleep instead of rendering.
func (r *Renderer) NeedsFrame() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.needsFrameLocked()
}

// needsFrameLocked reports whether the next frame has anything to do. The
// renderer lock must be held.
//
// A changed cursor counts as work even when no cell is dirty: the cursor is part
// of the frame the terminal shows, and a frame that moves it without a cell
// change is exactly what a focused text input produces on every keystroke it
// rejects. Treating that as idle would leave the cursor stuck.
func (r *Renderer) needsFrameLocked() bool {
	if r.forceAll || r.back.IsDirty() {
		return true
	}
	return r.cur.Valid && r.cur != r.prevCursor
}

// Frames returns how many frames have been painted.
func (r *Renderer) Frames() uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.frames
}

// BytesWritten returns the total bytes the renderer has written to the Sink.
func (r *Renderer) BytesWritten() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastPaintN
}

// Post queues fn to run on the render goroutine before the next frame.
//
// This is ADR 0003's whole async story: "mutate widget state only inside a Post
// callback". It is deliberately not an Elm architecture — no Cmd, no Msg, no
// reducer — because ADR 0003 argues the message algebra is for apps with a large
// message vocabulary and TermMosaic's users are writing dashboard widgets.
// runPosted drains and runs the posted callbacks outside the render lock.
func (r *Renderer) runPosted() {
	r.mu.Lock()
	posts := r.posted
	r.posted = nil
	r.mu.Unlock()
	for _, fn := range posts {
		fn()
	}
}

// Post queues fn to run on the render goroutine before the next frame.
//
// This is ADR 0003's whole async story: "mutate widget state only inside a Post
// callback". It is deliberately not an Elm architecture — no Cmd, no Msg, no
// reducer — because ADR 0003 argues the message algebra is for apps with a large
// message vocabulary and TermMosaic's users are writing dashboard widgets.
//
// A callback runs with the renderer's lock released, so it may safely call
// Invalidate. It must NOT call Render: the renderer is already inside a frame.
func (r *Renderer) Post(fn func()) {
	if fn == nil {
		return
	}
	r.mu.Lock()
	r.posted = append(r.posted, fn)
	r.mu.Unlock()
}

// FrameError is what Render reports when a frame could not be completed.
type FrameError struct {
	// Op is the failing operation, always "write" for now.
	Op string
	// Err is the underlying failure.
	Err error
	// Bytes is how many bytes the Sink accepted before failing.
	Bytes int
}

func (e *FrameError) Error() string {
	return fmt.Sprintf("termmosaic: frame %s failed after %d bytes: %v", e.Op, e.Bytes, e.Err)
}

// Unwrap returns the underlying error.
func (e *FrameError) Unwrap() error { return e.Err }

// Render draws one frame if anything is dirty, and returns the number of bytes
// written. A frame with nothing dirty returns 0 and touches neither the widget
// tree nor the Sink.
func (r *Renderer) Render() (int, error) {
	// Posted callbacks run with the renderer's lock released. A callback is
	// expected to mutate widget state and call Invalidate, and Invalidate takes
	// that lock, so draining them underneath it would deadlock. The cost is
	// that a callback must not call Render itself; that is documented on Post.
	r.runPosted()

	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.needsFrameLocked() {
		return 0, nil
	}

	// ADR 0003: Draw runs for every widget every frame. Widgets are not
	// required to implement incremental drawing, because Go cannot enforce
	// invalidation discipline the way a borrow checker does and silent
	// invalidation bugs are the worst failure mode a TUI has.
	if r.root != nil {
		r.root.Draw(r.back)
	}

	r.dirty = r.back.TakeDirtyInto(r.dirty, r.w, r.h)
	if r.forceAll {
		r.dirty = append(r.dirty[:0], buffer.Rect{W: r.w, H: r.h})
	}

	out := r.differ.Diff(diff.Frame{
		Cur:        r.back,
		Prev:       r.front,
		Width:      r.w,
		Height:     r.h,
		Rects:      r.dirty,
		Cursor:     r.cur,
		PrevCursor: r.prevCursor,
		ForceFull:  r.forceAll,
		Encoder:    r.encoder,
	})

	n := 0
	if len(out) > 0 {
		var err error
		n, err = r.sink.Write(out)
		if err != nil {
			return n, &FrameError{Op: "write", Err: err, Bytes: n}
		}
		if n != len(out) {
			return n, &FrameError{
				Op:    "write",
				Err:   errors.New("short write"),
				Bytes: n,
			}
		}
	}
	if err := r.sink.Flush(); err != nil {
		return n, &FrameError{Op: "flush", Err: err, Bytes: n}
	}

	// Swap the buffers so the frame just painted becomes the comparison base.
	// Nothing is copied: this is the whole reason for double buffering.
	r.front, r.back = r.back, r.front
	r.prevCursor = r.cur
	r.forceAll = false
	r.frames++
	r.lastPaintN = n
	return n, nil
}

// Pacer drives Render at the target frame rate, skipping frames where nothing is
// dirty.
//
// The pacing contract is a budget, not a guarantee: if a frame takes longer than
// the budget the pacer does not try to catch up, because a backlog of catch-up
// frames is how a TUI ends up spending all its time rendering. It draws the next
// frame when the next tick arrives.
type Pacer struct {
	r     *Renderer
	every time.Duration
}

// NewPacer returns a Pacer that renders at the renderer's configured rate.
func NewPacer(r *Renderer) *Pacer {
	fps := DefaultTargetFPS
	r.mu.Lock()
	fps = r.targetFPS
	r.mu.Unlock()
	return &Pacer{r: r, every: time.Second / time.Duration(fps)}
}

// Run renders until done is closed, then returns. It returns the first frame
// error it encounters, having stopped, so a caller never loses a write failure.
//
// Run blocks, so it belongs on its own goroutine or on the main one with the
// event loop elsewhere.
func (p *Pacer) Run(done <-chan struct{}) error {
	t := time.NewTicker(p.every)
	defer t.Stop()
	for {
		select {
		case <-done:
			return nil
		case <-t.C:
			// The dirty check happens inside Render, so an idle application
			// wakes up at the target rate, decides there is nothing to do, and
			// writes nothing. That is the trade for not having a wakeup
			// channel: a few microseconds per frame instead of a syscall.
			if !p.r.NeedsFrame() {
				continue
			}
			if _, err := p.r.Render(); err != nil {
				return err
			}
		}
	}
}

// Tick renders one frame if anything is dirty. It exists so a caller driving its
// own loop, or a test, can step the renderer deterministically instead of
// racing a ticker.
func (p *Pacer) Tick() (int, error) { return p.r.Render() }

// EnterAltScreen switches the sink to the alternate screen and clears it.
func (r *Renderer) EnterAltScreen() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return writeAll(r.sink, ansi.EnterAltScreen+ansi.EraseDisplay+ansi.HideCursor)
}

// LeaveAltScreen switches back to the main screen buffer, leaving the cursor
// visible and the alternate screen's contents behind.
func (r *Renderer) LeaveAltScreen() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return writeAll(r.sink, ansi.ShowCursor+ansi.LeaveAltScreen)
}

// Reset clears the screen and forces a full repaint on the next frame.
func (r *Renderer) Reset() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := writeAll(r.sink, ansi.EraseDisplay+ansi.CursorHome); err != nil {
		return err
	}
	r.differ.ResetStyle()
	r.prevCursor = diff.Cursor{}
	r.forceAll = true
	r.back.MarkAllDirty()
	return nil
}

// HideCursor hides the cursor without changing managed state.
func (r *Renderer) HideCursor() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return writeAll(r.sink, ansi.HideCursor)
}

// ShowCursor shows the cursor.
func (r *Renderer) ShowCursor() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return writeAll(r.sink, ansi.ShowCursor)
}

// writeAll writes a constant sequence to the sink, with the renderer's lock held
// by the caller.
func writeAll(sink termmosaic.Sink, s string) error {
	if _, err := sink.Write([]byte(s)); err != nil {
		return err
	}
	return sink.Flush()
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
