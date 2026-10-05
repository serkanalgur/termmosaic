package input

import (
	"strings"
	"testing"
	"time"

	"github.com/serkanalgur/termmosaic"
)

// feed runs one Feed and returns the events it produced.
//
// The destination is sized to the input rather than to a fixed 64, and that is a
// requirement rather than a style choice: Feed takes the caller's slice BY VALUE
// and returns only a count, so a caller that lets Feed grow the slice loses
// track of the grown one. Source avoids this by sizing its batch to the read
// buffer; a test has to do the same.
func feed(p *Parser, b string) []termmosaic.Event {
	dst := make([]termmosaic.Event, 0, len(b)+2)
	n := p.Feed(dst, []byte(b))
	return dst[:n]
}

func TestParserFeedsPlainText(t *testing.T) {
	p := NewParser(Config{})
	got := feed(p, "abc")
	if len(got) != 3 {
		t.Fatalf("got %d events, want 3: %+v", len(got), got)
	}
	for i, ev := range got {
		if ev.Kind != termmosaic.EventKey || ev.Rune != rune('a'+i) {
			t.Errorf("event %d = %+v, want rune %q", i, ev, 'a'+i)
		}
	}
}

func TestParserReassemblesASplitSequence(t *testing.T) {
	// The whole reason StatusIncomplete exists. Feeding an arrow one byte at a
	// time must produce exactly one KeyUp, which a test can do because Decode
	// needs no pipe and no goroutine.
	for _, seq := range []string{"\x1b[A", "\x1b[1;5A", "\x1b[3~", "\x1bOP", "\x1b[<0;4;5M", "\x1b[I", "\x1b[97;5u"} {
		p := NewParser(Config{})
		var got []termmosaic.Event
		for i := 0; i < len(seq); i++ {
			got = append(got, feed(p, seq[i:i+1])...)
		}
		if len(got) != 1 {
			t.Errorf("%q fed one byte at a time produced %d events, want 1: %+v", seq, len(got), got)
			continue
		}
		// And the whole thing at once must agree.
		whole := NewParser(Config{})
		all := feed(whole, seq)
		if len(all) != 1 || all[0] != got[0] {
			t.Errorf("%q: split gave %+v, whole gave %+v", seq, got, all)
		}
	}
}

func TestParserReassemblesASplitMultiByteRune(t *testing.T) {
	p := NewParser(Config{})
	var got []termmosaic.Event
	for _, part := range []string{"\xc3", "\xa9", "x"} {
		got = append(got, feed(p, part)...)
	}
	if len(got) != 2 {
		t.Fatalf("got %d events, want 2: %+v", len(got), got)
	}
	if got[0].Rune != 'é' || got[1].Rune != 'x' {
		t.Errorf("got %q then %q, want é then x", got[0].Rune, got[1].Rune)
	}
}

func TestParserDeliversAPasteAsOneEventAcrossManyFeeds(t *testing.T) {
	// One event per paste is the decision, and it has to hold when the paste
	// arrives in pieces — which is what happens whenever it is larger than one
	// read(2) buffer.
	p := NewParser(Config{})
	body := strings.Repeat("abcdefghij", 500)
	seq := "\x1b[200~" + body + "\x1b[201~"

	var got []termmosaic.Event
	// Feed in 7-byte chunks so both markers are split across reads too.
	for i := 0; i < len(seq); i += 7 {
		end := i + 7
		if end > len(seq) {
			end = len(seq)
		}
		got = append(got, feed(p, seq[i:end])...)
	}
	if len(got) != 1 {
		t.Fatalf("a paste fed in 7-byte chunks produced %d events, want 1", len(got))
	}
	if got[0].Kind != termmosaic.EventPaste {
		t.Errorf("kind = %v, want EventPaste", got[0].Kind)
	}
	if got[0].Text != body {
		t.Errorf("payload length = %d, want %d", len(got[0].Text), len(body))
	}
	if got[0].Truncated {
		t.Error("a 5000-byte paste must not be truncated")
	}
	if st := p.Stats(); st.PastesTruncated != 0 {
		t.Errorf("Stats.PastesTruncated = %d, want 0", st.PastesTruncated)
	}
}

