package input

import "github.com/serkanalgur/termmosaic"

// Bounds on a CSI parameter list. They exist so the scan needs no allocation
// and no growth check: a parameter list longer than this, or a parameter
// numerically larger than maxParamValue, is not something a terminal sends and
// is treated as noise rather than buffered.
const (
	maxCSIParams  = 16 // widest parameter list accepted, sub-parameters included
	maxParamValue = 65535

	paramAbsent = -1 // value() for a parameter that was not present at all

	// kitty's keycodes for the keys xterm has no unambiguous encoding for, used
	// when disambiguate is negotiated.
	kittyTabKey       = 9  // Tab
	kittyEnterKey     = 13 // Enter
	kittyEscapeKey    = 27 // Escape, and the base of the backspace-with-mods group
	kittyBackspaceKey = 127

	// kitty encodes F1-F12 in a private-use block as well as in the xterm
	// forms, and F13-F35 in the block immediately after. F13-F35 are DEFERRED
	// by ADR 0005, so that block is recognised and refused.
	kittyF1First  = 57364
	kittyF13First = 57376
)

// csiParams is a parsed CSI parameter list.
//
// Every CSI form this decoder accepts is a handful of small numbers, so the
// whole list lives in fixed storage and the decoder allocates nothing on the key
// path. Numbers are kept flat with a flag recording whether each was preceded by
// a colon, which is enough to answer every question the forms ask: the value of
// top-level parameter i, how many sub-parameters it has, and the k-th of them.
// Returning a slice of the array instead would make it escape and heap-allocate.
type csiParams struct {
	// nNums is how many numbers were parsed.
	nNums int
	// nums holds every parameter value in order.
	nums [maxCSIParams]int
	// colon[i] reports whether nums[i] was preceded by ':' rather than by ';' or
	// by the start of the sequence.
	colon [maxCSIParams]bool
	// nTop is the number of top-level parameters, counting empty ones.
	nTop int
	// prefix is the private parameter byte 0x3c-0x3f ('<', '=', '>', '?') if
	// the sequence carried one, else 0.
	prefix byte
}

// push appends one number to the list.
func (p *csiParams) push(v int, colon bool) bool {
	if p.nNums >= maxCSIParams {
		return false
	}
	p.nums[p.nNums] = v
	p.colon[p.nNums] = colon
	p.nNums++
	if !colon {
		p.nTop++
	}
	return true
}

// topOf returns the top-level parameter index that nums[i] belongs to.
func (p *csiParams) topOf(i int) int {
	n := 0
	for k := 0; k <= i; k++ {
		if !p.colon[k] {
			n++
		}
	}
	return n - 1
}

// value returns top-level parameter i, or paramAbsent if it was not present.
//
// An omitted or empty parameter reads as 0, which is what the protocols mean by
// it; distinguishing an explicit 0 from an omitted one would buy nothing here
// because every form that omits a parameter means the same thing as a 0.
func (p *csiParams) value(i int) int {
	if i < 0 || i >= p.nTop {
		return paramAbsent
	}
	for k := 0; k < p.nNums; k++ {
		if p.topOf(k) == i {
			return p.nums[k]
		}
	}
	return 0
}

// subCount returns how many numbers top-level parameter i carries, counting the
// parameter itself. A colon-separated list reports its true length.
func (p *csiParams) subCount(i int) int {
	if i < 0 || i >= p.nTop {
		return 0
	}
	n := 0
	for k := 0; k < p.nNums; k++ {
		if p.topOf(k) == i {
			n++
		}
	}
	return n
}

// subValue returns sub-parameter k of top-level parameter i. k 0 is the
// parameter itself, which is how a modifier is read out of "57;5:3".
func (p *csiParams) subValue(i, k int) int {
	if i < 0 || i >= p.nTop {
		return 0
	}
	m := 0
	for j := 0; j < p.nNums; j++ {
		if p.topOf(j) != i {
			continue
		}
		if m == k {
			return p.nums[j]
		}
		m++
	}
	return 0
}

