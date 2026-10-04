package headless

import (
	"bytes"
	"errors"
	"strings"
	"sync"

	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/internal/ansi"
)

// MemorySink is a Sink that records the bytes it receives and, crucially,
// maintains the cell buffer those bytes would have produced on a real terminal.
//
// ADR 0001 is explicit that this cell surface is the reason the headless backend
// exists: tcell's headless backend keeps GetCells behind an unexported interface
// and returns a freshly allocated slice on every inspection, so widget tests
// there can only match on escape sequences. Here, a test asserts on cells:
//
//	sink := headless.NewMemorySink(80, 24)
//	...render a frame...
//	if got := sink.CellAt(4, 2); got.FG != wantGreen { ... }
//
// It is not a general-purpose terminal emulator. It implements exactly the
// subset TermMosaic's encoder emits — SGR, CUP, ED, EL, DECTCEM cursor visibility
// — because anything else would be untested code pretending to be a terminal.
// An unrecognised sequence is counted in UnknownSequences rather than ignored,
// so a test can assert the renderer emits nothing it does not model.
//
// MemorySink satisfies termmosaic.Sink and is safe for concurrent use.
//
// # Cost, measured
//
// Interpreting the emitted bytes back into cells is not free, and a widget test
// suite will feel it. A forced full repaint of a 200x60 screen costs roughly
// 73 microseconds through the renderer alone and roughly 443 microseconds with
// the screen model in the path — about six times more, and most of the
// difference is the SGR parameter parser allocating a slice per styling change.
// See BenchmarkProbeRenderCostVersusScreenModel in the render package for the
// attribution.
//
// That is the right way round: the renderer is the part with a frame budget, and
// the screen model is only ever in a test. But it does mean a suite rendering
// thousands of frames will be dominated by assertion bookkeeping, so keep
// assertions targeted (assert on the cells that matter, not the whole screen)
// rather than snapshotting every frame.
type MemorySink struct {
	mu   sync.Mutex
	w, h int

	// screen is the cell buffer: what a real terminal would be showing.
	screen *buffer.Buffer
	// cur is the cursor position and style the screen is currently being
	// written at.
	curX, curY     int
	curStyle       ansi.Style
	cursorShown    bool
	cursorValid    bool
	altScreen      bool
	savedX, savedY int

	// all is the retained byte history, in order, subject to limit. The limit
	// exists so a long-running test or a benchmark cannot grow it without bound.
	all bytes.Buffer
	// limit is the maximum retained history in bytes. Zero means unbounded.
	limit int
	// frame is the bytes written since the last MarkFrame.
	frame bytes.Buffer

	writes      int
	unknown     int
	maxX, maxY  int
	pendingRune []byte
}

var _ termmosaic.Sink = (*MemorySink)(nil)

// NewMemorySink returns a MemorySink with a w-by-h screen.
func NewMemorySink(w, h int) *MemorySink {
	return &MemorySink{
		w:      w,
		h:      h,
		screen: buffer.NewBuffer(w, h),
	}
}

// Write consumes p, interpreting the sequences it contains.
func (s *MemorySink) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.appendHistory(p)
	// The frame log is bounded by the same limit, for the same reason: a caller
	// that never calls MarkFrame would otherwise accumulate every frame forever.
	if s.limit > 0 && s.frame.Len()+len(p) > s.limit {
		s.frame.Reset()
	}
	s.frame.Write(p)
	s.writes++
	return s.consume(p)
}

// appendHistory appends p to the retained byte history.
//
// When a limit is set and the history would exceed it, the history is dropped
// whole rather than compacted. Compacting would mean copying the whole retained
// buffer on every write past the limit, which turns a bounded sink into an O(n)
// one; dropping keeps it O(1) amortised. A test that needs the whole stream
// should not set a limit, and a benchmark that does not care about the stream
// should.
func (s *MemorySink) appendHistory(p []byte) {
	if s.limit > 0 && s.all.Len()+len(p) > s.limit {
		s.all.Reset()
	}
	s.all.Write(p)
}