func TestParserTruncatesAnOversizedPasteAndResyncs(t *testing.T) {
	// The specified behaviour: keep the first MaxPasteBytes, flag the event, and
	// KEEP SCANNING to the closing marker. Abandoning it instead would leave the
	// decoder out of sync with the terminal for every byte afterwards, which is a
	// far worse failure than a short paste.
	p := NewParser(Config{MaxPasteBytes: 16})
	body := strings.Repeat("z", 4096)
	seq := "\x1b[200~" + body + "\x1b[201~"

	var got []termmosaic.Event
	for i := 0; i < len(seq); i += 64 {
		end := i + 64
		if end > len(seq) {
			end = len(seq)
		}
		got = append(got, feed(p, seq[i:end])...)
	}
	if len(got) != 1 {
		t.Fatalf("got %d events, want 1: %+v", len(got), got)
	}
	if got[0].Kind != termmosaic.EventPaste {
		t.Fatalf("kind = %v, want EventPaste", got[0].Kind)
	}
	if !got[0].Truncated {
		t.Error("Truncated = false, want true")
	}
	if len(got[0].Text) != 16 {
		t.Errorf("payload length = %d, want 16", len(got[0].Text))
	}
	if got[0].Text != strings.Repeat("z", 16) {
		t.Errorf("payload = %q, want the FIRST 16 bytes of the paste", got[0].Text)
	}
	if st := p.Stats(); st.PastesTruncated != 1 {
		t.Errorf("Stats.PastesTruncated = %d, want 1", st.PastesTruncated)
	}
}

func TestParserResyncsAfterATruncatedPaste(t *testing.T) {
	// The specific failure the truncation rule exists to prevent: after an
	// oversized paste the stream must be usable, not permanently offset.
	p := NewParser(Config{MaxPasteBytes: 8})
	var got []termmosaic.Event
	got = append(got, feed(p, "\x1b[200~"+strings.Repeat("q", 500)+"\x1b[201~")...)
	got = append(got, feed(p, "a")...)
	got = append(got, feed(p, "\x1b[A")...)

	if len(got) != 3 {
		t.Fatalf("got %d events, want 3 (paste, 'a', KeyUp): %+v", len(got), got)
	}
	if got[0].Kind != termmosaic.EventPaste || !got[0].Truncated {
		t.Errorf("event 0 = %+v, want a truncated EventPaste", got[0])
	}
	if got[1].Rune != 'a' {
		t.Errorf("event 1 = %+v, want rune 'a'", got[1])
	}
	if got[2].Key != termmosaic.KeyUp {
		t.Errorf("event 2 = %+v, want KeyUp", got[2])
	}
}

func TestParserCountsMalformedSequences(t *testing.T) {
	// The bound is exact: 64 bytes without a final byte is still a legitimate
	// incomplete sequence and is RETAINED, and one more byte makes it a runaway
	// whose first 64 bytes are discarded. The bytes that follow are still
	// decoded, because scanning resumes after the discarded run rather than
	// throwing away everything that happened to arrive alongside it.
	p := NewParser(Config{})
	atBound := "\x1b[" + strings.Repeat("0", maxSequenceLength-2)
	if len(atBound) != maxSequenceLength {
		t.Fatalf("test setup: %d bytes, want %d", len(atBound), maxSequenceLength)
	}
	if got := feed(p, atBound); len(got) != 0 {
		t.Fatalf("a sequence exactly at the bound produced %d events, want 0", len(got))
	}
	if st := p.Stats(); st.Malformed != 0 {
		t.Fatalf("Stats.Malformed = %d before the bound was exceeded, want 0", st.Malformed)
	}

	// One byte more and it is a runaway. The discarded run is exactly
	// maxSequenceLength, and the filler AFTER it is decoded as ordinary keys:
	// scanning resumes at the first byte after the discarded run rather than
	// discarding everything that arrived alongside it.
	fresh := NewParser(Config{})
	runaway := "\x1b[" + strings.Repeat("0", 200)
	got := feed(fresh, runaway)
	if len(got) != len(runaway)-maxSequenceLength {
		t.Fatalf("got %d events, want %d: exactly the bytes after the discarded run", len(got), len(runaway)-maxSequenceLength)
	}
	for i, ev := range got {
		if ev.Rune != '0' {
			t.Fatalf("event %d = %+v, want a filler key", i, ev)
		}
	}
	if st := fresh.Stats(); st.Malformed != 1 {
		t.Errorf("Stats.Malformed = %d, want 1", st.Malformed)
	}
}

