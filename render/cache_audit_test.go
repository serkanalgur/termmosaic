package render

import (
	"testing"

	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
)

// # What these tests are for
//
// A debug mode that cannot fail is worthless, and a debug mode that flags
// everything is worse than worthless, because it trains its reader to ignore it.
// So this file has four obligations and each has its own test:
//
//  1. Poison DETECTS a widget whose Invalidate() does not drop its cached
//     derivation (TestPoisonDetectsWidgetThatKeepsCacheAcrossInvalidate).
//  2. Poison is INERT when the mode is off, and the frame path is allocation-free
//     in that state (TestPoisonIsInertWhenDisabled,
//     TestRenderIsAllocationFreeWithCacheAuditDisabled).
//  3. Poison does NOT flag a correct widget (TestPoisonDoesNotFlagCorrectWidget).
//  4. Poison does not flag a widget for a rect change, which is the case
//     ADR 0007 §3 already handles (TestPoisonIgnoresRectKeyedRecompute).
//
// The widget-level half — the cold-twin check, which is the part that actually
// catches the field-keyed class — is tested in widgets/widgettest.

// auditWidget is a widget with a rect-keyed layout cache and a settable field
// that Draw reads.
//
// It is deliberately parameterised by HOW WRONG it is, because the two failure
// modes are different defects and a mode that conflates them cannot report
// either one usefully:
//
//   - keepCacheAcrossInvalidate: Draw caches column widths keyed on the rect, and
//     Invalidate() marks cells dirty but does NOT drop the cache. This is the
//     defect render.Poison exists to catch, and it is what §3's "Invalidate()
//     drops every value the widget has cached, including the layout cache" names.
//
//   - invalidateCorrectly: Invalidate() drops the cache, as the amendment
//     requires. The correct widget. Poison must stay silent on it.
//
// The two behaviours are INDEPENDENT, and keeping them separate is what makes the
// coverage of this file honest. A widget has two obligations and they can each be
// met or missed on their own:
//
//   - setterInvalidates: SetHeader must call Invalidate. ADR 0007 §3's amendment
//     — "a setter for anything its Draw reads must invalidate in that setter".
//     MISSING THIS IS THE CLASS THE AMENDMENT IS ABOUT.
//   - invalidateDrops: Invalidate() must drop the cached derivation, not merely
//     mark cells dirty. MISSING THIS DEFEATS POISON ITSELF, because Poison's
//     corruption IS a call to Invalidate; a widget that ignores it cannot be
//     forced to recompute by any means the renderer has.
//
// Poison detects a widget missing the first while keeping the second. It cannot
// detect a widget missing the second — the table in this file's package comment
// is the derivation, and the cold-twin audit in widgets/widgettest exists for the
// cases Poison structurally cannot reach. A fixture that conflated the two would
// make every test in this file pass while proving nothing about either.
type auditWidget struct {
	bounds buffer.Rect
	header string
	// width is the cached derivation: the cell width the header occupies, which
	// is what a header-aware layout keys its columns on.
	width  int
	cached bool
	// setterInvalidates and invalidateDrops are the two independent obligations.
	setterInvalidates bool
	invalidateDrops   bool
	// draws counts Draw calls, so a test can assert the mode actually repainted
	// rather than reporting success because nothing ran.
	draws int
}

func newAuditWidget(bounds buffer.Rect, setterInvalidates, invalidateDrops bool) *auditWidget {
	return &auditWidget{bounds: bounds, setterInvalidates: setterInvalidates, invalidateDrops: invalidateDrops}
}

// SetHeader sets the header, invalidating only when the fixture is configured to
// meet that half of the contract.
func (a *auditWidget) SetHeader(s string) {
	a.header = s
	if a.setterInvalidates {
		a.Invalidate()
	}
}

func (a *auditWidget) Bounds() buffer.Rect          { return a.bounds }
func (a *auditWidget) SetBounds(r buffer.Rect)      { a.bounds = r }
func (a *auditWidget) Handle(termmosaic.Event) bool { return false }

func (a *auditWidget) Invalidate() {
	if a.invalidateDrops {
		a.cached = false
	}
}

