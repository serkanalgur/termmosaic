package buffer

// Lab-space colour selection for the degradation ladder.
//
// This file is the replacement for the redmean selection that
// colour_quantiser_perceptual_test.go measured and condemned: redmean's
// rmean/256 weights were integer-zero for every pair of uint8 channels, so the
// formula degenerated to fixed-weight 2*dr^2 + 4*dg^2 + 2*db^2 in gamma RGB
// and flipped the hue of plausible UI colours (brick red -> olive, dark green
// -> grey, coral -> dark olive) and collapsed the markets "down" colour to
// grey at the 16 rung.
//
// Selection metric: CIEDE2000 (delta E 2000) on sRGB -> CIE Lab (D65).
//
// The audit scores the ladder in CIEDE2000 — the current CIE standard for
// colour difference — and separates unavoidable gamut error from quantiser
// selection error. The obvious cheaper alternative, CIE76 (plain Euclidean
// distance in Lab), was measured against that same oracle before being
// rejected: it regresses shipped colours relative even to the defective
// redmean on the audit's own metric. Concretely, at the 16 rung markets.down
// (#d86a62) moves from grey #808080 (dE00 selection error 10.24) to dark red
// #800000 (selection error 15.03) — the audit's ideal is bright red #ff0000,
// and CIE76's chroma-blind geometry ranks the desaturated dark red nearest.
// A replacement that lowers some bounds and raises others is not a
// replacement. CIEDE2000 selection makes the quantiser optimal on exactly the
// metric the audit measures, so every selection bound collapses to zero and
// every theme colour and golden moves strictly closer to what dE00 calls
// ideal.
//
// Cost: the frame path does not care which metric is chosen, because steady
// state is a memo lookup (below). The metric only prices FIRST USE of a
// colour: ~45 us for an exhaustive 256-entry CIEDE2000 search (~0.18 us per
// palette pair), so a first frame with a few hundred distinct colours costs a
// few milliseconds, once per process. CIE76 would be ~5 us; that saving is
// not worth shipping a quantiser the audit itself grades as defective.
//
// The sRGB -> XYZ (D65) -> Lab transform and the CIEDE2000 implementation
// live here, once, as production code. The perceptual audit test uses these
// exact functions as its measuring instrument and pins them (standard sRGB
// primaries, achromatic greys, Sharma's 34 reference pairs): one transform,
// one metric, two consumers — the quantiser and the oracle can never drift
// apart, and TestQuantiserEffectiveDistanceIsPinned checks the selection
// wiring against an independent brute-force loop over the same metric.

import (
	"math"
	"sync"
)

// lab is a CIE L*a*b* coordinate triple under D65.
type lab struct{ l, a, b float64 }

// srgbChannelToLinear converts one sRGB channel (0-255) to linear light.
func srgbChannelToLinear(v uint8) float64 {
	c := float64(v) / 255.0
	if c <= 0.04045 {
		return c / 12.92
	}
	return math.Pow((c+0.055)/1.055, 2.4)
}

// srgbToLinearLUT tables the channel transform: the quantiser converts
// colours on a hot-adjacent path, and the LUT is bit-identical to calling
// srgbChannelToLinear per channel (the audit's grey-achromaticity check would
// catch any drift).
var srgbToLinearLUT = buildSrgbToLinearLUT()

func buildSrgbToLinearLUT() (lut [256]float64) {
	for i := range lut {
		lut[i] = srgbChannelToLinear(uint8(i))
	}
	return lut
}

// labWhiteD65 is the D65 white point in the sRGB RGB->XYZ matrix's scale.
var labWhiteD65 = [3]float64{0.95047, 1.00000, 1.08883}

// rgbToLab converts an sRGB triple to CIE Lab under D65. This is the single
// sRGB->Lab transform in the package; the perceptual audit test pins it
// (standard primaries, achromatic greys) and measures everything through it.
func rgbToLab(r, g, b uint8) lab {
	rl, gl, bl := srgbToLinearLUT[r], srgbToLinearLUT[g], srgbToLinearLUT[b]
	x := 0.4124564*rl + 0.3575761*gl + 0.1804375*bl
	y := 0.2126729*rl + 0.7151522*gl + 0.0721750*bl
	z := 0.0193339*rl + 0.1191920*gl + 0.9503041*bl
	f := func(t float64) float64 {
		if t > 216.0/24389.0 {
			return math.Cbrt(t)
		}
		return (24389.0/27.0*t + 16.0) / 116.0
	}
	fx, fy, fz := f(x/labWhiteD65[0]), f(y/labWhiteD65[1]), f(z/labWhiteD65[2])
	return lab{116*fy - 16, 500 * (fx - fy), 200 * (fy - fz)}
}

