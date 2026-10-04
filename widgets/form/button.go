package form

import (
	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
)

// Button rings. They are exported for the same reason the other widgets'
// markers are: the focus indicator is part of the widget's contract.
const (
	// ButtonRingFocused is drawn at each end of the label while the button has
	// focus.
	ButtonRingFocused = "[]"
	// ButtonRingUnfocused is drawn at each end of the label while it does not.
	// It is a space, so the button is the same width in both states and
	// selecting focus cannot reflow a form.
	ButtonRingUnfocused = "  "
)

// Button is a focusable label that activates on Enter, Space or a click.
//
// # Accessibility
//
// Focus is a SHAPE: the label is ringed with brackets — "[Save]" — while focused
// and padded with spaces — " Save " — while not. Brackets rather than colour,
// because the one thing a user must be able to see on a form full of buttons is
// which button Enter is about to press.
//
// The two rings are the same width, so a button's size does not depend on its
// focus state. A form whose buttons reflow every time the user tabs between them
// is worse than one whose focus is a little less obvious.
//
// # Key contract
//
// Keys are consumed only while focused, and a disabled button consumes nothing —
// not a click and not a key — so a form can disable an action without having to
// remove the widget from the tree.
//
//	activate      KeyEnter, KeySpace
//	focus         a left click inside Bounds, which also activates
type Button struct {
	bounds buffer.Rect

	// Label is the button's text. It is truncated with a marker rather than
	// clipped, so a button too narrow for its label says so.
	Label string

	// LabelStyle is the style of the label. An unset value resolves to the
	// terminal's own colours.
	LabelStyle buffer.Style

	// FocusStyle is the style of the whole button — background, label and
	// brackets — while focused. An unset value means "the label style with
	// AttrReverse", so focus is visible with no configuration and under
	// NO_COLOR, which suppresses colour but not attributes.
	FocusStyle buffer.Style

	// DisabledStyle is the style of the whole button while disabled. An unset
	// value means MutedStyle, because "you cannot do this" should not look
	// available.
	DisabledStyle buffer.Style

	// Background is painted across Bounds before anything else.
	Background buffer.Style

	// Disabled makes the button inert: it draws in DisabledStyle and consumes no
	// event. It is a field rather than a style-only convention because
	// "consumes nothing" is behaviour, not appearance.
	Disabled bool

	// Ascii selects buffer's ASCII truncation rung: pass !caps.Unicode.
	Ascii bool

	// OnActivate is called when the user activates the button.
	OnActivate func()

	// focused reports whether the button has focus.
	focused bool

	// body is the cached truncated label, rebuilt when the rect, the label or the
	// styles change.
	body          []buffer.Span
	cache         visibleCache
	cacheStyles   buttonStyles
	cacheFocused  bool
	cacheDisabled bool
	cacheLabel    string
}

// buttonStyles is the resolved style set a Button's cached display depends on.
type buttonStyles struct {
	label, focus, disabled, background buffer.Style
}

// NewButton returns a Button labelled label, sized r.
func NewButton(r buffer.Rect, label string) *Button {
	return &Button{bounds: r, Label: label}
}

// Bounds returns the button's rectangle, safe to call before the first Draw.
func (b *Button) Bounds() buffer.Rect { return b.bounds }

// SetBounds sets the button's rectangle. The cached label is keyed on it.
func (b *Button) SetBounds(r buffer.Rect) { b.bounds = r }

// Focused reports whether the button has focus.
func (b *Button) Focused() bool { return b.focused }

// SetFocused gives or removes focus.
func (b *Button) SetFocused(v bool) { b.focused = v }

// SetLabel sets the button's text.
func (b *Button) SetLabel(s string) { b.Label = s }

// SetDisabled enables or disables the button.
func (b *Button) SetDisabled(v bool) { b.Disabled = v }

// MinSize returns the smallest meaningful button: its label plus the two ring
// cells, on one row.
//
// The ring is counted even when the button is not focused, because MinSize must
// not depend on state the user controls: a layout that sized a button from
// MinSize would otherwise resize the form every time the user tabbed onto it.
func (b *Button) MinSize() buffer.Size {
	return buffer.Size{W: buffer.StringWidth(b.Label) + 2, H: 1}
}

