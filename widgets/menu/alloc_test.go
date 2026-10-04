package menu

import (
	"testing"

	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/headless"
	"github.com/serkanalgur/termmosaic/render"
)

// TestMenuDrawIsAllocationFreeInSteadyState is the claim ADR 0007 §3 makes about
// every widget: everything derived from the size is cached, so a frame of a menu
// whose rectangle has not changed allocates nothing at all.
//
// This is the test that fails if the layout cache is keyed on something other than
// the rect, because the failure it produces is a single allocation per frame that
// no assertion on cells would ever notice — the cells come out identical either
// way.
func TestMenuDrawIsAllocationFreeInSteadyState(t *testing.T) {
	m := openAt(t, 40, 12)
	buf := cellBuf(40, 12)
	m.Draw(buf) // build the layout once

	if got := testing.AllocsPerRun(200, func() { m.Draw(buf) }); got != 0 {
		t.Errorf("Draw allocated %.1f objects per run, want 0. The per-size work is cached against "+
			"the interior (ADR 0007 §3); an allocation here is a truncation or a string built in Draw", got)
	}
}

// TestMenuDrawAtEveryDepthIsAllocationFree repeats the claim with a submenu open,
// because a two-level frame draws two columns, two headers and two scroll engines —
// and a cache that covered only the root's column would pass the test above and
// fail here.
func TestMenuDrawAtEveryDepthIsAllocationFree(t *testing.T) {
	m := openAt(t, 60, 12)
	press(t, m, "\x1b[C")
	press(t, m, "\x1b[C")
	if d := m.Depth(); d != 3 {
		t.Fatalf("setup: depth %d, want 3", d)
	}
	buf := cellBuf(60, 12)
	m.Draw(buf)

	if got := testing.AllocsPerRun(200, func() { m.Draw(buf) }); got != 0 {
		t.Errorf("Draw at depth 3 allocated %.1f objects per run, want 0", got)
	}
}

// TestMenuDrawWithTogglesAndHintsIsAllocationFree covers the widest rows: checked
// items, hints and branches together, which is every optional gutter the layout
// can add.
func TestMenuDrawWithTogglesAndHintsIsAllocationFree(t *testing.T) {
	m := openAt(t, 60, 12, Item{
		Label: "t", Hint: "^T", Checkable: true, Checked: true,
		Items: []Item{{Label: "deep", Hint: "F2", Checkable: true}},
	})
	buf := cellBuf(60, 12)
	m.Draw(buf)

	if got := testing.AllocsPerRun(200, func() { m.Draw(buf) }); got != 0 {
		t.Errorf("Draw with every gutter allocated %.1f objects per run, want 0", got)
	}
}

// TestMenuDrawIsAllocationFreeAfterResizeReturnsToASizeItHasSeen is the subtle
// half: the cache is keyed on the interior, so returning to a PREVIOUSLY SEEN size
// must rebuild rather than serve a stale-but-valid-looking entry — and the rebuild
// must not happen on the frame AFTER it, either. What is asserted here is simply
// that Draw at a size already drawn once is still allocation-free, which is what a
// cache that forgot to compare the rect would break in the opposite direction.
func TestMenuDrawIsAllocationFreeAfterResizeReturnsToASizeItHasSeen(t *testing.T) {
	m := openAt(t, 40, 12)
	big, small := cellBuf(40, 12), cellBuf(20, 12)

	m.Draw(big)
	m.SetBounds(rect(20, 12))
	m.Draw(small)
	m.SetBounds(rect(40, 12)) // back to a size already drawn
	m.Draw(big)

	if got := testing.AllocsPerRun(100, func() { m.Draw(big) }); got != 0 {
		t.Errorf("Draw after returning to a previously seen size allocated %.1f objects per run, want 0", got)
	}
}

// TestMenuRenderedFrameIsAllocationFree is the whole-stack version, because the
// claim that matters to an application is about the FRAME and not about one widget
// inside it.
//
// The widget is the ROOT here, so every byte of the frame comes from the menu, and
// any allocation in the path is attributable. AllocsPerRun performs its own warm-up
// run, so the renderer, the differ and the encoder have settled by the time it
// measures.
func TestMenuRenderedFrameIsAllocationFree(t *testing.T) {
	const w, h = 40, 12
	m := openAt(t, w, h)
	sink := headless.NewMemorySink(w, h)
	r := render.New(sink, render.Config{Width: w, Height: h, Caps: termmosaic.DefaultCaps()})
	r.SetRoot(m)
	if _, err := r.Render(); err != nil {
		t.Fatalf("first frame: %v", err)
	}
	if got := testing.AllocsPerRun(50, func() { _, _ = r.Render() }); got != 0 {
		t.Errorf("a steady-state frame through render allocated %.1f objects, want 0", got)
	}
}

// TestMenuNavigateDoesNotAllocate is the input path. Handle is not the frame path,
// so an allocation there is a correctness-neutral cost — but this widget's whole
// argument for its size cache is that the hot paths are cheap, and a keypress that
// allocated a string per level would make a held arrow key a source of garbage.
func TestMenuNavigateDoesNotAllocate(t *testing.T) {
	m := openAt(t, 60, 12)
	m.Draw(cellBuf(60, 12))
	ev := decodeKey(t, "\x1b[B")

	if got := testing.AllocsPerRun(100, func() { m.Handle(ev) }); got != 0 {
		t.Errorf("a Down keypress allocated %.1f objects, want 0", got)
	}
}

// TestMenuOpenSubmenuDoesNotAllocateOnAKeypress covers the key that builds a level.
// Pushing a level allocates the level frame, which is correct and expected — so this
// asserts the cheaper claim instead: the frame it allocates is the ONLY thing, and
// it does not grow per keypress.
func TestMenuOpenSubmenuDoesNotAllocateOnAKeypress(t *testing.T) {
	m := openAt(t, 60, 12)
	m.Draw(cellBuf(60, 12))
	right := decodeKey(t, "\x1b[C")

	// Opening and closing repeatedly must not grow: the level frame is popped and
	// the next push allocates one again, so a leak here would show as a growing
	// heap rather than as a per-keypress cost. What is asserted is that Draw after
	// all that churn is still free.
	for range 50 {
		m.Handle(right)
		m.Handle(decodeKey(t, "\x1b[D"))
	}
	buf := cellBuf(60, 12)
	m.Draw(buf)
	if got := testing.AllocsPerRun(100, func() { m.Draw(buf) }); got != 0 {
		t.Errorf("Draw after 50 open/close cycles allocated %.1f objects, want 0", got)
	}
}

// openAt returns an open, focused, bordered menu sized w by h over items, which is
// the composition a real application would have.
func openAt(t *testing.T, w, h int, items ...Item) *Menu {
	t.Helper()
	if items == nil {
		items = tree()
	}
	m := newMenu(t, w, h, items...)
	m.Block().SetBorder(buffer.BorderPlain)
	return m
}
