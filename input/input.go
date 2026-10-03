// Package input decodes a terminal's byte stream into termmosaic.Event values.
//
// It exists as its own package, and in this two-layer shape, for one reason:
// every input bug is a function of the bytes, so the layer that holds the logic
// must be a pure function over a byte slice with no I/O, no clock and no
// retained state. That makes a sequence split across two read(2) calls a return
// value (StatusIncomplete) and a table-driven test rather than a flaky timing
// test. See ADR 0005.
//
//   - Decode is pure. It takes the whole configuration by value and returns one
//     event, the bytes it consumed, and a Status.
//   - Parser is the resumable driver. It holds only the state that genuinely
//     cannot be avoided: the bytes of a sequence that arrived incomplete, the
//     accumulation of a bracketed paste, and the escape deadline. The deadline
//     lives here and NOT in Decode because deciding whether a bare ESC is the
//     Escape key or the first byte of an arrow needs time, not bytes.
//   - Source is the single place the two streams a terminal offers, input bytes
//     and resize notifications, meet. It produces one ordered channel of events.
//     A full channel stops the read rather than dropping input: latency is
//     recoverable, a lost keystroke is not.
//
// Scope is exactly what ADR 0005 decides. Decoded: printable UTF-8 (including a
// multi-byte rune split across reads), C0 controls, CSI sequences for arrows,
// Home/End/PgUp/PgDn/Delete/Insert and F1-F12, SS3, the Linux console's
// ESC[[A-E form, legacy xterm modifiers, the kitty keyboard protocol, SGR 1006
// and urxvt 1015 and X10 mouse encodings, bracketed paste, and focus in/out.
//
// Deliberately NOT decoded: kitty F13-F35 (deferred, and appending them later
// must go at the END of the Key iota block), tmux/screen DCS passthrough
// (deferred, and a real gap), X11 UTF-8 extended mouse and 1016 SGR-pixel
// coordinates (deferred), kitty associated-text (deferred), and IME, which is
// scoped out entirely. EventCompose and the Compose payload field exist so
// composition is a future feature rather than a future rewrite, but this package
// never emits one.
//
// Capture is off by default for mouse and focus reporting, and the kitty
// handshake requests disambiguation (flag 0b1) only. A TUI that turns mouse
// reporting on by default takes text selection, middle-click paste and
// scrollback copying away from the user's shell.
package input

import "time"

// Status is the outcome of decoding one sequence from the front of seq.
type Status int

// Decode outcomes. The three together are the whole contract, and
// StatusIncomplete is the load-bearing one.
const (
	// StatusOK means an event was produced and n bytes were consumed. An
	// event whose Kind is EventNone means the bytes were a recognised reply
	// rather than a key — a terminal answering a capability query, say —
	// and were deliberately swallowed.
	StatusOK Status = iota
	// StatusIncomplete means seq holds a proper prefix of a sequence and more
	// bytes are needed. No event is produced, n is zero, and the caller must
	// retain every byte it was given. This is what makes a sequence that
	// straddles two reads a return value rather than a bug.
	StatusIncomplete
	// StatusInvalid means the bytes cannot be a valid sequence. The consumed
	// bytes are discarded and the caller resumes at the first unconsumed byte.
	StatusInvalid
)

// String returns the status's name.
func (s Status) String() string {
	switch s {
	case StatusOK:
		return "ok"
	case StatusIncomplete:
		return "incomplete"
	case StatusInvalid:
		return "invalid"
	default:
		return "unknown"
	}
}

// MouseMode selects how much mouse reporting Source asks the terminal for.
type MouseMode int

// Mouse reporting modes.
const (
	// MouseNone enables no mouse reporting at all. This is the default and it
	// is a deliberate one: enabling mouse reporting steals the mouse from the
	// user's shell, and text selection, middle-click paste and scrollback
	// copying all stop working with no terminal-side indication of why.
	MouseNone MouseMode = iota
	// MouseClick reports button presses, releases and wheel notches. Wheel is
	// included here rather than in a drag mode because List, Table and Pager
	// all scroll with it, and a wheel that needs drag mode enabled is a
	// usability trap. This is what EnableMouse-style helpers select.
	MouseClick
	// MouseDrag additionally reports motion with a button held.
	MouseDrag
	// MouseAll additionally reports all motion, including motion with no
	// button held. Every reported cell of travel becomes an event crossing the
	// Handle boundary, so this is opt-in only.
	MouseAll
)