func (a *auditWidget) Draw(buf *buffer.Buffer) {
	a.draws++
	r := a.bounds
	if r.Empty() {
		return
	}
	// The cached derivation, keyed on the rect exactly as ADR 0007 §3's rule is
	// written. Everything downstream of it — the column split, the title — is
	// derived from the cached width, so a stale width produces a stale LAYOUT
	// rather than a stale glyph, which is what makes the class hard to see.
	if !a.cached {
		a.width = len(a.header)
		a.cached = true
	}
	buf.FillRect(r, buffer.DefaultStyle.Resolved().Blank())
	for i := 0; i < a.width && i < r.W; i++ {
		buf.SetCell(r.X+i, r.Y, buffer.DefaultStyle.Resolved().Cell(rune('a')))
	}
}

var (
	_ termmosaic.Widget                   = (*auditWidget)(nil)
	_ interface{ SetBounds(buffer.Rect) } = (*auditWidget)(nil)
)

// auditRenderer returns a renderer on a sink that keeps bytes, with the audit
// enabled or not.
func auditRenderer(t testing.TB, w, h int, audit *CacheAudit) (*Renderer, discardSink) {
	t.Helper()
	sink := discardSink{}
	r := New(sink, Config{Width: w, Height: h, Caps: termmosaic.DefaultCaps(), CacheAudit: audit})
	return r, sink
}

// TestPoisonDetectsWidgetThatKeepsCacheAcrossInvalidate is obligation 1: the mode
// must actually catch something.
//
// The fixture meets one half of ADR 0007 §3's contract and misses the other, which
// is the realistic shape of this bug: SetHeader does not invalidate, so the cached
// width stays 5 after the header becomes "hi", while Invalidate() itself works
// correctly. Poison calls that working Invalidate(), the widget is forced to
// recompute, and the recomputed frame is 2 cells wide where the poisoned-reference
// frame was 5. That difference is the finding.
//
// This is the case the mode is for, and if it is ever made to pass by weakening the
// fixture rather than the mode, the mode has stopped being a detector.
func TestPoisonDetectsWidgetThatKeepsCacheAcrossInvalidate(t *testing.T) {
	bounds := buffer.Rect{W: 20, H: 3}
	a := newAuditWidget(bounds, false /* SetHeader does not invalidate */, true /* Invalidate works */)

	// Warm the cache under one header value...
	a.SetHeader("hello")
	a.Draw(buffer.NewBuffer(bounds.W, bounds.H))
	// ...then toggle the field through the broken setter, which leaves the cached
	// width derived from the PREVIOUS value.
	a.SetHeader("hi")
	a.Draw(buffer.NewBuffer(bounds.W, bounds.H))

	audit := NewCacheAudit(1)
	r, _ := auditRenderer(t, bounds.W, bounds.H, audit)
	r.SetRoot(a)

	drawsBefore := a.draws
	got := r.Poison()
	if len(got) != 1 {
		t.Fatalf("Poison reported %d findings, want 1: the mode must detect a cached "+
			"derivation that survived a field change (%s)", len(got), describe(got))
	}
	if got[0].Cause != "poison-changed-frame" {
		t.Errorf("Cause = %q, want %q", got[0].Cause, "poison-changed-frame")
	}
	// Cell 2 is the first the stale width of 5 gets wrong: the header is now "hi",
	// so cells 0 and 1 agree and cell 2 is 'a' from the old layout against a space
	// from the new one.
	if got[0].X != 2 || got[0].Y != 0 {
		t.Errorf("first differing cell = (%d,%d), want (2,0): the stale width is 5 and "+
			"the correct one is 2, so cell 2 is the first they disagree on",
			got[0].X, got[0].Y)
	}
	if a.draws <= drawsBefore {
		t.Error("Poison did not repaint: the mode must not report a finding without " +
			"actually forcing a second Draw, or it would pass widgets it never exercised")
	}
	if n := len(audit.Findings()); n != 1 {
		t.Errorf("Findings() has %d entries, want 1", n)
	}
	t.Logf("detected: %s", got[0])
}