// styles returns the resolved style set.
func (b *Button) styles() buttonStyles {
	lbl := b.LabelStyle.Resolved()
	focus := b.FocusStyle.Resolved()
	if b.FocusStyle.IsUnset() {
		focus = lbl.WithAttr(buffer.AttrReverse)
	}
	dis := b.DisabledStyle.Resolved()
	if b.DisabledStyle.IsUnset() {
		dis = buffer.MutedStyle.Resolved()
	}
	return buttonStyles{label: lbl, focus: focus, disabled: dis, background: b.Background.Resolved()}
}

// buttonRect returns the rectangle the button occupies inside its bounds: its
// label plus two ring cells, on one row, centred in both axes.
//
// The height is 1 even when Bounds is taller, so a button in a form row never
// grows to fill the row and never disagrees with its own MinSize.
func (b *Button) buttonRect() buffer.Rect {
	r := b.bounds
	w := buffer.StringWidth(b.Label) + 2
	if w > r.W {
		w = r.W
	}
	if w < 1 {
		w = 1
	}
	h := 1
	// Centre within Bounds, leaving any odd row on the bottom, which matches
	// geometry.Align's own reading and keeps the arithmetic symmetric with it.
	y := r.Y + (r.H-h)/2
	return buffer.Rect{X: r.X + (r.W-w)/2, Y: y, W: w, H: h}
}

// rebuild recomputes the cached truncated label for a button of w cells.
func (b *Button) rebuild(w int) {
	styles := b.styles()
	b.body = capRow(b.Ascii, b.Label, styles.label, w-2)
	b.cache.store(b.buttonRect())
	b.cacheStyles = styles
	b.cacheFocused = b.focused
	b.cacheDisabled = b.Disabled
	b.cacheLabel = b.Label
}

// Draw paints the background, the button's own background, and the label with
// its focus ring.
//
// It is total for every rect: a button narrower than its ring keeps the ring's
// left cell and clips the rest, which still shows something, and a one-cell
// button shows one cell of its label rather than blanking (ADR 0007 §4).
func (b *Button) Draw(buf *buffer.Buffer) {
	r := b.bounds
	if r.Empty() {
		return
	}
	buf.FillRect(r, b.Background.Resolved().Blank())

	br := b.buttonRect()
	if br.Empty() {
		return
	}
	styles := b.styles()
	if !b.cache.matches(br, false) || styles != b.cacheStyles ||
		b.focused != b.cacheFocused || b.Disabled != b.cacheDisabled || b.Label != b.cacheLabel {
		b.rebuild(br.W)
	}

	// The whole button takes one style, background included, so a focused button
	// is a filled region rather than three coloured fragments.
	fill, ring := styles.label, ButtonRingUnfocused
	switch {
	case b.Disabled:
		fill = styles.disabled
	case b.focused:
		fill, ring = styles.focus, ButtonRingFocused
	}
	buf.FillRect(br, fill.Blank())
	if len(b.body) > 0 {
		x := br.X + 1
		if x >= br.Right() {
			return
		}
		buf.SetSpans(x, br.Y, b.body)
	}
	if ring == ButtonRingFocused {
		buf.SetString(br.X, br.Y, ring[:1], fill)
		if br.W >= 2 {
			buf.SetString(br.Right()-1, br.Y, ring[1:], fill)
		}
	}
}

// Invalidate satisfies termmosaic.Widget. The button repaints in full every
// frame.
func (b *Button) Invalidate() {}

// Handle offers ev to the button and reports whether it consumed it. A disabled
// button consumes nothing.
func (b *Button) Handle(ev termmosaic.Event) bool {
	if b.Disabled {
		return false
	}
	switch ev.Kind {
	case termmosaic.EventMouse:
		if ev.Mouse.Button != termmosaic.MouseLeft || ev.Mouse.Action != termmosaic.MousePress {
			return false
		}
		if !b.bounds.Contains(ev.Mouse.X, ev.Mouse.Y) {
			return false
		}
		b.focused = true
		b.activate()
		return true
	case termmosaic.EventKey:
		if !b.focused || ev.Type == termmosaic.KeyRelease {
			return false
		}
		if activateKey(ev) {
			b.activate()
			return true
		}
		return false
	default:
		return false
	}
}

// activate fires the callback, if there is one.
func (b *Button) activate() {
	if b.OnActivate != nil {
		b.OnActivate()
	}
}

// Button is a Widget, a Focusable and a Minimizable.
var (
	_ termmosaic.Widget      = (*Button)(nil)
	_ termmosaic.Focusable   = (*Button)(nil)
	_ termmosaic.Minimizable = (*Button)(nil)
)