func TestParserEscapeDelayArmsAndExpires(t *testing.T) {
	// The clock is injectable, so this is a deterministic assertion rather than
	// a sleep-and-hope timing test — which is the point of keeping the deadline
	// in the parser instead of in Decode.
	p := NewParser(Config{EscapeDelay: 25 * time.Millisecond})
	now := time.Unix(0, 0)
	p.now = func() time.Time { return now }

	if got := feed(p, "\x1b"); len(got) != 0 {
		t.Fatalf("a bare ESC produced %d events, want 0 until the delay expires", len(got))
	}
	if !p.EscapePending() {
		t.Fatal("EscapePending = false, want true after a bare ESC")
	}
	now = now.Add(24 * time.Millisecond)
	if _, ok := p.expireEscape(); ok {
		t.Error("expireEscape fired before the delay elapsed")
	}
	if !p.EscapePending() {
		t.Error("a premature expireEscape must leave the wait armed")
	}
	now = now.Add(2 * time.Millisecond)
	ev, ok := p.expireEscape()
	if !ok {
		t.Fatal("expireEscape reported nothing after the delay elapsed")
	}
	if ev.Key != termmosaic.KeyEscape || ev.Kind != termmosaic.EventKey {
		t.Errorf("event = %+v, want an EventKey for KeyEscape", ev)
	}
	if p.EscapePending() {
		t.Error("EscapePending = true after the escape was reported")
	}
}

func TestParserEscapeDelayIsCancelledByTheNextByte(t *testing.T) {
	// A pending ESC resolved by more bytes must NOT also fire as KeyEscape.
	// Delivering both would be a lost keystroke plus a spurious key.
	p := NewParser(Config{EscapeDelay: 25 * time.Millisecond})
	now := time.Unix(0, 0)
	p.now = func() time.Time { return now }

	feed(p, "\x1b")
	if !p.EscapePending() {
		t.Fatal("EscapePending = false after a bare ESC")
	}
	got := feed(p, "\x1b[")
	if p.EscapePending() {
		t.Error("EscapePending = true after the ESC became half an arrow")
	}
	if len(got) != 0 {
		t.Errorf("a half sequence produced %d events, want 0", len(got))
	}
	now = now.Add(time.Hour)
	if _, ok := p.expireEscape(); ok {
		t.Error("expireEscape fired for a sequence that was never resolved")
	}
	// And the sequence still completes normally.
	got = append(got, feed(p, "A")...)
	if len(got) != 1 || got[0].Key != termmosaic.KeyUp {
		t.Errorf("got %+v, want one KeyUp: a resize or a timer must not flush a partial sequence", got)
	}
}

func TestParserNegativeEscapeDelayDisablesTheWait(t *testing.T) {
	p := NewParser(Config{EscapeDelay: -1})
	got := feed(p, "\x1b")
	if len(got) != 1 || got[0].Key != termmosaic.KeyEscape {
		t.Fatalf("got %+v, want one KeyEscape with no wait", got)
	}
	if p.EscapePending() {
		t.Error("EscapePending = true with the wait disabled")
	}
	// And Alt-chords are documented as the cost of that, not as a bug to hide.
	chord := NewParser(Config{EscapeDelay: -1})
	if got := feed(chord, "\x1ba"); len(got) != 1 || got[0].Rune != 'a' {
		t.Errorf("got %+v, want Alt+a", got)
	}
}