// String returns the mouse mode's name.
func (m MouseMode) String() string {
	switch m {
	case MouseNone:
		return "none"
	case MouseClick:
		return "click"
	case MouseDrag:
		return "drag"
	case MouseAll:
		return "all"
	default:
		return "unknown"
	}
}

// BackspaceMode selects which control byte means backspace.
type BackspaceMode int

// Backspace byte modes.
const (
	// BackspaceAuto accepts both 0x7f (DEL, what almost every modern terminal
	// sends) and 0x08 (BS, what the terminfo entry may imply). Guessing one is
	// how a user ends up with a TextInput whose backspace does nothing on
	// their terminal and works on the developer's.
	BackspaceAuto BackspaceMode = iota
	// BackspaceDelete accepts only 0x7f.
	BackspaceDelete
	// BackspaceControl accepts only 0x08.
	BackspaceControl
)

// Defaults for Config. They are named rather than only documented because a
// caller cannot tell an unset field from a zero one.
const (
	// DefaultEscapeDelay is how long a bare ESC waits for a following byte
	// before it is reported as KeyEscape. 25 ms is the value every terminal
	// library converged on and is above typical human inter-key latency. The
	// cost is stated plainly: pressing Escape and then another key within
	// 25 ms yields a chord. That is an ambiguity in the protocol, not a
	// defect in decoding.
	DefaultEscapeDelay = 25 * time.Millisecond
	// DefaultMaxPasteBytes caps one bracketed paste at 4 MiB. A longer paste
	// is truncated and flagged, and still scanned to its closing marker so
	// the stream stays in sync.
	DefaultMaxPasteBytes = 4 << 20
	// DefaultEventQueue is the depth of Source's event channel.
	DefaultEventQueue = 256
	// DefaultKittyFlags is the kitty keyboard flag set requested after a
	// successful query: disambiguate escape codes, and nothing else.
	DefaultKittyFlags uint8 = 0b1
	// KittyProbeTimeout is the hard bound on the kitty capability query.
	// Startup must never block on a terminal that ignores the query, and
	// 100 ms is well inside what a user perceives as instant for a terminal
	// that will never answer.
	KittyProbeTimeout = 100 * time.Millisecond
)

// maxSequenceLength bounds the bytes retained for one incomplete non-paste
// sequence.
//
// A sequence that exceeds it without reaching a final byte is declared
// StatusInvalid and its bytes are discarded, with NO resynchronisation search
// for a plausible introducer: a 64-byte runaway is almost always a stream the
// terminal is not really sending, and hunting for a '[' inside it can
// manufacture events from noise. A paste body is not a sequence and is not
// subject to this bound; it is bounded by Config.MaxPasteBytes instead, and it
// is scanned to its closing marker rather than abandoned.
const maxSequenceLength = 64

