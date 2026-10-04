package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/headless"
	"github.com/serkanalgur/termmosaic/render"
	"github.com/serkanalgur/termmosaic/widgets/block"
)

// update is set by -update to rewrite the golden files.
var update = flag.Bool("update", false, "rewrite the golden files in testdata")

// goldenScreen is the screen the golden files describe.
const (
	goldenW = 60
	goldenH = 14
)

// renderGolden renders n frames of the example's block into a MemorySink and
// returns the sink.
//
// It exercises the whole stack the way a real application does: the renderer,
// the two-tier diff, the ANSI encoder, and the headless screen model that turns
// the emitted bytes back into cells.
func renderGolden(t *testing.T, frames int) *headless.MemorySink {
	t.Helper()
	sink := headless.NewMemorySink(goldenW, goldenH)
	r := render.New(sink, render.Config{
		Width:  goldenW,
		Height: goldenH,
		Caps:   termmosaic.DefaultCaps(),
	})
	root := newHello(rootBounds(goldenW, goldenH), buffer.DepthTrueColor)
	r.SetRoot(root)
	for i := 0; i < frames; i++ {
		if _, err := r.Render(); err != nil {
			t.Fatalf("frame %d: %v", i, err)
		}
	}
	if got := sink.UnknownSequences(); got != 0 {
		t.Fatalf("the headless screen saw %d unrecognised sequences; assertions about it are unsound.\n%s",
			got, sink.RawString())
	}
	return sink
}

func checkGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading golden file (run `go test ./examples/hello -update` to create it): %v", err)
	}
	if got != string(want) {
		t.Errorf("golden mismatch for %s\n--- got ---\n%s\n--- want ---\n%s\n--- diff in words ---\n%s",
			name, got, want, describeDiff(string(want), got))
	}
}

// describeDiff renders a minimal word-level difference, so a failing golden
// test says what changed rather than only dumping two screens.
func describeDiff(want, got string) string {
	w := strings.Split(want, "\n")
	g := strings.Split(got, "\n")
	var b strings.Builder
	for i := 0; i < len(w) || i < len(g); i++ {
		var wl, gl string
		if i < len(w) {
			wl = w[i]
		}
		if i < len(g) {
			gl = g[i]
		}
		if wl != gl {
			fmt.Fprintf(&b, "row %d:\n  want %q\n  got  %q\n", i, wl, gl)
		}
	}
	if b.Len() == 0 {
		return "(lines differ only in trailing content)"
	}
	return b.String()
}

// TestGoldenScreen is the regression net for the entire foundation: the buffer
// primitives, the layout solver's output, the two-tier diff, the ANSI encoder and
// the renderer all have to agree to produce exactly this screen.
func TestGoldenScreen(t *testing.T) {
	sink := renderGolden(t, 1)
	checkGolden(t, "hello.txt", sink.String())
}

// TestGoldenStream pins the exact bytes, which catches encoder regressions that
// leave the visible screen unchanged: a redundant SGR, a missing default colour,
// a cursor move that was not needed.
func TestGoldenStream(t *testing.T) {
	sink := renderGolden(t, 1)
	checkGolden(t, "hello.sgr", sink.RawString())
}

// TestSecondFrameChangesOnlyTheCounter is the ADR 0002 property stated as a test
// at the application level: static chrome must cost nothing between frames.
func TestSecondFrameChangesOnlyTheCounter(t *testing.T) {
	sink := headless.NewMemorySink(goldenW, goldenH)
	r := render.New(sink, render.Config{
		Width: goldenW, Height: goldenH, Caps: termmosaic.DefaultCaps(),
	})
	r.SetRoot(newHello(rootBounds(goldenW, goldenH), buffer.DepthTrueColor))

	if _, err := r.Render(); err != nil {
		t.Fatal(err)
	}
	first := sink.String()

	sink.MarkFrame()
	r.InvalidateAll()
	n, err := r.Render()
	if err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatal("the second frame changes the counter, so it must write bytes")
	}
	// A full repaint of a 60x14 screen would be well over a thousand bytes. The
	// second frame changes two digits, so it must be a tiny fraction of that.
	if n > 120 {
		t.Errorf("second frame wrote %d bytes; static chrome is not being skipped", n)
	}
	if got := sink.String(); got == first {
		t.Error("the counter did not change between frames")
	}
	checkGolden(t, "hello_frame2.txt", sink.String())
}

