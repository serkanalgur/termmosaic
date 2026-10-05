// Package cacheaudit runs ADR 0007 §3's cache audit across the whole widget
// catalog.
//
// It exists as a package rather than as a test in widgets/widgettest because it
// has to import all eight widget packages at once, and a test inside any one of
// them could not. It imports them in one direction only — catalog outward, harness
// inward — which is the same direction as any application, so the audit exercises
// the real import graph rather than a privileged one.
//
// The audit is a GATE. Every finding fails the build.
//
// It was report-only when it was written, because the catalog was not clean and a
// gate that fails gets deleted rather than fixed. It is report-only no longer:
// the eight findings it found are fixed, and an audit that only logs is an audit
// that will find the ninth and say nothing. It runs on every `go test ./...`, so
// the gate is enforced on every push without any change to CI — see the
// recommendation in the package comment above the test.
package cacheaudit

import (
	"testing"

	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/geometry"
	"github.com/serkanalgur/termmosaic/layout"
	"github.com/serkanalgur/termmosaic/widgets/basic"
	"github.com/serkanalgur/termmosaic/widgets/block"
	"github.com/serkanalgur/termmosaic/widgets/data"
	"github.com/serkanalgur/termmosaic/widgets/dialog"
	"github.com/serkanalgur/termmosaic/widgets/form"
	"github.com/serkanalgur/termmosaic/widgets/menu"
	"github.com/serkanalgur/termmosaic/widgets/split"
	"github.com/serkanalgur/termmosaic/widgets/viz"
	"github.com/serkanalgur/termmosaic/widgets/widgettest"
)

// seed is fixed so a run is reproducible from its own name. See the determinism
// discussion in widgets/widgettest/cache_audit.go.
const seed = 20261005

// screen is the rect every subject is placed at. Large enough that a border, a
// title, a scrollbar and a status line all fit, because a widget that only caches
// at some sizes would otherwise pass by being too small to draw the field at all.
var screen = buffer.Rect{W: 60, H: 12}

// tint is a style that differs visibly from the default, so a themed widget that
// caches its palette shows up as a cell difference rather than as an invisible
// byte difference in a style the comparison already ignores.
var tint = buffer.Style{FG: buffer.NewColour(255, 0, 0)}

