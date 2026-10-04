package main

import (
	"context"
	"strings"
	"testing"

	"github.com/serkanalgur/termmosaic/widgets/widgettest"
)

func TestProbeTmp(t *testing.T) {
	m, _ := newOfflineSource().Fetch(context.Background())
	snap := &snapshot{m: m, at: at, source: "offline", ok: true, gen: 1}
	d := newDashboard(132, 40)
	d.SetFrame(buildFrame(snap))
	render := widgettest.Render(t, 132, 40, 2, d)
	t.Log("\n" + widgettest.Screen(render))
	// tab to table, press down twice
	d.Handle(termmosaic_SpecialKey(2))
	t.Log("focus after tab: " + d.FocusLabel())
	t.Log("table focused: " + boolStr(d.table.Focused()))
	t.Log("\n--- help ---\n" + widgettest.Screen(widgettest.Render(t, 132, 40, 1, d)))
	for _, l := range strings.Split(widgettest.Screen(widgettest.Render(t, 132, 40, 1, d)), "\n") {
		if strings.Contains(l, "quit") || strings.Contains(l, "ctrl-c") {
			t.Log("FOUND: " + l)
		}
	}
}
