// Package ansi encodes the fixed subset of ANSI/VT sequences TermMosaic
// emits.
//
// ADR 0001 is explicit that TermMosaic has no terminfo: it emits a fixed,
// well-chosen subset of SGR, CUP, ED and EL plus capability sniffing. That is
// correct for the overwhelming majority of terminals and wrong for a few
// exotic ones, and the ADR records that as an accepted limitation rather than a
// gap to close.
//
// Every encoder here appends to a caller-supplied byte slice and never
// allocates once that slice has grown, which is what lets the diff run at
// 0 allocs/op.
package ansi

import (
	"strconv"

	"github.com/serkanalgur/termmosaic/buffer"
)

// Control sequences, grouped so a reader can see the whole emitted surface in
// one place.
const (
	// CSI introduces a control sequence.
	CSI = "\x1b["

	// CursorHome moves the cursor to (1,1).
	CursorHome = CSI + "H"
	// HideCursor stops the cursor being drawn.
	HideCursor = CSI + "?25l"
	// ShowCursor resumes drawing the cursor.
	ShowCursor = CSI + "?25h"

	// SaveCursor stores the cursor position.
	SaveCursor = CSI + "s"
	// RestoreCursor restores the stored cursor position.
	RestoreCursor = CSI + "u"

	// EnterAltScreen switches to the alternate screen buffer.
	EnterAltScreen = CSI + "?1049h"
	// LeaveAltScreen switches back to the main screen buffer.
	LeaveAltScreen = CSI + "?1049l"

	// EraseInLine clears from the cursor to the end of the line.
	EraseInLine = CSI + "K"
	// EraseInLineToStart clears from the start of the line to the cursor.
	EraseInLineToStart = CSI + "1K"
	// EraseDisplay clears the whole screen.
	EraseDisplay = CSI + "2J"

	// Reset returns all rendition to default. Emitted once per styling
	// change, not once per frame.
	Reset = CSI + "0m"
)

// SGR codes. The attribute codes mirror buffer.Attr's bits.
const (
	sgrReset     = 0
	sgrBold      = 1
	sgrFaint     = 2
	sgrItalic    = 3
	sgrUnderline = 4
	sgrReverse   = 7
	sgrStrike    = 9

	sgrDefaultFG = 39
	sgrDefaultBG = 49

	// sgrFGColor and sgrBGColor introduce a colour, followed by either
	// sgrIndexed;<index> or sgrRGB;<r>;<g>;<b>.
	sgrFGColor  = 38
	sgrBGColor  = 48
	sgrIndexed  = 5
	sgrRGBValue = 2
)

// maxParams is the widest SGR sequence emitted: attribute reset plus six
// attributes, a truecolor foreground (6 params) and a truecolor background
// (6 params). Sized as a stack array so encoding does not allocate.
const maxParams = 16

// AppendCursorPosition appends a move to the 1-based cell (row, col).
func AppendCursorPosition(dst []byte, row, col int) []byte {
	dst = append(dst, CSI...)
	dst = strconv.AppendInt(dst, int64(row), 10)
	dst = append(dst, ';')
	dst = strconv.AppendInt(dst, int64(col), 10)
	return append(dst, 'H')
}

// AppendString appends a constant sequence such as HideCursor.
func AppendString(dst []byte, s string) []byte { return append(dst, s...) }

// AppendRune appends a rune as UTF-8.
//
// This is utf8.AppendRune spelled out. Invalid runes become U+FFFD rather than
// being dropped: a buffer holding a bad rune must still produce a frame, and a
// silent drop would misalign every cell after it on the line.
func AppendRune(dst []byte, r rune) []byte {
	switch {
	case r < 0 || r > 0x10FFFF || (r >= 0xD800 && r <= 0xDFFF):
		return append(dst, "�"...)
	case r < 0x80:
		return append(dst, byte(r))
	case r < 0x800:
		return append(dst, byte(0xC0|r>>6), byte(0x80|r&0x3F))
	case r < 0x10000:
		return append(dst, byte(0xE0|r>>12), byte(0x80|r>>6&0x3F), byte(0x80|r&0x3F))
	default:
		return append(dst,
			byte(0xF0|r>>18),
			byte(0x80|r>>12&0x3F),
			byte(0x80|r>>6&0x3F),
			byte(0x80|r&0x3F))
	}
}

