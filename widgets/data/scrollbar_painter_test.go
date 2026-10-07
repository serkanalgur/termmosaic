package data

import (
	"testing"

	"github.com/serkanalgur/termmosaic/buffer"
)

// thumbPos is one cell the scrollbar painter wrote.
type thumbPos struct{ x, y int }

// thumbCells scans buf for the thumb glyph r and returns every cell holding it,
// top row first. Asserting on positions rather than on whole rows is what makes
// a placement regression visible: two frames whose rows trim to the same string
// can still hold the thumb on different rows.
func thumbCells(buf *buffer.Buffer, r rune) []thumbPos {
	var out []thumbPos
	w, h := buf.Size()
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if buf.CellAt(x, y).Ch == r {
				out = append(out, thumbPos{x: x, y: y})
			}
		}
	}
	return out
}

// wantThumb asserts that the thumb glyph landed on exactly (wantX, y) for each
// y in wantY, and nowhere else on the screen.
func wantThumb(t *testing.T, got []thumbPos, wantX int, wantY []int) {
	t.Helper()
	if len(got) != len(wantY) {
		t.Fatalf("thumb occupies %d cells at %v, want %d at x=%d rows %v", len(got), got, len(wantY), wantX, wantY)
	}
	for i, y := range wantY {
		if got[i].x != wantX || got[i].y != y {
			t.Errorf("thumb cell %d is at (%d,%d), want (%d,%d)", i, got[i].x, got[i].y, wantX, y)
		}
	}
}

// filled returns a w-by-h buffer whose every cell holds the sentinel rune s, so
// a test can prove the painter wrote nothing at all.
func filled(w, h int, s rune) *buffer.Buffer {
	buf := cellBuf(w, h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			buf.SetCell(x, y, buffer.Cell{Ch: s})
		}
	}
	return buf
}

// untouched fails if any cell of buf no longer holds the sentinel s.
func untouched(t *testing.T, buf *buffer.Buffer, s rune) {
	t.Helper()
	w, h := buf.Size()
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if c := buf.CellAt(x, y).Ch; c != s {
				t.Fatalf("cell (%d,%d) = %q, want the untouched sentinel %q", x, y, c, s)
			}
		}
	}
}

// TestPaintScrollbarThumbWritesNothingWhenEverythingFits pins the painter's
// first documented rule: a collection that fits (maxOffset <= 0) writes
// nothing, which is itself the "everything is shown" signal the widgets rely
// on. The buffer is pre-filled with a sentinel so "wrote nothing" is
// observable rather than assumed.
func TestPaintScrollbarThumbWritesNothingWhenEverythingFits(t *testing.T) {
	track := buffer.Rect{X: 2, Y: 1, W: 1, H: 4}
	for _, tt := range []struct {
		name           string
		offset, maxOff int
		visible, count int
	}{
		{name: "the collection fits exactly", offset: 0, maxOff: 0, visible: 4, count: 4},
		{name: "a negative offset ceiling", offset: -1, maxOff: -3, visible: 2, count: 2},
		{name: "an offset beyond a zero ceiling", offset: 5, maxOff: 0, visible: 3, count: 3},
	} {
		buf := filled(8, 8, 'Z')
		paintScrollbarThumb(buf, track, tt.offset, tt.maxOff, tt.visible, tt.count, buffer.Style{}, thumbRune)
		untouched(t, buf, 'Z')
	}
}

// TestPaintScrollbarThumbClampsToTheOneCellMinimum pins the clamp every widget
// here leans on: when the visible fraction of the collection rounds to zero
// cells, the thumb is still exactly one cell tall. A thumb of zero height
// would erase the position signal entirely, and the widgets' own fixtures
// (100 items in four rows) hit this path on their first frame.
func TestPaintScrollbarThumbClampsToTheOneCellMinimum(t *testing.T) {
	track := buffer.Rect{X: 3, Y: 2, W: 1, H: 4}
	for _, tt := range []struct {
		name           string
		offset, maxOff int
		visible, count int
		wantY          int
	}{
		// thumbH = track.H * visible / count rounds to 0 in each of these;
		// the clamp raises it to one cell at the offset's start row, where
		// start = (track.H - 1) * offset / maxOff.
		{name: "at the top of the track", offset: 0, maxOff: 99, visible: 1, count: 100, wantY: 2},
		{name: "one third down the slack", offset: 33, maxOff: 99, visible: 1, count: 100, wantY: 3},
		{name: "at the bottom of the track", offset: 99, maxOff: 99, visible: 1, count: 100, wantY: 5},
		{name: "no rows visible at all", offset: 5, maxOff: 10, visible: 0, count: 10, wantY: 3},
	} {
		buf := filled(8, 8, 'Z')
		paintScrollbarThumb(buf, track, tt.offset, tt.maxOff, tt.visible, tt.count, buffer.Style{}, thumbRune)
		wantThumb(t, thumbCells(buf, thumbRune), track.Right()-1, []int{tt.wantY})
	}
}

