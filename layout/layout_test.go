package layout

import (
	"testing"

	"github.com/serkanalgur/termmosaic/geometry"
)

func sum(s []int) int {
	t := 0
	for _, v := range s {
		t += v
	}
	return t
}

func eq(t *testing.T, got []int, want ...int) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %v (%d sizes), want %v (%d sizes)", got, len(got), want, len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestLengthAndSpacing(t *testing.T) {
	// 3 fixed children, 1 cell of spacing, 20 available.
	got := Solve(Horizontal, []Constraint{Length(5), Length(5), Length(5)}, 1, 20)
	eq(t, got, 5, 5, 5)

	got = Solve(Vertical, []Constraint{Length(3), Length(4)}, 2, 20)
	eq(t, got, 3, 4)
}

func TestFillTakesTheRemainder(t *testing.T) {
	got := Solve(Horizontal, []Constraint{Length(10), Fill(1), Fill(1)}, 0, 50)
	eq(t, got, 10, 20, 20)
}

func TestFillByWeight(t *testing.T) {
	got := Solve(Horizontal, []Constraint{Fill(3), Fill(1)}, 0, 40)
	eq(t, got, 30, 10)
}

func TestFillAfterSpacing(t *testing.T) {
	// 2 Fills, 1 cell of spacing, 21 available: 20 to share, 10 each.
	got := Solve(Horizontal, []Constraint{Fill(1), Fill(1)}, 1, 21)
	eq(t, got, 10, 10)
}

func TestFillLargestRemainderIsExact(t *testing.T) {
	// The classic truncation bug: 100 over three Fills must be 34/33/33, not
	// 33/33/33 with a cell silently dropped.
	got := Solve(Horizontal, Split(3), 0, 100)
	eq(t, got, 34, 33, 33)
	if sum(got) != 100 {
		t.Fatalf("shares sum to %d, want exactly 100", sum(got))
	}
}

func TestFillLargestRemainderUnevenWeights(t *testing.T) {
	// 100 cells over weights 1,1,1 -> 34/33/33 by declaration order.
	got := Solve(Horizontal, []Constraint{Fill(1), Fill(1), Fill(1)}, 0, 100)
	eq(t, got, 34, 33, 33)

	// Weights 5,3,2 over 100 -> 50,30,20 exactly, no remainder.
	got = Solve(Horizontal, []Constraint{Fill(5), Fill(3), Fill(2)}, 0, 100)
	eq(t, got, 50, 30, 20)
	if sum(got) != 100 {
		t.Fatalf("shares sum to %d, want 100", sum(got))
	}

	// 10 cells over weights 1,1: exact.
	got = Solve(Horizontal, []Constraint{Fill(1), Fill(1)}, 0, 10)
	eq(t, got, 5, 5)
}

func TestPercentage(t *testing.T) {
	got := Solve(Horizontal, []Constraint{Percentage(30), Fill(1)}, 0, 100)
	eq(t, got, 30, 70)

	// Percentage sees the post-spacing space.
	got = Solve(Horizontal, []Constraint{Percentage(50), Percentage(50)}, 0, 101)
	eq(t, got, 50, 50)

	// Out-of-range percentages are clamped, not a panic.
	got = Solve(Horizontal, []Constraint{Percentage(200)}, 0, 10)
	eq(t, got, 10)
	got = Solve(Horizontal, []Constraint{Percentage(-5)}, 0, 10)
	eq(t, got, 0)
}

func TestRatio(t *testing.T) {
	got := Solve(Horizontal, []Constraint{Ratio(1, 3), Ratio(2, 3)}, 0, 60)
	eq(t, got, 20, 40)

	// A zero denominator resolves to zero rather than panicking: a layout that
	// divides by a variable cell count is a normal thing to write.
	got = Solve(Horizontal, []Constraint{Ratio(1, 0), Fill(1)}, 0, 10)
	eq(t, got, 0, 10)
}

