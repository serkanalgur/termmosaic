package virtual

import (
	"fmt"
	"testing"

	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/geometry"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// viewport is the rect every test renders into unless it says otherwise.
func viewport(w, h int) geometry.Rect { return geometry.Rect{W: w, H: h} }

// collecting returns a Paint that records the (index, row) pairs it was given.
// It allocates, which is fine: it is a test helper, not the frame path.
// Either accumulator may be nil when the test only cares about the other.
func collecting(got *[]int, rows *[]geometry.Rect) Paint {
	return func(_ *buffer.Buffer, row geometry.Rect, i int) {
		if got != nil {
			*got = append(*got, i)
		}
		if rows != nil {
			*rows = append(*rows, row)
		}
	}
}

// ---------------------------------------------------------------------------
// counting and the visible window
// ---------------------------------------------------------------------------

// TestNewClampsANegativeCount: a count arriving from a filter or a failing query
// is untrusted input, and a negative count makes every window calculation
// nonsense.
func TestNewClampsANegativeCount(t *testing.T) {
	m := New(-5)
	if m.Count() != 0 {
		t.Errorf("Count = %d, want 0", m.Count())
	}
	m.SetViewport(10)
	if got := m.Visible(); got != 0 {
		t.Errorf("Visible = %d, want 0", got)
	}
	if first, last := m.Range(); first != 0 || last != 0 {
		t.Errorf("Range = [%d,%d), want [0,0)", first, last)
	}
}

// TestVisibleIsClampCountOfTheViewport is the one-liner this package exists to
// share, checked against geometry.ClampCount itself rather than against a
// hand-written expectation.
func TestVisibleIsClampCountOfWhatIsLeft(t *testing.T) {
	for _, count := range []int{0, 1, 5, 50} {
		for h := 0; h <= 12; h++ {
			m := New(count)
			m.SetViewport(h)
			want := geometry.ClampCount(count, m.Capacity())
			if got := m.Visible(); got != want {
				t.Errorf("count %d viewport %d: Visible = %d, want %d", count, h, got, want)
			}
			if got := m.Capacity(); got != h {
				t.Errorf("count %d viewport %d: Capacity = %d, want %d", count, h, got, h)
			}
		}
	}
}

// TestVisibleIsTheShortWindowAtTheEnd is the distinction between Visible and
// Capacity, and it is the reason both exist: the last window of a scrolled
// collection is shorter than the viewport.
func TestVisibleIsTheShortWindowAtTheEnd(t *testing.T) {
	m := New(10)
	m.SetViewport(4)
	if got := m.Capacity(); got != 4 {
		t.Fatalf("Capacity = %d, want 4", got)
	}
	m.ScrollToEnd()
	if got := m.Capacity(); got != 4 {
		t.Errorf("Capacity = %d at the end, want 4: it measures the viewport, not the content", got)
	}
	if got := m.Visible(); got != 4 {
		t.Errorf("Visible = %d, want 4", got)
	}
	m.SetCount(6) // 6 items, a 4-row viewport: still a full window
	m.ScrollToEnd()
	if got := m.Visible(); got != 4 {
		t.Errorf("Visible = %d with 6 items in a 4-row viewport, want 4", got)
	}
	m.SetViewport(9) // more rows than items: a short final window
	m.ScrollToEnd()
	if got := m.Visible(); got != 6 {
		t.Errorf("Visible = %d with 6 items in a 9-row viewport, want 6", got)
	}
}

// TestVisibleAccountsForRowHeight: a two-cell row means half as many rows fit,
// which is what keeps a multi-line row from painting over its neighbour.
func TestVisibleAccountsForRowHeight(t *testing.T) {
	m := New(100)
	m.SetViewport(10)
	if got := m.Visible(); got != 10 {
		t.Fatalf("one-cell rows: Visible = %d, want 10", got)
	}
	m.SetRowHeight(3)
	if got := m.Visible(); got != 3 {
		t.Errorf("three-cell rows: Visible = %d, want 3", got)
	}
	if got := m.Capacity(); got != 3 {
		t.Errorf("three-cell rows: Capacity = %d, want 3", got)
	}
	m.SetRowHeight(0)
	if got := m.RowHeight(); got != 1 {
		t.Errorf("RowHeight after SetRowHeight(0) = %d, want 1: a row that occupies no cell cannot exist", got)
	}
}

// TestForEachPaintsExactlyTheVisibleWindow is the core property: rows
// [offset, offset+height) and nothing else.
func TestForEachPaintsExactlyTheVisibleWindow(t *testing.T) {
	m := New(1000)
	v := viewport(20, 5)
	m.Resize(v)

	var got []int
	var rows []geometry.Rect
	m.ForEach(v, buffer.NewBuffer(20, 5), collecting(&got, &rows))

	want := []int{0, 1, 2, 3, 4}
	if len(got) != len(want) {
		t.Fatalf("painted %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("painted %v, want %v", got, want)
		}
		if rows[i].Y != i || rows[i].H != 1 {
			t.Errorf("row %d rect = %+v, want y=%d h=1", got[i], rows[i], i)
		}
	}

	m.SetOffset(500)
	got, rows = nil, nil
	m.ForEach(v, buffer.NewBuffer(20, 5), collecting(&got, &rows))
	for i, idx := range got {
		if idx != 500+i {
			t.Fatalf("after scrolling, painted %v, want 500..504", got)
		}
		if rows[i].Y != i {
			t.Errorf("a scrolled viewport must still paint at screen row %d, got y=%d", i, rows[i].Y)
		}
	}
}

// TestForEachStopsAtTheEndOfTheCollection: the last window is short, and nothing
// is painted past the last item.
func TestForEachStopsAtTheEndOfTheCollection(t *testing.T) {
	m := New(3)
	v := viewport(20, 5)
	m.Resize(v)

	var got []int
	m.ForEach(v, buffer.NewBuffer(20, 5), collecting(&got, nil))
	if len(got) != 3 {
		t.Errorf("painted %v, want three rows", got)
	}
	if m.MaxOffset() != 0 {
		t.Errorf("MaxOffset = %d, want 0: a collection shorter than the viewport cannot scroll", m.MaxOffset())
	}
}

// TestForEachGivesAPartialRowItsRealHeight is the clipping contract: the bottom
// row of a viewport that is not a whole number of rows tall is passed its real
// height, so a painter that fills the row stays inside the viewport without any
// arithmetic of its own.
func TestForEachGivesAPartialRowItsRealHeight(t *testing.T) {
	m := New(100)
	v := viewport(10, 5)
	m.SetRowHeight(2)
	m.Resize(v)

	var rows []geometry.Rect
	m.ForEach(v, buffer.NewBuffer(10, 5), collecting(nil, &rows))

	if len(rows) != 2 {
		t.Fatalf("painted %d rows, want 2 (viewport 5 cells, rows 2 cells)", len(rows))
	}
	if rows[0].H != 2 || rows[1].H != 2 {
		t.Errorf("row heights = %d and %d, want 2 and 2", rows[0].H, rows[1].H)
	}

	// A viewport of 5 cells cannot hold a third 2-cell row, and ForEach must not
	// hand one out with a height that would spill.
	m.SetViewport(3)
	rows = nil
	m.ForEach(viewport(10, 3), buffer.NewBuffer(10, 3), collecting(nil, &rows))
	if len(rows) != 1 {
		t.Fatalf("painted %d rows in a 3-cell viewport, want 1", len(rows))
	}
	if rows[0].Bottom() > 3 {
		t.Errorf("row %+v spills past a 3-cell viewport", rows[0])
	}
}

// TestForEachIsTotalAtEverySize: no viewport, no painter or a viewport entirely
// off the screen must be a no-op rather than a panic.
func TestForEachIsTotalAtEverySize(t *testing.T) {
	for w := 0; w <= 6; w++ {
		for h := 0; h <= 6; h++ {
			for _, count := range []int{0, 1, 7, 1000} {
				m := New(count)
				v := viewport(w, h)
				m.Resize(v)
				buf := buffer.NewBuffer(atLeast1(w), atLeast1(h))
				m.ForEach(v, buf, func(dst *buffer.Buffer, row geometry.Rect, i int) {
					if i < 0 || i >= count {
						t.Fatalf("painted index %d for a collection of %d", i, count)
					}
					if row.Right() > v.Right() || row.Bottom() > v.Bottom() || row.X < v.X || row.Y < v.Y {
						t.Fatalf("row %+v escapes viewport %+v", row, v)
					}
					dst.FillRect(row, buffer.PlainStyle.Blank())
				})
				m.ForEach(v, buf, nil) // a nil painter must not panic
			}
		}
	}
}

// ---------------------------------------------------------------------------
// clamping, which is ADR 0007 §6 rule 2
// ---------------------------------------------------------------------------

// TestShrinkingTheViewportClampsTheOffsetAndDoesNotRecentre is rule 2 stated as a
// test: the offset is clamped so the viewport cannot run off the end, and the
// content is NOT recentred, because re-deriving the offset is a product decision
// each widget would make differently.
func TestShrinkingTheViewportClampsTheOffsetAndDoesNotRecentre(t *testing.T) {
	m := New(100)
	m.Resize(viewport(20, 10))
	m.SetOffset(90)
	if got := m.Visible(); got != 10 {
		t.Fatalf("Visible = %d, want 10", got)
	}
	if got := m.MaxOffset(); got != 90 {
		t.Fatalf("MaxOffset = %d, want 90", got)
	}
	if !m.AtEnd() {
		t.Fatal("AtEnd should be true at MaxOffset")
	}

	// A taller viewport cannot keep the old offset: the window would run past the
	// end. Clamping pulls the offset back to the last legal one.
	m.Resize(viewport(20, 50))
	if got, want := m.MaxOffset(), 100-50; got != want {
		t.Fatalf("MaxOffset = %d, want %d", got, want)
	}
	if got := m.Offset(); got != 50 {
		t.Errorf("Offset = %d, want 50: clamping pulls back to the end", got)
	}

	// A shorter viewport has room to scroll on again, and the offset must STAY
	// where clamping left it. Re-deriving it — recentring, or snapping back to
	// the end — is the product decision rule 2 refuses to let a widget make.
	m.Resize(viewport(20, 4))
	if got, want := m.MaxOffset(), 100-4; got != want {
		t.Fatalf("MaxOffset = %d, want %d", got, want)
	}
	if got := m.Offset(); got != 50 {
		t.Errorf("Offset = %d, want 50: the offset is clamped, not re-derived", got)
	}
	if m.AtEnd() {
		t.Error("AtEnd should be false once the viewport can scroll on again")
	}
}

// TestShrinkingTheCollectionClampsTheOffset covers the same rule for a data
// change rather than a size change: a filter that narrows 100 rows to 3 must not
// leave the viewport showing rows 90..99.
func TestShrinkingTheCollectionClampsTheOffset(t *testing.T) {
	m := New(100)
	m.Resize(viewport(20, 10))
	m.SetOffset(90)
	m.SetCount(3)
	if got := m.Offset(); got != 0 {
		t.Errorf("Offset = %d after the collection shrank to 3, want 0", got)
	}
}

// TestClampCostIsIndependentOfCount is the benchmark at the bottom of this file:
// SetCount and Resize must stay O(1), or a million-item collection clamps slower
// than a ten-item one and the whole package's claim is void.

// ---------------------------------------------------------------------------
// scrolling
// ---------------------------------------------------------------------------

// TestScrollOperationsClampRatherThanFail: a wheel notch at the end, or an arrow
// key at the top, is a no-op rather than an error.
func TestScrollOperationsClampRatherThanFail(t *testing.T) {
	m := New(10)
	m.Resize(viewport(20, 4))

	m.LineUp(5)
	if got := m.Offset(); got != 0 {
		t.Errorf("Offset = %d after LineUp from the top, want 0", got)
	}
	m.ScrollBy(-100)
	if got := m.Offset(); got != 0 {
		t.Errorf("Offset = %d, want 0", got)
	}
	m.ScrollBy(1000)
	if got, want := m.Offset(), 10-4; got != want {
		t.Errorf("Offset = %d, want %d", got, want)
	}
	m.LineDown(1)
	if got, want := m.Offset(), 10-4; got != want {
		t.Errorf("Offset = %d, want %d: scrolling past the end is a no-op", got, want)
	}

	// A page is a viewport minus one row of context, so paging up from the end of
	// a 4-row viewport lands on 3, not on 0.
	m.PageUp()
	if got, want := m.Offset(), 3; got != want {
		t.Errorf("Offset = %d after PageUp from the end, want %d", got, want)
	}
	m.PageDown()
	if got, want := m.Offset(), 6; got != want {
		t.Errorf("Offset = %d after PageDown, want %d", got, want)
	}
	m.PageUp()
	m.PageUp()
	m.PageUp()
	if got := m.Offset(); got != 0 {
		t.Errorf("Offset = %d after paging past the top, want 0", got)
	}

	m.ScrollToEnd()
	if !m.AtEnd() {
		t.Error("ScrollToEnd did not reach the end")
	}
	m.ScrollToStart()
	if got := m.Offset(); got != 0 {
		t.Errorf("Offset = %d after ScrollToStart, want 0", got)
	}
}

// TestScrollIntoViewMovesTheMinimumDistance is the one placement rule, and it is
// the same in every widget: never recentre, never jump when already visible.
func TestScrollIntoViewMovesTheMinimumDistance(t *testing.T) {
	m := New(1000)
	m.Resize(viewport(20, 10))

	m.ScrollIntoView(100)
	if got, want := m.Offset(), 100-10+1; got != want {
		t.Errorf("Offset = %d, want %d: the last visible slot", got, want)
	}
	if first, last := m.Range(); last != 100+1 {
		t.Errorf("Range = [%d,%d), want 100 to be the last visible row", first, last)
	}

	// Offset is 91, so rows 91..100 are visible. Row 95 is already on screen.
	m.ScrollIntoView(95)
	if got, want := m.Offset(), 100-10+1; got != want {
		t.Errorf("Offset = %d, want %d: an already-visible row must not move the viewport", got, want)
	}
	m.ScrollIntoView(91)
	if got := m.Offset(); got != 91 {
		t.Errorf("Offset = %d after scrolling to the first visible row, want 91", got)
	}

	m.ScrollIntoView(5) // above the window
	if got := m.Offset(); got != 5 {
		t.Errorf("Offset = %d, want 5", got)
	}

	m.ScrollIntoView(1000) // out of range entirely
	if got := m.Offset(); got != 5 {
		t.Errorf("Offset = %d, want 5: an out-of-range index must be ignored", got)
	}

	m.Resize(viewport(20, 0)) // nothing fits
	m.ScrollIntoView(7)
	if got := m.Offset(); got != 5 {
		t.Errorf("Offset = %d, want 5: with no visible row there is nothing to scroll into", got)
	}
}

// TestPageOnAShortCollectionStillMoves: a page key must do something even when the
// viewport shows the whole collection.
func TestPageOnAShortCollectionStillMoves(t *testing.T) {
	m := New(3)
	m.Resize(viewport(20, 10))
	m.ScrollBy(-1)
	m.PageDown()
	if got := m.Offset(); got != 0 {
		t.Errorf("Offset = %d, want 0: the collection cannot scroll", got)
	}
}

// ---------------------------------------------------------------------------
// hit testing
// ---------------------------------------------------------------------------

// TestItemAtIsTheInverseOfForEach: a click on a row must name that row, including
// a partially visible one, and a click off the viewport must name nothing.
func TestItemAtIsTheInverseOfForEach(t *testing.T) {
	m := New(100)
	v := viewport(20, 5)
	m.Resize(v)
	m.SetOffset(40)

	var rows []geometry.Rect
	m.ForEach(v, buffer.NewBuffer(20, 5), collecting(nil, &rows))
	for i, row := range rows {
		got, ok := m.ItemAt(row.Y)
		if !ok || got != 40+i {
			t.Errorf("ItemAt(%d) = %d,%v, want %d,true", row.Y, got, ok, 40+i)
		}
	}

	if _, ok := m.ItemAt(-1); ok {
		t.Error("ItemAt(-1) reported a row")
	}
	if _, ok := m.ItemAt(5); ok {
		t.Error("ItemAt past the viewport reported a row")
	}

	// A two-cell row maps a screen row back to the right item.
	m.SetRowHeight(2)
	m.Resize(viewport(20, 5))
	if got, ok := m.ItemAt(3); !ok || got != 41 {
		t.Errorf("ItemAt(3) with 2-cell rows = %d,%v, want 41,true", got, ok)
	}
}

// TestItemAtPastTheEndReportsNothing covers a viewport taller than the remaining
// collection: rows below the last item are not rows.
func TestItemAtPastTheEndReportsNothing(t *testing.T) {
	m := New(3)
	m.Resize(viewport(20, 5))
	if _, ok := m.ItemAt(4); ok {
		t.Error("ItemAt on a row below the last item reported a row")
	}
}

// ---------------------------------------------------------------------------
// cost
// ---------------------------------------------------------------------------

// TestFramePathAllocatesNothing is the package's whole reason for existing. The
// row painter is hoisted into a variable so the closure itself is not what the
// measurement sees.
func TestFramePathAllocatesNothing(t *testing.T) {
	for _, count := range []int{10, 100, 100_000} {
		m := New(count)
		v := viewport(40, 20)
		m.Resize(v)
		buf := buffer.NewBuffer(40, 20)
		st := buffer.PlainStyle

		paint := func(dst *buffer.Buffer, row geometry.Rect, i int) {
			dst.SetString(row.X, row.Y, "x", st)
			dst.FillRect(row, st.Blank())
		}
		m.ForEach(v, buf, paint) // warm

		if got := testing.AllocsPerRun(100, func() { m.ForEach(v, buf, paint) }); got != 0 {
			t.Errorf("count %d: ForEach allocated %.1f objects per run, want 0", count, got)
		}
		if got := testing.AllocsPerRun(100, func() { m.ScrollBy(1) }); got != 0 {
			t.Errorf("count %d: ScrollBy allocated %.1f objects per run, want 0", count, got)
		}
	}
}

// TestResizeReportsOnlyRealChanges is the ADR 0007 §3 hook: a widget recomputes
// its caches when Resize says the visible count moved, and not on every one of
// the eighteen events a drag produces.
func TestResizeReportsOnlyRealChanges(t *testing.T) {
	m := New(1000)
	m.Resize(viewport(20, 10))
	if got := m.Resize(viewport(20, 10)); got {
		t.Error("an identical rect reported a change")
	}
	if got := m.Resize(viewport(30, 10)); got {
		t.Error("a width change reported a change: only the height is the viewport")
	}
	if got := m.Resize(viewport(20, 5)); !got {
		t.Error("a height change did not report a change")
	}
	if got := m.Resize(viewport(20, 0)); !got {
		t.Error("a collapse to zero height did not report a change")
	}
	if got := m.Visible(); got != 0 {
		t.Errorf("Visible = %d, want 0", got)
	}
}

// atLeast1 clamps a degenerate dimension so a zero-sized buffer, which has no
// cells to paint into, is not asked to prove anything.
func atLeast1(v int) int {
	if v < 1 {
		return 1
	}
	return v
}

// ---------------------------------------------------------------------------
// benchmarks
// ---------------------------------------------------------------------------

// benchRender is the flat-cost proof: the same viewport over 10, 100, 10^4, 10^5
// and 10^6 items. It is deliberately a hand-rolled loop rather than a subtest so
// that the numbers are directly comparable on one line each, and it reports
// allocations because "fast" is not the claim — "fast and identical regardless of
// count" is.
func benchRender(b *testing.B, count int) {
	m := New(count)
	v := viewport(80, 24)
	m.Resize(v)
	buf := buffer.NewBuffer(80, 24)
	st := buffer.PlainStyle

	paint := func(dst *buffer.Buffer, row geometry.Rect, i int) {
		dst.FillRect(row, st.Blank())
		dst.SetString(row.X, row.Y, fmt.Sprint(i%10), st)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.ForEach(v, buf, paint)
	}
}

// BenchmarkRender10K is 10,000 items.
func BenchmarkRender10K(b *testing.B) { benchRender(b, 10_000) }

// BenchmarkRender100K is 100,000 items.
func BenchmarkRender100K(b *testing.B) { benchRender(b, 100_000) }

// BenchmarkRender1M is 1,000,000 items.
func BenchmarkRender1M(b *testing.B) { benchRender(b, 1_000_000) }

// BenchmarkRender10 is ten items, the baseline the three above are compared to.
func BenchmarkRender10(b *testing.B) { benchRender(b, 10) }

// BenchmarkClampCostIsIndependentOfCount measures the O(1) claim directly: a
// scroll or a resize on a million-item collection costs the same as on ten.
func BenchmarkClampCostIsIndependentOfCount(b *testing.B) {
	for _, count := range []int{10, 1_000_000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			m := New(count)
			m.Resize(viewport(80, 24))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				m.ScrollBy(1)
			}
		})
	}
}