// subjects is the catalog, one entry per widget, each with the transitions that
// ADR 0007 §3's amendment names: things a setter can change that Draw reads and
// that are NOT the rect.
//
// Every transition goes through the widget's PUBLIC setter, exactly as an
// application would. The audit is testing the catalog as written, so it must not
// reach past the setter to fix up what it suspects — doing that would hide the
// defect rather than report it.
func subjects() []widgettest.CacheSubject {
	return []widgettest.CacheSubject{
		// ---- basic ----
		{
			Name: "basic.Text",
			New: func() termmosaic.Widget {
				return basic.NewTextString(screen, "hello", tint)
			},
			Size: screen,
			Transitions: []widgettest.Transition{
				{Name: "SetText", Apply: func(w termmosaic.Widget) {
					w.(*basic.Text).SetText("a considerably longer run of text", tint)
				}},
				{Name: "SetBackground", Apply: func(w termmosaic.Widget) {
					w.(*basic.Text).SetBackground(tint)
				}},
				{Name: "SetAlign", Apply: func(w termmosaic.Widget) {
					w.(*basic.Text).SetAlign(geometry.AlignRight)
				}},
			},
		},
		{
			Name: "basic.Paragraph",
			New: func() termmosaic.Widget {
				return basic.NewParagraphString(screen, "hello world", tint)
			},
			Size: screen,
			Transitions: []widgettest.Transition{
				{Name: "SetSpans", Apply: func(w termmosaic.Widget) {
					w.(*basic.Paragraph).SetSpans([]buffer.Span{
						buffer.NewSpan("a much longer body of text that must wrap differently", tint),
					})
				}},
			},
		},

		// ---- block ----
		{
			Name: "block.Block",
			New: func() termmosaic.Widget {
				b := block.New(screen)
				b.SetBorder(buffer.BorderPlain)
				b.SetBorderStyle(tint)
				b.SetBackground(tint)
				return b
			},
			Size: screen,
			Transitions: []widgettest.Transition{
				// SetTitle is the interesting one: Block caches the TRUNCATED title
				// against the rect it was computed for (block.go capped/cappedRect),
				// so a title change at an unchanged rect is exactly the case ADR 0007
				// §3's amendment describes.
				{Name: "SetTitle", Apply: func(w termmosaic.Widget) {
					w.(*block.Block).SetTitleString("a very long title indeed", tint)
				}},
				{Name: "SetBorderStyle", Apply: func(w termmosaic.Widget) {
					w.(*block.Block).SetBorderStyle(tint)
				}},
				{Name: "SetPadding", Apply: func(w termmosaic.Widget) { w.(*block.Block).SetPadding(2) }},
				{Name: "SetBorder", Apply: func(w termmosaic.Widget) {
					w.(*block.Block).SetBorder(buffer.BorderDouble)
				}},
				{Name: "SetTitleAlign", Apply: func(w termmosaic.Widget) {
					w.(*block.Block).SetTitleAlign(geometry.AlignRight)
				}},
			},
		},

		// ---- data ----
		{
			Name: "data.List",
			New: func() termmosaic.Widget {
				return data.NewList(screen,
					data.ListItem{Label: "alpha"}, data.ListItem{Label: "beta"},
					data.ListItem{Label: "gamma"}, data.ListItem{Label: "delta"},
					data.ListItem{Label: "epsilon"}, data.ListItem{Label: "zeta"},
					data.ListItem{Label: "eta"}, data.ListItem{Label: "theta"},
					data.ListItem{Label: "iota"}, data.ListItem{Label: "kappa"},
					data.ListItem{Label: "lambda"}, data.ListItem{Label: "mu"},
					data.ListItem{Label: "nu"}, data.ListItem{Label: "xi"},
					data.ListItem{Label: "omicron"}, data.ListItem{Label: "pi"},
				)
			},
			Size: screen,
			Transitions: []widgettest.Transition{
				{Name: "SetItems", Apply: func(w termmosaic.Widget) {
					w.(*data.List).SetItems([]data.ListItem{
						{Label: "a much longer item label"},
						{Label: "second"}, {Label: "third"},
					})
				}},
				{Name: "Scrollbar", Apply: func(w termmosaic.Widget) { w.(*data.List).Scrollbar = false }},
				{Name: "Select", Apply: func(w termmosaic.Widget) { w.(*data.List).Select(3) }},
			},
		},
		{
			Name: "data.Table",
			New: func() termmosaic.Widget {
				return data.NewTable(screen,
					data.Column{Title: []buffer.Span{buffer.NewSpan("alpha", tint)}, Grow: 1},
					data.Column{Title: []buffer.Span{buffer.NewSpan("beta", tint)}, Grow: 1},
					data.Column{Title: []buffer.Span{buffer.NewSpan("gamma", tint)}, Grow: 1},
				)
			},
			Size: screen,
			Transitions: []widgettest.Transition{
				{Name: "Scrollbar", Apply: func(w termmosaic.Widget) { w.(*data.Table).Scrollbar = false }},
				{Name: "Header", Apply: func(w termmosaic.Widget) { w.(*data.Table).Header = false }},
				{Name: "SetRows", Apply: func(w termmosaic.Widget) {
					w.(*data.Table).SetRows([]data.Row{
						{Cells: []data.Cell{{Text: "a much longer cell value", Style: tint}}},
						{Cells: []data.Cell{{Text: "b", Style: tint}, {Text: "c", Style: tint}}},
						{Cells: []data.Cell{{Text: "d", Style: tint}, {Text: "e", Style: tint}}},
						{Cells: []data.Cell{{Text: "f", Style: tint}, {Text: "g", Style: tint}}},
						{Cells: []data.Cell{{Text: "h", Style: tint}, {Text: "i", Style: tint}}},
					})
				}},
			},
		},
		{
			Name: "data.Tree",
			New: func() termmosaic.Widget {
				return data.NewTree(screen,
					data.Node{Label: "root", Children: []data.Node{{Label: "child"}}},
					data.Node{Label: "second"}, data.Node{Label: "third"},
					data.Node{Label: "fourth"}, data.Node{Label: "fifth"},
					data.Node{Label: "sixth"}, data.Node{Label: "seventh"},
					data.Node{Label: "eighth"}, data.Node{Label: "ninth"},
					data.Node{Label: "tenth"}, data.Node{Label: "eleventh"},
					data.Node{Label: "twelfth"}, data.Node{Label: "thirteenth"},
					data.Node{Label: "fourteenth"}, data.Node{Label: "fifteenth"},
				)
			},
			Size: screen,
			Transitions: []widgettest.Transition{
				{Name: "Scrollbar", Apply: func(w termmosaic.Widget) { w.(*data.Tree).Scrollbar = false }},
				{Name: "SetNodes", Apply: func(w termmosaic.Widget) {
					w.(*data.Tree).SetNodes([]data.Node{
						{Label: "a much longer replacement node"},
						{Label: "second"},
					})
				}},
			},
		},
		{
			Name: "data.Pager",
			New: func() termmosaic.Widget {
				p := data.NewPager(screen)
				p.SetText("page one")
				return p
			},
			Size: screen,
			Transitions: []widgettest.Transition{
				{Name: "SetText", Apply: func(w termmosaic.Widget) {
					w.(*data.Pager).SetText("a different page of text entirely")
				}},
				{Name: "SetStatus", Apply: func(w termmosaic.Widget) { w.(*data.Pager).SetStatus(false) }},
			},
		},

		// ---- dialog ----
		{
			Name: "dialog.Dialog",
			New: func() termmosaic.Widget {
				d := dialog.New(screen, dialog.VariantInfo)
				d.SetTitle("title", tint)
				d.SetBodyString("a message")
				return d
			},
			Size: screen,
			Transitions: []widgettest.Transition{
				{Name: "SetTitle", Apply: func(w termmosaic.Widget) {
					w.(*dialog.Dialog).SetTitle("a much longer title", tint)
				}},
				{Name: "SetBodyString", Apply: func(w termmosaic.Widget) {
					w.(*dialog.Dialog).SetBodyString("another message entirely, and longer")
				}},
			},
		},

		// ---- form ----
		{
			Name: "form.TextInput",
			New: func() termmosaic.Widget {
				t := form.NewTextInput(screen)
				t.SetText("hello")
				t.Placeholder = "type here"
				return t
			},
			Size: screen,
			Transitions: []widgettest.Transition{
				{Name: "SetText", Apply: func(w termmosaic.Widget) {
					w.(*form.TextInput).SetText("a much longer value than before")
				}},
				{Name: "Placeholder", Apply: func(w termmosaic.Widget) {
					w.(*form.TextInput).Placeholder = "some other placeholder"
				}},
			},
		},
		{
			Name: "form.TextArea",
			New: func() termmosaic.Widget {
				a := form.NewTextArea(screen)
				a.SetText("hello")
				return a
			},
			Size: screen,
			Transitions: []widgettest.Transition{
				{Name: "SetText", Apply: func(w termmosaic.Widget) {
					w.(*form.TextArea).SetText("a much longer value than before, on several lines")
				}},
			},
		},
		{
			Name: "form.Button",
			New:  func() termmosaic.Widget { return form.NewButton(screen, "press") },
			Size: screen,
			Transitions: []widgettest.Transition{
				{Name: "SetLabel", Apply: func(w termmosaic.Widget) {
					w.(*form.Button).SetLabel("a much longer label")
				}},
				{Name: "SetDisabled", Apply: func(w termmosaic.Widget) { w.(*form.Button).SetDisabled(true) }},
			},
		},
		{
			Name: "form.Checkbox",
			New:  func() termmosaic.Widget { return form.NewCheckbox(screen, "check me") },
			Size: screen,
			Transitions: []widgettest.Transition{
				{Name: "SetLabel", Apply: func(w termmosaic.Widget) {
					w.(*form.Checkbox).SetLabel("a much longer label")
				}},
				{Name: "SetState", Apply: func(w termmosaic.Widget) {
					w.(*form.Checkbox).SetState(form.Checked)
				}},
				{Name: "TriState", Apply: func(w termmosaic.Widget) { w.(*form.Checkbox).TriState = true }},
			},
		},
		{
			Name: "form.Toggle",
			New:  func() termmosaic.Widget { return form.NewToggle(screen, "toggle me") },
			Size: screen,
			Transitions: []widgettest.Transition{
				{Name: "SetLabel", Apply: func(w termmosaic.Widget) {
					w.(*form.Toggle).SetLabel("a much longer label")
				}},
				{Name: "SetOn", Apply: func(w termmosaic.Widget) { w.(*form.Toggle).SetOn(true) }},
			},
		},
		{
			Name: "form.Radio",
			New: func() termmosaic.Widget {
				return form.NewRadio(screen, []string{"alpha", "beta", "gamma", "delta", "epsilon"})
			},
			Size: screen,
			Transitions: []widgettest.Transition{
				{Name: "SetLabels", Apply: func(w termmosaic.Widget) {
					w.(*form.Radio).SetLabels([]string{
						"a much longer option label", "second", "third", "fourth", "fifth",
					})
				}},
				{Name: "SetSelected", Apply: func(w termmosaic.Widget) { w.(*form.Radio).SetSelected(1) }},
			},
		},
		{
			Name: "form.Select",
			New: func() termmosaic.Widget {
				return form.NewSelect(screen, []string{"alpha", "beta", "gamma", "delta", "epsilon"})
			},
			Size: screen,
			Transitions: []widgettest.Transition{
				{Name: "SetLabels", Apply: func(w termmosaic.Widget) {
					w.(*form.Select).SetLabels([]string{
						"a much longer option label", "second", "third", "fourth", "fifth",
					})
				}},
				{Name: "SetMarker", Apply: func(w termmosaic.Widget) { w.(*form.Select).SetMarker("==> ") }},
			},
		},
		{
			Name: "form.Tabs",
			New: func() termmosaic.Widget {
				return form.NewTabs(screen, []string{"alpha", "beta", "gamma", "delta", "epsilon"})
			},
			Size: screen,
			Transitions: []widgettest.Transition{
				{Name: "SetTabs", Apply: func(w termmosaic.Widget) {
					w.(*form.Tabs).SetTabs([]string{
						"a much longer tab label", "second", "third", "fourth", "fifth",
					})
				}},
			},
		},
		{
			Name: "form.KeyHint",
			New: func() termmosaic.Widget {
				return form.NewKeyHint(screen, []form.Binding{
					form.NewBinding("ctrl-c", "quit"),
					form.NewBinding("ctrl-s", "save"),
				})
			},
			Size: screen,
			Transitions: []widgettest.Transition{
				{Name: "SetBindings", Apply: func(w termmosaic.Widget) {
					w.(*form.KeyHint).SetBindings([]form.Binding{
						form.NewBinding("ctrl-d", "something else entirely"),
						form.NewBinding("ctrl-e", "another"),
					})
				}},
			},
		},

		// ---- menu ----
		{
			Name: "menu.Menu",
			New: func() termmosaic.Widget {
				return menu.New(screen,
					menu.Item{Label: "alpha"}, menu.Item{Label: "beta"},
					menu.Item{Label: "gamma"}, menu.Item{Label: "delta"},
					menu.Item{Label: "epsilon"}, menu.Item{Label: "zeta"},
				)
			},
			Size: screen,
			Transitions: []widgettest.Transition{
				{Name: "SetItems", Apply: func(w termmosaic.Widget) {
					w.(*menu.Menu).SetItems([]menu.Item{
						{Label: "a much longer item label"},
						{Label: "second"},
					})
				}},
			},
		},

		// ---- split ----
		{
			Name: "split.Split",
			New: func() termmosaic.Widget {
				s := split.New(layout.Vertical,
					basic.NewTextString(screen, "left pane", tint),
					basic.NewTextString(screen, "right pane", tint),
				)
				s.SetBounds(screen)
				return s
			},
			Size: screen,
			Transitions: []widgettest.Transition{
				{Name: "SetSpacing", Apply: func(w termmosaic.Widget) { w.(*split.Split).SetSpacing(3) }},
			},
		},

		// ---- viz ----
		{
			Name: "viz.Meter",
			New: func() termmosaic.Widget {
				m := viz.NewMeter(screen)
				m.SetZones([]viz.Zone{
					{Name: "ok", From: 0, To: 0.5, Style: tint, FillStyle: tint},
					{Name: "critical", From: 0.5, To: 1.0, Style: tint, FillStyle: tint},
				})
				m.Set(0.5)
				return m
			},
			Size: screen,
			Transitions: []widgettest.Transition{
				{Name: "Set", Apply: func(w termmosaic.Widget) { w.(*viz.Meter).Set(0.9) }},
				{Name: "SetZones", Apply: func(w termmosaic.Widget) {
					w.(*viz.Meter).SetZones([]viz.Zone{
						{Name: "a much longer zone name", From: 0, To: 0.25, Style: tint, FillStyle: tint},
						{Name: "second", From: 0.25, To: 1.0, Style: tint, FillStyle: tint},
					})
				}},
				{Name: "SetShowValue", Apply: func(w termmosaic.Widget) { w.(*viz.Meter).SetShowValue(false) }},
				{Name: "Threshold", Apply: func(w termmosaic.Widget) { w.(*viz.Meter).Threshold = 0.25 }},
			},
		},
		{
			Name: "viz.Gauge",
			New: func() termmosaic.Widget {
				g := viz.NewGauge(screen)
				g.Value = 0.5
				return g
			},
			Size: screen,
			Transitions: []widgettest.Transition{
				{Name: "Value", Apply: func(w termmosaic.Widget) { w.(*viz.Gauge).Set(0.9) }},
				{Name: "ShowLabel", Apply: func(w termmosaic.Widget) { w.(*viz.Gauge).ShowLabel = false }},
			},
		},
		{
			Name: "viz.ProgressBar",
			New: func() termmosaic.Widget {
				p := viz.NewProgressBar(screen)
				p.SetLabel("progress", tint)
				return p
			},
			Size: screen,
			Transitions: []widgettest.Transition{
				{Name: "SetPercentage", Apply: func(w termmosaic.Widget) { w.(*viz.ProgressBar).SetPercentage(true) }},
				{Name: "SetLabel", Apply: func(w termmosaic.Widget) {
					w.(*viz.ProgressBar).SetLabel("a longer label", tint)
				}},
			},
		},
		{
			Name: "viz.BarChart",
			New: func() termmosaic.Widget {
				c := viz.NewBarChart(screen)
				c.Data = []viz.Datum{
					{Label: "a", Value: 1, Style: tint},
					{Label: "b", Value: 2, Style: tint},
					{Label: "c", Value: 3, Style: tint},
				}
				return c
			},
			Size: screen,
			Transitions: []widgettest.Transition{
				{Name: "Data", Apply: func(w termmosaic.Widget) {
					w.(*viz.BarChart).SetData([]viz.Datum{
						{Label: "a much longer label", Value: 5, Style: tint},
						{Label: "b", Value: 2, Style: tint},
						{Label: "c", Value: 3, Style: tint},
						{Label: "d", Value: 4, Style: tint},
					})
				}},
				{Name: "Vertical", Apply: func(w termmosaic.Widget) { w.(*viz.BarChart).Vertical = true }},
			},
		},
		{
			Name: "viz.Sparkline",
			New: func() termmosaic.Widget {
				s := viz.NewSparkline(screen)
				s.Values = []float64{1, 2, 3, 4, 5, 6, 7, 8}
				return s
			},
			Size: screen,
			Transitions: []widgettest.Transition{
				{Name: "Values", Apply: func(w termmosaic.Widget) {
					w.(*viz.Sparkline).SetValues([]float64{8, 7, 6, 5, 4, 3, 2, 1, 9, 10, 11, 12})
				}},
				{Name: "Braille", Apply: func(w termmosaic.Widget) { w.(*viz.Sparkline).Braille = true }},
			},
		},
	}
}

