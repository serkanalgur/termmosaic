package buffer

import "unicode/utf8"

// Span is a run of text in one style.
//
// A widget holds []Span; it does not hold a string plus a parallel style slice
// or three loose colour arguments. SetSpans is the only supported way to write
// mixed-style text: the wide-glyph continuation rule is subtle enough (see
// SetSpans) that hand-rolling it is a permanent-flicker bug waiting to happen.
type Span struct {
	// Text is the run's content. Its cell width is StringWidth(Text), which is 0
	// for empty text.
	Text string
	// Style is this run's rendition. An unset Style resolves to DefaultStyle;
	// use NewSpan rather than a bare literal. SetSpans resolves each span's
	// style exactly once on entry, so an author never has to resolve by hand.
	Style Style
}

// NewSpan returns a Span. It does not resolve the style: resolution belongs to
// the write, so that a cached []Span keeps the author's intent rather than a
// snapshot of the terminal defaults.
func NewSpan(text string, st Style) Span {
	return Span{Text: text, Style: st}
}

// SpansWidth returns the total cell width of spans, summing StringWidth. It
// counts CELLS, not runes: a double-width rune contributes two.
func SpansWidth(spans []Span) int {
	w := 0
	for i := range spans {
		w += StringWidth(spans[i].Text)
	}
	return w
}

// TruncSuffix is the marker Truncate appends: U+2026 HORIZONTAL ELLIPSIS, one
// cell wide.
const TruncSuffix = "…"

// AscTruncSuffix is its ASCII-rung counterpart: "~", also one cell wide.
//
// The two are the same width on purpose, so the width arithmetic a caller does
// is identical on both rungs and the choice of Truncate variant cannot shift a
// layout. TestTruncSuffixesAreOneCellWide pins that.
const AscTruncSuffix = "~"

// Truncate returns the longest prefix of spans fitting maxWidth, with
// TruncSuffix appended in the style of the last span kept.
//
// If spans already fit it returns spans unchanged — the same slice, the same
// backing array, no allocation. That case is the common one on a widget whose
// content is shorter than the space it was given.
//
// It allocates when it actually truncates. Call it from the widget's size-change
// check, cache the result against the rect it was computed for, and read the
// cache in Draw. Calling Truncate inside Draw is the most likely performance
// regression in the catalog: it is exactly what ADR 0007 §3 and ADR 0008 §4
// forbid, and it is documentation rather than a type, so nothing stops you.
// Nothing here caches anything on your behalf.
//
// Rules, all of which are the contract:
//   - maxWidth <= 0 returns an empty slice; there is nowhere to put even the
//     marker.
//   - The marker occupies the final cell, so kept content has maxWidth-1 cells
//     to work with. The result's width is therefore <= maxWidth, never above.
//   - The marker takes the style of the last span kept, so it reads as a
//     continuation of the text rather than as an unstyled artifact. When no
//     content fits at all (maxWidth == 1, or a first rune that does not fit), it
//     takes the style of the first non-empty input span instead; there is no
//     "last kept span" to take it from.
//   - A double-width rune is never split, same as SetSpans and Wrap.
func Truncate(spans []Span, maxWidth int) []Span {
	return truncate(spans, maxWidth, TruncSuffix)
}

// TruncateASCII is Truncate with AscTruncSuffix. A widget that already knows
// caps.Unicode is false picks this variant and gets an identical layout, because
// both markers are one cell wide.
func TruncateASCII(spans []Span, maxWidth int) []Span {
	return truncate(spans, maxWidth, AscTruncSuffix)
}

// truncate is the shared body of Truncate and TruncateASCII; the two differ
// only in the marker, which is the whole reason a caller must not pick one by
// accident and silently change the width arithmetic.
func truncate(spans []Span, maxWidth int, marker string) []Span {
	if maxWidth <= 0 {
		return nil
	}
	if SpansWidth(spans) <= maxWidth {
		return spans
	}

	budget := maxWidth - StringWidth(marker)
	out := make([]Span, 0, len(spans)+1)
	used := 0
	for i := range spans {
		if used >= budget {
			break
		}
		w := StringWidth(spans[i].Text)
		if used+w <= budget {
			if w > 0 {
				out = append(out, spans[i])
				used += w
			}
			continue
		}
		head, n := cutToWidth(spans[i].Text, budget-used)
		if n > 0 {
			out = append(out, Span{Text: head, Style: spans[i].Style})
			used += n
		}
		break
	}

	st := truncMarkerStyle(out, spans)
	out = append(out, Span{Text: marker, Style: st})
	return out
}

