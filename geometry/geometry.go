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

// ClampCount returns how many content units of n fit in available cells.
//
// It never returns a negative number and never more than n. This one function IS
// "show 3 rows at small sizes and 10 at large ones": pass the full content height
// and the available height.
//
// It counts CELLS, not glyphs: a double-width rune occupies two cells, so a row
// budget computed here can be one row optimistic once wide characters are in
// play. That is a recorded limitation, not an oversight.
//
// A negative n counts as zero content and a negative available counts as no space,
// because both are reachable from a layout under overflow (ADR 0007 §1 rule 2
// clips rects, and a clipped rect can be empty on one axis only).
func ClampCount(n, available int) int {
	if n <= 0 || available <= 0 {
		return 0
	}
	if n > available {
		return available
	}
	return n
}

// Priority ranks a content region for responsive budgeting. A widget drops
// regions from the lowest priority up when its available space cannot show them
// all. The four values are a total order and are the only ones.
type Priority uint8

// The four priorities, lowest first. PrioLow is the zero value, so a Region
// built without naming a priority is dropped first; PrioNormal is the default in
// practice because it is what a widget should reach for, and PrioAlways is the
// one a caller opts into deliberately for content that must never disappear.
const (
	PrioLow    Priority = iota // dropped first
	PrioNormal                 // the default; dropped before PrioHigh
	PrioHigh                   // dropped last among budgetable regions
	PrioAlways                 // never dropped, whatever the budget
)

// Region is one budgetable chunk of a widget's content, measured along whichever
// axis the widget is laying out. Budget does not know or care which axis that is;
// the axis is the caller's business.
//
// A Region is a statement about content, not about space, so it is built once at
// construction and stored — it does not depend on the current rect. Only Budget's
// answer does.
type Region struct {
	// Size is how many cells this region occupies along the caller's axis.
	// Negative values are treated as zero.
	Size int
	// Prio is the region's drop priority.
	Prio Priority
}

// Budget reports which of regions fit in available cells, dropping from the
// lowest priority up until the total fits. The returned slice has one entry per
// region, in the same order: true means the region is shown.
//
// Rules, all of which are the contract:
//   - PrioAlways regions are never dropped and always count against the budget.
//   - Within one priority, regions are kept in declaration order: equal priority
//     means declaration order is the tiebreak, never a coin flip. A region that
//     fits is kept, and a region that does not is skipped without consuming the
//     cells a later same-priority region could still use.
//   - If PrioAlways regions alone exceed available, every region is reported
//     kept. The caller is then over budget and clips. Budget never panics and
//     never reports a region as dropped when dropping it would not help.
//   - The returned slice is newly allocated. Callers must CACHE it and recompute
//     only when the widget's rectangle changes (ADR 0007 §3); Draw cannot call
//     this per frame without adding an allocation to every one of them.
//
// A nil or empty regions returns an empty non-nil slice, so a caller may range
// over the result and compare it against len(regions) without a nil check. A
// negative available counts as zero: there is no space, which is a state a
// clipped or detached terminal reaches legitimately. A negative Size counts as
// zero for the same reason it does in ClampCount.
//
// Budget does not measure; it counts cells along the caller's axis, so it is
// subject to the same recorded limitation as ClampCount — a double-width rune
// eats two cells and a budget computed here can be one unit optimistic.
func Budget(regions []Region, available int) []bool {
	show := make([]bool, len(regions))
	if len(regions) == 0 {
		return show
	}
	if available < 0 {
		available = 0
	}

	// PrioAlways first, because it is the only class whose total can be checked
	// against the budget before any decision is made. A Priority outside the
	// four defined values is treated as PrioAlways rather than skipped: skipping
	// would report such a region as dropped by default, and dropping content
	// because a caller invented a rank is the one outcome Budget must not produce.
	var always int
	for i := range regions {
		if normalisePrio(regions[i].Prio) == PrioAlways {
			always += regionSize(regions[i].Size)
		}
	}
	if always > available {
		// Dropping anything would not bring the total under budget, so nothing is
		// dropped. The caller clips, which is what ADR 0007 §4 requires of it.
		for i := range show {
			show[i] = true
		}
		return show
	}

	remaining := available - always
	for i := range regions {
		if normalisePrio(regions[i].Prio) == PrioAlways {
			show[i] = true
		}
	}
	// High to low: each priority sees only what the ones above it left. Walking
	// the priorities as outer loops is what makes "dropped from the lowest up" and
	// "never spend a cell on something that will be dropped" the same statement.
	for prio := PrioHigh; ; prio-- {
		for i := range regions {
			if normalisePrio(regions[i].Prio) != prio {
				continue
			}
			n := regionSize(regions[i].Size)
			if n <= remaining {
				remaining -= n
				show[i] = true
			}
		}
		if prio == PrioLow {
			break
		}
	}
	return show
}

// normalisePrio folds a Priority onto the four defined values, so Budget is total
// over the uint8 range rather than only over the constants. Anything above
// PrioHigh ranks with PrioAlways.
func normalisePrio(p Priority) Priority {
	if p > PrioHigh {
		return PrioAlways
	}
	return p
}

// regionSize reads a Region's size with negative values treated as zero, which
// is what makes a subtraction below safe from underflowing a cell count.
func regionSize(n int) int {
	if n < 0 {
		return 0
	}
	return n
}

// Touches reports whether two rectangles overlap or share an edge. The
// dirty-rect coalescer uses it, because two rectangles sharing a column are
// contiguous on screen and cheaper as one diff region than as two.
func (r Rect) Touches(o Rect) bool {
	return r.X <= o.X+o.W && o.X <= r.X+r.W &&
		r.Y <= o.Y+o.H && o.Y <= r.Y+r.H
}
