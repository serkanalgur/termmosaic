package data_test

// Regression tests for the Pager's cache contract, one per defect fixed under
// ADR 0007 §3's amendment. Each is written so that reverting the fix fails it.
//
// The instrument is the cold twin, not a cell assertion. A pager whose layout
// cache survived SetStatus renders a frame that is wrong in a way no golden file
// can describe, because the wrong frame is still a self-consistent pager: the
// status line is missing and the body is the right height for a pager that has no
// status line. Only comparing against a freshly constructed pager in the same
// state can say that the layout is stale rather than merely different.

import (
	"testing"

	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/widgets/data"
	"github.com/serkanalgur/termmosaic/widgets/widgettest"
)

// auditSize is large enough that the status line's row is spent on the status
// rather than dropped by the budget, which is what makes the defect visible: a
// pager too short to show a status line renders identically either way and the
// test would pass on a broken widget.
var auditSize = buffer.Rect{W: 40, H: 12}

func newAuditedPager() *data.Pager {
	p := data.NewPager(auditSize)
	p.SetText("one\ntwo\nthree\nfour\nfive\nsix\nseven\neight\nnine\nten\neleven\ntwelve")
	return p
}

// TestPagerSetStatusInvalidatesTheLayoutCache is the regression test for the
// defect where Status was an exported field read by adapt, with nothing calling
// Invalidate.
//
// It fails without the fix: setting the field leaves cachedRect pointing at the
// rect the layout was solved for, so the frame after the transition keeps the
// status line's row in the body and the cold twin disagrees in every body row.
func TestPagerSetStatusInvalidatesTheLayoutCache(t *testing.T) {
	subject := widgettest.CacheSubject{
		Name: "data.Pager",
		New:  func() termmosaic.Widget { return newAuditedPager() },
		Size: auditSize,
		Transitions: []widgettest.Transition{
			{Name: "SetStatus(false)", Apply: func(w termmosaic.Widget) {
				w.(*data.Pager).SetStatus(false)
			}},
			{Name: "SetStatus(true)", Apply: func(w termmosaic.Widget) {
				w.(*data.Pager).SetStatus(true)
			}},
		},
	}
	if f := widgettest.AuditColdTwin([]widgettest.CacheSubject{subject}, 1); len(f) > 0 {
		for _, finding := range f {
			t.Errorf("regression: %s", finding)
		}
	}
}

// TestPagerStatusIsNotAnExportedField pins the contract half.
//
// The behavioural test above proves the setter invalidates. It cannot prove there
// is no second route to the same state, and an exported field is that route: a
// caller writing `pager.Status = false` gets a pager that is genuinely in the new
// state, so the only thing wrong is the cache, and only a cache's history can tell
// you that. This assertion is what makes the class unreachable rather than merely
// unlikely, and it fails the moment the field is exported again even if the setter
// still works perfectly.
func TestPagerStatusIsNotAnExportedField(t *testing.T) {
	if widgettest.HasExportedField(newAuditedPager(), "Status") {
		t.Error("Pager.Status is exported: an application can assign it with no way to " +
			"invalidate the layout derived from it. Use SetStatus")
	}
}

// TestPagerSetStatusRoundTrips keeps the getter honest, so a caller can read the
// state back without reaching for the field the audit just unexported.
func TestPagerSetStatusRoundTrips(t *testing.T) {
	p := data.NewPager(auditSize)
	if !p.Status() {
		t.Error("a new Pager should show its status line")
	}
	p.SetStatus(false)
	if p.Status() {
		t.Error("SetStatus(false) did not take effect")
	}
	p.SetStatus(true)
	if !p.Status() {
		t.Error("SetStatus(true) did not take effect")
	}
}
