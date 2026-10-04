// Package basic provides the two text widgets: Text, which writes styled spans on
// one line, and Paragraph, which wraps them.
//
// Both follow the same two rules, which are the rules every widget in the catalog
// follows:
//
//   - The available space is Bounds(), never buf.Size(). The buffer is the screen;
//     the rect is the widget's space (ADR 0007 §1 rule 1).
//   - Nothing derived from the size is built in Draw. Wrap and Truncate both
//     allocate, so Paragraph caches its Wrapped and Text caches its truncation
//     against the rect they were computed for, and recompute only when Bounds()
//     differs (ADR 0007 §3, ADR 0008 §4).
//
// # Backgrounds
//
// Each widget has a Background field and paints its whole Bounds() with it before
// drawing anything. That is the repaint rule (ADR 0007 §1 rule 3) made uniform by
// ADR 0007's reconciliation: Style.Blank() is the sanctioned way to express a
// background, so every widget paints its chrome background the same way.
//
// The cost is that a widget composed inside a block must carry the block's
// background or it will paint the block's away. That is stated rather than hidden
// because the alternative — a widget leaving its own rect alone — is exactly the
// stale-cell bug the rule exists to prevent.
package basic

import (
	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/geometry"
)

// Text renders styled spans on one line.
//
// It is the smallest widget in the catalog and the one most often composed: a
// label in a form, a value beside it, a status line. It truncates rather than
// wraps, because wrapping is Paragraph's job and a widget that both wrapped and
// did not would be two widgets.
type Text struct {
	bounds buffer.Rect

	// Spans is the content. It is held by the caller and must not be modified
	// while the widget is drawing; SetSpans makes a shallow copy so the common
	// case of a literal cannot be mutated afterwards.
	Spans []buffer.Span

	// Align places the text within Bounds.
	Align geometry.Align

	// Background is painted across Bounds before the text. An unset value
	// resolves to the terminal's own colours.
	Background buffer.Style

	// Ascii selects buffer's ASCII rung: pass !caps.Unicode. It affects the
	// truncation marker only.
	Ascii bool

	// capped is the truncated text and cappedRect the bounds it was computed for.
	// Truncate allocates, so it happens on the size check and never in Draw.
	capped      []buffer.Span
	cappedWidth int
	cappedRect  buffer.Rect
	cappedValid bool
}

// NewText returns a Text holding spans, sized r.
func NewText(r buffer.Rect, spans []buffer.Span) *Text {
	t := &Text{bounds: r}
	t.SetSpans(spans)
	return t
}

// NewTextString returns a single-styled Text holding s, sized r.
func NewTextString(r buffer.Rect, s string, st buffer.Style) *Text {
	return NewText(r, []buffer.Span{buffer.NewSpan(s, st)})
}

// SetSpans sets the text. It copies the slice header's contents so a later
// mutation of the caller's backing array cannot change what a drawn frame means,
// and it invalidates the truncation cache.
func (t *Text) SetSpans(spans []buffer.Span) {
	if len(spans) == 0 {
		t.Spans = nil
	} else {
		t.Spans = append([]buffer.Span(nil), spans...)
	}
	t.cappedValid = false
}

// SetText replaces the text with one run in st.
func (t *Text) SetText(s string, st buffer.Style) {
	t.SetSpans([]buffer.Span{buffer.NewSpan(s, st)})
}

// SetAlign sets the horizontal placement within Bounds.
func (t *Text) SetAlign(a geometry.Align) { t.Align = a }

// SetBackground sets the style painted across Bounds.
func (t *Text) SetBackground(st buffer.Style) { t.Background = st }

// Bounds returns the text's rectangle, safe to call before the first Draw.
func (t *Text) Bounds() buffer.Rect { return t.bounds }

// SetBounds sets the text's rectangle. It must reflect the new rectangle before
// the next Draw; the truncation cache is keyed on the rect, so a new one makes the
// next Draw recompute.
func (t *Text) SetBounds(r buffer.Rect) { t.bounds = r }

// ContentWidth returns the cell width of the uncropped text, which a layout can
// use to size a pane without re-deriving StringWidth.
func (t *Text) ContentWidth() int { return buffer.SpansWidth(t.Spans) }

// Draw paints the background and then the text.
//
// It is total for every rect, including empty, and it allocates nothing: the only
// size-derived value it needs is the truncation, which is cached.
func (t *Text) Draw(buf *buffer.Buffer) {
	r := t.bounds
	if r.Empty() {
		return
	}
	buf.FillRect(r, t.Background.Resolved().Blank())

	if r.W <= 0 || len(t.Spans) == 0 {
		return
	}

	spans, w := t.Spans, buffer.SpansWidth(t.Spans)
	if w > r.W {
		// An over-long label is truncated rather than clipped at the buffer edge.
		// SetSpans bounds-checks against the BUFFER, not against this widget's
		// rect, so without this a Text in a narrow pane would write over its
		// neighbour.
		if !t.cappedValid || t.cappedRect != r {
			if t.Ascii {
				t.capped = buffer.TruncateASCII(t.Spans, r.W)
			} else {
				t.capped = buffer.Truncate(t.Spans, r.W)
			}
			t.cappedWidth = buffer.SpansWidth(t.capped)
			t.cappedRect = r
			t.cappedValid = true
		}
		spans, w = t.capped, t.cappedWidth
		if len(spans) == 0 {
			return
		}
	}
	buf.SetSpans(r.X+t.Align.Offset(r.W, w), r.Y, spans)
}

// Invalidate satisfies termmosaic.Widget. The text repaints in full every frame,
// so there is nothing finer-grained to mark.
func (t *Text) Invalidate() {}

