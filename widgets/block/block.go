// Package block provides Block, the catalog's only owner of borders and titles.
//
// ADR 0008 §2 fixes Block's contract for the whole catalog, and this file is that
// contract rather than a widget's opinion of it. List, Table, Tree, Pager, Tabs
// and everything else chrome-bearing compose a Block or call one; none of them
// contains a glyph table, a corner loop or a title threshold. If a second border
// appears anywhere in TermMosaic, that is a bug of exactly the kind this package
// exists to make impossible — the same collision that two copies of
// internal/ansi.Style had already produced in this repository.
//
// Everything visible here comes from buffer: every rune from
// buffer.BorderStyle.Glyphs, every style from a buffer.Style read through
// Resolved, every title run from a buffer.Span. Block spells no border rune and
// invents no styling vocabulary.
//
// # Thresholds
//
// Two sizes are decided here and nowhere else, because three authors would each
// pick their own and disagree:
//
//	MinBorderW, MinBorderH = 2  // one cell per corner on each axis
//	MinTitleW             = 5  // two corners, two pad spaces, one glyph
//
// Below the border threshold there is no border and no title, but the background
// is still painted and any content the composition puts inside still draws:
// "clip, never blank" (ADR 0007 §4). Below the title threshold the border draws
// and the title does not.
//
// # Painting and composition
//
// A Block paints its whole Bounds() in Background before anything else, because
// the renderer diffs and never clears: a widget that shrinks its content and
// does not repaint leaves stale cells on screen. Interior reports the rect left
// over after the border and the padding, and it is the rect a composing widget
// should be given.
//
// A Block is not focusable and Handle consumes nothing. Its Invalidate is a no-op
// for the same reason examples/hello's is: it repaints in full every frame and
// has no finer-grained state to mark.
package block

import (
	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/geometry"
)

// Block thresholds, fixed by ADR 0008 §2 for the entire catalog.
const (
	// MinBorderW is the narrowest rect that can carry a border: two cells, one
	// for each corner.
	MinBorderW = 2
	// MinBorderH is the shortest rect that can carry a border, for the same
	// reason.
	MinBorderH = 2
	// MinTitleW is the narrowest rect that can carry a title: the two corner
	// cells, the one space Block inserts on each side of the title, and one
	// glyph of title.
	MinTitleW = MinBorderW + titlePad + 1
	// titlePad is the one space Block inserts on each side of a title, so a
	// title's occupied width is its own width plus 2. It is not exported because
	// it is arithmetic inside MinTitleW rather than a decision a caller makes.
	titlePad = 2
)

// Block is a bordered region with an optional title. It draws chrome only: it
// has no content of its own beyond the background and the border.
//
// The zero Block draws no border (Border is BorderNone) with the terminal's own
// colours, which is a legal and unremarkable block. It is usable as constructed.
//
// A Block is a plain value in every respect that matters: it is not safe for
// concurrent use, because SetBounds and Draw race by design — the renderer owns
// Bounds between frames (ADR 0003).
type Block struct {
	// bounds is the widget's rectangle. Set with SetBounds or Bounds.
	bounds buffer.Rect

	// Border is the glyph set. BorderNone draws no border, no title and no
	// chrome cells; Interior then accounts for the padding alone.
	Border buffer.BorderStyle

	// BorderStyle is the rendition of the border cells. Read through
	// Resolved; an unset value is the terminal's own colours.
	BorderStyle buffer.Style

	// Background is the rendition Block paints its whole rect in before drawing
	// anything else. An unset value resolves to the terminal's own colours, so a
	// Block never invents a background the application did not ask for.
	//
	// A widget composed inside a Block should carry the same Background, or the
	// composition will paint the block's background away. That is the price of
	// the repaint-everything rule being stated once, here, for the whole catalog.
	Background buffer.Style

	// title is the padded title: the author's spans with one space in front and
	// behind, built by SetTitle rather than in Draw.
	title []buffer.Span
	// titleWidth is SpansWidth(title), cached with it.
	titleWidth int

	// TitleAlign places the title within the span between the two top corners,
	// which is W-2 cells wide. AlignLeft is the default and the zero value.
	TitleAlign geometry.Align

	// Ascii selects buffer's ASCII rung: pass !caps.Unicode.
	//
	// Block never reads Caps itself. It is the one boolean every widget passes
	// to buffer's shared lookup, and reading a global here is what ADR 0008
	// Decision 4 forbids.
	Ascii bool

	// Padding is inset from the border on all four sides, uniformly. Per-side
	// padding is deferred by ADR 0008; geometry.Rect.Inset is uniform by design.
	Padding int

	// capped is the truncated title, and cappedRect the bounds it was computed
	// for. Truncate allocates, so it runs from the size check and never from
	// Draw (ADR 0008 §4).
	capped      []buffer.Span
	cappedWidth int
	cappedRect  buffer.Rect
	cappedValid bool
}

