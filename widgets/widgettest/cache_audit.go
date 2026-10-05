// Package widgettest's cache-audit half: ADR 0007 §3's cold-twin check.
//
// # Why this exists separately from render.Poison
//
// render.Poison is the automatic half and it is sound, but it provably cannot
// catch the class ADR 0007 §3's amendment is about, and pretending otherwise
// would make this whole audit worthless. The reasoning is worth stating plainly
// because it is the whole justification for a second mechanism.
//
// A widget that caches column widths keyed on Bounds() and also reads w.header
// inside Draw has a perfectly correct Invalidate(): it drops the cache. Poison
// calls Invalidate(), the cache goes, the widget recomputes from the current
// header, and the cells come out right. Poison reports nothing.
//
// The defect is not in Invalidate. It is that the SETTER for header does not call
// it. So the window in which the bug exists is between the setter returning and
// the next Draw, and the only thing that can observe that window is a second
// widget in the same final state whose cache never saw the old one.
//
// Hence the twin. A widget that has just been transitioned, and a freshly
// constructed widget in exactly that final state, must render identical cells.
// The first has a cache warmed by the transition; the second has a provably cold
// one. Any difference is a derivation that survived a state change it should not
// have survived, and it is permanent — no future resize repairs it, which is the
// property that makes this class worth a mechanism at all.
//
// # Why the transitions live here and not in render
//
// Because only a harness knows what a meaningful transition is for a given widget.
// "Toggle the Table's scrollbar" is catalog knowledge; the renderer cannot have it
// without importing every widget package, which inverts the layering ADR 0003 and
// ADR 0008 establish. render owns the frame-level assertion; this owns the
// schedule of state changes that feed it.
//
// # Determinism
//
// Transitions are applied in the order given, and the schedule is derived from an
// explicit seed rather than from map iteration, which Go randomises per run. Two
// runs of the same audit over the same catalog with the same seed visit the same
// widget states in the same order and therefore report the same findings in the
// same order. Nothing here reads the clock or spawns a goroutine.

package widgettest

import (
	"fmt"
	"math/rand"
	"reflect"
	"sort"

	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
)

// Transition is one meaningful change of a widget's state, in the sense that
// ADR 0007 §3 §"a rect key is not the only thing a cache depends on" cares
// about: a field that Draw reads and that a setter could change without changing
// Bounds.
//
// Apply is called on the widget under audit. It must perform the same state
// change an application would, including calling the widget's own Invalidate
// exactly as that setter does — the audit is testing the catalog as written, not
// a corrected version of it. A widget whose setter fails to invalidate is the
// thing being looked for, so the harness must not paper over it.
type Transition struct {
	// Name identifies the transition in a finding. It should name the field
	// being changed, because that is what a reader then goes and looks at.
	Name string
	// Apply performs the state change on w.
	Apply func(w termmosaic.Widget)
}

// CacheSubject is one widget the audit can exercise.
type CacheSubject struct {
	// Name is the human-readable identity of the subject, used in findings.
	// Convention is "package.Type".
	Name string
	// New constructs a widget in its INITIAL state. It is called twice per
	// audited transition: once for the widget under audit, once for its cold
	// twin. It must be deterministic — two calls must produce identical state,
	// or the comparison below would be measuring the constructor rather than the
	// cache. That is the one real constraint on this function and it is not
	// optional: a New that embeds a timestamp or reads a global makes every
	// finding it produces meaningless.
	New func() termmosaic.Widget
	// Size places the widget on a screen. The widget is set to this rect, which
	// is what a layout would do before the first frame.
	Size buffer.Rect
	// Transitions are the state changes to audit, applied in order.
	Transitions []Transition
}

// ColdTwinFinding is a widget whose warmed cache and cold cache disagree.
type ColdTwinFinding struct {
	// Subject is the CacheSubject's Name.
	Subject string
	// Transition is the name of the state change that exposed it.
	Transition string
	// Cells is how many cells differ between the warm and cold frames. Reported
	// as a count because a stale layout typically differs in most of its area,
	// and the count is what distinguishes "one glyph" from "the whole layout is
	// the old one".
	Cells int
	// X, Y is the first differing cell in row-major order.
	X, Y int
	// Warm and Cold are the two cell values at that position, so the report says
	// what the stale cache produced and what it should have produced.
	Warm, Cold buffer.Cell
}

// String renders a finding as one line.
func (f ColdTwinFinding) String() string {
	return fmt.Sprintf("%s: cache survived %q: %d cells differ, first at (%d,%d): %q -> %q",
		f.Subject, f.Transition, f.Cells, f.X, f.Y, string(f.Warm.Ch), string(f.Cold.Ch))
}

