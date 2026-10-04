package block

import (
	"strings"
	"testing"

	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/geometry"
	"github.com/serkanalgur/termmosaic/widgets/widgettest"
)

// This package's tests must not spell a box-drawing rune: buffer/border.go is the
// only file in TermMosaic allowed to, and TestBoxDrawingRunesLiveInOneFile
// enforces it by scanning every .go file. Every expected string below is
// therefore assembled from the shared glyph table, which is also why a change to
// that table shows up here as a change in meaning rather than in spelling.

// plainSet is the Unicode glyph set under test.
//
// It is not called "glyphs" because TestNoPackageRedefinesSharedVocabulary
// reserves that name for buffer: a private table under any other name is the
// collision this repository cannot have, and that includes a helper in a test.
func plainSet() buffer.BorderGlyphs { return buffer.BorderPlain.Glyphs(false) }

// repeat builds a run of one glyph n cells wide.
func repeat(r rune, n int) string {
	if r == 0 || n <= 0 {
		return ""
	}
	return strings.Repeat(string(r), n)
}

// plain returns a w-by-h block with the default border and a title.
func plain(w, h int, title string) *Block {
	b := New(buffer.Rect{W: w, H: h})
	b.Border = buffer.BorderPlain
	b.SetTitleString(title, buffer.PlainStyle)
	return b
}

// rowOf renders row y of buf as a string, which is how a cell-level test reads
// back what a Draw wrote without involving the renderer.
func rowOf(buf *buffer.Buffer, y int) string {
	row := buf.Row(y)
	var sb strings.Builder
	for _, c := range row {
		sb.WriteRune(c.Rune())
	}
	return strings.TrimRight(sb.String(), " ")
}

// TestBlockDrawsBorderAndTitle is the ordinary case, asserted as a screen: a
// border, a title overwriting the top run between the corners, and nothing else.
func TestBlockDrawsBorderAndTitle(t *testing.T) {
	g := plainSet()
	sink := widgettest.Render(t, 12, 5, 1, plain(12, 5, "hi"))

	want := []string{
		string(g.TopLeft) + " hi " + repeat(g.Horizontal, 6) + string(g.TopRight),
		string(g.Vertical) + strings.Repeat(" ", 10) + string(g.Vertical),
		string(g.Vertical) + strings.Repeat(" ", 10) + string(g.Vertical),
		string(g.Vertical) + strings.Repeat(" ", 10) + string(g.Vertical),
		string(g.BottomLeft) + repeat(g.Horizontal, 10) + string(g.BottomRight),
	}
	for i, w := range want {
		if got := widgettest.Row(sink, i); got != w {
			t.Errorf("row %d\n got %q\nwant %q", i, got, w)
		}
	}
}

// TestBlockTitleOverwritesTheTopBorder pins the placement rule: the title sits ON
// the border row, not on a row of its own inside the box.
func TestBlockTitleOverwritesTheTopBorder(t *testing.T) {
	g := plainSet()
	sink := widgettest.Render(t, 10, 4, 1, plain(10, 4, "ab"))

	if got, want := widgettest.Row(sink, 0), string(g.TopLeft)+" ab "+repeat(g.Horizontal, 4)+string(g.TopRight); got != want {
		t.Errorf("top row\n got %q\nwant %q", got, want)
	}
	if got := widgettest.Row(sink, 1); got != string(g.Vertical)+strings.Repeat(" ", 8)+string(g.Vertical) {
		t.Errorf("row 1 should be blank between the verticals, got %q", got)
	}
}

// TestBlockTitlePaddingIsOneSpaceEachSide pins the automatic padding. A caller
// that adds the spaces itself gets two extra columns and a title that no longer
// aligns with the interior.
func TestBlockTitlePaddingIsOneSpaceEachSide(t *testing.T) {
	b := New(buffer.Rect{W: 20, H: 3})
	b.Border = buffer.BorderPlain
	b.SetTitleString("ab", buffer.PlainStyle)
	if got, want := b.TitleWidth(), 4; got != want {
		t.Errorf("TitleWidth = %d, want %d: 'ab' plus one space each side", got, want)
	}
	g := plainSet()
	want := string(g.TopLeft) + " ab " + repeat(g.Horizontal, 14) + string(g.TopRight)
	if got := widgettest.Row(widgettest.Render(t, 20, 3, 1, b), 0); got != want {
		t.Errorf("top row\n got %q\nwant %q", got, want)
	}
}