// TestCatalogCacheAudit is the audit, and it gates.
//
// Every finding fails the test, with its subject, transition, cell count and first
// differing cell — which is what a maintainer needs in order to go and look at the
// code. Each finding names the field that was changed, so the report points at the
// setter rather than at the symptom.
//
// # Reading a finding
//
// A finding is a claim that a widget which has just been transitioned renders
// different cells from a freshly constructed widget in exactly the same state.
// That claim has two possible causes, and the fix differs for each:
//
//   - The SETTER forgot to invalidate. The fix is one line in that setter, and the
//     widget keeps its exported field.
//
//   - The field was EXPORTED, so the transition reached it by assignment and there
//     was no invalidation to perform. The fix is to unexport the field and add a
//     setter that invalidates. That is a breaking API change, and it is the
//     deliberate trade: an exported field whose Draw-derived cache the caller must
//     remember to invalidate is a rule with no enforcement, and the bug it produces
//     is permanent — no future resize repairs it, because the cache key does not
//     change.
//
// Both are the class ADR 0007 §3's amendment describes. Neither is caught by
// review, which is the whole reason the mode exists.
//
// # What this gate does and does not cover
//
// It covers the transitions listed in subjects(), which is catalog knowledge and
// has to be written by hand. A new widget that is added to a package but not to
// subjects() is not audited, and a new cached field on an audited widget is not
// audited until a transition names it. The gate is a floor, not a proof; that is
// a limitation of having a human write the schedule, and the honest statement of
// it belongs here rather than in a comment that implies more coverage than exists.
func TestCatalogCacheAudit(t *testing.T) {
	subjects := subjects()
	for _, s := range subjects {
		// Every subject must be deterministic, or its findings are meaningless.
		// Asserted here rather than inside the audit so the failure names the
		// subject instead of arriving as an unexplained cell mismatch.
		if !widgettest.SameConstructionState(s.New(), s.New()) {
			t.Errorf("%s: New is not deterministic, so this subject's results would be "+
				"meaningless", s.Name)
		}
	}

	findings := widgettest.AuditColdTwin(subjects, seed)

	t.Logf("audited %d widgets from widgets/{basic,block,data,dialog,form,menu,split,viz} "+
		"with seed %d", len(subjects), seed)
	for _, f := range findings {
		t.Errorf("cache defect: %s", f)
	}
	t.Logf("total: %d finding(s)", len(findings))
}

