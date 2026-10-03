package diff

import (
	"strings"
	"testing"

	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/internal/ansi"
)

var (
	red   = buffer.NewColour(0xff, 0x00, 0x00)
	blue  = buffer.NewColour(0x00, 0x00, 0xff)
	white = buffer.NewColour(0xff, 0xff, 0xff)
)

// grid builds a w-by-h top-level buffer of spaces. It is a Buffer rather than a
// flat []buffer.Cell because Frame carries buffers (ADR 0006), so the fixtures
// build the same shape the renderer hands the diff.
func grid(w, h int) *buffer.Buffer {
	return buffer.NewBuffer(w, h)
}

func setCell(b *buffer.Buffer, x, y int, c buffer.Cell) {
	b.SetCell(x, y, c)
}

func TestDiffWritesNothingWhenNothingChanged(t *testing.T) {
	prev := grid(10, 3)
	cur := grid(10, 3)
	d := NewDiffer(1024)
	out := d.Diff(Frame{
		Cur: cur, Prev: prev, Width: 10, Height: 3,
		Encoder: ansi.Encoder{Depth: ansi.DepthTrueColor},
	})
	if len(out) != 0 {
		t.Fatalf("an idle frame must write 0 bytes, got %q", out)
	}
}

func TestRowSkipWritesNothingForIdenticalRows(t *testing.T) {
	prev := grid(10, 3)
	cur := grid(10, 3)
	d := NewDiffer(1024)

	// A dirty rect covering the whole grid, but no cell differs. The tier-1
	// row skip must suppress everything.
	out := d.Diff(Frame{
		Cur: cur, Prev: prev, Width: 10, Height: 3,
		Rects: []buffer.Rect{{W: 10, H: 3}},
	})
	if len(out) != 0 {
		t.Fatalf("expected 0 bytes for an unchanged grid, got %d: %q", len(out), out)
	}
}

func TestRowSkipEmitsOnlyDirtyRows(t *testing.T) {
	w, h := 10, 5
	prev := grid(w, h)
	cur := grid(w, h)
	// One changed cell on row 2 only.
	setCell(cur, 3, 2, buffer.NewCell('X', red, buffer.DefaultColour, 0))

	d := NewDiffer(1024)
	out := string(d.Diff(Frame{
		Cur: cur, Prev: prev, Width: w, Height: h,
		Rects: []buffer.Rect{{W: w, H: h}},
	}))
	// Exactly one cursor position, one SGR restating the absolute style (the
	// diff cannot assume what the terminal is in on its first written cell),
	// and one rune.
	want := "\x1b[3;4H\x1b[38;2;255;0;0;49mX"
	if out != want {
		t.Fatalf("got %q, want %q", out, want)
	}
}

func TestEmitsSGROnColourChange(t *testing.T) {
	w, h := 6, 1
	prev := grid(w, h)
	cur := grid(w, h)
	for x := 0; x < 6; x++ {
		setCell(prev, x, 0, buffer.NewCell('a', buffer.DefaultColour, buffer.DefaultColour, 0))
		setCell(cur, x, 0, buffer.NewCell('a', buffer.DefaultColour, buffer.DefaultColour, 0))
	}
	setCell(cur, 2, 0, buffer.NewCell('a', red, buffer.DefaultColour, 0))

	d := NewDiffer(1024)
	out := string(d.Diff(Frame{
		Cur: cur, Prev: prev, Width: w, Height: h,
		Rects:   []buffer.Rect{{W: w, H: h}},
		Encoder: ansi.Encoder{Depth: ansi.DepthTrueColor},
	}))
	// One move, one absolute SGR with the truecolor foreground, one rune. The
	// other five cells are unchanged and cost nothing.
	want := "\x1b[1;3H\x1b[38;2;255;0;0;49ma"
	if out != want {
		t.Fatalf("got %q, want %q", out, want)
	}
}

func TestNoSGRWhenOnlyTheRuneChanges(t *testing.T) {
	w, h := 4, 1
	prev := grid(w, h)
	cur := grid(w, h)
	for x := 0; x < w; x++ {
		setCell(prev, x, 0, buffer.NewCell('a', white, white, 0))
		setCell(cur, x, 0, buffer.NewCell('a', white, white, 0))
	}
	setCell(cur, 1, 0, buffer.NewCell('b', white, white, 0))

	d := NewDiffer(1024)
	// Seed the tracked style from a previous frame that left the cursor with
	// this style, which is what makes "no SGR" the correct output.
	out := string(d.Diff(Frame{
		Cur: cur, Prev: prev, Width: w, Height: h,
		Rects:      []buffer.Rect{{W: w, H: h}},
		PrevCursor: Cursor{Valid: true, Style: ansi.Style{FG: white, BG: white}},
		Encoder:    ansi.Encoder{Depth: ansi.DepthTrueColor},
	}))
	if out != "\x1b[1;2Hb" {
		t.Fatalf("got %q, want %q (an unchanged style must not re-emit SGR)", out, "\x1b[1;2Hb")
	}
}