// Config configures a Parser or a Source.
//
// The zero value is usable and conservative: no mouse reporting, no focus
// reporting, no kitty probe, no bracketed-paste enable request, both backspace
// bytes accepted, a 25 ms escape delay, a 4 MiB paste cap and a 256-deep event
// queue.
//
// Two kinds of field need a word, because a Go bool cannot distinguish "unset"
// from "false" and ADR 0005's prose and field comments disagree about which
// defaults are true:
//
//   - BracketedPaste and ProbeKitty are the two fields whose field comment says
//     "default true" while the zero-value summary says false. Both are false in
//     the zero value — the conservative reading, since a mode the application
//     did not ask for is never enabled on its behalf — and both are true in
//     DefaultConfig, which is what an application should pass. Turning a
//     terminal mode ON never depends on these flags: Decode recognises
//     bracketed paste, mouse and focus bytes unconditionally, so a terminal that
//     sends them anyway is decoded correctly.
//   - EscapeDelay is zero meaning "use the 25 ms default", because the zero
//     value must carry the documented default. A NEGATIVE EscapeDelay disables
//     the wait entirely, making a bare ESC always the Escape key, which breaks
//     Alt-chords. ADR 0005 states this as "zero disables the wait"; that cannot
//     coexist with a zero value that is also the documented default, and this is
//     the resolution.
//
// KittyFlags is not a bool: zero is normalised to DefaultKittyFlags (0b1).
type Config struct {
	// MouseMode selects how much mouse reporting Source enables. The zero
	// value is MouseNone.
	MouseMode MouseMode

	// EnableFocusReporting requests CSI ? 1004 h. Terminal focus and widget
	// focus are not the same thing, so this is opt-in: ESC [ I fires on
	// alt-tab, on terminal switching and on some window managers'
	// focus-follows-mouse behaviour, and the first thing an author writes is a
	// switch that ignores it.
	EnableFocusReporting bool

	// BracketedPaste requests CSI ? 2004 h. True in DefaultConfig, false in
	// the zero value. The cost of guessing wrong is only that a paste arrives
	// as key events; the cost of guessing right is that a TextInput's undo
	// behaves correctly.
	BracketedPaste bool

	// WriteProbe sends raw bytes to the terminal. It is the write path for the
	// mode-enable sequences and for the kitty keyboard query.
	//
	// It is a function rather than a method on Terminal because ADR 0001 chose
	// two narrow interfaces and ADR 0005 deliberately does not widen them; an
	// application wires this to its own Sink. A nil value disables probing and
	// every enable sequence, leaving Source purely a decoder.
	WriteProbe func(p []byte) error

	// ProbeKitty enables the CSI ? u capability query. True in DefaultConfig,
	// false in the zero value, and ignored entirely when WriteProbe is nil.
	ProbeKitty bool

	// KittyFlags is the flag set pushed after a successful query. The zero
	// value means DefaultKittyFlags (0b1, disambiguate escape codes).
	// eventTypes (0b10) doubles or triples event volume and no widget in the
	// catalog consumes a key release; alternateKeys (0b100) produces shifted
	// symbols nothing uses; reportAllKeysAsEscapeCodes (0b1000) would break
	// plain-text input; associatedText (0b10000) would change what Event.Rune
	// means before a TextInput exists to define it.
	KittyFlags uint8

	// KittyReportReleases emits EventKey events for key releases when the
	// terminal reports event types. False by default: a release is decoded,
	// carried in Event.Type, and suppressed.
	KittyReportReleases bool

	// EscapeDelay is how long a bare ESC waits for a following byte before it
	// is reported as KeyEscape. Zero means DefaultEscapeDelay; negative
	// disables the wait. See the Config documentation for why.
	EscapeDelay time.Duration

	// BackspaceByte selects which control byte means backspace. The zero
	// value accepts both.
	BackspaceByte BackspaceMode

	// MaxPasteBytes caps a single paste payload. Zero means
	// DefaultMaxPasteBytes. A longer paste is truncated, flagged with
	// Event.Truncated, and still scanned to its closing marker so the stream
	// stays in sync.
	MaxPasteBytes int

	// EventQueue is the depth of Source's event channel. Zero means
	// DefaultEventQueue. When the queue is full the reader stops reading the
	// tty; input is never dropped, because a silently dropped keystroke is a
	// text input that loses a character while a full queue is only latency.
	EventQueue int
}

// DefaultConfig returns the recommended configuration: no mouse, no focus
// reporting, bracketed paste and the kitty probe enabled, disambiguate-only
// kitty flags, both backspace bytes, a 25 ms escape delay, a 4 MiB paste cap
// and a 256-deep event queue.
func DefaultConfig() Config {
	return Config{
		BracketedPaste: true,
		ProbeKitty:     true,
		KittyFlags:     DefaultKittyFlags,
	}
}

// escapeDelay returns the effective escape delay, resolving the zero value.
func (c Config) escapeDelay() time.Duration {
	if c.EscapeDelay == 0 {
		return DefaultEscapeDelay
	}
	return c.EscapeDelay
}

// maxPasteBytes returns the effective paste cap, resolving the zero value.
func (c Config) maxPasteBytes() int {
	if c.MaxPasteBytes <= 0 {
		return DefaultMaxPasteBytes
	}
	return c.MaxPasteBytes
}

// eventQueue returns the effective event-queue depth, resolving the zero value.
func (c Config) eventQueue() int {
	if c.EventQueue <= 0 {
		return DefaultEventQueue
	}
	return c.EventQueue
}

// kittyFlags returns the effective requested flag set, resolving the zero value.
func (c Config) kittyFlags() uint8 {
	if c.KittyFlags == 0 {
		return DefaultKittyFlags
	}
	return c.KittyFlags
}