// New returns a Block with no border and no title, sized r.
func New(r buffer.Rect) *Block {
	b := &Block{bounds: r}
	return b
}

// Bounds returns the block's rectangle. It is safe to call before the block has
// ever been drawn.
func (b *Block) Bounds() buffer.Rect { return b.bounds }

// SetBounds sets the block's rectangle, which is how a layout or a parent hands
// it space. It must reflect the new rectangle before the next Draw (ADR 0007
// §3); the cached truncation is keyed on the rect, so setting a new one makes
// the next Draw recompute it.
func (b *Block) SetBounds(r buffer.Rect) { b.bounds = r }

// SetBorder sets the glyph set.
func (b *Block) SetBorder(s buffer.BorderStyle) { b.Border = s }

// SetBorderStyle sets the rendition of the border cells.
func (b *Block) SetBorderStyle(st buffer.Style) { b.BorderStyle = st }

// SetBackground sets the rendition Block paints its whole rect in.
func (b *Block) SetBackground(st buffer.Style) { b.Background = st }

// SetPadding sets the uniform inset between the border and the content.
func (b *Block) SetPadding(n int) {
	if n < 0 {
		n = 0
	}
	b.Padding = n
}

// SetTitleAlign sets where the title sits within the span between the two top
// corners.
func (b *Block) SetTitleAlign(a geometry.Align) { b.TitleAlign = a }

// SetTitle sets the title from styled spans.
//
// Block inserts one space on each side of the title itself; a caller must NOT
// add them. The pad spaces take the style of the first non-empty span, so both
// ends follow one rule rather than two.
//
// An empty title — one whose spans have no cell width — clears the title rather
// than leaving two spaces on the border, which is why the W>=5 threshold is
// exactly the width at which a one-glyph title fits.
func (b *Block) SetTitle(spans []buffer.Span) {
	if buffer.SpansWidth(spans) <= 0 {
		b.title = nil
		b.titleWidth = 0
		b.cappedValid = false
		return
	}
	// The padded title is built here rather than in Draw: it depends on the
	// title, not on the size, and building it per frame would allocate.
	pad := buffer.NewSpan(" ", padStyle(spans))
	out := make([]buffer.Span, 0, len(spans)+2)
	out = append(out, pad)
	out = append(out, spans...)
	out = append(out, pad)
	b.title = out
	b.titleWidth = buffer.SpansWidth(out)
	b.cappedValid = false
}

// SetTitleString sets the title to a single styled run. It is the uniform
// convenience over SetTitle.
func (b *Block) SetTitleString(s string, st buffer.Style) {
	b.SetTitle([]buffer.Span{buffer.NewSpan(s, st)})
}

// TitleWidth returns the cell width the title occupies, including the one space
// on each side Block adds, or 0 if there is no title. It is exported so a layout
// can ask how much room the chrome needs without re-deriving titlePad.
func (b *Block) TitleWidth() int { return b.titleWidth }

// Title returns the padded title spans, or nil if there is no title. The result
// aliases Block's own storage and must not be modified.
func (b *Block) Title() []buffer.Span { return b.title }

// padStyle returns the style the title's automatic padding wears: that of the
// first non-empty span, or DefaultStyle when every span is empty. One rule for
// both ends keeps the two pad spaces symmetric, which matters because a space is
// only ever visible as background.
func padStyle(spans []buffer.Span) buffer.Style {
	for i := range spans {
		if spans[i].Text != "" {
			return spans[i].Style
		}
	}
	return buffer.DefaultStyle
}

// Interior returns the rectangle left inside the border and the padding: where a
// composed widget belongs.
//
// It is computed from Bounds and the current configuration, so it costs three
// comparisons and one Inset and allocates nothing — cheap enough to call from a
// parent's Draw, which is how it is meant to be used:
//
//	left := block.New(r)
//	left.SetBounds(parent.PaneRect(0))
//	left.Draw(buf)
//	content := block.New(left.Interior())
//	content.SetSpans(0, 0, []buffer.Span{…})
//
// The result is clipped rather than blank: with BorderNone the border consumes
// nothing and only the padding applies, and a rect too small for either yields
// an empty rect rather than a negative one.
func (b *Block) Interior() buffer.Rect {
	r := b.bounds
	if r.Empty() {
		return r
	}
	inset := b.Padding
	if b.Border != buffer.BorderNone && r.W >= MinBorderW && r.H >= MinBorderH {
		inset++
	}
	return r.Inset(inset)
}

