package render

import (
	"testing"

	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/headless"
)

// discardSink is a Sink that throws the bytes away.
//
// It exists so a benchmark can measure the renderer without the headless screen
// model in the way, which matters because the screen model is the more expensive
// of the two by a wide margin. See BenchmarkProbeRenderCostVersusScreenModel.
type discardSink struct{}

func (discardSink) Write(p []byte) (int, error) { return len(p), nil }
func (discardSink) Flush() error                { return nil }

var _ termmosaic.Sink = discardSink{}

// BenchmarkProbeForceRepaintDiscardSink is a full repaint of a 200x60 screen with
// the byte sink discarded: the renderer's own cost, with no test harness in the
// measurement.
func BenchmarkProbeForceRepaintDiscardSink(b *testing.B) {
	r := New(discardSink{}, Config{Width: 200, Height: 60, Caps: termmosaic.DefaultCaps()})
	r.SetRoot(&block{bounds: buffer.Rect{W: 200, H: 60}, text: "chrome", fg: buffer.NewColour(255, 255, 255)})
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if err := r.Reset(); err != nil {
			b.Fatal(err)
		}
		if _, err := r.Render(); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkProbeRenderCostVersusScreenModel renders the same forced full repaint
// into a MemorySink and into the discard sink, so the difference is the cost of
// interpreting the emitted bytes back into cells.
//
// This matters for how widget tests will feel. The renderer is the thing that
// has to fit in a 16 ms frame budget; the screen model is only ever in a test,
// but it is several times more expensive than the renderer, so a suite that
// renders thousands of frames will be dominated by assertion bookkeeping rather
// than by the code under test.
func BenchmarkProbeRenderCostVersusScreenModel(b *testing.B) {
	b.Run("memory-sink", func(b *testing.B) {
		bSink := headless.NewMemorySink(200, 60)
		bSink.KeepBytes(1 << 20)
		r := New(bSink, Config{Width: 200, Height: 60, Caps: termmosaic.DefaultCaps()})
		r.SetRoot(&block{bounds: buffer.Rect{W: 200, H: 60}, text: "chrome", fg: buffer.NewColour(255, 255, 255)})
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if err := r.Reset(); err != nil {
				b.Fatal(err)
			}
			if _, err := r.Render(); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("discard-sink", func(b *testing.B) {
		r := New(discardSink{}, Config{Width: 200, Height: 60, Caps: termmosaic.DefaultCaps()})
		r.SetRoot(&block{bounds: buffer.Rect{W: 200, H: 60}, text: "chrome", fg: buffer.NewColour(255, 255, 255)})
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if err := r.Reset(); err != nil {
				b.Fatal(err)
			}
			if _, err := r.Render(); err != nil {
				b.Fatal(err)
			}
		}
	})
}