// TestBlockNoBorderBelow2x2 covers ADR 0008 §2's first threshold exactly: a
// border needs W>=2 AND H>=2, and one cell short on either axis there is no
// border and no title.
func TestBlockNoBorderBelow2x2(t *testing.T) {
	for _, sz := range []buffer.Rect{
		{W: 0, H: 0}, {W: 1, H: 1}, {W: 1, H: 2}, {W: 2, H: 1}, {W: 1, H: 9}, {W: 9, H: 1},
	} {
		b := plain(max1(sz.W), max1(sz.H), "hi")
		b.SetBounds(sz)
		sink := widgettest.Render(t, max1(sz.W), max1(sz.H), 1, b)
		for y := 0; y < max1(sz.H); y++ {
			if got := widgettest.Row(sink, y); strings.Trim(got, " ") != "" {
				t.Errorf("bounds %dx%d drew %q on row %d; no border or title fits there",
					sz.W, sz.H, got, y)
			}
		}
	}
}

// TestBlockBorderAtExactlyTwoByTwo is the other half of the same threshold: at
// exactly 2x2 the four corners do draw, and there is no room for anything else.
func TestBlockBorderAtExactlyTwoByTwo(t *testing.T) {
	g := plainSet()
	b := New(buffer.Rect{W: 2, H: 2})
	b.Border = buffer.BorderPlain
	b.SetTitleString("hi", buffer.PlainStyle)
	sink := widgettest.Render(t, 2, 2, 1, b)

	want := []string{
		string(g.TopLeft) + string(g.TopRight),
		string(g.BottomLeft) + string(g.BottomRight),
	}
	for i, w := range want {
		if got := widgettest.Row(sink, i); got != w {
			t.Errorf("row %d\n got %q\nwant %q", i, got, w)
		}
	}
}

// TestBlockNoTitleBelowWidth5 covers the second threshold exactly: a title
// additionally needs W>=5, which is two corners, two pad spaces and one glyph.
func TestBlockNoTitleBelowWidth5(t *testing.T) {
	g := plainSet()
	// noTitle builds a plain top row with no title, for the widths below.
	noTitle := func(w int) string {
		return string(g.TopLeft) + repeat(g.Horizontal, w-2) + string(g.TopRight)
	}

	for w := MinBorderW; w < MinTitleW; w++ {
		b := plain(w, 3, "x")
		if got := widgettest.Row(widgettest.Render(t, w, 3, 1, b), 0); got != noTitle(w) {
			t.Errorf("width %d: the title should not fit\n got %q\nwant %q", w, got, noTitle(w))
		}
	}
	// And at exactly MinTitleW it does fit, which is what makes 5 the threshold
	// rather than 6.
	b := plain(MinTitleW, 3, "x")
	if got, want := widgettest.Row(widgettest.Render(t, MinTitleW, 3, 1, b), 0), string(g.TopLeft)+" x "+string(g.TopRight); got != want {
		t.Errorf("width %d: a one-glyph title must fit\n got %q\nwant %q", MinTitleW, got, want)
	}
}

// TestBlockTitleTruncatesRatherThanDrops is ADR 0008 §2's "truncated, not
// dropped", with the consequence it exists for: the user learns there was more.
func TestBlockTitleTruncatesRatherThanDrops(t *testing.T) {
	const w = 10
	b := plain(w, 3, "a very long title indeed")
	g := plainSet()
	got := widgettest.Row(widgettest.Render(t, w, 3, 1, b), 0)

	if !strings.Contains(got, buffer.TruncSuffix) {
		t.Errorf("an over-long title was dropped or clipped without a marker: %q", got)
	}
	if n := buffer.StringWidth(got); n != w {
		t.Errorf("title row is %d cells, want %d: it must not spill past the corner", n, w)
	}
	if !strings.HasPrefix(got, string(g.TopLeft)) || !strings.HasSuffix(got, string(g.TopRight)) {
		t.Errorf("the corners were not preserved: %q", got)
	}
}

