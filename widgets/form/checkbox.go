package form

import (
	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
)

// CheckState is the state of a Checkbox: unchecked, checked, or the
// indeterminate state that means "some, but not all, of the children".
type CheckState uint8

// The three checkbox states.
const (
	// Unchecked is the zero state, so a zero Checkbox is an ordinary unchecked
	// box rather than a value that needs special-casing.
	Unchecked CheckState = iota
	// Checked is fully on.
	Checked
	// Indeterminate is the mixed state. It is never produced by toggling: it is
	// a state the APPLICATION computes from its children and sets, because only
	// the application knows what the children are.
	Indeterminate
)

// String returns the state's name, for diagnostics and golden-test failure
// messages. An out-of-range value renders in decimal rather than panicking,
// because a CheckState can arrive from data this package did not produce.
func (s CheckState) String() string {
	switch s {
	case Unchecked:
		return "unchecked"
	case Checked:
		return "checked"
	case Indeterminate:
		return "indeterminate"
	default:
		return "state(" + itoa(int(s)) + ")"
	}
}

// itoa is strconv.Itoa without the import, for a call on an error path whose
// result never escapes.
func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}

// Checkbox markers, exported for the same reason Radio's are: the non-colour
// state indicator is part of the widget's contract, not an implementation detail.
const (
	// CheckMarkUnchecked is the marker for Unchecked.
	CheckMarkUnchecked = "[ ]"
	// CheckMarkChecked is the marker for Checked.
	CheckMarkChecked = "[x]"
	// CheckMarkIndeterminate is the marker for Indeterminate.
	CheckMarkIndeterminate = "[-]"
)

// Checkbox is a toggleable box with an optional label, in one of three states.
//
// # Accessibility
//
// The marker is the state: "[ ]", "[x]" or "[-]". Three different SHAPES, so
// the state survives a monochrome terminal, NO_COLOR, and a reader who cannot
// distinguish the colours an application might add. CheckedStyle may make the
// checked box bolder or brighter, but nothing is lost if it does not.
//
// # Key contract
//
// Keys are consumed only while focused.
//
//	toggle      KeySpace or KeyEnter
//	cycle left  KeyLeft, but only when TriState is set
//	cycle right KeyRight, but only when TriState is set
//
// Toggling moves Unchecked → Checked → Unchecked, and Indeterminate → Checked,
// because a user pressing space on a mixed box is asserting "yes, all of it".
// Reaching Indeterminate is done by SetState, from the application's own
// knowledge of its children. TriState additionally lets Left and Right walk all
// three states, which is what an application whose children can be partially
// selected needs in order to clear the mixed state from the keyboard.
type Checkbox struct {
	bounds buffer.Rect

	// state is the current state. It is a field rather than a separate bool so
	// the zero value is meaningful.
	state CheckState

	// TriState enables Left and Right to cycle through all three states.
	TriState bool

	// focused reports whether the checkbox has focus.
	focused bool

	// Label is the text beside the box. It is truncated with a marker rather
	// than clipped, so an over-long label says there was more.
	Label string

	// LabelStyle is the style of the label. An unset value resolves to the
	// terminal's own colours.
	LabelStyle buffer.Style

	// CheckedStyle is the style of the MARKER when the box is checked or
	// indeterminate. An unset value means "the label style with AttrBold", so
	// the marked box is visibly marked with no configuration at all.
	CheckedStyle buffer.Style

	// Background is painted across Bounds before anything else.
	Background buffer.Style

	// Ascii selects buffer's ASCII truncation rung: pass !caps.Unicode.
	Ascii bool

	// OnChange is called after every user-driven state change, with the new
	// state. It is not called by SetState, so programmatic changes do not look
	// like user actions.
	OnChange func(CheckState)

	// label is the cached truncated label, rebuilt when the rect, the label or
	// the styles change.
	label        []buffer.Span
	cache        visibleCache
	cacheStyles  checkStyles
	cacheState   CheckState
	cacheFocused bool
	// cacheLabel is the Label the cache was built from. Comparing it rather than
	// the truncated output is what keeps a label that never fits from rebuilding
	// on every single frame.
	cacheLabel string
}

// checkStyles is the resolved style set a Checkbox's cached display depends on.
type checkStyles struct {
	label, marked, background buffer.Style
}

// NewCheckbox returns an unchecked Checkbox with the given label, sized r.
func NewCheckbox(r buffer.Rect, label string) *Checkbox {
	c := &Checkbox{bounds: r, Label: label}
	c.cacheState = c.state
	return c
}

// Bounds returns the checkbox's rectangle, safe to call before the first Draw.
func (c *Checkbox) Bounds() buffer.Rect { return c.bounds }

// SetBounds sets the checkbox's rectangle. The cached label is keyed on it.
func (c *Checkbox) SetBounds(r buffer.Rect) { c.bounds = r }

// Focused reports whether the checkbox has focus.
func (c *Checkbox) Focused() bool { return c.focused }

// SetFocused gives or removes focus.
func (c *Checkbox) SetFocused(v bool) { c.focused = v }

// MinSize returns the smallest meaningful checkbox: the marker and one cell of
// label, on one row.
func (c *Checkbox) MinSize() buffer.Size {
	return buffer.Size{W: buffer.StringWidth(CheckMarkUnchecked) + labelGapW, H: 1}
}

// SetLabel sets the text beside the box.
func (c *Checkbox) SetLabel(s string) { c.Label = s }

// State returns the current state. A value outside the three defined states is
// reported as Unchecked, because that is the only state a checkbox can honestly
// claim; anything else would draw a marker no user could interpret.
func (c *Checkbox) State() CheckState { return normaliseState(c.state) }

