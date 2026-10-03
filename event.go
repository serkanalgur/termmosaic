package termmosaic

import "github.com/serkanalgur/termmosaic/buffer"

// EventKind discriminates an Event.
type EventKind int

// Event kinds. A zero Event has Kind EventNone, which widgets ignore.
const (
	// EventNone is the zero value and carries no event.
	EventNone EventKind = iota
	// EventKey is a key press, repeat or release.
	EventKey
	// EventResize reports a new terminal size.
	EventResize
	// EventMouse is a mouse button press, release or motion.
	EventMouse
	// EventPaste is a bracketed-paste payload, delivered whole.
	EventPaste
	// EventFocus reports gaining or losing terminal focus.
	EventFocus
	// EventCompose reports IME composition state.
	//
	// DECLARED BUT NEVER EMITTED IN v0.x. IME support is deferred by ADR 0005
	// §7; this constant exists so the union can already express composition,
	// and so a widget author writing a switch today sees the case and knows
	// composition is coming. Do not emit it and do not handle it as if it were
	// live.
	EventCompose
)

// String returns the kind's name.
func (k EventKind) String() string {
	switch k {
	case EventNone:
		return "none"
	case EventKey:
		return "key"
	case EventResize:
		return "resize"
	case EventMouse:
		return "mouse"
	case EventPaste:
		return "paste"
	case EventFocus:
		return "focus"
	case EventCompose:
		return "compose"
	default:
		return "unknown"
	}
}

// Key identifies a key press. Printable keys are carried as their rune in
// Event.Rune; Key is only set for keys that have no printable form.
//
// The set is deliberately the xterm-encodable set, with no F13–F35. A new key
// must be APPENDED at the end of the iota block below: the constants are
// contiguous and inserting one in the middle silently renumbers every later
// value.
//
// TermMosaic decodes the kitty keyboard protocol when the handshake on an
// input.Source succeeds — not merely when a terminal advertises the capability.
// Caps.KittyKeyboard is a TERM heuristic meaning "may support", and the
// negotiation state lives on the Source because it is a property of a running
// session rather than of a device. See ADR 0005 §2. Without a successful
// handshake some combinations are indistinguishable; that is an accepted
// limitation of owning the input layer.
type Key int

// Non-printable keys.
const (
	// KeyNone means the event carries a printable rune rather than a special
	// key, so Event.Rune holds the character.
	KeyNone Key = iota
	// KeyEnter is the return key.
	KeyEnter
	// KeyTab is the tab key.
	KeyTab
	// KeyBacktab is shift-tab, reported as its own key because terminals
	// encode it that way and a widget usually wants to treat it differently
	// from tab.
	KeyBacktab
	// KeyBackspace is the backspace key. A terminal may deliver it as a
	// control character rather than as an escape sequence, depending on the
	// terminfo setting, and the input decoder reconciles that: both 0x7f and
	// 0x08 decode to KeyBackspace by default. See ADR 0005 §2, and
	// input.Config.BackspaceByte to pin one.
	KeyBackspace
	// KeyEscape is the escape key, which a bare escape is indistinguishable
	// from the start of an escape sequence.
	KeyEscape
	// KeySpace is the space bar, which terminals may deliver as a rune.
	KeySpace
	// KeyUp is the up arrow.
	KeyUp
	// KeyDown is the down arrow.
	KeyDown
	// KeyLeft is the left arrow.
	KeyLeft
	// KeyRight is the right arrow.
	KeyRight
	// KeyHome is the home key.
	KeyHome
	// KeyEnd is the end key.
	KeyEnd
	// KeyPageUp is the page-up key.
	KeyPageUp
	// KeyPageDown is the page-down key.
	KeyPageDown
	// KeyDelete is the delete key.
	KeyDelete
	// KeyInsert is the insert key.
	KeyInsert
	// KeyF1 is the F1 key.
	KeyF1
	// KeyF2 is the F2 key.
	KeyF2
	// KeyF3 is the F3 key.
	KeyF3
	// KeyF4 is the F4 key.
	KeyF4
	// KeyF5 is the F5 key.
	KeyF5
	// KeyF6 is the F6 key.
	KeyF6
	// KeyF7 is the F7 key.
	KeyF7
	// KeyF8 is the F8 key.
	KeyF8
	// KeyF9 is the F9 key.
	KeyF9
	// KeyF10 is the F10 key.
	KeyF10
	// KeyF11 is the F11 key.
	KeyF11
	// KeyF12 is the F12 key.
	KeyF12
)

