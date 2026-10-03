package headless

import (
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/internal/ansi"
)

func TestTerminalSizeAndResize(t *testing.T) {
	term := New(80, 24)
	if w, h := term.Size(); w != 80 || h != 24 {
		t.Fatalf("Size = %d,%d want 80,24", w, h)
	}
	if caps := term.Capabilities(); caps != termmosaic.DefaultCaps() {
		t.Errorf("Capabilities = %+v, want the defaults", caps)
	}

	term.SetSize(100, 30)
	if w, h := term.Size(); w != 100 || h != 30 {
		t.Fatalf("Size after SetSize = %d,%d", w, h)
	}
	select {
	case s := <-term.ResizeEvents():
		if s.W != 100 || s.H != 30 {
			t.Errorf("resize event = %+v, want 100x30", s)
		}
	case <-time.After(time.Second):
		t.Fatal("no resize event delivered")
	}
}

func TestTerminalCapabilitiesOption(t *testing.T) {
	caps := termmosaic.Caps{Color256: true}
	term := New(10, 10, WithCaps(caps))
	if got := term.Capabilities(); got != caps {
		t.Errorf("Capabilities = %+v, want %+v", got, caps)
	}
	if got := term.Capabilities().ColourDepth(); got != buffer.Depth256 {
		t.Errorf("ColourDepth = %v, want 256", got)
	}
}

func TestRawAndAltScreenState(t *testing.T) {
	term := New(10, 10)
	if err := term.EnterRawMode(); err != nil {
		t.Fatal(err)
	}
	if !term.RawMode() {
		t.Error("RawMode should be true after EnterRawMode")
	}
	if err := term.EnterRawMode(); err != nil {
		t.Fatalf("a second EnterRawMode must be a no-op, got %v", err)
	}
	if err := term.EnterAltScreen(); err != nil {
		t.Fatal(err)
	}
	if !term.AltScreen() {
		t.Error("AltScreen should be true")
	}
	if err := term.LeaveRawMode(); err != nil {
		t.Fatal(err)
	}
	if term.RawMode() {
		t.Error("RawMode should be false")
	}
	if err := term.LeaveAltScreen(); err != nil {
		t.Fatal(err)
	}
	if term.AltScreen() {
		t.Error("AltScreen should be false")
	}
}

func TestFeedAndRead(t *testing.T) {
	term := New(10, 10)
	term.FeedString("q")
	if term.PendingInput() != 1 {
		t.Errorf("PendingInput = %d, want 1", term.PendingInput())
	}
	p := make([]byte, 8)
	n, err := term.Read(p)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || string(p[:n]) != "q" {
		t.Errorf("Read = %q (%d), want \"q\"", p[:n], n)
	}
}

