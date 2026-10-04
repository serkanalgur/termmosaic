package form

import (
	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
)

// KeyHintSep is the separator KeyHint puts between two bindings when the
// application has not chosen one. Two spaces, because a single space makes the
// end of one binding's description run into the next binding's bracketed key.
const KeyHintSep = "  "

// Binding is one key and what it does, as shown by KeyHint.
type Binding struct {
	// Key is the key's name, e.g. "tab" or "enter". It is a string rather than a
	// termmosaic.Key so that a hint can name a key the Key enum does not have,
	// such as "ctrl+s", and so that the hint says what the user's terminal calls
	// the key rather than what the decoder calls it.
	Key string
	// Help is the description, e.g. "next field".
	Help string
}

// NewBinding returns a Binding.
func NewBinding(key, help string) Binding { return Binding{Key: key, Help: help} }

// KeyHint renders the keys available right now: the discoverability half of the
// form catalog.
//
// It is a widget rather than a string helper because a hint is the one piece of a
// form that changes whenever the state changes — a Select with no options has
// nothing to activate, so its hint goes away — and because it must obey the same
// repaint and clipping rules as everything else. Drawing it by hand at each call
// site is how a form ends up with three different hint styles.
//
// # Accessibility
//
// Each binding's key is BRACKETED — "[tab] next field" — so the key is a shape
// distinct from its description and the hint is readable without colour. No
// colour is used at all by default: KeyStyle, HelpStyle and SeparatorStyle all
// resolve to the terminal's own colours, and the hint is legible as plain text.
//
// # Layout
//
// One line. An over-long hint is TRUNCATED with buffer's marker rather than
// clipped, so a narrow pane tells the user that there were more keys rather than
// silently cutting the last one in half. The truncation is cached against the
// rect, so it never happens inside Draw.
type KeyHint struct {
	bounds buffer.Rect

	// Bindings are the hints to show, in order. They are held by the caller and
	// copied by SetBindings, so a later mutation of the caller's slice cannot
	// change what a drawn frame means.
	Bindings []Binding

	// Sep separates two bindings. An empty Sep means KeyHintSep.
	Sep string

	// KeyStyle is the style of the bracketed key.
	KeyStyle buffer.Style

	// HelpStyle is the style of the description.
	HelpStyle buffer.Style

	// SeparatorStyle is the style of the separator between bindings.
	SeparatorStyle buffer.Style

	// Background is painted across Bounds before anything else.
	Background buffer.Style

	// Ascii selects buffer's ASCII truncation rung: pass !caps.Unicode.
	Ascii bool

	// spans is the cached, already-truncated single line of the hint.
	spans []buffer.Span
	// cacheBindings is how many bindings spans was built from. It is compared
	// rather than the slice itself, so mutating the exported Bindings slice in
	// place still produces a correct next frame.
	cacheBindings int
	cache         visibleCache
	cacheStyles   hintStyles
}

// hintStyles is the resolved style set a KeyHint's cached line depends on.
type hintStyles struct {
	key, help, sep, background buffer.Style
}

// NewKeyHint returns a KeyHint over bindings, sized r.
func NewKeyHint(r buffer.Rect, bindings []Binding) *KeyHint {
	k := &KeyHint{bounds: r}
	k.SetBindings(bindings)
	return k
}

// Bounds returns the hint's rectangle, safe to call before the first Draw.
func (k *KeyHint) Bounds() buffer.Rect { return k.bounds }

// SetBounds sets the hint's rectangle. The cached line is keyed on it.
func (k *KeyHint) SetBounds(r buffer.Rect) { k.bounds = r }

// MinSize returns the smallest meaningful hint: one cell, showing one cell of
// the first key.
func (k *KeyHint) MinSize() buffer.Size { return buffer.Size{W: 1, H: 1} }

// SetBindings replaces the bindings and invalidates the cached line.
func (k *KeyHint) SetBindings(b []Binding) {
	if len(b) == 0 {
		k.Bindings = nil
	} else {
		k.Bindings = append([]Binding(nil), b...)
	}
	k.cache.ok = false
}

// SetSep sets the separator between bindings. It invalidates the cached line,
// because the separator is part of that line.
func (k *KeyHint) SetSep(s string) {
	k.Sep = s
	k.cache.ok = false
}

// separator returns the separator to use, resolving the empty value.
func (k *KeyHint) separator() string {
	if k.Sep == "" {
		return KeyHintSep
	}
	return k.Sep
}

// styles returns the resolved style set.
func (k *KeyHint) styles() hintStyles {
	return hintStyles{
		key:        k.KeyStyle.Resolved(),
		help:       k.HelpStyle.Resolved(),
		sep:        k.SeparatorStyle.Resolved(),
		background: k.Background.Resolved(),
	}
}

// rebuild recomputes the cached line for a rect r.W cells wide. It is the only
// allocating operation in KeyHint.
func (k *KeyHint) rebuild(r buffer.Rect) {
	styles := k.styles()
	sep := k.separator()
	var spans []buffer.Span
	for i, b := range k.Bindings {
		if i > 0 {
			spans = append(spans, buffer.NewSpan(sep, styles.sep))
		}
		if b.Key != "" {
			// The brackets are the non-colour signal, so they are written
			// literally rather than folded into a style: the shape is the
			// information.
			spans = append(spans, buffer.NewSpan("["+b.Key+"]", styles.key))
			if b.Help != "" {
				spans = append(spans, buffer.NewSpan(" ", styles.help))
			}
		}
		if b.Help != "" {
			spans = append(spans, buffer.NewSpan(b.Help, styles.help))
		}
	}
	if len(spans) == 0 {
		k.spans = nil
	} else if r.W > 0 {
		if k.Ascii {
			k.spans = buffer.TruncateASCII(spans, r.W)
		} else {
			k.spans = buffer.Truncate(spans, r.W)
		}
	} else {
		k.spans = nil
	}
	k.cache.store(r)
	k.cacheStyles = styles
	k.cacheBindings = len(k.Bindings)
}

// cacheCurrent reports whether the cached line describes what a rebuild would
// produce.
func (k *KeyHint) cacheCurrent(r buffer.Rect, styles hintStyles) bool {
	return k.cache.matches(r, false) && styles == k.cacheStyles &&
		len(k.Bindings) == k.cacheBindings
}

// Draw paints the background and then the hint line.
//
// It is total for every rect: below one cell of width the truncated line is nil
// and the background is still painted, so the widget never blanks and never
// writes outside Bounds.
func (k *KeyHint) Draw(buf *buffer.Buffer) {
	r := k.bounds
	if r.Empty() {
		return
	}
	buf.FillRect(r, k.Background.Resolved().Blank())
	if r.W <= 0 || r.H <= 0 {
		return
	}

	styles := k.styles()
	if !k.cacheCurrent(r, styles) {
		k.rebuild(r)
	}
	if len(k.spans) == 0 {
		return
	}
	buf.SetSpans(r.X, r.Y, k.spans)
}

// Invalidate satisfies termmosaic.Widget. The hint repaints in full every frame.
func (k *KeyHint) Invalidate() {}

// Handle satisfies termmosaic.Widget. A KeyHint is a label: it consumes nothing,
// because a key pressed while reading the hints belongs to whatever the hints
// describe.
func (k *KeyHint) Handle(termmosaic.Event) bool { return false }

// KeyHint is a Widget and a Minimizable. It is deliberately NOT Focusable: it has
// no state of its own to reach.
var (
	_ termmosaic.Widget      = (*KeyHint)(nil)
	_ termmosaic.Minimizable = (*KeyHint)(nil)
)