// String returns the key's name, for diagnostics and key-hint rendering.
func (k Key) String() string {
	if k == KeyNone {
		return ""
	}
	if name, ok := keyNames[k]; ok {
		return name
	}
	return "?"
}

var keyNames = map[Key]string{
	KeyEnter: "enter", KeyTab: "tab", KeyBacktab: "backtab",
	KeyBackspace: "backspace", KeyEscape: "esc", KeySpace: "space",
	KeyUp: "up", KeyDown: "down", KeyLeft: "left", KeyRight: "right",
	KeyHome: "home", KeyEnd: "end", KeyPageUp: "pgup", KeyPageDown: "pgdn",
	KeyDelete: "del", KeyInsert: "ins",
	KeyF1: "f1", KeyF2: "f2", KeyF3: "f3", KeyF4: "f4", KeyF5: "f5",
	KeyF6: "f6", KeyF7: "f7", KeyF8: "f8", KeyF9: "f9", KeyF10: "f10",
	KeyF11: "f11", KeyF12: "f12",
}

// KeyMod is a bitmask of modifier keys held during an event.
type KeyMod uint8

// Modifier keys.
const (
	// ModShift is the Shift key.
	ModShift KeyMod = 1 << iota
	// ModAlt is the Alt key, labelled Option on macOS.
	ModAlt
	// ModCtrl is the Control key.
	ModCtrl
	// ModSuper is the Super or Command key.
	ModSuper
)

// Has reports whether every bit in other is set in m.
func (m KeyMod) Has(other KeyMod) bool { return m&other == other }

// String returns the modifiers in a stable, conventional order.
func (m KeyMod) String() string {
	if m == 0 {
		return ""
	}
	var out []byte
	for _, e := range []struct {
		bit  KeyMod
		name string
	}{
		{ModCtrl, "ctrl"}, {ModAlt, "alt"}, {ModShift, "shift"}, {ModSuper, "super"},
	} {
		if m&e.bit != 0 {
			if len(out) > 0 {
				out = append(out, '+')
			}
			out = append(out, e.name...)
		}
	}
	return string(out)
}

// MouseButton identifies a mouse button.
type MouseButton int

// Mouse buttons.
const (
	// MouseNone means no button, for motion events.
	MouseNone MouseButton = iota
	// MouseLeft is the primary button.
	MouseLeft
	// MouseMiddle is the middle button.
	MouseMiddle
	// MouseRight is the secondary button.
	MouseRight
	// MouseWheelUp is a wheel-up notch.
	MouseWheelUp
	// MouseWheelDown is a wheel-down notch.
	MouseWheelDown
)

// MouseAction is what happened to the button.
type MouseAction int

// Mouse actions.
const (
	// MousePress is a button going down.
	MousePress MouseAction = iota
	// MouseRelease is a button coming up.
	MouseRelease
	// MouseDrag is motion with a button held.
	MouseDrag
	// MouseMove is motion with no button held.
	MouseMove
)

// Mouse is the payload of an EventMouse.
type Mouse struct {
	X, Y   int
	Button MouseButton
	Action MouseAction
	// Mod is the modifier state at the time of the event.
	Mod KeyMod
}

// KeyType is what happened to a key.
//
// Without the kitty keyboard protocol every event is KeyPress and autorepeat is
// indistinguishable from a second press; that is an accepted limitation of the
// xterm encoding, not a bug.
type KeyType uint8