func TestReadUnblocksOnClose(t *testing.T) {
	term := New(10, 10)
	errCh := make(chan error, 1)
	go func() {
		_, err := term.Read(make([]byte, 4))
		errCh <- err
	}()
	time.Sleep(20 * time.Millisecond)
	if err := term.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-errCh:
		if !errors.Is(err, io.EOF) {
			t.Errorf("Read after close = %v, want io.EOF", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close did not unblock Read")
	}
}

func TestCloseIsIdempotentAndClosesChannels(t *testing.T) {
	term := New(10, 10)
	if err := term.Close(); err != nil {
		t.Fatal(err)
	}
	if err := term.Close(); err != nil {
		t.Fatalf("a second Close must be a no-op, got %v", err)
	}
	if !term.Closed() {
		t.Error("Closed should report true")
	}
	if _, ok := <-term.ResizeEvents(); ok {
		t.Error("ResizeEvents should be closed")
	}
	if err := term.EnterRawMode(); !errors.Is(err, ErrClosed()) {
		t.Errorf("EnterRawMode on a closed terminal = %v, want ErrClosed", err)
	}
	// Feeding a closed terminal must not panic.
	term.FeedString("x")
}

func TestCloseRestoresTerminalState(t *testing.T) {
	term := New(10, 10)
	_ = term.EnterRawMode()
	_ = term.EnterAltScreen()
	_ = term.Close()
	if term.RawMode() || term.AltScreen() {
		t.Error("Close must leave raw mode and the alternate screen; a broken shell is worse than an error")
	}
}

// --- MemorySink --------------------------------------------------------------

func TestMemorySinkExposesCellsNotJustBytes(t *testing.T) {
	s := NewMemorySink(10, 2)
	green := buffer.NewColour(0x30, 0xc0, 0x80)
	enc := ansi.Encoder{Depth: ansi.DepthTrueColor}
	out := ansi.AppendString(nil, "\x1b[1;1H")
	out = enc.AppendSGR(out, ansi.Style{FG: green, BG: buffer.DefaultColour})
	out = append(out, "hi"...)
	if _, err := s.Write(out); err != nil {
		t.Fatal(err)
	}
	if s.UnknownSequences() != 0 {
		t.Fatalf("unknown sequences: %q", s.RawString())
	}
	c := s.CellAt(0, 0)
	if c.Ch != 'h' || c.FG != green {
		t.Errorf("CellAt(0,0) = %+v, want 'h' in %v", c, green)
	}
	if got := s.CellAt(1, 0).Ch; got != 'i' {
		t.Errorf("CellAt(1,0) = %q, want 'i'", got)
	}
	// The byte surface is available too.
	if !strings.Contains(s.RawString(), "38;2") {
		t.Error("raw bytes should still be recorded")
	}
	if len(s.Cells()) != 20 {
		t.Errorf("Cells() length = %d, want 20", len(s.Cells()))
	}
	// Cells is a copy: mutating it must not affect the screen.
	cells := s.Cells()
	cells[0] = buffer.DefaultCell
	if s.CellAt(0, 0).Ch != 'h' {
		t.Error("Cells() must return a copy")
	}
}

func TestMemorySinkTrueColorAndAttributes(t *testing.T) {
	s := NewMemorySink(10, 1)
	style := ansi.Style{
		FG:   buffer.NewColour(0x12, 0x34, 0x56),
		BG:   buffer.NewColour(0xab, 0xcd, 0xef),
		Attr: buffer.AttrBold | buffer.AttrUnderline,
	}
	enc := ansi.Encoder{Depth: ansi.DepthTrueColor}
	if _, err := s.Write(enc.AppendSGR(nil, style)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Write([]byte("A")); err != nil {
		t.Fatal(err)
	}
	c := s.CellAt(0, 0)
	if c.FG != style.FG || c.BG != style.BG {
		t.Errorf("colours = %v/%v, want %v/%v", c.FG, c.BG, style.FG, style.BG)
	}
	if c.Attr != style.Attr {
		t.Errorf("attr = %v, want %v", c.Attr, style.Attr)
	}
	if s.UnknownSequences() != 0 {
		t.Errorf("unknown sequences: %q", s.RawString())
	}
}

func TestMemorySinkIndex256AndNamed16(t *testing.T) {
	cases := []struct {
		depth ansi.Depth
		want  buffer.Colour
	}{
		{ansi.Depth256, func() buffer.Colour {
			r, g, b := buffer.Index256PaletteRGB(int(buffer.NewColour(0xff, 0, 0).Index256()))
			return buffer.NewColour(r, g, b)
		}()},
		{ansi.Depth16, func() buffer.Colour {
			r, g, b := buffer.Index256PaletteRGB(int(buffer.NewColour(0xff, 0, 0).Named16()))
			return buffer.NewColour(r, g, b)
		}()},
	}
	for _, tc := range cases {
		enc := ansi.Encoder{Depth: tc.depth}
		s := NewMemorySink(4, 1)
		if _, err := s.Write(enc.AppendSGR(nil, ansi.Style{
			FG: buffer.NewColour(0xff, 0, 0), BG: buffer.DefaultColour,
		})); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Write([]byte("A")); err != nil {
			t.Fatal(err)
		}
		if got := s.CellAt(0, 0).FG; got != tc.want {
			t.Errorf("depth %s: FG = %v, want %v", tc.depth, got, tc.want)
		}
		if s.UnknownSequences() != 0 {
			t.Errorf("depth %s: unknown sequences: %q", tc.depth, s.RawString())
		}
	}
}

func TestMemorySinkNoColour(t *testing.T) {
	s := NewMemorySink(4, 1)
	enc := ansi.Encoder{Depth: ansi.DepthTrueColor, NoColor: true}
	if _, err := s.Write(enc.AppendSGR(nil, ansi.Style{
		FG: buffer.NewColour(1, 2, 3), BG: buffer.NewColour(4, 5, 6), Attr: buffer.AttrBold,
	})); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Write([]byte("A")); err != nil {
		t.Fatal(err)
	}
	c := s.CellAt(0, 0)
	if c.FG != buffer.DefaultColour || c.BG != buffer.DefaultColour {
		t.Errorf("NO_COLOR left colours set: %+v", c)
	}
	if !c.Attr.Has(buffer.AttrBold) {
		t.Error("NO_COLOR must not suppress attributes")
	}
}

func TestMemorySinkCursorVisibility(t *testing.T) {
	s := NewMemorySink(4, 1)
	if _, err := s.Write([]byte(ansi.ShowCursor)); err != nil {
		t.Fatal(err)
	}
	if _, _, shown := s.Cursor(); !shown {
		t.Error("cursor should be shown")
	}
	if _, err := s.Write([]byte(ansi.HideCursor)); err != nil {
		t.Fatal(err)
	}
	if _, _, shown := s.Cursor(); shown {
		t.Error("cursor should be hidden")
	}
}

func TestMemorySinkEraseDisplayAndLine(t *testing.T) {
	s := NewMemorySink(4, 2)
	// The renderer addresses cells with CUP, not line feeds: in raw mode \n is
	// a line feed with no carriage return, so relying on newline positioning
	// would be wrong.
	if _, err := s.Write([]byte("abcd\x1b[2;1Hefgh")); err != nil {
		t.Fatal(err)
	}
	if s.Line(0) != "abcd" {
		t.Fatalf("row 0 = %q", s.Line(0))
	}
	// Erase to end of line from position 2.
	if _, err := s.Write([]byte("\x1b[1;3H\x1b[K")); err != nil {
		t.Fatal(err)
	}
	if got := s.Line(0); got != "ab" {
		t.Errorf("after EL row 0 = %q, want %q", got, "ab")
	}
	if got := s.Line(1); got != "efgh" {
		t.Errorf("EL must not touch other rows, row 1 = %q", got)
	}
	// Full erase.
	if _, err := s.Write([]byte(ansi.EraseDisplay)); err != nil {
		t.Fatal(err)
	}
	if got := s.String(); strings.TrimSpace(got) != "" {
		t.Errorf("after ED the screen is %q, want blank", got)
	}
}

func TestMemorySinkEraseRetainsBackground(t *testing.T) {
	s := NewMemorySink(4, 1)
	enc := ansi.Encoder{Depth: ansi.DepthTrueColor}
	bg := buffer.NewColour(0x11, 0x22, 0x33)
	if _, err := s.Write(enc.AppendSGR(nil, ansi.Style{FG: buffer.DefaultColour, BG: bg})); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Write([]byte("ab\x1b[1;3H\x1b[K")); err != nil {
		t.Fatal(err)
	}
	if got := s.CellAt(3, 0).BG; got != bg {
		t.Errorf("erased cell BG = %v, want %v: an erase must not flash the region to the default", got, bg)
	}
}

func TestMemorySinkWideGlyph(t *testing.T) {
	s := NewMemorySink(4, 1)
	if _, err := s.Write([]byte("a漢b")); err != nil {
		t.Fatal(err)
	}
	if !s.CellAt(2, 0).IsContinuation() {
		t.Error("the right half of a wide glyph must be a continuation cell")
	}
	// The text surface skips continuation cells so the string stays aligned.
	if got := s.Line(0); got != "a漢b" {
		t.Errorf("row = %q, want %q", got, "a漢b")
	}
}

func TestMemorySinkSplitWriteIsReassembled(t *testing.T) {
	s := NewMemorySink(4, 1)
	enc := ansi.Encoder{Depth: ansi.DepthTrueColor}
	full := enc.AppendSGR(nil, ansi.Style{FG: buffer.NewColour(1, 2, 3), BG: buffer.DefaultColour})
	// Split the escape sequence across two writes, as a real writer can.
	if _, err := s.Write(full[:4]); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Write(full[4:]); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Write([]byte("A")); err != nil {
		t.Fatal(err)
	}
	if got := s.CellAt(0, 0).FG; got != buffer.NewColour(1, 2, 3) {
		t.Errorf("FG after a split write = %v, want #010203", got)
	}
}

func TestMemorySinkResizeClears(t *testing.T) {
	s := NewMemorySink(4, 2)
	if _, err := s.Write([]byte("ab\x1b[2;1Hcd")); err != nil {
		t.Fatal(err)
	}
	s.Resize(8, 4)
	if w, h := s.Size(); w != 8 || h != 4 {
		t.Fatalf("size = %dx%d", w, h)
	}
	if strings.TrimSpace(s.String()) != "" {
		t.Errorf("Resize must clear the screen, got %q", s.String())
	}
}

func TestMemorySinkMarkFrameAndReset(t *testing.T) {
	s := NewMemorySink(4, 1)
	if _, err := s.Write([]byte("a")); err != nil {
		t.Fatal(err)
	}
	if got := string(s.MarkFrame()); got != "a" {
		t.Errorf("MarkFrame = %q, want %q", got, "a")
	}
	if got := len(s.MarkFrame()); got != 0 {
		t.Errorf("a second MarkFrame with no writes returned %d bytes", got)
	}
	if s.Writes() != 1 {
		t.Errorf("Writes = %d, want 1", s.Writes())
	}
	s.Reset()
	if s.Writes() != 0 || len(s.Bytes()) != 0 || strings.TrimSpace(s.String()) != "" {
		t.Error("Reset must clear bytes, screen and counters")
	}
}

func TestMemorySinkUnknownSequenceIsCounted(t *testing.T) {
	s := NewMemorySink(4, 1)
	// A sequence the screen does not model: it must be counted, not ignored, so
	// that a test asserting on the screen knows the screen is not faithful.
	if _, err := s.Write([]byte("\x1b[3 q")); err != nil {
		t.Fatal(err)
	}
	if s.UnknownSequences() == 0 {
		t.Error("an unrecognised sequence must be counted")
	}
}

func TestMemorySinkMaxCoords(t *testing.T) {
	s := NewMemorySink(10, 10)
	if _, err := s.Write([]byte("\x1b[3;5HA")); err != nil {
		t.Fatal(err)
	}
	if s.MaxX() != 4 || s.MaxY() != 2 {
		t.Errorf("MaxX/MaxY = %d/%d, want 4/2", s.MaxX(), s.MaxY())
	}
}

func TestMemorySinkOutOfRangeIsSafe(t *testing.T) {
	s := NewMemorySink(4, 2)
	if got := s.CellAt(-1, -1); got != buffer.DefaultCell {
		t.Error("out-of-range CellAt must return DefaultCell")
	}
	if got := s.CellAt(99, 99); got != buffer.DefaultCell {
		t.Error("out-of-range CellAt must return DefaultCell")
	}
	if got := s.Line(99); got != "" {
		t.Errorf("out-of-range Line = %q, want empty", got)
	}
	// Writing past the bottom edge must not grow the screen.
	if _, err := s.Write([]byte("\x1b[99;99HA")); err != nil {
		t.Fatal(err)
	}
	if w, h := s.Size(); w != 4 || h != 2 {
		t.Errorf("size changed to %dx%d", w, h)
	}
}

func TestCapsColourDepthLadder(t *testing.T) {
	cases := []struct {
		caps termmosaic.Caps
		want buffer.ColourDepth
	}{
		{termmosaic.Caps{TrueColor: true, Color256: true}, buffer.DepthTrueColor},
		{termmosaic.Caps{Color256: true}, buffer.Depth256},
		{termmosaic.Caps{}, buffer.Depth16},
	}
	for _, tc := range cases {
		if got := tc.caps.ColourDepth(); got != tc.want {
			t.Errorf("%+v.ColourDepth() = %v, want %v", tc.caps, got, tc.want)
		}
	}
}

func BenchmarkMemorySinkWrite(b *testing.B) {
	s := NewMemorySink(200, 60)
	enc := ansi.Encoder{Depth: ansi.DepthTrueColor}
	frame := enc.AppendSGR(nil, ansi.Style{FG: buffer.NewColour(1, 2, 3), BG: buffer.NewColour(4, 5, 6)})
	frame = append(frame, []byte("\x1b[1;1H")...)
	frame = append(frame, "hello world"...)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := s.Write(frame); err != nil {
			b.Fatal(err)
		}
	}
}