func TestMinAndMax(t *testing.T) {
	// Max clamps to the available space.
	got := Solve(Horizontal, []Constraint{Max(5), Max(100)}, 0, 10)
	eq(t, got, 5, 10)

	// Min behaves like Length when it has nothing to grow into.
	got = Solve(Horizontal, []Constraint{Min(3), Fill(1)}, 0, 10)
	eq(t, got, 3, 7)

	// Min then Max is a clamp on the space available.
	got = Solve(Horizontal, []Constraint{Min(100), Max(3)}, 0, 50)
	eq(t, got, 50, 3)
}

// --- overflow and underflow ---------------------------------------------------

func TestOverflowFixedConstraintsExceedAvailable(t *testing.T) {
	// 10 + 10 + 10 needs 20 but has 15. The documented behaviour is that Fill
	// gets nothing and the fixed constraints keep their sizes, so the caller
	// can SEE the overflow rather than having it silently clipped.
	got := Solve(Horizontal, []Constraint{Length(10), Length(10), Fill(1)}, 0, 15)
	eq(t, got, 10, 10, 0)
	if sum(got) != 20 {
		t.Errorf("sum = %d, want 20: overflow must remain visible", sum(got))
	}
}

func TestOverflowAllFixedNoFill(t *testing.T) {
	got := Solve(Horizontal, []Constraint{Length(8), Length(8)}, 0, 10)
	eq(t, got, 8, 8)
}

func TestSpacingOverflow(t *testing.T) {
	// Spacing alone exceeds the space: the group resolves against zero.
	got := Solve(Horizontal, []Constraint{Length(5), Length(5)}, 10, 15)
	eq(t, got, 5, 5)
}

func TestUnderflowLeftoverIsNotDistributed(t *testing.T) {
	// 40 cells available, only 10 used, and no Fill. The leftover is simply not
	// assigned; every distribution rule would surprise someone.
	got := Solve(Horizontal, []Constraint{Length(10)}, 0, 40)
	eq(t, got, 10)
}

func TestZeroAndNegativeAvailable(t *testing.T) {
	got := Solve(Horizontal, []Constraint{Length(5), Fill(1)}, 0, 0)
	eq(t, got, 5, 0)

	got = Solve(Horizontal, []Constraint{Fill(1), Fill(1)}, 0, -50)
	eq(t, got, 0, 0)
}

func TestNoConstraints(t *testing.T) {
	eq(t, Solve(Horizontal, nil, 3, 100))
	eq(t, Solve(Vertical, []Constraint{}, 3, 100))
}

// --- Fill ordering, the ADR's flagged sharp edge -----------------------------

func TestFillPositionInDeclarationOrderDoesNotMatter(t *testing.T) {
	// ADR 0004 flags Fill's order sensitivity as its top usability risk. This
	// implementation resolves every fixed constraint before any Fill, so a Fill
	// before or after a Length gets the same answer. That is a deliberate
	// simplification of tmux's rule, and this test is what makes it a
	// guarantee rather than an accident.
	a := Solve(Horizontal, []Constraint{Fill(1), Length(20)}, 0, 40)
	b := Solve(Horizontal, []Constraint{Length(20), Fill(1)}, 0, 40)
	eq(t, a, 20, 20)
	eq(t, b, 20, 20)
}

func TestNegativeSpacingIsTreatedAsZero(t *testing.T) {
	got := Solve(Horizontal, []Constraint{Length(5), Length(5)}, -4, 20)
	eq(t, got, 5, 5)
}

func TestNilConstraintIsTreatedAsZeroFill(t *testing.T) {
	got := Solve(Horizontal, []Constraint{Length(5), nil}, 0, 20)
	eq(t, got, 5, 0)
}

// --- positioning -------------------------------------------------------------

func TestOffset(t *testing.T) {
	sizes := []int{10, 20, 30}
	if got := Offset(sizes, 1, 0); got != 0 {
		t.Errorf("Offset(0) = %d, want 0", got)
	}
	if got := Offset(sizes, 1, 1); got != 11 {
		t.Errorf("Offset(1) = %d, want 11", got)
	}
	if got := Offset(sizes, 1, 2); got != 32 {
		t.Errorf("Offset(2) = %d, want 32", got)
	}
	if got := Offset(sizes, 1, -1); got != 0 {
		t.Errorf("Offset(-1) = %d, want 0", got)
	}
}

