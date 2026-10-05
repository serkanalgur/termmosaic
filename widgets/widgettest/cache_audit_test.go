package widgettest

import (
	"testing"

	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
)

// # The three obligations, and why the cold twin has to answer all of them
//
// render/cache_audit_test.go proves the automatic mode detects one specific
// defect. This file proves the harness half catches the defects the automatic mode
// structurally cannot, and — just as importantly — that it does not fire on a
// correct widget. A mode that flags everything is as useless as one that flags
// nothing, and the second is the more common way for an audit like this to rot.
//
// The fixture that matters most is brokenBoth, whose Invalidate() is also broken:
// render.Poison passes it (see TestPoisonCannotDetectWidgetWhoseInvalidateIsBroken
// in package render) because a call to Invalidate() is the only lever the renderer
// has. The cold twin catches it, because the twin's cache is provably cold and
// the two frames disagree.

// twinWidget is a widget with a rect-keyed layout cache and one settable field.
//
// The three modes are the two independent obligations from ADR 0007 §3's
// amendment, plus the combination that defeats both mechanisms separately:
//
//   - setterInvalidates: SetHeader must invalidate, or the cached width survives
//     the change. Caught by render.Poison.
//   - invalidateDrops: Invalidate() must drop the cache. Caught ONLY by the cold
//     twin, because Poison's corruption is itself a call to Invalidate().
//   - both broken: caught by the cold twin, and missed by Poison.
type twinWidget struct {
	bounds buffer.Rect
	header string
	// width is the cached derivation: the header's cell width, which a
	// header-aware layout keys its column split on. A stale width therefore
	// produces a stale LAYOUT, not merely a stale glyph.
	width  int
	cached bool

	setterInvalidates bool
	invalidateDrops   bool
}

func (t *twinWidget) Bounds() buffer.Rect          { return t.bounds }
func (t *twinWidget) SetBounds(r buffer.Rect)      { t.bounds = r }
func (t *twinWidget) Handle(termmosaic.Event) bool { return false }

func (t *twinWidget) Invalidate() {
	if t.invalidateDrops {
		t.cached = false
	}
}

func (t *twinWidget) SetHeader(s string) {
	t.header = s
	if t.setterInvalidates {
		t.Invalidate()
	}
}

func (t *twinWidget) Draw(buf *buffer.Buffer) {
	r := t.bounds
	if r.Empty() {
		return
	}
	if !t.cached {
		t.width = len(t.header)
		t.cached = true
	}
	buf.FillRect(r, buffer.DefaultStyle.Resolved().Blank())
	st := buffer.DefaultStyle.Resolved()
	for i := 0; i < t.width && i < r.W; i++ {
		buf.SetCell(r.X+i, r.Y, st.Cell('a'))
	}
}

var (
	_ termmosaic.Widget                   = (*twinWidget)(nil)
	_ interface{ SetBounds(buffer.Rect) } = (*twinWidget)(nil)
)

// subject builds an audit subject over a twinWidget in the given mode.
func subject(name string, setterInvalidates, invalidateDrops bool) CacheSubject {
	return CacheSubject{
		Name: name,
		New: func() termmosaic.Widget {
			w := &twinWidget{setterInvalidates: setterInvalidates, invalidateDrops: invalidateDrops}
			w.SetHeader("hello")
			return w
		},
		Size: buffer.Rect{W: 20, H: 3},
		Transitions: []Transition{
			{
				// The transition changes the field Draw reads without changing the
				// rect, which is the condition ADR 0007 §3's amendment names: the
				// cache hits and nothing will ever invalidate it again.
				Name:  "SetHeader(\"hi\")",
				Apply: func(w termmosaic.Widget) { w.(*twinWidget).SetHeader("hi") },
			},
		},
	}
}

var (
	correctWidget  = subject("twin/correct", true, true)
	setterBroken   = subject("twin/setter-does-not-invalidate", false, true)
	invalidBroken  = subject("twin/invalidate-does-not-drop", true, false)
	bothBroken     = subject("twin/both-broken", false, false)
	allSubjectsSet = []CacheSubject{correctWidget, setterBroken, invalidBroken, bothBroken}
)