// Key event types.
const (
	// KeyPress is a key going down. It is the zero value, so a synthesized
	// EventKey is a press unless something says otherwise.
	KeyPress KeyType = iota
	// KeyRepeat is an autorepeat, distinguishable from a second press only
	// under the kitty keyboard protocol.
	KeyRepeat
	// KeyRelease is a key coming up.
	KeyRelease
)

// String returns the key event type's name.
func (t KeyType) String() string {
	switch t {
	case KeyPress:
		return "press"
	case KeyRepeat:
		return "repeat"
	case KeyRelease:
		return "release"
	default:
		return "unknown"
	}
}

// ComposePhase is the stage of an IME composition.
type ComposePhase uint8

// Composition phases.
const (
	// ComposeStart begins a composition.
	ComposeStart ComposePhase = iota
	// ComposeUpdate replaces the preedit string of an in-progress composition.
	ComposeUpdate
	// ComposeCommit carries the committed text and ends the composition.
	ComposeCommit
	// ComposeEnd discards a composition without committing it.
	ComposeEnd
)

// String returns the composition phase's name.
func (p ComposePhase) String() string {
	switch p {
	case ComposeStart:
		return "start"
	case ComposeUpdate:
		return "update"
	case ComposeCommit:
		return "commit"
	case ComposeEnd:
		return "end"
	default:
		return "unknown"
	}
}

// Compose is the payload of an EventCompose.
//
// RESERVED, NEVER EMITTED IN v0.x. IME support is deferred by ADR 0005 and
// this type exists so the union can already express composition. The field set
// is what an implementation needs and no more: Text is the preedit string as
// the terminal reports it, Cursor is the byte offset of the caret within Text,
// and Phase says whether this is the start of a composition, an update to it,
// the committed text, or the end.
type Compose struct {
	Text   string
	Cursor int
	Phase  ComposePhase
}

// Event is a single input event delivered to the widget tree.
//
// One struct rather than an interface, because events cross the input path per
// keystroke and an interface here would mean a heap allocation for no benefit.
// Unused fields are zero, with one exception: Compose is nil unless Kind is
// EventCompose.
//
// INVARIANT: this struct is copied by value on the hot path and must not grow
// without a deliberate decision. TestEventSizeIsBounded pins sizeof(Event); see
// ADR 0005 section 8.
type Event struct {
	// Kind discriminates the event.
	Kind EventKind
	// Key is the non-printable key for EventKey, or KeyNone for a printable
	// character.
	Key Key
	// Rune is the printable character for EventKey, or 0.
	Rune rune
	// Mod is the modifier state.
	Mod KeyMod
	// Type is press, repeat or release. Always KeyPress unless the kitty
	// keyboard protocol reported event types (ADR 0005 section 2).
	Type KeyType
	// Mouse is the payload for EventMouse.
	Mouse Mouse
	// Size is the new size for EventResize.
	Size Size
	// Text is the payload for EventPaste, the entire paste undelimited and
	// unescaped, and the committed text for EventCompose.
	Text string
	// Compose is the payload for EventCompose, and nil otherwise. This is the
	// only pointer field in Event and the one field allowed to be nil; see
	// ADR 0005 section 7 for why composition is reserved but deferred.
	Compose *Compose
	// Truncated reports that a paste payload exceeded the configured maximum
	// and was cut short. The text is still a valid prefix of what was pasted.
	Truncated bool
	// Focused reports the new state for EventFocus.
	Focused bool
}

// KeyEvent returns an EventKey for a printable rune.
func KeyEvent(r rune, mod KeyMod) Event {
	return Event{Kind: EventKey, Rune: r, Mod: mod}
}

// SpecialKeyEvent returns an EventKey for a non-printable key.
func SpecialKeyEvent(k Key, mod KeyMod) Event {
	return Event{Kind: EventKey, Key: k, Mod: mod}
}

// ResizeEvent returns an EventResize for the given size.
func ResizeEvent(w, h int) Event {
	return Event{Kind: EventResize, Size: buffer.Size{W: w, H: h}}
}