// TestPaintScrollbarThumbSitsAtTheTopAtZeroAndReachesTheBottomAtMaxOffset pins
// the two ends of the placement: offset 0 puts the thumb's first cell on the
// track's first row, and offset == maxOffset puts its last cell on the track's
// last row. The thumb here is 6*3/6 = 3 cells tall, so both ends are visible
// as positions rather than collapsed into the one-cell clamp, and the sweep
// below asserts that nothing outside the track's column and rows changed.
func TestPaintScrollbarThumbSitsAtTheTopAtZeroAndReachesTheBottomAtMaxOffset(t *testing.T) {
	track := buffer.Rect{X: 1, Y: 3, W: 1, H: 6}
	const (
		visible = 3
		count   = 6
	)
	maxOffset := count - visible // the ceiling the widgets hand the painter
	for _, tt := range []struct {
		name   string
		offset int
		wantY  []int
	}{
		{name: "at the top", offset: 0, wantY: []int{3, 4, 5}},
		{name: "at the bottom", offset: maxOffset, wantY: []int{6, 7, 8}},
	} {
		buf := filled(8, 10, 'Z')
		paintScrollbarThumb(buf, track, tt.offset, maxOffset, visible, count, buffer.Style{}, thumbRune)
		wantThumb(t, thumbCells(buf, thumbRune), track.Right()-1, tt.wantY)
		// The thumb's position is the only thing this painter may change:
		// every other cell of the buffer is the sentinel it started as.
		w, h := buf.Size()
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				if x == track.Right()-1 && y >= track.Y && y < track.Y+track.H {
					continue
				}
				if c := buf.CellAt(x, y).Ch; c != 'Z' {
					t.Errorf("%s: cell (%d,%d) = %q, outside the track", tt.name, x, y, c)
				}
			}
		}
	}
}

// TestPaintScrollbarThumbIsTotalOnDegenerateTracks is ADR 0007 §4's total-Draw
// contract applied to the painter directly: every degenerate track — the zero
// rect, a zero or negative height — is defined, panics nowhere, and writes
// nothing at all, which is the observable form of "nothing outside the track"
// when the track holds no cells. The painter's guard is maxOffset <= 0 ||
// track.H < 1, and these are its track-side cases.
func TestPaintScrollbarThumbIsTotalOnDegenerateTracks(t *testing.T) {
	for _, tt := range []struct {
		name  string
		track buffer.Rect
	}{
		{name: "the zero rect", track: buffer.Rect{}},
		{name: "zero height", track: buffer.Rect{X: 2, Y: 1, W: 1, H: 0}},
		{name: "negative height", track: buffer.Rect{X: 2, Y: 1, W: 1, H: -3}},
		{name: "zero size at a non-zero origin", track: buffer.Rect{X: 5, Y: 5, W: 0, H: 0}},
	} {
		// No panic is asserted by the test running at all.
		buf := filled(10, 10, 'Z')
		paintScrollbarThumb(buf, tt.track, 2, 5, 2, 6, buffer.Style{}, thumbRune)
		untouched(t, buf, 'Z')
	}

	// A zero-width track with a positive height is exercised for panic-freedom
	// only, NOT for write placement, and the omission is deliberate. The
	// painter's guard is on the H axis, exactly as all three pre-refactor
	// drawScrollbar bodies guarded it (HEAD list.go `if h < 1`, table.go
	// `t.rowH <= 0`, tree.go `in.H <= 0`), and no caller can produce such a
	// track: barW is only ever 0 or scrollbarW (the constant 1), the painter is
	// called only when barW > 0, geometry.Budget keeps the size-1 scrollbar
	// region only when the interior has a cell to spare, List and Tree return
	// on an empty interior before adapt runs, and Table always passes a track
	// of scrollbarW cells. Probing it shows the painter writing one cell to the
	// LEFT of the track's origin (Right()-1 on an empty rect), which lands on
	// real buffer cells whenever the origin is not the buffer's edge — the
	// frame is safe here because the case is unreachable through every call
	// site, NOT because SetCell discards the write. Pinning the placement would
	// turn an unreachable wart into a contract a hardening change could not
	// make. Tightening the guard to track.Empty() would close it.
	buf := filled(10, 10, 'Z')
	paintScrollbarThumb(buf, buffer.Rect{X: 2, Y: 1, W: 0, H: 4}, 2, 5, 2, 6, buffer.Style{}, thumbRune)
}