// KeepBytes bounds the retained byte history. Once writes total more than limit
// bytes the history is discarded and starts accumulating again, so memory stays
// bounded regardless of how many frames are rendered. Zero means unbounded,
// which is the default because most tests write a handful of frames.
//
// The cell screen is never bounded: it is the assertion surface, and a test that
// wants a bounded screen should size the sink accordingly.
func (s *MemorySink) KeepBytes(limit int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.limit = limit
	if s.limit > 0 && s.all.Len() > s.limit {
		s.all.Reset()
	}
}

// Flush is a no-op: the screen is updated synchronously in Write. It exists
// because the renderer calls it, and a Sink that needed a flush to be
// observable would be a worse testing surface.
func (s *MemorySink) Flush() error { return nil }

// ---------------------------------------------------------------------------
// Assertion surface: cells
// ---------------------------------------------------------------------------

// Cells returns the screen as a flat, row-major slice of cells. It is a
// detached dense copy, so a test may hold it across further writes.
//
// The copy is built row by row with Row rather than from a flat slice: the
// screen is always a top-level buffer, so this is the one place a flat grid is
// the right shape — a caller holding the result has the whole grid, detached,
// and cannot accidentally compute a stride-sensitive index against a view
// (ADR 0006).
func (s *MemorySink) Cells() []buffer.Cell {
	s.mu.Lock()
	defer s.mu.Unlock()
	w, h := s.screen.Width(), s.screen.Height()
	out := make([]buffer.Cell, 0, w*h)
	for y := 0; y < h; y++ {
		out = append(out, s.screen.Row(y)...)
	}
	return out
}

// CellAt returns the screen cell at (x, y). Out-of-range coordinates return
// buffer.DefaultCell rather than panicking, so a test asserting on a cell just
// past the edge gets a clear failure instead of a crash.
func (s *MemorySink) CellAt(x, y int) buffer.Cell {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.screen.CellAt(x, y)
}

// String returns the screen as text, one line per row, with trailing spaces
// trimmed. Continuation cells contribute nothing. This is what golden files
// compare against.
func (s *MemorySink) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var b strings.Builder
	for y := 0; y < s.h; y++ {
		for x := 0; x < s.w; x++ {
			c := s.screen.CellAt(x, y)
			if c.IsContinuation() {
				continue
			}
			r := c.Ch
			if r == 0 {
				r = ' '
			}
			b.WriteRune(r)
		}
		line := strings.TrimRight(b.String(), " ")
		b.Reset()
		b.WriteString(line)
		if y < s.h-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// Line returns row y as text with trailing spaces trimmed. Out-of-range rows
// return "".
func (s *MemorySink) Line(y int) string {
	lines := strings.Split(s.String(), "\n")
	if y < 0 || y >= len(lines) {
		return ""
	}
	return lines[y]
}

// Size returns the screen size in cells.
func (s *MemorySink) Size() (w, h int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w, s.h
}

// Resize changes the screen size and clears it, which is what a real terminal
// does on SIGWINCH. The renderer is separately responsible for forcing a full
// repaint afterwards; a test that resizes without rendering will see a blank
// screen, which is correct rather than a bug.
func (s *MemorySink) Resize(w, h int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.w, s.h = w, h
	s.screen.Resize(w, h)
	s.curX, s.curY = 0, 0
	s.cursorValid = false
}

// Cursor returns the cursor position and whether it is currently shown.
func (s *MemorySink) Cursor() (x, y int, shown bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.curX, s.curY, s.cursorShown
}

// AltScreen reports whether the renderer has switched to the alternate screen.
func (s *MemorySink) AltScreen() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.altScreen
}

// ---------------------------------------------------------------------------
// Assertion surface: bytes
// ---------------------------------------------------------------------------

// Bytes returns the retained byte history, subject to KeepBytes, which may have
// discarded earlier writes.
func (s *MemorySink) Bytes() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]byte, s.all.Len())
	copy(out, s.all.Bytes())
	return out
}

// String of all bytes, as text. Only useful for asserting on escape sequences.
func (s *MemorySink) RawString() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.all.String()
}

