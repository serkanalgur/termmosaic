// Command hello is TermMosaic's runnable example: it clears the screen, draws a
// bordered block with a live frame counter and the negotiated colour depth, and
// exits on q.
//
// It is also the project's smoke test against a real terminal. The golden test
// beside it renders exactly this widget into a MemorySink and compares the result
// against a checked-in file, which is the regression net for the buffer, the
// diff, the layout solver, the renderer and the encoder with no terminal
// involved at all.
//
// Input goes through the real input subsystem: one goroutine over
// source.Events() receives keys, mouse events and resizes on one ordered
// channel, instead of the two-goroutine input-plus-resize dance and the raw-byte
// scan for 'q' this example used before ADR 0005. Everything about how those
// bytes got decoded is the input package's business, not the example's.
//
// The border and the title belong to widgets/block, which is the catalog's only
// owner of both (ADR 0008 §2). This example used to draw them by hand, with its own
// W<4 and W<16 guards, and that was the single worst thing about it: it was a second
// border implementation in an example, which is precisely the collision Block
// exists to prevent. It now composes a Block, spells no border rune and invents no
// threshold, and asks the Block for the interior rectangle its body draws into.
package main

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/input"
	"github.com/serkanalgur/termmosaic/render"
	"github.com/serkanalgur/termmosaic/term"
	"github.com/serkanalgur/termmosaic/widgets/block"
)

// The example's palette: four plain colours. This is an application's styling
// decision, which is where ADR 0008 puts it — the framework ships no default
// colours at all, only attribute-only named styles.
var (
	bg      = buffer.NewColour(0x10, 0x14, 0x1c)
	fg      = buffer.NewColour(0xd8, 0xdc, 0xe4)
	titleFg = buffer.NewColour(0x30, 0xc0, 0x80)
	dim     = buffer.NewColour(0x60, 0x6a, 0x7a)
	edge    = buffer.NewColour(0x30, 0x36, 0x40)
)

// The styles the example draws with, each built through NewStyle rather than a
// composite literal: a partial literal would silently leave a channel at opaque
// black, which is Style's documented footgun.
var (
	stBody   = buffer.NewStyle(fg, bg, 0)
	stMuted  = buffer.NewStyle(dim, bg, 0)
	stValue  = buffer.NewStyle(fg, bg, 0)
	stAccent = buffer.NewStyle(titleFg, bg, 0)
	stTitle  = buffer.NewStyle(titleFg, bg, buffer.AttrBold)
	stEdge   = buffer.NewStyle(edge, bg, 0)
)

// blockW and blockH are the example's block size in cells.
const (
	blockW = 46
	blockH = 9
)

// hello is the example's widget: a Block for the chrome, plus a body that changes
// each frame.
//
// The Block is a value field rather than a pointer because it has no identity: it is
// chrome, and a widget tree holding a pointer to a border would be one more thing to
// keep alive for no reason.
type hello struct {
	bounds buffer.Rect
	// blk draws the border, the background and the title, and reports the interior
	// rectangle the body draws into. It is the only thing in this file that knows
	// what a border is.
	blk block.Block
	// depth is the colour depth the renderer negotiated. It is shown in the
	// block so the degradation ladder is visible in a real terminal.
	depth buffer.ColourDepth
	// ticks counts the frames drawn, so the block has something dynamic for the
	// diff to skip past.
	ticks int
}

// newHello returns the example's widget with its chrome configured.
//
// The Block is configured ONCE, here, rather than per frame: the title is constant,
// so the span slice and the truncated form are built at construction and the draw
// path only reads them (ADR 0008 §4).
func newHello(r buffer.Rect, depth buffer.ColourDepth) *hello {
	h := &hello{bounds: r, depth: depth}
	h.blk.SetBounds(r)
	h.blk.SetBorder(buffer.BorderPlain)
	h.blk.SetBorderStyle(stEdge)
	h.blk.SetBackground(stBody)
	h.blk.SetPadding(1)
	// The title is "termmosaic", not " termmosaic ": Block inserts the one space on
	// each side itself, and a caller that added them would shift the title two
	// columns right of where the border's interior says it belongs.
	h.blk.SetTitleString("termmosaic", stTitle)
	return h
}