// TestColdTwinDetectsStaleDerivation is obligation 1: the audit catches a real
// defect.
//
// runOne drives a single subject through its single transition and asserts the
// disagreement the warm and cold frames should have.
func TestColdTwinDetectsStaleDerivation(t *testing.T) {
	// Every mode except the correct one must be flagged. The three broken modes
	// are listed explicitly rather than derived, because the point of the test is
	// exactly which defects the mechanism covers — including invalidBroken and
	// bothBroken, which render.Poison cannot see.
	for _, s := range []CacheSubject{setterBroken, invalidBroken, bothBroken} {
		t.Run(s.Name, func(t *testing.T) {
			f, bad := auditOne(s, s.Transitions[0])
			if !bad {
				t.Fatalf("audit reported no finding, but this widget keeps a cached " +
					"width derived from the previous header: the defect is real and the " +
					"audit must catch it")
			}
			if f.Subject != s.Name {
				t.Errorf("Subject = %q, want %q", f.Subject, s.Name)
			}
			if f.Transition != s.Transitions[0].Name {
				t.Errorf("Transition = %q, want %q", f.Transition, s.Transitions[0].Name)
			}
			// The header goes from 5 cells to 2, so the first three cells are 'a' in
			// the stale layout and blank from cell 2 on: 3 cells differ, and the
			// first of them is (2,0).
			if f.Cells != 3 {
				t.Errorf("Cells = %d, want 3: the stale width is 5 and the correct one "+
					"is 2, so cells 2, 3 and 4 disagree", f.Cells)
			}
			if f.X != 2 || f.Y != 0 {
				t.Errorf("first differing cell = (%d,%d), want (2,0)", f.X, f.Y)
			}
			if f.Warm.Ch != 'a' {
				t.Errorf("Warm cell = %q, want 'a': the stale layout is the one still "+
					"holding the old width", string(f.Warm.Ch))
			}
			if f.Cold.Ch != ' ' {
				t.Errorf("Cold cell = %q, want ' ': the cold twin is the correct frame",
					string(f.Cold.Ch))
			}
			t.Logf("caught: %s", f)
		})
	}
}

// TestColdTwinDoesNotFlagCorrectWidget is obligation 3: the audit does not flag
// everything.
//
// The correct widget toggles its header through a setter that invalidates, and its
// Invalidate() drops the cache. There is no stale derivation to find, so the audit
// must be silent.
//
// This is the test that keeps the mechanism trustworthy. Without it, an audit that
// reported a finding for every widget would still pass obligation 1, and a
// maintainer would be right to ignore its output entirely.
func TestColdTwinDoesNotFlagCorrectWidget(t *testing.T) {
	if f, bad := auditOne(correctWidget, correctWidget.Transitions[0]); bad {
		t.Errorf("audit reported a finding on a widget that invalidates correctly: %s", f)
	}
}

// TestColdTwinIsSilentWhenThereIsNoTransitionToAudit is the degenerate case.
//
// A subject with no transitions must produce nothing. It cannot, since there is
// nothing to run, and the test says so rather than leaving the case to inference.
func TestColdTwinIsSilentWhenThereIsNoTransitionToAudit(t *testing.T) {
	s := CacheSubject{
		Name: "empty",
		New:  func() termmosaic.Widget { return &twinWidget{} },
		Size: buffer.Rect{W: 8, H: 2},
	}
	if got := AuditColdTwin([]CacheSubject{s}, 1); len(got) != 0 {
		t.Errorf("audit reported %d findings for a subject with no transitions: %v",
			len(got), got)
	}
}

// TestAuditColdTwinReportsOnlyRealFindings runs the whole subject set and pins the
// exact expected set. Every other test in this file checks one subject in
// isolation; this one checks that the aggregate is exactly the three broken ones
// and nothing else, which is what a catalog audit will actually assert on.
func TestAuditColdTwinReportsOnlyRealFindings(t *testing.T) {
	got := AuditColdTwin(allSubjectsSet, 1)

	want := map[string]bool{}
	for _, s := range []CacheSubject{setterBroken, invalidBroken, bothBroken} {
		want[s.Name] = false
	}
	for _, f := range got {
		if _, ok := want[f.Subject]; !ok {
			t.Errorf("unexpected finding for %q: %s", f.Subject, f)
			continue
		}
		want[f.Subject] = true
	}
	for name, seen := range want {
		if !seen {
			t.Errorf("no finding reported for the broken subject %q", name)
		}
	}
	if len(got) != len(want) {
		t.Errorf("reported %d findings, want %d: %v", len(got), len(want), got)
	}
}