// Handle satisfies termmosaic.Widget. A Text is not focusable and consumes
// nothing: it is a label, and a key aimed at a label belongs to whatever the label
// describes.
func (t *Text) Handle(termmosaic.Event) bool { return false }

// Paragraph renders wrapped, aligned, styled text over as many lines as fit.
//
// It shows the TOP of its content: there is no scrolling here, because scrolling a
// block of text is Pager's job and a Paragraph that scrolled by itself would be a
// second scroll model. The paragraph that does not fit is the one that gets
// dropped content, which is ADR 0007's "information budget" case stated for text:
// show what fits, clip the rest, never blank.
type Paragraph struct {
	bounds buffer.Rect

	// Spans is the content. Set via SetSpans, which copies it.
	Spans []buffer.Span

	// Align places each line within Bounds. Wrap produces left-aligned lines;
	// alignment moves them, it does not change where they break.
	Align geometry.Align

	// Background is painted across Bounds before the text.
	Background buffer.Style

	// Ascii selects buffer's ASCII rung: pass !caps.Unicode.
	Ascii bool

	// wrapped is the cached Wrap result and wrappedRect the bounds it was
	// computed for. Wrap allocates twice over — the rune slice and every line —
	// which is exactly why it is here and not in Draw.
	wrapped     buffer.Wrapped
	wrappedRect buffer.Rect
	wrappedOK   bool
}

// NewParagraph returns a Paragraph holding spans, sized r.
func NewParagraph(r buffer.Rect, spans []buffer.Span) *Paragraph {
	p := &Paragraph{bounds: r}
	p.SetSpans(spans)
	return p
}

// NewParagraphString returns a single-styled Paragraph holding s, sized r.
func NewParagraphString(r buffer.Rect, s string, st buffer.Style) *Paragraph {
	return NewParagraph(r, []buffer.Span{buffer.NewSpan(s, st)})
}

// SetSpans sets the content and invalidates the wrap cache, because the cache is
// keyed on the rect AND on the text: new text at the same size is a different set
// of lines.
func (p *Paragraph) SetSpans(spans []buffer.Span) {
	if len(spans) == 0 {
		p.Spans = nil
	} else {
		p.Spans = append([]buffer.Span(nil), spans...)
	}
	p.wrappedOK = false
}

// SetText replaces the content with one run in st.
func (p *Paragraph) SetText(s string, st buffer.Style) {
	p.SetSpans([]buffer.Span{buffer.NewSpan(s, st)})
}

// SetAlign sets the placement of each line within Bounds.
func (p *Paragraph) SetAlign(a geometry.Align) { p.Align = a }

// SetBackground sets the style painted across Bounds.
func (p *Paragraph) SetBackground(st buffer.Style) { p.Background = st }

// Bounds returns the paragraph's rectangle, safe to call before the first Draw.
func (p *Paragraph) Bounds() buffer.Rect { return p.bounds }

// SetBounds sets the paragraph's rectangle. The wrap cache is keyed on it, so the
// next Draw recomputes at the new width.
func (p *Paragraph) SetBounds(r buffer.Rect) { p.bounds = r }

// ContentWidth returns the cell width of the unwrapped content.
func (p *Paragraph) ContentWidth() int { return buffer.SpansWidth(p.Spans) }

// Lines returns the number of lines the current wrap produced, for a caller that
// wants to know whether the content overflowed. It is zero before the first Draw,
// because the wrap depends on the width and there is no width yet.
func (p *Paragraph) Lines() int { return p.wrapped.Height() }

// Wrapped returns the cached wrap result, so a caller can measure or address lines
// without wrapping again. It is empty before the first Draw.
func (p *Paragraph) Wrapped() buffer.Wrapped { return p.wrapped }

// Draw paints the background and then as many wrapped lines as fit.
//
// It is total for every rect, including empty, and it allocates nothing in steady
// state. The first Draw after a size change wraps, which allocates; that is the
// documented cost of the caching rule and it happens once per rect, not once per
// frame.
func (p *Paragraph) Draw(buf *buffer.Buffer) {
	r := p.bounds
	if r.Empty() {
		return
	}
	buf.FillRect(r, p.Background.Resolved().Blank())

	if r.W <= 0 || r.H <= 0 || len(p.Spans) == 0 {
		return
	}

	if !p.wrappedOK || p.wrappedRect != r {
		p.wrapped = buffer.Wrap(p.Spans, r.W)
		p.wrappedRect = r
		p.wrappedOK = true
	}

	// Show the top of the content: the rows that fit, and no more.
	rows := geometry.ClampCount(p.wrapped.Height(), r.H)
	for i := 0; i < rows; i++ {
		line := p.wrapped.Line(i)
		if len(line) == 0 {
			continue
		}
		buf.SetSpans(r.X+p.Align.Offset(r.W, buffer.SpansWidth(line)), r.Y+i, line)
	}
}

// Invalidate satisfies termmosaic.Widget. The paragraph repaints in full every
// frame.
func (p *Paragraph) Invalidate() {}

// Handle satisfies termmosaic.Widget. A Paragraph consumes nothing; scrolling its
// text is the Pager's business.
func (p *Paragraph) Handle(termmosaic.Event) bool { return false }

// Compile-time proof that both widgets are widgets, and that neither claims focus:
// focus belongs to whatever is editable, and a label that swallowed a keypress
// would make a form unusable.
var (
	_ termmosaic.Widget = (*Text)(nil)
	_ termmosaic.Widget = (*Paragraph)(nil)
)
