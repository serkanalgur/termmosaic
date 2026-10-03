package input

import "github.com/serkanalgur/termmosaic"

// Bit layout shared by the three mouse encodings. The low two bits are the
// button, bit 5 (32) is motion, bit 6 (64) selects the wheel block and bit 7
// (128) is a button mask rather than a button number.
const (
	mouseBtnMask  = 0x03
	mouseShiftBit = 0x04
	mouseAltBit   = 0x08
	mouseCtrlBit  = 0x10
	mouseMotion   = 0x20
	mouseWheel    = 0x40
	mouseExtra    = 0x80

	// xMouseOffset is the 32 added to every X10 coordinate and button byte, so
	// that none of them collides with a control character.
	xMouseOffset = 32
)

// decodeMouseParams handles the '<' private prefix, which is shared by SGR 1006
// and urxvt 1015.
//
// Both are 1-based coordinate forms and differ in how a release is reported: SGR
// uses a lowercase final byte, 1015 has no way to say it. Decoding both is
// cheap and is required, because a terminal that ignores our 1006 request emits
// 1015 and receiving mouse bytes we cannot parse is worse than not enabling
// mouse at all.
func decodeMouseParams(n int, p *csiParams, final byte) (termmosaic.Event, int, Status) {
	if p.nTop < 3 {
		return swallow(n)
	}
	code := p.value(0)
	x := p.value(1)
	y := p.value(2)
	if code < 0 || x < 0 || y < 0 {
		return swallow(n)
	}
	// Coordinates are 1-based on the wire and 0-based in cells.
	ev := mouseEvent(code, x-1, y-1)
	if ev.Kind != termmosaic.EventMouse {
		return swallow(n)
	}
	if final == 'm' {
		// SGR 1006 release. Force the action to a release: the wheel and
		// button bits in the code still identify what was released.
		ev.Mouse.Action = termmosaic.MouseRelease
		if ev.Mouse.Button == termmosaic.MouseNone {
			ev.Mouse.Button = buttonOf(code)
		}
	} else if final != 'M' {
		return swallow(n)
	}
	return ev, n, StatusOK
}

// decodeMouseX10 handles the legacy X10 and "normal" mouse form, CSI M Cb Cx Cy,
// in which the three following bytes are raw and every value carries a +32
// offset.
//
// X10 cannot report a release as such; a button code of 3 is the conventional
// spelling of one, and a click is otherwise reported as a press.
func decodeMouseX10(seq []byte, n int) (termmosaic.Event, int, Status) {
	if len(seq) < n+3 {
		// The three bytes are coordinates, and a coordinate arriving late is
		// exactly the split-read case StatusIncomplete exists for.
		return termmosaic.Event{}, 0, StatusIncomplete
	}
	code := int(seq[n]) - xMouseOffset
	x := int(seq[n+1]) - xMouseOffset
	y := int(seq[n+2]) - xMouseOffset
	if code < 0 || x < 0 || y < 0 {
		return termmosaic.Event{}, n + 3, StatusInvalid
	}
	ev := mouseEvent(code, x, y)
	if ev.Kind != termmosaic.EventMouse {
		return termmosaic.Event{}, n + 3, StatusInvalid
	}
	return ev, n + 3, StatusOK
}

// mouseEvent builds an EventMouse from a decoded button code and a
// zero-based cell position.
func mouseEvent(code, x, y int) termmosaic.Event {
	var ev termmosaic.Event
	if code < 0 || x < 0 || y < 0 {
		return ev
	}

	button := buttonOf(code)
	action := termmosaic.MousePress
	switch {
	case code&mouseWheel != 0:
		// The wheel block: 64 is up, 65 is down, and the low bits carry the
		// button, so 64+0 through 64+3 are the four directions. A wheel notch
		// is reported as MousePress because it is a discrete event with no
		// motion in it, and MouseWheelUp/MouseWheelDown on Button already say
		// what it is. MouseMove would make a consumer treat every notch as
		// motion and clear its pressed state.
		action = termmosaic.MousePress
	case code&mouseMotion != 0:
		if button == termmosaic.MouseNone || code&3 == 3 {
			action = termmosaic.MouseMove
		} else {
			action = termmosaic.MouseDrag
		}
	case code&3 == 3:
		// Button 3 with no motion bit is the conventional X10 spelling of a
		// release.
		action = termmosaic.MouseRelease
	}

	ev.Kind = termmosaic.EventMouse
	ev.Mouse.X = x
	ev.Mouse.Y = y
	ev.Mouse.Button = button
	ev.Mouse.Action = action
	if code&mouseShiftBit != 0 {
		ev.Mouse.Mod |= termmosaic.ModShift
	}
	if code&mouseAltBit != 0 {
		ev.Mouse.Mod |= termmosaic.ModAlt
	}
	if code&mouseCtrlBit != 0 {
		ev.Mouse.Mod |= termmosaic.ModCtrl
	}
	return ev
}

// buttonOf decodes the button bits of a mouse code.
//
// There are more wheel directions in the bit pattern than Mouse has constants
// for, so the two without a representation map onto the two that exist and the
// rest fall back to the nearest: reporting a horizontal wheel scroll as a
// vertical one is a far smaller failure than discarding the event, and
// horizontal scrolling is vanishingly rare in a terminal.
func buttonOf(code int) termmosaic.MouseButton {
	switch {
	case code&mouseWheel != 0:
		switch code & mouseBtnMask {
		case 0:
			return termmosaic.MouseWheelUp
		default:
			return termmosaic.MouseWheelDown
		}
	case code&mouseExtra != 0:
		// A button-mask form rather than a button number; the individual bits
		// are outside what Mouse expresses.
		return termmosaic.MouseNone
	}
	switch code & mouseBtnMask {
	case 0:
		return termmosaic.MouseLeft
	case 1:
		return termmosaic.MouseMiddle
	case 2:
		return termmosaic.MouseRight
	default:
		return termmosaic.MouseNone
	}
}
