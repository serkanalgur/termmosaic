package main

// The palette, the styles, and the results table's schema.
//
// # Colour is an application decision
//
// ADR 0008 ships no default colours, so every colour here is this example's. The
// set is deliberately small — a background, a foreground, two greys and one accent
// — because what remains readable in monochrome is the real test of a palette: a
// screen that needs eleven shades to be legible is a screen that is illegible on
// a terminal with eight.
//
// # What the accent is for, and what it is not for
//
// The accent marks exactly two things: the matched terms inside a snippet, and
// the focus marker in the query row's gutter. Both carry a second, non-colour
// signal — the matched terms are underlined as well as coloured, and the marker
// is a glyph in its own column — so a reader on a monochrome terminal, or with
// NO_COLOR set, loses emphasis and nothing else.
//
// Nothing else on this screen is coloured, and in particular NO state is signalled
// by hue. "searching…", "no results" and "search failed: …" are three different
// WORDS on the same status line in the same style, which is what makes the
// degradation total rather than partial.
//
// Each style is built through buffer.NewStyle rather than a composite literal: a
// partial literal would leave a channel at opaque black, which is Style's
// documented footgun.

import (
	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/geometry"
	"github.com/serkanalgur/termmosaic/widgets/block"
	"github.com/serkanalgur/termmosaic/widgets/data"
)

// alignRight is where a number goes in its column, named so that a reader of the
// schema below does not have to know that right-alignment is a geometry type.
const alignRight = geometry.AlignRight

// The palette.
var (
	bg      = buffer.NewColour(0x0e, 0x11, 0x16)
	panelBG = buffer.NewColour(0x14, 0x19, 0x21)
	fg      = buffer.NewColour(0xd8, 0xdc, 0xe4)
	dim     = buffer.NewColour(0x6a, 0x74, 0x82)
	edge    = buffer.NewColour(0x2c, 0x33, 0x3d)
	accent  = buffer.NewColour(0x7a, 0xc2, 0xe8)
	warn    = buffer.NewColour(0xd8, 0xa8, 0x40)
)

// The styles the screen draws with.
var (
	stBody     = buffer.NewStyle(fg, bg, 0)
	stPanel    = buffer.NewStyle(fg, panelBG, 0)
	stMuted    = buffer.NewStyle(dim, bg, 0)
	stField    = buffer.NewStyle(fg, panelBG, 0)
	stTitle    = buffer.NewStyle(dim, panelBG, buffer.AttrBold)
	stEdge     = buffer.NewStyle(edge, panelBG, 0)
	stAccent   = buffer.NewStyle(accent, panelBG, 0)
	stHead     = buffer.NewStyle(dim, panelBG, 0)
	stSelected = buffer.NewStyle(fg, panelBG, buffer.AttrReverse)

	// stWarn is the status line's style when it carries a failure. It sits on the
	// screen background rather than a panel's, because the status line is not
	// inside a panel.
	//
	// The failure is ALSO a word — "search failed" — so this is emphasis on a
	// sentence that already says what happened, never the sentence itself.
	stWarn = buffer.NewStyle(warn, bg, 0)

	// stMatch is a matched term inside a snippet. It is UNDERLINED as well as
	// coloured, and that is the whole accessibility argument: an attribute survives
	// NO_COLOR and a monochrome terminal, where a hue says nothing at all.
	stMatch = buffer.NewStyle(accent, bg, buffer.AttrUnderline)

	// stSnipHead is the detail pane's leading line — the article's description,
	// or the sentence that says what the body is. Bold rather than coloured,
	// because a heading's job is to be heavier than what is under it.
	stSnipHead = buffer.NewStyle(fg, bg, buffer.AttrBold)
)

// framePanel configures a widget's block with this application's chrome.
//
// It is the only place in this example that touches a border, and it does so
// through widgets/block rather than by drawing one. Every panel therefore has the
// same border, the same background and the same title placement, which is what
// makes a screen assembled from four different widgets read as one screen.
func framePanel(b *block.Block, title string) {
	b.SetBorder(buffer.BorderPlain)
	b.SetBorderStyle(stEdge)
	b.SetBackground(stPanel)
	b.SetTitleString(title, stTitle)
}

// resultColumns is the results table's schema: title, word count, last revision.
//
// A Table rather than a List, and the choice is worth stating. A List renders one
// string per item, so a result row would have to be a single formatted line —
// "Text-based user interface   1929w   2026-09-18" — with the spacing built into
// the string by hand. That gives up three things a table gives for free:
//
//   - COMPARABILITY. The word-count column is right-aligned and fixed-width, so
//     "5038" lines up with "87" and a reader scanning for the longest article
//     finds it by shape. Padded into a string, the same column is only comparable
//     because the formatter did the arithmetic.
//   - WIDTHS THAT DO NOT MOVE. The word counts span two orders of magnitude in
//     the bundled capture (87 and 5038). A content-measured column would be as
//     wide as 5038 on every row, which is what the markets example had to solve
//     with a fixed Width for exactly the same reason.
//   - A HEADER ROW, so the columns are named once at the top rather than guessed
//     at from the shape of the data.
//
// The title column GROWS and the other two are fixed, so the slack goes to the
// only column whose content genuinely varies: article titles run from
// "Alsamixer" to "Text-based user interface" to anything the reader searches for.
//
// None of the columns is wider than it needs to be, and the title column is the
// only one that can truncate — which is why the table scrolls horizontally rather
// than the title column silently becoming a column of ellipses.
func resultColumns() []data.Column {
	return []data.Column{
		{Title: []buffer.Span{buffer.NewSpan("article", stHead)}, Grow: 1, HeaderStyle: stHead},
		{Title: []buffer.Span{buffer.NewSpan("words", stHead)}, Width: 7, Align: alignRight, HeaderStyle: stHead},
		{Title: []buffer.Span{buffer.NewSpan("updated", stHead)}, Width: 12, HeaderStyle: stHead},
	}
}