// deltaE00 is the CIEDE2000 colour difference between two Lab colours. It
// follows Sharma, Wu & Dalal (2005), "The CIEDE2000 Color-Difference
// Formula: Implementation Notes, Supplementary Test Data, and Mathematical
// Observations"; the audit test pins it against their 34 reference pairs
// (TestQuantiserMetricIsPinned), so a refactor that breaks the metric fails
// the pinning test rather than quietly redefining "nearest".
func deltaE00(x, y lab) float64 {
	const deg = math.Pi / 180
	c1 := math.Hypot(x.a, x.b)
	c2 := math.Hypot(y.a, y.b)
	cbar := (c1 + c2) / 2
	g := 0.5 * (1 - math.Sqrt(math.Pow(cbar, 7)/(math.Pow(cbar, 7)+math.Pow(25.0, 7))))
	a1p := (1 + g) * x.a
	a2p := (1 + g) * y.a
	c1p := math.Hypot(a1p, x.b)
	c2p := math.Hypot(a2p, y.b)
	h1p := math.Atan2(x.b, a1p) / deg
	if h1p < 0 {
		h1p += 360
	}
	h2p := math.Atan2(y.b, a2p) / deg
	if h2p < 0 {
		h2p += 360
	}
	dLp := y.l - x.l
	dCp := c2p - c1p
	dhp := 0.0
	if c1p*c2p != 0 {
		dhp = h2p - h1p
		if dhp > 180 {
			dhp -= 360
		} else if dhp < -180 {
			dhp += 360
		}
	}
	dHp := 2 * math.Sqrt(c1p*c2p) * math.Sin(dhp*deg/2)
	lbarp := (x.l + y.l) / 2
	cbarp := (c1p + c2p) / 2
	// Sharma's reference form: when either chroma is zero the mean hue is the
	// raw sum (it is multiplied by zero everywhere it appears, so the choice
	// is immaterial to dE00 — but keeping the branch matches the reference
	// and keeps the initialisation meaningful).
	hbarp := h1p + h2p
	if c1p*c2p != 0 {
		if math.Abs(h1p-h2p) <= 180 {
			hbarp = (h1p + h2p) / 2
		} else if h1p+h2p < 360 {
			hbarp = (h1p + h2p + 360) / 2
		} else {
			hbarp = (h1p + h2p - 360) / 2
		}
	}
	t := 1 - 0.17*math.Cos((hbarp-30)*deg) + 0.24*math.Cos(2*hbarp*deg) +
		0.32*math.Cos((3*hbarp+6)*deg) - 0.20*math.Cos((4*hbarp-63)*deg)
	dtheta := 30 * math.Exp(-math.Pow((hbarp-275)/25, 2))
	rc := 2 * math.Sqrt(math.Pow(cbarp, 7)/(math.Pow(cbarp, 7)+math.Pow(25.0, 7)))
	sl := 1 + (0.015*math.Pow(lbarp-50, 2))/math.Sqrt(20+math.Pow(lbarp-50, 2))
	sc := 1 + 0.045*cbarp
	sh := 1 + 0.015*cbarp*t
	rt := -math.Sin(2*dtheta*deg) * rc
	return math.Sqrt(math.Pow(dLp/sl, 2) + math.Pow(dCp/sc, 2) + math.Pow(dHp/sh, 2) +
		rt*(dCp/sc)*(dHp/sh))
}

// Palette entries in Lab, once at init.
var (
	paletteLab16  [16]lab
	paletteLab256 [256]lab
)

func init() {
	for i, p := range NamedPalette {
		paletteLab16[i] = rgbToLab(p[0], p[1], p[2])
	}
	for i, p := range index256Palette {
		paletteLab256[i] = rgbToLab(p[0], p[1], p[2])
	}
}

// nearestPalette returns the index of the palette entry with the smallest
// CIEDE2000 difference to src. Ties resolve to the lowest index: the
// comparison is strict and the search runs in index order, so the one
// legitimate duplicate in the xterm palette (#808080 at indices 8 and 244)
// lands on 8 — the same tie-breaking rule redmean had, now in Lab space. The
// audit's oracle (measureColour in colour_quantiser_perceptual_test.go)
// iterates the same way over the same metric, so the quantiser's pick and the
// oracle's ideal coincide and measured selection error is zero by
// construction; the audit's thresholds pin exactly that property.
func nearestPalette(src lab, palette []lab) int {
	best, bestD := 0, math.Inf(1)
	for i, p := range palette {
		if d := deltaE00(src, p); d < bestD {
			best, bestD = i, d
		}
	}
	return best
}

// labQuantMemo caches Colour -> {Nearest16, Nearest256}. Quantisation is a
// pure function of the colour and the fixed palettes, so one process-wide
// table behind a read lock is sound and keeps every Quantiser value
// stateless. First use of a colour computes both rungs (the 256 search
// dominates; a 16-rung miss may as well warm the 256 entry while it holds
// the source Lab); steady state — a frame repainting colours already seen —
// is a hash lookup and nothing else, which is what keeps the 256/16 frame
// path allocation-free no matter how expensive the metric is.
//
// The memo is capped so a program that repaints arbitrary colours every
// frame (a full gradient sweep, say) cannot grow it towards the 16.7M
// distinct colours of the cube. Past the cap, colours are still quantised
// correctly — just uncached, at the exhaustive-search cost of ~45 us, which
// no first frame notices and no steady-state frame ever pays.
const labQuantMemoCap = 1 << 16

var labQuantMemo = struct {
	sync.RWMutex
	m map[Colour][2]uint8 // [0] = Nearest16, [1] = Nearest256
}{m: make(map[Colour][2]uint8)}

// nearestIndices returns the {16-rung, 256-rung} nearest palette indices for
// c under CIEDE2000, from the memo or by exhaustive search.
func nearestIndices(c Colour) [2]uint8 {
	labQuantMemo.RLock()
	v, ok := labQuantMemo.m[c]
	labQuantMemo.RUnlock()
	if ok {
		return v
	}
	r, g, b := c.RGB()
	src := rgbToLab(r, g, b)
	v = [2]uint8{
		uint8(nearestPalette(src, paletteLab16[:])),
		uint8(nearestPalette(src, paletteLab256[:])),
	}
	labQuantMemo.Lock()
	if len(labQuantMemo.m) < labQuantMemoCap {
		labQuantMemo.m[c] = v
	}
	labQuantMemo.Unlock()
	return v
}
