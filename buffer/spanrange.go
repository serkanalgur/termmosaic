package buffer

// This file owns the range-clipped span writers: SetSpansIn, SetSpansCappedIn,
// SetSpansWindowIn and SetStringIn.
//
// They exist because SetSpans and SetString clip to the BUFFER's right edge, and
// a widget's row almost never reaches the screen's edge: a table cell ends at its
// column, a gauge label ends at its pane, a pager's body ends at its rect. Before
// these, a widget with a column had exactly three bad options — pre-truncate with
// Truncate, which allocates on the frame path; draw past its own cell into the
// gutter and the border beside it; or re-implement the wide-glyph rules, which is
// how widgets/data and widgets/viz each grew their own copy of the same forty
// lines. Two copies of the continuation-cell rule is two chances for one of them
// to put the next span's style on the right half of a glyph, and that mismatch
// never compares equal to the previous frame: the row flickers forever (see
// SetSpans).
//
// All four are 0 allocations: they take a slice or a string and never build one,
// which is what makes them callable from Draw. The allocating counterparts remain
// Truncate and Wrap.

// SetSpansIn writes spans left to right on row y into the cells [x0, x1), and
// returns the column just past the last cell it wrote.
//
// It is SetSpans with an explicit right edge, for a widget whose text ends before
// the buffer's does. x1 is exclusive: the cell at x1 is never written, so a range
// of width n occupies exactly n cells.
//
// The rules are SetSpans', and they are the reason this is one function rather
// than a per-widget one:
//   - A span's Style is resolved through Resolved exactly once, on entry.
//   - A double-width rune writes a glyph cell and a continuation cell, and BOTH
//     take THIS SPAN'S STYLE.
//   - A rune that does not fit before x1 ends the run. A wide glyph is never
//     half-written, so clipping at the right edge can never leave a continuation
//     cell without the glyph it belongs to. The returned column is then the one
//     the text stopped at, which is how a caller tells truncation from fitting.
//   - Zero-width runes are dropped, as everywhere else.
//   - x1 <= x0 writes nothing and returns x0.
//   - An out-of-range y is not an error: every cell write is ignored by SetCell,
//     so nothing is drawn and the returned column is the one the content would
//     have reached. This matches the behaviour the widget-local copies had.
//
// 0 allocations.
func (b *Buffer) SetSpansIn(x0, x1, y int, spans []Span) int {
	if x1 <= x0 {
		return x0
	}
	x := x0
	for i := range spans {
		if x >= x1 {
			return x
		}
		st := spans[i].Style.Resolved()
		for _, r := range spans[i].Text {
			w := RuneWidth(r)
			switch {
			case w == 0:
				// Zero-width runes occupy no cell and are dropped, matching
				// SetSpans, SetString and Wrap.
			case x+w > x1:
				// Half a glyph is worse than none, so the whole run ends here.
				return x
			case w == 2:
				b.SetCell(x, y, st.Cell(r))
				b.SetCell(x+1, y, ContinuationCell(st))
			default:
				b.SetCell(x, y, st.Cell(r))
			}
			x += w
		}
	}
	return x
}

// SetStringIn is SetSpansIn of one span: it writes s into the cells [x0, x1) on
// row y and returns the column just past the last cell written.
//
// It exists because uniform text is the common case and one span must not cost a
// slice on the frame path — a widget drawing a run of digits, or a pager drawing
// one segment of a highlighted line, has a string and no slice.
//
// 0 allocations.
func (b *Buffer) SetStringIn(x0, x1, y int, s string, st Style) int {
	if x1 <= x0 {
		return x0
	}
	c := st.Resolved()
	x := x0
	for _, r := range s {
		w := RuneWidth(r)
		switch {
		case w == 0:
		case x+w > x1:
			return x
		case w == 2:
			b.SetCell(x, y, c.Cell(r))
			b.SetCell(x+1, y, ContinuationCell(c))
		default:
			b.SetCell(x, y, c.Cell(r))
		}
		x += w
	}
	return x
}

