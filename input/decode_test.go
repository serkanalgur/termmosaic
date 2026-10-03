package input

import (
	"strings"
	"testing"

	"github.com/serkanalgur/termmosaic"
)

// decodeCase is one row of the decoder's table.
//
// The table is the deliverable ADR 0005 is really about: every input bug is a
// function of the bytes, so the whole decoder's surface fits in a literal, and a
// case that needs a pipe and a goroutine to express is a case the design got
// wrong.
type decodeCase struct {
	name string
	seq  string
	cfg  Config
	want termmosaic.Event
	// n is the number of bytes Decode must consume. Zero means the whole
	// sequence, which is what almost every row is; a row that puts two sequences
	// in one string says so explicitly, and nWantsNone marks the StatusIncomplete
	// rows, whose contract is that nothing is consumed at all.
	n  int
	st Status
}

// nWantsNone marks a StatusIncomplete row, where Decode must consume nothing.
const nWantsNone = -1

// cfgReleases reports key releases, which are suppressed by default.
var cfgReleases = Config{KittyReportReleases: true}

func key(k termmosaic.Key, mod termmosaic.KeyMod) termmosaic.Event {
	return termmosaic.Event{Kind: termmosaic.EventKey, Key: k, Mod: mod}
}

func runeKey(r rune, mod termmosaic.KeyMod) termmosaic.Event {
	return termmosaic.Event{Kind: termmosaic.EventKey, Rune: r, Mod: mod}
}

func mouse(x, y int, b termmosaic.MouseButton, a termmosaic.MouseAction, m termmosaic.KeyMod) termmosaic.Event {
	return termmosaic.Event{
		Kind:  termmosaic.EventMouse,
		Mouse: termmosaic.Mouse{X: x, Y: y, Button: b, Action: a, Mod: m},
	}
}