// Bounds returns the block's rectangle.
func (h *hello) Bounds() buffer.Rect { return h.bounds }

// Invalidate satisfies termmosaic.Widget. The block redraws in full every frame,
// so there is nothing finer-grained to mark.
func (h *hello) Invalidate() {}

// Handle satisfies termmosaic.Widget. The example quits from its own input
// goroutine, so no key is handled here.
func (h *hello) Handle(termmosaic.Event) bool { return false }

// Draw paints the block.
//
// It runs every frame, as ADR 0003 specifies: widgets describe themselves on
// demand and are not required to implement incremental drawing, because Go cannot
// enforce invalidation discipline and silent invalidation bugs are the worst failure
// mode a TUI has.
//
// It allocates nothing. The Block caches its truncated title against the rect it was
// computed for, so the only thing here that varies per frame is the frame counter,
// which is written digit by digit.
func (h *hello) Draw(buf *buffer.Buffer) {
	// The Block is told the bounds on every frame rather than only at construction:
	// the application recomputes them on a resize, and a Block whose rectangle were
	// stale would paint chrome in the wrong place (ADR 0007 §3).
	h.blk.SetBounds(h.bounds)
	h.blk.Draw(buf)

	// The body draws into the Block's interior, so it cannot touch the border, the
	// title or the padding however the block is resized.
	h.drawBody(buf, h.blk.Interior())
}

// drawBody paints the label/value rows. It only indexes, writes a small number
// digit by digit and calls SetString, so the whole thing is allocation-free —
// which is what ADR 0008 §4 requires of Draw.
func (h *hello) drawBody(buf *buffer.Buffer, r buffer.Rect) {
	h.ticks++
	rows := [...]struct {
		label string
		value string
		st    buffer.Style
	}{
		{"frame", "", stValue},
		{"depth", h.depth.String(), stMuted},
		{"quit", "press q", stAccent},
	}
	for i, row := range rows {
		// labelCol and valueCol are the body's own columns inside the block's
		// interior, so the border, the title and the padding are all handled by the
		// Block and none of this arithmetic knows they exist.
		const labelCol, valueCol = 0, 10
		if r.W <= valueCol || r.H <= i {
			// Too narrow for a value, or too short for this row: skip it and keep
			// the rest. Clipping, never blanking.
			continue
		}
		y := r.Y + i
		buf.SetString(r.X+labelCol, y, row.label, stMuted)
		if i == 0 {
			// The frame counter is written digit by digit rather than formatted
			// into a string. fmt.Sprintf allocates, and one allocation per frame is
			// exactly what the 0-allocs draw path forbids; this is the same hazard
			// ADR 0008 §4 names for Wrap and Truncate, one function call earlier in
			// the pipeline.
			h.writeInt(buf, r.X+valueCol, y, h.ticks)
			continue
		}
		buf.SetString(r.X+valueCol, y, row.value, row.st)
	}
}

// writeInt writes v's decimal digits left to right at (x, y) in stValue and
// returns the x coordinate just past them. It is the allocation-free replacement
// for fmt.Sprintf("%d", v) on the draw path.
//
// v is clamped at zero: a negative frame count would need a sign, and no
// meaningful number here is negative, so the extra branch would be dead weight.
func (h *hello) writeInt(buf *buffer.Buffer, x, y, v int) int {
	if v < 0 {
		v = 0
	}
	// Walk to the highest decimal place, then step back down emitting digits.
	div := 1
	for d := v / 10; d > 0; d /= 10 {
		div *= 10
	}
	for {
		buf.Set(x, y, rune('0'+v/div%10), stValue)
		x++
		if div == 1 {
			return x
		}
		div /= 10
	}
}

// centred returns a w-by-h rectangle centred in a sw-by-sh screen, clamped so a
// block never asks for more room than the terminal has. Taking the screen size as
// an argument rather than reading a global keeps the golden test able to drive
// exactly this code.
func centred(sw, sh, w, h int) buffer.Rect {
	bw, bh := w, h
	if bw > sw {
		bw = sw
	}
	if bh > sh {
		bh = sh
	}
	return buffer.Rect{X: (sw - bw) / 2, Y: (sh - bh) / 2, W: bw, H: bh}
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "hello:", err)
		os.Exit(1)
	}
}