// Depth is buffer.ColourDepth. It is aliased rather than redefined so the
// public Caps type can name the ladder without depending on an internal
// package.
type Depth = buffer.ColourDepth

// Colour encoding depths, re-exported so encoder code reads without a prefix.
const (
	// DepthTrueColor emits SGR 38;2 and 48;2 with 8-bit channels.
	DepthTrueColor = buffer.DepthTrueColor
	// Depth256 emits SGR 38;5 and 48;5 with an xterm-256 palette index.
	Depth256 = buffer.Depth256
	// Depth16 emits SGR 30-37/90-97 and 40-47/100-107.
	Depth16 = buffer.Depth16
	// DepthNone emits no colour at all, for NO_COLOR and dumb terminals.
	DepthNone = buffer.DepthNone
)

// Style is buffer.Style — an ALIAS, not a second type.
//
// It was its own {FG, BG, Attr} struct here, which is precisely the collision
// ADR 0008 exists to prevent: one type in a public package, one in an internal
// one, same name and same fields and no assignability between them — and the
// internal one is what the frame actually compares. A style that is
// ==-comparable but not assignable to what the diff tracks is a comparison that
// silently never comes true.
//
// Aliasing rather than re-declaring means the diff's tracked style and the
// widget-facing style are literally one type, with no conversion anywhere. It is
// an alias and not a defined type on purpose: a defined type would need
// conversions at every boundary and would reintroduce the split.
//
// No import cycle: this package already imported buffer for Depth.
type Style = buffer.Style

// Encoder appends SGR sequences for a style at a given colour depth. It is a
// value type and holds no mutable state: the caller owns current-style
// tracking, which lives in the diff because that is where the decision to emit
// lives too.
type Encoder struct {
	// Depth selects the colour encoding. The zero value is truecolor.
	Depth Depth
	// NoColor suppresses every colour SGR, honouring the NO_COLOR convention.
	// Attributes are still emitted. NO_COLOR is a user-environment
	// convention, not a terminal capability, so it is a separate switch rather
	// than a depth.
	NoColor bool
	// Quantiser maps colours onto the 256 and 16 palettes. A nil value means
	// the default Lab-space quantiser. This is the hook the colour decision
	// in STATUS.md refers to: a different metric (OKLab, CIEDE2000) needs no
	// change to the diff.
	Quantiser buffer.Quantiser
}

func (e Encoder) quantiser() buffer.Quantiser {
	if e.Quantiser == nil {
		return buffer.DefaultQuantiser{}
	}
	return e.Quantiser
}

// ColoursEnabled reports whether the encoder emits colour at all.
func (e Encoder) ColoursEnabled() bool { return !e.NoColor && e.Depth != DepthNone }

// AppendSGR appends the complete SGR sequence for s.
//
// It emits absolute state, never a delta. The diff decides when something
// changed; making the sequence a delta would make the output depend on the
// terminal's prior state, which the MemorySink's screen model could not
// reproduce and a real terminal could mis-apply after a resize.
func (e Encoder) AppendSGR(dst []byte, s Style) []byte {
	var p [maxParams]int
	n := e.params(p[:0], s)
	if len(n) == 0 {
		return append(dst, Reset...)
	}
	dst = append(dst, CSI...)
	for i, code := range n {
		if i > 0 {
			dst = append(dst, ';')
		}
		dst = strconv.AppendInt(dst, int64(code), 10)
	}
	return append(dst, 'm')
}

// params builds the full SGR parameter list for a style: attribute reset and
// attribute codes first, then foreground, then background.
func (e Encoder) params(dst []int, s Style) []int {
	dst = appendAttrParams(dst, s.Attr)
	if !e.ColoursEnabled() {
		return dst
	}
	dst = e.appendFGParams(dst, s.FG)
	dst = e.appendBGParams(dst, s.BG)
	return dst
}

