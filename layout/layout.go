// Package layout implements TermMosaic's constraint-based layout solver.
//
// ADR 0004 chose constraint-based layout with our own solver over vendored Yoga
// for a packaging reason that is worth restating here because it constrains the
// code: Yoga is cgo, and cgo breaks CGO_ENABLED=0 cross-compilation, which is
// how most Go users build release binaries. The solver therefore must stay pure
// Go, and — because it must not drag the buffer package in with it — it must not
// import it. It is a pure function over ints, exhaustively testable in CI with
// no terminal, which is the whole reason the choice was defensible.
//
// # Resolution order, and the sharp edge
//
// ADR 0004 records that Fill is order-sensitive as its highest-likelihood
// usability complaint, so the rule is stated precisely here and covered by tests:
//
//  1. spacing is reserved first: (len-1) * Spacing cells come off the top.
//  2. every non-Fill constraint resolves, in declaration order, against the
//     remaining space. Percentage and Ratio see the post-spacing space.
//  3. whatever is left over — and nothing else — is shared out among the Fill
//     constraints in proportion to their weights, by largest remainder.
//
// The consequence, stated plainly because it surprises people: a Fill that
// appears BEFORE a Length in the declaration order behaves identically to one
// after it, because Fill only ever sees what is left after every fixed
// constraint has been subtracted. Order therefore does not matter for Fill, and
// that is a deliberate simplification over tmux's ordering-sensitive rule. What
// does matter is that Fill gets nothing when the fixed constraints already
// exceed the available space.
package layout

import (
	"sort"

	"github.com/serkanalgur/termmosaic/geometry"
)

// Direction is the axis a Layout arranges its children along.
type Direction int

// Layout directions.
const (
	// Horizontal arranges children left to right, splitting the available
	// width.
	Horizontal Direction = iota
	// Vertical arranges children top to bottom, splitting the available
	// height.
	Vertical
)

// String returns the direction's name.
func (d Direction) String() string {
	switch d {
	case Horizontal:
		return "horizontal"
	case Vertical:
		return "vertical"
	default:
		return "unknown"
	}
}

// Layout is a direction, a list of constraints, and the spacing between
// children.
//
// One constraint per child, in order. A Layout with no constraints resolves to
// no sizes; a Layout with more constraints than available space is legal and
// resolves to overlapping sizes, which the caller is expected to notice.
type Layout struct {
	// Direction is the axis to split.
	Direction Direction
	// Constraint holds one constraint per child, in declaration order.
	Constraint []Constraint
	// Spacing is the number of cells between adjacent children. Negative values
	// are treated as zero.
	Spacing int
}

// Constraint is one child's sizing rule.
//
// The interface is closed: apply is unexported, so only the constructors in this
// package produce a Constraint. That is intentional — ADR 0004 lists
// "web-developer expectations" as an open risk and Layered sugar (Flex, Grid) as
// the escape hatch, and a closed set is what keeps the solver's semantics
// predictable enough to document.
type Constraint interface {
	// apply returns this constraint's size given the space available to the
	// whole group and the group's full constraint list.
	apply(available int, constraints []Constraint) int
	// String describes the constraint, for diagnostics and layout debugging.
	String() string
}

// kind tags each concrete constraint type.
type kind uint8

// Constraint kinds.
const (
	kindLength kind = iota
	kindMin
	kindMax
	kindPercentage
	kindRatio
	kindFill
)

type sized struct {
	kind kind
	n    int // Length/Min/Max/Fill(weight) value, or percentage
	d    int // Ratio denominator
}

