package render

// This file is ADR 0007 §3's deferred item, and it is the mechanical half of a
// rule whose cheap half is only a convention on the widget author.
//
// # The failure class
//
// ADR 0007 §3 settles adaptation: a widget that caches anything derived from its
// size caches it against the rect it was computed for, and recomputes when
// Bounds() differs. That rule is silent about every OTHER input the cached value
// depends on, and §3's amendment ("a rect key is not the only thing a cache
// depends on") records the gap:
//
//	A widget that caches column widths keyed on Bounds() is correct as long as
//	nothing but the size changes. Then someone toggles Scrollbar, Header,
//	Status, Zone, a theme colour or a selection-dependent layout, the rect is
//	unchanged, the cache hits, and the widget renders the old layout —
//	permanently, not for one frame, because nothing will ever produce a
//	different rect to invalidate it.
//
// The damage is not that one frame is wrong. It is that there is no repair. A
// rect-keyed cache is self-healing because a resize eventually arrives; a
// field-keyed cache is not, because the field that broke it is the same field
// that will keep it broken until the process exits.
//
// # What is hard about catching it
//
// Review cannot catch it, and neither can a test that asserts on cells. A widget
// whose Draw recomputes from w.header every frame and one whose Draw serves a
// header-shaped value out of a cache keyed on Bounds() produce IDENTICAL cells
// for every input a cell assertion can express. The difference is only visible
// in the ORDER: which derivation happened to be in the cache when Draw ran. So
// the detector must control the cache's history, and a mode that merely re-draws
// cannot — the second Draw hits the same cache entry as the first.
//
// # The two checks
//
// This mode splits the class into the two mechanisms that can produce it, because
// they need different instruments and only one of them can be automatic.
//
// Check 1 — POISON (automatic, renderer-owned, this file). ADR 0007 §3 says
// Invalidate() means "your cached derivation may now be wrong". Poison takes that
// at its word: it snapshots the frame, calls Invalidate() on the root, forces a
// full repaint, and asserts the cells are byte-identical. This is the sentence
// "a debug mode that corrupts a widget's cache after a Draw and asserts the next
// frame is identical" written as code. It catches a cache that Invalidate() does
// not drop, and a Draw that is not idempotent — both real, both invisible to a
// cell assertion, both fixable in the widget.
//
// Check 2 — COLD TWIN (harness-owned, widgets/widgettest). Poison does NOT catch
// the class §3's amendment is actually about, and it is important to be precise
// about why. A widget that caches column widths on Bounds() and reads w.header in
// Draw has an Invalidate() that works correctly: it drops the cache. Poison
// calls it, the cache is dropped, the widget recomputes from w.header, and the
// frame comes out right. The bug only exists when the field is toggled through a
// setter that does NOT invalidate — so between the toggle and the next Draw, and
// no operation the renderer can perform will change that, because the renderer's
// only lever is Invalidate() and calling it is exactly what fixes the widget.
//
// Detecting that requires an instrument the renderer does not have: a second,
// freshly constructed widget in the same final state, whose cache is provably
// cold. A cold widget and a warm widget in the same state must produce identical
// cells. If they do not, the warm one served a stale derivation. That comparison
// needs the catalog's setters — only the harness knows that toggling a Table's
// scrollbar is a meaningful transition — so it lives in widgets/widgettest, and
// Poison is the primitive it borrows.
//
// Check 1 without Check 2 catches the amendment's class zero times. That is not
// a reason to ship Check 1 alone, because Check 1 is sound and the class has a
// neighbour, but it is the reason the harness is not optional.
//
// # Where this lives, and why not in render's own package for the cold twin
//
// Poison is here, in render, for three reasons.
//
// First, the ADR says so and the ADR is right about this one: "the expensive
// half belongs to the renderer". Poison needs the front and back buffers, the
// dirty accumulator and a full repaint, all of which are the renderer's private
// state. Reaching them from a test means going through Render, and Render is
// exactly what the audit is perturbing.
//
// Second, and decisively: Poison reaches widgets only through termmosaic.Widget's
// four methods — Bounds, Draw, Invalidate, Handle. It never touches a widget's
// fields, so render gains no dependency on any widget package and the layering
// the ADRs establish is untouched. render still imports only the root package,
// buffer, geometry and its own internals. Poisoning by reflection into private
// cache fields was the alternative and it is rejected: it would make render
// depend on every widget package's private layout, it would break on any struct
// rename, and it would test the fields rather than the contract. The contract is
// the thing that can be wrong in thirty implementations at once.
//
// Third, Poison needs the ZERO-COST-WHEN-OFF guarantee ADR 0002 documents for the
// frame path, and that guarantee is only meaningful in the package that owns the
// frame path. A mode living in the test harness could not be argued to cost
// nothing, because it would not be in the thing being argued about. Here the
// entire cost when disabled is one nil comparison in Render.
//
// # Determinism
//
// No wall-clock, no map iteration, no goroutine scheduling. Poison compares cells
// in row-major order and reports the FIRST differing cell, so a failing audit
// names the same cell on every run and on every machine. The harness's transition
// order comes from an explicit seed via math/rand's own reproducible source; see
// widgets/widgettest.