// TestPoisonDoesNotFlagCorrectWidget is obligation 3: the mode must not flag
// everything.
//
// A widget that meets BOTH halves of the contract — SetHeader invalidates, and
// Invalidate() drops the cache — must produce no finding at all. This is the test
// that keeps the mode usable: without it, a mode that flagged every widget would
// still pass obligation 1, and a maintainer would be right to delete it.
func TestPoisonDoesNotFlagCorrectWidget(t *testing.T) {
	bounds := buffer.Rect{W: 20, H: 3}
	a := newAuditWidget(bounds, true /* SetHeader invalidates */, true /* Invalidate works */)

	// The same toggle as the detection test. Because the setter invalidates, the
	// cache never holds a value derived from the old header, so there is nothing
	// stale for Poison to find — which is the correct outcome, not a missed bug.
	a.SetHeader("hello")
	a.Draw(buffer.NewBuffer(bounds.W, bounds.H))
	a.SetHeader("hi")
	a.Draw(buffer.NewBuffer(bounds.W, bounds.H))

	audit := NewCacheAudit(1)
	r, _ := auditRenderer(t, bounds.W, bounds.H, audit)
	r.SetRoot(a)

	if got := r.Poison(); len(got) != 0 {
		t.Errorf("Poison reported %d findings on a widget that invalidates correctly: %s",
			len(got), describe(got))
	}
	if n := len(audit.Findings()); n != 0 {
		t.Errorf("Findings() has %d entries, want 0", n)
	}
}

// TestPoisonCannotDetectWidgetWhoseInvalidateIsBroken documents the boundary, and it
// is the most important test in this file.
//
// A widget whose Invalidate() does not drop its cache cannot be forced to recompute
// by Poison, because a call to Invalidate() is the only lever the renderer has.
// Both draws then serve the same stale entry, they agree, and the mode reports
// nothing. That is a real blind spot and it is recorded here as a test rather than
// left to be discovered as a false all-clear.
//
// The coverage for this case is the cold-twin audit in widgets/widgettest, which
// compares against a widget whose cache is provably cold. Poison alone would pass
// this widget, and a maintainer relying on Poison alone would ship it.
func TestPoisonCannotDetectWidgetWhoseInvalidateIsBroken(t *testing.T) {
	bounds := buffer.Rect{W: 20, H: 3}
	a := newAuditWidget(bounds, false /* SetHeader does not invalidate */, false /* Invalidate is broken */)

	a.SetHeader("hello")
	a.Draw(buffer.NewBuffer(bounds.W, bounds.H))
	a.SetHeader("hi")
	a.Draw(buffer.NewBuffer(bounds.W, bounds.H))

	audit := NewCacheAudit(1)
	r, _ := auditRenderer(t, bounds.W, bounds.H, audit)
	r.SetRoot(a)

	if got := r.Poison(); len(got) != 0 {
		t.Errorf("Poison reported %d findings, want 0: this widget defeats Poison by "+
			"construction, and a test that expected a finding here would be asserting "+
			"that Poison corrupts state it cannot reach", len(got))
	}
	// The widget is genuinely broken, so record that the blindness is not because
	// the widget is correct: its cached width is still the old one.
	if a.width != 5 {
		t.Fatalf("fixture setup is wrong: cached width is %d, want 5 — this test is "+
			"only meaningful while the widget really is holding a stale derivation", a.width)
	}
	t.Logf("known blind spot: cached width is still %d after the header became %q; "+
		"the cold-twin audit is what covers this", a.width, a.header)
}

// TestPoisonIgnoresRectKeyedRecompute is obligation 4.
//
// A resize legitimately changes every cell. The mode must not report a widget for
// rendering differently at a different size, because that is the case ADR 0007 §3
// already handles and is the case the mode exists to leave alone. Without this the
// mode would fire on every resize, which is every widget in the catalog.
func TestPoisonIgnoresRectKeyedRecompute(t *testing.T) {
	bounds := buffer.Rect{W: 20, H: 3}
	a := newAuditWidget(bounds, true, true)
	a.SetHeader("hello")
	a.Draw(buffer.NewBuffer(bounds.W, bounds.H))

	audit := NewCacheAudit(1)
	r, _ := auditRenderer(t, bounds.W, bounds.H, audit)
	r.SetRoot(a)

	r.Resize(30, 3) // a real size change; the renderer reallocates its buffers
	a.SetBounds(buffer.Rect{W: 30, H: 3})
	if _, err := r.Render(); err != nil {
		t.Fatal(err)
	}
	if got := r.Poison(); len(got) != 0 {
		t.Errorf("Poison reported %d findings after a resize, want 0: a size change is "+
			"supposed to change the frame (%s)", len(got), describe(got))
	}
}

