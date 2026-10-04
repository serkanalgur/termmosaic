package form

import (
	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
)

// Toggle markers. They are exported for the same reason Checkbox's are: the
// non-colour state indicator is part of the widget's contract.
const (
	// ToggleMarkOn is the marker when the toggle is on. It is padded to five
	// cells so that it is exactly as wide as ToggleMarkOff and the label beside it
	// never moves when the state changes.
	ToggleMarkOn = "[on ]"
	// ToggleMarkOff is the marker when the toggle is off.
	ToggleMarkOff = "[off]"
)

// Toggle is an on/off switch with an optional label.
//
// It is not a Checkbox with a different marker: a toggle means "this is running
// right now" and a checkbox means "include this in the operation", and users
// read them differently. The difference is spelled in the marker — the words "on"
// and "off" — because a bare "[x]" would be ambiguous between the two.
//
// # Accessibility
//
// The state is the WORD: "[on]" or "[off]", both five cells wide so the label
// never moves when the state changes. Nothing about the state depends on colour.
//
// # Key contract
//
// Keys are consumed only while focused. Every binding toggles; there is no
// "set to on" key, because a toggle has two states and reaching either is the
// same action.
//
//	toggle      KeySpace, KeyEnter, KeyLeft, KeyRight
type Toggle struct {
	bounds buffer.Rect

	// on is the current state. It is a field so that the zero Toggle is off,
	// which is the safe default for a switch nobody has turned on yet.
	on bool

	// focused reports whether the toggle has focus.
	focused bool

	// Label is the text beside the switch.
	Label string

	// LabelStyle is the style of the label. An unset value resolves to the
	// terminal's own colours.
	LabelStyle buffer.Style

	// OnStyle is the style of the "[on]" marker. An unset value means "the label
	// style with AttrBold".
	OnStyle buffer.Style

	// Background is painted across Bounds before anything else.
	Background buffer.Style

	// Ascii selects buffer's ASCII truncation rung: pass !caps.Unicode.
	Ascii bool

	// OnChange is called after every user-driven change, with the new state. It
	// is not called by SetOn.
	OnChange func(bool)

	// label is the cached truncated label.
	label        []buffer.Span
	cache        visibleCache
	cacheStyles  toggleStyles
	cacheOn      bool
	cacheFocused bool
	cacheLabel   string
}

// toggleStyles is the resolved style set a Toggle's cached display depends on.
type toggleStyles struct {
	label, on, background buffer.Style
}

// NewToggle returns a Toggle with the given label, sized r, in the off state.
func NewToggle(r buffer.Rect, label string) *Toggle {
	return &Toggle{bounds: r, Label: label}
}

// Bounds returns the toggle's rectangle, safe to call before the first Draw.
func (t *Toggle) Bounds() buffer.Rect { return t.bounds }

// SetBounds sets the toggle's rectangle. The cached label is keyed on it.
func (t *Toggle) SetBounds(r buffer.Rect) { t.bounds = r }

// Focused reports whether the toggle has focus.
func (t *Toggle) Focused() bool { return t.focused }

// SetFocused gives or removes focus.
func (t *Toggle) SetFocused(v bool) { t.focused = v }

// MinSize returns the smallest meaningful toggle: the marker and one cell of
// label, on one row. It is the OFF marker's width that is used, and the two
// markers are deliberately the same width so that MinSize is true in both
// states.
func (t *Toggle) MinSize() buffer.Size {
	return buffer.Size{W: buffer.StringWidth(ToggleMarkOff) + labelGapW, H: 1}
}

// On reports whether the toggle is on.
func (t *Toggle) On() bool { return t.on }

// SetOn sets the state programmatically without firing OnChange.
func (t *Toggle) SetOn(v bool) { t.on = v }

// SetLabel sets the text beside the switch.
func (t *Toggle) SetLabel(s string) { t.Label = s }

// Toggle flips the state and fires OnChange when one is set.
func (t *Toggle) Toggle() {
	t.on = !t.on
	if t.OnChange != nil {
		t.OnChange(t.on)
	}
}

// mark returns the non-colour state indicator.
func (t *Toggle) mark() string {
	if t.on {
		return ToggleMarkOn
	}
	return ToggleMarkOff
}

// styles returns the resolved style set.
func (t *Toggle) styles() toggleStyles {
	lbl := t.LabelStyle.Resolved()
	on := t.OnStyle.Resolved()
	if t.OnStyle.IsUnset() {
		on = lbl.WithAttr(buffer.AttrBold)
	}
	return toggleStyles{label: lbl, on: on, background: t.Background.Resolved()}
}

// rebuild recomputes the cached truncated label for a rect r.W cells wide.
func (t *Toggle) rebuild(r buffer.Rect) {
	styles := t.styles()
	head := buffer.StringWidth(ToggleMarkOff) + labelGapW
	t.label = capRow(t.Ascii, t.Label, styles.label, r.W-head)
	t.cache.store(r)
	t.cacheStyles = styles
	t.cacheOn = t.on
	t.cacheFocused = t.focused
	t.cacheLabel = t.Label
}

// Draw paints the background, the marker and the label.
//
// It is total for every rect: below MinSize the marker clips and the label
// truncates to nothing, and the widget never blanks itself (ADR 0007 §4).
func (t *Toggle) Draw(buf *buffer.Buffer) {
	r := t.bounds
	if r.Empty() {
		return
	}
	buf.FillRect(r, t.Background.Resolved().Blank())
	if r.W <= 0 || r.H <= 0 {
		return
	}

	styles := t.styles()
	if !t.cache.matches(r, false) || styles != t.cacheStyles || t.on != t.cacheOn ||
		t.focused != t.cacheFocused || t.Label != t.cacheLabel {
		t.rebuild(r)
	}

	markStyle := styles.label
	if t.on {
		markStyle = styles.on
	}
	x := buf.SetString(r.X, r.Y, clipText(t.mark()+labelGap, r.W), markStyle)
	if len(t.label) > 0 {
		buf.SetSpans(x, r.Y, t.label)
	}
}

// Invalidate satisfies termmosaic.Widget. The toggle repaints in full every
// frame.
func (t *Toggle) Invalidate() {}

// Handle offers ev to the toggle and reports whether it consumed it.
func (t *Toggle) Handle(ev termmosaic.Event) bool {
	switch ev.Kind {
	case termmosaic.EventMouse:
		if ev.Mouse.Button != termmosaic.MouseLeft || ev.Mouse.Action != termmosaic.MousePress {
			return false
		}
		if !t.bounds.Contains(ev.Mouse.X, ev.Mouse.Y) {
			return false
		}
		t.focused = true
		t.Toggle()
		return true
	case termmosaic.EventKey:
		if !t.focused || ev.Type == termmosaic.KeyRelease {
			return false
		}
		switch ev.Key {
		case termmosaic.KeyLeft, termmosaic.KeyRight:
			t.Toggle()
			return true
		}
		if activateKey(ev) {
			t.Toggle()
			return true
		}
		return false
	default:
		return false
	}
}

// Toggle is a Widget, a Focusable and a Minimizable.
var (
	_ termmosaic.Widget      = (*Toggle)(nil)
	_ termmosaic.Focusable   = (*Toggle)(nil)
	_ termmosaic.Minimizable = (*Toggle)(nil)
)