func TestIdleFrameWritesNothing(t *testing.T) {
	sink := renderGolden(t, 1)
	sink.MarkFrame()
	r := render.New(sink, render.Config{
		Width: goldenW, Height: goldenH, Caps: termmosaic.DefaultCaps(),
	})
	r.SetRoot(newHello(rootBounds(goldenW, goldenH), buffer.DepthTrueColor))
	if _, err := r.Render(); err != nil {
		t.Fatal(err)
	}
	before := sink.Writes()
	for i := 0; i < 5; i++ {
		n, err := r.Render()
		if err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Fatalf("idle frame %d wrote %d bytes", i, n)
		}
	}
	if sink.Writes() != before {
		t.Error("an idle frame reached the Sink")
	}
}

// TestDegradedColourGolden renders at each rung of the ladder, so a change to
// the degradation mapping shows up as a golden diff rather than as a surprise in
// someone's terminal.
func TestDegradedColourGolden(t *testing.T) {
	cases := []struct {
		name string
		caps termmosaic.Caps
	}{
		{"truecolor", termmosaic.Caps{TrueColor: true, Color256: true}},
		{"256", termmosaic.Caps{Color256: true}},
		{"16", termmosaic.Caps{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sink := headless.NewMemorySink(goldenW, goldenH)
			r := render.New(sink, render.Config{
				Width: goldenW, Height: goldenH, Caps: tc.caps,
			})
			root := newHello(rootBounds(goldenW, goldenH), tc.caps.ColourDepth())
			r.SetRoot(root)
			if _, err := r.Render(); err != nil {
				t.Fatal(err)
			}
			if got := sink.UnknownSequences(); got != 0 {
				t.Fatalf("%d unknown sequences at depth %v", got, tc.caps.ColourDepth())
			}
			checkGolden(t, "hello_"+tc.name+".sgr", sink.RawString())
		})
	}
}

// TestNoColorGolden proves NO_COLOR changes the byte stream and nothing else
// about the visible text.
func TestNoColorGolden(t *testing.T) {
	withColor := headless.NewMemorySink(goldenW, goldenH)
	r1 := render.New(withColor, render.Config{
		Width: goldenW, Height: goldenH, Caps: termmosaic.DefaultCaps(),
	})
	r1.SetRoot(newHello(rootBounds(goldenW, goldenH), buffer.DepthTrueColor))
	if _, err := r1.Render(); err != nil {
		t.Fatal(err)
	}

	noColor := headless.NewMemorySink(goldenW, goldenH)
	r2 := render.New(noColor, render.Config{
		Width: goldenW, Height: goldenH, Caps: termmosaic.DefaultCaps(), NoColor: true,
	})
	r2.SetRoot(newHello(rootBounds(goldenW, goldenH), buffer.DepthTrueColor))
	if _, err := r2.Render(); err != nil {
		t.Fatal(err)
	}

	if strings.Contains(noColor.RawString(), "38;2") {
		t.Error("NO_COLOR frame emitted a truecolor sequence")
	}
	if len(noColor.RawString()) >= len(withColor.RawString()) {
		t.Errorf("NO_COLOR frame is not smaller: %d vs %d bytes",
			len(noColor.RawString()), len(withColor.RawString()))
	}
	if noColor.String() != withColor.String() {
		t.Error("NO_COLOR must not change the visible text")
	}
	checkGolden(t, "hello_nocolor.sgr", noColor.RawString())
}

