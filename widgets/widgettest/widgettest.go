// Package widgettest renders widgets through the whole stack in a test, with no
// terminal involved.
//
// It exists because a widget test that calls Draw into a bare buffer proves only
// that the widget does not panic. The thing that actually breaks in this codebase
// is the seam between a widget and the diff: a continuation cell wearing the
// wrong style, a cell the widget did not repaint after a shrink, an SGR sequence
// the encoder emits differently at another depth. All three are invisible unless
// the frame goes through the renderer, the two-tier diff, the ANSI encoder and
// finally the headless screen model, which is what Render does.
//
// So the rule this package exists to make easy is: every widget test renders
// through Render rather than through Draw, and asserts on cells.
package widgettest

import (
	"strings"
	"testing"

	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/headless"
	"github.com/serkanalgur/termmosaic/render"
)

// Render draws frames frames of root on a w-by-h screen and returns the sink.
//
// It fails the test if the encoder emits anything the headless screen model does
// not implement, because a screen reconstructed from an incomplete model makes
// every assertion against it unsound.
func Render(t testing.TB, w, h, frames int, root termmosaic.Widget) *headless.MemorySink {
	t.Helper()
	sink := headless.NewMemorySink(w, h)
	r := render.New(sink, render.Config{Width: w, Height: h, Caps: termmosaic.DefaultCaps()})
	r.SetRoot(root)
	for i := 0; i < frames; i++ {
		if _, err := r.Render(); err != nil {
			t.Fatalf("frame %d: %v", i, err)
		}
	}
	if got := sink.UnknownSequences(); got != 0 {
		t.Fatalf("the headless screen saw %d unrecognised sequences; assertions about it are unsound", got)
	}
	return sink
}

// Screen returns the sink's screen as h lines with trailing spaces trimmed.
//
// Trailing whitespace is invisible on a terminal and would make every expected
// value in a test wrong by however many blanks a block chose to paint, so it is
// removed here rather than in fifty expected strings.
func Screen(sink *headless.MemorySink) string {
	_, h := sink.Size()
	lines := make([]string, h)
	for y := 0; y < h; y++ {
		lines[y] = strings.TrimRight(sink.Line(y), " ")
	}
	return strings.Join(lines, "\n")
}

// Row returns row y of the sink with trailing spaces trimmed.
func Row(sink *headless.MemorySink, y int) string {
	return strings.TrimRight(sink.Line(y), " ")
}