// TestContractFieldsAreNotExported pins the other half of the fix above.
//
// The cold-twin comparison proves that a setter invalidates. It cannot prove that
// there is no OTHER route to the same state — an exported field is exactly that
// route, and it is invisible to a cell comparison because the field assignment
// produces a widget that is genuinely in the new state; only the cache is stale,
// and only the cache's history differs from the cold twin's. So the exportedness
// is asserted directly.
//
// This is the check that makes the class unreachable rather than merely unlikely.
// Re-exporting any of these fields fails here even if every setter still works,
// which is the point: two working routes and one broken route is how ProgressBar
// ended up in the audit with SetLabel invalidating and its label field not.
func TestContractFieldsAreNotExported(t *testing.T) {
	for _, tc := range []struct {
		widget string
		value  any
		field  string
	}{
		{"data.Pager", data.NewPager(buffer.Rect{}), "Status"},
		{"split.Split", split.New(layout.Vertical), "Spacing"},
		{"viz.Meter", viz.NewMeter(buffer.Rect{}), "ShowValue"},
		{"viz.ProgressBar", viz.NewProgressBar(buffer.Rect{}), "Percentage"},
		{"viz.ProgressBar", viz.NewProgressBar(buffer.Rect{}), "Label"},
	} {
		if widgettest.HasExportedField(tc.value, tc.field) {
			t.Errorf("%s.%s is exported: an application can assign it with no way to "+
				"invalidate the layout derived from it. Use the setter", tc.widget, tc.field)
		}
	}
}