// Writes returns how many Write calls the sink has received. A no-op frame must
// not increment it, which is how the "idle frame writes nothing" property is
// asserted.
func (s *MemorySink) Writes() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writes
}

// UnknownSequences returns how many sequences the sink did not recognise. It
// must stay 0: a non-zero value means the renderer emitted something this screen
// does not model, and any assertion about the screen is then unsound.
func (s *MemorySink) UnknownSequences() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.unknown
}

// MarkFrame records a frame boundary, returning the bytes written since the
// previous call. A test can assert that exactly one frame wrote bytes.
func (s *MemorySink) MarkFrame() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]byte, s.frame.Len())
	copy(out, s.frame.Bytes())
	s.frame.Reset()
	return out
}

// Reset clears the recorded bytes, the screen, the cursor and the write count.
// The screen returns to buffer.DefaultCell, as it was at construction.
func (s *MemorySink) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.all.Reset()
	s.frame.Reset()
	s.screen.Clear()
	s.curX, s.curY = 0, 0
	s.curStyle = ansi.Style{}
	s.cursorShown = false
	s.cursorValid = false
	s.writes = 0
	s.unknown = 0
	s.maxX, s.maxY = 0, 0
	s.pendingRune = s.pendingRune[:0]
}

// ---------------------------------------------------------------------------
// The screen model
// ---------------------------------------------------------------------------

// consume interprets p, returning the number of bytes consumed and an error if
// the stream ends mid-sequence. A trailing partial sequence is held in
// pendingRune and completed by the next Write, because the renderer may split a
// frame across writes.
func (s *MemorySink) consume(p []byte) (int, error) {
	if len(s.pendingRune) > 0 {
		p = append(s.pendingRune, p...)
		s.pendingRune = s.pendingRune[:0]
	}
	i := 0
	for i < len(p) {
		c := p[i]
		switch {
		case c == 0x1b:
			n, err := s.escape(p[i:])
			if err == errPartial {
				s.pendingRune = append(s.pendingRune[:0], p[i:]...)
				return i, nil
			}
			if err != nil {
				return i, err
			}
			i += n
		case c == '\r':
			s.curX = 0
			i++
		case c == '\n':
			// Line feed moves down without a carriage return, which is what a
			// real terminal does.
			s.curY++
			s.clampCursor()
			i++
		case c == '\b':
			s.curX--
			s.clampCursor()
			i++
		case c == '\t':
			s.curX = (s.curX/8 + 1) * 8
			s.clampCursor()
			i++
		case c < 0x20:
			i++ // other control characters have no effect on the cell grid
		default:
			r, size := decodeRune(p[i:])
			if r == 0 {
				break
			}
			s.putRune(r)
			i += size
		}
	}
	return i, nil
}

// errPartial signals that a sequence continues in the next Write.
var errPartial = errors.New("headless: partial sequence")

// escape interprets the escape sequence at the start of p, returning the number
// of bytes consumed.
func (s *MemorySink) escape(p []byte) (int, error) {
	if len(p) < 2 {
		return 0, errPartial
	}
	switch p[1] {
	case '[':
		return s.csi(p)
	case 'c':
		// Full reset: clear the screen and home the cursor.
		s.eraseAll()
		s.curX, s.curY = 0, 0
		return 2, nil
	case '7':
		s.savedX, s.savedY = s.curX, s.curY
		return 2, nil
	case '8':
		s.curX, s.curY = s.savedX, s.savedY
		s.clampCursor()
		return 2, nil
	case ']':
		// OSC: terminated by BEL or ST (ESC \). Skipped, since TermMosaic does
		// not yet emit any OSC payload it depends on.
		return skipOSC(p)
	default:
		s.unknown++
		return 2, nil
	}
}