// SetState sets the state programmatically without firing OnChange.
func (c *Checkbox) SetState(s CheckState) {
	c.state = normaliseState(s)
	c.cache.ok = false
}

// Toggle flips the state as a user activation does: Checked becomes Unchecked and
// anything else becomes Checked. It fires OnChange when one is set.
func (c *Checkbox) Toggle() {
	if c.State() == Checked {
		c.SetState(Unchecked)
	} else {
		c.SetState(Checked)
	}
	if c.OnChange != nil {
		c.OnChange(c.State())
	}
}

// normaliseState folds an out-of-range state onto Unchecked, so a Checkbox is
// total over the whole uint8 range rather than only over its constants.
func normaliseState(s CheckState) CheckState {
	if s > Indeterminate {
		return Unchecked
	}
	return s
}

// marker returns the non-colour state indicator for a state.
func marker(s CheckState) string {
	switch normaliseState(s) {
	case Checked:
		return CheckMarkChecked
	case Indeterminate:
		return CheckMarkIndeterminate
	default:
		return CheckMarkUnchecked
	}
}

// styles returns the resolved style set.
func (c *Checkbox) styles() checkStyles {
	lbl := c.LabelStyle.Resolved()
	marked := c.CheckedStyle.Resolved()
	if c.CheckedStyle.IsUnset() {
		marked = lbl.WithAttr(buffer.AttrBold)
	}
	return checkStyles{label: lbl, marked: marked, background: c.Background.Resolved()}
}

// rebuild recomputes the cached truncated label for a rect r.W cells wide.
func (c *Checkbox) rebuild(r buffer.Rect) {
	styles := c.styles()
	head := buffer.StringWidth(CheckMarkUnchecked) + labelGapW
	c.label = capRow(c.Ascii, c.Label, styles.label, r.W-head)
	c.cache.store(r)
	c.cacheStyles = styles
	c.cacheState = c.State()
	c.cacheFocused = c.focused
	c.cacheLabel = c.Label
}

// Draw paints the background, the marker and the label.
//
// It is total for every rect: below MinSize the label is truncated to nothing
// and the marker clips, rather than the widget blanking itself (ADR 0007 §4).
func (c *Checkbox) Draw(buf *buffer.Buffer) {
	r := c.bounds
	if r.Empty() {
		return
	}
	buf.FillRect(r, c.Background.Resolved().Blank())
	if r.W <= 0 || r.H <= 0 {
		return
	}

	styles := c.styles()
	state := c.State()
	if !c.cache.matches(r, false) || styles != c.cacheStyles ||
		state != c.cacheState || c.focused != c.cacheFocused || c.Label != c.cacheLabel {
		c.rebuild(r)
	}

	// The marker wears the marked style whenever the box is not plainly
	// unchecked, and the plain style otherwise. Colour reinforces the glyph; it
	// never carries the state by itself.
	markStyle := styles.label
	if state != Unchecked {
		markStyle = styles.marked
	}
	x := buf.SetString(r.X, r.Y, clipText(marker(state)+labelGap, r.W), markStyle)
	if len(c.label) > 0 {
		buf.SetSpans(x, r.Y, c.label)
	}
}

// Invalidate satisfies termmosaic.Widget. The checkbox repaints in full every
// frame.
func (c *Checkbox) Invalidate() {}

// Handle offers ev to the checkbox and reports whether it consumed it.
func (c *Checkbox) Handle(ev termmosaic.Event) bool {
	switch ev.Kind {
	case termmosaic.EventMouse:
		return c.handleMouse(ev)
	case termmosaic.EventKey:
		return c.handleKey(ev)
	default:
		return false
	}
}

// handleMouse toggles on a click inside Bounds and takes focus. A click outside
// Bounds is not consumed and changes nothing.
func (c *Checkbox) handleMouse(ev termmosaic.Event) bool {
	if ev.Mouse.Button != termmosaic.MouseLeft || ev.Mouse.Action != termmosaic.MousePress {
		return false
	}
	if !c.bounds.Contains(ev.Mouse.X, ev.Mouse.Y) {
		return false
	}
	c.focused = true
	c.Toggle()
	return true
}

// handleKey applies one key.
func (c *Checkbox) handleKey(ev termmosaic.Event) bool {
	if !c.focused || ev.Type == termmosaic.KeyRelease {
		return false
	}
	if activateKey(ev) {
		c.Toggle()
		return true
	}
	switch ev.Key {
	case termmosaic.KeyRight:
		if !c.TriState {
			return false
		}
		c.SetState(nextState(c.State(), 1))
		if c.OnChange != nil {
			c.OnChange(c.State())
		}
		return true
	case termmosaic.KeyLeft:
		if !c.TriState {
			return false
		}
		c.SetState(nextState(c.State(), -1))
		if c.OnChange != nil {
			c.OnChange(c.State())
		}
		return true
	}
	return false
}

// nextState walks the three states in one direction. It is total over the cycle,
// so a shift at either end wraps rather than clamping — wrapping is what makes
// Left and Right reversible, which is the reason they exist.
func nextState(s CheckState, delta int) CheckState {
	switch delta {
	case -1:
		switch s {
		case Unchecked:
			return Indeterminate
		case Indeterminate:
			return Checked
		default:
			return Unchecked
		}
	default:
		switch s {
		case Unchecked:
			return Checked
		case Checked:
			return Indeterminate
		default:
			return Unchecked
		}
	}
}

// Checkbox is a Widget, a Focusable and a Minimizable.
var (
	_ termmosaic.Widget      = (*Checkbox)(nil)
	_ termmosaic.Focusable   = (*Checkbox)(nil)
	_ termmosaic.Minimizable = (*Checkbox)(nil)
)