func TestRectPositionsChildren(t *testing.T) {
	r := geometry.Rect{X: 5, Y: 2, W: 40, H: 10}
	l := Layout{Direction: Horizontal, Constraint: []Constraint{Length(10), Fill(1)}, Spacing: 2}
	got := SolveLayout(l, r, 40, 0)
	want := geometry.Rect{X: 5, Y: 2, W: 10, H: 10}
	if got != want {
		t.Errorf("child 0 = %+v, want %+v", got, want)
	}
	got = SolveLayout(l, r, 40, 1)
	want = geometry.Rect{X: 17, Y: 2, W: 28, H: 10}
	if got != want {
		t.Errorf("child 1 = %+v, want %+v", got, want)
	}

	v := Layout{Direction: Vertical, Constraint: []Constraint{Length(3), Fill(1)}}
	got = SolveLayout(v, geometry.Rect{W: 20, H: 10}, 10, 1)
	want = geometry.Rect{W: 20, Y: 3, H: 7}
	if got != want {
		t.Errorf("vertical child 1 = %+v, want %+v", got, want)
	}
}

func TestRectOutOfRangeIsEmpty(t *testing.T) {
	if got := Rect(geometry.Rect{W: 10, H: 10}, Horizontal, []int{5}, 0, 3); !got.Empty() {
		t.Errorf("out-of-range child = %+v, want empty", got)
	}
}

func TestConstraintStrings(t *testing.T) {
	cases := []struct {
		c    Constraint
		want string
	}{
		{Length(20), "Length(20)"},
		{Min(10), "Min(10)"},
		{Max(5), "Max(5)"},
		{Percentage(30), "Percentage(30)"},
		{Ratio(1, 3), "Ratio(1,3)"},
		{Fill(1), "Fill(1)"},
		{Length(-4), "Length(0)"},
	}
	for _, tc := range cases {
		if got := tc.c.String(); got != tc.want {
			t.Errorf("got %q, want %q", got, tc.want)
		}
	}
}

func TestDirectionString(t *testing.T) {
	if Horizontal.String() != "horizontal" || Vertical.String() != "vertical" {
		t.Error("Direction.String is wrong")
	}
}

func TestSplit(t *testing.T) {
	// 43 available less 3 gaps leaves 40 for four equal Fills.
	got := Solve(Horizontal, Split(4), 1, 43)
	eq(t, got, 10, 10, 10, 10)
	if sum(got) != 40 {
		t.Errorf("sum = %d, want 40 (43 available less 3 gaps)", sum(got))
	}
	// An uneven split hands the leftover to the first Fill by largest
	// remainder, and the total is still exact.
	got = Solve(Horizontal, Split(4), 0, 44)
	eq(t, got, 11, 11, 11, 11)
	if sum(got) != 44 {
		t.Errorf("sum = %d, want exactly 44", sum(got))
	}
	if len(Split(-1)) != 0 {
		t.Error("Split of a negative count must be empty")
	}
}

// TestSolverIsPure is ADR 0004's claim that the solver is a pure function: the
// same inputs must always give the same outputs, with no hidden state and no
// dependence on call order.
func TestSolverIsPure(t *testing.T) {
	cs := []Constraint{Length(10), Fill(1), Fill(2), Percentage(25)}
	first := Solve(Horizontal, cs, 1, 60)
	for i := 0; i < 50; i++ {
		got := Solve(Horizontal, cs, 1, 60)
		for j := range got {
			if got[j] != first[j] {
				t.Fatalf("run %d differs: %v vs %v", i, got, first)
			}
		}
	}
}

func BenchmarkSolve(b *testing.B) {
	cs := []Constraint{Length(20), Fill(1), Fill(2), Percentage(10)}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = Solve(Horizontal, cs, 1, 120)
	}
}

func BenchmarkSolveManyFills(b *testing.B) {
	cs := make([]Constraint, 16)
	for i := range cs {
		cs[i] = Fill(1)
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = Solve(Vertical, cs, 0, 200)
	}
}