import (
	"fmt"

	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
)

// CacheAudit is ADR 0007 §3's deferred debug mode: it corrupts a widget's cached
// derivation after a Draw and asserts the next frame is byte-identical.
//
// It is opt-in through Config.CacheAudit and off by default. A nil *CacheAudit
// means every method here is unreachable and Render's added cost is one pointer
// comparison — see Render and the zero-allocation test.
//
// The zero value is not usable; construct one with NewCacheAudit.
type CacheAudit struct {
	// seed drives the harness's transition order. It lives here rather than in
	// the harness so that a whole audit run is described by one value and a
	// failing run is reproducible from its own output.
	seed uint64

	// snapshot is the frame Poison compares against, allocated on first use so
	// that enabling the mode costs one allocation and never allocates again.
	snapshot []buffer.Cell
	// snapW and snapH are the snapshot's dimensions. A resize invalidates the
	// snapshot, because cells from a differently sized screen cannot be
	// compared against cells from this one.
	snapW, snapH int

	// findings accumulates every mismatch Poison has seen, deduplicated by
	// identity so a widget audited over a hundred transitions reports one
	// finding rather than a hundred.
	findings []CacheFinding
}

// NewCacheAudit returns a CacheAudit whose harness ordering is seeded with seed.
//
// The seed is honoured here only so it can be read back by
// widgets/widgettest.CacheAuditDriver; Poison itself is entirely deterministic
// and does not use it, because Poison's job is to compare two frames and the
// order in which comparisons are requested cannot change their results. A
// different seed is a different audit schedule, not a different Poison.
func NewCacheAudit(seed uint64) *CacheAudit {
	return &CacheAudit{seed: seed}
}

// Seed returns the seed this audit was constructed with. It is exported so a
// failing audit can name the schedule that produced it.
func (a *CacheAudit) Seed() uint64 { return a.seed }

// CacheFinding is one mismatch between a poisoned frame and the frame before it.
type CacheFinding struct {
	// Widget is the root's concrete type, which is how an audit names the thing
	// to go and read.
	Widget string
	// X and Y are the FIRST differing cell in row-major order. First rather than
	// every cell, because a stale layout differs in thousands of cells and a
	// report that lists them all is unreadable and non-deterministic in length.
	X, Y int
	// Before and After are the cell values at that position: what the frame
	// showed, and what the poisoned repaint produced.
	Before, After buffer.Cell
	// Cause is a short machine-readable tag, currently always "poison-changed-frame".
	Cause string
}

// String renders a finding as one line, in this repository's usual
// "termmosaic: what happened" voice.
func (f CacheFinding) String() string {
	return fmt.Sprintf("termmosaic: %s: cache-poison repaint changed cell (%d,%d): %q -> %q (%s)",
		f.Widget, f.X, f.Y, string(f.Before.Ch), string(f.After.Ch), f.Cause)
}

// Findings returns the mismatches seen so far, in the order they were first
// observed. It returns nil when the audit is clean, so a test can assert on
// emptiness without a length comparison against a default.
func (a *CacheAudit) Findings() []CacheFinding {
	if a == nil || len(a.findings) == 0 {
		return nil
	}
	return a.findings
}

// addFinding records f unless an identical finding is already present. The
// deduplication key is the whole finding, which is sound because Poison's
// comparison is deterministic: the same widget at the same cell with the same
// before and after is the same bug however many transitions reached it.
func (a *CacheAudit) addFinding(f CacheFinding) {
	if a == nil {
		return
	}
	for _, existing := range a.findings {
		if existing == f {
			return
		}
	}
	a.findings = append(a.findings, f)
}

// capture snapshots the given buffer's cells.
//
// It is called once per frame while the audit is enabled, and allocates only on
// the first call: the snapshot is reused for the lifetime of the audit, and
// resized only when the screen changes size. That reuse is what lets the mode be
// enabled in a benchmark without becoming the thing being measured.
func (a *CacheAudit) capture(src *buffer.Buffer) {
	w, h := src.Size()
	if cap(a.snapshot) < w*h {
		a.snapshot = make([]buffer.Cell, w*h)
		a.snapW, a.snapH = 0, 0
	}
	a.snapshot = a.snapshot[:w*h]
	// Copy densely, row by row, rather than by a flat index loop: Buffer
	// deliberately exports no flat accessor, because on a sub-buffer — a
	// strided view of a wider parent — consecutive cells are not adjacent in
	// memory and a flat copy would be silently wrong (ADR 0006). Row-by-row
	// copy is the idiom Buffer's own Row documentation prescribes. The buffers
	// compared here are top-level, where the two agree; the loop is written the
	// safe way regardless, because a helper that is only correct for top-level
	// buffers is a trap for whoever calls it next.
	for row := 0; row < h; row++ {
		copy(a.snapshot[row*w:(row+1)*w], src.Row(row))
	}
	a.snapW, a.snapH = w, h
}

