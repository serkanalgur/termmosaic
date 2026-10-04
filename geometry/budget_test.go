package geometry

import "testing"

// The tests below are ADR 0007's own list, by the names the ADR gives them,
// because these four identifiers are shared vocabulary: two other widget sets
// were written against the same sentence and a behaviour change here is a change
// to all of them at once.

// TestClampCountNeverNegativeOrExceeds pins the two properties that make
// ClampCount safe to call on any rect a layout can produce, including the
// degenerate ones. A negative n and a negative available are both reachable: a
// clipped rect can be empty on one axis only, and a detached terminal reports 0.
func TestClampCountNeverNegativeOrExceeds(t *testing.T) {
	cases := []struct{ n, available, want int }{
		{10, 3, 3},
		{3, 10, 3},
		{10, 10, 10},
		{0, 10, 0},
		{10, 0, 0},
		{0, 0, 0},
		{10, -1, 0},
		{-1, 10, 0},
		{-1, -1, 0},
	}
	for _, c := range cases {
		if got := ClampCount(c.n, c.available); got != c.want {
			t.Errorf("ClampCount(%d, %d) = %d, want %d", c.n, c.available, got, c.want)
		}
	}

	// The invariant itself, over a sweep that includes every negative on both
	// sides, because "never negative, never more than n" is the contract and the
	// table above is only nine samples of it. A negative n means no content, and
	// the only valid answer to it is exactly zero.
	for n := -3; n <= 12; n++ {
		for available := -3; available <= 12; available++ {
			got := ClampCount(n, available)
			if got < 0 {
				t.Fatalf("ClampCount(%d, %d) = %d, which is negative", n, available, got)
			}
			if n < 0 {
				if got != 0 {
					t.Fatalf("ClampCount(%d, %d) = %d, want 0: a negative count is no content", n, available, got)
				}
				continue
			}
			if got > n {
				t.Fatalf("ClampCount(%d, %d) = %d, which is more than the content", n, available, got)
			}
		}
	}
}

// TestBudgetDropsLowestPriorityFirst is the ordering rule, read as a sweep: as
// the budget shrinks, regions leave strictly lowest priority first, and nothing
// leaves while there is still room for it.
//
// The fixture interleaves the priorities in declaration order — low, normal, high,
// normal, low — so "declaration order" and "priority order" are visibly different
// here, and a bug that used one for the other cannot pass.
func TestBudgetDropsLowestPriorityFirst(t *testing.T) {
	regions := []Region{
		{Size: 3, Prio: PrioNormal}, // 0 metrics
		{Size: 2, Prio: PrioLow},    // 1 sparkline
		{Size: 3, Prio: PrioHigh},   // 2 identity
		{Size: 4, Prio: PrioNormal}, // 3 table
		{Size: 2, Prio: PrioLow},    // 4 footnote
	}

	cases := []struct {
		available int
		want      []bool
	}{
		{14, []bool{true, true, true, true, true}},
		{13, []bool{true, true, true, true, false}},
		{11, []bool{true, false, true, true, false}},
		// The sparkline survives at 9 because the table does not fit and therefore
		// does not consume the cells: a dropped region frees its budget for a
		// later one at the same priority.
		{9, []bool{true, true, true, false, false}},
		{6, []bool{true, false, true, false, false}},
		{4, []bool{false, false, true, false, false}},
		// PrioHigh fits exactly in three cells, so it is still shown at 3 and only
		// goes below that — it is dropped LAST among the budgetable regions.
		{3, []bool{false, false, true, false, false}},
		// A two-cell PrioLow region still fits a two-cell budget, which is the
		// point: Budget reports what FITS, not what is most important.
		{2, []bool{false, true, false, false, false}},
		{1, []bool{false, false, false, false, false}},
	}
	for _, c := range cases {
		if got := Budget(regions, c.available); !equalBools(got, c.want) {
			t.Errorf("Budget(_, %d) = %v, want %v", c.available, got, c.want)
		}
	}

	// With room for everything, nothing is dropped at all — including the regions
	// a smaller budget drops — so Budget is a function of the budget and not of any
	// cumulative history of previous calls.
	if got := Budget(regions, 40); !equalBools(got, []bool{true, true, true, true, true}) {
		t.Errorf("Budget(_, 40) = %v, want all kept", got)
	}
}

// TestBudgetKeepsDeclarationOrderWithinAPriority is what "never a coin flip"
// means: with equal sizes and equal priorities, the earlier region is the one
// that survives.
func TestBudgetKeepsDeclarationOrderWithinAPriority(t *testing.T) {
	regions := []Region{
		{Size: 4, Prio: PrioNormal},
		{Size: 4, Prio: PrioNormal},
		{Size: 4, Prio: PrioNormal},
		{Size: 4, Prio: PrioNormal},
	}
	want := []bool{true, true, false, false}

	// Repeated because the failure mode this test exists to catch is a map
	// iteration or a sort that happens to agree with declaration order on the
	// first run.
	for run := 0; run < 32; run++ {
		if got := Budget(regions, 8); !equalBools(got, want) {
			t.Fatalf("run %d: Budget(_, 8) = %v, want %v", run, got, want)
		}
	}

	// Mixed priorities at the same size: declaration order is the tiebreak only
	// WITHIN a priority, so the high one is kept even though it is declared third.
	mixed := []Region{
		{Size: 5, Prio: PrioNormal},
		{Size: 5, Prio: PrioNormal},
		{Size: 5, Prio: PrioHigh},
	}
	if got := Budget(mixed, 5); !equalBools(got, []bool{false, false, true}) {
		t.Errorf("Budget(mixed, 5) = %v, want only the PrioHigh region kept", got)
	}
}

