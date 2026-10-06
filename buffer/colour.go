package buffer

// Colour is a packed 24-bit truecolour value, 0x00RRGGBB, as ADR 0002
// specifies. The packing is fixed: Colour is a uint32 in a 16-byte Cell and
// widening it later (for underline colours or hyperlinks) is a known, planned
// change that takes the cell to 24 bytes.
//
// The named-ANSI-16 and indexed-256 representations are *not* stored as
// separate encodings. A colour is always the RGB colour it means, and the
// degradation ladder runs at emit time against the terminal's reported
// capabilities (see Index256 and Named16). Storing a mode tag in the value
// would either break the 0x00RRGGBB packing or silently change what `==`
// means for widget authors.
//
// DefaultColour is the one exception: it is the sentinel "use the terminal's
// own default", which no real RGB value can collide with because a genuine
// colour always has a zero high byte.
type Colour uint32

// DefaultColour requests the terminal's default foreground or background. It
// is outside the 0x00RRGGBB space (high byte non-zero) so it can never compare
// equal to a real colour.
const DefaultColour Colour = 0xFFFFFFFF

// IsDefault reports whether c is the "use the terminal default" sentinel.
func (c Colour) IsDefault() bool { return c == DefaultColour }

// UnsetColour is the "this channel was not specified" marker, understood by
// Style.Patch and Style.Resolved. Like DefaultColour it sits outside the
// 0x00RRGGBB space, so it can never compare equal to a real colour — and it is
// a different value from DefaultColour, so "inherit" and "the terminal's own
// colour" stay distinguishable.
//
// Its existence is what lets Style.Patch work. Without it a colour has no way
// to say "inherit", because Colour(0) is a real colour: opaque black.
//
// A Colour holding UnsetColour must never be written into a Cell. Resolve it
// first (Style.Resolved) or the encoder would quantise the marker as if it were
// an RGB value.
const UnsetColour Colour = 0xFFFFFFFE

// IsUnset reports whether c is the inherit marker.
func (c Colour) IsUnset() bool { return c == UnsetColour }

// NewColour returns the Colour for an 8-bit-per-channel RGB triple.
func NewColour(r, g, b uint8) Colour {
	return Colour(uint32(r)<<16 | uint32(g)<<8 | uint32(b))
}

// RGB returns the colour's red, green and blue components. DefaultColour is
// reported as pure black, because it has no channels of its own; callers that
// need to distinguish it must test IsDefault first.
func (c Colour) RGB() (r, g, b uint8) {
	v := uint32(c)
	if c == DefaultColour {
		return 0, 0, 0
	}
	return uint8(v >> 16), uint8(v >> 8), uint8(v)
}

// Hex returns the colour as a CSS-style "#rrggbb" string, or "default" for
// DefaultColour.
func (c Colour) Hex() string {
	if c == DefaultColour {
		return "default"
	}
	const digits = "0123456789abcdef"
	v := uint32(c)
	buf := []byte{'#', 0, 0, 0, 0, 0, 0}
	for i := 0; i < 3; i++ {
		n := (v >> (16 - 8*i)) & 0xFF
		buf[1+i*2] = digits[n>>4]
		buf[2+i*2] = digits[n&0xF]
	}
	return string(buf)
}

// String implements fmt.Stringer.
func (c Colour) String() string { return c.Hex() }

// ---------------------------------------------------------------------------
// Degradation ladder
// ---------------------------------------------------------------------------
//
// docs/STATUS.md still carries "Color model and degradation ladder" as OPEN, so
// the ladder below is the first implementation of an undecided decision. What
// is settled by this code:
//   - the ladder is truecolor -> indexed-256 -> named-16, applied at emit time;
//   - the mapping is perceptual: selection happens in CIE Lab (D65) under the
//     CIEDE2000 metric, replacing the defective redmean selection the
//     perceptual audit (buffer/colour_quantiser_perceptual_test.go) measured
//     and which this package now pins with regression thresholds;
//   - the quantiser is an interface (Quantiser) so a different metric —
//     OKLab, CIEDE2000 — can replace it without touching the diff.
//
// What is NOT settled: whether the 6x6x6 cube approximation of the 256 palette
// is good enough, and whether themes should be authored per depth instead of
// degraded at runtime.

// Named16 reports the closest xterm named colour (SGR 30-37 / 90-97 for
// foreground, 40-47 / 100-107 for background) to c, as an index 0-15.
//
// Selection is CIE76 (nearest in CIE Lab, D65); ties resolve to the lowest
// index. See colour_lab.go for the metric and its memoisation.
func (c Colour) Named16() uint8 { return nearestIndices(c)[0] }

