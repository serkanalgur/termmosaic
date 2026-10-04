package buffer

// Style is a cell's complete rendition: foreground, background and attributes.
// It is exactly the flat projection of a Cell's three styling fields, so
// converting in either direction is a field copy and no allocation
// (Cell.Style and Style.Cell).
//
// It is 12 bytes and is passed BY VALUE everywhere. Never take or store a
// *Style: a pointer needs a stable address, breaks == comparison, and puts an
// indirection on the per-cell write for nothing. Go's register-based ABI hands
// a 3-field, 12-byte struct's fields to registers independently, so f(st Style)
// costs exactly what f(fg, bg, attr) costs — which is why bundling these three
// channels is free and there is no reason to spell them out separately.
//
// THE ZERO STYLE IS A RESERVED SENTINEL meaning "the author did not choose",
// not a style. Its three fields would otherwise read as opaque black on black,
// which is a legitimate style nobody means by default: Colour(0) is black, so
// Style{} is not self-evidently empty. That is why IsUnset exists and why
// Resolved exists.
//
// AUTHORING RULE: build a style with NewStyle or with a With* method, never by
// a partial composite literal. Style{FG: accent} leaves BG as opaque black and
// compiles, which is the sharpest edge in this file.
//
// A style is resolved through Resolved exactly once before it is read — the
// idiom is `st := w.BorderStyle.Resolved()` — so that "the author set nothing"
// has one meaning in the entire catalog. Do not substitute a nil check, a
// parallel *Style field, or a per-widget default constant.
//
// A style does NOT consult NO_COLOR, the environment, or terminal capabilities.
// Those are encode-time concerns (ansi.Encoder), applied once at emit, so that
// a widget's output never depends on a global. Colour is authored as RGB and
// degraded by the quantiser; there is no authored colour depth here and no
// ColourFromIndex in v1.
type Style struct {
	// FG is the foreground colour. DefaultColour means the terminal's own
	// foreground; UnsetColour means "inherit", which only Patch interprets.
	FG Colour
	// BG is the background colour, with the same two special values as FG.
	BG Colour
	// Attr is the rendition attribute mask. It has no unset value: zero means
	// "no attributes", and Patch accumulates rather than replaces it.
	Attr Attr
}

// DefaultStyle is what an unset Style resolves to: the terminal's own
// foreground and background, no attributes.
//
// Every framework default style in this file is DefaultStyle plus attributes.
// The framework ships NO default colours in v1 — colour is entirely the
// application's choice — so this variable is never anything but the terminal's
// own defaults, and no widget should branch on it.
var DefaultStyle = Style{FG: DefaultColour, BG: DefaultColour}

// NewStyle returns a Style, mapping an unset colour channel (UnsetColour) to
// the terminal default. It is the constructor widgets and applications should
// reach for, and it is interchangeable with a With* chain off DefaultStyle.
func NewStyle(fg, bg Colour, attr Attr) Style {
	return Style{FG: resolvedColour(fg), BG: resolvedColour(bg), Attr: attr}
}

// resolvedColour maps the inherit marker to the terminal default and leaves
// every other colour alone — including opaque black, which is a real colour and
// must survive a resolve untouched.
func resolvedColour(c Colour) Colour {
	if c.IsUnset() {
		return DefaultColour
	}
	return c
}

// WithFG returns a copy of s with its foreground replaced. A c of UnsetColour
// leaves s's foreground alone, so a With* chain stays Patch-compatible.
func (s Style) WithFG(c Colour) Style {
	if c.IsUnset() {
		return s
	}
	s.FG = c
	return s
}

// WithBG returns a copy of s with its background replaced, with the same
// UnsetColour semantics as WithFG.
func (s Style) WithBG(c Colour) Style {
	if c.IsUnset() {
		return s
	}
	s.BG = c
	return s
}

// WithAttr returns a copy of s with a ORed into its attributes, so attribute
// intent accumulates along a builder chain:
//
//	DefaultStyle.WithBG(bg).WithAttr(AttrBold).WithFG(accent)
//
// This is the ONLY sanctioned way to derive a style from another other than
// Patch. Note that WithAttr cannot clear an attribute either; build a Style
// explicitly for that.
func (s Style) WithAttr(a Attr) Style {
	s.Attr |= a
	return s
}

// IsUnset reports whether s is exactly the zero Style, which is the reserved
// "the author did not choose" sentinel. Because Colour(0) is opaque black, a
// style cannot be assumed empty by inspection; this is the only test.
func (s Style) IsUnset() bool { return s == (Style{}) }

// Resolved returns s with every unset channel replaced by the terminal default:
// the whole zero Style becomes DefaultStyle, and any channel holding
// UnsetColour becomes DefaultColour.
//
// It is idempotent, and a no-op for every style built by NewStyle or a With*
// method. It never turns an authored colour into a default one — opaque black
// resolves to opaque black.
//
// THE IDIOM: every widget field of type Style is read through exactly this
// call. Resolved does not consult NO_COLOR, the environment or Caps; it is a
// pure function of the value.
func (s Style) Resolved() Style {
	if s.IsUnset() {
		// The zero Style means "author chose nothing", so it resolves wholesale
		// rather than field-wise: its zero channels are opaque black, not
		// inherit markers.
		return DefaultStyle
	}
	return Style{FG: resolvedColour(s.FG), BG: resolvedColour(s.BG), Attr: s.Attr}
}