// TestBudgetNeverDropsPrioAlways is the rule with the most teeth, and its second
// half is the one worth stating loudly: when PrioAlways alone overflows, Budget
// reports EVERY region as kept rather than dropping one to look busy. Dropping
// would not make the total fit, so the honest answer is "all of it is wanted and
// the caller must clip" — which is exactly what ADR 0007 §4 tells the caller to
// do.
func TestBudgetNeverDropsPrioAlways(t *testing.T) {
	regions := []Region{
		{Size: 2, Prio: PrioLow},
		{Size: 3, Prio: PrioAlways}, // status bar
		{Size: 4, Prio: PrioNormal},
		{Size: 1, Prio: PrioAlways}, // clock
	}
	always := []int{0, 1}

	for _, i := range always {
		if got := Budget(regions, 6); !got[i] {
			t.Errorf("region %d is PrioAlways and was dropped at a budget that fits it", i)
		}
	}
	// 4 + 1 exactly fills 5, so the normal region is what goes.
	if got := Budget(regions, 5); !equalBools(got, []bool{false, true, false, true}) {
		t.Errorf("Budget(_, 5) = %v, want only the two PrioAlways regions kept", got)
	}

	// The overflow case. Both PrioAlways regions total 4 and the budget is 3, so
	// dropping any budgetable region cannot bring the total under budget.
	overflow := Budget(regions, 3)
	for i, kept := range overflow {
		if !kept {
			t.Errorf("region %d reported dropped while PrioAlways alone overflows the budget; "+
				"Budget must never drop a region when dropping it would not help", i)
		}
	}
	if got := Budget(regions, 0); !equalBools(got, []bool{true, true, true, true}) {
		t.Errorf("Budget(_, 0) = %v, want all kept at a zero budget", got)
	}
	// A negative budget is no space, which is a real state and not an error.
	if got := Budget(regions, -5); !equalBools(got, []bool{true, true, true, true}) {
		t.Errorf("Budget(_, -5) = %v, want all kept", got)
	}
}

// TestBudgetEdgeCases covers the inputs the ADR does not spell out, and states the
// choices it leaves to the implementation so a caller can rely on them.
func TestBudgetEdgeCases(t *testing.T) {
	// Nil and empty both return an empty NON-NIL slice, so a caller can compare
	// the result against len(regions) without a nil check.
	for _, regions := range [][]Region{nil, {}} {
		got := Budget(regions, 10)
		if got == nil {
			t.Errorf("Budget(%v, 10) = nil, want an empty non-nil slice", regions)
		}
		if len(got) != 0 {
			t.Errorf("Budget(%v, 10) has %d entries, want 0", regions, len(got))
		}
	}

	// A negative Size is zero cells, so it always fits and is never the reason a
	// region is dropped.
	negative := []Region{
		{Size: -5, Prio: PrioLow},
		{Size: 10, Prio: PrioLow},
	}
	if got := Budget(negative, 10); !equalBools(got, []bool{true, true}) {
		t.Errorf("Budget(negative, 10) = %v, want both kept: a negative size is zero cells", got)
	}

	// A zero-size region is kept even when nothing else fits, because showing it
	// costs nothing.
	if got := Budget([]Region{{Size: 0, Prio: PrioLow}}, 0); !equalBools(got, []bool{true}) {
		t.Errorf("Budget(zeroSize, 0) = %v, want kept", got)
	}

	// A priority outside the four defined values is treated as PrioAlways rather
	// than skipped, so an invented rank cannot silently blank content.
	unknown := []Region{{Size: 40, Prio: Priority(200)}}
	if got := Budget(unknown, 3); !equalBools(got, []bool{true}) {
		t.Errorf("Budget(unknownPrio, 3) = %v, want kept: an out-of-range priority ranks with PrioAlways", got)
	}
}

// TestBudgetResultIsNewlyAllocated pins the caching obligation from the other
// side: the caller OWNS the returned slice and may keep it, because Budget does
// not reuse a buffer across calls. A widget caches this result against its rect
// (ADR 0007 §3), and that cache is only correct if Budget never hands back the
// same array twice.
func TestBudgetResultIsNewlyAllocated(t *testing.T) {
	regions := []Region{{Size: 2, Prio: PrioNormal}, {Size: 2, Prio: PrioNormal}}

	first := Budget(regions, 4)
	first[0] = false
	second := Budget(regions, 2)

	if !second[0] {
		t.Error("Budget returned aliased storage; a cached result would be corrupted by the next call")
	}
	if !equalBools(first, []bool{false, true}) {
		t.Error("mutating the first result changed it retroactively")
	}
}

// equalBools compares two results and reports a length mismatch as a difference,
// because a caller indexing by region position must never get a panic from a
// budget function.
func equalBools(got, want []bool) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