// scanCSI parses a CSI sequence beginning at seq[0], which must be ESC [.
//
// It returns the final byte, the number of bytes consumed, and a Status. It
// returns StatusIncomplete with n == 0 when seq holds a proper prefix, which is
// the case the whole design exists for. It returns StatusInvalid once the
// sequence has run past maxSequenceLength without a final byte, with n ==
// len(seq) so the caller discards the whole run: we do NOT hunt for a plausible
// introducer inside the runaway, because a 64-byte runaway is almost always a
// stream the terminal is not really sending and manufacturing an event from
// inside it is worse than dropping it.
func scanCSI(seq []byte, p *csiParams) (final byte, n int, st Status) {
	if len(seq) < 3 {
		return 0, 0, StatusIncomplete
	}
	i := 2
	if b := seq[i]; b >= 0x3c && b <= 0x3f {
		p.prefix = b
		i++
	}
	// sep is the separator that introduced the value currently being accumulated:
	// 0 for the first, ';' for a new top-level parameter, ':' for a
	// sub-parameter. The separator that FOLLOWS a value says nothing about how
	// that value is attached, which is the easy thing to get wrong here:
	// "27;5:3" is keycode 27 with a modifier list of [5, 3], not three
	// top-level parameters.
	value, sep, pushed := 0, byte(0), 0
	for ; i < len(seq); i++ {
		b := seq[i]
		switch {
		case b >= '0' && b <= '9':
			value = value*10 + int(b-'0')
			if value > maxParamValue {
				return 0, i + 1, StatusInvalid
			}
		case b == ';' || b == ':':
			if !p.push(value, sep == ':') {
				return 0, i + 1, StatusInvalid
			}
			pushed++
			sep = b
			value = 0
		case b >= 0x40 && b <= 0x7e:
			// ECMA-48 says an omitted parameter is the same as a zero, but for
			// this decoder "no parameters at all" is a DIFFERENT sequence from
			// "one empty parameter": CSI M is the legacy mouse form and CSI 1 M
			// is a modified cursor-position report. So an implicit parameter is
			// recorded only when the sequence actually had parameter bytes.
			if pushed > 0 || sep == ':' || value != 0 {
				p.push(value, sep == ':')
			}
			return b, i + 1, StatusOK
		case b < 0x20:
			// C0 controls are permitted inside a CSI and are executed without
			// affecting it; we skip them.
			continue
		default:
			return 0, i + 1, StatusInvalid
		}
		if i >= maxSequenceLength {
			// Discard the run we gave up on and hand the caller back the offset
			// just past it, so bytes AFTER the runaway that happen to be a good
			// sequence are still decoded. Discarding everything we were handed
			// would be the other reading of "resume after the discarded run",
			// and it would lose real keystrokes that merely followed some noise.
			return 0, maxSequenceLength, StatusInvalid
		}
	}
	return 0, 0, StatusIncomplete
}