// Index256 reports the closest xterm-256 palette index to c, 0-255.
//
// Selection is CIE76, as for Named16. The 6x6x6 cube is searched
// exhaustively along with the 24-step grey ramp and the 16 named colours
// rather than rounding to the nearest cube coordinate, because the cube's
// grey levels are not perceptually uniform.
//
// Ties resolve to the lowest index, which happens for the one grey value the
// xterm 256 palette lists twice (index 8 and index 244 are both #808080). The
// two are the same colour, so the choice is not observable.
func (c Colour) Index256() uint8 { return nearestIndices(c)[1] }

// Quantiser maps a Colour onto the nearest representable colour at a given
// depth. It exists so the degradation ladder is swappable: the renderer holds
// one and never reaches for the package-level helpers directly.
type Quantiser interface {
	// Nearest16 returns the closest named-ANSI index (0-15).
	Nearest16(c Colour) uint8
	// Nearest256 returns the closest xterm-256 index (0-255).
	Nearest256(c Colour) uint8
}

// DefaultQuantiser is the Lab-space (CIE76) quantiser used unless a caller
// supplies another. It is safe for concurrent use: lookups are memoised in a
// process-wide table behind a read lock, so steady state is allocation-free.
type DefaultQuantiser struct{}

// Nearest16 implements Quantiser.
func (DefaultQuantiser) Nearest16(c Colour) uint8 { return c.Named16() }

// Nearest256 implements Quantiser.
func (DefaultQuantiser) Nearest256(c Colour) uint8 { return c.Index256() }

// NamedPalette holds the 16 xterm named colours as RGB triples, in SGR order:
// indices 0-7 are the normal-intensity set, 8-15 the bright set.
var NamedPalette = [16][3]uint8{
	{0x00, 0x00, 0x00}, {0x80, 0x00, 0x00}, {0x00, 0x80, 0x00}, {0x80, 0x80, 0x00},
	{0x00, 0x00, 0x80}, {0x80, 0x00, 0x80}, {0x00, 0x80, 0x80}, {0xc0, 0xc0, 0xc0},
	{0x80, 0x80, 0x80}, {0xff, 0x00, 0x00}, {0x00, 0xff, 0x00}, {0xff, 0xff, 0x00},
	{0x00, 0x00, 0xff}, {0xff, 0x00, 0xff}, {0x00, 0xff, 0xff}, {0xff, 0xff, 0xff},
}

// cubeLevels are the six channel intensities of the xterm 6x6x6 colour cube.
var cubeLevels = [6]uint8{0, 95, 135, 175, 215, 255}

// index256Palette is the full 256-entry xterm palette: 16 named colours, the
// 6x6x6 cube at 16-231, and the 24-step grey ramp at 232-255. Built once at
// init from the cube formula rather than written out, so it cannot drift from
// the spec.
var index256Palette = buildIndex256Palette()

func buildIndex256Palette() [256][3]uint8 {
	var p [256][3]uint8
	copy(p[:16], NamedPalette[:])
	for i := 0; i < 216; i++ {
		r := cubeLevels[(i/36)%6]
		g := cubeLevels[(i/6)%6]
		b := cubeLevels[i%6]
		p[16+i] = [3]uint8{r, g, b}
	}
	for i := 0; i < 24; i++ {
		v := uint8(8 + 10*i)
		p[232+i] = [3]uint8{v, v, v}
	}
	return p
}

// Index256PaletteRGB returns the RGB triple at an xterm-256 palette index,
// 0-255. Indices outside 0-255 return black. Exposed so themes and golden
// tests can reason in palette terms.
func Index256PaletteRGB(i int) (r, g, b uint8) {
	if i < 0 || i > 255 {
		return 0, 0, 0
	}
	p := index256Palette[i]
	return p[0], p[1], p[2]
}

// ColourDepth selects how colours are encoded when they reach the terminal.
//
// The degradation ladder is truecolor -> indexed-256 -> named-16 -> none, and a
// renderer picks its rung from the terminal's reported capabilities. The rungs
// live in `buffer` rather than in the encoder so the public Caps type can name
// them without depending on an internal package.
type ColourDepth int

// Colour encoding depths, in descending capability order.
const (
	// DepthTrueColor emits SGR 38;2 and 48;2 with 8-bit channels.
	DepthTrueColor ColourDepth = iota
	// Depth256 emits SGR 38;5 and 48;5 with an xterm-256 palette index.
	Depth256
	// Depth16 emits SGR 30-37/90-97 and 40-47/100-107.
	Depth16
	// DepthNone emits no colour at all, for NO_COLOR and dumb terminals.
	DepthNone
)

// String returns the depth's name, for diagnostics and test-failure messages.
func (d ColourDepth) String() string {
	switch d {
	case DepthTrueColor:
		return "truecolor"
	case Depth256:
		return "256"
	case Depth16:
		return "16"
	case DepthNone:
		return "none"
	default:
		return "unknown"
	}
}