// TestBlockTitleTruncatesOnShrink is the case a grow-only resize test cannot
// catch: the truncation is cached against the rect it was computed for, so a
// block that fitted its title in 40 cells and is then shown in 10 recomputes
// rather than painting the old, over-long title.
func TestBlockTitleTruncatesOnShrink(t *testing.T) {
	b := plain(40, 3, "a very long title indeed")
	buf := buffer.NewBuffer(40, 3)
	b.Draw(buf)
	if got := rowOf(buf, 0); strings.Contains(got, buffer.TruncSuffix) {
		t.Errorf("at width 40 the title fits and must not be truncated: %q", got)
	}

	b.SetBounds(buffer.Rect{W: 10, H: 3})
	b.Draw(buf)
	if got := rowOf(buf, 0); !strings.Contains(got, buffer.TruncSuffix) {
		t.Errorf("after shrinking to 10 the cached wide title was reused: %q", got)
	}
}

// TestBlockWritesOnlyInsideBounds is the half of the shrink test that a cached
// truncation bug would also break: a sentinel painted outside the block's new
// bounds must survive, because a widget writes only inside Bounds.
func TestBlockWritesOnlyInsideBounds(t *testing.T) {
	b := plain(40, 3, "a very long title indeed")
	buf := buffer.NewBuffer(40, 3)
	b.Draw(buf)

	const sentinel = '!'
	outside := buffer.Rect{X: 10, Y: 0, W: 5, H: 3}
	buf.FillRect(outside, buffer.PlainStyle.Cell(sentinel))
	b.SetBounds(buffer.Rect{W: 10, H: 3})
	b.Draw(buf)

	for y := outside.Y; y < outside.Bottom(); y++ {
		for x := outside.X; x < outside.Right(); x++ {
			if got := buf.CellAt(x, y).Rune(); got != sentinel {
				t.Fatalf("the block wrote %q at (%d,%d), outside its 10-wide bounds", got, x, y)
			}
		}
	}
}

// TestBlockShrinkRepaintsWholeRect is ADR 0007 §1 rule 3 at the widget level: a
// block that showed a title and is then asked to show none, at the same bounds,
// must repaint the row the title was on. The renderer diffs and never clears, so
// a Block that only drew its border would leave the title on screen forever.
func TestBlockShrinkRepaintsWholeRect(t *testing.T) {
	b := plain(30, 5, "a very long title indeed")
	buf := buffer.NewBuffer(30, 5)
	b.Draw(buf)
	if got := rowOf(buf, 0); !strings.Contains(got, "a very") {
		t.Fatalf("setup: the title should be on the top row, got %q", got)
	}

	b.SetTitleString("", buffer.PlainStyle)
	b.Draw(buf)

	g := plainSet()
	want := string(g.TopLeft) + repeat(g.Horizontal, 28) + string(g.TopRight)
	if got := rowOf(buf, 0); got != want {
		t.Errorf("the title row was not repainted\n got %q\nwant %q", got, want)
	}
}

// TestBlockBackgroundIsRepainted checks the other half of the same rule: the
// background changes style, and every cell must take the new one rather than a
// mixture of two frames.
func TestBlockBackgroundIsRepainted(t *testing.T) {
	// The two backgrounds must be DISTINGUISHABLE, and the rect must have
	// interior cells. Getting either wrong makes this test vacuous: two equal
	// backgrounds can never fail, and at H=2 the border loop writes every cell
	// so the background fill is never the thing under test.
	//
	// DefaultColour is a plain byte copy, so use an explicit RGB colour
	// instead — passing DefaultColour for both is the trap this comment names.
	one := buffer.NewStyle(buffer.NewColour(0, 0, 0), buffer.NewColour(0, 0, 0x80), 0)
	two := buffer.NewStyle(buffer.NewColour(0, 0, 0), buffer.NewColour(0, 0, 0xff), 0)
	if one.BG == two.BG {
		t.Fatal("test setup: the two backgrounds are identical, so this cannot fail")
	}

	b := New(buffer.Rect{W: 6, H: 3})
	b.SetBackground(one)
	buf := buffer.NewBuffer(6, 3)
	b.Draw(buf)

	b.SetBackground(two)
	b.Draw(buf)

	// (2,1) is an interior cell: on no border edge, so the background fill is
	// the only thing that can write it.
	if got := buf.CellAt(2, 1).BG; got != two.BG {
		t.Errorf("interior cell (2,1) kept background %#x, want %#x; the fill is not repainting", got, two.BG)
	}
	for y := 0; y < 3; y++ {
		for x := 0; x < 6; x++ {
			if got := buf.CellAt(x, y).BG; got != two.BG {
				t.Errorf("cell (%d,%d) kept the old background %#x, want %#x", x, y, got, two.BG)
			}
		}
	}
}