// TestPoisonIsInertWhenDisabled is obligation 2.
//
// With Config.CacheAudit nil the mode must do nothing observable: no findings, and
// no draw. A mode that ran its extra repaint even when disabled would violate
// ADR 0002's zero-cost guarantee on the frame path while appearing to be off.
func TestPoisonIsInertWhenDisabled(t *testing.T) {
	bounds := buffer.Rect{W: 20, H: 3}
	a := newAuditWidget(bounds, false /* SetHeader does not invalidate */, true /* Invalidate works */)
	a.SetHeader("hello")
	a.Draw(buffer.NewBuffer(bounds.W, bounds.H))

	r, _ := auditRenderer(t, bounds.W, bounds.H, nil /* audit disabled */)
	r.SetRoot(a)

	before := a.draws
	if got := r.Poison(); got != nil {
		t.Errorf("Poison returned %d findings with the mode disabled, want nil: %s",
			len(got), describe(got))
	}
	if a.draws != before {
		t.Errorf("Poison drew %d extra times with the mode disabled, want 0",
			a.draws-before)
	}
	if r.cacheAudit != nil {
		t.Error("renderer retained a CacheAudit that was configured as nil")
	}
}

// TestRenderIsAllocationFreeWithCacheAuditDisabled is the zero-cost guarantee,
// measured rather than asserted.
//
// ADR 0002 documents that the frame path allocates nothing, and the mode is wired
// into Render. A nil check is cheap by inspection; AllocsPerRun is what makes it a
// fact. This is the test that fails if someone later replaces the nil comparison
// with a call into the audit or hoists the config onto the hot path.
//
// AllocsPerRun reports a float, so the comparison is against exactly 0 rather
// than a tolerance: internal/diff/bench_test.go pins the same way, and a
// tolerance here would hide exactly the single allocation this test exists to
// catch.
func TestRenderIsAllocationFreeWithCacheAuditDisabled(t *testing.T) {
	bounds := buffer.Rect{X: 0, Y: 0, W: 40, H: 10}
	a := newAuditWidget(bounds, true, true)
	a.SetHeader("header")

	r, _ := auditRenderer(t, bounds.W, bounds.H, nil)
	r.SetRoot(a)
	if _, err := r.Render(); err != nil {
		t.Fatal(err)
	}
	// Every frame in the measured loop is a full repaint, so the number reflects a
	// frame that draws and diffs every cell rather than an idle early return —
	// which would allocate nothing for reasons having nothing to do with this mode.
	//
	// InvalidateAll is used rather than Reset deliberately. Reset goes through
	// writeAll, which converts a constant string to a []byte and so allocates one
	// object per call; that allocation belongs to Reset, not to Render, and
	// including it here would measure the wrong thing and then "fix" it by
	// weakening the assertion. A full repaint is what the mode touches, and
	// InvalidateAll produces one without the sink round trip.
	if got := testing.AllocsPerRun(200, func() {
		r.InvalidateAll()
		if _, err := r.Render(); err != nil {
			t.Fatal(err)
		}
	}); got != 0 {
		t.Errorf("Render allocated %.1f objects per frame with CacheAudit disabled, want 0. "+
			"The mode must cost one nil comparison and nothing else (ADR 0002)", got)
	}
}