func TestParserResetDropsPartialState(t *testing.T) {
	// Reset is called on raw-mode entry so a half-read sequence from before the
	// mode change cannot leak into the new mode: a stale ESC would otherwise be
	// reported as a key press the user never made, minutes later.
	// The precondition is the point: Reset can only be shown to drop state that
	// was actually there, so each half of it is asserted rather than assumed.
	partial := NewParser(Config{})
	feed(partial, "\x1b[")
	if len(partial.partial) == 0 {
		t.Fatal("precondition: an incomplete CSI retained no bytes to drop")
	}
	partial.Reset()
	if len(partial.partial) != 0 {
		t.Errorf("partial = %q after Reset, want empty", partial.partial)
	}

	// A paste must be open ACROSS calls for there to be an abandoned one: a paste
	// whose marker and payload arrive together is decoded whole and never leaves
	// the accumulator holding anything.
	p := NewParser(Config{})
	feed(p, "\x1b[200~")
	feed(p, "partial")
	if !p.inPaste || string(p.paste) != "partial" {
		t.Fatalf("precondition: paste not open with its payload held: inPaste=%v paste=%q",
			p.inPaste, p.paste)
	}
	p.Reset()

	now := time.Unix(0, 0)
	p.now = func() time.Time { return now }
	if p.EscapePending() {
		t.Error("EscapePending = true after Reset")
	}
	if p.inPaste || len(p.paste) != 0 {
		t.Errorf("paste state survived Reset: inPaste=%v paste=%q", p.inPaste, p.paste)
	}
	// A paste left open by Reset must not swallow the rest of the stream.
	// The orphaned closing marker is consumed as a tilde form we do not
	// implement, and the byte after it is a key. The point is that the paste
	// Reset abandoned does not swallow the rest of the stream.
	got := feed(p, "\x1b[201~a")
	if len(got) != 1 {
		t.Fatalf("got %+v, want the closing marker swallowed and 'a' delivered", got)
	}
	if got[0].Rune != 'a' {
		t.Errorf("event 0 = %+v, want 'a'", got[0])
	}
}

func TestParserPushAppendsNonDecodedEvents(t *testing.T) {
	p := NewParser(Config{})
	// dst must have spare CAPACITY, because Push takes the slice by value and
	// returns only a count: the event it appended is reachable only by extending
	// the caller's own slice over it.
	dst := make([]termmosaic.Event, 0, 8)
	if n := p.Push(dst, termmosaic.ResizeEvent(80, 24)); n != 1 {
		t.Fatalf("Push returned %d, want 1: it reports how MANY it appended, not the new length", n)
	}
	dst = dst[:1]
	dst = append(dst, termmosaic.SpecialKeyEvent(termmosaic.KeyUp, 0))
	if n := p.Push(dst, termmosaic.ResizeEvent(100, 30)); n != 1 {
		t.Fatalf("Push returned %d, want 1", n)
	}
	dst = dst[:2]
	if len(dst) != 2 || dst[0].Kind != termmosaic.EventResize || dst[1].Key != termmosaic.KeyUp {
		t.Fatalf("dst = %+v, want the resize then the KeyUp", dst)
	}
}

func TestParserReusesItsBuffers(t *testing.T) {
	// The parser holds two reusable buffers. If either were reallocated per Feed
	// the key path would allocate, which is the one performance claim ADR 0005
	// makes; TestDecodeIsZeroAllocation is the hard version of this and this one
	// just names the intent.
	p := NewParser(Config{})
	feed(p, "\x1b[1;5A")
	partial := cap(p.partial)
	pasteCap := cap(p.paste)
	for i := 0; i < 50; i++ {
		feed(p, "\x1b[200~some text\x1b[201~")
		feed(p, "\x1b[")
	}
	if cap(p.partial) != partial {
		t.Errorf("partial capacity changed from %d to %d", partial, cap(p.partial))
	}
	if cap(p.paste) < pasteCap {
		t.Errorf("paste capacity shrank from %d to %d", pasteCap, cap(p.paste))
	}
}

func TestParserStatsStartAtZero(t *testing.T) {
	p := NewParser(Config{})
	if got := p.Stats(); got != (Stats{}) {
		t.Errorf("a fresh parser reports %+v, want zero", got)
	}
}