// truncMarkerStyle answers "which style does the truncation marker take", once,
// so Truncate and TruncateASCII cannot disagree about it: the style of the last
// span kept, or — when nothing was kept — of the first non-empty input span.
func truncMarkerStyle(kept, input []Span) Style {
	if len(kept) > 0 {
		return kept[len(kept)-1].Style
	}
	for i := range input {
		if input[i].Text != "" {
			return input[i].Style
		}
	}
	return DefaultStyle
}

// cutToWidth returns the longest prefix of s whose cell width is at most max,
// and that width. A double-width rune that does not fit is dropped rather than
// half-taken, so the returned prefix may be narrower than max allows.
//
// max <= 0 returns an empty string.
func cutToWidth(s string, max int) (string, int) {
	if max <= 0 {
		return "", 0
	}
	w := 0
	for i, r := range s {
		rw := RuneWidth(r)
		if rw == 0 {
			// Zero-width runes occupy no cell, so a prefix may end after one
			// without exceeding max. They are carried into the kept text here
			// and dropped at write time, matching SetSpans.
			continue
		}
		if w+rw > max {
			return s[:i], w
		}
		w += rw
	}
	return s, w
}

// Wrapped is the result of Wrap: styled lines, ready to index.
//
// It is immutable once built and safe to cache on a widget. The Width field is
// part of the value so that a widget caching a Wrapped can compare the width it
// was built for against the width it now has — that comparison is ADR 0007 §3's
// rect-keyed cache, and this type supports it rather than reinventing it.
type Wrapped struct {
	// Lines holds each line as a slice of spans. A line is never empty; a Wrap
	// that would produce one (a dropped wide glyph with nowhere to go) omits it.
	Lines [][]Span
	// Width is the column width Wrap was given, echoed back for the size check.
	Width int
}

// Height returns the number of lines, i.e. how many cells tall the wrapped text
// is.
func (w Wrapped) Height() int { return len(w.Lines) }

// Line returns line i's spans, or nil if i is out of range. It never panics, so
// a widget showing fewer lines than the text needs simply gets nil and draws
// nothing — "clip, never blank" (ADR 0007 §4).
func (w Wrapped) Line(i int) []Span {
	if i < 0 || i >= len(w.Lines) {
		return nil
	}
	return w.Lines[i]
}