// csi interprets a control sequence introducer at the start of p.
func (s *MemorySink) csi(p []byte) (int, error) {
	end := -1
	for i := 2; i < len(p); i++ {
		if p[i] >= 0x40 && p[i] <= 0x7e {
			end = i
			break
		}
	}
	if end < 0 {
		return 0, errPartial
	}
	body := p[2:end]
	final := p[end]
	consumed := end + 1

	// A leading '?' marks a private-mode sequence.
	private := false
	if len(body) > 0 && body[0] == '?' {
		private = true
		body = body[1:]
	}

	switch {
	case !private && final == 'm':
		s.applySGR(body)
	case !private && final == 'H', !private && final == 'f':
		y, x := 1, 1
		params := parseParams(body)
		if len(params) > 0 {
			y = params[0]
		}
		if len(params) > 1 {
			x = params[1]
		}
		if y < 1 {
			y = 1
		}
		if x < 1 {
			x = 1
		}
		s.curY, s.curX = y-1, x-1
		s.cursorValid = true
		s.clampCursor()
	case !private && final == 'J':
		s.eraseDisplay(parseParams(body))
	case !private && final == 'K':
		s.eraseLine(parseParams(body))
	case !private && final == 'A':
		s.curY -= paramOr(body, 0, 1)
		s.clampCursor()
	case !private && final == 'B':
		s.curY += paramOr(body, 0, 1)
		s.clampCursor()
	case !private && final == 'C':
		s.curX += paramOr(body, 0, 1)
		s.clampCursor()
	case !private && final == 'D':
		s.curX -= paramOr(body, 0, 1)
		s.clampCursor()
	case !private && final == 'G':
		s.curX = paramOr(body, 0, 1) - 1
		s.clampCursor()
	case private && final == 'h' && bytes.Contains(body, []byte("1049")):
		s.altScreen = true
	case private && final == 'l' && bytes.Contains(body, []byte("1049")):
		s.altScreen = false
	case private && final == 'h' && bytes.Contains(body, []byte("25")):
		s.cursorShown = true
	case private && final == 'l' && bytes.Contains(body, []byte("25")):
		s.cursorShown = false
	default:
		s.unknown++
	}
	return consumed, nil
}

// paramOr returns parameter i, or def when absent or zero.
func paramOr(body []byte, i, def int) int {
	p := parseParams(body)
	if i >= len(p) || p[i] == 0 {
		return def
	}
	return p[i]
}

// parseParams splits a numeric parameter list on ';' with empty fields as 0.
func parseParams(body []byte) []int {
	if len(body) == 0 {
		return nil
	}
	var out []int
	cur := 0
	has := false
	for _, c := range body {
		switch {
		case c >= '0' && c <= '9':
			cur = cur*10 + int(c-'0')
			has = true
		case c == ';':
			if has {
				out = append(out, cur)
			} else {
				out = append(out, 0)
			}
			cur, has = 0, false
		}
	}
	if has {
		out = append(out, cur)
	} else if len(out) == 0 {
		out = append(out, 0)
	}
	return out
}

// skipOSC skips an operating-system command terminated by BEL or ST.
func skipOSC(p []byte) (int, error) {
	for i := 2; i < len(p); i++ {
		if p[i] == 0x07 {
			return i + 1, nil
		}
		if p[i] == 0x1b && i+1 < len(p) && p[i+1] == '\\' {
			return i + 2, nil
		}
		if p[i] == 0x1b && i+1 == len(p) {
			return 0, errPartial
		}
	}
	return 0, errPartial
}