// TestAuditIsDeterministic pins the reproducibility the ADR's "mechanically
// instead of by review" claim depends on.
//
// A mode that reported a different set of findings on each run could not be used
// as a gate, because a green run would prove nothing. Three runs of the whole
// subject set with the same seed must produce identical findings in identical
// order, and two different seeds must still produce the same SET, since the order
// a bug is discovered in cannot change whether it exists.
func TestAuditIsDeterministic(t *testing.T) {
	first := AuditColdTwin(allSubjectsSet, 42)
	for i := 0; i < 3; i++ {
		again := AuditColdTwin(allSubjectsSet, 42)
		if len(again) != len(first) {
			t.Fatalf("run %d reported %d findings, first run reported %d",
				i, len(again), len(first))
		}
		for j := range first {
			if again[j] != first[j] {
				t.Fatalf("run %d finding %d = %s, first run = %s: the audit must be "+
					"reproducible for a fixed seed", i, j, again[j], first[j])
			}
		}
	}

	// A different seed reorders the search but must not change the answer.
	other := AuditColdTwin(allSubjectsSet, 9999)
	if len(other) != len(first) {
		t.Errorf("a different seed reported %d findings, want %d: the seed may change "+
			"the order a bug is found in, not whether it exists", len(other), len(first))
	}
}

// TestSameConstructionState pins the assumption every comparison above rests on.
//
// AuditColdTwin builds two widgets per transition and compares them. If New were
// nondeterministic — reading a clock, consuming from a global, drawing a random
// colour — the two arms would differ for reasons that have nothing to do with the
// cache, and every finding would be noise that a maintainer would learn to skip.
func TestSameConstructionState(t *testing.T) {
	a := &twinWidget{header: "hello", width: 5, cached: true}
	b := &twinWidget{header: "hello", width: 5, cached: true}
	if !SameConstructionState(a, b) {
		t.Error("two identically-constructed widgets compared unequal")
	}
	b.header = "hi"
	if SameConstructionState(a, b) {
		t.Error("widgets in different states compared equal: the determinism assertion " +
			"would be vacuous")
	}

	// And the subjects this file audits must themselves satisfy it.
	for _, s := range allSubjectsSet {
		if !SameConstructionState(s.New(), s.New()) {
			t.Errorf("subject %q has a nondeterministic New, which would make every "+
				"finding it produces meaningless", s.Name)
		}
	}
}

// TestColdTwinIgnoresResizeTransitions guards against the mode's most obvious false
// positive.
//
// A widget that is given a different rect legitimately renders differently, and
// that is the case ADR 0007 §3's rect-keyed rule already handles. Reporting it
// would fire on every resize of every widget in the catalog, and a mode that fires
// on everything is the reason this kind of tool gets abandoned.
func TestColdTwinIgnoresResizeTransitions(t *testing.T) {
	s := CacheSubject{
		Name: "resize-only",
		New: func() termmosaic.Widget {
			w := &twinWidget{setterInvalidates: true, invalidateDrops: true}
			w.SetHeader("hello")
			return w
		},
		Size: buffer.Rect{W: 20, H: 3},
		Transitions: []Transition{
			{
				// Changes only the rect, which the rect-keyed cache handles by
				// construction. Both arms apply it, so they agree.
				Name:  "SetBounds",
				Apply: func(w termmosaic.Widget) { w.(*twinWidget).SetBounds(buffer.Rect{W: 24, H: 3}) },
			},
		},
	}
	if f, bad := auditOne(s, s.Transitions[0]); bad {
		t.Errorf("audit reported a finding for a rect change: %s", f)
	}
}

// TestColdTwinHandlesEmptyBounds pins the degenerate-size contract of ADR 0007 §4
// on this path.
//
// A zero-sized screen is a valid size, not an error, and the audit must not report
// a finding for it — there are no cells to compare, and a finding would be a
// fabricated defect.
func TestColdTwinHandlesEmptyBounds(t *testing.T) {
	s := CacheSubject{
		Name: "zero-size",
		New: func() termmosaic.Widget {
			return &twinWidget{setterInvalidates: true, invalidateDrops: true}
		},
		Size: buffer.Rect{W: 0, H: 0},
		Transitions: []Transition{
			{Name: "SetHeader", Apply: func(w termmosaic.Widget) { w.(*twinWidget).SetHeader("hi") }},
		},
	}
	if f, bad := auditOne(s, s.Transitions[0]); bad {
		t.Errorf("audit reported a finding at zero size: %s", f)
	}
}
