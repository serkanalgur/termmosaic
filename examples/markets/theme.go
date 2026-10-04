package main

// The palette and the shared chrome helpers.
//
// # Colour is an application decision
//
// ADR 0008 ships no default colours, so every colour here is this example's. The
// set is deliberately small — a background, a foreground, three greys and three
// direction colours — because what remains readable in monochrome is the real
// test of a palette: a dashboard that needs eleven shades to be legible is a
// dashboard that is illegible on a terminal with eight.
//
// # The direction colours are a pair, not a scale
//
// stUp and stDown are the only two colours on the screen that mean anything, and
// they are paired with stFlat for everything that does not move. Green and red
// alone would be the failure mode; here they are emphasis layered on a sign and an
// arrow, so removing them costs emphasis and nothing else.
//
// Each style is built through buffer.NewStyle rather than a composite literal: a
// partial literal would leave a channel at opaque black, which is Style's
// documented footgun, and a dashboard with a black stripe across it is a
// dashboard nobody trusts.

import (
	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/geometry"
	"github.com/serkanalgur/termmosaic/widgets/block"
	"github.com/serkanalgur/termmosaic/widgets/data"
)

// alignRight is where a number goes in its column, named so that a reader of the
// schema above does not have to know that right-alignment is a geometry type.
const alignRight = geometry.AlignRight

// The palette.
var (
	bg      = buffer.NewColour(0x0e, 0x11, 0x16)
	panelBG = buffer.NewColour(0x14, 0x19, 0x21)
	fg      = buffer.NewColour(0xd8, 0xdc, 0xe4)
	dim     = buffer.NewColour(0x66, 0x70, 0x7e)
	edge    = buffer.NewColour(0x2c, 0x33, 0x3d)
	up      = buffer.NewColour(0x3c, 0xc8, 0x8c)
	down    = buffer.NewColour(0xd8, 0x6a, 0x62)
	flat    = buffer.NewColour(0x8a, 0x93, 0xa0)
	accent  = buffer.NewColour(0x54, 0xa8, 0xf0)
	warn    = buffer.NewColour(0xd8, 0xa8, 0x40)
)

// The styles the screen draws with.
var (
	stBody     = buffer.NewStyle(fg, bg, 0)
	stPanel    = buffer.NewStyle(fg, panelBG, 0)
	stMuted    = buffer.NewStyle(dim, bg, 0)
	stFigure   = buffer.NewStyle(fg, panelBG, buffer.AttrBold)
	stTitle    = buffer.NewStyle(warn, panelBG, buffer.AttrBold)
	stEdge     = buffer.NewStyle(edge, panelBG, 0)
	stAccent   = buffer.NewStyle(accent, panelBG, 0)
	stTrack    = buffer.NewStyle(edge, panelBG, 0)
	stSelected = buffer.NewStyle(fg, panelBG, buffer.AttrReverse)

	// stUp, stDown and stFlat are the direction palette. See the file comment:
	// they are emphasis, never the signal.
	stUp   = buffer.NewStyle(up, panelBG, 0)
	stDown = buffer.NewStyle(down, panelBG, 0)
	stFlat = buffer.NewStyle(flat, panelBG, 0)

	// stFlatFill is the meter's "flat" band fill, which has to differ from stFlat
	// because it paints a REGION rather than text: on a track, a colour with no
	// contrast against its neighbours reads as no band at all.
	stFlatFill = buffer.NewStyle(edge, panelBG, buffer.AttrReverse)

	// stThreshold is the meter's zero line. It is bold rather than coloured,
	// because a threshold is a POSITION and a position is better marked by weight
	// than by hue.
	stThreshold = buffer.NewStyle(fg, panelBG, buffer.AttrBold)

	// stWarn is the footer's style when it carries a failure. It sits on the screen
	// background rather than a panel's, because the footer is not inside a panel.
	stWarn = buffer.NewStyle(warn, bg, 0)

	// The sparkline's three emphases. Min and Max are attributes rather than
	// colours: the series' shape is already geometry, so these only need to say
	// "this one is an extreme", and a reverse video cell says it on a monochrome
	// terminal where a hue would say nothing.
	stSpark     = buffer.NewStyle(accent, panelBG, 0)
	stSparkLow  = buffer.NewStyle(accent, panelBG, buffer.AttrFaint)
	stSparkHigh = buffer.NewStyle(accent, panelBG, buffer.AttrBold)

	// stSymbol is a table cell's instrument, which is muted relative to its numbers
	// so the eye goes to the numbers first.
	stSymbol = buffer.NewStyle(dim, panelBG, 0)
)

// framePanel configures a widget's block with this application's chrome.
//
// It is the only place in this example that touches a border, and it does so
// through widgets/block rather than by drawing one. Every panel therefore has the
// same border, the same background and the same title placement, which is what
// makes a screen assembled from nine different widgets read as one screen.
func framePanel(b *block.Block, title string) {
	b.SetBorder(buffer.BorderPlain)
	b.SetBorderStyle(stEdge)
	b.SetBackground(stPanel)
	b.SetTitleString(title, stTitle)
}

// tableColumns is the detail table's schema.
//
// The rate column is a FIXED width rather than content-measured because the rates
// span three orders of magnitude — 0.75 for GBP and 1348 for KRW — and a
// content-measured column would be as wide as the widest rate on every row. The
// 1d column is fixed for the same reason: a right-aligned number that moves as
// the numbers change is a column nobody can compare down.
//
// None of the columns Grow: the table's width comes from its pane, and a column
// that absorbed the slack would leave the others ragged at a wide terminal.
func tableColumns() []data.Column {
	return []data.Column{
		{Title: []buffer.Span{buffer.NewSpan("pair", stMuted)}, Width: 9, HeaderStyle: stMuted},
		{Title: []buffer.Span{buffer.NewSpan("rate", stMuted)}, Width: 11, Align: alignRight, HeaderStyle: stMuted},
		// The change column is 10 rather than 9 because its widest content is the
		// arrow and a signed percentage with a space between them, and a column that
		// truncates the direction glyph is the one failure this screen must not have.
		{Title: []buffer.Span{buffer.NewSpan("1d", stMuted)}, Width: 10, Align: alignRight, HeaderStyle: stMuted},
	}
}