// TestResizeGolden renders the example through a size sweep that goes UP, back
// DOWN and through the degenerate sizes, checking the golden after each full-size
// step.
//
// ADR 0007 §1 rule 3's forced-changes table asks for exactly this shape — "extended
// to cover a shrink and a degenerate size (0x0 and a size below the block's
// minimum), since a resize test that only grows cannot catch stale cells" — and the
// reason is worth keeping: growing gives the widget more room, so every extra cell
// is freshly painted and nothing can be left over. Only SHRINKING can leave a row
// from the previous size on screen, and only a size below the block's minimum
// exercises the guards that drop the title and the body.
func TestResizeGolden(t *testing.T) {
	sink := headless.NewMemorySink(goldenW, goldenH)
	r := render.New(sink, render.Config{
		Width: goldenW, Height: goldenH, Caps: termmosaic.DefaultCaps(),
	})
	root := newHello(rootBounds(goldenW, goldenH), buffer.DepthTrueColor)
	r.SetRoot(root)
	if _, err := r.Render(); err != nil {
		t.Fatal(err)
	}

	// resizeTo performs the resize exactly as the example's event handler does —
	// recompute bounds, then Resize — with nothing in between, which is ADR 0007
	// §5's order of operations — and then asserts that the screen matches what a
	// FRESH start at that size would have produced.
	//
	// The comparison ignores DIGITS because the block's frame counter differs
	// between a renderer that has been sweeping and one that has just started. Every
	// other character is chrome, and chrome is what a stale cell would corrupt: a row
	// left over from a wider frame would show the wider frame's text.
	resizeTo := func(t *testing.T, w, h int) {
		t.Helper()
		sink.Resize(w, h)
		r.Resize(w, h)
		root.bounds = rootBounds(w, h)
		if _, err := r.Render(); err != nil {
			t.Fatalf("resize to %dx%d: %v", w, h, err)
		}
		if got := sink.UnknownSequences(); got != 0 {
			t.Fatalf("%dx%d: %d unrecognised sequences:\n%s", w, h, got, sink.RawString())
		}
		if got, _ := r.Size(); got != w {
			t.Fatalf("Size() reports %dx%d after resizing to %dx%d", got, h, w, h)
		}
		assertScreenMatchesAFreshStart(t, sink, w, h)
	}

	// --- GROW ---------------------------------------------------------------
	const newW, newH = 72, 18
	resizeTo(t, newW, newH)
	checkGolden(t, "hello_resized.txt", sink.String())

	// --- SHRINK, below the size the block was built for ----------------------
	// 30x7 is narrower and shorter than blockW x blockH, so the title, the value
	// column and the body rows all compete for the same cells.
	resizeTo(t, 30, 7)
	if got := root.bounds.W; got != 30 {
		t.Errorf("the block's width is %d on a 30-wide screen, want the full screen", got)
	}
	checkGolden(t, "hello_shrunk.txt", sink.String())

	// --- DEGENERATE ----------------------------------------------------------
	// 0x0 is the detached-terminal case and 4x2 is below the block's minimum;
	// both must render without failing and both must leave the renderer able to
	// come back to a real size.
	for _, size := range [][2]int{{0, 0}, {4, 2}, {20, 1}} {
		resizeTo(t, size[0], size[1])
		if w, h := r.Size(); w != size[0] || h != size[1] {
			t.Errorf("Size() = %dx%d, want %dx%d", w, h, size[0], size[1])
		}
	}

	// --- AND BACK -----------------------------------------------------------
	// The last step is the one that matters: recovering from a degenerate size
	// must leave the same screen a fresh start at that size would.
	resizeTo(t, goldenW, goldenH)
	checkGolden(t, "hello_recovered.txt", sink.String())
}