// decodeCSI dispatches a complete CSI sequence.
func decodeCSI(seq []byte, cfg Config, flags *uint8) (termmosaic.Event, int, Status) {
	var p csiParams
	final, n, st := scanCSI(seq, &p)
	if st != StatusOK {
		return termmosaic.Event{}, n, st
	}

	// The kitty keyboard flags reply. Recognised, deliberately NOT emitted as a
	// key, and published once to whoever asked for it.
	if p.prefix == '?' && final == 'u' && p.nTop == 1 {
		if v := p.value(0); v > 0 && v <= 255 && flags != nil {
			*flags = uint8(v)
		}
		return termmosaic.Event{}, n, StatusOK
	}

	// Bracketed paste, whole, when it arrives whole.
	if start := pasteStartLen(seq); start > 0 {
		return decodePaste(seq, start, cfg)
	}

	// Mouse: SGR 1006 and urxvt 1015 share the '<' private prefix.
	if p.prefix == '<' {
		return decodeMouseParams(n, &p, final)
	}

	// X10 / normal mouse: CSI M Cb Cx Cy, three raw bytes.
	if final == 'M' && p.nTop == 0 && p.prefix == 0 {
		return decodeMouseX10(seq, n)
	}

	// Focus in and out, guarded on carrying no parameters because CSI O with
	// parameters is a different sequence entirely.
	if p.nTop == 0 && p.prefix == 0 {
		switch final {
		case 'I':
			return termmosaic.Event{Kind: termmosaic.EventFocus, Focused: true}, n, StatusOK
		case 'O':
			return termmosaic.Event{Kind: termmosaic.EventFocus, Focused: false}, n, StatusOK
		}
	}

	// The Linux console's ESC [ [ A-E form for F1-F5: the '[' is a final byte,
	// so the real final is the byte after it.
	if final == '[' {
		if len(seq) <= n {
			return termmosaic.Event{}, 0, StatusIncomplete
		}
		switch seq[n] {
		case 'A':
			return keyEvent(termmosaic.KeyF1, 0), n + 1, StatusOK
		case 'B':
			return keyEvent(termmosaic.KeyF2, 0), n + 1, StatusOK
		case 'C':
			return keyEvent(termmosaic.KeyF3, 0), n + 1, StatusOK
		case 'D':
			return keyEvent(termmosaic.KeyF4, 0), n + 1, StatusOK
		case 'E':
			return keyEvent(termmosaic.KeyF5, 0), n + 1, StatusOK
		}
		return swallow(n)
	}

	if final == '~' {
		return decodeTilde(&p, n)
	}

	if final == 'u' && p.prefix == 0 {
		return decodeKittyKey(&p, cfg, n)
	}

	// Everything else is a final byte with an optional legacy modifier, which is
	// how arrows, Home, End, F1-F4 and Backtab arrive.
	if k, ok := csiFinalKey(final); ok {
		return keyEvent(k, legacyMod(p.value(1))), n, StatusOK
	}

	// A well-formed CSI we have no meaning for. It is consumed rather than
	// discarded a byte at a time, so its body is never decoded as keystrokes,
	// and it is deliberately not turned into a key: a reply we do not understand
	// is not input.
	return swallow(n)
}

// swallow consumes a well-formed sequence the decoder deliberately produces no
// event for: an unimplemented key code, a suppressed key release, a terminal
// reply we do not understand.
//
// Swallowing rather than returning StatusInvalid matters: StatusInvalid means
// "these bytes were broken", and counting an unimplemented-but-valid sequence as
// corruption would send an application hunting for an input bug that does not
// exist. Consuming the whole run also stops its body being decoded as
// keystrokes.
func swallow(n int) (termmosaic.Event, int, Status) {
	return termmosaic.Event{}, n, StatusOK
}

// decodePaste decodes a bracketed paste whose start marker is at the front of
// seq and whose body begins at offset start.
//
// If the closing marker is in seq the paste is delivered as one event. If it is
// not, the result is StatusIncomplete with n == 0: a paste body is not a
// sequence and must not be bounded by maxSequenceLength, so the caller
// accumulates it rather than declaring the stream malformed. Parser owns that
// accumulation and it is incremental, because re-scanning the accumulated body
// on every chunk would be quadratic in the paste size.
//
// A payload over Config.MaxPasteBytes keeps its FIRST bytes, is flagged
// Truncated, and is still scanned to the closing marker. Abandoning it instead
// would leave us out of sync with the terminal for every subsequent byte, which
// is a far worse failure than a short paste.
func decodePaste(seq []byte, start int, cfg Config) (termmosaic.Event, int, Status) {
	body := seq[start:]
	stop := pasteEnd(body)
	if stop < 0 {
		return termmosaic.Event{}, 0, StatusIncomplete
	}
	payload := body[:stop]
	limit := cfg.maxPasteBytes()
	truncated := false
	if len(payload) > limit {
		payload = payload[:limit]
		truncated = true
	}
	return pasteEvent(payload, truncated), start + stop + len(pasteStop), StatusOK
}