func run() error {
	// Fall back to the headless path when stdout is not a terminal, so the
	// example is runnable in CI and under redirection instead of erroring out.
	if !isTerminal(os.Stdout) {
		fmt.Println("hello: stdout is not a terminal, so there is nothing to draw onto.")
		fmt.Println("hello: run it in a real terminal, or run the golden test:")
		fmt.Println("           go test ./examples/hello/")
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

	r := render.New(term.NewSink(os.Stdout), render.Config{
		Width:   w,
		Height:  h,
		Caps:    caps,
		NoColor: render.NoColorFromEnv(os.Getenv),
	})
	// This application has no text input, so a cursor blinking in the last cell
	// drawn would look like a bug. It is managed-but-hidden: the renderer still
	// positions it, it is just never visible.
	r.SetCursor(render.Cursor{Valid: true, Visible: false})

	root := newHello(centred(w, h, blockW, blockH), caps.ColourDepth())
	r.SetRoot(root)

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

	// quit is closed by whichever goroutine decides the program is finished.
	// Both the input loop and a signal handler may want to stop the program, and
	// closing a channel twice panics, so the close goes through sync.Once.
	quit := make(chan struct{})
	var quitOnce sync.Once
	stop := func() { quitOnce.Do(func() { close(quit) }) }

	// Input. The Source is built AFTER raw mode, because the kitty keyboard query
	// it sends is answered on the input stream and is meaningless without raw
	// mode. WriteProbe is wired to the example's own sink rather than to a method
	// on Terminal, which is the point of ADR 0005 not widening ADR 0001's
	// interface.
	//
	// DefaultConfig requests bracketed paste and probes for kitty disambiguation;
	// it leaves mouse capture and focus reporting OFF, because enabling either by
	// default takes text selection and scrollback copying away from the user's
	// shell. A real application that wanted a mouse would set MouseMode.
	sink := term.NewSink(os.Stdout)
	src := input.NewSource(t, withProbe(input.DefaultConfig(), sink))

	// The body shows a frame counter that only changes when a frame is drawn, so
	// the example needs a heartbeat to keep the block alive at the target rate.
	// A real application would be invalidated by whatever it is displaying.
	stopBeat := make(chan struct{})
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stopBeat:
				return
			case <-quit:
				return
			case <-ticker.C:
				r.InvalidateAll()
			}
		}
	}()

	// Keys and resizes arrive on ONE ordered channel, so a resize can never be
	// delivered between the bytes of a half-read escape sequence, and no input
	// event is ever dropped to make room for a resize.
	go func() {
		for {
			select {
			case <-quit:
				return
			case ev, ok := <-src.Events():
				if !ok {
					// The terminal reached EOF.
					stop()
					return
				}
				switch ev.Kind {
				case termmosaic.EventKey:
					if isQuitKey(ev) {
						stop()
						return
					}
				case termmosaic.EventResize:
					w, h = ev.Size.W, ev.Size.H
					root.bounds = centred(w, h, blockW, blockH)
					r.Resize(w, h)
					// Force a full repaint: the previous frame described a
					// screen that no longer exists.
					r.InvalidateAll()
				}
			}
		}
	}()

	pacer := render.NewPacer(r)
	frameErr := make(chan error, 1)
	go func() { frameErr <- pacer.Run(quit) }()

	err = <-frameErr
	close(stopBeat)
	// Closing the Source restores the terminal modes it enabled and stops its
	// goroutines. It does not close the terminal, which the deferred t.Close()
	// above owns.
	_ = src.Close()
	_ = r.LeaveAltScreen()
	_ = t.LeaveRawMode()
	return err
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

// isQuitKey reports whether ev should end the example.
//
// These are the keys a person actually reaches for, expressed in the decoder's
// vocabulary rather than as raw bytes: Ctrl-C arrives as Ctrl+'c' rather than as
// 0x03, and Escape arrives as KeyEscape only after the decoder has waited out its
// ambiguity delay. The raw-byte version could not tell an arrow key from four
// unrelated bytes.
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
