package viz_test

// Regression tests for the viz package's cache defects under ADR 0007 §3's
// amendment: four of them, split between setters that forgot to invalidate and
// exported fields that gave an application a route with no invalidation at all.
//
// BarChart is in this file for one reason only — its SetData defect. Its axis
// label is a separate concern and is not touched here.

import (
	"testing"

	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/widgets/viz"
	"github.com/serkanalgur/termmosaic/widgets/widgettest"
)

// auditTint is a style that differs visibly from the default, so a themed widget's
// cached palette shows up as a cell difference rather than an invisible one.
var auditTint = buffer.Style{FG: buffer.NewColour(255, 0, 0)}

// auditSize is large enough that a label, a value and a percentage all fit: a
// widget too small to draw the region under test renders identically with and
// without it, and the test would pass on a broken widget.
var auditSize = buffer.Rect{W: 60, H: 12}

func newAuditedBarChart() *viz.BarChart {
	c := viz.NewBarChart(auditSize)
	c.SetData([]viz.Datum{
		{Label: "a", Value: 1, Style: auditTint},
		{Label: "b", Value: 2, Style: auditTint},
		{Label: "c", Value: 3, Style: auditTint},
	})
	return c
}

func newAuditedMeter() *viz.Meter {
	m := viz.NewMeter(auditSize)
	m.SetZones([]viz.Zone{
		{Name: "ok", From: 0, To: 0.5, Style: auditTint, FillStyle: auditTint},
		{Name: "critical", From: 0.5, To: 1.0, Style: auditTint, FillStyle: auditTint},
	})
	m.Set(0.5)
	return m
}

func newAuditedProgressBar() *viz.ProgressBar {
	p := viz.NewProgressBar(auditSize)
	p.SetLabel("progress", auditTint)
	return p
}

func newAuditedSparkline() *viz.Sparkline {
	s := viz.NewSparkline(auditSize)
	s.SetValues([]float64{1, 2, 3, 4, 5, 6, 7, 8})
	return s
}

// assertNoColdTwinFinding runs one subject through the cold-twin check and fails
// naming the finding. Every test here is one subject and one transition, so a
// shared helper would obscure rather than deduplicate — the finding's own String is
// the report a maintainer reads.
func assertNoColdTwinFinding(t *testing.T, name string, newWidget func() termmosaic.Widget,
	transitions ...widgettest.Transition,
) {
	t.Helper()
	subject := widgettest.CacheSubject{
		Name:        name,
		New:         newWidget,
		Size:        auditSize,
		Transitions: transitions,
	}
	for _, finding := range widgettest.AuditColdTwin([]widgettest.CacheSubject{subject}, 1) {
		t.Errorf("regression: %s", finding)
	}
}

// TestBarChartSetDataInvalidatesTheLayoutCache covers the defect where SetData
// replaced the series without dropping cachedRect, so adapt kept the old column
// widths and the old scale.
//
// It fails without the fix: the bars are drawn to the previous series' maximum in
// the previous series' columns, and because a longer label and a fourth datum both
// change the solve, the cold twin disagrees in most of the chart.
func TestBarChartSetDataInvalidatesTheLayoutCache(t *testing.T) {
	assertNoColdTwinFinding(t, "viz.BarChart", func() termmosaic.Widget { return newAuditedBarChart() },
		widgettest.Transition{Name: "SetData", Apply: func(w termmosaic.Widget) {
			w.(*viz.BarChart).SetData([]viz.Datum{
				{Label: "a much longer label", Value: 5, Style: auditTint},
				{Label: "b", Value: 2, Style: auditTint},
				{Label: "c", Value: 3, Style: auditTint},
				{Label: "d", Value: 4, Style: auditTint},
			})
		}})
}

// TestSparklineSetValuesInvalidatesTheNormalisation covers the defect where
// SetValues replaced the series without dropping the normalisation cache.
//
// This is the worst-shaped instance of the class in the catalog. A sparkline drawn
// through the wrong range still looks like a sparkline: it shows a plausible
// shape for the wrong data, with nothing on screen to say so. Removing the reset
// makes the cold twin disagree on the first plotted cell.
func TestSparklineSetValuesInvalidatesTheNormalisation(t *testing.T) {
	assertNoColdTwinFinding(t, "viz.Sparkline", func() termmosaic.Widget { return newAuditedSparkline() },
		widgettest.Transition{Name: "SetValues", Apply: func(w termmosaic.Widget) {
			w.(*viz.Sparkline).SetValues([]float64{8, 7, 6, 5, 4, 3, 2, 1, 9, 10, 11, 12})
		}})
}