// decodeTilde maps the CSI <n> [; <mod>] ~ family, which is where Delete,
// Insert, Home, End, PgUp, PgDn and F1-F12 live on most terminals.
func decodeTilde(p *csiParams, n int) (termmosaic.Event, int, Status) {
	mod := legacyMod(p.value(1))
	code := p.value(0)
	var k termmosaic.Key
	switch code {
	case 1, 7:
		k = termmosaic.KeyHome
	case 2:
		k = termmosaic.KeyInsert
	case 3:
		k = termmosaic.KeyDelete
	case 4, 8:
		k = termmosaic.KeyEnd
	case 5:
		k = termmosaic.KeyPageUp
	case 6:
		k = termmosaic.KeyPageDown
	case 11:
		k = termmosaic.KeyF1
	case 12:
		k = termmosaic.KeyF2
	case 13:
		k = termmosaic.KeyF3
	case 14:
		k = termmosaic.KeyF4
	case 15:
		k = termmosaic.KeyF5
	case 17:
		k = termmosaic.KeyF6
	case 18:
		k = termmosaic.KeyF7
	case 19:
		k = termmosaic.KeyF8
	case 20:
		k = termmosaic.KeyF9
	case 21:
		k = termmosaic.KeyF10
	case 23:
		k = termmosaic.KeyF11
	case 24:
		k = termmosaic.KeyF12
	default:
		// 200 and 201 are the bracketed-paste markers and are handled before
		// this point. Anything else is a well-formed tilde form we do not
		// implement: consumed and swallowed rather than counted as malformed,
		// because it is not a broken sequence, it is an unimplemented one.
		return swallow(n)
	}
	return keyEvent(k, mod), n, StatusOK
}

// csiFinalKey maps a CSI final byte to the key it names, for the forms that
// carry no numeric key code.
func csiFinalKey(b byte) (termmosaic.Key, bool) {
	switch b {
	case 'A':
		return termmosaic.KeyUp, true
	case 'B':
		return termmosaic.KeyDown, true
	case 'C':
		return termmosaic.KeyRight, true
	case 'D':
		return termmosaic.KeyLeft, true
	case 'E':
		// Keypad Begin. TermMosaic has no KeyBegin and Home is the closest
		// documented approximation; the alternative is discarding a keystroke.
		return termmosaic.KeyHome, true
	case 'F':
		return termmosaic.KeyEnd, true
	case 'H':
		return termmosaic.KeyHome, true
	case 'P':
		return termmosaic.KeyF1, true
	case 'Q':
		return termmosaic.KeyF2, true
	case 'R':
		return termmosaic.KeyF3, true
	case 'S':
		return termmosaic.KeyF4, true
	case 'Z':
		return termmosaic.KeyBacktab, true
	}
	return termmosaic.KeyNone, false
}

// decodeKittyKey maps the kitty keyboard protocol's CSI <code> [; <mod>]
// [; <type>] u form.
//
// The two modifier encodings are genuinely different and both are decoded: this
// is a bitmap in kitty's own layout, whereas CSI 1 ; <mod> <final> is a
// one-based legacy encoding in which meta and super are different keys. A
// terminal that ignores our kitty request uses the legacy one, and we cannot
// choose which we get.
//
// Kitty also has hyper (16), meta (32), caps-lock (64) and num-lock (128). They
// are decoded and DROPPED: KeyMod is four bits and widening it changes every
// Mod consumer. Dropping caps-lock is arguably wrong; it is harmless in practice
// and is an accepted limitation rather than something to engineer around.
func decodeKittyKey(p *csiParams, cfg Config, n int) (termmosaic.Event, int, Status) {
	code := p.value(0)
	if code == paramAbsent {
		return swallow(n)
	}

	// The modifier is normally top-level parameter 1, but the compatibility
	// forms put the event type beside it as a sub-parameter ("57;5:3").
	mod := kittyMod(p.subValue(1, 0))
	typ := keyTypeOf(p.value(2))
	if p.subCount(1) > 1 {
		typ = keyTypeOf(p.subValue(1, 1))
	}

	var k termmosaic.Key
	var r rune
	switch {
	case code >= 32 && code < 127:
		r = rune(code)
	case code >= kittyF1First && code < kittyF13First:
		k = kittyFunctional(code)
	case code == kittyTabKey:
		k = termmosaic.KeyTab
	case code == kittyEnterKey:
		k = termmosaic.KeyEnter
	case code == kittyEscapeKey, code == kittyBackspaceKey:
		// kitty reports both Escape and Backspace in the low codes: 27 with a
		// modifier is Backspace, and a bare 27 is Escape. Without the
		// modifier we cannot tell them apart, and Backspace is the far more
		// frequent key, so a bare 27 is Backspace. A bare ESC byte still
		// decodes as Escape via Parser's escape delay, so nothing is lost.
		k = termmosaic.KeyBackspace
	default:
		// F13-F35, and anything else we do not know. Recognised and refused
		// rather than mapped to a wrong key.
		return swallow(n)
	}

	ev := keyEvent(k, mod)
	if r != 0 {
		ev.Rune = r
	}
	ev.Type = typ
	if ev.Type == termmosaic.KeyRelease && !cfg.KittyReportReleases {
		// Decoded and deliberately not emitted: nothing in the widget catalog
		// consumes a key release. Swallowed, not counted as malformed.
		return swallow(n)
	}
	return ev, n, StatusOK
}

