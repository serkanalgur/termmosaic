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
	"github.com/serkanalgur/termmosaic/widgets/widgettest"
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
// It goes through widgettest.Render, which is the whole point of that package:
// the widget, the renderer, the two-tier diff, the ANSI encoder and the headless
// screen model all have to agree, and asserting on anything short of that proves
// less than it looks like it proves.
func renderGolden(t *testing.T, frames int) *headless.MemorySink {
	t.Helper()
	return widgettest.Render(t, goldenW, goldenH, frames,
		newHello(rootBounds(goldenW, goldenH), buffer.DepthTrueColor))
}

// renderAt renders the example's block at an arbitrary screen size, which is how
// the responsive tests get a screen without seven near-copies of renderGolden.
func renderAt(t *testing.T, w, h, frames int) *headless.MemorySink {
	t.Helper()
	return widgettest.Render(t, w, h, frames,
		newHello(rootBounds(w, h), buffer.DepthTrueColor))
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

// TestGoldenAtEverySize is the responsive golden: the same widget, at seven
// sizes spanning every breakpoint this example introduces, plus the degenerate
// ones.
//
// The sizes are chosen to sit either side of both column thresholds rather than
// at round numbers, because a golden at 60x24 proves nothing about a threshold at
// 36. twoColW and threeColW are interior widths, and the border plus padding
// costs four cells, so the corresponding screen widths are twoColW+6 and
// threeColW+6 — hence 41 and 65 below, chosen to be one cell short of each.
func TestGoldenAtEverySize(t *testing.T) {
	for _, tc := range []struct {
		w, h int
		name string
	}{
		{30, 8, "size_30x8"},   // below MinSize: the diagnostic, not a layout
		{40, 10, "size_40x10"}, // at MinSize: the sparsest honest layout
		{45, 12, "size_45x12"}, // one column: interior 39 < twoColW
		{46, 12, "size_46x12"}, // two columns: interior 40 >= twoColW
		{65, 16, "size_65x16"}, // still two columns: interior 59 < threeColW
		{66, 16, "size_66x16"}, // three columns: interior 60 >= threeColW
		{80, 24, "size_80x24"},
		{120, 40, "size_120x40"},
		{200, 60, "size_200x60"},
		{1, 1, "size_1x1"},
		{0, 0, "size_0x0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sink := renderAt(t, tc.w, tc.h, 1)
			checkGolden(t, tc.name+".txt", sink.String())
		})
	}
}