func TestSGRIsEmittedForEveryStyleChangeNotEveryCell(t *testing.T) {
	w, h := 4, 1
	prev := grid(w, h)
	cur := grid(w, h)
	// Four adjacent cells, all changed, two sharing a style.
	setCell(cur, 0, 0, buffer.NewCell('a', red, blue, buffer.AttrBold))
	setCell(cur, 1, 0, buffer.NewCell('b', red, blue, buffer.AttrBold))
	setCell(cur, 2, 0, buffer.NewCell('c', white, blue, buffer.AttrBold))
	setCell(cur, 3, 0, buffer.NewCell('d', red, blue, 0))

	d := NewDiffer(1024)
	out := string(d.Diff(Frame{
		Cur: cur, Prev: prev, Width: w, Height: h,
		Rects:   []buffer.Rect{{W: w, H: h}},
		Encoder: ansi.Encoder{Depth: ansi.DepthTrueColor},
	}))
	// Cell 0: SGR. Cells 1..3: runes only until the style changes at 2, then
	// SGR again. So exactly 2 SGR sequences for 4 changed cells.
	// Three style changes across four adjacent cells: cells 0 and 1 share a
	// style and are written as one run ("ab"), cells 2 and 3 each change it.
	// So one cursor move plus three SGR sequences.
	if n := strings.Count(out, "\x1b["); n != 4 {
		t.Fatalf("got %d escape sequences, want 1 CUP + 3 SGR: %q", n, out)
	}
	// No escape sequence between 'a' and 'b': they are one run.
	if !strings.Contains(out, "ab\x1b[") {
		t.Errorf("expected adjacent same-style cells to be written as a run: %q", out)
	}
}

func TestColorDepthLadder(t *testing.T) {
	w, h := 2, 1
	prev := grid(w, h)
	cur := grid(w, h)
	setCell(cur, 0, 0, buffer.NewCell('a', red, blue, 0))

	cases := []struct {
		depth ansi.Depth
		want  string
	}{
		{ansi.DepthTrueColor, "\x1b[1;1H\x1b[38;2;255;0;0;48;2;0;0;255ma"},
		// #0000ff is present in the 16-colour set as bright blue (index 12),
		// so the ladder resolves to 12 / 104 rather than a cube entry.
		{ansi.Depth256, "\x1b[1;1H\x1b[38;5;9;48;5;12ma"},
		{ansi.Depth16, "\x1b[1;1H\x1b[91;104ma"},
	}
	for _, tc := range cases {
		t.Run(tc.depth.String(), func(t *testing.T) {
			d := NewDiffer(1024)
			out := string(d.Diff(Frame{
				Cur: cur, Prev: prev, Width: w, Height: h,
				Rects:   []buffer.Rect{{W: w, H: h}},
				Encoder: ansi.Encoder{Depth: tc.depth},
			}))
			if out != tc.want {
				t.Fatalf("depth %s:\n got %q\nwant %q", tc.depth, out, tc.want)
			}
		})
	}
}

func TestNoColorSuppressesColourButKeepsAttributes(t *testing.T) {
	w, h := 2, 1
	prev := grid(w, h)
	cur := grid(w, h)
	setCell(cur, 0, 0, buffer.NewCell('a', red, blue, buffer.AttrUnderline|buffer.AttrBold))

	d := NewDiffer(1024)
	out := string(d.Diff(Frame{
		Cur: cur, Prev: prev, Width: w, Height: h,
		Rects:   []buffer.Rect{{W: w, H: h}},
		Encoder: ansi.Encoder{Depth: ansi.DepthTrueColor, NoColor: true},
	}))
	if strings.Contains(out, "38;2") || strings.Contains(out, "48;2") {
		t.Fatalf("NO_COLOR must suppress colour, got %q", out)
	}
	if !strings.Contains(out, "\x1b[0;1;4m") {
		t.Fatalf("attributes must survive NO_COLOR, got %q", out)
	}
}

func TestDepthNoneEmitsNoColour(t *testing.T) {
	w, h := 2, 1
	prev := grid(w, h)
	cur := grid(w, h)
	setCell(cur, 0, 0, buffer.NewCell('a', red, red, 0))

	d := NewDiffer(1024)
	out := string(d.Diff(Frame{
		Cur: cur, Prev: prev, Width: w, Height: h,
		Rects:   []buffer.Rect{{W: w, H: h}},
		Encoder: ansi.Encoder{Depth: ansi.DepthNone},
	}))
	// DepthNone still emits a reset: an earlier frame may have set attributes,
	// and there is no portable "SGR 22 off", so clearing means reset.
	if out != "\x1b[1;1H\x1b[0ma" {
		t.Fatalf("got %q, want a reset with no colour", out)
	}
}

