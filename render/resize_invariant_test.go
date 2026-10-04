package render

import (
	"testing"

	"github.com/serkanalgur/termmosaic/geometry"
	"github.com/serkanalgur/termmosaic/headless"
)

// TestResizeNeverBreaksFrameInvariant asserts the property that makes diff's
// checkFrame panic unreachable from the renderer.
//
// checkFrame panics unless Frame.Width/Height match both buffers exactly. That
// is correct for a caller handing the diff mismatched buffers, but the renderer
// must never be such a caller: if Resize let front and back drift apart, every
// frame after a resize would panic instead of rendering.
//
// The invariant is that Resize sizes BOTH buffers to exactly w-by-h, clamping
// negatives first, so they cannot disagree with r.w/r.h or with each other.
// This also pins the degenerate sizes a real terminal can produce during a
// drag-resize or a detach/reattach, including 0x0 and negative input.
func TestResizeNeverBreaksFrameInvariant(t *testing.T) {
	sizes := []geometry.Size{
		{W: 0, H: 0}, {W: 1, H: 1}, {W: 1, H: 40}, {W: 40, H: 1},
		{W: 80, H: 24}, {W: -5, H: -5}, {W: 200, H: 60}, {W: 3, H: 2},
	}
	sink := headless.NewMemorySink(20, 5)
	r := New(sink, Config{Width: 20, Height: 5})
	for _, s := range sizes {
		r.Resize(s.W, s.H)
		if r.front.Width() != r.w || r.front.Width() != r.back.Width() {
			t.Errorf("width drift at %dx%d: front=%d back=%d r.w=%d",
				s.W, s.H, r.front.Width(), r.back.Width(), r.w)
		}
		if r.front.Height() != r.h || r.front.Height() != r.back.Height() {
			t.Errorf("height drift at %dx%d: front=%d back=%d r.h=%d",
				s.W, s.H, r.front.Height(), r.back.Height(), r.h)
		}
		if _, err := r.Render(); err != nil {
			t.Errorf("Render failed after resize to %dx%d: %v", s.W, s.H, err)
		}
	}
}
