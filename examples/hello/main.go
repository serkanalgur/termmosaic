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
// Note what this example does not use: the widget catalog is empty at this stage
// of the project. The block is drawn by direct buffer writes on purpose, because
// this example is the foundation layer and widgets get built against it later.
// The drawing code is therefore an example, not a reusable widget.
package main

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/render"
	"github.com/serkanalgur/termmosaic/term"
)

// Palette for the example. These are plain colour values, not a theme: the theme
// and styling system is still an OPEN decision in STATUS.md.
var (
	bg      = buffer.NewColour(0x10, 0x14, 0x1c)
	fg      = buffer.NewColour(0xd8, 0xdc, 0xe4)
	titleFg = buffer.NewColour(0x30, 0xc0, 0x80)
	dim     = buffer.NewColour(0x60, 0x6a, 0x7a)
	edge    = buffer.NewColour(0x30, 0x36, 0x40)
)

// blockW and blockH are the example's block size in cells.
const (
	blockW = 46
	blockH = 9
)

// hello is the example's widget: a bordered block whose body changes each frame.
type hello struct {
	bounds buffer.Rect
	// depth is the colour depth the renderer negotiated. It is shown in the
	// block so the degradation ladder is visible in a real terminal.
	depth buffer.ColourDepth
	// ticks counts the frames drawn, so the block has something dynamic for the
	// diff to skip past.
	ticks int
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
// demand and are not required to implement incremental drawing, because Go
// cannot enforce invalidation discipline and silent invalidation bugs are the
// worst failure mode a TUI has.
func (h *hello) Draw(buf *buffer.Buffer) {
	r := h.bounds
	if r.W < 4 || r.H < 4 {
		return
	}

	// The background is painted first and unconditionally. The renderer never
	// clears: it diffs against the previous frame, so a cell that is not
	// written keeps whatever was there. A widget that shrinks its content must
	// therefore repaint its whole bounds, or stale cells show through.
	buf.FillRect(r, buffer.NewCell(' ', fg, bg, 0))

	h.drawBorder(buf, r)
	h.drawTitle(buf, r)
	h.drawBody(buf, r)
}

// drawBorder paints the eight glyph corners and the runs between them.
//
// SetString would be shorter, but writing the eight cells explicitly shows
// exactly how much work a future Border widget has to do, and keeps this example
// honest about what the buffer API costs.
func (h *hello) drawBorder(buf *buffer.Buffer, r buffer.Rect) {
	horiz := buffer.NewCell('─', edge, bg, 0)
	vert := buffer.NewCell('│', edge, bg, 0)
	for x := r.X + 1; x < r.Right()-1; x++ {
		buf.SetCell(x, r.Y, horiz)
		buf.SetCell(x, r.Bottom()-1, horiz)
	}
	for y := r.Y + 1; y < r.Bottom()-1; y++ {
		buf.SetCell(r.X, y, vert)
		buf.SetCell(r.Right()-1, y, vert)
	}
	buf.SetCell(r.X, r.Y, buffer.NewCell('┌', edge, bg, 0))
	buf.SetCell(r.Right()-1, r.Y, buffer.NewCell('┐', edge, bg, 0))
	buf.SetCell(r.X, r.Bottom()-1, buffer.NewCell('└', edge, bg, 0))
	buf.SetCell(r.Right()-1, r.Bottom()-1, buffer.NewCell('┘', edge, bg, 0))
}

// drawTitle paints the inset title on the top border.
func (h *hello) drawTitle(buf *buffer.Buffer, r buffer.Rect) {
	if r.W < 16 {
		return
	}
	buf.SetString(r.X+2, r.Y, " termmosaic ", titleFg, bg, buffer.AttrBold)
}

// drawBody paints the label/value rows.
func (h *hello) drawBody(buf *buffer.Buffer, r buffer.Rect) {
	h.ticks++
	rows := []struct {
		label string
		value string
		col   buffer.Colour
	}{
		{"frame", fmt.Sprintf("%d", h.ticks), fg},
		{"depth", h.depth.String(), dim},
		{"quit", "press q", titleFg},
	}
	for i, row := range rows {
		y := r.Y + 2 + i
		if y >= r.Bottom()-1 || r.X+18 >= r.Right() {
			break
		}
		buf.SetString(r.X+2, y, row.label, dim, bg, 0)
		buf.SetString(r.X+12, y, row.value, row.col, bg, 0)
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

	root := &hello{depth: caps.ColourDepth(), bounds: centred(w, h, blockW, blockH)}
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

	// Resize: the renderer recomputes the layout and forces a full repaint,
	// because the previous frame described a screen that no longer exists.
	go func() {
		for {
			select {
			case <-quit:
				return
			case s, ok := <-t.ResizeEvents():
				if !ok {
					return
				}
				w, h = s.W, s.H
				root.bounds = centred(w, h, blockW, blockH)
				r.Resize(w, h)
				r.InvalidateAll()
			}
		}
	}()

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

	// Input: TermMosaic has no input decoder yet — see the term package
	// documentation — so this example scans raw bytes for a quit key itself.
	// That is a documented gap, not a demonstration of the intended API.
	go func() {
		b := make([]byte, 32)
		for {
			n, err := t.Read(b)
			if err != nil {
				return
			}
			for i := 0; i < n; i++ {
				switch b[i] {
				case 'q', 'Q', 0x03, 0x1b: // q, Q, Ctrl-C, Esc
					stop()
					return
				}
			}
		}
	}()

	pacer := render.NewPacer(r)
	frameErr := make(chan error, 1)
	go func() { frameErr <- pacer.Run(quit) }()

	err = <-frameErr
	close(stopBeat)
	_ = r.LeaveAltScreen()
	_ = t.LeaveRawMode()
	return err
}

func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}