// Patch returns s with every channel o actually specifies, and ORs o.Attr into
// s.Attr. An unset channel in o leaves s's channel alone, which is what makes
// "bold and this foreground, inherited background" expressible without a
// parallel type hierarchy.
//
// Only UnsetColour counts as unspecified. A patch channel holding opaque black
// OVERRIDES, because black is a real colour and inheriting is what UnsetColour
// is for. Patch therefore does not treat an all-zero o as "specify nothing" —
// it specifies black.
//
// Patch cannot REMOVE attributes; it only accumulates them. To clear one, build
// a Style explicitly. That asymmetry is deliberate: accumulation is the
// operation every caller wants and removal is rare enough to be explicit.
//
// Resolving a Style's unset sentinel is Resolved's job, not Patch's: s may
// legitimately be the zero Style (author chose nothing) and Patch must still
// inherit from it. Resolve the result, not the operands.
func (s Style) Patch(o Style) Style {
	if !o.FG.IsUnset() {
		s.FG = o.FG
	}
	if !o.BG.IsUnset() {
		s.BG = o.BG
	}
	s.Attr |= o.Attr
	return s
}

// Cell returns r rendered in this style.
//
// It is a field copy and does not resolve: passing an unset Style here produces
// a cell in the literal zero style, which is the documented footgun rather than
// a silent correction. Read the style through Resolved first.
func (s Style) Cell(r rune) Cell {
	return Cell{Ch: r, FG: s.FG, BG: s.BG, Attr: s.Attr}
}

// Blank returns a space in this style. It is the sanctioned way for a widget to
// express its background, e.g. FillRect(r, st.Blank()), so that every widget
// paints its chrome background the same way.
//
// Like Cell it does not resolve; read the style through Resolved first.
func (s Style) Blank() Cell { return s.Cell(' ') }

// Style returns the cell's rendition: the flat projection of its three styling
// fields. It is the inverse of Style.Cell and allocates nothing.
func (c Cell) Style() Style {
	return Style{FG: c.FG, BG: c.BG, Attr: c.Attr}
}

// Framework default styles.
//
// Attributes only — see the note on DefaultStyle and the framework's decision
// to ship no default colours. These are the only named styles the framework
// provides, and a widget that wants a bold header uses HeadingStyle rather than
// writing AttrBold|AttrUnderline itself.
//
// Every one of these carries UnsetColour, not Colour(0), in its colour channels.
// That is not cosmetic: ADR 0008 requires every framework default style to
// resolve to the terminal's own foreground and background, and an attribute-only
// literal would carry Colour(0) — opaque black — which resolves to opaque black.
// UnsetColour is the only channel value that means "not specified", so it is the
// only spelling that makes the documented behaviour true.
//
// It also makes these correct Patch bases: an override with an unset channel
// inherits the attribute-only default rather than an authored black, which is
// what the deferred theme type will rely on.
//
// Read one through Resolved (directly, or via SetSpans/SetString, which resolve
// on entry) before writing it into a cell.
var (
	// PlainStyle is DefaultStyle with no attributes. It is literally the unset
	// sentinel, which is exactly right: a style that specifies nothing and a
	// style that specifies the terminal's own colours are the same request.
	PlainStyle = Style{}

	// BoldStyle requests increased intensity.
	BoldStyle = Style{FG: UnsetColour, BG: UnsetColour, Attr: AttrBold}
	// FaintStyle requests decreased intensity.
	FaintStyle = Style{FG: UnsetColour, BG: UnsetColour, Attr: AttrFaint}
	// ItalicStyle requests italic rendition.
	ItalicStyle = Style{FG: UnsetColour, BG: UnsetColour, Attr: AttrItalic}
	// UnderlineStyle requests underline.
	UnderlineStyle = Style{FG: UnsetColour, BG: UnsetColour, Attr: AttrUnderline}
	// ReverseStyle swaps the foreground and background colours. Its widget-level
	// interaction with an explicit BG is not resolved by the framework; the
	// encoder emits both independently and the terminal decides.
	ReverseStyle = Style{FG: UnsetColour, BG: UnsetColour, Attr: AttrReverse}
	// StrikeStyle requests strikethrough.
	StrikeStyle = Style{FG: UnsetColour, BG: UnsetColour, Attr: AttrStrike}

	// EmphasisStyle is emphasis in a label or value. It exists because this is
	// requested constantly and three authors would otherwise each spell it
	// differently.
	EmphasisStyle = Style{FG: UnsetColour, BG: UnsetColour, Attr: AttrBold}
	// HeadingStyle is a column header or title.
	HeadingStyle = Style{FG: UnsetColour, BG: UnsetColour, Attr: AttrBold | AttrUnderline}
	// MutedStyle is secondary or disabled text.
	MutedStyle = Style{FG: UnsetColour, BG: UnsetColour, Attr: AttrFaint}
)