func TestForceFullIgnoresRects(t *testing.T) {
	w, h := 4, 2
	prev := grid(w, h)
	cur := grid(w, h)
	// ForceFull with identical grids must still emit everything.
	d := NewDiffer(4096)
	out := string(d.Diff(Frame{
		Cur: cur, Prev: prev, Width: w, Height: h,
		ForceFull: true,
	}))
	if len(out) == 0 {
		t.Fatal("ForceFull must emit the whole grid even when nothing changed")
	}
	if strings.Count(out, " ") != w*h {
		t.Fatalf("expected %d runes, got %q", w*h, out)
	}
}

func TestContinuationCellIsNotWritten(t *testing.T) {
	w, h := 4, 1
	prev := grid(w, h)
	cur := grid(w, h)
	// A wide glyph at cells 0 and 1, written via SetString semantics.
	cur.SetCell(0, 0, buffer.NewCell('漢', red, buffer.DefaultColour, 0))
	cur.SetCell(1, 0, buffer.NewCell(0, red, buffer.DefaultColour, 0))
	// Mark cell 1 as the continuation half the way SetString does. The flag is
	// private, so reach it through a Buffer, which is the only supported path.
	b := buffer.NewBuffer(w, h)
	b.SetString(0, 0, "漢", red, buffer.DefaultColour, 0)
	cur.SetCell(0, 0, b.CellAt(0, 0))
	cur.SetCell(1, 0, b.CellAt(1, 0))

	d := NewDiffer(1024)
	out := string(d.Diff(Frame{
		Cur: cur, Prev: prev, Width: w, Height: h,
		Rects:   []buffer.Rect{{W: w, H: h}},
		Encoder: ansi.Encoder{Depth: ansi.DepthNone},
	}))
	// Only the glyph rune is emitted, not the continuation's zero rune.
	if strings.Contains(out, "\x00") {
		t.Fatalf("continuation rune leaked into the stream: %q", out)
	}
	if !strings.Contains(out, "漢") {
		t.Fatalf("the glyph itself must be emitted: %q", out)
	}
}

// TestDiffRejectsMismatchedFrameSize is the guard ADR 0006 adds because Frame
// carries buffers rather than flat slices: Width and Height are now redundant
// with Cur, and two sources of truth would be their own footgun. The check runs
// once per Diff call, not per row, and it fails loudly because a mismatch means
// the renderer handed the diff two different grids — which mis-indexes into
// wrong pixels rather than a crash.
func TestDiffRejectsMismatchedFrameSize(t *testing.T) {
	d := NewDiffer(1024)
	enc := ansi.Encoder{Depth: ansi.DepthTrueColor}
	rects := []buffer.Rect{{W: 4, H: 2}}

	cases := []struct {
		name        string
		f           Frame
		wantInPanic string
	}{
		{
			name: "nil Cur",
			f:    Frame{Prev: grid(4, 2), Width: 4, Height: 2, Rects: rects, Encoder: enc},
		},
		{
			name: "nil Prev",
			f:    Frame{Cur: grid(4, 2), Width: 4, Height: 2, Rects: rects, Encoder: enc},
		},
		{
			name:        "Cur wider than the frame",
			f:           Frame{Cur: grid(6, 2), Prev: grid(4, 2), Width: 4, Height: 2, Rects: rects, Encoder: enc},
			wantInPanic: "Cur",
		},
		{
			name:        "Cur shorter than the frame",
			f:           Frame{Cur: grid(4, 1), Prev: grid(4, 2), Width: 4, Height: 2, Rects: rects, Encoder: enc},
			wantInPanic: "Cur",
		},
		{
			name:        "Prev wider than the frame",
			f:           Frame{Cur: grid(4, 2), Prev: grid(8, 2), Width: 4, Height: 2, Rects: rects, Encoder: enc},
			wantInPanic: "Prev",
		},
		{
			name:        "Prev shorter than the frame",
			f:           Frame{Cur: grid(4, 2), Prev: grid(4, 3), Width: 4, Height: 2, Rects: rects, Encoder: enc},
			wantInPanic: "Prev",
		},
		{
			name:        "frame larger than both buffers",
			f:           Frame{Cur: grid(4, 2), Prev: grid(4, 2), Width: 8, Height: 2, Rects: rects, Encoder: enc},
			wantInPanic: "Cur",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// The guard is unconditional: it fires even for an idle frame with
			// no rects, because "did anything change" is not what makes a
			// malformed Frame safe.
			defer func() {
				r := recover()
				if r == nil {
					t.Fatal("Diff must panic on a frame whose buffers disagree with its size")
				}
				msg, ok := r.(string)
				if !ok {
					t.Fatalf("panic value is %T, want a string", r)
				}
				if tc.wantInPanic != "" && !strings.Contains(msg, tc.wantInPanic) {
					t.Errorf("panic message %q does not name %s", msg, tc.wantInPanic)
				}
				t.Logf("panic: %s", msg)
			}()
			d.Diff(tc.f)
		})
	}
}