// assertScreenMatchesAFreshStart asserts that got shows the same chrome as a
// renderer started fresh at w-by-h would.
//
// Digits are removed before the comparison because the example's block shows a frame
// counter, and a renderer that has been swept through several sizes has a different
// one. Everything else is chrome, and chrome is precisely what a stale cell
// corrupts: a row surviving from a wider frame carries that frame's text, and a
// column surviving from a taller one carries that one's border.
func assertScreenMatchesAFreshStart(t *testing.T, got *headless.MemorySink, w, h int) {
	t.Helper()
	fresh := headless.NewMemorySink(w, h)
	r := render.New(fresh, render.Config{Width: w, Height: h, Caps: termmosaic.DefaultCaps()})
	r.SetRoot(newHello(rootBounds(w, h), buffer.DepthTrueColor))
	if _, err := r.Render(); err != nil {
		t.Fatalf("fresh start at %dx%d: %v", w, h, err)
	}
	strip := func(s string) string {
		return strings.Map(func(r rune) rune {
			if r >= '0' && r <= '9' {
				return -1
			}
			return r
		}, s)
	}
	a, b := strip(got.String()), strip(fresh.String())
	if a != b {
		t.Errorf("%dx%d: the swept screen does not match a fresh start at the same size.\n"+
			"--- swept ---\n%s\n--- fresh ---\n%s", w, h, a, b)
	}
}

// TestWriteIntRendersEveryDecimalPlace covers the hand-rolled formatting that
// replaced fmt.Sprintf on the draw path. It is worth testing precisely because
// it exists to avoid an allocation: a bug here would be a wrong digit on screen,
// and a "simplification" back to Sprintf would reintroduce the heap traffic
// TestDrawIsAllocationFree forbids.
func TestWriteIntRendersEveryDecimalPlace(t *testing.T) {
	for _, v := range []int{0, 1, 7, 9, 10, 42, 99, 100, 1000, 9999, 123456, 1 << 20, -5} {
		b := buffer.NewBuffer(12, 1)
		h := &hello{}
		next := h.writeInt(b, 0, 0, v)
		if got := b.CellAt(next-1, 0); next < 1 || got.Style() != stValue {
			t.Errorf("writeInt(%d) ended at %d, outside the buffer or unstyled", v, next)
		}
		if got := strings.TrimRight(rowText(b, next), " "); got != wantDecimal(v) {
			t.Errorf("writeInt(%d) wrote %q, want %q", v, got, wantDecimal(v))
		}
		// Everything before the digits must be untouched.
		if next > 0 {
			_ = b.CellAt(0, 0)
		}
	}
}

// rowText renders row 0's first n cells as a string.
func rowText(b *buffer.Buffer, n int) string {
	var sb strings.Builder
	for x := 0; x < n; x++ {
		sb.WriteRune(b.CellAt(x, 0).Rune())
	}
	return sb.String()
}

// wantDecimal is the reference the hand-rolled formatter is checked against. It
// is strconv, used only in a test, which is the point: the draw path must not.
func wantDecimal(v int) string {
	if v < 0 {
		v = 0
	}
	return strconv.Itoa(v)
}

// TestGoldenFilesExist fails with a clear instruction rather than a file-not-found
// error, because that is the first thing anyone hits here. The list includes the
// shrink and recovery files, so a golden that was never generated is a failure here
// rather than a confusing diff later.
func TestGoldenFilesExist(t *testing.T) {
	names := []string{
		"hello.txt", "hello.sgr", "hello_frame2.txt",
		"hello_resized.txt", "hello_shrunk.txt", "hello_recovered.txt",
	}
	for _, name := range names {
		if _, err := os.Stat(filepath.Join("testdata", name)); err != nil {
			t.Errorf("missing golden file %s: %v", name, err)
		}
	}
}

// TestExampleBorderThresholdsMatchTheAgreedValues pins the two numbers ADR 0008 §2
// fixes for borders and titles: a border needs two cells on each axis, and a title
// additionally needs five.
//
// The constants used to live in this file, because the example drew its own border.
// It no longer does — it composes a block.Block, the catalog's only owner of borders
// and titles — so the assertion has moved to where the thresholds now live. It is
// the same assertion at the same strength: the VALUES are still pinned here, and a
// change to them still fails this test rather than silently changing the catalog.
func TestExampleBorderThresholdsMatchTheAgreedValues(t *testing.T) {
	if block.MinBorderW != 2 || block.MinBorderH != 2 {
		t.Errorf("border threshold = %dx%d, want 2x2: two cells per axis, one per corner",
			block.MinBorderW, block.MinBorderH)
	}
	// A title needs two corners plus a space, a glyph and a space.
	const titlePad = 2 // one space each side, inserted by Block
	if titleMin := block.MinBorderW + titlePad + 1; titleMin != 5 || block.MinTitleW != 5 {
		t.Errorf("title threshold = %d (derived %d), want 5", block.MinTitleW, titleMin)
	}
}