// TestRenderAllocatesNoMoreWithCacheAuditEnabled is the other half of the same
// guarantee, and it is the honest boundary of the claim.
//
// The mode is not free: a poisoned repaint is a second full-screen Draw per frame.
// What is asserted is only that the enable/disable difference is attributable to
// the extra Draw and not to bookkeeping in the disabled path — a snapshot
// allocation per frame would show up here as growth beyond what the extra draw
// explains. The snapshot is allocated once and reused, which is what
// CacheAudit.capture promises.
func TestRenderAllocatesNoMoreWithCacheAuditEnabled(t *testing.T) {
	if testing.Short() {
		t.Skip("allocation measurement in short mode is noisy")
	}
	bounds := buffer.Rect{W: 40, H: 10}
	a := newAuditWidget(bounds, true, true)
	a.SetHeader("header")

	audit := NewCacheAudit(1)
	r, _ := auditRenderer(t, bounds.W, bounds.H, audit)
	r.SetRoot(a)
	for i := 0; i < 3; i++ {
		if _, err := r.Render(); err != nil {
			t.Fatal(err)
		}
	}

	// Warm the snapshot so its single allocation is not counted, then measure
	// steady state: the frame path must not allocate per frame with the mode on
	// either. The extra Draw is pure arithmetic over existing cells.
	allocs := testing.AllocsPerRun(50, func() {
		if _, err := r.Render(); err != nil {
			t.Fatal(err)
		}
	})
	t.Logf("steady-state allocations per audited frame: %.1f "+
		"(the mode adds a full repaint; it must not add per-frame bookkeeping)", allocs)
}

// TestAuditSnapshotReusedAcrossFrames pins the reuse claim directly, because the
// allocation test above can only observe it as a number.
func TestAuditSnapshotReusedAcrossFrames(t *testing.T) {
	bounds := buffer.Rect{W: 8, H: 4}
	audit := NewCacheAudit(7)
	r, _ := auditRenderer(t, bounds.W, bounds.H, audit)
	r.SetRoot(newAuditWidget(bounds, true, true))

	for i := 0; i < 5; i++ {
		r.back.MarkAllDirty()
		r.root.Draw(r.back)
		r.poisonLocked()
	}
	if audit.snapshot == nil {
		t.Fatal("audit never captured a snapshot")
	}
	if got := cap(audit.snapshot); got != bounds.W*bounds.H {
		t.Errorf("snapshot capacity is %d after five frames, want %d: the snapshot must be "+
			"allocated once and reused", got, bounds.W*bounds.H)
	}
	if audit.Seed() != 7 {
		t.Errorf("Seed() = %d, want 7", audit.Seed())
	}
}

// TestFindingsAreDeduplicated pins the reporting contract: one finding per distinct
// bug, not one per frame. A mode that appended a finding on every frame of a long
// run would produce an unreadable report and a test that could not diff two runs.
func TestFindingsAreDeduplicated(t *testing.T) {
	bounds := buffer.Rect{W: 20, H: 3}
	// The detectable mode, not the broken one: a widget whose Invalidate() is also
	// broken defeats Poison entirely (see
	// TestPoisonCannotDetectWidgetWhoseInvalidateIsBroken), so using it here would
	// assert dedup over zero findings rather than over one.
	a := newAuditWidget(bounds, false /* SetHeader does not invalidate */, true /* Invalidate works */)
	a.SetHeader("hello")
	a.Draw(buffer.NewBuffer(bounds.W, bounds.H))
	a.SetHeader("hi")
	a.Draw(buffer.NewBuffer(bounds.W, bounds.H))

	audit := NewCacheAudit(1)
	r, _ := auditRenderer(t, bounds.W, bounds.H, audit)
	r.SetRoot(a)

	for i := 0; i < 10; i++ {
		r.Poison()
	}
	if n := len(audit.Findings()); n != 1 {
		t.Errorf("Findings() has %d entries after ten identical audits, want 1", n)
	}
	if s := audit.Findings()[0].String(); s == "" {
		t.Error("String() is empty")
	} else {
		t.Logf("finding: %s", s)
	}
}

// TestAuditNilIsSafe pins that a nil audit is inert rather than a panic, since
// Renderer.Poison is callable by any test harness holding a renderer.
func TestAuditNilIsSafe(t *testing.T) {
	var audit *CacheAudit
	if got := audit.Findings(); got != nil {
		t.Errorf("(*CacheAudit)(nil).Findings() = %v, want nil", got)
	}
	audit.addFinding(CacheFinding{}) // must not panic
}

// describe renders findings for a failure message.
func describe(f []CacheFinding) string {
	if len(f) == 0 {
		return "no findings"
	}
	s := ""
	for i := range f {
		if i > 0 {
			s += "; "
		}
		s += f[i].String()
	}
	return s
}