// TestDiffRejectsSubBuffer pins the second half of the guard: the frame's
// buffers must be top-level, because RowBytes refuses a view. Before ADR 0006
// this was undetectable — a bare []Cell carries no stride to check a flat index
// against, which is how the original unsound byte compare shipped.
func TestDiffRejectsSubBuffer(t *testing.T) {
	// The parent is wider than the view, so the view's stride differs from its
	// width while its cell count still matches the frame's dimensions. Only the
	// stride can tell the two apart — exactly the case the old flat []Cell API
	// could not detect, because a slice carries no stride.
	parent := buffer.NewBuffer(12, 10)
	sub := parent.SubBuffer(0, 0, 10, 10)
	if got := sub.Width() * sub.Height(); got != 100 {
		t.Fatalf("fixture is wrong: the view holds %d cells, want 100", got)
	}

	d := NewDiffer(1024)
	f := Frame{
		Cur: sub, Prev: parent, Width: 10, Height: 10,
		Rects:   []buffer.Rect{{W: 10, H: 10}},
		Encoder: ansi.Encoder{Depth: ansi.DepthTrueColor},
	}

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("Diff must not accept a sub-buffer: its rows are not byte-comparable")
		}
		t.Logf("panic: %v", r)
	}()
	d.Diff(f)
}

func TestCursorDiffedSeparately(t *testing.T) {
	w, h := 4, 4
	prev := grid(w, h)
	cur := grid(w, h)
	d := NewDiffer(1024)

	// No cells changed, cursor moves: still bytes, but only a position.
	out := string(d.Diff(Frame{
		Cur: cur, Prev: prev, Width: w, Height: h,
		Cursor:     Cursor{X: 2, Y: 1, Visible: true, Valid: true},
		PrevCursor: Cursor{X: 0, Y: 0, Visible: true, Valid: true},
	}))
	if out != "\x1b[2;3H" {
		t.Fatalf("got %q, want a bare cursor move", out)
	}

	// Nothing changed at all, cursor unchanged: zero bytes.
	out = string(d.Diff(Frame{
		Cur: cur, Prev: prev, Width: w, Height: h,
		Cursor:     Cursor{X: 2, Y: 1, Visible: true, Valid: true},
		PrevCursor: Cursor{X: 2, Y: 1, Visible: true, Valid: true},
	}))
	if len(out) != 0 {
		t.Fatalf("a fully idle frame must write 0 bytes, got %q", out)
	}
}

func TestCursorVisibilityChange(t *testing.T) {
	w, h := 2, 2
	prev := grid(w, h)
	cur := grid(w, h)
	d := NewDiffer(1024)
	out := string(d.Diff(Frame{
		Cur: cur, Prev: prev, Width: w, Height: h,
		Cursor:     Cursor{X: 0, Y: 0, Visible: true, Valid: true},
		PrevCursor: Cursor{X: 0, Y: 0, Visible: false, Valid: true},
	}))
	// The position is unchanged, so only the visibility toggle is written.
	if out != ansi.ShowCursor {
		t.Fatalf("got %q, want only a show-cursor", out)
	}
}

func TestWideGlyphOnlyWrittenOnce(t *testing.T) {
	// Regression guard: writing the continuation cell's zero rune would clear
	// the glyph on screen. The diff must skip it.
	w, h := 4, 1
	prev := grid(w, h)
	src := buffer.NewBuffer(w, h)
	src.SetString(0, 0, "漢", red, buffer.DefaultColour, 0)
	cur := grid(w, h)
	for x := 0; x < w; x++ {
		cur.SetCell(x, 0, src.CellAt(x, 0))
	}
	d := NewDiffer(1024)
	out := string(d.Diff(Frame{
		Cur: cur, Prev: prev, Width: w, Height: h,
		Rects:   []buffer.Rect{{W: w, H: h}},
		Encoder: ansi.Encoder{Depth: ansi.DepthNone},
	}))
	if out != "\x1b[1;1H\x1b[0m漢" {
		t.Fatalf("got %q, want a single glyph with no continuation rune", out)
	}
}