func (c sized) String() string {
	switch c.kind {
	case kindLength:
		return "Length(" + itoa(c.n) + ")"
	case kindMin:
		return "Min(" + itoa(c.n) + ")"
	case kindMax:
		return "Max(" + itoa(c.n) + ")"
	case kindPercentage:
		return "Percentage(" + itoa(c.n) + ")"
	case kindRatio:
		return "Ratio(" + itoa(c.n) + "," + itoa(c.d) + ")"
	case kindFill:
		return "Fill(" + itoa(c.n) + ")"
	default:
		return "unknown"
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
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

// apply returns the constraint's share of available space.
//
// fill returns 0 here: Fill's size depends on the other constraints, which
// Solve computes, so the measure pass cannot resolve it and must not try.
func (c sized) apply(available int, _ []Constraint) int {
	if available < 0 {
		available = 0
	}
	var v int
	switch c.kind {
	case kindLength:
		v = c.n
	case kindMin:
		// A minimum cannot exceed what there is: "at least n" of a space
		// smaller than n means "all of it". Without this clamp, Min(100) in
		// 50 cells reports 100 and silently manufactures 50 cells that do not
		// exist.
		if c.n < available {
			v = c.n
		} else {
			v = available
		}
	case kindMax:
		if c.n < available {
			v = c.n
		} else {
			v = available
		}
	case kindPercentage:
		v = available * c.n / 100
	case kindRatio:
		if c.d == 0 {
			v = 0
		} else {
			v = available * c.n / c.d
		}
	default:
		v = 0
	}
	if v < 0 {
		return 0
	}
	return v
}

// Length returns a fixed-size constraint of n cells.
func Length(n int) Constraint {
	if n < 0 {
		n = 0
	}
	return sized{kind: kindLength, n: n}
}

// Min returns a constraint of at least n cells, and never more than the space
// actually available.
//
// Min behaves exactly like Length inside a Layout: a minimum that is not
// competing with a Fill has nothing to grow into. It differs in that it clamps
// to the available space rather than overflowing it, so Min(100) Max(3) in 50
// cells is a clamp to 3, not a demand for 100.
func Min(n int) Constraint {
	if n < 0 {
		n = 0
	}
	return sized{kind: kindMin, n: n}
}

// Max returns a constraint of at most n cells, clamped to the space available.
func Max(n int) Constraint {
	if n < 0 {
		n = 0
	}
	return sized{kind: kindMax, n: n}
}

// Percentage returns a constraint of p percent of the space available to the
// group, after spacing is reserved. Values outside 0-100 are clamped.
func Percentage(p int) Constraint {
	if p < 0 {
		p = 0
	}
	if p > 100 {
		p = 100
	}
	return sized{kind: kindPercentage, n: p}
}

// Ratio returns a constraint of n/d of the space available to the group. A zero
// denominator resolves to zero rather than panicking, because a layout that
// divides by a variable cell count is a normal thing to write and a TUI that
// panics on it is unusable.
func Ratio(n, d int) Constraint {
	return sized{kind: kindRatio, n: n, d: d}
}

// Fill returns a constraint that consumes space left over by every other
// constraint, shared in proportion to weight among all Fill constraints.
//
// A weight of 0 or less is treated as 1, so Fill() still means "share equally"
// rather than "claim nothing", which is the less surprising reading.
func Fill(weight int) Constraint {
	if weight < 1 {
		weight = 1
	}
	return sized{kind: kindFill, n: weight}
}

// Split is a convenience for the common all-Fill case: n children sharing the
// space equally.
func Split(n int) []Constraint {
	if n < 0 {
		n = 0
	}
	out := make([]Constraint, n)
	for i := range out {
		out[i] = Fill(1)
	}
	return out
}

// Solve returns the size in cells of each constraint in cs, given the space
// available along the layout's axis.
//
// The returned slice has one entry per constraint and is newly allocated; Solve
// is not on the per-frame hot path, because layouts are computed when the tree
// changes rather than every frame (ADR 0003's dirty-rectangle model is what
// makes that affordable).
//
// # Overflow and underflow
//
// Both are defined, neither panics, and both are covered by tests:
//
//   - Overflow: the fixed constraints exceed the available space. Fill
//     constraints receive 0, the fixed constraints keep their sizes, and the
//     resulting sizes sum to MORE than the available space. The caller decides
//     whether to clip; Solve does not, because silently shrinking a pane to fit
//     would hide a layout bug that the application author needs to see.
//   - Underflow: space is left over and there is no Fill constraint to take it.
//     The leftover is simply not assigned. Solve does not distribute it, because
//     every rule for doing so would be a surprise to someone.
//
// A negative available size is treated as zero.
func Solve(d Direction, cs []Constraint, spacing, available int) []int {
	if available < 0 {
		available = 0
	}
	if spacing < 0 {
		spacing = 0
	}
	n := len(cs)
	out := make([]int, n)
	if n == 0 {
		return out
	}

	// 1. Reserve spacing. An over-spaced layout reserves what it can and lets
	//    the fixed constraints resolve against zero rather than a negative.
	gaps := spacing * (n - 1)
	space := available - gaps
	if space < 0 {
		space = 0
	}

	// 2. Measure every non-Fill constraint, in declaration order.
	used := 0
	totalWeight := 0
	for i, c := range cs {
		if c == nil {
			continue
		}
		if c.(sized).kind == kindFill {
			totalWeight += c.(sized).n
			out[i] = 0
			continue
		}
		s := c.apply(space, cs)
		out[i] = s
		used += s
	}

	// 3. Hand what is left to the Fills, by weight, by largest remainder.
	//
	//    left can be negative, which is the overflow case: Fill gets nothing.
	left := space - used
	if left < 0 {
		left = 0
	}
	if totalWeight == 0 {
		return out
	}
	distribute(out, cs, left, totalWeight)
	return out
}

// distribute shares left cells among the Fill constraints in proportion to
// their weights, using largest remainder so the shares sum to exactly left.
//
// Proportional division by integer arithmetic truncates: three Fills over 100
// cells want 33.33 each, and naive truncation gives 33, 33, 33 and silently
// drops a cell — which shows up as a one-column gap at the right edge. Largest
// remainder hands the leftover cells to the shares whose fractional part was
// largest, so the total is exact.
func distribute(out []int, cs []Constraint, left, totalWeight int) {
	// Remainders are collected in a small fixed array for the overwhelmingly
	// common case of a handful of children, and fall back to a heap slice
	// beyond that.
	var buf [8]remainder
	var extra []remainder
	if countFills(cs) <= len(buf) {
		extra = buf[:0]
	} else {
		extra = make([]remainder, 0, countFills(cs))
	}

	assigned := 0
	for i, c := range cs {
		if c == nil || c.(sized).kind != kindFill {
			continue
		}
		share := left * c.(sized).n
		out[i] = share / totalWeight
		assigned += out[i]
		extra = append(extra, remainder{idx: i, frac: share % totalWeight})
	}
	leftover := left - assigned
	if leftover <= 0 {
		return
	}

	// Sort by descending fractional part, ties broken by declaration order so
	// the result is deterministic and independent of sort stability.
	sort.Slice(extra, func(a, b int) bool {
		if extra[a].frac != extra[b].frac {
			return extra[a].frac > extra[b].frac
		}
		return extra[a].idx < extra[b].idx
	})
	for _, e := range extra {
		if leftover == 0 {
			break
		}
		out[e.idx]++
		leftover--
	}
}

// remainder is one Fill's claim on a leftover cell.
type remainder struct {
	idx  int
	frac int
}

// countFills returns how many constraints are Fill.
func countFills(cs []Constraint) int {
	n := 0
	for _, c := range cs {
		if c != nil && c.(sized).kind == kindFill {
			n++
		}
	}
	return n
}

// Offset returns the start position of child i within a run of the given sizes
// with the given spacing, measured from the start of the axis.
//
// It exists because Solve returns sizes and the caller needs positions, and
// computing them in one place keeps the behaviour consistent: sizes that
// overflow the axis are still positioned, so the overflow stays visible instead
// of being clipped away.
func Offset(sizes []int, spacing, i int) int {
	if i <= 0 {
		return 0
	}
	off := 0
	for j := 0; j < i && j < len(sizes); j++ {
		off += sizes[j] + spacing
	}
	return off
}

// Rect returns the rectangle child i occupies within r, given the layout's
// resolved sizes and direction.
//
// This is the composition rule ADR 0004 specifies: nesting is not a new engine,
// it is running Solve on a sub-rectangle.
func Rect(r geometry.Rect, d Direction, sizes []int, spacing, i int) geometry.Rect {
	if i < 0 || i >= len(sizes) {
		return geometry.Rect{}
	}
	pos := Offset(sizes, spacing, i)
	if d == Horizontal {
		return geometry.Rect{X: r.X + pos, Y: r.Y, W: sizes[i], H: r.H}
	}
	return geometry.Rect{X: r.X, Y: r.Y + pos, W: r.W, H: sizes[i]}
}

// SolveLayout returns the rectangle child i occupies within r under l, given
// available cells along the layout's axis. It returns an empty rectangle if i
// is out of range.
func SolveLayout(l Layout, r geometry.Rect, available, i int) geometry.Rect {
	sizes := Solve(l.Direction, l.Constraint, l.Spacing, available)
	return Rect(r, l.Direction, sizes, l.Spacing, i)
}