// Wrap breaks spans into lines of at most width cells, preserving each rune's
// style.
//
// IT ALLOCATES. Lines and every Line are new slices. Build it in the widget's
// constructor for static text, or in its size-change check for reflowing text,
// cache it against the rect it was computed for, and have Draw only index it.
// Calling Wrap inside Draw allocates every frame and is the failure mode this
// doc comment exists to prevent (ADR 0008 §4).
//
// The rules, all of which are the contract:
//   - The ONLY break opportunity in v1 is U+0020 SPACE. No hyphen breaks, no
//     CJK boundary breaks, no slash breaks.
//   - A span boundary is NOT a break opportunity. Break opportunities are
//     computed on the concatenated text, so a word may straddle two spans ("Se"
//     in one style, "lected" in another) and is never split by the wrap.
//   - The space that causes a break is consumed. Every other space is preserved
//     verbatim, so trimming cannot silently change a column alignment.
//   - A word longer than width is cut at the cell boundary with NO marker.
//     Truncate owns the marker; a widget that wants "there is more" calls it on
//     the last line it can actually show.
//   - A double-width glyph is never split. One that does not fit the remaining
//     cells is dropped and the line ends; if that leaves the line empty the line
//     is omitted from Lines entirely.
//   - Zero-width runes (combining marks) are DROPPED, matching SetSpans and
//     RuneWidth's documented lack of grapheme composition. This ADR inherits
//     that limitation rather than half-solving it: a mark is not attached to the
//     preceding span, because each cell is written independently and it would
//     not render anyway.
//   - width <= 0 returns a Wrapped with no Lines and Height() == 0. It never
//     panics.
//   - Runes sharing a style are merged into one span within a line, compared by
//     exact Style equality. Two styles that resolve to the same thing but differ
//     as values (HeadingStyle and Style{Attr: AttrBold | AttrUnderline}) stay
//     separate spans, because merging is a cosmetic detail and resolving here
//     would bake the terminal defaults into a cached value.
func Wrap(spans []Span, width int) Wrapped {
	out := Wrapped{Width: width}
	if width <= 0 {
		return out
	}

	flat := flatten(spans)
	if len(flat) == 0 {
		return out
	}

	i := 0
	for i < len(flat) {
		lineStart, lineW, lastSpace := i, 0, -1
		for i < len(flat) {
			w := RuneWidth(flat[i].r)
			if lineW+w > width {
				break
			}
			lineW += w
			if flat[i].r == ' ' {
				lastSpace = i
			}
			i++
		}

		switch {
		case i < len(flat) && flat[i].r == ' ':
			// The rune that does not fit is itself a space. That space caused the
			// break, so it is consumed: emit the line as it stands and resume
			// after the space. This case has to come before the lastSpace case,
			// because a space that ran out of room was never recorded as a break
			// opportunity and would otherwise lead the next line.
			out.Lines = append(out.Lines, buildLine(flat[lineStart:i]))
			i++

		case i < len(flat) && lastSpace > lineStart:
			// The line is full and it contains a break opportunity: emit
			// everything before the space and consume it. lastSpace > lineStart
			// (not >=) keeps a line that merely STARTS with a space from becoming
			// an empty line; that space is preserved verbatim instead.
			//
			// The i < len(flat) guard is load-bearing: when the text ran out
			// rather than the width running out, the last space in the line is
			// just text, and breaking there would silently drop it.
			out.Lines = append(out.Lines, buildLine(flat[lineStart:lastSpace]))
			i = lastSpace + 1

		case i > lineStart:
			// The text ended. Everything from lineStart to i fits.
			out.Lines = append(out.Lines, buildLine(flat[lineStart:i]))

		case i < len(flat):
			// Nothing on this line fits at all: the rune at i is wider than the
			// whole width (only possible for a double-width glyph in a one-column
			// area). Drop it and omit the line, per the contract above.
			i++
		}
	}
	return out
}

// flatRune is one rune of the concatenated text with the style of the span it
// came from. Flattening is what makes "a span boundary is not a word boundary"
// implementable: wrap sees one string, and each rune remembers its style.
type flatRune struct {
	r  rune
	st Style
}

// flatten concatenates spans into one rune sequence, dropping zero-width runes.
// It is the first of Wrap's two allocations.
func flatten(spans []Span) []flatRune {
	n := 0
	for i := range spans {
		n += len(spans[i].Text)
	}
	flat := make([]flatRune, 0, n)
	for i := range spans {
		for _, r := range spans[i].Text {
			if RuneWidth(r) == 0 {
				continue
			}
			flat = append(flat, flatRune{r: r, st: spans[i].Style})
		}
	}
	return flat
}

// buildLine turns a run of flattened runes into a line of spans, merging
// adjacent runes that share a style so a wrapped word is not one span per rune.
func buildLine(flat []flatRune) []Span {
	line := make([]Span, 0, 4)
	start := 0
	for i := 1; i <= len(flat); i++ {
		if i < len(flat) && flat[i].st == flat[i-1].st {
			continue
		}
		text := make([]byte, 0, (i-start)*utf8.UTFMax)
		for _, fr := range flat[start:i] {
			text = utf8.AppendRune(text, fr.r)
		}
		line = append(line, Span{Text: string(text), Style: flat[start].st})
		start = i
	}
	return line
}