// matches reports whether src is cell-for-cell equal to the snapshot, and if not
// where the first difference is.
//
// Row-major order, first difference only. That ordering is the whole
// determinism guarantee: given the same two frames, this always names the same
// cell, on any machine, in any run.
func (a *CacheAudit) matches(src *buffer.Buffer) (bool, int, int) {
	w, h := src.Size()
	if w != a.snapW || h != a.snapH {
		// A different size is a different frame, not a corrupted cache. Callers
		// re-capture across a resize; reporting it as a finding would make every
		// audited test fail on the first resize for no reason.
		return true, 0, 0
	}
	for row := 0; row < h; row++ {
		line := src.Row(row)
		want := a.snapshot[row*w : (row+1)*w]
		for col := 0; col < w; col++ {
			if line[col] != want[col] {
				return false, col, row
			}
		}
	}
	return true, 0, 0
}

// Poison runs Check 1 against the renderer's current root: it treats the frame
// the renderer last painted as correct, corrupts the widget's cached derivation
// by calling Invalidate() on it, forces a full repaint into the back buffer, and
// reports whether any cell changed.
//
// The corrupt-then-compare order matters. Snapshotting after the poison would
// compare the poisoned frame with itself and always agree; snapshotting before is
// what makes "identical" a claim about the widget rather than about the buffer.
//
// Poison does NOT swap the buffers or write to the Sink. It draws into the back
// buffer and then marks it dirty again, so the renderer's frame state stays
// coherent and the next real frame is unaffected. A debug mode that corrupted the
// frame path's invariants would be unusable in exactly the long-running
// scenarios it exists to investigate.
//
// It returns the findings added by this call, which is empty when the widget
// honoured Invalidate() and Draw is idempotent.
func (r *Renderer) Poison() []CacheFinding {
	if r.cacheAudit == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.root == nil {
		return nil
	}

	// Draw once so the back buffer holds a frame in the widget's current state.
	// Without this a caller who enabled the audit and called Poison before any
	// Render would compare an untouched buffer against an untouched buffer and
	// be told the widget was correct.
	r.back.MarkAllDirty()
	r.root.Draw(r.back)
	return r.poisonLocked()
}

// poisonLocked is the whole mode. The renderer's lock must be held.
//
// It is one function rather than two because the automatic per-frame path in
// Render and the manual path must be the same check. A mode whose documented
// behaviour and whose actual behaviour are two code paths is a mode that will
// eventually pass tests it was never running.
//
// The invariant asserted is: drawing the same state twice, with the widget's
// cached derivation dropped in between, produces identical cells. Both halves
// matter. The first draw is the reference and the widget is free to cache
// whatever it likes; the second draw is forced to recompute, and if the two
// disagree then the widget's cached value was not a function of its state, which
// is the defect.
//
// A note on what this does to the frame that goes out: the poisoned draw is left
// in the back buffer, so a broken widget paints its stale derivation. That is
// deliberate. Substituting the reference frame would make the mode lie to the
// screen, and a debug mode that hides the bug it found is worse than no mode.
// Correct widgets are unaffected, because for them the two draws are identical by
// construction.
func (r *Renderer) poisonLocked() []CacheFinding {
	a := r.cacheAudit
	before := len(a.findings)

	// The reference frame, captured BEFORE the corruption. Snapshotting after
	// would compare the poisoned frame with itself and always agree, which is
	// what makes the ordering here the actual test rather than a detail.
	a.capture(r.back)

	// The corruption. ADR 0007 §3: Invalidate() means "your cached derivation may
	// now be wrong", so the only correct response is to drop it and recompute.
	r.root.Invalidate()

	// Force the repaint of every cell. Invalidate on the widget is not enough by
	// itself: the dirty accumulator decides what Render walks, and a widget that
	// marks nothing dirty would produce no repaint at all — which would pass this
	// audit while testing nothing whatsoever. That is the failure mode a naive
	// implementation of this mode has, and it fails silently, which is the worst
	// way to fail.
	r.back.MarkAllDirty()
	r.root.Draw(r.back)

	if ok, x, y := a.matches(r.back); !ok {
		a.addFinding(CacheFinding{
			Widget: widgetName(r.root),
			X:      x,
			Y:      y,
			Before: a.snapshot[y*a.snapW+x],
			After:  r.back.CellAt(x, y),
			Cause:  "poison-changed-frame",
		})
	}

	// Leave the buffer dirty so the next real frame repaints in full. The poisoned
	// draw is a diagnostic artefact and must not be mistaken for the renderer's
	// idea of what the screen currently shows.
	r.back.MarkAllDirty()

	if len(a.findings) == before {
		return nil
	}
	return a.findings[before:]
}

// widgetName returns a widget's concrete type name for a report.
//
// reflect is confined to this one helper rather than used for the poisoning
// itself. Naming a type in an error message is not a dependency on a widget's
// internals, and reflect.TypeOf on an interface value cannot reach a private
// field — so the layering argument in the package comment is unaffected.
func widgetName(w termmosaic.Widget) string {
	if w == nil {
		return "<nil>"
	}
	return fmt.Sprintf("%T", w)
}