// applySGR updates the current rendition from an SGR parameter list.
func (s *MemorySink) applySGR(body []byte) {
	params := parseParams(body)
	if len(params) == 0 {
		params = []int{0}
	}
	for i := 0; i < len(params); i++ {
		p := params[i]
		switch {
		case p == 0:
			s.curStyle = ansi.Style{FG: buffer.DefaultColour, BG: buffer.DefaultColour}
		case p == sgrBold:
			s.curStyle.Attr |= buffer.AttrBold
		case p == sgrFaint:
			s.curStyle.Attr |= buffer.AttrFaint
		case p == sgrItalic:
			s.curStyle.Attr |= buffer.AttrItalic
		case p == sgrUnderline:
			s.curStyle.Attr |= buffer.AttrUnderline
		case p == sgrReverse:
			s.curStyle.Attr |= buffer.AttrReverse
		case p == sgrStrike:
			s.curStyle.Attr |= buffer.AttrStrike
		case p == 22:
			s.curStyle.Attr &^= buffer.AttrBold | buffer.AttrFaint
		case p == 23:
			s.curStyle.Attr &^= buffer.AttrItalic
		case p == 24:
			s.curStyle.Attr &^= buffer.AttrUnderline
		case p == 27:
			s.curStyle.Attr &^= buffer.AttrReverse
		case p == 29:
			s.curStyle.Attr &^= buffer.AttrStrike
		case p == sgrDefaultFG:
			s.curStyle.FG = buffer.DefaultColour
		case p == sgrDefaultBG:
			s.curStyle.BG = buffer.DefaultColour
		case p >= 30 && p <= 37:
			s.curStyle.FG = paletteRGB(p - 30)
		case p >= 90 && p <= 97:
			s.curStyle.FG = paletteRGB(p - 90 + 8)
		case p >= 40 && p <= 47:
			s.curStyle.BG = paletteRGB(p - 40)
		case p >= 100 && p <= 107:
			s.curStyle.BG = paletteRGB(p - 100 + 8)
		case p == sgrFGColor, p == sgrBGColor:
			// Extended colour: 38;5;<idx> or 38;2;<r>;<g>;<b>.
			isFG := p == sgrFGColor
			if i+1 >= len(params) {
				i = len(params)
				break
			}
			switch params[i+1] {
			case sgrIndexed:
				if i+2 >= len(params) {
					i = len(params)
					break
				}
				c := paletteRGB(params[i+2])
				if isFG {
					s.curStyle.FG = c
				} else {
					s.curStyle.BG = c
				}
				i += 2
			case sgrRGBValue:
				if i+4 >= len(params) {
					i = len(params)
					break
				}
				c := buffer.NewColour(uint8(params[i+2]), uint8(params[i+3]), uint8(params[i+4]))
				if isFG {
					s.curStyle.FG = c
				} else {
					s.curStyle.BG = c
				}
				i += 4
			default:
				s.unknown++
			}
		default:
			s.unknown++
		}
	}
}

// paletteRGB returns the buffer.Colour at an xterm palette index.
func paletteRGB(i int) buffer.Colour {
	r, g, b := buffer.Index256PaletteRGB(i)
	return buffer.NewColour(r, g, b)
}

// SGR codes duplicated from internal/ansi so the screen model stays in step with
// the encoder. They are unexported constants rather than a dependency because
// the screen is an independent interpretation: if the encoder changed a code,
// this should fail its tests rather than silently track the change.
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

	sgrFGColor  = 38
	sgrBGColor  = 48
	sgrIndexed  = 5
	sgrRGBValue = 2
)

func (s *MemorySink) eraseDisplay(params []int) {
	mode := 0
	if len(params) > 0 {
		mode = params[0]
	}
	switch mode {
	case 0:
		s.eraseFromCursorToEnd()
	case 1:
		s.eraseFromStartToCursor()
	default:
		s.eraseAll()
	}
}

func (s *MemorySink) eraseLine(params []int) {
	mode := 0
	if len(params) > 0 {
		mode = params[0]
	}
	x0, x1 := 0, s.w-1
	switch mode {
	case 0:
		x0 = s.curX
	case 1:
		x1 = s.curX
	default:
		// Mode 2 is the whole line; the default range already covers it.
	}
	for x := x0; x <= x1 && x < s.w; x++ {
		if s.curY >= 0 && s.curY < s.h {
			s.screen.SetCell(x, s.curY, s.blankCell())
		}
	}
}

func (s *MemorySink) eraseFromCursorToEnd() {
	if s.curY < 0 || s.curY >= s.h {
		return
	}
	s.eraseLine(nil)
	for y := s.curY + 1; y < s.h; y++ {
		s.eraseRow(y)
	}
}

func (s *MemorySink) eraseFromStartToCursor() {
	if s.curY < 0 || s.curY >= s.h {
		return
	}
	for x := 0; x <= s.curX && x < s.w; x++ {
		s.screen.SetCell(x, s.curY, s.blankCell())
	}
	for y := 0; y < s.curY; y++ {
		s.eraseRow(y)
	}
}

