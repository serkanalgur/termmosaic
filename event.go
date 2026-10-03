package termmosaic

import "github.com/serkanalgur/termmosaic/buffer"

// EventKind discriminates an Event.
type EventKind int

// Event kinds. A zero Event has Kind EventNone, which widgets ignore.
const (
	// EventNone is the zero value and carries no event.
	EventNone EventKind = iota
	// EventKey is a key press, possibly with modifiers.
	EventKey
	// EventResize reports a new terminal size.
	EventResize
	// EventMouse is a mouse button press, release or motion.
	EventMouse
	// EventPaste is a bracketed-paste payload.
	EventPaste
	// EventFocus reports gaining or losing terminal focus.
	EventFocus
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
	default:
		return "unknown"
	}
}

// Key identifies a key press. Printable keys are carried as their rune in
// Event.Rune; Key is only set for keys that have no printable form.
//
// The set is deliberately the xterm-encodable set. TermMosaic supports the
// kitty keyboard protocol when the terminal advertises it (Caps.KittyKeyboard),
// which is how keys like Hyper and Super and unambiguous Ctrl+Shift+letter
// arrive; without it, some combinations are indistinguishable and this is an
// accepted limitation of owning the input layer.
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
	// KeyBackspace is the backspace key. Note that a terminal may deliver it
	// as a control character rather than as an escape sequence, depending on
	// the terminfo setting; the input decoder will have to reconcile that.
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

// Event is a single input event delivered to the widget tree.
//
// One struct rather than an interface, because events are allocated per
// keystroke and an interface here would mean a heap allocation on the input
// path for no benefit. Unused fields are zero.
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
	// Mouse is the payload for EventMouse.
	Mouse Mouse
	// Size is the new size for EventResize.
	Size Size
	// Text is the payload for EventPaste.
	Text string
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