// AuditColdTwin runs the cold-twin check over every subject and transition, and
// returns one finding per disagreement.
//
// Both arms are compared at the BUFFER level, by calling Draw into a bare
// buffer, rather than through the renderer's diff and the headless screen model
// as this package's Render and Capture helpers do. That is a deliberate
// departure from the rest of this package and it is the right instrument for this
// particular check: the diff decides what to emit from the cells the widget
// wrote, so routing through it can only lose information. Two frames that differ
// in their cells can diff to identical bytes, and a check whose subject is "did
// the widget write different cells" must not be filtered by a stage whose job is
// to discard exactly the differences it can prove are invisible to the user. The
// widget/diff seam that this package's other helpers exist for is a different
// question, answered by a different helper.
//
// The buffer-level comparison is also what makes the finding actionable: it
// reports which cell the stale derivation produced, not which byte sequence a
// terminal would have received.
//
// seed selects the order transitions are visited in. The order does not change
// whether a bug is found, only the order findings are reported, so this exists to
// make a large catalog's report reproducible rather than to explore a search
// space. Pass the same seed to reproduce a run.
func AuditColdTwin(subjects []CacheSubject, seed uint64) []ColdTwinFinding {
	var findings []ColdTwinFinding

	// Shuffle a copy so the caller's slice is not reordered, and so the order is a
	// function of the seed alone. rand.New with an explicit source is
	// deterministic across runs and platforms, unlike the global source.
	order := rand.New(rand.NewSource(int64(seed)))
	for _, s := range subjects {
		idx := order.Perm(len(s.Transitions))
		for _, i := range idx {
			tr := s.Transitions[i]
			if f, bad := auditOne(s, tr); bad {
				findings = append(findings, f)
			}
		}
	}

	// Sort so a report is diffable between runs regardless of the shuffle. The
	// shuffle exists so that a bug which only manifests after a particular
	// sequence of transitions is reachable at all; the sort exists so that two
	// runs can be compared. Sorting does not undo the first, because every
	// transition is still applied to a freshly built widget.
	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].Subject != findings[j].Subject {
			return findings[i].Subject < findings[j].Subject
		}
		return findings[i].Transition < findings[j].Transition
	})
	return findings
}

// auditOne runs a single subject through a single transition.
//
// Both arms are constructed fresh, so the transition under test is applied to a
// widget that has not been through it before. That matters: applying a toggle to
// an already-transitioned widget could, on a correct widget, hit a cache entry
// populated after the previous transition and mask the very thing being tested.
func auditOne(s CacheSubject, tr Transition) (ColdTwinFinding, bool) {
	warm := s.New()
	setBounds(warm, s.Size)

	// The warm arm: draw twice so any layout cache is populated, then transition,
	// then draw again. Only the frame after the transition is kept — the earlier
	// draws exist to warm the cache, and comparing them would be a different check
	// (Draw idempotence, which render.Poison already covers).
	warmDraws(warm, s.Size, 2)
	tr.Apply(warm)
	bufWarm := renderOne(warm, s.Size, 1)

	// The cold arm: a widget in exactly the same final state, never drawn, so its
	// cache has nothing in it. SetBounds is applied before the transition so the
	// two arms agree on the rect — if they disagreed on size the first difference
	// found would be the rect itself and the report would be noise.
	cold := s.New()
	setBounds(cold, s.Size)
	tr.Apply(cold)
	bufCold := renderOne(cold, s.Size, 1)

	w, h := s.Size.W, s.Size.H
	if w <= 0 || h <= 0 {
		return ColdTwinFinding{}, false
	}

	n, firstX, firstY := 0, 0, 0
	for y := 0; y < h; y++ {
		wr, cr := bufWarm.Row(y), bufCold.Row(y)
		if len(wr) != w || len(cr) != w {
			// A widget drew outside its buffer. That is its own defect and
			// buffer.SetCell silently drops the write, so there is nothing to
			// compare and reporting it here would misattribute it.
			return ColdTwinFinding{}, false
		}
		for x := 0; x < w; x++ {
			if wr[x] != cr[x] {
				if n == 0 {
					firstX, firstY = x, y
				}
				n++
			}
		}
	}
	if n == 0 {
		return ColdTwinFinding{}, false
	}
	return ColdTwinFinding{
		Subject:    s.Name,
		Transition: tr.Name,
		Cells:      n,
		X:          firstX,
		Y:          firstY,
		Warm:       bufWarm.CellAt(firstX, firstY),
		Cold:       bufCold.CellAt(firstX, firstY),
	}, true
}