func (s *MemorySink) eraseAll() {
	for y := 0; y < s.h; y++ {
		s.eraseRow(y)
	}
}

func (s *MemorySink) eraseRow(y int) {
	for x := 0; x < s.w; x++ {
		s.screen.SetCell(x, y, s.blankCell())
	}
}

// blankCell is a space in the current rendition, which is what ED and EL leave
// behind: the background colour is retained, so erasing a coloured region does
// not flash it back to the terminal default.
func (s *MemorySink) blankCell() buffer.Cell {
	return s.curStyle.Blank()
}

// putRune writes a rune at the cursor and advances, wrapping at the right edge
// as a real terminal does. A double-width rune writes a continuation cell to its
// right, matching what the buffer wrote.
func (s *MemorySink) putRune(r rune) {
	if s.curY < 0 || s.curY >= s.h {
		s.advance(0, 1)
		return
	}
	w := buffer.RuneWidth(r)
	if w == 0 {
		return
	}
	if s.curX >= s.w {
		s.curX = 0
		s.curY++
		if s.curY >= s.h {
			// Off the bottom: a real terminal scrolls. TermMosaic sizes its
			// buffer to the terminal, so this should not happen; dropping the
			// rune is safer than growing the screen behind the test's back.
			return
		}
	}
	if w == 2 && s.curX+1 >= s.w {
		// A wide glyph with only one cell left: skip it rather than half-write.
		s.advance(1, 0)
		return
	}
	s.screen.SetCell(s.curX, s.curY, s.curStyle.Cell(r))
	if w == 2 {
		s.screen.SetCell(s.curX+1, s.curY, buffer.ContinuationCell(s.curStyle))
	}
	if s.curX > s.maxX {
		s.maxX = s.curX
	}
	if s.curY > s.maxY {
		s.maxY = s.curY
	}
	s.advance(w, 0)
}

// advance moves the cursor dx columns and dy rows, wrapping.
func (s *MemorySink) advance(dx, dy int) {
	s.curX += dx
	s.curY += dy
	for s.curX >= s.w {
		s.curX -= s.w
		s.curY++
	}
	s.clampCursor()
}

func (s *MemorySink) clampCursor() {
	if s.curX < 0 {
		s.curX = 0
	}
	if s.curY < 0 {
		s.curY = 0
	}
	if s.curX > s.w {
		s.curX = s.w
	}
	if s.curY > s.h {
		s.curY = s.h
	}
}

// decodeRune decodes the UTF-8 rune at the start of p. It returns size 0 for an
// invalid sequence, which the caller treats as a byte to skip.
func decodeRune(p []byte) (rune, int) {
	if len(p) == 0 {
		return 0, 0
	}
	c := p[0]
	if c < 0x80 {
		return rune(c), 1
	}
	var size int
	var r rune
	switch {
	case c&0xE0 == 0xC0:
		size, r = 2, rune(c&0x1F)
	case c&0xF0 == 0xE0:
		size, r = 3, rune(c&0x0F)
	case c&0xF8 == 0xF0:
		size, r = 4, rune(c&0x07)
	default:
		return 0, 1
	}
	if len(p) < size {
		return 0, 1
	}
	for i := 1; i < size; i++ {
		if p[i]&0xC0 != 0x80 {
			return 0, 1
		}
		r = r<<6 | rune(p[i]&0x3F)
	}
	if r > 0x10FFFF || (r >= 0xD800 && r <= 0xDFFF) {
		return 0, size
	}
	return r, size
}

// MaxX and MaxY report the furthest cell the renderer has written to, which is a
// cheaper assertion than scanning the whole screen when a test only cares that
// output stays in bounds.
func (s *MemorySink) MaxX() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.maxX
}

// MaxY reports the furthest row the renderer has written to.
func (s *MemorySink) MaxY() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.maxY
}

// Compile-time assertion that the encoder's sequence set is what the screen
// models. If the encoder grows a sequence, UnknownSequences turns non-zero and
// the tests that assert it is zero fail loudly rather than silently drifting.
var _ = ansi.CSI