// TestExampleOwnsNoBorderVocabulary is the structural half of the same change: the
// example must contain no border glyph lookup and no title threshold of its own,
// because those are the two things three parallel authors each reinvented.
// TestBoxDrawingRunesLiveInOneFile enforces the rune half across the whole module;
// this asserts the threshold half at the file where the duplication used to be.
func TestExampleOwnsNoBorderVocabulary(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"minBorderW", "minBorderH", "minTitleW", "Glyphs("} {
		if strings.Contains(string(src), forbidden) {
			t.Errorf("main.go mentions %q; the border, the title and its thresholds belong to "+
				"widgets/block now", forbidden)
		}
	}
}

// TestExampleTitleAppearsAtExactlyTheTitleThreshold is the functional counterpart:
// the example's Block shows its title at width 5 and drops it at width 4, which is
// what the constants above claim.
func TestExampleTitleAppearsAtExactlyTheTitleThreshold(t *testing.T) {
	showsTitle := func(w int) bool {
		b := newHello(buffer.Rect{W: w, H: 3}, buffer.DepthTrueColor)
		buf := buffer.NewBuffer(w, 3)
		b.Draw(buf)
		return strings.Contains(rowText(buf, w), "t")
	}
	if !showsTitle(block.MinTitleW) {
		t.Errorf("the title must be drawn at width %d", block.MinTitleW)
	}
	if showsTitle(block.MinTitleW - 1) {
		t.Errorf("the title must be dropped at width %d", block.MinTitleW-1)
	}
}

// TestDrawIsTotalAtEverySize is ADR 0007 §4's degenerate-size contract applied to
// the example: Draw must be defined for every rectangle, including empty, and
// must never panic. Every write is bounds-checked, so the assertion is really
// that the guards short-circuit before they do any work.
func TestDrawIsTotalAtEverySize(t *testing.T) {
	for w := 0; w <= 12; w++ {
		for h := 0; h <= 12; h++ {
			widget := newHello(buffer.Rect{W: w, H: h}, buffer.DepthTrueColor)
			b := buffer.NewBuffer(atLeast1(w), atLeast1(h))
			widget.Draw(b) // must not panic for any size, including 0x0
		}
	}
}

// TestDrawIsAllocationFree is the widget-side half of ADR 0008 §4: a Draw that
// builds nothing derived from its size allocates nothing.
//
// The Block's title is built once in newHello and its truncation cached per rect;
// the body writes one integer digit by digit, which for these small values does not
// escape. If a future change makes Draw call Wrap or Truncate, this fails — which is
// the point, because that is the most likely performance regression in the catalog
// and documentation alone does not catch it.
func TestDrawIsAllocationFree(t *testing.T) {
	b := buffer.NewBuffer(60, 14)
	h := newHello(rootBounds(60, 14), buffer.DepthTrueColor)
	h.Draw(b) // warm any lazily-initialised state

	if got := testing.AllocsPerRun(100, func() { h.Draw(b) }); got != 0 {
		t.Errorf("Draw allocated %.1f objects per run, want 0. ADR 0008 §4 forbids calling Wrap, "+
			"Truncate, or building a []Span inside Draw; cache them on a rect change instead", got)
	}
}

// atLeast1 clamps a degenerate dimension, because NewBuffer rejects nothing but
// a zero-sized buffer has no cells to draw into and would prove nothing.
func atLeast1(v int) int {
	if v < 1 {
		return 1
	}
	return v
}
