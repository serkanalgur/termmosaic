package geometry

import "testing"

// Rect's geometry lives in this package but is tested through the buffer alias
// where the dirty-rect coalescer exercises it. These tests pin the operations
// themselves, since layout depends on them being right in isolation.
func TestRectGeometry(t *testing.T) {
	r := Rect{X: 1, Y: 2, W: 3, H: 4}
	if r.Right() != 4 || r.Bottom() != 6 {
		t.Errorf("edges = %d,%d want 4,6", r.Right(), r.Bottom())
	}
	if !r.Contains(1, 2) || r.Contains(4, 2) || r.Contains(1, 6) {
		t.Error("Contains is wrong")
	}
	if (Rect{}).Empty() != true || r.Empty() {
		t.Error("Empty is wrong")
	}
	if got := r.Inset(1); got != (Rect{X: 2, Y: 3, W: 1, H: 2}) {
		t.Errorf("Inset(1) = %+v", got)
	}
	if got := r.Inset(10); !got.Empty() {
		t.Errorf("Inset past the edge should be empty, got %+v", got)
	}
	a := Rect{X: 0, Y: 0, W: 2, H: 2}
	if !a.Intersects(Rect{X: 1, Y: 1, W: 2, H: 2}) {
		t.Error("overlapping rects should intersect")
	}
	if a.Intersects(Rect{X: 2, Y: 0, W: 2, H: 2}) {
		t.Error("edge-adjacent rects share no cell")
	}
	if !a.Touches(Rect{X: 2, Y: 0, W: 2, H: 2}) {
		t.Error("touches should be true for edge adjacency")
	}
	if got := a.Union(Rect{X: 5, Y: 5, W: 1, H: 1}); got != (Rect{X: 0, Y: 0, W: 6, H: 6}) {
		t.Errorf("union = %+v", got)
	}
}