// SetSpansCappedIn writes spans into the cells [x0, x1) and, when the content is
// wider than the range, ends with the one-cell truncation marker in the final
// cell. It returns the column just past what it wrote.
//
// It is the allocation-free counterpart of Truncate for the frame path, and it
// exists because Truncate allocates whenever it actually truncates — which for a
// visible row is every frame. The rules match Truncate's, with one documented
// difference forced by not building the truncated slice:
//
//   - Content that already fits is drawn whole, with no marker.
//   - Content that does not fit gives up its final cell to the marker, so the
//     result is never wider than the range.
//   - A range one cell wide carries the marker alone, since there is nowhere to
//     put even one character of content beside it.
//   - The marker takes the style of the LAST NON-EMPTY INPUT span, where
//     Truncate takes the last span it KEPT. Truncate can know the difference
//     because it has the kept slice in hand; this cannot without allocating one,
//     and the difference is only observable for a content whose last span is
//     entirely clipped away. It is a real difference and is named here rather
//     than papered over.
//   - A double-width glyph is never split, and the marker can never land on a
//     continuation cell: content is written into [x0, x1-1) first, so the widest
//     glyph that fits ends at x1-2.
//
// 0 allocations.
func (b *Buffer) SetSpansCappedIn(x0, x1, y int, spans []Span, mark rune) int {
	if x1 <= x0 {
		return x0
	}
	if SpansWidth(spans) <= x1-x0 {
		return b.SetSpansIn(x0, x1, y, spans)
	}
	st := markerStyle(spans).Resolved()
	if x1-x0 == 1 {
		b.SetCell(x0, y, st.Cell(mark))
		return x1
	}
	b.SetSpansIn(x0, x1-1, y, spans)
	b.SetCell(x1-1, y, st.Cell(mark))
	return x1
}

// SetSpansWindowIn writes spans into the cells [x0, x1) on row y starting skip
// CELLS into the content, and ends with the truncation marker when the rest does
// not fit. It returns x1 when the marker was written and the column it stopped
// at otherwise.
//
// It is SetSpansCappedIn for a horizontally scrolled region: a column whose first
// cells are off screen to the left has to show its TAIL, and neither SetSpansIn
// nor SetSpansCappedIn can express a window that does not start at the content's
// beginning. Without it, a scrolled table either repeats the column's head or
// paints it over the gutter and the border to its left.
//
// The rules, on top of SetSpansCappedIn's:
//   - skip counts CELLS, not spans and not runes, so a caller's column width
//     arithmetic is the same arithmetic it already does. A negative skip is
//     treated as zero.
//   - The marker replaces the LAST cell of the window even when the window filled
//     exactly: "state0" in five cells is "stat…", because a reader has to be able
//     to see that the cell is cut.
//   - A double-width glyph straddling the window's left edge is dropped rather
//     than half taken, the same rule SetSpansIn applies at the right edge.
//   - A window ONE cell wide shows the content and NO marker: one cell cannot
//     hold both, and showing the cell a one-column table scrolled onto is more
//     useful than showing "~". This is the one case where the window and the
//     capped rule differ, and it is pinned by a test.
//
// 0 allocations.
func (b *Buffer) SetSpansWindowIn(x0, x1, y int, spans []Span, skip int, mark rune) int {
	if x1 <= x0 {
		return x0
	}
	if skip < 0 {
		skip = 0
	}
	seen, x := 0, x0
	stopped := false
	for i := range spans {
		if stopped {
			break
		}
		st := spans[i].Style.Resolved()
		for _, r := range spans[i].Text {
			w := RuneWidth(r)
			if w == 0 {
				continue
			}
			if seen+w <= skip {
				seen += w
				continue
			}
			if seen < skip {
				// A wide rune straddling the window's left edge is dropped rather
				// than half taken, exactly as SetSpansIn drops one that does not
				// fit at the right edge.
				seen += w
				stopped = true
				break
			}
			if x+w > x1 {
				stopped = true
				break
			}
			if w == 2 {
				b.SetCell(x, y, st.Cell(r))
				b.SetCell(x+1, y, ContinuationCell(st))
			} else {
				b.SetCell(x, y, st.Cell(r))
			}
			seen += w
			x += w
		}
	}
	if stopped && x1-x0 > 1 {
		// The marker replaces the LAST cell of the window even when the window
		// filled exactly, so a reader can see the cell is cut.
		b.writeMarkerAt(x1-1, y, mark, markerStyle(spans))
		x = x1
	}
	return x
}

// writeMarkerAt writes the truncation marker at cell x on row y, first clearing
// the glyph half of a wide rune that would otherwise be left stranded by the
// mark landing on its continuation cell.
//
// The window writer can fill the range exactly, so unlike SetSpansCappedIn it can
// reach a state where x is a continuation cell: content "漢a漢b" in five cells
// writes the glyph at 3-4 and stops. Overwriting only the continuation would
// leave a glyph cell whose other half says something else, which renders as
// whatever the terminal does with an unpaired wide cell and never compares equal
// to the previous frame. Blanking the glyph half leaves an ordinary blank, which
// is invisible against a row the caller has already painted.
func (b *Buffer) writeMarkerAt(x, y int, mark rune, st Style) {
	if x > 0 && b.CellAt(x, y).IsContinuation() {
		b.SetCell(x-1, y, st.Blank())
	}
	b.SetCell(x, y, st.Cell(mark))
}

// markerStyle returns the style a truncation marker wears: that of the last
// non-empty input span, since the frame-path writers never build the truncated
// slice whose last KEPT span Truncate uses.
func markerStyle(spans []Span) Style {
	for i := len(spans) - 1; i >= 0; i-- {
		if spans[i].Text != "" {
			return spans[i].Style
		}
	}
	return DefaultStyle
}