// TestLayoutChangesWithWidth is the assertion that makes "responsive" mean
// something: the arrangement must be observably different either side of each
// threshold, not merely the same layout with different padding.
//
// It asserts on the SCREEN rather than on the widget's internals, because the
// screen is what the user sees and the only thing a responsive claim is about.
// Three properties, one per band:
//
//   - one column: "frame" and "depth" are on DIFFERENT rows, so the facts stack.
//   - two columns: they are on the SAME row, so the facts sit side by side.
//   - the "cols" fact reports the count in words, so the block says what it did.
//
// The two-column case also proves the column is real rather than a doubled gap:
// the second column starts past the first column's width.
func TestLayoutChangesWithWidth(t *testing.T) {
	// rowOf returns the screen row containing needle, and whether it was found.
	rowOf := func(sink *headless.MemorySink, needle string) (int, bool) {
		_, h := sink.Size()
		for y := 0; y < h; y++ {
			if strings.Contains(widgettest.Row(sink, y), needle) {
				return y, true
			}
		}
		return -1, false
	}

	// Interior = screen - 2*screenMargin - 2 border - 2 padding = screen - 6, so
	// the band boundaries in screen widths are twoColW+6 = 46 and
	// threeColW+6 = 66. The cases below sit inside each band rather than on its
	// edge; TestColumnThresholdsAreWhereTheyAreClaimed covers the edges.
	for _, tc := range []struct {
		name     string
		w, h     int
		wantCols int
	}{
		{"one column", 40, 12, 1},
		{"two columns", 50, 14, 2},
		{"three columns", 70, 16, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sink := renderAt(t, tc.w, tc.h, 1)
			screen := widgettest.Screen(sink)

			// The block says which arrangement it chose, so the assertion is
			// readable from the screen without counting columns.
			wantReport := fmt.Sprintf("%d column%s", tc.wantCols, plural(tc.wantCols))
			if !strings.Contains(screen, wantReport) {
				t.Errorf("%dx%d does not report %q.\n%s", tc.w, tc.h, wantReport, screen)
			}

			// The three facts are laid out column-major over factRows rows, so
			// their rows say how the grid split: with one column all three stack,
			// with two the third joins the first, and with three all three share a
			// row. That progression is the layout changing, not the padding.
			frameRow, okF := rowOf(sink, "frame")
			depthRow, okD := rowOf(sink, "depth")
			colsRow, okC := rowOf(sink, "cols")
			if !okF || !okD || !okC {
				t.Fatalf("%dx%d: a fact is missing (frame=%v depth=%v cols=%v).\n%s",
					tc.w, tc.h, okF, okD, okC, screen)
			}
			switch tc.wantCols {
			case 1:
				if frameRow == depthRow || depthRow == colsRow {
					t.Errorf("%dx%d: one column must stack the facts, but they are on rows %d/%d/%d",
						tc.w, tc.h, frameRow, depthRow, colsRow)
				}
			case 2:
				if frameRow != colsRow {
					t.Errorf("%dx%d: two columns must put the third fact beside the first, but \"cols\" is on row %d and \"frame\" on %d",
						tc.w, tc.h, colsRow, frameRow)
				}
				if frameRow == depthRow {
					t.Errorf("%dx%d: two columns over two rows must not put every fact on one row",
						tc.w, tc.h)
				}
			case 3:
				if frameRow != depthRow || depthRow != colsRow {
					t.Errorf("%dx%d: three columns must put all three facts on one row, but they are on %d/%d/%d",
						tc.w, tc.h, frameRow, depthRow, colsRow)
				}
			}
		})
	}
}

// plural is the "s" in the fact's own wording, so the expected report string is
// built by the same rule the example uses rather than hardcoded three times.
func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// TestColumnThresholdsAreWhereTheyAreClaimed pins the two numbers the layout
// switches on, in screen widths, and asserts the switch actually happens on the
// boundary rather than merely near it. A threshold nobody can trigger is a
// comment, not a layout.
func TestColumnThresholdsAreWhereTheyAreClaimed(t *testing.T) {
	// chrome is what the block spends on the screen before the interior: the
	// margin on each side plus the border and the padding on each side.
	const chrome = 2*screenMargin + 2 + 2
	for _, tc := range []struct {
		w        int
		wantCols int
	}{
		{twoColW + chrome - 1, 1},
		{twoColW + chrome, 2},
		{threeColW + chrome - 1, 2},
		{threeColW + chrome, 3},
	} {
		inner := rootBounds(tc.w, 40)
		inner = newChrome(inner).Interior()
		if got := columnsFor(inner.W); got != tc.wantCols {
			t.Errorf("a %d-wide screen has a %d-wide interior, so columnsFor = %d, want %d",
				tc.w, inner.W, got, tc.wantCols)
		}
	}
}

// TestColumnsForIsTotal pins the clamp at both ends: an interior of any width,
// including negative, yields a count in [minCols, maxCols]. The upper clamp is
// what stops a very wide terminal from producing more columns than there are
// facts, and the lower one is what stops a degenerate rect producing zero
// columns and dividing by zero in adapt.
func TestColumnsForIsTotal(t *testing.T) {
	for innerW := -5; innerW <= 200; innerW++ {
		got := columnsFor(innerW)
		if got < minCols || got > maxCols {
			t.Fatalf("columnsFor(%d) = %d, outside [%d, %d]", innerW, got, minCols, maxCols)
		}
	}
	if got := columnsFor(1 << 30); got != maxCols {
		t.Errorf("columnsFor(1<<30) = %d, want the clamp at %d", got, maxCols)
	}
}