// Draw paints the block's background, then its border, then its title.
//
// It is total: defined for every rect including empty, 0x0 and 1x1, and it
// allocates nothing. The title's truncation is cached against the rect it was
// computed for and recomputed only when the rect differs, which is ADR 0007 §3's
// lazy rect-keyed rule applied to the one allocating operation Block performs.
func (b *Block) Draw(buf *buffer.Buffer) {
	r := b.bounds
	if r.Empty() {
		// The degenerate-size contract: an empty Bounds returns immediately.
		// Cheap, and thirty widgets testing the same condition is the point of
		// deciding it once.
		return
	}

	// Repaint the whole rect first. The renderer diffs and never clears, so a
	// block that drew a title last frame and has none this frame would otherwise
	// leave the old one on screen.
	buf.FillRect(r, b.Background.Resolved().Blank())

	if b.Border == buffer.BorderNone {
		return
	}
	if r.W < MinBorderW || r.H < MinBorderH {
		// Too small for a border, and therefore for a title. The background is
		// already painted and any composed content still draws: clip, never
		// blank.
		return
	}

	g := b.Border.Glyphs(b.Ascii)
	st := b.BorderStyle.Resolved()

	if g.Horizontal != 0 {
		h := st.Cell(g.Horizontal)
		for x := r.X + 1; x < r.Right()-1; x++ {
			buf.SetCell(x, r.Y, h)
			buf.SetCell(x, r.Bottom()-1, h)
		}
	}
	if g.Vertical != 0 {
		v := st.Cell(g.Vertical)
		for y := r.Y + 1; y < r.Bottom()-1; y++ {
			buf.SetCell(r.X, y, v)
			buf.SetCell(r.Right()-1, y, v)
		}
	}
	if g.TopLeft != 0 {
		buf.SetCell(r.X, r.Y, st.Cell(g.TopLeft))
		buf.SetCell(r.Right()-1, r.Y, st.Cell(g.TopRight))
		buf.SetCell(r.X, r.Bottom()-1, st.Cell(g.BottomLeft))
		buf.SetCell(r.Right()-1, r.Bottom()-1, st.Cell(g.BottomRight))
	}

	b.drawTitle(buf, r)
}

// drawTitle writes the title over the top border row, between the two corners.
//
// The title overwrites border cells rather than sitting on a row of its own, is
// inset by one space on each side, and is aligned within the W-2 span between
// the corners. An over-long title is truncated rather than dropped, so the user
// learns there was more.
func (b *Block) drawTitle(buf *buffer.Buffer, r buffer.Rect) {
	if b.titleWidth == 0 || r.W < MinTitleW {
		return
	}
	avail := r.W - titlePad // the span between the corner cells
	spans, w := b.title, b.titleWidth
	if w > avail {
		if !b.cappedValid || b.cappedRect != r {
			b.capped = b.truncateTitle(avail)
			b.cappedWidth = buffer.SpansWidth(b.capped)
			b.cappedRect = r
			b.cappedValid = true
		}
		spans, w = b.capped, b.cappedWidth
	}
	if w > avail {
		// Truncate cannot exceed maxWidth, so this is unreachable; the guard is
		// here so a future change to the cache cannot turn into a spill past the
		// right corner.
		return
	}
	buf.SetSpans(r.X+1+b.TitleAlign.Offset(avail, w), r.Y, spans)
}

// truncateTitle is the one allocating operation in this package. It picks the
// marker variant from the ASCII rung so a non-UTF-8 terminal gets "~" and the
// layout is identical either way — both markers are one cell wide.
func (b *Block) truncateTitle(avail int) []buffer.Span {
	if b.Ascii {
		return buffer.TruncateASCII(b.title, avail)
	}
	return buffer.Truncate(b.title, avail)
}

// Invalidate satisfies termmosaic.Widget. The block repaints in full every frame
// and holds no state finer-grained than its bounds, so there is nothing to mark.
func (b *Block) Invalidate() {}

// Handle satisfies termmosaic.Widget. A Block has no focus and no interaction:
// it consumes nothing, and a composing parent forwards events to its content.
func (b *Block) Handle(termmosaic.Event) bool { return false }

// Block is a Widget. It is deliberately NOT Focusable: focus belongs to whatever
// the block contains, and a chrome widget that claimed it would swallow keys the
// composed content needed.
var _ termmosaic.Widget = (*Block)(nil)
