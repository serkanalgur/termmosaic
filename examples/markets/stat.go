package main

// statTile is one figure in the KPI row.
//
// # Why this is a widget and not three lines in the dashboard
//
// A KPI tile is a Block for the chrome plus a label, a value and a note, and the
// dashboard needs four of them. Written inline that is four copies of the same
// twelve lines, which is how the four drift apart — and a KPI row where one tile
// lost its arrow is exactly the accessibility failure this screen exists to avoid.
// So it is a widget: one Draw, four identical instances.
//
// # No widget API was added to make it
//
// The catalog has no stat tile, and this does not add one: it composes
// widgets/block and widgets/basic, both of which already exist, and the tile is
// local to this example. The composition boundary matters — the label and value
// are basic.Text widgets that paint their own background, so the tile gives them
// the panel background, or composing them would paint the block's own away.
//
// See the report: a stat tile is a primitive the catalog would benefit from, and
// this file is the shape it would take.

import (
	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/widgets/basic"
	"github.com/serkanalgur/termmosaic/widgets/block"
)

// statTile's internal geometry, in cells.
const (
	// tileLabelH is the label row. One row, always: a label that wraps into the
	// value row would make two tiles of different heights in a row of fixed height,
	// and a KPI row that is not level is a KPI row nobody can scan.
	tileLabelH = 1
)

// statTile is a bordered panel with a label, a value and a note.
type statTile struct {
	bounds buffer.Rect

	blk *block.Block

	label *basic.Text
	value *basic.Text
	note  *basic.Text

	// t is the tile's current text, held so that set can tell a real change from
	// a re-render. A tile that rewrote its strings on every frame would allocate
	// on the frame path, which is the one thing this file exists to avoid.
	t tileText
	// valid distinguishes a tile that has never been set from one set to the zero
	// value, which are the same value and different answers.
	valid bool
}

// newStatTile returns a tile showing the given label and nothing else, which is
// the state a freshly constructed dashboard is in.
func newStatTile(label string) *statTile {
	s := &statTile{
		blk:   block.New(buffer.Rect{}),
		label: basic.NewTextString(buffer.Rect{}, label, stMuted),
		value: basic.NewTextString(buffer.Rect{}, "", stFigure),
		note:  basic.NewTextString(buffer.Rect{}, "", stMuted),
	}
	s.blk.SetBorder(buffer.BorderPlain)
	s.blk.SetBorderStyle(stEdge)
	s.blk.SetBackground(stPanel)
	// The three texts live inside the block, so they carry the block's background.
	// Without this each would paint the panel colour away in its own rectangle,
	// which is the documented cost of the repaint-everything rule.
	s.label.SetBackground(stPanel)
	s.value.SetBackground(stPanel)
	s.note.SetBackground(stPanel)
	return s
}

// set replaces the tile's text, doing nothing when it is unchanged.
//
// The early return is the whole of the allocation story: SetSpans copies, so a
// set on every frame would put an allocation on the frame path, and the frame path
// is where this example's zero-allocation claim lives.
func (s *statTile) set(t tileText) {
	if s.valid && s.t == t {
		return
	}
	s.t = t
	s.valid = true
	s.label.SetText(t.label, stMuted)
	// The value's style carries the direction, and the note carries the arrow —
	// so a reader with no colour perception reads the arrow, and a reader with
	// colour reads both.
	s.value.SetText(t.value, changeStyle(t.dir))
	s.note.SetText(t.note, stMuted)
}

// Bounds returns the tile's rectangle, safe to call before the first Draw.
func (s *statTile) Bounds() buffer.Rect { return s.bounds }

// SetBounds sets the tile's rectangle. It must be current before the next Draw.
func (s *statTile) SetBounds(r buffer.Rect) { s.bounds = r }

// MinSize returns the smallest rectangle in which the tile shows all three of its
// rows. It includes the chrome, which is the convention termmosaic.Minimizable
// pins for the whole catalog.
func (s *statTile) MinSize() buffer.Size {
	return buffer.Size{W: kpiTileW, H: kpiH}
}

// Invalidate satisfies termmosaic.Widget. The tile repaints its whole rectangle
// every frame, so there is nothing finer-grained to mark.
func (s *statTile) Invalidate() {}

// Handle satisfies termmosaic.Widget. A KPI tile is a label, and a key aimed at a
// label belongs to whatever the label describes.
func (s *statTile) Handle(termmosaic.Event) bool { return false }

// Draw paints the chrome and the three rows.
//
// It allocates nothing: the interior is laid out with integer arithmetic, and the
// three texts truncate through their own caches. The whole rect is repainted first
// by the Block, which is how ADR 0007 §1 rule 3 is satisfied for this widget.
func (s *statTile) Draw(buf *buffer.Buffer) {
	r := s.bounds
	if r.Empty() {
		return
	}
	s.blk.SetBounds(r)
	s.blk.Draw(buf)

	in := s.blk.Interior()
	if in.Empty() {
		return
	}
	// The rows are carved off the interior top-down, and whatever is left is the
	// note's. A tile one row too short shows its label and value and clips the
	// note rather than blanking itself, which is ADR 0007 §4's rule: losing content
	// is worse than losing the indication that content was lost.
	labelRow := buffer.Rect{X: in.X, Y: in.Y, W: in.W, H: tileLabelH}
	s.label.SetBounds(labelRow)
	s.label.Draw(buf)

	y := in.Y + tileLabelH
	if y >= in.Bottom() {
		return
	}
	valueRow := buffer.Rect{X: in.X, Y: y, W: in.W, H: 1}
	s.value.SetBounds(valueRow)
	s.value.Draw(buf)

	y++
	if y >= in.Bottom() {
		return
	}
	noteRow := buffer.Rect{X: in.X, Y: y, W: in.W, H: geometryClamp(in.Bottom() - y)}
	s.note.SetBounds(noteRow)
	s.note.Draw(buf)
}

// geometryClamp returns n as a height, floored at one cell.
//
// A basic.Text with a zero-height rect draws its background and nothing else,
// which is correct; this exists so the note row is never negative, which would be
// a rectangle the buffer's bounds check would reject and which reads as a bug in
// the tile rather than as a clip.
func geometryClamp(n int) int {
	if n < 1 {
		return 1
	}
	return n
}

var (
	_ termmosaic.Widget      = (*statTile)(nil)
	_ termmosaic.Minimizable = (*statTile)(nil)
)