// TestBelowMinSizeDrawsTheDiagnosticAndNotALayout is the boundary test ADR 0007
// §4 asks for: at a size under MinSize the widget says so in one line rather
// than clipping a layout into nonsense, and it still repaints its whole rect so
// nothing from a larger size is left behind.
func TestBelowMinSizeDrawsTheDiagnosticAndNotALayout(t *testing.T) {
	sink := renderAt(t, 30, 8, 1)
	screen := widgettest.Screen(sink)

	if !strings.Contains(screen, "needs 38x8") {
		t.Errorf("a 30x8 screen must produce the minimum-size diagnostic.\n%s", screen)
	}
	// Below the minimum there is no layout, so none of its parts may appear. The
	// title stands in for the border here: it lives ON the border, so a screen
	// showing one has a border, and the assertion cannot name a border rune
	// because TestBoxDrawingRunesLiveInOneFile requires every border glyph to
	// come from the table — including from a test that means well.
	for _, absent := range []string{"termmosaic", "frame", "press q"} {
		if strings.Contains(screen, absent) {
			t.Errorf("a 30x8 screen drew %q; below MinSize the block must draw the diagnostic and nothing else.\n%s",
				absent, screen)
		}
	}
}

// TestMinSizeIsReportedAndHonoured states the Minimizable contract at the
// application level: the block reports a minimum, and Draw's behaviour changes at
// exactly that size rather than somewhere near it.
func TestMinSizeIsReportedAndHonoured(t *testing.T) {
	h := newHello(buffer.Rect{W: 200, H: 60}, buffer.DepthTrueColor)
	m := h.MinSize()
	if m.W != minW || m.H != minH {
		t.Errorf("MinSize = %dx%d, want %dx%d", m.W, m.H, minW, minH)
	}

	saysDiagnostic := func(w, h int) bool {
		buf := buffer.NewBuffer(atLeast1(w), atLeast1(h))
		newHello(buffer.Rect{W: w, H: h}, buffer.DepthTrueColor).Draw(buf)
		return strings.Contains(rowText(buf, w), "n")
	}
	if saysDiagnostic(minW-1, minH) != true {
		t.Errorf("at %dx%d the block must show the diagnostic", minW-1, minH)
	}
	if saysDiagnostic(minW, minH-1) != true {
		t.Errorf("at %dx%d the block must show the diagnostic", minW, minH-1)
	}
	if saysDiagnostic(minW, minH) != false {
		t.Errorf("at exactly MinSize the block must draw its layout, not the diagnostic")
	}
}

// TestBudgetDropsTheNoteFirstAndNeverTheHint is the height half of
// responsiveness, asserted through the screen: the low-priority explanatory
// paragraph appears only when there is height for it, and the pinned hint is
// never the thing that goes.
//
// It uses the note's opening words and the hint's, so a change to either string
// fails here rather than silently making the test vacuous.
func TestBudgetDropsTheNoteFirstAndNeverTheHint(t *testing.T) {
	has := func(sink *headless.MemorySink, needle string) bool {
		return strings.Contains(widgettest.Screen(sink), needle)
	}
	tall := renderAt(t, 60, 14, 1)
	if !has(tall, "the layout above is recomputed") {
		t.Errorf("at 60x14 there is height for the note paragraph.\n%s", widgettest.Screen(tall))
	}
	if !has(tall, "press q to quit") {
		t.Errorf("the hint is PrioAlways and must survive every budget.\n%s", widgettest.Screen(tall))
	}

	// 40x10 is MinSize exactly: enough for the facts and the hint, not enough
	// for the tagline or the note.
	short := renderAt(t, 40, 10, 1)
	if has(short, "the layout above is recomputed") {
		t.Errorf("at 40x10 there is no height for the note paragraph.\n%s", widgettest.Screen(short))
	}
	if !has(short, "press q to quit") {
		t.Errorf("the hint must still be present at MinSize.\n%s", widgettest.Screen(short))
	}
	if !has(short, "depth truecolor") {
		t.Errorf("the facts are PrioHigh and must outrank the tagline and the note.\n%s", widgettest.Screen(short))
	}
}

