package input

import (
	"unicode/utf8"

	"github.com/serkanalgur/termmosaic"
)

// Escape and control bytes with special meaning to the decoder.
const (
	escByte = 0x1b // ESC
	delByte = 0x7f // DEL, what most terminals send for backspace
	belByte = 0x07 // BEL, an OSC terminator
	delRune = '�'  // replacement rune for an invalid byte run
)

// Bracketed-paste markers, as a terminal sends them. These two literals and the
// helpers around them are the single definition of "is this a paste": Decode
// and Parser both go through them rather than each recognising the markers
// separately, so Parser's incremental path cannot drift from Decode's
// whole-paste path.
var (
	pasteStart = []byte("\x1b[200~")
	pasteStop  = []byte("\x1b[201~")
)

// pasteStartLen returns the length of a complete bracketed-paste start marker
// at the front of seq, or 0 if seq does not begin with one.
func pasteStartLen(seq []byte) int {
	if len(seq) < len(pasteStart) {
		return 0
	}
	for i := range pasteStart {
		if seq[i] != pasteStart[i] {
			return 0
		}
	}
	return len(pasteStart)
}

// pasteEnd returns the index at which the first bracketed-paste end marker
// begins in seq, or -1 if there is not one.
//
// It returns the START of the marker, not the index just past it, because both
// callers need the payload boundary and the consumed length and they differ by
// len(pasteStop). Returning the wrong end here is a silent bug rather than a
// crash: the marker would be delivered as the tail of the pasted text.
func pasteEnd(seq []byte) int {
	for i := 0; i+len(pasteStop) <= len(seq); i++ {
		if seq[i] != escByte {
			continue
		}
		match := true
		for k := 1; k < len(pasteStop); k++ {
			if seq[i+k] != pasteStop[k] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

// pasteEvent builds the single event a bracketed paste is delivered as.
//
// One event per paste is the decision, not an optimisation: a 10,000-character
// paste delivered as 10,000 key events would run Handle 10,000 times through the
// whole widget tree, push 10,000 undo entries making one Ctrl-Z useless, and run
// every onChange callback 10,000 times. As one event it is one undoable
// operation, which is what every editor does and what users expect.
func pasteEvent(payload []byte, truncated bool) termmosaic.Event {
	return termmosaic.Event{
		Kind:      termmosaic.EventPaste,
		Text:      string(payload),
		Truncated: truncated,
	}
}

// Decode decodes exactly one input sequence from the front of seq.
//
// Decode is pure: it reads seq and returns values. It does not block, does not
// consult a clock, does not retain seq, does not write to anything, and — on
// every path except a bracketed paste, whose payload is inherently a copy — does
// not allocate. Config is taken by value.
//
// The three statuses are the whole contract, and StatusIncomplete is the
// important one: it is what makes a sequence that straddles two reads a return
// value rather than a bug. n is the number of bytes to discard, and it is zero
// for StatusIncomplete, which means "retain everything you were given".
//
// Decode has no clock and therefore cannot decide whether a bare ESC is the
// Escape key or the first byte of an arrow key: Decode returns
// StatusIncomplete for a bare ESC and Parser waits out Config.EscapeDelay.
// KeyEscape is never emitted by Decode; see Parser.
//
// The returned event's Kind is EventNone when the bytes were a recognised
// protocol reply deliberately swallowed rather than turned into a key. Today
// that means the kitty keyboard flags reply, CSI ? <flags> u.
func Decode(seq []byte, cfg Config) (termmosaic.Event, int, Status) {
	return decode(seq, cfg, nil)
}

// decode is Decode with one addition: when flags is non-nil it receives the
// kitty keyboard flag set if seq is a CSI ? <flags> u reply.
//
// The pointer is the whole mechanism by which the negotiation reaches a Source,
// and it exists so there is exactly ONE implementation of the reply's meaning.
// A pure function cannot publish to a channel, and adding a field to Event to
// carry flags would grow a hot-path struct for a value produced once per
// session. A caller-supplied out-parameter keeps Decode total and keeps the
// rule in one place.
func decode(seq []byte, cfg Config, flags *uint8) (termmosaic.Event, int, Status) {
	if len(seq) == 0 {
		return termmosaic.Event{}, 0, StatusIncomplete
	}
	if seq[0] == escByte {
		return decodeEscape(seq, cfg, flags)
	}
	return decodeControl(seq, cfg)
}

// decodeEscape handles a sequence introduced by ESC.
//
// A bare ESC is StatusIncomplete, never KeyEscape: deciding that needs time and
// Decode has no clock. Everything else is either a known introducer or an
// Alt-chord, and an Alt-chord is resolved by decoding the remainder and OR-ing
// ModAlt onto the result, which keeps one implementation of every key form
// rather than two.
func decodeEscape(seq []byte, cfg Config, flags *uint8) (termmosaic.Event, int, Status) {
	if len(seq) == 1 {
		return termmosaic.Event{}, 0, StatusIncomplete
	}

	switch seq[1] {
	case '[':
		return decodeCSI(seq, cfg, flags)
	case 'O':
		return decodeSS3(seq)
	case ']':
		// OSC, terminated by BEL or ST (ESC \). The payloads that arrive
		// unbidden on the input stream are window titles, colour queries and
		// clipboard reads. None of them is input, and decoding a body as
		// keystrokes would type the terminal's own reply into the user.
		return consumeString(seq, true)
	case 'P', '_', '^', 'X':
		// DCS, SOS, PM and APC: string sequences with an ST terminator and no
		// BEL form. Consuming tmux's DCS passthrough wrapper here keeps the
		// stream in sync; this is NOT tmux passthrough support, which is
		// deferred by ADR 0005 and is a real gap.
		return consumeString(seq, false)
	case escByte:
		if len(seq) == 2 {
			// ESC ESC — not yet decidable as Alt-Escape.
			return termmosaic.Event{}, 0, StatusIncomplete
		}
		return altChord(seq, cfg)
	default:
		return altChord(seq, cfg)
	}
}

// altChord decodes the remainder of an ESC-prefixed sequence and ORs ModAlt
// onto it. consumed counts the ESC that introduced it.
func altChord(seq []byte, cfg Config) (termmosaic.Event, int, Status) {
	ev, n, st := decode(seq[1:], cfg, nil)
	if st == StatusIncomplete {
		return termmosaic.Event{}, 0, StatusIncomplete
	}
	if ev.Kind == termmosaic.EventKey {
		ev.Mod |= termmosaic.ModAlt
	}
	return ev, n + 1, st
}

// consumeString consumes an OSC, DCS, SOS, PM or APC sequence through its
// terminator and produces no event.
//
// It exists to keep the decoder in sync rather than to interpret the payload. A
// terminal answering a query we did not send, or a tmux DCS wrapper, would
// otherwise have its body decoded as a stream of keystrokes. The string-sequence
// forms have no 64-byte bound in the wild — a clipboard read is kilobytes — so
// they are bounded by the same limit as everything else rather than
// unboundedly, and a runaway is discarded rather than accumulated.
func consumeString(seq []byte, allowBEL bool) (termmosaic.Event, int, Status) {
	for i := 2; i < len(seq); i++ {
		if seq[i] == escByte && i+1 < len(seq) && seq[i+1] == '\\' {
			return termmosaic.Event{}, i + 2, StatusOK
		}
		if allowBEL && seq[i] == belByte {
			return termmosaic.Event{}, i + 1, StatusOK
		}
	}
	if len(seq) >= maxSequenceLength {
		return termmosaic.Event{}, maxSequenceLength, StatusInvalid
	}
	return termmosaic.Event{}, 0, StatusIncomplete
}

// decodeSS3 handles ESC O <final>, which is how F1-F4 and, on some terminals,
// Home, End and the arrows arrive in application cursor mode.
func decodeSS3(seq []byte) (termmosaic.Event, int, Status) {
	if len(seq) < 3 {
		return termmosaic.Event{}, 0, StatusIncomplete
	}
	if k, ok := ss3Key(seq[2]); ok {
		return keyEvent(k, 0), 3, StatusOK
	}
	// Not a key SS3 defines. On a terminal that does not use application
	// cursor mode this would have been an Alt-chord, so fall back to that
	// rather than discarding a keystroke.
	return altChord(seq, Config{})
}

// ss3Key maps an SS3 final byte to a key.
func ss3Key(b byte) (termmosaic.Key, bool) {
	switch b {
	case 'P':
		return termmosaic.KeyF1, true
	case 'Q':
		return termmosaic.KeyF2, true
	case 'R':
		return termmosaic.KeyF3, true
	case 'S':
		return termmosaic.KeyF4, true
	case 'A':
		return termmosaic.KeyUp, true
	case 'B':
		return termmosaic.KeyDown, true
	case 'C':
		return termmosaic.KeyRight, true
	case 'D':
		return termmosaic.KeyLeft, true
	case 'H':
		return termmosaic.KeyHome, true
	case 'F':
		return termmosaic.KeyEnd, true
	}
	return termmosaic.KeyNone, false
}

// keyEvent builds a non-printable key event with no rune.
func keyEvent(k termmosaic.Key, mod termmosaic.KeyMod) termmosaic.Event {
	return termmosaic.Event{Kind: termmosaic.EventKey, Key: k, Mod: mod}
}

// decodeControl handles a byte that is not ESC: C0 controls, DEL, ASCII
// printables and UTF-8.
//
// A C0 control with no Key of its own is reported as Ctrl plus the character it
// conventionally modifies, because that is what the terminal meant and it is
// what the kitty keyboard protocol reports too. An invalid byte run becomes
// U+FFFD and a key event rather than a swallowed sequence, matching
// ansi.AppendRune's existing invalid-rune policy — that rule is the named
// single-site change ADR 0005 section 7 defers to the day composition exists.
func decodeControl(seq []byte, cfg Config) (termmosaic.Event, int, Status) {
	b := seq[0]

	switch {
	case b < 0x20:
		return decodeC0(b, cfg)
	case b == delByte:
		if cfg.BackspaceByte == BackspaceControl {
			return termmosaic.Event{}, 1, StatusInvalid
		}
		return keyEvent(termmosaic.KeyBackspace, 0), 1, StatusOK
	case b < utf8.RuneSelf:
		return termmosaic.Event{Kind: termmosaic.EventKey, Rune: rune(b)}, 1, StatusOK
	}

	// A multi-byte rune. A truncated one is StatusIncomplete, which is how a
	// rune split across two read(2) calls becomes a return value rather than a
	// bug.
	if !utf8.FullRune(seq) {
		if len(seq) >= utf8.UTFMax {
			// Four bytes and still not a rune: the run is invalid, not
			// truncated.
			return termmosaic.Event{Kind: termmosaic.EventKey, Rune: delRune}, 1, StatusOK
		}
		return termmosaic.Event{}, 0, StatusIncomplete
	}
	r, size := utf8.DecodeRune(seq)
	if r == utf8.RuneError && size <= 1 {
		return termmosaic.Event{Kind: termmosaic.EventKey, Rune: delRune}, 1, StatusOK
	}
	return termmosaic.Event{Kind: termmosaic.EventKey, Rune: r}, size, StatusOK
}

// decodeC0 maps a C0 control byte to an event.
func decodeC0(b byte, cfg Config) (termmosaic.Event, int, Status) {
	switch b {
	case '\t':
		return keyEvent(termmosaic.KeyTab, 0), 1, StatusOK
	case '\r', '\n':
		// LF is Ctrl-J in a terminal's own encoding, but under raw mode every
		// terminal sends it as a bare newline for Enter, and treating it as
		// Enter is what makes Enter work on the ones that send LF.
		return keyEvent(termmosaic.KeyEnter, 0), 1, StatusOK
	case 0x08:
		if cfg.BackspaceByte == BackspaceDelete {
			return termmosaic.Event{}, 1, StatusInvalid
		}
		return keyEvent(termmosaic.KeyBackspace, 0), 1, StatusOK
	case 0x00:
		// NUL is Ctrl-Space (equivalently Ctrl-@), not an unprintable nothing.
		return termmosaic.Event{
			Kind: termmosaic.EventKey,
			Rune: ' ',
			Mod:  termmosaic.ModCtrl,
		}, 1, StatusOK
	}
	if b >= 0x01 && b <= 0x1a {
		return termmosaic.Event{
			Kind: termmosaic.EventKey,
			Rune: rune(b-0x01) + 'a',
			Mod:  termmosaic.ModCtrl,
		}, 1, StatusOK
	}
	if b >= 0x1c && b <= 0x1f {
		return termmosaic.Event{
			Kind: termmosaic.EventKey,
			Rune: rune(b) + 0x40, // Ctrl-\\, Ctrl-], Ctrl-^, Ctrl-_
			Mod:  termmosaic.ModCtrl,
		}, 1, StatusOK
	}
	// ESC is 0x1b and never reaches here. Anything else in C0 with no Key of
	// its own is reported as itself so that nothing is silently swallowed.
	return termmosaic.Event{Kind: termmosaic.EventKey, Rune: rune(b)}, 1, StatusOK
}