// TestBlockTitleAlignmentWithinInterior pins that alignment is applied to the
// W-2 span between the corners and that the padded width is the title's own width
// plus two, so all three alignments end inside the border.
func TestBlockTitleAlignmentWithinInterior(t *testing.T) {
	const w, title = 13, "ab"
	const avail, padded = w - 2, 4

	g := plainSet()
	for _, tc := range []struct {
		name   string
		align  geometry.Align
		offset int
	}{
		{"left", geometry.AlignLeft, 0},
		{"center", geometry.AlignCenter, (avail - padded) / 2},
		{"right", geometry.AlignRight, avail - padded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := plain(w, 3, title)
			b.TitleAlign = tc.align
			got := widgettest.Row(widgettest.Render(t, w, 3, 1, b), 0)
			want := string(g.TopLeft) + repeat(g.Horizontal, tc.offset) + " ab " +
				repeat(g.Horizontal, avail-tc.offset-padded) + string(g.TopRight)
			if got != want {
				t.Errorf("\n got %q\nwant %q", got, want)
			}
		})
	}
}

// TestBlockAlignmentSurvivesATitleWiderThanTheInteriorIs covers the interaction
// of alignment with truncation: a centred, truncated title is still exactly
// interior-width, because Truncate returns at most avail cells.
func TestBlockAlignmentSurvivesATitleWiderThanTheInterior(t *testing.T) {
	for _, align := range []geometry.Align{geometry.AlignLeft, geometry.AlignCenter, geometry.AlignRight} {
		const w = 9
		b := plain(w, 3, "a very long title indeed")
		b.TitleAlign = align
		got := widgettest.Row(widgettest.Render(t, w, 3, 1, b), 0)
		if n := buffer.StringWidth(got); n != w {
			t.Errorf("%v: title row is %d cells, want %d\n%q", align, n, w, got)
		}
	}
}

// TestBlockBorderNoneDrawsNothingAndStillPaints covers BorderNone: no border
// cells, no title, but the background is still painted, because "clip, never
// blank" and because a block that stopped painting would leave stale cells.
func TestBlockBorderNoneDrawsNothingAndStillPaints(t *testing.T) {
	bg := buffer.NewStyle(buffer.NewColour(0, 0x20, 0x40), buffer.DefaultColour, 0)
	b := New(buffer.Rect{W: 8, H: 3})
	b.SetBackground(bg)
	b.SetTitleString("hi", buffer.PlainStyle)

	if got := b.Border; got != buffer.BorderNone {
		t.Errorf("the zero Block's border is %v, want BorderNone", got)
	}
	sink := widgettest.Render(t, 8, 3, 1, b)
	for y := 0; y < 3; y++ {
		for x := 0; x < 8; x++ {
			if got := sink.CellAt(x, y).Rune(); got != ' ' {
				t.Fatalf("BorderNone drew %q at (%d,%d)", got, x, y)
			}
		}
		if got := sink.CellAt(0, y).BG; got != bg.BG {
			t.Errorf("row %d background is %#x, want %#x: the block must still paint", y, got, bg.BG)
		}
	}
}

// TestBlockASCIIRung checks the one boolean: with Ascii set, every style renders
// from the ASCII table rather than from its own glyphs.
func TestBlockASCIIRung(t *testing.T) {
	b := New(buffer.Rect{W: 8, H: 3})
	b.Border = buffer.BorderRounded
	b.Ascii = true
	got := widgettest.Row(widgettest.Render(t, 8, 3, 1, b), 0)

	if want := "+" + strings.Repeat("-", 6) + "+"; got != want {
		t.Errorf("ASCII top row\n got %q\nwant %q", got, want)
	}
	a := buffer.BorderRounded.Glyphs(true)
	if a.Horizontal != '-' || a.Vertical != '|' {
		t.Errorf("the ASCII table is not what the block drew: %+v", a)
	}
}