// runDecodeCases drives the shared table.
func runDecodeCases(t *testing.T, cases []decodeCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wantN := tc.n
			switch wantN {
			case 0:
				wantN = len(tc.seq)
			case nWantsNone:
				wantN = 0
			}
			got, n, st := Decode([]byte(tc.seq), tc.cfg)
			if st != tc.st {
				t.Fatalf("status = %v, want %v (event %+v)", st, tc.st, got)
			}
			if n != wantN {
				t.Fatalf("consumed %d bytes, want %d", n, wantN)
			}
			if got != tc.want {
				t.Fatalf("event = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestDecodePlainAndControlCharacters(t *testing.T) {
	runDecodeCases(t, []decodeCase{
		{"ascii letter", "a", Config{}, runeKey('a', 0), 0, StatusOK},
		{"ascii digit", "7", Config{}, runeKey('7', 0), 0, StatusOK},
		{"space", " ", Config{}, runeKey(' ', 0), 0, StatusOK},
		{"tilde", "~", Config{}, runeKey('~', 0), 0, StatusOK},
		{"nul is ctrl-space", "\x00", Config{}, runeKey(' ', termmosaic.ModCtrl), 0, StatusOK},
		{"ctrl-a", "\x01", Config{}, runeKey('a', termmosaic.ModCtrl), 0, StatusOK},
		{"ctrl-c", "\x03", Config{}, runeKey('c', termmosaic.ModCtrl), 0, StatusOK},
		{"ctrl-z", "\x1a", Config{}, runeKey('z', termmosaic.ModCtrl), 0, StatusOK},
		{"ctrl-backslash", "\x1c", Config{}, runeKey('\\', termmosaic.ModCtrl), 0, StatusOK},
		{"ctrl-underscore", "\x1f", Config{}, runeKey('_', termmosaic.ModCtrl), 0, StatusOK},
		{"tab", "\t", Config{}, key(termmosaic.KeyTab, 0), 0, StatusOK},
		{"enter is CR", "\r", Config{}, key(termmosaic.KeyEnter, 0), 0, StatusOK},
		{"enter is LF", "\n", Config{}, key(termmosaic.KeyEnter, 0), 0, StatusOK},
		{"backspace control byte", "\x08", Config{}, key(termmosaic.KeyBackspace, 0), 0, StatusOK},
		{"backspace delete byte", "\x7f", Config{}, key(termmosaic.KeyBackspace, 0), 0, StatusOK},
	})
}

func TestDecodeBackspaceBytePinning(t *testing.T) {
	runDecodeCases(t, []decodeCase{
		{
			"delete pinned rejects BS",
			"\x08", Config{BackspaceByte: BackspaceDelete},
			termmosaic.Event{}, 0, StatusInvalid,
		},
		{
			"control pinned rejects DEL",
			"\x7f", Config{BackspaceByte: BackspaceControl},
			termmosaic.Event{}, 0, StatusInvalid,
		},
		{
			"control pinned accepts BS",
			"\x08", Config{BackspaceByte: BackspaceControl},
			key(termmosaic.KeyBackspace, 0), 0, StatusOK,
		},
		{
			"delete pinned accepts DEL",
			"\x7f", Config{BackspaceByte: BackspaceDelete},
			key(termmosaic.KeyBackspace, 0), 0, StatusOK,
		},
	})
}

func TestDecodeUTF8(t *testing.T) {
	runDecodeCases(t, []decodeCase{
		{"two byte rune", "é", Config{}, runeKey('é', 0), 0, StatusOK},
		{"three byte rune", "▸", Config{}, runeKey('▸', 0), 0, StatusOK},
		{"four byte rune", "\U0001F600", Config{}, runeKey('\U0001F600', 0), 0, StatusOK},
		{
			// The case the whole StatusIncomplete contract exists for: a rune
			// split across two read(2) calls.
			"two byte rune split across reads",
			"\xc3", Config{},
			termmosaic.Event{}, nWantsNone, StatusIncomplete,
		},
		{
			"three byte rune split to two bytes",
			"\xe2\x96", Config{},
			termmosaic.Event{}, nWantsNone, StatusIncomplete,
		},
		{
			"invalid byte becomes U+FFFD",
			"\xff", Config{},
			runeKey('�', 0), 0, StatusOK,
		},
		{
			// Four bytes that cannot become a rune must not be held forever
			// waiting for a fifth.
			"undecodable four byte run",
			"\xf0\x28\x8c\x28", Config{},
			runeKey('�', 0), 1, StatusOK,
		},
	})
}

func TestDecodeArrowsAndNavigation(t *testing.T) {
	runDecodeCases(t, []decodeCase{
		{"up", "\x1b[A", Config{}, key(termmosaic.KeyUp, 0), 0, StatusOK},
		{"down", "\x1b[B", Config{}, key(termmosaic.KeyDown, 0), 0, StatusOK},
		{"right", "\x1b[C", Config{}, key(termmosaic.KeyRight, 0), 0, StatusOK},
		{"left", "\x1b[D", Config{}, key(termmosaic.KeyLeft, 0), 0, StatusOK},
		{"home as H", "\x1b[H", Config{}, key(termmosaic.KeyHome, 0), 0, StatusOK},
		{"home as 1~", "\x1b[1~", Config{}, key(termmosaic.KeyHome, 0), 0, StatusOK},
		{"home as 7~", "\x1b[7~", Config{}, key(termmosaic.KeyHome, 0), 0, StatusOK},
		{"end as F", "\x1b[F", Config{}, key(termmosaic.KeyEnd, 0), 0, StatusOK},
		{"end as 4~", "\x1b[4~", Config{}, key(termmosaic.KeyEnd, 0), 0, StatusOK},
		{"end as 8~", "\x1b[8~", Config{}, key(termmosaic.KeyEnd, 0), 0, StatusOK},
		{"insert", "\x1b[2~", Config{}, key(termmosaic.KeyInsert, 0), 0, StatusOK},
		{"delete", "\x1b[3~", Config{}, key(termmosaic.KeyDelete, 0), 0, StatusOK},
		{"pgup", "\x1b[5~", Config{}, key(termmosaic.KeyPageUp, 0), 0, StatusOK},
		{"pgdn", "\x1b[6~", Config{}, key(termmosaic.KeyPageDown, 0), 0, StatusOK},
		{"backtab", "\x1b[Z", Config{}, key(termmosaic.KeyBacktab, 0), 0, StatusOK},
	})
}

func TestDecodeLegacyModifiers(t *testing.T) {
	runDecodeCases(t, []decodeCase{
		{"ctrl-up", "\x1b[1;5A", Config{}, key(termmosaic.KeyUp, termmosaic.ModCtrl), 0, StatusOK},
		{"shift-down", "\x1b[1;2B", Config{}, key(termmosaic.KeyDown, termmosaic.ModShift), 0, StatusOK},
		{"alt-right", "\x1b[1;3C", Config{}, key(termmosaic.KeyRight, termmosaic.ModAlt), 0, StatusOK},
		{"shift-alt-left", "\x1b[1;4D", Config{}, key(termmosaic.KeyLeft, termmosaic.ModShift|termmosaic.ModAlt), 0, StatusOK},
		{"ctrl-alt-delete", "\x1b[3;7~", Config{}, key(termmosaic.KeyDelete, termmosaic.ModCtrl|termmosaic.ModAlt), 0, StatusOK},
		// The legacy meta bit (8) is decoded and dropped rather than mapped to
		// ModSuper, because in xterm's encoding meta and super are different
		// keys and KeyMod's fourth bit is super.
		{"meta is dropped", "\x1b[1;9A", Config{}, key(termmosaic.KeyUp, 0), 0, StatusOK},
		{"no modifier", "\x1b[1;1A", Config{}, key(termmosaic.KeyUp, 0), 0, StatusOK},
		{"shift-F3 by legacy param", "\x1b[1;2R", Config{}, key(termmosaic.KeyF3, termmosaic.ModShift), 0, StatusOK},
	})
}

func TestDecodeFunctionKeys(t *testing.T) {
	runDecodeCases(t, []decodeCase{
		{"SS3 F1", "\x1bOP", Config{}, key(termmosaic.KeyF1, 0), 0, StatusOK},
		{"SS3 F4", "\x1bOS", Config{}, key(termmosaic.KeyF4, 0), 0, StatusOK},
		{"SS3 up", "\x1bOA", Config{}, key(termmosaic.KeyUp, 0), 0, StatusOK},
		{"SS3 home", "\x1bOH", Config{}, key(termmosaic.KeyHome, 0), 0, StatusOK},
		{"CSI P is F1", "\x1b[P", Config{}, key(termmosaic.KeyF1, 0), 0, StatusOK},
		{"CSI S is F4", "\x1b[S", Config{}, key(termmosaic.KeyF4, 0), 0, StatusOK},
		{"tilde F1", "\x1b[11~", Config{}, key(termmosaic.KeyF1, 0), 0, StatusOK},
		{"tilde F5", "\x1b[15~", Config{}, key(termmosaic.KeyF5, 0), 0, StatusOK},
		{"tilde F6", "\x1b[17~", Config{}, key(termmosaic.KeyF6, 0), 0, StatusOK},
		{"tilde F10", "\x1b[21~", Config{}, key(termmosaic.KeyF10, 0), 0, StatusOK},
		{"tilde F11", "\x1b[23~", Config{}, key(termmosaic.KeyF11, 0), 0, StatusOK},
		{"tilde F12", "\x1b[24~", Config{}, key(termmosaic.KeyF12, 0), 0, StatusOK},
		{"linux console F1", "\x1b[[A", Config{}, key(termmosaic.KeyF1, 0), 0, StatusOK},
		{"linux console F5", "\x1b[[E", Config{}, key(termmosaic.KeyF5, 0), 0, StatusOK},
	})
}

func TestDecodeEscapeChords(t *testing.T) {
	runDecodeCases(t, []decodeCase{
		{"alt-a", "\x1ba", Config{}, runeKey('a', termmosaic.ModAlt), 0, StatusOK},
		{"alt-A", "\x1bA", Config{}, runeKey('A', termmosaic.ModAlt), 0, StatusOK},
		{"alt-enter", "\x1b\r", Config{}, key(termmosaic.KeyEnter, termmosaic.ModAlt), 0, StatusOK},
		{"alt-ctrl-a", "\x1b\x01", Config{}, runeKey('a', termmosaic.ModAlt|termmosaic.ModCtrl), 0, StatusOK},
		{"alt-rune", "\x1bé", Config{}, runeKey('é', termmosaic.ModAlt), 0, StatusOK},
		{"ESC ESC is undecided", "\x1b\x1b", Config{}, termmosaic.Event{}, nWantsNone, StatusIncomplete},
		{"ESC ESC then a letter is one chord", "\x1b\x1ba", Config{}, runeKey('a', termmosaic.ModAlt), 0, StatusOK},
	})
}

func TestDecodeBareEscapeIsNeverAKey(t *testing.T) {
	// Decode has no clock, so it cannot decide that a lone ESC is the Escape
	// key. It says so, and Parser waits. If this ever changed, KeyEscape would
	// start arriving one byte early and every Alt-chord would break.
	prefixes := []string{
		"\x1b", "\x1b[", "\x1b[1", "\x1b[1;", "\x1b[1;5", "\x1b[3~"[0:3],
		"\x1bO", "\x1bOP"[0:2], "\x1b[[", "\x1b[[A"[0:3], "\x1b[<", "\x1b[<0;1",
		"\x1b[<0;1;1"[0:6], "\x1b[I"[0:2], "\x1b[?", "\x1b[?1", "\x1b[97", "\x1b[97;5",
		"\x1b]", "\x1b]0;t", "\x1bPtmux", "\x1b[200~body", "\x1b[201",
	}
	for _, seq := range prefixes {
		ev, n, st := Decode([]byte(seq), Config{})
		if st != StatusIncomplete {
			t.Errorf("Decode(%q) status = %v, want StatusIncomplete", seq, st)
		}
		if n != 0 {
			t.Errorf("Decode(%q) consumed %d bytes, want 0", seq, n)
		}
		if ev.Kind != termmosaic.EventNone {
			t.Errorf("Decode(%q) produced an event, want none", seq)
		}
	}
}

func TestDecodeIncompleteForEverySequenceType(t *testing.T) {
	// Every prefix of a representative sequence is incomplete, which is what
	// makes a sequence split across two reads a return value. This is the single
	// most valuable test in the subsystem, and it is a loop rather than a timing
	// test precisely because Decode is pure.
	full := []string{
		"\x1b[A",
		"\x1b[1;5A",
		"\x1b[3~",
		"\x1bOP",
		"\x1b[[A",
		"\x1b[I",
		"\x1b[<0;12;34M",
		"\x1b[M\x20\x21\x22",
		"\x1b[?1u",
		"\x1b[97u",
		"\x1b]0;title\x07",
		"\x1b[200~body\x1b[201~",
	}
	for _, seq := range full {
		for i := 1; i < len(seq); i++ {
			prefix := seq[:i]
			ev, n, st := Decode([]byte(prefix), Config{})
			if st == StatusIncomplete {
				if n != 0 {
					t.Errorf("Decode(%q) incomplete but consumed %d bytes", prefix, n)
				}
				if ev.Kind != termmosaic.EventNone {
					t.Errorf("Decode(%q) incomplete but produced an event", prefix)
				}
				continue
			}
			t.Errorf("Decode(%q) status = %v, want StatusIncomplete", prefix, st)
		}
		// And the whole thing must still decode.
		if _, n, st := Decode([]byte(seq), Config{}); st == StatusIncomplete || n != len(seq) {
			t.Errorf("Decode(%q) on the whole sequence: n=%d st=%v", seq, n, st)
		}
	}
}

func TestDecodeKittyKeyboard(t *testing.T) {
	runDecodeCases(t, []decodeCase{
		{"letter keycode", "\x1b[97u", Config{}, runeKey('a', 0), 0, StatusOK},
		{"shift-ctrl letter", "\x1b[97;5u", Config{}, runeKey('a', termmosaic.ModShift|termmosaic.ModCtrl), 0, StatusOK},
		{"alt-ctrl letter", "\x1b[97;6u", Config{}, runeKey('a', termmosaic.ModCtrl|termmosaic.ModAlt), 0, StatusOK},
		{"super letter", "\x1b[97;8u", Config{}, runeKey('a', termmosaic.ModSuper), 0, StatusOK},
		{"caps-lock bit is dropped", "\x1b[97;65u", Config{}, runeKey('a', termmosaic.ModShift), 0, StatusOK},
		{"hyper bit is dropped", "\x1b[97;17u", Config{}, runeKey('a', termmosaic.ModShift), 0, StatusOK},
		{"num-lock bit is dropped", "\x1b[97;129u", Config{}, runeKey('a', termmosaic.ModShift), 0, StatusOK},
		{
			"repeat",
			"\x1b[97;1;2u", Config{},
			termmosaic.Event{Kind: termmosaic.EventKey, Rune: 'a', Mod: termmosaic.ModShift, Type: termmosaic.KeyRepeat},
			0, StatusOK,
		},
		{"release suppressed by default", "\x1b[97;1;3u", Config{}, termmosaic.Event{}, 0, StatusOK},
		{
			"release reported when asked",
			"\x1b[97;1;3u", cfgReleases,
			termmosaic.Event{Kind: termmosaic.EventKey, Rune: 'a', Mod: termmosaic.ModShift, Type: termmosaic.KeyRelease},
			0, StatusOK,
		},
		{"functional key block F1", "\x1b[57364u", Config{}, key(termmosaic.KeyF1, 0), 0, StatusOK},
		{"functional key block F12", "\x1b[57375u", Config{}, key(termmosaic.KeyF12, 0), 0, StatusOK},
		{"shift-ctrl backspace", "\x1b[27;5u", Config{}, key(termmosaic.KeyBackspace, termmosaic.ModShift|termmosaic.ModCtrl), 0, StatusOK},
		{"shift-ctrl enter", "\x1b[13;5u", Config{}, key(termmosaic.KeyEnter, termmosaic.ModShift|termmosaic.ModCtrl), 0, StatusOK},
		{"shift-ctrl tab", "\x1b[9;5u", Config{}, key(termmosaic.KeyTab, termmosaic.ModShift|termmosaic.ModCtrl), 0, StatusOK},
		{
			// The compatibility form puts the event type beside the modifier as
			// a sub-parameter, so "27;5:3" is Backspace with ctrl, released.
			"compat form with sub-parameters",
			"\x1b[27;5:3u", cfgReleases,
			termmosaic.Event{
				Kind: termmosaic.EventKey, Key: termmosaic.KeyBackspace,
				Mod: termmosaic.ModShift | termmosaic.ModCtrl, Type: termmosaic.KeyRelease,
			},
			0, StatusOK,
		},
		{
			// F13 and above are deferred, so the block is recognised and
			// refused rather than mapped onto a wrong key.
			"F13 is deferred",
			"\x1b[57376u", Config{},
			termmosaic.Event{}, 0, StatusOK,
		},
		{
			"F35 is deferred",
			"\x1b[57398u", Config{},
			termmosaic.Event{}, 0, StatusOK,
		},
	})
}

func TestDecodeKittyFlagsReplyIsSwallowed(t *testing.T) {
	for _, seq := range []string{"\x1b[?0u", "\x1b[?1u", "\x1b[?31u", "\x1b[?255u"} {
		ev, n, st := Decode([]byte(seq), Config{})
		if st != StatusOK {
			t.Errorf("Decode(%q) status = %v, want StatusOK", seq, st)
		}
		if n != len(seq) {
			t.Errorf("Decode(%q) consumed %d, want %d", seq, n, len(seq))
		}
		if ev.Kind != termmosaic.EventNone {
			t.Errorf("Decode(%q) produced %v, want no event: a terminal's answer to our query is not a key", seq, ev.Kind)
		}
	}
}

func TestDecodeKittyFlagsReplyIsCapturedThroughTheOutParameter(t *testing.T) {
	var flags uint8
	if _, _, st := decode([]byte("\x1b[?31u"), Config{}, &flags); st != StatusOK {
		t.Fatalf("status = %v", st)
	}
	if flags != 31 {
		t.Errorf("flags = %d, want 31", flags)
	}
	// A nil out-parameter must stay valid, because Decode itself passes one.
	if _, _, st := Decode([]byte("\x1b[?31u"), Config{}); st != StatusOK {
		t.Errorf("Decode with nil flags pointer returned %v", st)
	}
}

func TestDecodeFocus(t *testing.T) {
	runDecodeCases(t, []decodeCase{
		{"focus in", "\x1b[I", Config{}, termmosaic.Event{Kind: termmosaic.EventFocus, Focused: true}, 0, StatusOK},
		{"focus out", "\x1b[O", Config{}, termmosaic.Event{Kind: termmosaic.EventFocus, Focused: false}, 0, StatusOK},
	})
}

func TestDecodeMouseSGR1006(t *testing.T) {
	runDecodeCases(t, []decodeCase{
		{"left press", "\x1b[<0;1;1M", Config{}, mouse(0, 0, termmosaic.MouseLeft, termmosaic.MousePress, 0), 0, StatusOK},
		{"middle press", "\x1b[<1;10;5M", Config{}, mouse(9, 4, termmosaic.MouseMiddle, termmosaic.MousePress, 0), 0, StatusOK},
		{"right press", "\x1b[<2;3;4M", Config{}, mouse(2, 3, termmosaic.MouseRight, termmosaic.MousePress, 0), 0, StatusOK},
		{"left release via lowercase m", "\x1b[<0;1;1m", Config{}, mouse(0, 0, termmosaic.MouseLeft, termmosaic.MouseRelease, 0), 0, StatusOK},
		{"shift-modified press", "\x1b[<4;7;8M", Config{}, mouse(6, 7, termmosaic.MouseLeft, termmosaic.MousePress, termmosaic.ModShift), 0, StatusOK},
		{"shift-alt press", "\x1b[<12;1;1M", Config{}, mouse(0, 0, termmosaic.MouseLeft, termmosaic.MousePress, termmosaic.ModShift|termmosaic.ModAlt), 0, StatusOK},
		{"ctrl press", "\x1b[<16;1;1M", Config{}, mouse(0, 0, termmosaic.MouseLeft, termmosaic.MousePress, termmosaic.ModCtrl), 0, StatusOK},
		{"wheel up", "\x1b[<64;3;4M", Config{}, mouse(2, 3, termmosaic.MouseWheelUp, termmosaic.MousePress, 0), 0, StatusOK},
		{"wheel down", "\x1b[<65;3;4M", Config{}, mouse(2, 3, termmosaic.MouseWheelDown, termmosaic.MousePress, 0), 0, StatusOK},
		{"motion with no button", "\x1b[<35;9;9M", Config{}, mouse(8, 8, termmosaic.MouseNone, termmosaic.MouseMove, 0), 0, StatusOK},
		{"motion with left held is a drag", "\x1b[<32;9;9M", Config{}, mouse(8, 8, termmosaic.MouseLeft, termmosaic.MouseDrag, 0), 0, StatusOK},
	})
}

func TestDecodeMouseUrxvt1015(t *testing.T) {
	// 1015 is the same '<' prefix with an uppercase final and no way to report a
	// release. It must still decode, because a terminal that ignores our 1006
	// request emits exactly this and unparseable mouse bytes are worse than no
	// mouse at all.
	runDecodeCases(t, []decodeCase{
		{"urxvt 1015 press", "\x1b[<0;11;22M", Config{}, mouse(10, 21, termmosaic.MouseLeft, termmosaic.MousePress, 0), 0, StatusOK},
	})
}

func TestDecodeMouseX10(t *testing.T) {
	// CSI M then three bytes, each with the +32 offset removed. X10 has no
	// motion bit set by these values, so they are all presses.
	runDecodeCases(t, []decodeCase{
		{"left press", "\x1b[M\x20\x21\x22", Config{}, mouse(1, 2, termmosaic.MouseLeft, termmosaic.MousePress, 0), 0, StatusOK},
		{"middle press", "\x1b[M\x21\x25\x28", Config{}, mouse(5, 8, termmosaic.MouseMiddle, termmosaic.MousePress, 0), 0, StatusOK},
		{"right press", "\x1b[M\x22\x25\x28", Config{}, mouse(5, 8, termmosaic.MouseRight, termmosaic.MousePress, 0), 0, StatusOK},
		{"button 3 is a release", "\x1b[M\x23\x25\x28", Config{}, mouse(5, 8, termmosaic.MouseNone, termmosaic.MouseRelease, 0), 0, StatusOK},
		{"wheel up", "\x1b[M\x60\x25\x28", Config{}, mouse(5, 8, termmosaic.MouseWheelUp, termmosaic.MousePress, 0), 0, StatusOK},
		{"wheel down", "\x1b[M\x61\x25\x28", Config{}, mouse(5, 8, termmosaic.MouseWheelDown, termmosaic.MousePress, 0), 0, StatusOK},
		{"motion with left held is a drag", "\x1b[M\x20\x25\x28", Config{}, mouse(5, 8, termmosaic.MouseLeft, termmosaic.MousePress, 0), 0, StatusOK},
		{
			// The X10 motion bit is 32, so 0x20 + 32 is a drag and 0x20 alone is
			// a press. Getting this backwards turns every click into a drag.
			"motion bit makes a drag",
			"\x1b[M\x40\x25\x28", Config{},
			mouse(5, 8, termmosaic.MouseLeft, termmosaic.MouseDrag, 0), 0, StatusOK,
		},
	})
}

func TestDecodePasteIsOneEvent(t *testing.T) {
	runDecodeCases(t, []decodeCase{
		{
			"whole paste",
			"\x1b[200~hello\x1b[201~", Config{},
			termmosaic.Event{Kind: termmosaic.EventPaste, Text: "hello"},
			0, StatusOK,
		},
		{
			"empty paste",
			"\x1b[200~\x1b[201~", Config{},
			termmosaic.Event{Kind: termmosaic.EventPaste, Text: ""},
			0, StatusOK,
		},
		{
			// Text is the payload as-is: no unescaping, no newline
			// normalisation, no trimming. Terminals send literal bytes between
			// the markers and we do not improve on them.
			"paste carries control bytes verbatim",
			"\x1b[200~a\r\n\tb\x1b[201~", Config{},
			termmosaic.Event{Kind: termmosaic.EventPaste, Text: "a\r\n\tb"},
			0, StatusOK,
		},
		{
			"paste at the cap is not truncated",
			"\x1b[200~abcde\x1b[201~", Config{MaxPasteBytes: 5},
			termmosaic.Event{Kind: termmosaic.EventPaste, Text: "abcde"},
			0, StatusOK,
		},
		{
			"paste over the cap keeps the first bytes, is flagged, and is still consumed to the end",
			"\x1b[200~abcdefgh\x1b[201~", Config{MaxPasteBytes: 5},
			termmosaic.Event{Kind: termmosaic.EventPaste, Text: "abcde", Truncated: true},
			0, StatusOK,
		},
		{
			"open paste is incomplete",
			"\x1b[200~abc", Config{},
			termmosaic.Event{}, nWantsNone, StatusIncomplete,
		},
		{
			"start marker alone is incomplete",
			"\x1b[200~", Config{},
			termmosaic.Event{}, nWantsNone, StatusIncomplete,
		},
	})
}

func TestDecodePasteNeverBecomesKeyEvents(t *testing.T) {
	// The decision ADR 0005 §4 makes is not an optimisation. A 10,000-character
	// paste delivered as 10,000 key events would run Handle 10,000 times, push
	// 10,000 undo entries and run every onChange callback 10,000 times. This
	// asserts one call produces one event no matter how big the paste is.
	body := strings.Repeat("x", 5000)
	seq := "\x1b[200~" + body + "\x1b[201~"
	ev, n, st := Decode([]byte(seq), Config{})
	if st != StatusOK {
		t.Fatalf("status = %v", st)
	}
	if n != len(seq) {
		t.Fatalf("consumed %d, want %d", n, len(seq))
	}
	if ev.Kind != termmosaic.EventPaste {
		t.Fatalf("kind = %v, want EventPaste", ev.Kind)
	}
	if ev.Text != body {
		t.Fatalf("payload length = %d, want %d", len(ev.Text), len(body))
	}
	if ev.Truncated {
		t.Error("a 5000-byte paste is under the 4 MiB default and must not be truncated")
	}
}

func TestDecodeConsumesOneSequenceOnly(t *testing.T) {
	// The central contract: Decode decodes the FRONT of seq and says how much it
	// used, so a caller can loop. Every form has to get this right or a pasted
	// batch of text would lose its tail.
	runDecodeCases(t, []decodeCase{
		{"two letters", "ab", Config{}, runeKey('a', 0), 1, StatusOK},
		{"letter then arrow", "a\x1b[A", Config{}, runeKey('a', 0), 1, StatusOK},
		{"arrow then letter", "\x1b[Ab", Config{}, key(termmosaic.KeyUp, 0), 3, StatusOK},
		{"two arrows", "\x1b[A\x1b[B", Config{}, key(termmosaic.KeyUp, 0), 3, StatusOK},
		{"paste then letter", "\x1b[200~x\x1b[201~a", Config{}, termmosaic.Event{Kind: termmosaic.EventPaste, Text: "x"}, 13, StatusOK},
		{"mouse then key", "\x1b[<0;1;1Ma", Config{}, mouse(0, 0, termmosaic.MouseLeft, termmosaic.MousePress, 0), 9, StatusOK},
		{"OSC then letter", "\x1b]0;title\x07a", Config{}, termmosaic.Event{}, 10, StatusOK},
	})
}

func TestDecodeStringSequencesAreSwallowedNotTyped(t *testing.T) {
	// A terminal's reply to a query it was never asked, or a tmux DCS wrapper,
	// must not be delivered as keystrokes. Consuming it keeps the stream in sync;
	// this is explicitly NOT tmux passthrough support, which is deferred.
	runDecodeCases(t, []decodeCase{
		{"OSC title with BEL", "\x1b]0;a title\x07", Config{}, termmosaic.Event{}, 0, StatusOK},
		{"OSC title with ST", "\x1b]0;a title\x1b\\", Config{}, termmosaic.Event{}, 0, StatusOK},
		{"OSC clipboard", "\x1b]52;c;cGF5bG9h\x07", Config{}, termmosaic.Event{}, 0, StatusOK},
		{"DCS passthrough wrapper", "\x1bPtmux;abc\x1b\\", Config{}, termmosaic.Event{}, 0, StatusOK},
		{"APC", "\x1b_Gf=100,a=T\x1b\\", Config{}, termmosaic.Event{}, 0, StatusOK},
		{"OSC that never terminates is incomplete", "\x1b]0;a title", Config{}, termmosaic.Event{}, nWantsNone, StatusIncomplete},
	})
}

func TestDecodeOverflowDropsWithoutResyncHunting(t *testing.T) {
	// A non-paste sequence that runs past maxSequenceLength without a final byte
	// is declared invalid and the WHOLE run is discarded. No attempt is made to
	// find a plausible introducer inside it, because a 64-byte runaway is almost
	// always a stream the terminal is not really sending and hunting for a '['
	// inside it manufactures events from noise.
	runaway := "\x1b[" + strings.Repeat("0", 200)
	ev, n, st := Decode([]byte(runaway), Config{})
	if st != StatusInvalid {
		t.Errorf("runaway status = %v, want StatusInvalid", st)
	}
	if n != maxSequenceLength {
		t.Errorf("runaway consumed %d bytes, want %d: the run we gave up on must be discarded", n, maxSequenceLength)
	}
	if ev.Kind != termmosaic.EventNone {
		t.Errorf("runaway produced an event, want none")
	}

	// Exactly at the bound is still a legitimate incomplete sequence, and one
	// byte past it is a runaway. That is what "exceeds 64 bytes" means.
	atBound := "\x1b[" + strings.Repeat("0", maxSequenceLength-2)
	if len(atBound) != maxSequenceLength {
		t.Fatalf("test setup: atBound is %d bytes", len(atBound))
	}
	if _, n, st := Decode([]byte(atBound), Config{}); st != StatusIncomplete || n != 0 {
		t.Errorf("%d-byte prefix: n=%d st=%v, want 0 and StatusIncomplete", len(atBound), n, st)
	}
	overBound := "\x1b[" + strings.Repeat("0", maxSequenceLength-1)
	if _, n, st := Decode([]byte(overBound), Config{}); st != StatusInvalid || n != maxSequenceLength {
		t.Errorf("%d-byte prefix: n=%d st=%v, want %d and StatusInvalid", len(overBound), n, st, maxSequenceLength)
	}
}

func TestDecodeDiscardedRunResumesAtTheNextByte(t *testing.T) {
	// StatusInvalid's contract is "discard the consumed bytes and resume at the
	// first unconsumed one", so a run we refuse must not eat the good bytes after
	// it. Driving Decode in a loop is exactly what Parser does, so the loop is
	// the assertion.
	seq := "\x1b[" + strings.Repeat("0", 200) + "a\x1b[A"
	var got []termmosaic.Event
	rest := []byte(seq)
	for len(rest) > 0 {
		ev, n, _ := Decode(rest, Config{})
		if n == 0 {
			break
		}
		if ev.Kind != termmosaic.EventNone {
			got = append(got, ev)
		}
		rest = rest[n:]
	}
	// Resuming at byte 64 means the leftover filler decodes as ordinary keys and
	// the two real sequences after it survive. What matters is that the last two
	// events are the ones we appended, not that the noise vanished.
	if len(got) < 2 {
		t.Fatalf("loop over the runaway produced %d events, want at least 2", len(got))
	}
	if got[len(got)-1].Key != termmosaic.KeyUp || got[len(got)-2].Rune != 'a' {
		t.Errorf("loop over the runaway ended with %+v %+v, want 'a' then KeyUp", got[len(got)-2], got[len(got)-1])
	}
}

func TestDecodeStatusStrings(t *testing.T) {
	for st, want := range map[Status]string{
		StatusOK:         "ok",
		StatusIncomplete: "incomplete",
		StatusInvalid:    "invalid",
		Status(99):       "unknown",
	} {
		if got := st.String(); got != want {
			t.Errorf("Status(%d).String() = %q, want %q", int(st), got, want)
		}
	}
}

func TestDecodeModeAndKeyTypeStrings(t *testing.T) {
	for m, want := range map[MouseMode]string{
		MouseNone: "none", MouseClick: "click", MouseDrag: "drag", MouseAll: "all", MouseMode(9): "unknown",
	} {
		if got := m.String(); got != want {
			t.Errorf("MouseMode.String() = %q, want %q", got, want)
		}
	}
	for kt, want := range map[termmosaic.KeyType]string{
		termmosaic.KeyPress: "press", termmosaic.KeyRepeat: "repeat", termmosaic.KeyRelease: "release", termmosaic.KeyType(9): "unknown",
	} {
		if got := kt.String(); got != want {
			t.Errorf("KeyType.String() = %q, want %q", got, want)
		}
	}
	for cp, want := range map[termmosaic.ComposePhase]string{
		termmosaic.ComposeStart: "start", termmosaic.ComposeUpdate: "update",
		termmosaic.ComposeCommit: "commit", termmosaic.ComposeEnd: "end", termmosaic.ComposePhase(9): "unknown",
	} {
		if got := cp.String(); got != want {
			t.Errorf("ComposePhase.String() = %q, want %q", got, want)
		}
	}
}

func TestDecodeConfigDefaultsAreConservative(t *testing.T) {
	// Every default the ADR specifies as OFF has to be OFF in the zero value.
	// A framework that turns mouse reporting on by default takes text selection,
	// middle-click paste and scrollback copying away from the user's shell.
	var zero Config
	if zero.MouseMode != MouseNone {
		t.Errorf("zero MouseMode = %v, want MouseNone", zero.MouseMode)
	}
	if zero.EnableFocusReporting {
		t.Error("zero EnableFocusReporting must be false")
	}
	if zero.ProbeKitty {
		t.Error("zero ProbeKitty must be false")
	}
	if zero.BracketedPaste {
		t.Error("zero BracketedPaste must be false; DefaultConfig is where it is on")
	}
	if zero.KittyReportReleases {
		t.Error("zero KittyReportReleases must be false")
	}
	if zero.WriteProbe != nil {
		t.Error("zero WriteProbe must be nil")
	}
	if got := zero.escapeDelay(); got != DefaultEscapeDelay {
		t.Errorf("zero escapeDelay = %v, want %v", got, DefaultEscapeDelay)
	}
	if got := zero.maxPasteBytes(); got != DefaultMaxPasteBytes {
		t.Errorf("zero maxPasteBytes = %d, want %d", got, DefaultMaxPasteBytes)
	}
	if got := zero.eventQueue(); got != DefaultEventQueue {
		t.Errorf("zero eventQueue = %d, want %d", got, DefaultEventQueue)
	}
	if got := zero.kittyFlags(); got != DefaultKittyFlags {
		t.Errorf("zero kittyFlags = %d, want %d (0b1)", got, DefaultKittyFlags)
	}

	// A negative escape delay disables the wait entirely.
	noWait := Config{EscapeDelay: -1}
	if got := noWait.escapeDelay(); got >= 0 {
		t.Errorf("negative EscapeDelay resolved to %v, want a disabled wait", got)
	}

	// DefaultConfig is the recommended shape: bracketed paste and the probe on,
	// disambiguate-only kitty flags, still no mouse and no focus reporting.
	d := DefaultConfig()
	if !d.BracketedPaste || !d.ProbeKitty {
		t.Errorf("DefaultConfig = %+v, want bracketed paste and the kitty probe on", d)
	}
	if d.MouseMode != MouseNone || d.EnableFocusReporting {
		t.Errorf("DefaultConfig = %+v, want mouse and focus reporting off", d)
	}
	if d.KittyFlags != 0b1 {
		t.Errorf("DefaultConfig KittyFlags = %b, want 0b1", d.KittyFlags)
	}
}

func TestDecodeConfigProbesAreIndependentOfParsing(t *testing.T) {
	// Turning a terminal mode OFF gates which enable sequence is written, never
	// what the decoder recognises. A terminal that sends a bracketed paste or a
	// mouse report anyway must be decoded correctly, which is the whole reason
	// mouse is decoded even though mouse capture is off by default.
	cfg := Config{}
	for _, seq := range []string{"\x1b[200~hi\x1b[201~", "\x1b[<0;1;1M", "\x1b[I", "\x1b[?1u"} {
		ev, _, st := Decode([]byte(seq), cfg)
		if st == StatusIncomplete {
			t.Errorf("Decode(%q) with everything off reported incomplete", seq)
		}
		if ev.Kind == termmosaic.EventNone && seq != "\x1b[?1u" {
			t.Errorf("Decode(%q) produced no event with all capture off", seq)
		}
	}
}
