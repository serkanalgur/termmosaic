// Package geometry holds TermMosaic's coordinate types: the rectangle and the
// size, in cell coordinates.
//
// It exists as its own leaf package for one reason: ADR 0004 requires the layout
// solver to be pure and to not import the buffer package, yet the solver needs
// to produce rectangles. Geometry is depended on by both buffer and layout and
// depends on neither, so the ADR's constraint holds without either package
// giving up a type.
package geometry

// Rect is an axis-aligned rectangle in cell coordinates. X and Y are the
// top-left corner and W and H the extent; a Rect with W or H <= 0 is empty and
// intersects nothing.
type Rect struct {
	X, Y, W, H int
}

// Empty reports whether the rectangle covers no cells.
func (r Rect) Empty() bool { return r.W <= 0 || r.H <= 0 }

// Right returns the exclusive right edge.
func (r Rect) Right() int { return r.X + r.W }

// Bottom returns the exclusive bottom edge.
func (r Rect) Bottom() int { return r.Y + r.H }

// Contains reports whether the cell (x, y) lies inside the rectangle.
func (r Rect) Contains(x, y int) bool {
	return x >= r.X && x < r.Right() && y >= r.Y && y < r.Bottom()
}

// Intersects reports whether the two rectangles share at least one cell.
func (r Rect) Intersects(o Rect) bool {
	return r.X < o.Right() && o.X < r.Right() && r.Y < o.Bottom() && o.Y < r.Bottom()
}

// Union returns the smallest rectangle covering both operands. Unlike Intersects it
// treats shared edges as mergeable, which is what dirty-rect coalescing wants.
func (r Rect) Union(o Rect) Rect {
	x0, y0 := min(r.X, o.X), min(r.Y, o.Y)
	x1, y1 := max(r.Right(), o.Right()), max(r.Bottom(), o.Bottom())
	return Rect{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}
}

// Clip constrains the rectangle to a w-by-h region, returning an empty
// rectangle if it lies entirely outside. The diff uses it to clamp
// invalidation regions to the grid.
func (r Rect) Clip(w, h int) Rect {
	if r.Empty() {
		return Rect{}
	}
	if r.X < 0 {
		r.W += r.X
		r.X = 0
	}
	if r.Y < 0 {
		r.H += r.Y
		r.Y = 0
	}
	r.W = min(r.W, w-r.X)
	r.H = min(r.H, h-r.Y)
	if r.W < 0 {
		r.W = 0
	}
	if r.H < 0 {
		r.H = 0
	}
	return r
}

// Inset returns the rectangle shrunk by n cells on every side, which is how a
// widget derives its content area from its bounds.
func (r Rect) Inset(n int) Rect {
	if r.Empty() {
		return r
	}
	r.X += n
	r.Y += n
	r.W -= 2 * n
	r.H -= 2 * n
	return r.Clip(maxInt, maxInt)
}

// maxInt stands in for an unbounded clip.
const maxInt = 1<<62 - 1

// Size is a terminal width and height in cells.
type Size struct {
	W, H int
}

// Touches reports whether two rectangles overlap or share an edge. The
// dirty-rect coalescer uses it, because two rectangles sharing a column are
// contiguous on screen and cheaper as one diff region than as two.
func (r Rect) Touches(o Rect) bool {
	return r.X <= o.X+o.W && o.X <= r.X+r.W &&
		r.Y <= o.Y+o.H && o.Y <= r.Y+r.H
}
