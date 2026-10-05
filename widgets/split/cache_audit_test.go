package split_test

// Regression tests for the Split's cache contract, one per defect fixed under
// ADR 0007 §3's amendment.
//
// The defect here was a CONTRACT defect, not a broken setter: SetSpacing was
// already correct and already marked the cached solve dirty. What was wrong was
// that Spacing was ALSO an exported field, so the same state was reachable by an
// assignment that marked nothing. The behavioural test below therefore cannot be
// the whole proof — it passes on the broken widget too, because the broken route is
// not the route it exercises. The exportedness assertion is the proof, and it is
// written as a separate test so that a reader does not mistake one for the other.

import (
	"testing"

	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/layout"
	"github.com/serkanalgur/termmosaic/widgets/basic"
	"github.com/serkanalgur/termmosaic/widgets/split"
	"github.com/serkanalgur/termmosaic/widgets/widgettest"
)

// auditSize is tall enough for three panes plus a gap, so a spacing change moves
// every pane rather than being absorbed by rounding.
var auditSize = buffer.Rect{W: 40, H: 13}

func newAuditedSplit() *split.Split {
	s := split.New(layout.Vertical,
		basic.NewTextString(buffer.Rect{}, "one", buffer.DefaultStyle),
		basic.NewTextString(buffer.Rect{}, "two", buffer.DefaultStyle),
		basic.NewTextString(buffer.Rect{}, "three", buffer.DefaultStyle),
	)
	s.SetBounds(auditSize)
	return s
}

// TestSplitSetSpacingInvalidatesTheSolve pins the setter half of the contract:
// changing the gap re-solves the panes at the same bounds.
//
// It fails if SetSpacing ever stops setting dirty — the regression this guards
// against is not hypothetical: the field being exported is exactly what let an
// equivalent change reach the widget without it.
func TestSplitSetSpacingInvalidatesTheSolve(t *testing.T) {
	subject := widgettest.CacheSubject{
		Name: "split.Split",
		New:  func() termmosaic.Widget { return newAuditedSplit() },
		Size: auditSize,
		Transitions: []widgettest.Transition{
			{Name: "SetSpacing(3)", Apply: func(w termmosaic.Widget) {
				w.(*split.Split).SetSpacing(3)
			}},
		},
	}
	if f := widgettest.AuditColdTwin([]widgettest.CacheSubject{subject}, 1); len(f) > 0 {
		for _, finding := range f {
			t.Errorf("regression: %s", finding)
		}
	}
}

// TestSplitSpacingIsNotAnExportedField is the proof that the defect is fixed.
//
// Re-exporting the field fails this test immediately, whether or not every setter
// works. That is the only assertion that distinguishes "the documented route
// works" from "there is no undocumented route", and the second is the property
// ADR 0007 §3 is actually asking for.
func TestSplitSpacingIsNotAnExportedField(t *testing.T) {
	if widgettest.HasExportedField(newAuditedSplit(), "Spacing") {
		t.Error("Split.Spacing is exported: an application can assign it with no way to " +
			"mark the cached solve dirty, so the panes keep their old sizes until the " +
			"next resize. Use SetSpacing")
	}
}

// TestSplitSpacingRoundTrips covers the getter and the clamping SetSpacing
// documents, so the replacement for the field is complete on its own.
func TestSplitSpacingRoundTrips(t *testing.T) {
	s := split.New(layout.Vertical)
	if s.Spacing() != 0 {
		t.Errorf("a new Split should have no gap, got %d", s.Spacing())
	}
	s.SetSpacing(3)
	if s.Spacing() != 3 {
		t.Errorf("SetSpacing(3) did not take effect, got %d", s.Spacing())
	}
	s.SetSpacing(-1)
	if s.Spacing() != 0 {
		t.Errorf("a negative spacing should clamp to zero, got %d", s.Spacing())
	}
}