// TestMeterSetShowValueInvalidatesTheLayoutCache covers the defect where
// ShowValue was an exported bool read by adapt.
//
// Hiding the number gives its four reserved cells back to the bar, so the stale
// budget leaves the bar in its old place and the number painted over the zone
// name. Both wrongnesses are visible, which is what made this one worth an audit
// finding rather than a shrug.
func TestMeterSetShowValueInvalidatesTheLayoutCache(t *testing.T) {
	assertNoColdTwinFinding(t, "viz.Meter", func() termmosaic.Widget { return newAuditedMeter() },
		widgettest.Transition{Name: "SetShowValue", Apply: func(w termmosaic.Widget) {
			w.(*viz.Meter).SetShowValue(false)
		}})
}

// TestMeterShowValueIsNotAnExportedField pins the contract half of that fix.
//
// The behavioural test above passes on the broken widget as well, because the
// broken route — assignment — is not the route it exercises. This assertion is
// what makes the defect unreachable rather than merely untested.
func TestMeterShowValueIsNotAnExportedField(t *testing.T) {
	if widgettest.HasExportedField(newAuditedMeter(), "ShowValue") {
		t.Error("Meter.ShowValue is exported: an application can assign it with no way to " +
			"re-run adapt, leaving the bar sized against a budget that had no room for " +
			"the value. Use SetShowValue")
	}
}

// TestProgressBarSettersInvalidateTheLayoutCache covers both ProgressBar defects
// at once, because they are the same defect in two fields and one fix: the label
// and the percentage are both budgeted regions, and both were reachable by an
// exported field with no invalidation.
//
// The percentage is the awkward one. SetLabel already dropped the cache correctly,
// so a bar configured through its setters was right and a bar configured through
// its fields was wrong, and the API taught the wrong lesson by having both.
// Unexporting the fields removes the choice.
func TestProgressBarSettersInvalidateTheLayoutCache(t *testing.T) {
	assertNoColdTwinFinding(t, "viz.ProgressBar", func() termmosaic.Widget { return newAuditedProgressBar() },
		widgettest.Transition{Name: "SetLabel", Apply: func(w termmosaic.Widget) {
			w.(*viz.ProgressBar).SetLabel("a longer label", auditTint)
		}},
		widgettest.Transition{Name: "SetPercentage", Apply: func(w termmosaic.Widget) {
			w.(*viz.ProgressBar).SetPercentage(true)
		}},
		widgettest.Transition{Name: "SetLabelSpans", Apply: func(w termmosaic.Widget) {
			w.(*viz.ProgressBar).SetLabelSpans([]buffer.Span{
				buffer.NewSpan("a ", auditTint),
				buffer.NewSpan("much longer label", auditTint),
			})
		}})
}

// TestProgressBarContractFieldsAreNotExported pins both contract halves.
func TestProgressBarContractFieldsAreNotExported(t *testing.T) {
	for _, field := range []string{"Percentage", "Label"} {
		if widgettest.HasExportedField(newAuditedProgressBar(), field) {
			t.Errorf("ProgressBar.%s is exported: an application can assign it with no "+
				"way to re-run the budget, so the bar keeps the width it was solved for. "+
				"Use the setter", field)
		}
	}
}

// TestProgressBarLabelRoundTrips covers the replacement for the label field, so
// unexporting it did not cost the ability to read the label back or to set a
// multi-styled one.
func TestProgressBarLabelRoundTrips(t *testing.T) {
	p := newAuditedProgressBar()
	if got := buffer.SpansWidth(p.Label()); got != len("progress") {
		t.Errorf("Label() should report the label just set, got width %d", got)
	}
	p.SetLabelSpans([]buffer.Span{buffer.NewSpan("ab", auditTint), buffer.NewSpan("cde", auditTint)})
	if got := buffer.SpansWidth(p.Label()); got != 5 {
		t.Errorf("SetLabelSpans should replace the label, got width %d", got)
	}
}