// kittyFunctional maps kitty's private-use functional key block onto F1-F12.
// The block starts at F13 and is not produced: those keys are deferred by
// ADR 0005, and adding them in the middle of the Key iota block would silently
// renumber every later constant.
func kittyFunctional(code int) termmosaic.Key {
	keys := [...]termmosaic.Key{
		termmosaic.KeyF1, termmosaic.KeyF2, termmosaic.KeyF3, termmosaic.KeyF4,
		termmosaic.KeyF5, termmosaic.KeyF6, termmosaic.KeyF7, termmosaic.KeyF8,
		termmosaic.KeyF9, termmosaic.KeyF10, termmosaic.KeyF11, termmosaic.KeyF12,
	}
	i := code - kittyF1First
	if i < 0 || i >= len(keys) {
		return termmosaic.KeyNone
	}
	return keys[i]
}

// legacyMod decodes the xterm modifier parameter, where the value is one plus a
// bitmask of shift (1), alt (2), ctrl (4) and meta (8).
//
// The meta bit is DROPPED rather than mapped to ModSuper: in xterm's encoding
// meta and super are different keys, and KeyMod's fourth bit is super, which
// only the kitty encoding reports. A terminal that sends xterm meta therefore
// loses that distinction, which is the accepted limitation of the four-bit
// KeyMod recorded by ADR 0005.
func legacyMod(v int) termmosaic.KeyMod {
	if v == paramAbsent || v < 2 {
		return 0
	}
	b := v - 1
	var m termmosaic.KeyMod
	if b&1 != 0 {
		m |= termmosaic.ModShift
	}
	if b&2 != 0 {
		m |= termmosaic.ModAlt
	}
	if b&4 != 0 {
		m |= termmosaic.ModCtrl
	}
	return m
}

// kittyMod decodes a kitty modifier bitmap: shift (1), alt (2), ctrl (4),
// super (8), then hyper (16), meta (32), caps-lock (64) and num-lock (128).
// The last four are decoded and dropped; see decodeKittyKey.
func kittyMod(v int) termmosaic.KeyMod {
	var m termmosaic.KeyMod
	if v&1 != 0 {
		m |= termmosaic.ModShift
	}
	if v&2 != 0 {
		m |= termmosaic.ModAlt
	}
	if v&4 != 0 {
		m |= termmosaic.ModCtrl
	}
	if v&8 != 0 {
		m |= termmosaic.ModSuper
	}
	return m
}

// keyTypeOf maps a kitty event-type number to a KeyType. Anything unrecognised
// is a press, which is the correct default: a press is what every terminal
// without the protocol produces, and inventing a repeat or a release would be
// worse than reporting a press.
func keyTypeOf(v int) termmosaic.KeyType {
	switch v {
	case 2:
		return termmosaic.KeyRepeat
	case 3:
		return termmosaic.KeyRelease
	default:
		return termmosaic.KeyPress
	}
}