// appendAttrParams appends the attribute codes. A non-zero attribute list
// always begins with a reset, because terminals do not reliably clear attributes
// when individual codes are turned back off (there is no "SGR 22 off" for bold
// that is honoured everywhere, and turning bold off with 22 also clears faint).
func appendAttrParams(dst []int, a buffer.Attr) []int {
	if a == 0 {
		return dst
	}
	dst = append(dst, sgrReset)
	if a.Has(buffer.AttrBold) {
		dst = append(dst, sgrBold)
	}
	if a.Has(buffer.AttrFaint) {
		dst = append(dst, sgrFaint)
	}
	if a.Has(buffer.AttrItalic) {
		dst = append(dst, sgrItalic)
	}
	if a.Has(buffer.AttrUnderline) {
		dst = append(dst, sgrUnderline)
	}
	if a.Has(buffer.AttrReverse) {
		dst = append(dst, sgrReverse)
	}
	if a.Has(buffer.AttrStrike) {
		dst = append(dst, sgrStrike)
	}
	return dst
}

func (e Encoder) appendFGParams(dst []int, c buffer.Colour) []int {
	switch {
	case c.IsDefault(), c.IsUnset():
		// IsUnset is a backstop, not the normal path: Style.Resolved maps the
		// inherit marker to DefaultColour, so a resolved style never carries it.
		// Without the case, a style that skipped Resolved would quantise the
		// marker (0xFFFFFFFE) as if it were a near-white RGB value and produce a
		// confusing frame rather than a visible no-op.
		return append(dst, sgrDefaultFG)
	case e.Depth == Depth16:
		i := int(e.quantiser().Nearest16(c))
		if i < 8 {
			return append(dst, 30+i)
		}
		return append(dst, 90+i-8)
	case e.Depth == Depth256:
		return append(dst, sgrFGColor, sgrIndexed, int(e.quantiser().Nearest256(c)))
	default:
		r, g, b := c.RGB()
		return append(dst, sgrFGColor, sgrRGBValue, int(r), int(g), int(b))
	}
}

func (e Encoder) appendBGParams(dst []int, c buffer.Colour) []int {
	switch {
	case c.IsDefault(), c.IsUnset():
		// See appendFGParams: backstop for an unresolved style.
		return append(dst, sgrDefaultBG)
	case e.Depth == Depth16:
		i := int(e.quantiser().Nearest16(c))
		if i < 8 {
			return append(dst, 40+i)
		}
		return append(dst, 100+i-8)
	case e.Depth == Depth256:
		return append(dst, sgrBGColor, sgrIndexed, int(e.quantiser().Nearest256(c)))
	default:
		r, g, b := c.RGB()
		return append(dst, sgrBGColor, sgrRGBValue, int(r), int(g), int(b))
	}
}

// StyleString returns the complete SGR sequence for s as a string. For tests
// and diagnostics only; the renderer always appends to a scratch buffer.
func (e Encoder) StyleString(s Style) string { return string(e.AppendSGR(nil, s)) }

// AppendSGRDelta appends the shortest SGR sequence that moves the terminal
// from prev to next, assuming the terminal is genuinely in state prev.
//
// It emits only the components that actually changed — foreground, background,
// attributes — because that is the whole point of tracking current style state
// in the diff. An absolute restatement of an unchanged background is 16 wasted
// bytes on every styling change, which is most of the difference between a
// 107-byte frame and a 169-byte one.
//
// An attribute change always falls back to the absolute form, because the only
// portable way to clear an attribute is SGR 0, which also clears both colours
// and so requires them restating anyway.
//
// The caller MUST only use this when it knows the terminal's current style.
// When that is unknown — the first cell of a frame, or after a forced full
// repaint — use AppendSGR.
func (e Encoder) AppendSGRDelta(dst []byte, prev, next Style) []byte {
	if prev == next {
		return dst
	}
	if prev.Attr != next.Attr {
		return e.AppendSGR(dst, next)
	}
	if !e.ColoursEnabled() {
		return dst
	}
	var p [maxParams]int
	var n []int
	if prev.FG != next.FG {
		n = e.appendFGParams(p[:0], next.FG)
	}
	if prev.BG != next.BG {
		// Reuses p's storage: the foreground group already written into n is
		// still there, so this appends the background group after it.
		n = e.appendBGParams(n, next.BG)
	}
	if len(n) == 0 {
		return dst
	}
	dst = append(dst, CSI...)
	for i, code := range n {
		if i > 0 {
			dst = append(dst, ';')
		}
		dst = strconv.AppendInt(dst, int64(code), 10)
	}
	return append(dst, 'm')
}