// renderOne draws w frames at the given size and returns the resulting buffer.
//
// frames > 1 is how a layout cache is guaranteed warm before the transition is
// applied: one frame may be a first-draw that takes a different path from a
// steady-state one, and auditing against a cold cache on the warm arm would make
// the comparison meaningless.
func renderOne(w termmosaic.Widget, size buffer.Rect, frames int) *buffer.Buffer {
	warmDraws(w, size, frames)
	// The frame that is kept is drawn into a FRESH buffer rather than reusing the
	// one the warm-up drew into, so the returned cells are exactly what one Draw of
	// the current state produces and not an accumulation of every draw before it.
	// A widget that paints its whole rect first — which Block does, and which the
	// catalog requires, because the renderer never clears — makes those two the
	// same, and relying on that would quietly make this helper wrong for any widget
	// that does not repaint in full.
	buf := buffer.NewBuffer(size.W, size.H)
	w.Draw(buf)
	return buf
}

// warmDraws draws w frames times to populate any cached derivation, discarding the
// cells. It exists so the warm-up is visible at the call site as a warm-up rather
// than as a buffer somebody forgot to use.
func warmDraws(w termmosaic.Widget, size buffer.Rect, frames int) {
	buf := buffer.NewBuffer(size.W, size.H)
	setBounds(w, size)
	for i := 0; i < frames; i++ {
		w.Draw(buf)
	}
}

// setBounds gives the widget its rect if it can take one.
//
// Bounds is not part of termmosaic.Widget — the interface is Bounds/Draw/
// Invalidate/Handle, and setting a widget's rectangle is how a layout hands it
// space. Every widget in the catalog has a SetBounds, so this is a type
// assertion rather than a change to the interface, and ADR 0003's four-method
// surface stays four methods. A widget without one is audited at whatever rect it
// was constructed with, which is a legal zero-Bounds frame and still tests the
// cache.
func setBounds(w termmosaic.Widget, size buffer.Rect) {
	if s, ok := w.(interface{ SetBounds(buffer.Rect) }); ok {
		s.SetBounds(size)
	}
}

// HasExportedField reports whether the concrete type of v has an EXPORTED struct
// field called name.
//
// It exists to pin the half of ADR 0007 §3's amendment that a cell comparison
// cannot see at all. A widget whose Draw reads a field and caches a derivation
// from it is correct only if every route to changing that field invalidates. When
// the field is exported, there is a route that does not: assignment, from
// anywhere, in one statement, with no call to Invalidate and nothing to compile
// against. The result is a permanently stale layout — the shape ADR 0007 §3 calls
// out as having no repair, because no future resize will produce the key the cache
// is missing.
//
// So for a field the catalog derives something from, the field is not exported.
// That is a breaking change, and it is the deliberate one: the alternative is a
// documented rule ("call Invalidate() after assigning") that a caller has no way
// to be reminded of and this repository has no way to enforce in a user's program.
// Making it a compile error makes the class unreachable rather than merely
// discouraged.
//
// This takes a value rather than a type so that a test can pass a constructed
// widget and read the assertion as a statement about that widget. v may be nil,
// which reports false rather than panicking.
//
// It is reflection over field NAMES and export status, not over values, and it is
// deliberately not the poisoning mechanism: naming a field is not a dependency on
// its contents, which is why this can live in a test harness while render's mode
// needs no widget package at all.
func HasExportedField(v any, name string) bool {
	if v == nil {
		return false
	}
	t := reflect.TypeOf(v)
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == nil || t.Kind() != reflect.Struct {
		return false
	}
	f, ok := t.FieldByName(name)
	return ok && f.IsExported()
}

// SameConstructionState reports whether two calls to a subject's New produced the
// same state, by comparing them field by field through reflection.
//
// It exists for one purpose: to let a test assert the assumption the whole
// cold-twin comparison rests on — that New is deterministic. If it is not, every
// finding it produces is measuring the constructor rather than the cache.
//
// It is deliberately NOT used inside the comparison itself. If New were
// nondeterministic the honest outcome is a confusing failure, not a skipped check;
// a skipped check would be this mode lying about being thorough.
//
// Unexported fields are readable through reflection for comparison, so this sees
// the cache fields too. That is correct here and only here: this is a test-side
// assertion about determinism, not the poisoning mechanism, which is why render
// does not need it and why this lives in the harness rather than in the frame path.
func SameConstructionState(a, b any) bool {
	return reflect.DeepEqual(a, b)
}