// TestBlockTruncatedTitleUsesTheASCIIMarker checks that the marker variant, not
// just the glyphs, follows the rung — so a non-UTF-8 terminal gets no mojibake.
func TestBlockTruncatedTitleUsesTheASCIIMarker(t *testing.T) {
	b := New(buffer.Rect{W: 8, H: 3})
	b.Border = buffer.BorderPlain
	b.Ascii = true
	b.SetTitleString("a long title", buffer.PlainStyle)
	got := widgettest.Row(widgettest.Render(t, 8, 3, 1, b), 0)

	if !strings.Contains(got, buffer.AscTruncSuffix) {
		t.Errorf("ASCII rung did not use AscTruncSuffix: %q", got)
	}
	if strings.Contains(got, buffer.TruncSuffix) {
		t.Errorf("ASCII rung used the Unicode marker: %q", got)
	}
}

// TestBlockInteriorAfterBorderAndPadding is the composition contract: a widget
// drawn inside Interior() never touches the border, whatever the padding.
func TestBlockInteriorAfterBorderAndPadding(t *testing.T) {
	for _, tc := range []struct {
		name   string
		border buffer.BorderStyle
		pad    int
		w, h   int
		want   buffer.Rect
	}{
		{"border only", buffer.BorderPlain, 0, 10, 5, buffer.Rect{X: 1, Y: 1, W: 8, H: 3}},
		{"border and one of padding", buffer.BorderPlain, 1, 10, 5, buffer.Rect{X: 2, Y: 2, W: 6, H: 1}},
		{"no border, so padding only", buffer.BorderNone, 1, 10, 5, buffer.Rect{X: 1, Y: 1, W: 8, H: 3}},
		{"neither", buffer.BorderNone, 0, 10, 5, buffer.Rect{X: 0, Y: 0, W: 10, H: 5}},
		{"padding larger than the rect clips to empty", buffer.BorderPlain, 3, 10, 5, buffer.Rect{}},
		// Below the border threshold the border consumes nothing, so the whole
		// 1x1 rect is interior: there is no room for a border and blanking the
		// only cell would lose content for no reason.
		{"1x1 has no border, so it is all interior", buffer.BorderPlain, 0, 1, 1, buffer.Rect{W: 1, H: 1}},
		{"too narrow for a border", buffer.BorderPlain, 0, 1, 5, buffer.Rect{W: 1, H: 5}},
	} {
		b := New(buffer.Rect{W: tc.w, H: tc.h})
		b.Border = tc.border
		b.SetPadding(tc.pad)
		if got := b.Interior(); got != tc.want {
			t.Errorf("%s: Interior = %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

// TestBlockEveryBorderStyleDrawsItsOwnRunes is the catalog-wide guarantee: each
// style renders from its own table, and no two styles share glyphs by accident.
func TestBlockEveryBorderStyleDrawsItsOwnRunes(t *testing.T) {
	for _, st := range []buffer.BorderStyle{
		buffer.BorderPlain, buffer.BorderRounded, buffer.BorderDouble, buffer.BorderThick,
	} {
		g := st.Glyphs(false)
		if g.Horizontal == 0 || g.TopLeft == 0 {
			t.Errorf("%v has no glyphs", st)
			continue
		}
		b := New(buffer.Rect{W: 6, H: 3})
		b.Border = st
		got := widgettest.Row(widgettest.Render(t, 6, 3, 1, b), 0)
		want := string(g.TopLeft) + repeat(g.Horizontal, 4) + string(g.TopRight)
		if got != want {
			t.Errorf("%v top row\n got %q\nwant %q", st, got, want)
		}
	}
}

// TestBlockDrawIsTotalAtEverySize is ADR 0007 §4's contract applied to Block:
// every rect, including empty and 1x1, must be defined and must not panic, for
// every border style and for an out-of-range one arriving from untrusted input.
func TestBlockDrawIsTotalAtEverySize(t *testing.T) {
	for w := 0; w <= 8; w++ {
		for h := 0; h <= 8; h++ {
			for _, st := range []buffer.BorderStyle{
				buffer.BorderNone, buffer.BorderPlain, buffer.BorderRounded,
				buffer.BorderDouble, buffer.BorderThick, buffer.BorderASCII,
				buffer.BorderStyle(200),
			} {
				b := New(buffer.Rect{W: w, H: h})
				b.Border = st
				b.SetTitleString("a long title", buffer.PlainStyle)
				b.SetPadding(2)
				widgettest.Render(t, max1(w), max1(h), 1, b)
			}
		}
	}
}

// TestBlockDrawIsAllocationFree is the §4 guard: Draw builds nothing derived from
// its size. The truncation happens once per rect, not once per frame.
func TestBlockDrawIsAllocationFree(t *testing.T) {
	for _, w := range []int{40, 12, 6} {
		b := plain(w, 9, "a very long title indeed")
		buf := buffer.NewBuffer(w, 9)
		b.Draw(buf) // warm the truncation cache

		if got := testing.AllocsPerRun(200, func() { b.Draw(buf) }); got != 0 {
			t.Errorf("width %d: Draw allocated %.1f objects per run, want 0. Truncate belongs in the "+
				"size-change check, not in Draw (ADR 0008 §4)", w, got)
		}
	}
}

// TestBlockDrawIsAllocationFreeWithoutTitle is the same guard for the common
// case of a block with no title at all, where nothing should be cached at all.
func TestBlockDrawIsAllocationFreeWithoutTitle(t *testing.T) {
	b := New(buffer.Rect{W: 20, H: 5})
	b.Border = buffer.BorderPlain
	buf := buffer.NewBuffer(20, 5)
	b.Draw(buf)

	if got := testing.AllocsPerRun(200, func() { b.Draw(buf) }); got != 0 {
		t.Errorf("Draw allocated %.1f objects per run, want 0", got)
	}
}

// TestBlockSetTitleWithNoWidthClearsTheTitle keeps the W>=5 threshold honest: if
// an empty title occupied its two pad spaces it would fit at width 4 and the
// threshold would be a lie.
func TestBlockSetTitleWithNoWidthClearsTheTitle(t *testing.T) {
	b := plain(20, 3, "x")
	b.SetTitleString("", buffer.PlainStyle)
	if got := b.TitleWidth(); got != 0 {
		t.Errorf("TitleWidth after an empty title = %d, want 0", got)
	}
	if got := b.Title(); got != nil {
		t.Errorf("Title after an empty title = %v, want nil", got)
	}
}

// TestBlockMixedStyleTitlePadsWithTheFirstSpan pins the one rule the ADR leaves
// to the widget: both automatic pad spaces take the first non-empty span's style,
// so the two ends match.
func TestBlockMixedStyleTitlePadsWithTheFirstSpan(t *testing.T) {
	b := New(buffer.Rect{W: 20, H: 3})
	b.Border = buffer.BorderPlain
	b.SetTitle([]buffer.Span{
		buffer.NewSpan("a", buffer.BoldStyle),
		buffer.NewSpan("b", buffer.UnderlineStyle),
	})
	title := b.Title()
	if len(title) != 4 {
		t.Fatalf("Title has %d spans, want 4: two pads and two runs", len(title))
	}
	if title[0].Style != buffer.BoldStyle || title[3].Style != buffer.BoldStyle {
		t.Errorf("pad styles have attrs %v and %v, want both the first span's %v",
			title[0].Style.Attr, title[3].Style.Attr, buffer.BoldStyle.Attr)
	}
}

// TestBlockHandlesNothing asserts a Block consumes no events, which is what lets
// a parent forward keys straight to the content it composed inside.
func TestBlockHandlesNothing(t *testing.T) {
	b := plain(10, 3, "x")
	if b.Handle(termmosaic.Event{Kind: termmosaic.EventKey, Rune: 'q'}) {
		t.Error("a Block consumed a key event; focus belongs to its content")
	}
	b.Invalidate() // must not panic
}

// max1 clamps a degenerate dimension so a zero-sized screen, which has no cells
// to draw into, is not asked to prove anything.
func max1(v int) int {
	if v < 1 {
		return 1
	}
	return v
}
