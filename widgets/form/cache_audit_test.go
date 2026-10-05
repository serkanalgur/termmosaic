package form_test

// Regression test for the one Select defect fixed under ADR 0007 §3's amendment:
// SetMarker changed the marker column without rebuilding the cached rows.
//
// This one is an ordinary setter defect rather than a contract one — the marker
// field is still exported — so the cold-twin comparison is the whole proof and it
// fails cleanly when the rebuild is removed.

import (
	"testing"

	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/widgets/form"
	"github.com/serkanalgur/termmosaic/widgets/widgettest"
)

// auditSize is wide enough that the marker column and every label fit, so the
// comparison measures the marker rather than a truncation of it.
var auditSize = buffer.Rect{W: 40, H: 10}

func newAuditedSelect() *form.Select {
	return form.NewSelect(auditSize, []string{
		"alpha", "beta", "gamma", "delta", "epsilon", "zeta",
	})
}

// TestSelectSetMarkerInvalidatesTheRowCache fails without the fix.
//
// The marker is a column of its own and every label starts after it, so a wider
// marker moves all of them. Without rebuildCache the cached rows keep the old
// column widths and the cold twin disagrees on the first row and every row below.
func TestSelectSetMarkerInvalidatesTheRowCache(t *testing.T) {
	subject := widgettest.CacheSubject{
		Name: "form.Select",
		New:  func() termmosaic.Widget { return newAuditedSelect() },
		Size: auditSize,
		Transitions: []widgettest.Transition{
			{Name: "SetMarker", Apply: func(w termmosaic.Widget) {
				w.(*form.Select).SetMarker("==> ")
			}},
		},
	}
	if f := widgettest.AuditColdTwin([]widgettest.CacheSubject{subject}, 1); len(f) > 0 {
		for _, finding := range f {
			t.Errorf("regression: %s", finding)
		}
	}
}