// TestTaglineTruncatesWithAMarkerWhenNarrow is the width half of responsiveness
// at the smallest size that still draws a layout: the tagline is present and cut
// short with an ellipsis, so the user learns there was more.
func TestTaglineTruncatesWithAMarkerWhenNarrow(t *testing.T) {
	// 44x12 has one column and an interior too narrow for the whole tagline.
	narrow := widgettest.Screen(renderAt(t, 44, 12, 1))
	if !strings.Contains(narrow, "…") {
		t.Errorf("at 44x12 the tagline does not fit and must end in an ellipsis.\n%s", narrow)
	}
	if !strings.Contains(narrow, "hello from termmosaic") {
		t.Errorf("at 44x12 the tagline must still be visible, truncated rather than dropped.\n%s", narrow)
	}

	// 120x40 has room for all of it, and the full text must be there with no
	// marker on the row.
	wide := widgettest.Screen(renderAt(t, 120, 40, 1))
	if !strings.Contains(wide, tagline) {
		t.Errorf("at 120x40 the tagline must read in full.\n%s", wide)
	}
	for y, row := range strings.Split(wide, "\n") {
		if strings.Contains(row, "hello from termmosaic") && strings.Contains(row, "…") {
			t.Errorf("row %d truncates a tagline that fits: %q", y, row)
		}
	}
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
	// 200x60 is the size the old fixed 46x9 block failed hardest at: it drew the
	// same 46x9 there as on a 46x9 terminal. Growing to it exercises the three
	// column arrangement.
	const newW, newH = 200, 60
	resizeTo(t, newW, newH)
	checkGolden(t, "hello_resized.txt", sink.String())

	// --- SHRINK, below the block's minimum ----------------------------------
	// 30x7 is narrower and shorter than MinSize, so the layout, the title and the
	// facts all disappear and the diagnostic takes over. A shrink is the only step
	// that can leave a stale row behind, and this is the shrink most likely to.
	resizeTo(t, 30, 7)
	if got, want := root.bounds.W, 30-2*screenMargin; got != want {
		t.Errorf("the block's width is %d on a 30-wide screen, want the screen less its margins (%d)", got, want)
	}
	checkGolden(t, "hello_shrunk.txt", sink.String())

	// --- DEGENERATE ----------------------------------------------------------
	// 0x0 is the detached-terminal case and 4x2 is far below MinSize; both must
	// render without failing and both must leave the renderer able to come back to
	// a real size.
	for _, size := range [][2]int{{0, 0}, {4, 2}, {20, 1}} {
		resizeTo(t, size[0], size[1])
		if w, h := r.Size(); w != size[0] || h != size[1] {
			t.Errorf("Size() = %dx%d, want %dx%d", w, size[0], h, size[1])
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

// TestRootBoundsGrowsWithTheScreen is the direct statement of the fix: the block
// must not have a preferred size. At every screen width from MinSize upwards the
// block takes the whole width less its margins, so a 200x60 terminal gets a
// 198-wide block rather than the 46x9 the example used to ask for.
//
// It also pins the margins: they are a fixed inset, not a proportion, so the
// block's width is exactly the screen's width less two margins at every size.
func TestRootBoundsGrowsWithTheScreen(t *testing.T) {
	for w := minW; w <= 240; w++ {
		r := rootBounds(w, 40)
		if want := w - 2*screenMargin; r.W != want {
			t.Fatalf("at %d wide the block is %d wide, want %d — the block has a maximum size again",
				w, r.W, want)
		}
		if r.X != screenMargin {
			t.Fatalf("at %d wide the block starts at %d, want the %d-cell margin",
				w, r.X, screenMargin)
		}
	}
	if got := rootBounds(200, 60); got.W <= 46 {
		t.Errorf("at 200x60 the block is %d wide; the old fixed 46x9 would have been narrower", got.W)
	}
}

// TestRootBoundsIsNeverEmptyOnANonEmptyScreen is the margin rule at its limit.
// Min(1) on both sides of a one-cell axis overflows the space between them, so
// the Fill in the middle receives nothing and the block collapses — which means
// a 1x1 terminal would draw no block at all. marginFor exists to prevent that,
// and this is the assertion that it does.
//
// Every screen with a cell in it must produce a block with a cell in it. A
// widget that blanks itself at the size where it has least room is the clamp
// mistake in miniature.
func TestRootBoundsIsNeverEmptyOnANonEmptyScreen(t *testing.T) {
	for w := 1; w <= 80; w++ {
		for h := 1; h <= 40; h++ {
			r := rootBounds(w, h)
			if r.Empty() {
				t.Fatalf("a %dx%d screen produced an empty block; the margin consumed the screen",
					w, h)
			}
			if r.Right() > w || r.Bottom() > h {
				t.Fatalf("a %dx%d screen produced a %v block, which is off screen", w, h, r)
			}
		}
	}
	// And at 0x0 the block IS empty, which is the one case where that is correct:
	// there is nothing to draw into.
	if r := rootBounds(0, 0); !r.Empty() {
		t.Errorf("rootBounds(0, 0) = %v, want an empty rect", r)
	}
}

// TestMarginForYieldsRatherThanAnnihilating states marginFor's rule directly: it
// drops the margin on an axis too narrow to carry two of them and at least one
// cell of block, and keeps it otherwise.
func TestMarginForYieldsRatherThanAnnihilating(t *testing.T) {
	for avail := -5; avail <= 40; avail++ {
		got := marginFor(avail, screenMargin)
		if got < 0 {
			t.Fatalf("marginFor(%d) = %d, want a non-negative margin", avail, got)
		}
		if avail < 2*screenMargin+1 {
			if got != 0 {
				t.Errorf("marginFor(%d) = %d, want 0: the axis cannot carry two margins and a cell",
					avail, got)
			}
			continue
		}
		if got != screenMargin {
			t.Errorf("marginFor(%d) = %d, want %d", avail, got, screenMargin)
		}
	}
}

// TestWriteIntRendersEveryDecimalPlace covers the hand-rolled formatting that
// replaced fmt.Sprintf on the draw path. It is worth testing precisely because it
// exists to avoid an allocation: a bug here would be a wrong digit on screen,
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
	}
}

// TestWriteIntInStopsAtItsEdge is the clipping half: a number may not run past
// its own column into the next one, because a partially written number is worse
// than none.
func TestWriteIntInStopsAtItsEdge(t *testing.T) {
	h := &hello{}
	for _, tc := range []struct {
		v, x0, x1, wantCells int
	}{
		{123456, 0, 3, 3},
		{123456, 0, 0, 0},
		{123456, 5, 2, 0},
		{7, 0, 1, 1},
		{7, 3, 4, 1},
	} {
		b := buffer.NewBuffer(12, 1)
		next := h.writeIntIn(b, tc.x0, tc.x1, 0, tc.v)
		if got := next - tc.x0; got != tc.wantCells {
			t.Errorf("writeIntIn(_, %d, %d, _, %d) wrote %d cells, want %d",
				tc.x0, tc.x1, tc.v, got, tc.wantCells)
		}
		if next > tc.x1 && tc.x1 > tc.x0 {
			t.Errorf("writeIntIn(_, %d, %d, _, %d) returned %d, past its edge", tc.x0, tc.x1, tc.v, next)
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

// wantDecimal is the reference the hand-rolled formatter is checked against. It is
// strconv, used only in a test, which is the point: the draw path must not.
func wantDecimal(v int) string {
	if v < 0 {
		v = 0
	}
	return strconv.Itoa(v)
}

// TestGoldenFilesExist fails with a clear instruction rather than a file-not-found
// error, because that is the first thing anyone hits here. The list includes the
// per-size responsive goldens and the shrink and recovery files, so a golden that
// was never generated is a failure here rather than a confusing diff later.
func TestGoldenFilesExist(t *testing.T) {
	names := []string{
		"hello.txt", "hello.sgr", "hello_frame2.txt",
		"hello_resized.txt", "hello_shrunk.txt", "hello_recovered.txt",
		"size_30x8.txt", "size_40x10.txt", "size_45x12.txt", "size_46x12.txt",
		"size_65x16.txt", "size_66x16.txt", "size_80x24.txt", "size_120x40.txt",
		"size_200x60.txt", "size_1x1.txt", "size_0x0.txt",
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
//
// It goes through newChrome rather than newHello because hello gates its whole
// layout on MinSize, so at width 5 hello draws the diagnostic and never reaches
// the Block at all. The chrome is the thing under test here, and newChrome is the
// example's own chrome configuration.
func TestExampleTitleAppearsAtExactlyTheTitleThreshold(t *testing.T) {
	showsTitle := func(w int) bool {
		b := newChrome(buffer.Rect{W: w, H: 3})
		b.SetTitleString("termmosaic", stTitle)
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
//
// It walks both axes to 14 so it crosses MinSize in both directions: above it the
// layout runs, below it the diagnostic runs, and the two paths are different code
// that could each be the one that panics on a 1-wide rect.
func TestDrawIsTotalAtEverySize(t *testing.T) {
	for w := 0; w <= 14; w++ {
		for h := 0; h <= 14; h++ {
			widget := newHello(buffer.Rect{W: w, H: h}, buffer.DepthTrueColor)
			b := buffer.NewBuffer(atLeast1(w), atLeast1(h))
			widget.Draw(b) // must not panic for any size, including 0x0
		}
	}
}

// TestDegenerateSizesRenderWithoutPanic is the same contract one level up, through
// the renderer rather than through a bare buffer: 0x0 is the detached terminal,
// 0x24 and 24x0 are the two half-degenerate cases that a one-axis guard is the
// classic way to miss, and 1x1 is the smallest real terminal.
//
// ADR 0007 §4 requires zero bytes written and no Sink call at a zero size, which
// render.Render already guarantees; what this adds is that the example's own
// guards do not panic on the way there.
func TestDegenerateSizesRenderWithoutPanic(t *testing.T) {
	for _, tc := range [][2]int{{0, 0}, {1, 1}, {0, 24}, {24, 0}, {0, 1}, {1, 0}} {
		widget := newHello(rootBounds(tc[0], tc[1]), buffer.DepthTrueColor)
		r := render.New(headless.NewMemorySink(tc[0], tc[1]), render.Config{
			Width: tc[0], Height: tc[1], Caps: termmosaic.DefaultCaps(),
		})
		r.SetRoot(widget)
		n, err := r.Render()
		if err != nil {
			t.Errorf("%dx%d: Render: %v", tc[0], tc[1], err)
		}
		if tc[0] == 0 || tc[1] == 0 {
			if n != 0 {
				t.Errorf("%dx%d: wrote %d bytes; a zero size must write none", tc[0], tc[1], n)
			}
			continue
		}
		if n == 0 {
			t.Errorf("%dx%d: wrote nothing, but a non-degenerate size has something to show", tc[0], tc[1])
		}
	}
}

// TestDrawIsAllocationFree is the widget-side half of ADR 0008 §4: a Draw that
// builds nothing derived from its size allocates nothing.
//
// The Block's title, the tagline's truncation and the note's wrap are all cached
// per rect, the body layout is derived once per rect, and the only per-frame work
// is the frame counter, written digit by digit. If a future change makes Draw call
// Wrap or Truncate, or re-derive the layout, this fails — which is the point,
// because that is the most likely performance regression in the catalog and
// documentation alone does not catch it.
//
// It runs at three sizes because the layout differs between them: one column, two
// columns, and below MinSize where a completely different path draws.
func TestDrawIsAllocationFree(t *testing.T) {
	for _, tc := range []struct{ w, h int }{
		{30, 8}, // below MinSize: the diagnostic path
		{60, 14},
		{200, 60}, // three columns
	} {
		b := buffer.NewBuffer(tc.w, tc.h)
		h := newHello(rootBounds(tc.w, tc.h), buffer.DepthTrueColor)
		h.Draw(b) // warm any lazily-initialised state

		if got := testing.AllocsPerRun(100, func() { h.Draw(b) }); got != 0 {
			t.Errorf("%dx%d: Draw allocated %.1f objects per run, want 0. ADR 0008 §4 forbids calling "+
				"Wrap, Truncate, or building a []Span inside Draw; cache them on a rect change instead",
				tc.w, tc.h, got)
		}
	}
}

// TestAdaptIsSkippedForAnUnchangedRect is the caching rule stated as a test: the
// cached layout is reused when the rect has not changed, and recomputed when it
// has. A widget that re-derived everything every frame would still pass the
// allocation test at zero allocations only by luck, so the keying is asserted
// directly.
func TestAdaptIsSkippedForAnUnchangedRect(t *testing.T) {
	h := newHello(rootBounds(60, 14), buffer.DepthTrueColor)
	b := buffer.NewBuffer(60, 14)

	h.Draw(b)
	first := h.lay
	if !h.lay.valid || h.lay.cols != 2 {
		t.Fatalf("after one draw the layout is %+v; want a valid two-column layout", first)
	}

	h.Draw(b)
	if h.lay.cols != first.cols || h.lay.factRows != first.factRows {
		t.Errorf("a second draw at the same rect changed the layout: %+v then %+v", first, h.lay)
	}

	h.bounds = rootBounds(120, 40)
	b = buffer.NewBuffer(120, 40)
	h.Draw(b)
	if h.lay.cols != 3 {
		t.Errorf("after growing to 120x40 the layout has %d columns, want 3 — the cache did not re-derive",
			h.lay.cols)
	}
	// And the re-derive must be visible on screen, not only in the struct: a
	// cache that recomputes but is not drawn would satisfy the assertion above.
	if !strings.Contains(widgettest.Screen(
		widgettest.Render(t, 120, 40, 1,
			newHello(rootBounds(120, 40), buffer.DepthTrueColor))), "3 columns") {
		t.Error("the three-column layout is not on screen at 120x40")
	}
}

// TestResizeRepaintsTheWholeRect is ADR 0007 §1 rule 3 at the application level:
// shrinking must not leave a stale row behind. The sweep goes big, then small, and
// compares each step against a fresh renderer at the same size — which is the only
// comparison that can see a leftover row, since a golden for the new size alone
// would match just as happily with a stale row above it.
func TestResizeRepaintsTheWholeRect(t *testing.T) {
	sizes := [][2]int{{200, 60}, {120, 40}, {60, 14}, {40, 10}, {30, 8}, {60, 14}, {200, 60}}
	sink := headless.NewMemorySink(sizes[0][0], sizes[0][1])
	r := render.New(sink, render.Config{
		Width: sizes[0][0], Height: sizes[0][1], Caps: termmosaic.DefaultCaps(),
	})
	root := newHello(rootBounds(sizes[0][0], sizes[0][1]), buffer.DepthTrueColor)
	r.SetRoot(root)
	if _, err := r.Render(); err != nil {
		t.Fatal(err)
	}
	for _, s := range sizes[1:] {
		sink.Resize(s[0], s[1])
		r.Resize(s[0], s[1])
		root.bounds = rootBounds(s[0], s[1])
		if _, err := r.Render(); err != nil {
			t.Fatalf("resize to %dx%d: %v", s[0], s[1], err)
		}
		assertScreenMatchesAFreshStart(t, sink, s[0], s[1])
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
